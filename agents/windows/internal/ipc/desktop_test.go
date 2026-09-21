package ipc

import (
	"testing"
	"time"
)

// --- Собственный источник состояния экрана ---

// fixedLock — источник, который всегда отвечает одно и то же.
type fixedLock LockState

func (f fixedLock) SessionLock() LockState { return LockState(f) }

func TestOwnLockOutranksTheHelper(t *testing.T) {
	// Служба спросила Windows сама. Помощнику тут возражать нечем: подменить
	// ответ диспетчера сессий из-под учётной записи ребёнка нельзя.
	now := time.Now
	for _, c := range []struct {
		name     string
		own      LockState
		claimed  bool
		expected bool
	}{
		{"Windows отменяет выдуманную блокировку", LockOff, true, false},
		{"Windows подтверждает блокировку", LockOn, true, true},
		{"Windows видит блокировку, о которой помощник умолчал", LockOn, false, true},
		{"Windows видит открытый экран", LockOff, false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			d := NewDesktop(time.Minute, now)
			d.SetLockSource(fixedLock(c.own))
			d.Update(Sample{Process: "game.exe", SessionLocked: c.claimed})
			if got := d.SessionLocked(); got != c.expected {
				t.Fatalf("экран заблокирован: %v, ожидалось %v", got, c.expected)
			}
		})
	}
}

func TestLockedScreenSurvivesASilentHelper(t *testing.T) {
	// Ради этого всё и затевалось не меньше, чем ради ловли лжи. Помощник
	// перезапускается сам по себе — за этим следит сторож, — и ребёнок,
	// который заблокировал экран и ушёл, платил за эти полминуты ни за что.
	tick := time.Now()
	d := NewDesktop(time.Second, func() time.Time { return tick })
	d.SetLockSource(fixedLock(LockOn))
	d.Update(Sample{Process: "game.exe", SessionLocked: true})

	tick = tick.Add(time.Minute) // помощника давно не слышно
	if !d.Silent() {
		t.Fatal("помощник почему-то не считается пропавшим")
	}
	if !d.SessionLocked() {
		t.Fatal("заблокированный экран забыт вместе с помощником — ребёнок платит за то, чего не делает")
	}
}

func TestSilenceStillCostsWhenNobodyCanBeAsked(t *testing.T) {
	// А вот без своего источника правило прежнее и остаётся прежним: снять
	// наблюдателя не должно быть способом получить бесплатное время.
	tick := time.Now()
	d := NewDesktop(time.Second, func() time.Time { return tick })
	d.Update(Sample{Process: "game.exe", SessionLocked: true})

	tick = tick.Add(time.Minute)
	if d.SessionLocked() {
		t.Fatal("молчание помощника сошло за заблокированный экран")
	}
}

func TestUnknownOwnLockLeavesTheHelpersWord(t *testing.T) {
	d := NewDesktop(time.Minute, time.Now)
	d.SetLockSource(fixedLock(LockUnknown))
	d.Update(Sample{Process: "game.exe", SessionLocked: true})
	if !d.SessionLocked() {
		t.Fatal("неудачный вызов отнял у помощника слово")
	}
}

func TestNoLockSourceIsSafe(t *testing.T) {
	// Источника нет вовсе — так собирается всё, что не Windows.
	d := NewDesktop(time.Minute, time.Now)
	d.Update(Sample{Process: "game.exe", SessionLocked: true})
	if !d.SessionLocked() {
		t.Fatal("без источника потеряли слово помощника")
	}
}
