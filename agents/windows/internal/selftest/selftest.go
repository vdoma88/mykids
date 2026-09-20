// Package selftest — проверка Win32-слоя на живой машине.
//
// Всё остальное в агенте проверяется тестами на любой ОС. Win32-слой — нет:
// активное окно, время простоя, блокировка экрана и оверлей существуют только
// в настоящей Windows, и до первого запуска там о них ничего не известно.
//
// Проверять это командной строкой — значит просить родителя разбирать вывод
// `status` и решать, хорошо это или плохо. Поэтому решения приняты здесь:
// каждая проверка сама говорит, прошла она или нет, и что делать, если нет.
// Окно — только кнопки и текст.
package selftest

import (
	"fmt"
	"time"
)

// Status — итог проверки.
type Status string

const (
	// Pass — работает.
	Pass Status = "ок"
	// Fail — не работает, и без этого агент бесполезен.
	Fail Status = "не работает"
	// Warn — работает, но что-то стоит поправить.
	Warn Status = "внимание"
)

// Result — что показать родителю про одну проверку.
type Result struct {
	Name   string
	Status Status
	// Detail — что именно увидели. Числа и имена, а не оценки: по ним можно
	// понять причину, а по «всё плохо» — нельзя.
	Detail string
	// Hint — что делать. Пусто, когда делать нечего.
	Hint string
	// Why — что эта проверка доказывает. Нужно, чтобы отчёт можно было
	// переслать, не пересказывая, зачем всё это.
	Why string
}

// Probe — всё, что проверки спрашивают у системы.
//
// Под Windows это internal/win32 и internal/winsvc, в тестах — фальшивка.
// Иначе проверки пришлось бы проверять на Windows, то есть не проверять.
type Probe interface {
	ForegroundProcess() (string, error)
	IdleTime() (time.Duration, error)
	SessionLocked() bool
	// SessionLockWTS — состояние экрана глазами диспетчера сессий: второй
	// источник, которым служба проверяет помощника. Второе значение — удалось
	// ли спросить.
	SessionLockWTS() (locked bool, known bool)
	ActiveSession() uint32
	ServiceState() (string, error)
	// Elevated — запущено ли с правами администратора.
	Elevated() bool
}

// RestQuiet — сколько не трогать мышь, чтобы увидеть рост простоя.
//
// Три секунды, а не полминуты: родитель не будет сидеть неподвижно долго,
// а для проверки «счётчик вообще идёт» этого довольно.
const RestQuiet = 3 * time.Second

// FreshInput — каким должен быть простой сразу после нажатия кнопки.
//
// С запасом: между нажатием и замером проходит доля секунды, но если
// GetLastInputInfo и GetTickCount64 меряют разными единицами — а это
// классическая ошибка в этом месте, — разница будет не в секундах, а в
// сутках, и порог её поймает.
const FreshInput = 30 * time.Second

// Run прогоняет все проверки по порядку.
//
// sleep вынесен наружу, чтобы тесты не ждали по-настоящему.
func Run(p Probe, sleep func(time.Duration)) []Result {
	return []Result{
		foreground(p),
		idle(p, sleep),
		locked(p),
		lockWTS(p),
		session(p),
		service(p),
		rights(p),
	}
}

func foreground(p Probe) Result {
	r := Result{
		Name: "Активное окно",
		Why:  "по имени окна агент отличает игру от браузера: без этого он не сможет ни считать время, ни пропускать разрешённое",
	}
	name, err := p.ForegroundProcess()
	switch {
	case err != nil:
		r.Status, r.Detail = Fail, err.Error()
		r.Hint = "Агент не видит активное окно. Проверьте, что программа запущена в той же сессии, где вы работаете, а не по удалённому подключению."
	case name == "":
		r.Status, r.Detail = Fail, "имя пустое"
		r.Hint = "Окно нашлось, а имя процесса — нет. Сообщите об этом: без имени всё экранное время будет считаться неизвестным, то есть запрещённым."
	default:
		r.Status, r.Detail = Pass, "сейчас активно: "+name
	}
	return r
}

func idle(p Probe, sleep func(time.Duration)) Result {
	r := Result{
		Name: "Время простоя",
		Why:  "время без ввода не списывается; если счётчик простоя врёт, ребёнок будет терять минуты, пока его нет за компьютером",
	}

	first, err := p.IdleTime()
	if err != nil {
		r.Status, r.Detail = Fail, err.Error()
		r.Hint = "Windows не отдаёт время последнего ввода. Сообщите об этом."
		return r
	}
	// Замер идёт сразу после нажатия кнопки, поэтому простой обязан быть
	// близким к нулю. Огромное значение здесь означает, что единицы
	// измерения перепутаны, — и тогда агент сочтёт отдыхом всё подряд.
	if first > FreshInput {
		r.Status = Fail
		r.Detail = fmt.Sprintf("сразу после нажатия кнопки простой показан как %s", human(first))
		r.Hint = "Счётчик простоя считает неверно. С таким значением агент решит, что за компьютером никого нет, и перестанет списывать время."
		return r
	}

	sleep(RestQuiet)
	second, err := p.IdleTime()
	if err != nil {
		r.Status, r.Detail = Fail, err.Error()
		return r
	}

	grew := second - first
	// Половину даём на то, что родитель всё-таки шевельнул мышью.
	if grew < RestQuiet/2 {
		r.Status = Warn
		r.Detail = fmt.Sprintf("за %s простой вырос на %s", human(RestQuiet), human(grew))
		r.Hint = "Похоже, мышь или клавиатуру всё-таки трогали. Нажмите «Проверить всё» ещё раз и уберите руки."
		return r
	}

	r.Status = Pass
	r.Detail = fmt.Sprintf("за %s простой вырос на %s — счётчик идёт", human(RestQuiet), human(grew))
	return r
}

func locked(p Probe) Result {
	r := Result{
		Name: "Состояние экрана",
		Why:  "пока экран заблокирован, время не списывается; если агент считает его заблокированным всегда, лимит не кончится никогда",
	}
	if p.SessionLocked() {
		r.Status = Fail
		r.Detail = "экран считается заблокированным прямо сейчас"
		r.Hint = "Вы работаете за этим компьютером, а агент видит заблокированный экран. С такой ошибкой время не будет списываться вообще."
		return r
	}
	r.Status, r.Detail = Pass, "экран открыт — так и должно быть, вы за ним работаете"
	return r
}

func lockWTS(p Probe) Result {
	r := Result{
		Name: "Встречная проверка экрана",
		Why:  "служба спрашивает Windows о блокировке сама; без этого ей остаётся верить помощнику на слово, а помощник работает с правами ребёнка",
	}
	locked, known := p.SessionLockWTS()
	switch {
	case !known:
		r.Status = Warn
		r.Detail = "диспетчер сессий не ответил"
		r.Hint = "Не страшно: служба вернётся к слову помощника, как было раньше. Но одной защитой от подменённого помощника станет меньше — пришлите отчёт."
	case locked:
		// Проверку запускает человек, который прямо сейчас смотрит в экран.
		// Если Windows при этом говорит «заблокирован», ответ неверен — и
		// ошибка именно в ту сторону, которая раздаёт время даром.
		r.Status = Fail
		r.Detail = "Windows отвечает, что сессия заблокирована, хотя вы работаете за этим компьютером"
		r.Hint = "С таким ответом время не будет списываться вообще. Пришлите отчёт: службе придётся отключить встречную проверку на этой машине."
	default:
		r.Status = Pass
		r.Detail = "Windows отвечает: сессия открыта — как и есть на самом деле"
	}
	return r
}

func session(p Probe) Result {
	r := Result{
		Name: "Сессия пользователя",
		Why:  "по номеру сессии служба находит, куда запустить помощника: без него она останется слепой",
	}
	if id := p.ActiveSession(); id != 0 {
		r.Status, r.Detail = Pass, fmt.Sprintf("номер сессии: %d", id)
		return r
	}
	r.Status = Fail
	r.Detail = "активной сессии не видно"
	r.Hint = "Служба не сможет запустить помощника. Так бывает при удалённом подключении и на экране входа — проверьте, войдя в систему обычным способом."
	return r
}

func service(p Probe) Result {
	r := Result{
		Name: "Служба MyKids",
		Why:  "служба считает время и принимает решения; остановить её из учётной записи ребёнка нельзя",
	}
	state, err := p.ServiceState()
	switch {
	case err != nil:
		r.Status, r.Detail = Warn, err.Error()
		r.Hint = "Состояние службы прочитать не удалось. Для этого обычно нужны права администратора."
	case state == "не установлена":
		r.Status, r.Detail = Warn, state
		r.Hint = "Это нормально, если вы просто проверяете машину. Для боевой установки запустите install-windows.ps1."
	case state == "работает":
		r.Status, r.Detail = Pass, state
	default:
		r.Status, r.Detail = Warn, state
		r.Hint = "Служба установлена, но не работает. Пока она остановлена, время не считается."
	}
	return r
}

func rights(p Probe) Result {
	r := Result{
		Name: "Права запуска",
		Why:  "проверка должна показывать то, что увидит ребёнок, а он работает без прав администратора",
	}
	if p.Elevated() {
		r.Status = Warn
		r.Detail = "запущено с правами администратора"
		r.Hint = "Для полной картины запустите эту проверку ещё раз из учётной записи ребёнка, обычным двойным щелчком."
		return r
	}
	r.Status, r.Detail = Pass, "обычные права — как у ребёнка"
	return r
}

// Worst — итог всего прогона: худший из полученных.
//
// Нужен, чтобы в заголовке окна стояло одно слово, а не шесть.
func Worst(rs []Result) Status {
	out := Pass
	for _, r := range rs {
		if r.Status == Fail {
			return Fail
		}
		if r.Status == Warn {
			out = Warn
		}
	}
	return out
}

// human — длительность по-русски и без долей секунды.
func human(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%d с", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%d мин %02d с", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%d ч %02d мин", int(d.Hours()), int(d.Minutes())%60)
}
