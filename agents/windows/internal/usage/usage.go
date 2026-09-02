// Package usage считает израсходованное экранное время.
//
// Агент считает только минуты. Кредиты и обмен остаются на сервере: раннер и
// агент работают на устройстве ребёнка, и доверять их арифметике в вопросах
// начисления нельзя.
package usage

import (
	"time"

	"github.com/vdoma88/mykids/agents/windows/internal/schedule"
)

// Sample — один замер состояния рабочего стола.
type Sample struct {
	At          time.Time
	Process     string        // имя исполняемого файла активного окна
	Idle        time.Duration // сколько прошло с последнего ввода
	SessionLock bool          // экран заблокирован или пользователь вышел
}

// Day — накопленное за локальные сутки.
type Day struct {
	Key          string `json:"day"`          // "YYYY-MM-DD" в поясе семьи
	UsedSeconds  int    `json:"usedSeconds"`  // израсходовано
	GrantSeconds int    `json:"grantSeconds"` // выдано на сегодня
}

// Remaining — сколько секунд осталось. Может уйти в минус: экран мог
// отработать дольше выданного, пока агент не успел заблокировать.
func (d Day) Remaining() int { return d.GrantSeconds - d.UsedSeconds }

// Accountant накапливает расход между замерами.
type Accountant struct {
	idleThreshold time.Duration
	alwaysAllowed map[string]bool
	last          time.Time
	// Максимальный засчитываемый разрыв между замерами. Всё, что больше,
	// считается сном или остановкой агента и не списывается целиком.
	maxGap time.Duration
}

// New создаёт учётчик.
func New(idleThreshold time.Duration, alwaysAllowed []string, maxGap time.Duration) *Accountant {
	allowed := make(map[string]bool, len(alwaysAllowed))
	for _, name := range alwaysAllowed {
		allowed[normalize(name)] = true
	}
	return &Accountant{idleThreshold: idleThreshold, alwaysAllowed: allowed, maxGap: maxGap}
}

func normalize(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		out = append(out, r)
	}
	return string(out)
}

// IsAllowlisted сообщает, освобождён ли процесс от учёта и блокировки.
func (a *Accountant) IsAllowlisted(process string) bool {
	return a.alwaysAllowed[normalize(process)]
}

// Observe засчитывает время, прошедшее с прошлого замера, и возвращает,
// сколько секунд списано.
func (a *Accountant) Observe(s Sample, day *Day, dayKey string) int {
	prev := a.last
	a.last = s.At

	if day.Key != dayKey {
		// Сутки сменились: счётчики обнуляются, выдачу проставит вызывающий.
		day.Key = dayKey
		day.UsedSeconds = 0
		day.GrantSeconds = 0
	}

	if prev.IsZero() {
		return 0 // первый замер задаёт точку отсчёта
	}

	gap := s.At.Sub(prev)
	if gap <= 0 {
		// Часы прыгнули назад. Время не списываем, но и не начисляем обратно.
		return 0
	}
	if gap > a.maxGap {
		gap = a.maxGap
	}

	switch {
	case s.SessionLock:
		return 0
	case s.Idle >= a.idleThreshold:
		return 0
	case a.IsAllowlisted(s.Process):
		return 0
	}

	seconds := int(gap.Seconds())
	day.UsedSeconds += seconds
	return seconds
}

// Grant проставляет дневную выдачу, если она ещё не выдана.
// Идемпотентна: повторный вызов в тех же сутках ничего не меняет.
func Grant(day *Day, limitMinutes, carryOverMinutes int) {
	total := (limitMinutes + carryOverMinutes) * 60
	if total > day.GrantSeconds {
		day.GrantSeconds = total
	}
}

// CarryOver — сколько минут переносится на завтра, с учётом потолка.
func CarryOver(prev Day, maxMinutes int) int {
	left := prev.Remaining() / 60
	if left < 0 {
		return 0
	}
	if left > maxMinutes {
		return maxMinutes
	}
	return left
}

// Verdict — итоговое решение агента.
type Verdict struct {
	Allow     bool
	Reason    string
	Window    string
	LeftSecs  int
	WarnSoon  bool
	TasksOnly bool
}

// Decide объединяет расписание и остаток времени.
//
// Расписание проверяется раньше баланса: купленные минуты не должны отменять
// отбой, иначе магазин превращается в способ обойти режим дня.
func Decide(windows []schedule.Window, m schedule.Moment, day Day, warnBefore time.Duration) Verdict {
	d := schedule.Evaluate(windows, m)
	left := day.Remaining()

	switch d.Mode {
	case schedule.ModeBlocked:
		return Verdict{Reason: "расписание", Window: d.Window, LeftSecs: left}
	case schedule.ModeTasksOnly:
		return Verdict{Reason: "только задания", Window: d.Window, LeftSecs: left, TasksOnly: true}
	}

	if left <= 0 {
		return Verdict{Reason: "время на сегодня закончилось", LeftSecs: 0}
	}
	return Verdict{
		Allow:    true,
		LeftSecs: left,
		WarnSoon: warnBefore > 0 && time.Duration(left)*time.Second <= warnBefore,
	}
}
