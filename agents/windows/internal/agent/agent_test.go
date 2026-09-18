package agent

import (
	"testing"
	"time"

	"github.com/vdoma88/mykids/agents/windows/internal/config"
	"github.com/vdoma88/mykids/agents/windows/internal/schedule"
	"github.com/vdoma88/mykids/agents/windows/internal/state"
	"github.com/vdoma88/mykids/agents/windows/internal/usage"
)

type fakeDesktop struct {
	proc   string
	idle   time.Duration
	locked bool
}

func (f *fakeDesktop) ForegroundProcess() (string, error) { return f.proc, nil }
func (f *fakeDesktop) IdleTime() (time.Duration, error)   { return f.idle, nil }
func (f *fakeDesktop) SessionLocked() bool                { return f.locked }

type fakeEnforcer struct {
	blocked  bool
	messages []string
	blocks   int
	unblocks int
}

func (f *fakeEnforcer) Block(m string) error {
	if !f.blocked {
		f.blocks++
	}
	f.blocked = true
	f.messages = append(f.messages, m)
	return nil
}
func (f *fakeEnforcer) Unblock() {
	if f.blocked {
		f.unblocks++
	}
	f.blocked = false
}

func testPolicy() config.Policy {
	p := config.Default()
	p.Timezone = "UTC"
	p.DailyLimitMinutes = []int{60, 60, 60, 60, 60, 60, 60}
	p.Windows = []schedule.Window{
		{Name: "отбой", Days: []int{0, 1, 2, 3, 4, 5, 6}, From: "21:30", To: "07:00", Mode: schedule.ModeBlocked},
	}
	p.AlwaysAllowed = []string{"explorer.exe"}
	p.WarnBeforeMinutes = 5
	return p
}

func newAgent(t *testing.T, d Desktop, e Enforcer) *Agent {
	t.Helper()
	a, err := New(testPolicy(), d, e, state.State{})
	if err != nil {
		t.Fatalf("создание агента: %v", err)
	}
	return a
}

func TestGrantsDailyLimitOnFirstTick(t *testing.T) {
	a := newAgent(t, &fakeDesktop{proc: "game.exe"}, &fakeEnforcer{})
	v, err := a.Tick(time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !v.Allow || v.LeftSecs != 3600 {
		t.Errorf("первый тик: %+v, ожидалось 3600 секунд и разрешение", v)
	}
}

func TestBlocksWhenTimeRunsOut(t *testing.T) {
	e := &fakeEnforcer{}
	a := newAgent(t, &fakeDesktop{proc: "game.exe"}, e)
	base := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)

	// Тикаем минутными шагами, пока час не выйдет
	for i := 0; i <= 61; i++ {
		if _, err := a.Tick(base.Add(time.Duration(i) * time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	if !e.blocked {
		t.Fatal("экран не заблокирован после исчерпания лимита")
	}
	if e.blocks != 1 {
		t.Errorf("блокировка сработала %d раз, ожидался один переход", e.blocks)
	}
	last := e.messages[len(e.messages)-1]
	if last == "" || last == BlockMessage(usage.Verdict{Window: "отбой"}) {
		t.Errorf("неверное сообщение блокировки: %q", last)
	}
}

func TestScheduleBlocksDespiteFullBalance(t *testing.T) {
	e := &fakeEnforcer{}
	a := newAgent(t, &fakeDesktop{proc: "game.exe"}, e)
	// 22:00 — отбой, баланс полный
	if _, err := a.Tick(time.Date(2026, 3, 9, 22, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if !e.blocked {
		t.Error("отбой не заблокировал экран при полном балансе")
	}
}

func TestAllowlistedAppSurvivesBlock(t *testing.T) {
	e := &fakeEnforcer{}
	d := &fakeDesktop{proc: "explorer.exe"}
	a := newAgent(t, d, e)
	// Отбой, но активен проводник из белого списка
	v, err := a.Tick(time.Date(2026, 3, 9, 22, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !v.Allow || e.blocked {
		t.Errorf("приложение из белого списка заблокировано: %+v", v)
	}
}

func TestUnblocksWhenWindowEnds(t *testing.T) {
	e := &fakeEnforcer{}
	a := newAgent(t, &fakeDesktop{proc: "game.exe"}, e)
	if _, err := a.Tick(time.Date(2026, 3, 9, 22, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if !e.blocked {
		t.Fatal("отбой не заблокировал")
	}
	// 08:00 следующего дня: отбой кончился, сутки новые
	if _, err := a.Tick(time.Date(2026, 3, 10, 8, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if e.blocked {
		t.Error("экран не разблокирован после окончания окна")
	}
	if e.unblocks != 1 {
		t.Errorf("разблокировок %d, ожидалась одна", e.unblocks)
	}
}

func TestIdleTimeNotCharged(t *testing.T) {
	d := &fakeDesktop{proc: "game.exe", idle: 10 * time.Minute}
	a := newAgent(t, d, &fakeEnforcer{})
	base := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	for i := 0; i <= 70; i++ {
		if _, err := a.Tick(base.Add(time.Duration(i) * time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	v, _ := a.Tick(base.Add(71 * time.Minute))
	if !v.Allow {
		t.Errorf("простой списал время: осталось %d секунд", v.LeftSecs)
	}
}

func TestCarryOverAcrossDays(t *testing.T) {
	d := &fakeDesktop{proc: "game.exe"}
	a := newAgent(t, d, &fakeEnforcer{})
	// День первый: потратили 10 минут из 60
	base := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	for i := 0; i <= 10; i++ {
		a.Tick(base.Add(time.Duration(i) * time.Minute))
	}
	// День второй: 60 лимита + перенос, ограниченный потолком в 30
	v, err := a.Tick(time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if v.LeftSecs != (60+30)*60 {
		t.Errorf("остаток на второй день %d секунд, ожидалось %d", v.LeftSecs, (60+30)*60)
	}
}

func TestWarnBeforeLimit(t *testing.T) {
	a := newAgent(t, &fakeDesktop{proc: "game.exe"}, &fakeEnforcer{})
	base := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	var warned bool
	for i := 0; i <= 58; i++ {
		v, _ := a.Tick(base.Add(time.Duration(i) * time.Minute))
		if v.WarnSoon {
			warned = true
		}
	}
	if !warned {
		t.Error("предупреждение перед концом лимита не сработало")
	}
}

func TestFormatLeft(t *testing.T) {
	cases := map[int]string{0: "0 мин", 59: "0 мин", 600: "10 мин", 3900: "1 ч 05 мин", -5: "0 мин"}
	for secs, want := range cases {
		if got := FormatLeft(secs); got != want {
			t.Errorf("FormatLeft(%d) = %q, ожидалось %q", secs, got, want)
		}
	}
}

func TestSetPolicyAppliesServerLimits(t *testing.T) {
	a := newAgent(t, &fakeDesktop{proc: "game.exe"}, nil)
	next := testPolicy()
	next.DailyLimitMinutes = []int{5, 5, 5, 5, 5, 5, 5}

	changed, err := a.SetPolicy(next)
	if err != nil {
		t.Fatalf("SetPolicy: %v", err)
	}
	if !changed {
		t.Fatal("новая политика должна считаться изменением")
	}
	if a.Policy.LimitFor(1) != 5 {
		t.Fatalf("лимит не применился: %d", a.Policy.LimitFor(1))
	}
}

func TestSetPolicyIsQuietWhenNothingChanged(t *testing.T) {
	// Пересборка учётчика сбрасывает точку отсчёта: следующий замер списал бы
	// ноль. При обмене раз в минуту это дарило бы ребёнку время.
	a := newAgent(t, &fakeDesktop{proc: "game.exe"}, nil)

	base := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	if _, err := a.Tick(base); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	changed, err := a.SetPolicy(testPolicy())
	if err != nil {
		t.Fatalf("SetPolicy: %v", err)
	}
	if changed {
		t.Fatal("та же политика не должна считаться изменением")
	}

	v, err := a.Tick(base.Add(time.Minute))
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if v.ConsumedSecs != 60 {
		t.Fatalf("после пустой смены политики списано %d секунд вместо 60", v.ConsumedSecs)
	}
}

func TestSetPolicyRejectsBadTimezone(t *testing.T) {
	// Иначе агент принял бы политику, по которой не может посчитать сутки.
	a := newAgent(t, &fakeDesktop{proc: "game.exe"}, nil)
	bad := testPolicy()
	bad.Timezone = "Нет/Такого"
	if _, err := a.SetPolicy(bad); err == nil {
		t.Fatal("неизвестный пояс должен быть ошибкой")
	}
	if a.Policy.Timezone == "Нет/Такого" {
		t.Fatal("отклонённая политика не должна применяться")
	}
}

func TestSetPolicyAppliesNewAllowlist(t *testing.T) {
	// Белый список зашит в учётчик: без пересборки новый не подействовал бы.
	a := newAgent(t, &fakeDesktop{proc: "phone.exe"}, nil)
	if a.IsAllowlisted("phone.exe") {
		t.Fatal("подготовка: процесс не должен быть в списке")
	}
	next := testPolicy()
	next.AlwaysAllowed = []string{"phone.exe"}
	if _, err := a.SetPolicy(next); err != nil {
		t.Fatalf("SetPolicy: %v", err)
	}
	if !a.IsAllowlisted("phone.exe") {
		t.Fatal("новый белый список не применился")
	}
}

// agentWith собирает агента с заранее заданным состоянием — так проверяется
// то, что случилось до запуска.
func agentWith(t *testing.T, st state.State) *Agent {
	t.Helper()
	a, err := New(testPolicy(), &fakeDesktop{proc: "game.exe"}, nil, st)
	if err != nil {
		t.Fatalf("создание агента: %v", err)
	}
	return a
}

func TestRecoverChargesGapAfterKill(t *testing.T) {
	// Ребёнок снял агента в 12:00 и запустил обратно в 12:40, поиграв сорок
	// минут без учёта. Пропуск обязан быть оплачен, иначе снимать агента выгодно.
	killed := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	a := agentWith(t, state.State{LastSeenAt: killed})

	r := a.RecoverUnclean(killed.Add(40 * time.Minute))
	if !r.Unclean {
		t.Fatal("нештатная остановка не распознана")
	}
	if r.ChargedSecs != 2400 {
		t.Fatalf("списано %d секунд вместо 2400", r.ChargedSecs)
	}
	if got := a.State().Today.Remaining(); got != 1200 {
		t.Fatalf("остаток %d вместо 1200", got)
	}
	if a.State().UncleanStops != 1 {
		t.Fatalf("счётчик остановок: %d", a.State().UncleanStops)
	}
}

func TestRecoverQuietAfterCleanShutdown(t *testing.T) {
	// Штатная остановка — выключенный на ночь компьютер. Списывать нечего.
	stopped := time.Date(2026, 3, 8, 22, 0, 0, 0, time.UTC)
	a := agentWith(t, state.State{CleanShutdown: true, LastSeenAt: stopped})

	r := a.RecoverUnclean(stopped.Add(10 * time.Hour))
	if r.Unclean || r.ChargedSecs != 0 {
		t.Fatalf("штатная остановка не должна ничего списывать: %+v", r)
	}
	if a.State().UncleanStops != 0 {
		t.Fatalf("счётчик остановок вырос: %d", a.State().UncleanStops)
	}
}

func TestRecoverQuietOnFirstEverStart(t *testing.T) {
	// Первый запуск: сравнивать не с чем, и наказывать не за что.
	a := agentWith(t, state.State{})
	if r := a.RecoverUnclean(time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)); r.Unclean {
		t.Fatalf("первый запуск принят за нештатную остановку: %+v", r)
	}
}

func TestRecoverCappedByDailyGrant(t *testing.T) {
	// Агента не было сутки. Обнулить сегодняшний день — да, загнать в долг на
	// неделю вперёд из-за сбоя питания — нет.
	killed := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)
	a := agentWith(t, state.State{LastSeenAt: killed})

	r := a.RecoverUnclean(killed.Add(24 * time.Hour))
	if r.ChargedSecs != 3600 {
		t.Fatalf("списано %d вместо дневной выдачи 3600", r.ChargedSecs)
	}
	if got := a.State().Today.Remaining(); got != 0 {
		t.Fatalf("остаток %d вместо нуля", got)
	}
}

func TestRecoverThenTickKeepsCharge(t *testing.T) {
	// Списание не должно потеряться при первом же тике: Grant идемпотентен,
	// а Rollover не должен сработать второй раз и обнулить день.
	killed := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	a := agentWith(t, state.State{LastSeenAt: killed})
	a.RecoverUnclean(killed.Add(30 * time.Minute))

	v, err := a.Tick(killed.Add(30 * time.Minute))
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if v.LeftSecs != 1800 {
		t.Fatalf("после списания осталось %d вместо 1800", v.LeftSecs)
	}
}

func TestRecoverIgnoresBackwardClock(t *testing.T) {
	// Часы перевели назад — пропуск получился отрицательным. Дарить время за это нельзя.
	killed := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	a := agentWith(t, state.State{LastSeenAt: killed})

	r := a.RecoverUnclean(killed.Add(-2 * time.Hour))
	if r.ChargedSecs != 0 {
		t.Fatalf("отрицательный пропуск списал %d секунд", r.ChargedSecs)
	}
	if a.State().Today.UsedSeconds != 0 {
		t.Fatalf("учёт изменён: %d", a.State().Today.UsedSeconds)
	}
}

func TestPendingRecoveryChangesNothing(t *testing.T) {
	// Диагностика обязана быть безвредной: списать пропуск и не сохранить
	// состояние значило бы списать его второй раз при следующем запуске.
	killed := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	a := agentWith(t, state.State{LastSeenAt: killed})
	now := killed.Add(30 * time.Minute)

	r := a.PendingRecovery(now)
	if !r.Unclean || r.ChargedSecs != 1800 {
		t.Fatalf("диагностика посчитала неверно: %+v", r)
	}

	st := a.State()
	if st.Today.UsedSeconds != 0 || st.Today.Key != "" || st.UncleanStops != 0 {
		t.Fatalf("диагностика изменила состояние: %+v", st)
	}

	// А настоящее восстановление после неё обязано списать ровно столько же.
	applied := a.RecoverUnclean(now)
	if applied.ChargedSecs != r.ChargedSecs {
		t.Fatalf("списано %d, а обещано было %d", applied.ChargedSecs, r.ChargedSecs)
	}
	if a.State().Today.UsedSeconds != 1800 {
		t.Fatalf("настоящее списание не применилось: %d", a.State().Today.UsedSeconds)
	}
}

func TestPendingRecoveryQuietAfterCleanShutdown(t *testing.T) {
	stopped := time.Date(2026, 3, 8, 22, 0, 0, 0, time.UTC)
	a := agentWith(t, state.State{CleanShutdown: true, LastSeenAt: stopped})
	if r := a.PendingRecovery(stopped.Add(10 * time.Hour)); r.Unclean {
		t.Fatalf("штатная остановка показана как нештатная: %+v", r)
	}
}
