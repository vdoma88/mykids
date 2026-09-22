package screen

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vdoma88/mykids/agents/windows/internal/usage"
)

func allowed(left int) usage.Verdict {
	return usage.Verdict{Allow: true, LeftSecs: left}
}

func TestTrayShowsHowMuchIsLeft(t *testing.T) {
	// Главное, ради чего значок и нужен: посмотреть остаток, ничего не открывая.
	tr := BuildTray(allowed(47*60), Context{})
	if !strings.Contains(tr.Tip, "47 минут") {
		t.Fatalf("под курсором не видно остатка: %q", tr.Tip)
	}
	if !strings.Contains(tr.Tip, "MyKids") {
		t.Fatalf("непонятно, чей это значок среди десятка других: %q", tr.Tip)
	}
}

func TestTrayNeverOffersToQuit(t *testing.T) {
	// Пункт «Выход» в меню наблюдателя — это предложение получить бесплатное
	// время одним щелчком. Проверяем на всех состояниях сразу.
	cases := []usage.Verdict{
		allowed(3600),
		allowed(30),
		{Allow: false},
		{Allow: false, Window: "отбой", Until: "07:00"},
	}
	forbidden := []string{"выход", "закрыть", "остановить", "отключить", "выключить", "снять"}
	for _, v := range cases {
		for _, it := range BuildTray(v, Context{ChildURL: "http://дом:3000/child", Credits: 120}).Items {
			label := strings.ToLower(it.Label)
			for _, bad := range forbidden {
				if strings.Contains(label, bad) {
					t.Fatalf("в меню есть «%s» — это способ снять наблюдателя щелчком", it.Label)
				}
			}
		}
	}
}

func TestTrayAgreesWithTheScreen(t *testing.T) {
	// Подпись под курсором и заголовок закрытого экрана подросток сверит
	// первым же делом. Разойдутся — не поверит ни одной.
	for _, v := range []usage.Verdict{
		{Allow: false},
		{Allow: false, Window: "отбой", Until: "07:00"},
	} {
		c := Context{TomorrowMinutes: 90}
		tip := BuildTray(v, c).Tip
		title := Build(v, c).Title
		if !strings.Contains(tip, title) {
			t.Fatalf("трей говорит %q, а экран %q", tip, title)
		}
	}
}

func TestTrayLeadsToTasksOnlyWhenThereIsAnAddress(t *testing.T) {
	// В автономном режиме адреса нет, и выдумывать его нельзя: пункт, который
	// никуда не ведёт, хуже отсутствующего.
	withURL := BuildTray(allowed(600), Context{ChildURL: "http://дом:3000/child"})
	var found string
	for _, it := range withURL.Items {
		if it.URL != "" {
			found = it.URL
		}
	}
	if found != "http://дом:3000/child" {
		t.Fatalf("из трея не попасть к заданиям: %+v", withURL.Items)
	}

	offline := BuildTray(allowed(600), Context{})
	for _, it := range offline.Items {
		if it.URL != "" {
			t.Fatalf("без адреса в меню всё равно есть ссылка: %q", it.URL)
		}
	}
}

func TestTrayTipFitsWindowsLimit(t *testing.T) {
	// Длинную подпись Windows не обрезает, а отбрасывает целиком — значок
	// выглядит сломанным. Имя окна приходит из политики, то есть извне.
	long := usage.Verdict{Allow: false, Window: strings.Repeat("очень длинное окно ", 30), Until: "07:00"}
	tip := BuildTray(long, Context{}).Tip
	if n := utf8.RuneCountInString(tip); n > TipLimit {
		t.Fatalf("подпись в %d знаков — Windows её не покажет вовсе", n)
	}
	if !strings.HasPrefix(tip, "MyKids") {
		t.Fatalf("при обрезке потеряно имя программы: %q", tip)
	}
	if !utf8.ValidString(tip) {
		t.Fatal("обрезка разрубила символ пополам")
	}
}

func TestTrayShowsCreditsWhenThereAreAny(t *testing.T) {
	// Кредиты — это выход из закрытого экрана, и знать про них надо заранее,
	// а не в тот момент, когда экран уже закрылся.
	tr := BuildTray(allowed(600), Context{Credits: 240})
	if !strings.Contains(strings.Join(labels(tr), "\n"), "240") {
		t.Fatalf("кредиты не видны: %+v", tr.Items)
	}
	// А нуля быть не должно: строка «Кредитов: 0» ничего не сообщает.
	if strings.Contains(strings.Join(labels(BuildTray(allowed(600), Context{})), "\n"), "Кредитов") {
		t.Fatal("показали нулевые кредиты — это строка ни о чём")
	}
}

func TestTrayStateMatchesRemainingTime(t *testing.T) {
	// Последнюю минуту называем словами, как и везде: «осталось 0 минут» при
	// полуминуте в запасе — бессмысленная цифра.
	if tip := BuildTray(allowed(30), Context{}).Tip; !strings.Contains(tip, "меньше минуты") {
		t.Fatalf("последняя минута названа нулём: %q", tip)
	}
}

func labels(t Tray) []string {
	out := make([]string, 0, len(t.Items))
	for _, it := range t.Items {
		out = append(out, it.Label)
	}
	return out
}
