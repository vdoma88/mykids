package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vdoma88/mykids/agents/windows/internal/agent"
	"github.com/vdoma88/mykids/agents/windows/internal/client"
	"github.com/vdoma88/mykids/agents/windows/internal/clock"
	"github.com/vdoma88/mykids/agents/windows/internal/config"
	"github.com/vdoma88/mykids/agents/windows/internal/link"
	"github.com/vdoma88/mykids/agents/windows/internal/outbox"
	"github.com/vdoma88/mykids/agents/windows/internal/schedule"
	"github.com/vdoma88/mykids/agents/windows/internal/state"
)

// desk — рабочий стол, которым управляет тест.
type desk struct {
	mu     sync.Mutex
	proc   string
	idle   time.Duration
	locked bool
	// fail — наблюдать перестало получаться: так ведёт себя win32-слой,
	// когда рабочего стола нет.
	fail bool
}

func (d *desk) set(proc string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.proc = proc
}
func (d *desk) breakDown() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.fail = true
}
func (d *desk) ForegroundProcess() (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.fail {
		return "", errors.New("рабочий стол недоступен")
	}
	return d.proc, nil
}
func (d *desk) IdleTime() (time.Duration, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.idle, nil
}
func (d *desk) SessionLocked() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.locked
}

// fakeClock — часы, которыми управляет тест.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
	n  time.Duration
}

func (f *fakeClock) read() clock.Reading {
	f.mu.Lock()
	defer f.mu.Unlock()
	return clock.Reading{Wall: f.t, Mono: f.n}
}
func (f *fakeClock) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.t, f.n = f.t.Add(d), f.n+d
}

func policy() config.Policy {
	p := config.Default()
	p.Timezone = "UTC"
	p.DailyLimitMinutes = []int{60, 60, 60, 60, 60, 60, 60}
	p.Windows = []schedule.Window{}
	p.AlwaysAllowed = []string{"explorer.exe"}
	p.CarryOverMaxMinutes = 0
	return p
}

type harness struct {
	core      *Core
	desk      *desk
	clk       *fakeClock
	link      *link.Link
	statePath string
	logs      []string
	mu        sync.Mutex
}

// newHarness собирает ядро без сервера: клиент не настроен, значит работа идёт
// автономно — ровно как у агента, который сервера ещё не видел.
func newHarness(t *testing.T, st state.State) *harness {
	return newHarnessWith(t, st, nil, policy())
}

// newHarnessWith собирает стенд с заданным рабочим столом и политикой.
//
// Рабочий стол задаётся снаружи, потому что под службой он не win32, а тот,
// который наполняет помощник: проверять связку на фальшивом столе значило бы
// проверять не ту связку.
func newHarnessWith(t *testing.T, st state.State, desktop agent.Desktop, pol config.Policy) *harness {
	t.Helper()
	dir := t.TempDir()

	box, err := outbox.Open(filepath.Join(dir, "outbox.jsonl"))
	if err != nil {
		t.Fatalf("outbox: %v", err)
	}
	clk := clock.New(0)
	lnk := link.New(client.New("", "", "test"), box, clk, filepath.Join(dir, "cache.json"))
	lnk.SetPending(st.PendingSeconds)
	lnk.SetTampers(st.PendingTampers)

	d := &desk{proc: "game.exe"}
	if desktop == nil {
		desktop = d
	}
	a, err := agent.New(pol, desktop, nil, st)
	if err != nil {
		t.Fatalf("agent: %v", err)
	}

	fc := &fakeClock{t: time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)}
	h := &harness{desk: d, clk: fc, link: lnk, statePath: filepath.Join(dir, "state.json")}
	h.core = New(Options{
		Agent: a, Link: lnk, Clock: clk, Source: fc.read,
		StatePath: h.statePath, LocalPolicy: pol,
		Log: func(format string, args ...any) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.logs = append(h.logs, format)
		},
	})
	return h
}

func (h *harness) saved(t *testing.T) state.State {
	t.Helper()
	st, err := state.Load(h.statePath)
	if err != nil {
		t.Fatalf("чтение состояния: %v", err)
	}
	return st
}

func TestTickChargesTimeAndQueuesIt(t *testing.T) {
	h := newHarness(t, state.State{})
	h.core.Tick() // первый замер задаёт точку отсчёта

	h.clk.advance(90 * time.Second)
	v := h.core.Tick()

	if v.ConsumedSecs != 60 {
		// Разрыв между замерами ограничен минутой: сон машины не списывается целиком.
		t.Fatalf("списано %d секунд вместо 60", v.ConsumedSecs)
	}
	if h.link.Queued() != 1 {
		t.Fatalf("минута не попала в очередь на сервер: %d", h.link.Queued())
	}
	if !v.Allow {
		t.Fatalf("время ещё есть, а экран закрыт: %+v", v)
	}
}

func TestAllowlistedAppIsNotCharged(t *testing.T) {
	h := newHarness(t, state.State{})
	h.desk.set("explorer.exe")
	h.core.Tick()
	h.clk.advance(60 * time.Second)

	if v := h.core.Tick(); v.ConsumedSecs != 0 {
		t.Fatalf("приложение из белого списка списало %d секунд", v.ConsumedSecs)
	}
}

func TestSavePersistsEverythingNeededForRestart(t *testing.T) {
	h := newHarness(t, state.State{})
	h.core.Tick()
	h.clk.advance(30 * time.Second)
	h.core.Tick() // полминуты — в остаток, не в очередь

	h.core.Save(false)
	st := h.saved(t)

	if st.PendingSeconds != 30 {
		t.Fatalf("остаток секунд не сохранён: %d", st.PendingSeconds)
	}
	if st.LastSeenAt.IsZero() {
		t.Fatal("метка времени не сохранена — следующий запуск не посчитает пропуск")
	}
	if st.CleanShutdown {
		t.Fatal("рабочее сохранение не должно помечаться штатным завершением")
	}
	if st.Today.UsedSeconds != 30 {
		t.Fatalf("расход за день не сохранён: %d", st.Today.UsedSeconds)
	}
}

func TestCleanSaveMarksShutdown(t *testing.T) {
	// Штатная остановка: следующий запуск не станет оплачивать пропуск.
	h := newHarness(t, state.State{})
	h.core.Save(true)
	if !h.saved(t).CleanShutdown {
		t.Fatal("штатное завершение не отмечено")
	}
}

func TestRecoverChargesGapAndTellsParent(t *testing.T) {
	// Прошлый запуск убили: пропуск оплачивается, родитель узнаёт.
	killedAt := time.Date(2026, 9, 19, 11, 20, 0, 0, time.UTC)
	h := newHarness(t, state.State{LastSeenAt: killedAt})

	rec := h.core.Recover()
	if !rec.Unclean {
		t.Fatal("нештатная остановка не распознана")
	}
	if rec.ChargedSecs != 2400 {
		t.Fatalf("списано %d секунд вместо 40 минут", rec.ChargedSecs)
	}
	if h.link.Queued() != 40 {
		t.Fatalf("пропуск не ушёл в очередь на сервер: %d мин", h.link.Queued())
	}
	tampers := h.link.Tampers()
	if len(tampers) != 1 || tampers[0].Kind != "unclean_stop" {
		t.Fatalf("родителю не сообщили: %+v", tampers)
	}
	if !strings.Contains(tampers[0].Detail, "40 мин") {
		t.Fatalf("в сообщении нет величины списания: %q", tampers[0].Detail)
	}
}

func TestRecoverQuietAfterCleanShutdown(t *testing.T) {
	h := newHarness(t, state.State{CleanShutdown: true, LastSeenAt: time.Date(2026, 9, 19, 2, 0, 0, 0, time.UTC)})
	if rec := h.core.Recover(); rec.Unclean {
		t.Fatalf("штатная остановка принята за снятие: %+v", rec)
	}
	if len(h.link.Tampers()) != 0 {
		t.Fatal("родителю сообщили о том, чего не было")
	}
}

func TestRunSavesCleanlyOnCancel(t *testing.T) {
	// Служба получила команду остановиться: состояние обязано лечь на диск
	// помеченным как штатное завершение.
	h := newHarness(t, state.State{})
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- h.core.Run(ctx, Intervals{Tick: time.Millisecond, Sync: time.Hour, Save: time.Hour}) }()

	// Ждём, пока цикл хотя бы раз отработает.
	deadline := time.After(3 * time.Second)
	for h.core.Verdict().LeftSecs == 0 {
		select {
		case <-deadline:
			t.Fatal("цикл не сделал ни одного тика")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run вернул ошибку: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run не завершился по отмене")
	}

	if !h.saved(t).CleanShutdown {
		t.Fatal("остановка по команде не помечена штатной")
	}
}

func TestRunGrantsDailyLimitBeforeRecovering(t *testing.T) {
	// Порядок важен: ограничение списания за пропуск считается от дневной
	// выдачи. Без неё списывать было бы не из чего.
	killedAt := time.Date(2026, 9, 19, 11, 30, 0, 0, time.UTC)
	h := newHarness(t, state.State{LastSeenAt: killedAt})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.core.Run(ctx, Intervals{Tick: time.Hour, Sync: time.Hour, Save: time.Hour}) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	st := h.saved(t)
	if st.Today.GrantSeconds != 3600 {
		t.Fatalf("дневная выдача не проставлена: %d", st.Today.GrantSeconds)
	}
	// Полчаса пропуска списаны и ограничены выдачей.
	if st.Today.UsedSeconds != 1800 {
		t.Fatalf("пропуск списан неверно: %d секунд", st.Today.UsedSeconds)
	}
}

func TestConcurrentTicksAreSafe(t *testing.T) {
	// Наблюдения приходят из потока соединения с помощником, тики — из таймера.
	h := newHarness(t, state.State{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				h.core.Tick()
				h.core.Verdict()
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 200; j++ {
			h.core.Save(false)
		}
	}()
	wg.Wait()
}

func TestTickKeepsLastVerdictWhenDesktopFails(t *testing.T) {
	// Наблюдать не вышло: показывать помощнику нечего, прежнее решение в силе.
	// Обнулить его значило бы закрыть экран из-за сбоя наблюдения.
	h := newHarness(t, state.State{})
	h.core.Tick()
	h.clk.advance(30 * time.Second)
	h.core.Tick()
	before := h.core.Verdict()
	if before.LeftSecs == 0 {
		t.Fatal("подготовка: решение должно быть непустым")
	}

	h.desk.breakDown()
	h.clk.advance(30 * time.Second)

	got := h.core.Tick()
	if got.LeftSecs != before.LeftSecs || got.Allow != before.Allow {
		t.Fatalf("после ошибки наблюдения решение изменилось: было %+v, стало %+v", before, got)
	}
	// И время за этот замер не списано: наблюдения не было.
	if got.ConsumedSecs != before.ConsumedSecs {
		t.Fatalf("списание при неудачном наблюдении: %d", got.ConsumedSecs)
	}
}
