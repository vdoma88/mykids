// Package schedule решает, разрешён ли экран прямо сейчас по расписанию семьи.
//
// Логика намеренно повторяет packages/domain/src/time.ts: сервер считает то же
// самое онлайн, но агент обязан уметь решать без сети. Обе реализации гоняют
// общий файл packages/contracts/test-vectors/schedule.json.
package schedule

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Mode — режим окна расписания.
type Mode string

const (
	ModeBlocked   Mode = "blocked"    // экран недоступен
	ModeTasksOnly Mode = "tasks_only" // доступны только задания
	ModeAllowed   Mode = "allowed"    // явное разрешение, перекрывает блокирующие
)

// Window — окно расписания. Если To меньше From, окно пересекает полночь.
type Window struct {
	Name string `json:"name"`
	Days []int  `json:"days"` // 0 — воскресенье, как в time.Weekday
	From string `json:"from"` // "HH:MM" по местному времени
	To   string `json:"to"`
	Mode Mode   `json:"mode"`
}

// Moment — момент времени в поясе семьи.
type Moment struct {
	Day          string // "YYYY-MM-DD"
	Weekday      int    // 0 — воскресенье
	MinutesOfDay int    // 0..1439
}

// At раскладывает момент по местному времени.
func At(t time.Time, loc *time.Location) Moment {
	local := t.In(loc)
	return Moment{
		Day:          local.Format("2006-01-02"),
		Weekday:      int(local.Weekday()),
		MinutesOfDay: local.Hour()*60 + local.Minute(),
	}
}

// ParseTimeOfDay переводит "HH:MM" в минуты от полуночи.
func ParseTimeOfDay(v string) (int, error) {
	parts := strings.Split(v, ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("ожидался формат HH:MM, получено %q", v)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return 0, fmt.Errorf("неверный час в %q", v)
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return 0, fmt.Errorf("неверные минуты в %q", v)
	}
	return h*60 + m, nil
}

func containsDay(days []int, day int) bool {
	for _, d := range days {
		if d == day {
			return true
		}
	}
	return false
}

// Covers сообщает, попадает ли момент в окно.
//
// Дни недели у окна через полночь относятся к дню НАЧАЛА окна: отбой
// 21:30–07:00 по будням действует и в 06:00 субботы, потому что это
// продолжение пятничной ночи, и не действует в 06:00 понедельника.
func (w Window) Covers(m Moment) bool {
	from, err := ParseTimeOfDay(w.From)
	if err != nil {
		return false
	}
	to, err := ParseTimeOfDay(w.To)
	if err != nil {
		return false
	}
	if from == to {
		return false // вырожденное окно нулевой длины
	}

	if from < to {
		return containsDay(w.Days, m.Weekday) && m.MinutesOfDay >= from && m.MinutesOfDay < to
	}
	if m.MinutesOfDay >= from {
		return containsDay(w.Days, m.Weekday)
	}
	if m.MinutesOfDay < to {
		return containsDay(w.Days, (m.Weekday+6)%7)
	}
	return false
}

// Decision — итог применения расписания.
type Decision struct {
	Mode   Mode
	Window string // имя сработавшего окна, пусто если ни одно не подошло
}

// Evaluate применяет окна к моменту.
//
// Явное разрешающее окно перекрывает блокирующие. Иначе первое блокирующее,
// затем первое «только задания».
func Evaluate(windows []Window, m Moment) Decision {
	var blocking, tasksOnly *Window
	for i := range windows {
		w := &windows[i]
		if !w.Covers(m) {
			continue
		}
		switch w.Mode {
		case ModeAllowed:
			return Decision{Mode: ModeAllowed, Window: w.Name}
		case ModeBlocked:
			if blocking == nil {
				blocking = w
			}
		case ModeTasksOnly:
			if tasksOnly == nil {
				tasksOnly = w
			}
		}
	}
	if blocking != nil {
		return Decision{Mode: ModeBlocked, Window: blocking.Name}
	}
	if tasksOnly != nil {
		return Decision{Mode: ModeTasksOnly, Window: tasksOnly.Name}
	}
	return Decision{Mode: ModeAllowed}
}

// FirstMatch возвращает имя первого сработавшего окна в порядке объявления.
// Используется тестом на общих векторах.
func FirstMatch(windows []Window, m Moment) string {
	for _, w := range windows {
		if w.Covers(m) {
			return w.Name
		}
	}
	return ""
}
