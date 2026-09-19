// Package watchdog — решения о том, когда поднимать помощника.
//
// Служба живёт в нулевой сессии и рабочего стола не видит; помощник живёт в
// сессии ребёнка и видит его. До сих пор помощника никто не запускал, и
// боевая установка получалась слепой: служба считала время, но окон не
// видела, оверлей рисовать было некому, а молчание помощника честно считала
// расходом — то есть экран закрывался всегда и ничем.
//
// Запуск процесса в чужой сессии — это Win32 и только он. Здесь — решения:
// когда пробовать, когда ждать, и когда перестать считать это сбоем и начать
// считать это снятием наблюдателя. Их можно проверить без Windows, и они
// проверены.
package watchdog

import "time"

// Session — сессия пользователя, в которой должен жить помощник.
//
// Ноль означает «никого нет»: экран никто не смотрит, и поднимать в нём
// нечего.
type Session uint32

// Desktop — то, что умеет Windows и не умеет тест.
type Desktop interface {
	// ActiveSession — сессия, в которой сейчас работает пользователь.
	// Ноль, если на машине никого нет.
	ActiveSession() Session
	// HelperAlive — жив ли запущенный помощник.
	HelperAlive() bool
	// StartHelper поднимает помощника в указанной сессии.
	StartHelper(Session) error
}

const (
	// FirstDelay — пауза перед повторной попыткой после неудачи.
	//
	// Не мгновенно: при входе в систему рабочий стол ещё не готов, и первая
	// попытка часто падает просто потому, что рано. Крутиться в этот момент
	// в цикле — верный способ забить журнал событий ошибками, которые
	// прошли бы сами.
	FirstDelay = 3 * time.Second
	// MaxDelay — потолок паузы. Дальше растить некуда: помощника нет,
	// а значит экран закрыт, и ребёнок ждёт.
	MaxDelay = time.Minute
	// Restarts и Window — сколько перезапусков за какое время считать
	// не сбоем, а снятием наблюдателя.
	//
	// Помощник может упасть сам, и один-два перезапуска — это починка,
	// о которой родителю знать незачем. Но помощник, который умирает
	// раз за разом, — это либо сломанная машина, либо ребёнок с
	// диспетчером задач, и в обоих случаях родитель должен об этом узнать.
	Restarts = 5
	Window   = 10 * time.Minute
)

// Action — что сделать по итогам проверки.
type Action struct {
	// Started — помощник только что поднят.
	Started bool
	// Err — почему не поднялся. Не повод останавливать службу: учёт идёт
	// и без помощника, молчание считается расходом.
	Err error
	// Tamper — пора сообщить родителю, что помощника снимают.
	Tamper bool
	// Detail — что написать в событии.
	Detail string
}

// Watchdog поднимает помощника и считает, как часто приходится это делать.
type Watchdog struct {
	desktop Desktop
	// next — раньше этого момента не пробуем.
	next time.Time
	// delay — текущая пауза после неудачи, растёт вдвое до MaxDelay.
	delay time.Duration
	// starts — моменты запусков за последнее окно.
	starts []time.Time
	// reported — о снятии уже сообщено; повторять на каждом перезапуске
	// незачем, иначе одно событие превратится в поток.
	reported bool
}

// New собирает сторожа.
func New(d Desktop) *Watchdog { return &Watchdog{desktop: d} }

// Check — одна проверка. Зовётся из цикла службы.
func (w *Watchdog) Check(now time.Time) Action {
	session := w.desktop.ActiveSession()
	if session == 0 {
		// Никто не вошёл — и поднимать нечего, и сбоем это не является.
		// Счётчик перезапусков сбрасываем: выход из системы не должен
		// копиться и выглядеть как снятие помощника.
		w.reset()
		return Action{}
	}

	if w.desktop.HelperAlive() {
		// Живой помощник — повод забыть прошлые неудачи, но не прошлые
		// перезапуски: именно их частота и отличает сбой от снятия.
		w.delay = 0
		w.next = time.Time{}
		return Action{}
	}

	if now.Before(w.next) {
		return Action{}
	}

	if err := w.desktop.StartHelper(session); err != nil {
		w.backoff(now)
		return Action{Err: err}
	}

	w.delay = 0
	w.next = time.Time{}
	return w.started(now)
}

// started отмечает запуск и решает, пора ли сообщать родителю.
func (w *Watchdog) started(now time.Time) Action {
	w.starts = append(w.starts, now)
	// Держим только окно: перезапуск месячной давности ни о чём не говорит.
	cut := now.Add(-Window)
	kept := w.starts[:0]
	for _, t := range w.starts {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	w.starts = kept

	if len(w.starts) < Restarts || w.reported {
		return Action{Started: true}
	}
	w.reported = true
	return Action{
		Started: true,
		Tamper:  true,
		Detail:  "помощник в сессии ребёнка перезапускался " + count(len(w.starts)) + " за " + Window.String(),
	}
}

// backoff откладывает следующую попытку.
func (w *Watchdog) backoff(now time.Time) {
	if w.delay == 0 {
		w.delay = FirstDelay
	} else if w.delay < MaxDelay {
		w.delay *= 2
	}
	if w.delay > MaxDelay {
		w.delay = MaxDelay
	}
	w.next = now.Add(w.delay)
}

// reset забывает всё: никого нет в системе, считать нечего.
func (w *Watchdog) reset() {
	w.starts = nil
	w.delay = 0
	w.next = time.Time{}
	w.reported = false
}

// count — число словами не нужно, нужна цифра; отдельной функцией, чтобы
// не тянуть strconv ради одной строки в двух местах.
func count(n int) string {
	if n <= 0 {
		return "0 раз"
	}
	digits := ""
	for v := n; v > 0; v /= 10 {
		digits = string(rune('0'+v%10)) + digits
	}
	word := "раз"
	if n%100 < 11 || n%100 > 14 {
		switch n % 10 {
		case 2, 3, 4:
			word = "раза"
		}
	}
	return digits + " " + word
}
