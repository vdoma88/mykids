package usage

import (
	"testing"
	"time"

	"github.com/vdoma88/mykids/agents/windows/internal/schedule"
)

const day = "2026-03-09"

func newAcc() *Accountant {
	return New(2*time.Minute, []string{"explorer.exe", "MyKids-Agent.exe"}, 90*time.Second)
}

func TestFirstSampleSetsBaseline(t *testing.T) {
	a := newAcc()
	var d Day
	base := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	if got := a.Observe(Sample{At: base, Process: "game.exe"}, &d, day); got != 0 {
		t.Errorf("первый замер списал %d секунд, ожидалось 0", got)
	}
	if d.UsedSeconds != 0 {
		t.Errorf("израсходовано %d, ожидалось 0", d.UsedSeconds)
	}
}

func TestAccrualBetweenSamples(t *testing.T) {
	a := newAcc()
	var d Day
	base := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	a.Observe(Sample{At: base, Process: "game.exe"}, &d, day)
	got := a.Observe(Sample{At: base.Add(30 * time.Second), Process: "game.exe"}, &d, day)
	if got != 30 || d.UsedSeconds != 30 {
		t.Errorf("списано %d, накоплено %d; ожидалось 30/30", got, d.UsedSeconds)
	}
}

func TestIdleNotCounted(t *testing.T) {
	a := newAcc()
	var d Day
	base := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	a.Observe(Sample{At: base, Process: "game.exe"}, &d, day)
	got := a.Observe(Sample{At: base.Add(30 * time.Second), Process: "game.exe", Idle: 5 * time.Minute}, &d, day)
	if got != 0 || d.UsedSeconds != 0 {
		t.Errorf("простой засчитан: списано %d", got)
	}
}

func TestLockedSessionNotCounted(t *testing.T) {
	a := newAcc()
	var d Day
	base := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	a.Observe(Sample{At: base, Process: "game.exe"}, &d, day)
	if got := a.Observe(Sample{At: base.Add(time.Minute), Process: "game.exe", SessionLock: true}, &d, day); got != 0 {
		t.Errorf("заблокированная сессия засчитана: %d", got)
	}
}

func TestAllowlistedNotCounted(t *testing.T) {
	a := newAcc()
	var d Day
	base := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	a.Observe(Sample{At: base, Process: "explorer.exe"}, &d, day)
	// Регистр в имени процесса не должен влиять
	if got := a.Observe(Sample{At: base.Add(time.Minute), Process: "EXPLORER.EXE"}, &d, day); got != 0 {
		t.Errorf("процесс из белого списка засчитан: %d", got)
	}
	if !a.IsAllowlisted("mykids-agent.exe") {
		t.Error("сравнение имён должно игнорировать регистр")
	}
}

func TestLongGapClamped(t *testing.T) {
	// Ноутбук закрыли на два часа: списывать их целиком нельзя.
	a := newAcc()
	var d Day
	base := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	a.Observe(Sample{At: base, Process: "game.exe"}, &d, day)
	got := a.Observe(Sample{At: base.Add(2 * time.Hour), Process: "game.exe"}, &d, day)
	if got != 90 {
		t.Errorf("разрыв не ограничен: списано %d, ожидалось 90", got)
	}
}

func TestClockJumpBackwards(t *testing.T) {
	a := newAcc()
	var d Day
	base := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	a.Observe(Sample{At: base, Process: "game.exe"}, &d, day)
	if got := a.Observe(Sample{At: base.Add(-time.Hour), Process: "game.exe"}, &d, day); got != 0 {
		t.Errorf("перевод часов назад дал списание %d", got)
	}
	if d.UsedSeconds != 0 {
		t.Errorf("перевод часов назад изменил расход: %d", d.UsedSeconds)
	}
}

func TestDayRollover(t *testing.T) {
	a := newAcc()
	d := Day{Key: "2026-03-08", UsedSeconds: 3600, GrantSeconds: 3600}
	base := time.Date(2026, 3, 9, 0, 1, 0, 0, time.UTC)
	a.Observe(Sample{At: base, Process: "game.exe"}, &d, day)
	if d.Key != day || d.UsedSeconds != 0 || d.GrantSeconds != 0 {
		t.Errorf("смена суток не обнулила счётчики: %+v", d)
	}
}

func TestGrantIdempotent(t *testing.T) {
	d := Day{Key: day}
	Grant(&d, 60, 20)
	first := d.GrantSeconds
	Grant(&d, 60, 20)
	if first != 80*60 || d.GrantSeconds != first {
		t.Errorf("выдача не идемпотентна: %d затем %d", first, d.GrantSeconds)
	}
}

func TestGrantNeverShrinks(t *testing.T) {
	// Родитель снизил лимит посреди дня: уже выданное не отбираем.
	d := Day{Key: day, GrantSeconds: 7200}
	Grant(&d, 30, 0)
	if d.GrantSeconds != 7200 {
		t.Errorf("выдача уменьшилась до %d", d.GrantSeconds)
	}
}

func TestCarryOver(t *testing.T) {
	cases := []struct{ used, grant, max, want int }{
		{grant: 3600, used: 1800, max: 30, want: 30}, // остаток 30 мин, потолок 30
		{grant: 3600, used: 3000, max: 30, want: 10},
		{grant: 3600, used: 5400, max: 30, want: 0}, // ушёл в минус
		{grant: 3600, used: 0, max: 15, want: 15},
	}
	for _, c := range cases {
		got := CarryOver(Day{UsedSeconds: c.used, GrantSeconds: c.grant}, c.max)
		if got != c.want {
			t.Errorf("CarryOver(used=%d grant=%d max=%d) = %d, ожидалось %d", c.used, c.grant, c.max, got, c.want)
		}
	}
}

func TestDecide(t *testing.T) {
	windows := []schedule.Window{
		{Name: "отбой", Days: []int{1}, From: "21:30", To: "07:00", Mode: schedule.ModeBlocked},
		{Name: "уроки", Days: []int{1}, From: "16:00", To: "18:00", Mode: schedule.ModeTasksOnly},
	}
	full := Day{GrantSeconds: 3600}
	empty := Day{GrantSeconds: 3600, UsedSeconds: 3600}

	// Расписание важнее баланса
	if v := Decide(windows, schedule.Moment{Weekday: 1, MinutesOfDay: 22 * 60}, full, 0); v.Allow || v.Window != "отбой" {
		t.Errorf("отбой не сработал при полном балансе: %+v", v)
	}
	if v := Decide(windows, schedule.Moment{Weekday: 1, MinutesOfDay: 17 * 60}, full, 0); v.Allow || !v.TasksOnly {
		t.Errorf("окно заданий не распознано: %+v", v)
	}
	if v := Decide(windows, schedule.Moment{Weekday: 1, MinutesOfDay: 19 * 60}, full, 0); !v.Allow {
		t.Errorf("свободное время заблокировано: %+v", v)
	}
	if v := Decide(windows, schedule.Moment{Weekday: 1, MinutesOfDay: 19 * 60}, empty, 0); v.Allow {
		t.Errorf("нулевой остаток не заблокировал: %+v", v)
	}

	// Предупреждение перед концом лимита
	almost := Day{GrantSeconds: 3600, UsedSeconds: 3600 - 120}
	if v := Decide(windows, schedule.Moment{Weekday: 1, MinutesOfDay: 19 * 60}, almost, 5*time.Minute); !v.Allow || !v.WarnSoon {
		t.Errorf("не предупредили за 2 минуты до конца: %+v", v)
	}
}

func TestChargeGapMakesKillingAgentPointless(t *testing.T) {
	// Ребёнок снял агента на полчаса и играл. Пропуск обязан быть оплачен.
	day := Day{Key: "2026-09-18", GrantSeconds: 3600}
	charged := ChargeGap(&day, 30*time.Minute)

	if charged != 1800 {
		t.Fatalf("списано %d секунд вместо 1800", charged)
	}
	if day.UsedSeconds != 1800 || day.Remaining() != 1800 {
		t.Fatalf("учёт после списания: потрачено %d, осталось %d", day.UsedSeconds, day.Remaining())
	}
}

func TestChargeGapCappedByDailyGrant(t *testing.T) {
	// Агент не работал сутки. Обнулить сегодняшний день — да, уйти в долг на
	// неделю вперёд из-за одного сбоя питания — нет.
	day := Day{Key: "2026-09-18", GrantSeconds: 3600}
	charged := ChargeGap(&day, 24*time.Hour)

	if charged != 3600 {
		t.Fatalf("списано %d секунд вместо 3600", charged)
	}
	if day.Remaining() != 0 {
		t.Fatalf("остаток должен обнулиться, получено %d", day.Remaining())
	}
}

func TestChargeGapKeepsAlreadySpent(t *testing.T) {
	// Пропуск добавляется к уже потраченному, а не заменяет его.
	day := Day{Key: "2026-09-18", GrantSeconds: 3600, UsedSeconds: 600}
	if charged := ChargeGap(&day, 10*time.Minute); charged != 600 {
		t.Fatalf("списано %d вместо 600", charged)
	}
	if day.UsedSeconds != 1200 {
		t.Fatalf("потрачено %d вместо 1200", day.UsedSeconds)
	}
}

func TestChargeGapIgnoresNonPositive(t *testing.T) {
	// Часы могли прыгнуть назад: отрицательный пропуск не повод дарить время.
	for _, gap := range []time.Duration{0, -time.Hour} {
		day := Day{Key: "2026-09-18", GrantSeconds: 3600, UsedSeconds: 600}
		if charged := ChargeGap(&day, gap); charged != 0 {
			t.Fatalf("пропуск %v списал %d секунд", gap, charged)
		}
		if day.UsedSeconds != 600 {
			t.Fatalf("пропуск %v изменил учёт: %d", gap, day.UsedSeconds)
		}
	}
}

func TestChargeGapWithoutGrantChargesNothing(t *testing.T) {
	// Выдачи ещё не было — списывать не из чего, и уходить в минус незачем.
	day := Day{Key: "2026-09-18"}
	if charged := ChargeGap(&day, time.Hour); charged != 0 {
		t.Fatalf("списано %d при нулевой выдаче", charged)
	}
	if day.UsedSeconds != 0 {
		t.Fatalf("учёт изменён: %d", day.UsedSeconds)
	}
}

func TestFrequentSamplesDoNotLoseTime(t *testing.T) {
	// Помощник шлёт наблюдения каждую секунду, таймер службы тикает своим
	// чередом: промежутки выходят по полсекунды. Округление вниз превращало
	// час экрана в считанные минуты.
	acc := New(2*time.Minute, nil, time.Minute)
	day := Day{Key: "2026-09-19", GrantSeconds: 3600}
	base := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

	// Шестьдесят замеров по полсекунды — это ровно тридцать секунд.
	for i := 0; i <= 60; i++ {
		acc.Observe(Sample{At: base.Add(time.Duration(i) * 500 * time.Millisecond), Process: "game.exe"},
			&day, "2026-09-19")
	}
	if day.UsedSeconds != 30 {
		t.Fatalf("за 30 секунд списано %d — остаток теряется", day.UsedSeconds)
	}
}

func TestCarryDoesNotAccumulateWhileIdle(t *testing.T) {
	// Простой не списывается совсем: копить с него остаток значило бы
	// списывать время, которого не было.
	acc := New(10*time.Second, nil, time.Minute)
	day := Day{Key: "2026-09-19", GrantSeconds: 3600}
	base := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

	acc.Observe(Sample{At: base, Process: "game.exe"}, &day, "2026-09-19")
	for i := 1; i <= 60; i++ {
		acc.Observe(Sample{
			At:      base.Add(time.Duration(i) * 500 * time.Millisecond),
			Process: "game.exe", Idle: time.Minute,
		}, &day, "2026-09-19")
	}
	if day.UsedSeconds != 0 {
		t.Fatalf("простой списал %d секунд", day.UsedSeconds)
	}

	// И накопленного остатка после простоя быть не должно.
	acc.Observe(Sample{At: base.Add(31 * time.Second), Process: "game.exe"}, &day, "2026-09-19")
	if day.UsedSeconds > 1 {
		t.Fatalf("после простоя разом списано %d секунд", day.UsedSeconds)
	}
}

func TestCarryNeverExceedsOneSecond(t *testing.T) {
	// Остаток — это хвост, а не копилка: он не должен превращаться в скачок.
	acc := New(2*time.Minute, nil, time.Minute)
	day := Day{Key: "2026-09-19", GrantSeconds: 3600}
	base := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

	prev := 0
	for i := 0; i <= 100; i++ {
		acc.Observe(Sample{At: base.Add(time.Duration(i) * 100 * time.Millisecond), Process: "game.exe"},
			&day, "2026-09-19")
		if day.UsedSeconds-prev > 1 {
			t.Fatalf("замер %d списал %d секунд разом", i, day.UsedSeconds-prev)
		}
		prev = day.UsedSeconds
	}
	if day.UsedSeconds != 10 {
		t.Fatalf("за 10 секунд списано %d", day.UsedSeconds)
	}
}
