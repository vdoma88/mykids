package service

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/vdoma88/mykids/agents/windows/internal/config"
	"github.com/vdoma88/mykids/agents/windows/internal/ipc"
	"github.com/vdoma88/mykids/agents/windows/internal/state"
)

// withHelper собирает службу так, как она собрана в бою: рабочий стол агента —
// тот, который наполняет помощник, а не фальшивый.
func withHelper(t *testing.T, pol config.Policy) (*harness, *ipc.Conn) {
	t.Helper()
	remote := ipc.NewDesktop(0, time.Now)
	h := newHarnessWith(t, state.State{}, remote, pol)

	srv, cli := net.Pipe()
	go func() {
		_ = ipc.Serve(srv, h.core.Handler(remote, 2*time.Minute, func() time.Time {
			return h.clk.read().Wall
		}))
	}()
	c := ipc.Dial(cli)
	t.Cleanup(func() { _ = c.Close() })
	return h, c
}

// talk проводит несколько обменов, продвигая часы на секунду каждый.
func talk(t *testing.T, h *harness, c *ipc.Conn, rounds int, procs []string, idleSeconds int) {
	t.Helper()
	for i := 0; i < rounds; i++ {
		if _, err := c.Exchange(ipc.Sample{
			At:          h.clk.read().Wall,
			Process:     procs[i%len(procs)],
			IdleSeconds: idleSeconds,
		}); err != nil {
			t.Fatalf("обмен %d: %v", i, err)
		}
		h.clk.advance(time.Second)
	}
}

func TestLyingHelperIsCaughtAndChargedAnyway(t *testing.T) {
	h, c := withHelper(t, policy())
	// Всегда «меня тут нет», но окна переключаются одно за другим.
	talk(t, h, c, 12, []string{"game.exe", "chrome.exe", "steam.exe", "discord.exe"}, 9999)

	// Родитель обязан узнать.
	var lie string
	for _, e := range h.link.Tampers() {
		if e.Kind == "helper_lied" {
			lie = e.Detail
		}
	}
	if lie == "" {
		t.Fatalf("ложь помощника не дошла до родителя: %+v", h.link.Tampers())
	}
	if !strings.Contains(lie, "окно") {
		t.Fatalf("родителю не объяснили, в чём ложь: %q", lie)
	}

	// И главное: время всё равно списано, несмотря на заявленный простой.
	used := h.core.Verdict().LeftSecs
	if used == 3600 {
		t.Fatal("заявленному простою поверили — время не списывается")
	}
	if h.link.Queued() == 0 && h.link.Pending() == 0 {
		t.Fatal("списанное время никуда не ушло")
	}
}

func TestHonestHelperIsNotCharged(t *testing.T) {
	// Тот же путь, но помощник честный: окно одно, простой настоящий.
	h, c := withHelper(t, policy())
	talk(t, h, c, 12, []string{"game.exe"}, 9999)

	for _, e := range h.link.Tampers() {
		if e.Kind == "helper_lied" {
			t.Fatalf("честного помощника обвинили: %q", e.Detail)
		}
	}
	if h.core.Verdict().LeftSecs != 3600 {
		t.Fatalf("настоящий простой списал время: осталось %d", h.core.Verdict().LeftSecs)
	}
}

func TestVerdictCarriesReadyMessage(t *testing.T) {
	// Текст готовит служба: помощнику незачем знать правила.
	//
	// Лимит в минуту, а не час: разрыв между замерами ограничен минутой,
	// и часовой лимит пришлось бы выбирать шестьюдесятью тиками.
	short := policy()
	short.DailyLimitMinutes = []int{1, 1, 1, 1, 1, 1, 1}
	h := newHarnessWith(t, state.State{}, nil, short)

	h.core.Tick()
	h.clk.advance(61 * time.Second)
	h.core.Tick()

	v := Verdict(h.core.Verdict())
	if v.Allow {
		t.Fatalf("лимит исчерпан, а экран открыт: %+v", v)
	}
	if v.Message == "" {
		t.Fatal("помощнику не дали текст для оверлея")
	}
	if !strings.Contains(v.Message, "заработать") {
		t.Fatalf("ребёнку не сказали, что делать дальше: %q", v.Message)
	}
}

func TestAllowedVerdictCarriesNoMessage(t *testing.T) {
	// Пустой текст при разрешении — не забывчивость: рисовать нечего.
	h := newHarness(t, state.State{})
	h.core.Tick()
	v := Verdict(h.core.Verdict())
	if !v.Allow || v.Message != "" {
		t.Fatalf("при разрешённом экране пришёл текст: %+v", v)
	}
}
