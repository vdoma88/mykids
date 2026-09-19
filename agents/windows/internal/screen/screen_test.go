package screen

import (
	"strings"
	"testing"
	"time"

	"github.com/vdoma88/mykids/agents/windows/internal/usage"
)

func ctx() Context {
	return Context{
		Minutes: 0, Credits: 120, CreditsPerMinute: 2,
		TomorrowMinutes: 90, ChildURL: "https://семья.дом/child",
	}
}

// all собирает весь текст экрана — так проще проверять, чего в нём нет.
func all(s Screen) string {
	return s.Title + "\n" + strings.Join(s.Lines, "\n") + "\n" + s.Hint
}

func TestAllowedScreenIsEmpty(t *testing.T) {
	s := Build(usage.Verdict{Allow: true, LeftSecs: 1800}, ctx())
	if s.Kind != None {
		t.Fatalf("при разрешённом экране что-то показывается: %+v", s)
	}
}

func TestWarningComesBeforeTheScreenIsTaken(t *testing.T) {
	// Потерять экран посреди игры без предупреждения — худшее, что можно
	// сделать, и единственное, что запомнится.
	s := Build(usage.Verdict{Allow: true, LeftSecs: 300, WarnSoon: true}, ctx())

	if s.Kind != Warn {
		t.Fatalf("предупреждения нет: %+v", s)
	}
	if !strings.Contains(s.Title, "5 минут") {
		t.Fatalf("не сказано, сколько осталось: %q", s.Title)
	}
	// Единственное, что сейчас полезно.
	if !strings.Contains(s.Hint, "Сохранись") {
		t.Fatalf("не сказано главное — успеть сохраниться: %q", s.Hint)
	}
}

func TestLastMinuteIsNotCalledZero(t *testing.T) {
	// Найдено живым запуском: при остатке меньше минуты округление вниз
	// давало «Осталось 0 минут», и эта надпись висела перед глазами всю
	// последнюю минуту. Ноль тут — не предупреждение, а недоразумение.
	for _, left := range []int{59, 30, 1} {
		s := Build(usage.Verdict{Allow: true, LeftSecs: left, WarnSoon: true}, ctx())
		if strings.Contains(s.Title, "0 минут") {
			t.Fatalf("при остатке %d с показано %q", left, s.Title)
		}
		if !strings.Contains(s.Title, "меньше минуты") {
			t.Fatalf("при остатке %d с показано %q", left, s.Title)
		}
	}
	// А ровно минута — уже минута, и называть её «меньше» неправда.
	if s := Build(usage.Verdict{Allow: true, LeftSecs: 60, WarnSoon: true}, ctx()); !strings.Contains(s.Title, "1 минута") {
		t.Fatalf("минута названа как %q", s.Title)
	}
}

func TestLimitScreenShowsTheWayOut(t *testing.T) {
	s := Build(usage.Verdict{}, ctx())
	text := all(s)

	if s.Kind != Block {
		t.Fatalf("экран не закрыт: %+v", s)
	}
	// Кредиты сами по себе ни о чём не говорят, минуты говорят.
	if !strings.Contains(text, "120") || !strings.Contains(text, "1 час") {
		t.Fatalf("кредиты не пересчитаны в минуты: %q", text)
	}
	if !strings.Contains(text, "Завтра будет 1 час 30 минут") {
		t.Fatalf("не сказано, что будет завтра: %q", text)
	}
	if !strings.Contains(text, "семья.дом/child") {
		t.Fatalf("не сказано, куда идти зарабатывать: %q", text)
	}
}

func TestLimitScreenWithoutCreditsIsHonest(t *testing.T) {
	c := ctx()
	c.Credits = 0
	text := all(Build(usage.Verdict{}, c))

	if !strings.Contains(text, "Кредитов пока нет") {
		t.Fatalf("о пустом балансе не сказано прямо: %q", text)
	}
	// И всё равно показываем выход: закрытый экран без выхода — наказание.
	if !strings.Contains(text, "семья.дом/child") {
		t.Fatalf("без кредитов не сказано, где их взять: %q", text)
	}
}

func TestScheduleScreenSaysWhenItEnds(t *testing.T) {
	// «До 07:00» переносится куда легче, чем просто «закрыто».
	s := Build(usage.Verdict{Window: "отбой", Until: "07:00"}, ctx())
	if !strings.Contains(s.Title, "отбой") || !strings.Contains(s.Title, "07:00") {
		t.Fatalf("не сказано, что за окно и до какого часа: %q", s.Title)
	}
}

func TestScheduleScreenSaysTimeIsNotSpent(t *testing.T) {
	// Иначе закрытый экран ощущается как отнятое, а не как расписание.
	text := all(Build(usage.Verdict{Window: "отбой", Until: "07:00"}, ctx()))
	if !strings.Contains(text, "не тратится") {
		t.Fatalf("не сказано, что время сохраняется: %q", text)
	}
}

func TestScheduleScreenWithoutEndTime(t *testing.T) {
	// Окно без времени окончания — не повод писать «до »: лучше без хвоста.
	s := Build(usage.Verdict{Window: "отбой"}, ctx())
	if strings.Contains(s.Title, "до ") {
		t.Fatalf("обрывок «до» без времени: %q", s.Title)
	}
	if !strings.Contains(s.Title, "отбой") {
		t.Fatalf("окно не названо: %q", s.Title)
	}
}

func TestTasksOnlyScreenDoesNotPromiseWhatIsClosed(t *testing.T) {
	// Пока браузер не в белом списке, задания в этом режиме не открыты.
	// Написать «открыты только задания» значило бы соврать ребёнку.
	text := all(Build(usage.Verdict{Window: "уроки", Until: "16:00", TasksOnly: true}, ctx()))
	if strings.Contains(text, "Открыты") || strings.Contains(text, "открыты") {
		t.Fatalf("обещано то, чего нет: %q", text)
	}
	if !strings.Contains(text, "не тратится") {
		t.Fatalf("не сказано, что время сохраняется: %q", text)
	}
}

func TestOfflineScreenDoesNotInventAnAddress(t *testing.T) {
	// В автономном режиме адреса нет, и выдумывать его нельзя.
	c := ctx()
	c.ChildURL = ""
	s := Build(usage.Verdict{}, c)
	if s.Hint != "" {
		t.Fatalf("без сервера предложен несуществующий адрес: %q", s.Hint)
	}
}

func TestUnknownRateDoesNotInventMinutes(t *testing.T) {
	// Курс неизвестен — обещать пересчёт нельзя, но кредиты назвать можно.
	c := ctx()
	c.CreditsPerMinute = 0
	text := all(Build(usage.Verdict{}, c))
	if strings.Contains(text, "это ещё") {
		t.Fatalf("пересчёт обещан без курса: %q", text)
	}
	if !strings.Contains(text, "120") {
		t.Fatalf("кредиты не названы: %q", text)
	}
}

func TestToneIsNotCondescending(t *testing.T) {
	// Подростку уменьшительные слова и восклицания читаются как разговор
	// свысока, а оценки его поведения — как упрёк.
	screens := []Screen{
		Build(usage.Verdict{Allow: true, LeftSecs: 300, WarnSoon: true}, ctx()),
		Build(usage.Verdict{}, ctx()),
		Build(usage.Verdict{Window: "отбой", Until: "07:00"}, ctx()),
		Build(usage.Verdict{Window: "уроки", Until: "16:00", TasksOnly: true}, ctx()),
	}
	bad := []string{"!", "минуточк", "дружок", "малыш", "нельзя больше",
		"хватит", "слишком много", "ты потратил", "пора спать"}

	for _, s := range screens {
		text := strings.ToLower(all(s))
		for _, word := range bad {
			if strings.Contains(text, word) {
				t.Fatalf("разговор свысока: %q в %q", word, all(s))
			}
		}
	}
}

func TestMinutesWordDeclines(t *testing.T) {
	// «21 минута», а не «21 минут»: небрежный текст подросток замечает первым.
	cases := map[int]string{
		60: "1 минута", 120: "2 минуты", 300: "5 минут",
		660: "11 минут", 1260: "21 минута", 1320: "22 минуты",
		3600: "1 час", 7380: "2 часа 03 минуты", 18000: "5 часов",
	}
	for seconds, want := range cases {
		if got := minutesWord(seconds); got != want {
			t.Errorf("minutesWord(%d) = %q, ожидалось %q", seconds, got, want)
		}
	}
}

func TestDurationSpeaksRussian(t *testing.T) {
	// time.Duration печатает «1m30s», и на экране ребёнка это выглядит как
	// сообщение об ошибке, а не как объяснение.
	cases := map[time.Duration]string{
		45 * time.Second: "45 секунд", 21 * time.Second: "21 секунда",
		2 * time.Second: "2 секунды", 90 * time.Second: "1 минута",
		3 * time.Minute: "3 минуты", time.Hour: "1 час",
	}
	for d, want := range cases {
		if got := Duration(d); got != want {
			t.Errorf("Duration(%s) = %q, ожидалось %q", d, got, want)
		}
	}
}

func TestNegativeTimeShowsZero(t *testing.T) {
	// Остаток уходит в минус, когда экран отработал дольше выданного.
	// «−3 минуты» ребёнку ничего не говорят.
	if got := minutesWord(-500); got != "0 минут" {
		t.Fatalf("отрицательное время показано как %q", got)
	}
}
