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
