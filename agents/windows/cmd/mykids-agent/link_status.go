package main

import (
	"fmt"
	"time"

	"github.com/vdoma88/mykids/agents/windows/internal/agent"
	"github.com/vdoma88/mykids/agents/windows/internal/clock"
	"github.com/vdoma88/mykids/agents/windows/internal/config"
	"github.com/vdoma88/mykids/agents/windows/internal/link"
)

// printLink показывает состояние связи с сервером.
//
// Отдельный блок нужен потому, что «ребёнок получил лишнее время» чаще всего
// объясняется не учётом, а связью: устаревший кэш политики, очередь, которая
// не уходит, или переведённые часы.
func printLink(res link.Result, syncErr error, clk *clock.Clock, lnk *link.Link,
	e config.Enrollment, rec agent.Recovery) {
	fmt.Println("— связь с сервером —")
	fmt.Printf("  сервер:     %s\n", e.Redacted())

	switch {
	case syncErr != nil:
		fmt.Printf("  обмен:      ОШИБКА — %v\n", syncErr)
	case res.Online:
		fmt.Println("  обмен:      успешно")
	default:
		fmt.Println("  обмен:      нет связи, работаем автономно")
	}

	fmt.Printf("  политика:   %s\n", res.Source)
	if res.Stale {
		fmt.Println("              ВНИМАНИЕ: связи не было несколько суток")
	}
	if res.Note != "" {
		fmt.Printf("              %s\n", res.Note)
	}

	if res.Online {
		fmt.Printf("  остатки:    %d мин, %d кредитов\n", res.Balances.Minutes, res.Balances.Credits)
	}

	fmt.Printf("  очередь:    %d мин ждёт отправки", lnk.Queued())
	if lnk.Pending() > 0 {
		fmt.Printf(" (+%d с не набрали минуты)", lnk.Pending())
	}
	fmt.Println()
	if res.Accepted > 0 || res.Duplicates > 0 {
		fmt.Printf("  отправлено: принято %d, повторов %d\n", res.Accepted, res.Duplicates)
	}

	fmt.Println("— часы —")
	// Округляем до секунды: доли секунды — это сетевая задержка, а не сдвиг,
	// и печатать «поправка: 0s» вместо «нет» значит пугать родителя на ровном месте.
	offset := clk.Offset().Round(time.Second)
	switch {
	case offset == 0:
		fmt.Println("  поправка:   нет")
	case clk.Trusted():
		fmt.Printf("  поправка:   %s (по времени сервера)\n", offset)
	default:
		fmt.Printf("  поправка:   %s (компенсация замеченного сдвига)\n", offset)
	}
	if clk.Suspicious() {
		fmt.Println("              ВНИМАНИЕ: системные часы заметно расходятся с настоящим временем")
	}
	if n := clk.Tampers(); n > 0 {
		fmt.Printf("  подкруток:  %d за этот запуск\n", n)
	}

	if rec.Unclean || len(lnk.Tampers()) > 0 {
		fmt.Println("— вмешательство —")
		if rec.Unclean {
			fmt.Printf("  прошлый запуск завершился нештатно: агент не работал %s\n",
				rec.Gap.Round(time.Minute))
			// Именно «будет списано»: status ничего не меняет, списывает запуск.
			fmt.Printf("  при запуске watch или run будет списано: %d мин\n", rec.ChargedSecs/60)
		}
		if n := len(lnk.Tampers()); n > 0 {
			fmt.Printf("  не доставлено родителю: %d сообщ.\n", n)
		}
	}
}
