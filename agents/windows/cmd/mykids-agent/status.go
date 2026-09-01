package main

import (
	"fmt"
	"time"

	"github.com/vdoma88/mykids/agents/windows/internal/agent"
	"github.com/vdoma88/mykids/agents/windows/internal/config"
	"github.com/vdoma88/mykids/agents/windows/internal/schedule"
	"github.com/vdoma88/mykids/agents/windows/internal/usage"
)

// printStatus печатает всё, что агент видит и как это истолковал.
//
// Смысл команды — дать проверить наблюдение до того, как агенту доверят
// закрывать экран, поэтому здесь показываются исходные данные, а не только
// итоговый вердикт.
func printStatus(a *agent.Agent, v usage.Verdict, now time.Time, p config.Policy, policyPath, statePath string, tickErr error) {
	m := schedule.At(now, a.Location)
	st := a.State()

	fmt.Println("— файлы —")
	fmt.Printf("  политика:   %s\n", policyPath)
	fmt.Printf("  состояние:  %s\n", statePath)

	fmt.Println("— время —")
	fmt.Printf("  пояс:       %s\n", p.Timezone)
	fmt.Printf("  локально:   %s (%s)\n", now.In(a.Location).Format("2006-01-02 15:04:05"), weekdayName(m.Weekday))

	fmt.Println("— что видит агент —")
	proc, procErr := a.Desktop.ForegroundProcess()
	switch {
	case procErr != nil:
		fmt.Printf("  активное окно: ОШИБКА — %v\n", procErr)
	case proc == "":
		fmt.Println("  активное окно: нет (экран заблокирован или окно системное)")
	default:
		mark := ""
		if a.IsAllowlisted(proc) {
			mark = "  [в белом списке, не считается]"
		}
		fmt.Printf("  активное окно: %s%s\n", proc, mark)
	}

	if idle, err := a.Desktop.IdleTime(); err != nil {
		fmt.Printf("  простой:       ОШИБКА — %v\n", err)
	} else {
		counted := "считается"
		if idle >= time.Duration(p.IdleThresholdSeconds)*time.Second {
			counted = "не считается, простой дольше порога"
		}
		fmt.Printf("  простой:       %s (%s)\n", idle.Round(time.Second), counted)
	}
	fmt.Printf("  сессия:        %s\n", lockedLabel(a.Desktop.SessionLocked()))

	fmt.Println("— учёт за сегодня —")
	fmt.Printf("  выдано:     %s\n", agent.FormatLeft(st.Today.GrantSeconds))
	fmt.Printf("  потрачено:  %s\n", agent.FormatLeft(st.Today.UsedSeconds))
	fmt.Printf("  осталось:   %s\n", agent.FormatLeft(st.Today.Remaining()))
	if st.Yesterday.Key != "" {
		fmt.Printf("  перенос со вчера: %d мин\n", usage.CarryOver(st.Yesterday, p.CarryOverMaxMinutes))
	}

	fmt.Println("— расписание —")
	if len(p.Windows) == 0 {
		fmt.Println("  окон не задано")
	}
	for _, w := range p.Windows {
		state := "не активно"
		if w.Covers(m) {
			state = "АКТИВНО"
		}
		fmt.Printf("  %-10s %s–%s  %s\n", w.Name, w.From, w.To, state)
	}

	fmt.Println("— вердикт —")
	if tickErr != nil {
		fmt.Printf("  ОШИБКА наблюдения: %v\n", tickErr)
	} else {
		fmt.Printf("  %s\n", describe(v))
	}
	if st.UncleanStops > 0 {
		fmt.Printf("  нештатных остановок: %d\n", st.UncleanStops)
	}
}

func weekdayName(d int) string {
	names := []string{"воскресенье", "понедельник", "вторник", "среда", "четверг", "пятница", "суббота"}
	if d < 0 || d >= len(names) {
		return "?"
	}
	return names[d]
}

func lockedLabel(locked bool) string {
	if locked {
		return "заблокирована"
	}
	return "активна"
}
