// Package screen — текст, который видит ребёнок.
//
// Лежит отдельно от службы нарочно: тот же текст показывает и одиночный режим
// без службы. Две редакции одних и тех же слов разъезжаются молча, и разъедется
// та, которую реже смотрят.
package screen

import (
	"fmt"
	"strings"
	"time"

	"github.com/vdoma88/mykids/agents/windows/internal/usage"
)

// Экран, который видит подросток, — это не наказание, а правило. Разница
// в том, что правило объясняет себя и оставляет выбор:
//
//   - предупреждать заранее. Потерять экран посреди игры без предупреждения —
//     худшее, что можно сделать, и единственное, что запоминается;
//   - называть числа, а не оценки. «Осталось 12 минут», а не «ты засиделся»;
//   - говорить, когда кончится. «До 07:00» переносится куда легче, чем просто
//     «закрыто»;
//   - говорить, тратится ли время. В отбой оно не тратится, и знать это важно:
//     иначе закрытый экран ощущается как отнятое;
//   - показывать выход. Сколько кредитов есть и что на них можно.
//
// Никаких восклицательных знаков и уменьшительных слов: подростку они читаются
// как разговор свысока.

// Kind — что должно быть на экране.
type Kind string

const (
	// None — ничего показывать не нужно.
	None Kind = ""
	// Warn — предупреждение: время скоро кончится. Не перекрывает экран.
	Warn Kind = "warn"
	// Block — экран закрыт.
	Block Kind = "block"
)

// Screen — то, что видит ребёнок.
//
// Текст целиком готовит служба, а помощник только рисует. Причина та же, что и
// везде на этой границе: помощник работает с правами ребёнка, и позволить ему
// сочинять надпись значило бы позволить подменённому помощнику написать что
// угодно — хоть «всё в порядке, играй дальше».
type Screen struct {
	Kind  Kind   `json:"kind,omitempty"`
	Title string `json:"title,omitempty"`
	// Lines — объяснение: сколько осталось, тратится ли время, что будет завтра.
	Lines []string `json:"lines,omitempty"`
	// Hint — что можно сделать прямо сейчас. Без этого закрытый экран
	// выглядит наказанием, а не правилом.
	Hint string `json:"hint,omitempty"`
}

// Context — то, что служба знает сверх решения учёта.
type Context struct {
	// Minutes и Credits — остатки на сервере.
	Minutes int
	Credits int
	// CreditsPerMinute — курс обмена. Ноль означает «курс неизвестен»:
	// сервера не видели, и обещать пересчёт нельзя.
	CreditsPerMinute int
	// TomorrowMinutes — дневной лимит на завтра.
	TomorrowMinutes int
	// ChildURL — куда идти за заданиями и магазином. Пусто в автономном режиме.
	ChildURL string
}

// Build превращает решение учёта в то, что увидит ребёнок.
func Build(v usage.Verdict, c Context) Screen {
	if v.Allow {
		if !v.WarnSoon {
			return Screen{}
		}
		// Единственное, что сейчас полезно, — успеть сохраниться.
		return Screen{
			Kind:  Warn,
			Title: fmt.Sprintf("Осталось %s", leftWord(v.LeftSecs)),
			Hint:  "Сохранись.",
		}
	}

	switch {
	case v.Window != "" && v.TasksOnly:
		return Screen{
			Kind:  Block,
			Title: windowTitle(v),
			// Про «открыты задания» не пишем: пока браузер не в белом списке,
			// они не открыты, и обещать это значило бы соврать.
			Lines: []string{"Экранное время не тратится — оно останется на потом."},
			Hint:  tasksHint(c),
		}

	case v.Window != "":
		return Screen{
			Kind:  Block,
			Title: windowTitle(v),
			Lines: []string{"Экранное время не тратится — оно останется на потом."},
		}

	default:
		return Screen{
			Kind:  Block,
			Title: "Время на сегодня кончилось",
			Lines: limitLines(c),
			Hint:  tasksHint(c),
		}
	}
}

// windowTitle — «Сейчас «отбой» — до 07:00».
func windowTitle(v usage.Verdict) string {
	if v.Until == "" {
		return fmt.Sprintf("Сейчас «%s»", v.Window)
	}
	return fmt.Sprintf("Сейчас «%s» — до %s", v.Window, v.Until)
}

// limitLines объясняет, что делать с кончившимся лимитом.
func limitLines(c Context) []string {
	var lines []string

	switch {
	case c.Credits <= 0:
		lines = append(lines, "Кредитов пока нет.")
	case c.CreditsPerMinute > 0:
		// Считаем в понятных ребёнку единицах: кредиты сами по себе ни о чём
		// не говорят, а минуты говорят.
		lines = append(lines, fmt.Sprintf("Кредитов: %d — это ещё %s.",
			c.Credits, minutesWord(c.Credits/c.CreditsPerMinute*60)))
	default:
		lines = append(lines, fmt.Sprintf("Кредитов: %d.", c.Credits))
	}

	if c.TomorrowMinutes > 0 {
		lines = append(lines, fmt.Sprintf("Завтра будет %s.", minutesWord(c.TomorrowMinutes*60)))
	}
	return lines
}

// tasksHint — куда идти зарабатывать. В автономном режиме адреса нет, и
// выдумывать его нельзя.
func tasksHint(c Context) string {
	if c.ChildURL == "" {
		return ""
	}
	return "Задания и магазин: " + c.ChildURL
}

// leftWord — остаток времени для обратного отсчёта.
//
// Последнюю минуту называем словами, а не нулём. Округлять вниз тут нельзя:
// «осталось 0 минут» при пятидесяти секундах в запасе — бессмысленная цифра,
// и висит она на экране целую минуту, пока время ещё идёт. Округлять вверх
// тоже нельзя: «1 минута» при трёх секундах — обещание, которое тут же
// нарушится, а подросток такое запоминает.
func leftWord(seconds int) string {
	if seconds < 60 {
		return "меньше минуты"
	}
	return minutesWord(seconds)
}

// minutesWord — «12 минут», «1 час 05 минут». Часы появляются только там, где
// без них считать неудобно.
func minutesWord(seconds int) string {
	if seconds < 0 {
		seconds = 0
	}
	m := seconds / 60
	if m < 60 {
		return fmt.Sprintf("%d %s", m, plural(m, "минута", "минуты", "минут"))
	}
	h := m / 60
	if m%60 == 0 {
		return fmt.Sprintf("%d %s", h, plural(h, "час", "часа", "часов"))
	}
	return fmt.Sprintf("%d %s %02d %s", h, plural(h, "час", "часа", "часов"),
		m%60, plural(m%60, "минута", "минуты", "минут"))
}

// Duration — длительность по-русски: «45 секунд», «3 минуты», «1 час 05 минут».
//
// time.Duration печатает себя по-английски («1m30s»), и на экране ребёнка это
// выглядит как сообщение об ошибке, а не как объяснение.
func Duration(d time.Duration) string {
	seconds := int(d / time.Second)
	if seconds < 0 {
		seconds = 0
	}
	if seconds < 60 {
		return fmt.Sprintf("%d %s", seconds, plural(seconds, "секунда", "секунды", "секунд"))
	}
	return minutesWord(seconds)
}

// plural склоняет слово по русскому числу: 1 минута, 2 минуты, 5 минут.
func plural(n int, one, few, many string) string {
	if n < 0 {
		n = -n
	}
	if n%100 >= 11 && n%100 <= 14 {
		return many
	}
	switch n % 10 {
	case 1:
		return one
	case 2, 3, 4:
		return few
	default:
		return many
	}
}

// Render складывает экран в сплошной текст для оверлея.
//
// Подсказка отделена пустой строкой: это не продолжение объяснения, а то
// единственное, что можно сделать прямо сейчас, и найти её надо с одного
// взгляда.
func Render(s Screen) string {
	parts := []string{s.Title}
	parts = append(parts, s.Lines...)
	out := strings.Join(parts, "\n")
	if s.Hint != "" {
		out += "\n\n" + s.Hint
	}
	return out
}
