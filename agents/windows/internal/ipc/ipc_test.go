package ipc

import (
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vdoma88/mykids/agents/windows/internal/agent"
)

// Служба подставляет Desktop в агента вместо настоящего рабочего стола.
var _ agent.Desktop = (*Desktop)(nil)

var at = time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

// pair поднимает службу и помощника на паре в памяти — ровно то же, что
// именованный канал, только без Windows.
func pair(t *testing.T, h Handler) *Conn {
	t.Helper()
	srv, cli := net.Pipe()
	go func() { _ = Serve(srv, h) }()
	c := Dial(cli)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestExchangeCarriesObservationAndVerdict(t *testing.T) {
	var got Sample
	c := pair(t, func(s Sample) Verdict {
		got = s
		return Verdict{Allow: true, LeftSecs: 1200, WarnSoon: true, Reason: "ок"}
	})

	sent := Sample{At: at, Process: "game.exe", IdleSeconds: 7, SessionLocked: true}
	v, err := c.Exchange(sent)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if got.Process != "game.exe" || got.IdleSeconds != 7 || !got.SessionLocked {
		t.Fatalf("наблюдение дошло искажённым: %+v", got)
	}
	if !got.At.Equal(at) {
		t.Fatalf("время наблюдения потеряно: %v", got.At)
	}
	if !v.Allow || v.LeftSecs != 1200 || !v.WarnSoon || v.Reason != "ок" {
		t.Fatalf("решение дошло искажённым: %+v", v)
	}
}

func TestExchangeSurvivesManyRounds(t *testing.T) {
	// Помощник шлёт наблюдения каждые несколько секунд часами: буфер не должен
	// разъехаться и склеить соседние строки.
	n := 0
	c := pair(t, func(s Sample) Verdict {
		n++
		return Verdict{Allow: true, LeftSecs: 3600 - n}
	})

	for i := 0; i < 500; i++ {
		v, err := c.Exchange(Sample{At: at, Process: "game.exe"})
		if err != nil {
			t.Fatalf("обмен %d: %v", i, err)
		}
		if v.LeftSecs != 3600-(i+1) {
			t.Fatalf("обмен %d: ответ не тот, %d", i, v.LeftSecs)
		}
	}
}

func TestClosedConnectionIsNotAnError(t *testing.T) {
	// Помощник перезапускается — служба обязана жить дальше.
	srv, cli := net.Pipe()
	done := make(chan error, 1)
	go func() { done <- Serve(srv, func(Sample) Verdict { return Verdict{} }) }()

	cli.Close()
	if err := <-done; !errors.Is(err, ErrClosed) {
		t.Fatalf("закрытие должно давать ErrClosed, получено %v", err)
	}
}

func TestGarbageBreaksConnection(t *testing.T) {
	// Помощник, шлющий мусор, либо сломан, либо подменён: разговаривать
	// с ним дальше нельзя.
	srv, cli := net.Pipe()
	done := make(chan error, 1)
	go func() { done <- Serve(srv, func(Sample) Verdict { return Verdict{Allow: true} }) }()

	go func() { io.WriteString(cli, "это не json\n") }()
	err := <-done
	if err == nil || errors.Is(err, ErrClosed) {
		t.Fatalf("мусор должен быть ошибкой, получено %v", err)
	}
	if !strings.Contains(err.Error(), "разбор наблюдения") {
		t.Fatalf("непонятная ошибка: %v", err)
	}
}

func TestOversizedLineIsRejected(t *testing.T) {
	// Помощник работает с правами ребёнка: подсунуть службе бесконечную
	// строку не должно быть способом её положить.
	srv, cli := net.Pipe()
	done := make(chan error, 1)
	go func() { done <- Serve(srv, func(Sample) Verdict { return Verdict{} }) }()

	go func() {
		defer cli.Close()
		io.WriteString(cli, `{"process":"`+strings.Repeat("ж", MaxLine)+`"}`+"\n")
	}()

	err := <-done
	if err == nil {
		t.Fatal("слишком длинная строка должна быть ошибкой")
	}
	if !strings.Contains(err.Error(), "длиннее") {
		t.Fatalf("ожидалась ошибка о длине, получено %v", err)
	}
}

func TestConcurrentHelpersDoNotShareState(t *testing.T) {
	// Пользовательских сессий может быть несколько: соединения независимы.
	var mu sync.Mutex
	seen := map[string]int{}
	h := func(s Sample) Verdict {
		mu.Lock()
		defer mu.Unlock()
		seen[s.Process]++
		return Verdict{Allow: true, Window: s.Process}
	}

	var wg sync.WaitGroup
	for _, name := range []string{"первый.exe", "второй.exe"} {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			c := pair(t, h)
			for i := 0; i < 20; i++ {
				v, err := c.Exchange(Sample{At: at, Process: p})
				if err != nil {
					t.Errorf("%s: %v", p, err)
					return
				}
				if v.Window != p {
					t.Errorf("%s: ответ от чужого соединения: %q", p, v.Window)
					return
				}
			}
		}(name)
	}
	wg.Wait()

	if seen["первый.exe"] != 20 || seen["второй.exe"] != 20 {
		t.Fatalf("наблюдения перепутаны: %+v", seen)
	}
}

// --- рабочий стол глазами службы ---

// clock — часы, которыми управляет тест.
type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

func TestDesktopUsesLatestSample(t *testing.T) {
	c := &clock{t: at}
	d := NewDesktop(0, c.now)
	d.Update(Sample{Process: "game.exe", IdleSeconds: 45, SessionLocked: true})

	proc, err := d.ForegroundProcess()
	if err != nil || proc != "game.exe" {
		t.Fatalf("активное окно: %q, %v", proc, err)
	}
	idle, err := d.IdleTime()
	if err != nil || idle != 45*time.Second {
		t.Fatalf("простой: %v, %v", idle, err)
	}
	if !d.SessionLocked() {
		t.Fatal("блокировка сессии потеряна")
	}
	if d.Silent() {
		t.Fatal("свежее наблюдение не может считаться молчанием")
	}
}

func TestSilentHelperMeansScreenInUse(t *testing.T) {
	// Главный инвариант: убить помощника не должно быть способом получить
	// бесплатное время.
	c := &clock{t: at}
	d := NewDesktop(0, c.now)
	d.Update(Sample{Process: "explorer.exe", IdleSeconds: 9999, SessionLocked: true})

	c.advance(DefaultStale + time.Second)

	if !d.Silent() {
		t.Fatal("молчание не замечено")
	}
	proc, _ := d.ForegroundProcess()
	if proc != SilentProcess {
		t.Fatalf("активное окно должно стать неизвестным, получено %q", proc)
	}
	// Простой обнуляется: иначе время перестало бы списываться.
	if idle, _ := d.IdleTime(); idle != 0 {
		t.Fatalf("простой при молчании: %v, ожидался ноль", idle)
	}
	// И блокировка тоже: заблокированный экран времени не тратит.
	if d.SessionLocked() {
		t.Fatal("при молчании экран нельзя считать заблокированным")
	}
}

func TestSilentProcessIsNotAllowlisted(t *testing.T) {
	// Имя обязано не совпадать ни с чем настоящим, иначе оно попадёт в белый
	// список и молчащий помощник откроет экран вместо того, чтобы закрыть.
	acc := newAccountantLike(t)
	if acc(SilentProcess) {
		t.Fatalf("%q попал в белый список", SilentProcess)
	}
	if !strings.ContainsAny(SilentProcess, "()") {
		t.Fatalf("%q слишком похоже на имя файла", SilentProcess)
	}
}

// newAccountantLike повторяет проверку белого списка так, как её делает учёт.
func newAccountantLike(t *testing.T) func(string) bool {
	t.Helper()
	allowed := map[string]bool{"explorer.exe": true, "mykids-agent.exe": true, "taskmgr.exe": true}
	return func(p string) bool { return allowed[strings.ToLower(p)] }
}

func TestNoSampleAtAllIsSilent(t *testing.T) {
	// Служба поднялась, помощник ещё нет: это молчание, а не разрешение.
	c := &clock{t: at}
	d := NewDesktop(0, c.now)
	if !d.Silent() {
		t.Fatal("до первого наблюдения должно быть молчание")
	}
	if proc, _ := d.ForegroundProcess(); proc != SilentProcess {
		t.Fatalf("активное окно: %q", proc)
	}
	if d.Since() != 0 {
		t.Fatalf("без наблюдений Since должен быть нулём, получено %v", d.Since())
	}
}

func TestDesktopRecoversWhenHelperReturns(t *testing.T) {
	c := &clock{t: at}
	d := NewDesktop(0, c.now)
	d.Update(Sample{Process: "game.exe"})
	c.advance(time.Hour)
	if !d.Silent() {
		t.Fatal("подготовка: должно быть молчание")
	}

	d.Update(Sample{Process: "word.exe", IdleSeconds: 3})
	if d.Silent() {
		t.Fatal("помощник вернулся, а служба считает его пропавшим")
	}
	if proc, _ := d.ForegroundProcess(); proc != "word.exe" {
		t.Fatalf("новое наблюдение не применилось: %q", proc)
	}
}

func TestDesktopIgnoresHelperClock(t *testing.T) {
	// Помощник работает с правами ребёнка: его часы — не источник истины.
	// Наблюдение со временем из прошлого всё равно считается свежим.
	c := &clock{t: at}
	d := NewDesktop(0, c.now)
	d.Update(Sample{At: at.Add(-10 * time.Hour), Process: "game.exe"})

	if d.Silent() {
		t.Fatal("свежесть считается по часам службы, а не помощника")
	}
}

func TestDesktopSinceGrows(t *testing.T) {
	c := &clock{t: at}
	d := NewDesktop(0, c.now)
	d.Update(Sample{Process: "game.exe"})
	c.advance(12 * time.Second)
	if got := d.Since(); got != 12*time.Second {
		t.Fatalf("Since вернул %v", got)
	}
}

func TestDesktopIsSafeForConcurrentUse(t *testing.T) {
	// Наблюдения кладёт поток соединения, а читает их цикл учёта.
	d := NewDesktop(0, time.Now)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			d.Update(Sample{Process: "game.exe", IdleSeconds: i})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			d.ForegroundProcess()
			d.IdleTime()
			d.SessionLocked()
			d.Silent()
		}
	}()
	wg.Wait()
}
