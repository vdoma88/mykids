package helper

import (
	"strings"
	"testing"
	"time"

	"github.com/vdoma88/mykids/agents/windows/internal/ipc"
	"github.com/vdoma88/mykids/agents/windows/internal/screen"
)

func TestAllowLeavesScreenAlone(t *testing.T) {
	s := OnVerdict(ipc.Verdict{Allow: true, LeftSecs: 600})
	if s.Kind != screen.None {
		t.Fatalf("при разрешённом экране что-то показывается: %+v", s)
	}
}

func TestWarningDoesNotBlockTheScreen(t *testing.T) {
	// Отнять экран ровно тогда, когда надо спешить сохраниться, значит
	// сделать предупреждение бесполезным и обидным.
	s := OnVerdict(ipc.Verdict{
		Allow: true, LeftSecs: 300,
		Screen: screen.Screen{Kind: screen.Warn, Title: "Осталось 5 минут", Hint: "Сохранись."},
	})
	if s.Kind != screen.Warn {
		t.Fatalf("предупреждение потеряно: %+v", s)
	}
	if !strings.Contains(s.Hint, "Сохранись") {
		t.Fatalf("самое полезное не дошло: %+v", s)
	}
}

func TestBlockUsesServiceText(t *testing.T) {
	// Текст придумывает служба: подменённый помощник иначе написал бы своё.
	s := OnVerdict(ipc.Verdict{Screen: screen.Screen{
		Title: "Сейчас «отбой» — до 07:00",
		Lines: []string{"Экранное время не тратится."},
	}})
	if s.Kind != screen.Block {
		t.Fatal("экран должен быть закрыт")
	}
	if !strings.Contains(s.Title, "отбой") || len(s.Lines) != 1 {
		t.Fatalf("текст службы искажён: %+v", s)
	}
}

func TestBlockWithoutTextStillExplains(t *testing.T) {
	// Пустой чёрный экран без объяснений — худшее, что можно показать.
	s := OnVerdict(ipc.Verdict{})
	if s.Kind != screen.Block || s.Title == "" {
		t.Fatalf("экран без объяснения: %+v", s)
	}
}

func TestBriefServiceRestartDoesNotBlink(t *testing.T) {
	// Диспетчер поднимает упавшую службу за секунды: мигать оверлеем незачем.
	for _, d := range []time.Duration{0, time.Second, Grace - time.Millisecond} {
		if s := OnServiceLost(d); s.Kind != screen.None {
			t.Fatalf("экран закрыт через %v после потери службы", d)
		}
	}
}

func TestLostServiceEventuallyBlocks(t *testing.T) {
	// Зеркало правила на стороне службы: без неё время никто не считает,
	// и оставить экран открытым значит раздавать его даром.
	s := OnServiceLost(Grace + time.Second)
	if s.Kind != screen.Block {
		t.Fatal("служба пропала надолго, а экран открыт")
	}
	text := s.Title + strings.Join(s.Lines, " ") + s.Hint
	if !strings.Contains(text, "связи") {
		t.Fatalf("ребёнку не объяснили причину: %q", text)
	}
	// И главное: ребёнок в этом не виноват, и вести себя как будто виноват —
	// значит учить его, что правила произвольны.
	if !strings.Contains(text, "сбой") {
		t.Fatalf("не сказано, что это сбой, а не наказание: %q", text)
	}
	if !strings.Contains(text, "родител") {
		t.Fatalf("не сказано, что делать: %q", text)
	}
}

func TestLostServiceMessageSaysHowLong(t *testing.T) {
	// Родителю это первое, что нужно знать, когда ребёнок жалуется на оверлей.
	s := OnServiceLost(3 * time.Minute)
	// И по-русски: «3m0s» на экране ребёнка выглядит как сообщение об ошибке.
	if !strings.Contains(strings.Join(s.Lines, " "), "3 минуты") {
		t.Fatalf("в сообщении нет длительности: %+v", s.Lines)
	}
}
