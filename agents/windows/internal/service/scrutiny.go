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
}

// Finding — то, что стоит показать родителю.
type Finding struct {
	Lying  bool
	Detail string
}

// Observe разбирает очередное наблюдение.
//
// idleThreshold берётся из политики: «покой» — это то, что учёт не списывает.
// now — часы службы; часам помощника здесь верить тем более нельзя.
func (s *Scrutiny) Observe(sample ipc.Sample, idleThreshold time.Duration, now time.Time) Finding {
	idle := time.Duration(sample.IdleSeconds) * time.Second
	atRest := sample.SessionLocked || idle >= idleThreshold

	if !atRest {
		// Покой кончился. Счёт обнулять здесь незачем: он обнуляется при входе
		// в покой ниже, а лишнее присваивание только делает вид, что важно.
		s.atRest = false
		return Finding{}
	}

	if !s.atRest {
		// Покой только начался: первое окно берём за точку отсчёта.
		s.atRest, s.changes, s.lastProcess = true, 0, sample.Process
		return Finding{}
	}

	if sample.Process == s.lastProcess || sample.Process == "" || s.lastProcess == "" {
		return Finding{}
	}
	s.lastProcess = sample.Process
	s.changes++

	if s.changes < LieThreshold {
		return Finding{}
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
	if !s.Distrusted(now) {
		return sample
	}
	sample.IdleSeconds = 0
	sample.SessionLocked = false
	return sample
}
