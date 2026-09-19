package helper

import (
	"strings"
	"testing"
	"time"

	"github.com/vdoma88/mykids/agents/windows/internal/ipc"
)

func TestAllowLeavesScreenOpen(t *testing.T) {
	if s := OnVerdict(ipc.Verdict{Allow: true, LeftSecs: 600}); s.Block {
		t.Fatalf("разрешённый экран закрыт: %+v", s)
	}
}

func TestBlockUsesServiceText(t *testing.T) {
	// Текст придумывает служба: подменённый помощник иначе написал бы своё.
	s := OnVerdict(ipc.Verdict{Message: "Сейчас «отбой».\nЭкран закрыт по расписанию."})
	if !s.Block {
		t.Fatal("экран должен быть закрыт")
	}
	if !strings.Contains(s.Message, "отбой") {
		t.Fatalf("текст службы потерян: %q", s.Message)
	}
}

func TestBlockWithoutTextStillExplains(t *testing.T) {
	// Пустой чёрный экран без объяснений — худшее, что можно показать ребёнку.
	s := OnVerdict(ipc.Verdict{})
	if !s.Block || s.Message == "" {
		t.Fatalf("экран без объяснения: %+v", s)
	}
}

func TestBriefServiceRestartDoesNotBlink(t *testing.T) {
	// Диспетчер поднимает упавшую службу за секунды: мигать оверлеем незачем.
	for _, d := range []time.Duration{0, time.Second, Grace - time.Millisecond} {
		if s := OnServiceLost(d); s.Block {
			t.Fatalf("экран закрыт через %v после потери службы", d)
		}
	}
}

func TestLostServiceEventuallyBlocks(t *testing.T) {
	// Зеркало правила на стороне службы: без неё время никто не считает,
	// и оставить экран открытым значит раздавать его даром.
	s := OnServiceLost(Grace + time.Second)
	if !s.Block {
		t.Fatal("служба пропала надолго, а экран открыт")
	}
	if !strings.Contains(s.Message, "связи") {
		t.Fatalf("ребёнку не объяснили причину: %q", s.Message)
	}
}

func TestLostServiceMessageSaysHowLong(t *testing.T) {
	// Родителю это первое, что нужно знать, когда ребёнок жалуется на оверлей.
	s := OnServiceLost(3 * time.Minute)
	if !strings.Contains(s.Message, "3m0s") {
		t.Fatalf("в сообщении нет длительности: %q", s.Message)
	}
}
