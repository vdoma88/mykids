package service

import (
	"fmt"
	"time"

	"github.com/vdoma88/mykids/agents/windows/internal/ipc"
)

// Заявленный простой — единственное, чего служба проверить не может напрямую:
// из нулевой сессии не видно, трогали ли клавиатуру. Помощник работает с
// правами ребёнка, и подменённый помощник, вечно сообщающий «простой», получил
// бы бесконечное бесплатное время.
//
// Прямо проверить нельзя, но поймать на противоречии можно: без ввода активное
// окно не переключается. Если помощник заявляет простой, а окна при этом
// сменяются одно за другим, он лжёт — и это видно, не выходя за пределы того,
// что он сам сообщил.
//
// Порог в несколько смен, а не одна: окно умеет меняться и само — всплывшее
// уведомление, запущенная по расписанию задача, хранитель экрана. Одна смена
// ничего не доказывает, три подряд при заявленном покое — доказывают.
const (
	// LieThreshold — сколько смен окна при заявленном покое считать ложью.
	LieThreshold = 3
	// DistrustFor — как долго не верить простою после пойманной лжи.
	//
	// Не навсегда: помощник мог быть подменён на время, а вечное недоверие
	// означало бы, что ребёнок больше никогда не сможет отойти от компьютера
	// без списания.
	DistrustFor = 10 * time.Minute
	// LockLieThreshold — сколько расхождений подряд с собственным источником
	// считать ложью.
	//
	// Не одно: ребёнок может разблокировать экран ровно между замером
	// помощника и вопросом службы, и тогда расхождение честное. Три подряд
	// гонкой уже не объяснишь.
	//
	// Порог нужен только для того, чтобы не дёргать родителя зря. Кражу
	// времени он не сторожит: заявленная блокировка отменяется сразу,
	// с первого же расхождения, не дожидаясь никакого счёта.
	LockLieThreshold = 3
)

// Scrutiny следит за правдоподобием того, что сообщает помощник.
type Scrutiny struct {
	// atRest — помощник сейчас заявляет покой: простой или блокировку.
	atRest bool
	// lastProcess — окно на прошлом замере, пока покой длится.
	lastProcess string
	// changes — сколько раз окно сменилось, не прерывая покоя.
	changes int
	// distrustUntil — до какого момента простою не верим.
	distrustUntil time.Time
	// lockOff — сколько раз подряд помощник заявил блокировку, которой
	// Windows не подтвердила.
	lockOff int
	// ownLock — что сказал собственный источник на последнем наблюдении.
	// Его же применяет Correct: правка относится к тому наблюдению, которое
	// только что разобрали.
	ownLock ipc.LockState
}

// Finding — то, что стоит показать родителю.
type Finding struct {
	Lying  bool
	Detail string
}

// Observe разбирает очередное наблюдение.
//
// own — что о блокировке экрана знает сама служба, спросив Windows.
// ipc.LockUnknown означает «спросить не удалось»: тогда всё как раньше, на
// слово помощнику.
//
// idleThreshold берётся из политики: «покой» — это то, что учёт не списывает.
// now — часы службы; часам помощника здесь верить тем более нельзя.
func (s *Scrutiny) Observe(sample ipc.Sample, own ipc.LockState, idleThreshold time.Duration, now time.Time) Finding {
	s.ownLock = own
	lockLie := s.checkLock(sample, own, now)

	// Дальше разбираем уже исправленное наблюдение: заявленная блокировка,
	// которой Windows не подтвердила, покоем не считается, и прятаться за ней
	// от проверки на противоречии не выйдет.
	sample = overrideLock(sample, own)

	idle := time.Duration(sample.IdleSeconds) * time.Second
	atRest := sample.SessionLocked || idle >= idleThreshold

	if !atRest {
		// Покой кончился. Счёт обнулять здесь незачем: он обнуляется при входе
		// в покой ниже, а лишнее присваивание только делает вид, что важно.
		s.atRest = false
		return lockLie
	}

	if !s.atRest {
		// Покой только начался: первое окно берём за точку отсчёта.
		s.atRest, s.changes, s.lastProcess = true, 0, sample.Process
		return lockLie
	}

	if sample.Process == s.lastProcess || sample.Process == "" || s.lastProcess == "" {
		return lockLie
	}
	s.lastProcess = sample.Process
	s.changes++

	if s.changes < LieThreshold {
		return lockLie
	}

	// Поймали. Счёт сбрасываем, чтобы не сообщать родителю одно и то же
	// каждую секунду, а простою перестаём верить.
	s.changes = 0
	s.distrustUntil = now.Add(DistrustFor)
	what := "простой"
	if sample.SessionLocked {
		what = "заблокированный экран"
	}
	return Finding{
		Lying: true,
		Detail: fmt.Sprintf(
			"помощник сообщает %s, но активное окно сменилось %d раза подряд — без ввода окна не переключаются",
			what, LieThreshold),
	}
}

// Distrusted сообщает, верить ли сейчас заявленному покою.
func (s *Scrutiny) Distrusted(now time.Time) bool {
	return now.Before(s.distrustUntil)
}

// Correct убирает из наблюдения то, чему сейчас нет веры.
//
// Не выбрасывает наблюдение целиком: активное окно помощник сообщает честно
// (иначе его ловит та же проверка), а вот покой перестаёт засчитываться —
// время идёт как потраченное. Асимметрия та же, что и везде: лишняя минута
// списания против часов даром.
func (s *Scrutiny) Correct(sample ipc.Sample, now time.Time) ipc.Sample {
	// Сперва то, что служба знает сама: это не мера недоверия, а просто более
	// достоверный ответ на тот же вопрос. Применяется всегда, а не только к
	// пойманному помощнику.
	sample = overrideLock(sample, s.ownLock)

	if !s.Distrusted(now) {
		return sample
	}
	sample.IdleSeconds = 0
	sample.SessionLocked = false
	return sample
}

// overrideLock заменяет заявленную блокировку на то, что видит сама служба.
//
// В обе стороны, а не только в свою пользу. Подменить ответ Windows о
// состоянии сессии из-под учётной записи ребёнка нельзя, а заблокировать экран
// и одновременно им пользоваться нельзя тем более, — значит, этот ответ просто
// вернее того, что сказал помощник.
func overrideLock(sample ipc.Sample, own ipc.LockState) ipc.Sample {
	switch own {
	case ipc.LockOn:
		sample.SessionLocked = true
	case ipc.LockOff:
		sample.SessionLocked = false
	}
	return sample
}

// checkLock ловит помощника на заявленной блокировке, которой не было.
//
// Ловит только эту сторону расхождения. Обратная — помощник говорит «открыт»,
// Windows говорит «заблокирован» — ребёнку невыгодна и ничего не доказывает:
// помощник мог замерить экран за мгновение до блокировки. Обвинять за то, что
// стоило бы обвиняемому времени, незачем.
func (s *Scrutiny) checkLock(sample ipc.Sample, own ipc.LockState, now time.Time) Finding {
	if !sample.SessionLocked || own != ipc.LockOff {
		s.lockOff = 0
		return Finding{}
	}

	s.lockOff++
	if s.lockOff < LockLieThreshold {
		return Finding{}
	}

	// Поймали. Счёт сбрасываем, чтобы не повторять родителю одно и то же
	// каждую секунду, а заявленному покою перестаём верить целиком: помощник,
	// соврав о блокировке, не заслуживает доверия и в простое.
	s.lockOff = 0
	s.distrustUntil = now.Add(DistrustFor)
	return Finding{
		Lying: true,
		Detail: fmt.Sprintf(
			"помощник сообщает заблокированный экран %d раза подряд, а Windows отвечает, что сессия открыта",
			LockLieThreshold),
	}
}
