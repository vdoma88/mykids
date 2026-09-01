package main

import (
	"fmt"

	"github.com/vdoma88/mykids/agents/windows/internal/agent"
	"github.com/vdoma88/mykids/agents/windows/internal/usage"
)

// describe — человекочитаемая строка состояния. Печатается только при
// изменении, чтобы журнал не заливался одинаковыми строками.
func describe(v usage.Verdict) string {
	left := agent.FormatLeft(v.LeftSecs)
	switch {
	case v.Allow && v.WarnSoon:
		return fmt.Sprintf("разрешено, осталось %s — скоро конец", left)
	case v.Allow:
		return fmt.Sprintf("разрешено, осталось %s", left)
	case v.TasksOnly:
		return fmt.Sprintf("только задания (%s)", v.Window)
	case v.Window != "":
		return fmt.Sprintf("заблокировано расписанием (%s)", v.Window)
	default:
		return "заблокировано: время на сегодня закончилось"
	}
}
