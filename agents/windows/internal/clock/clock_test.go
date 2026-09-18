package clock

import (
	"strings"
	"testing"
	"time"
)

// fake — часы, которыми тест управляет: настенное время и монотонный счётчик
// двигаются независимо, как при переводе часов.
type fake struct {
	wall time.Time
	mono time.Duration
}

func newFake() *fake {
	return &fake{wall: time.Date(2026, 9, 18, 20, 0, 0, 0, time.UTC)}
}

func (f *fake) read() Reading { return Reading{Wall: f.wall, Mono: f.mono} }

// tick — время идёт нормально: оба источника растут одинаково.
func (f *fake) tick(d time.Duration) Reading {
	f.wall = f.wall.Add(d)
	f.mono += d
	return f.read()
}

// shift — часы переставили: настенное прыгнуло, счётчик нет.
func (f *fake) shift(d time.Duration) Reading {
	f.wall = f.wall.Add(d)
	return f.read()
}

func TestNormalFlowIsNotTamper(t *testing.T) {
	c := New(0)
	f := newFake()
	c.Observe(f.read())
	for i := 0; i < 20; i++ {
		if _, tampered := c.Observe(f.tick(5 * time.Second)); tampered {
			t.Fatalf("шаг %d: ровный ход принят за подкрутку", i)
		}
	}
	if c.Offset() != 0 || c.Tampers() != 0 {
		t.Fatalf("поправка появилась из ниоткуда: %v, подкруток %d", c.Offset(), c.Tampers())
	}
}

func TestFirstReadingIsNeverTamper(t *testing.T) {
	// Сравнивать не с чем: сообщать о подкрутке на первом замере нельзя.
	f := newFake()
	if _, tampered := New(0).Observe(f.read()); tampered {
		t.Fatal("первый замер не может быть подкруткой")
	}
}

func TestNtpDriftIsTolerated(t *testing.T) {
	c := New(0)
	f := newFake()
	c.Observe(f.read())
	f.tick(5 * time.Second)
	// Поправка NTP на несколько секунд — обычное дело, не вмешательство.
	if _, tampered := c.Observe(f.shift(20 * time.Second)); tampered {
		t.Fatal("поправка NTP принята за подкрутку")
	}
	if c.Offset() != 0 {
		t.Fatalf("мелкий дрейф не должен менять поправку: %v", c.Offset())
	}
}

func TestClockBackIsCaughtAndCompensated(t *testing.T) {
	c := New(0)
	f := newFake()
	c.Observe(f.read())
	f.tick(time.Minute)
	before := c.Now(f.read())

	// Ребёнок переводит часы на три часа назад, чтобы уйти из окна «отбой».
	jump, tampered := c.Observe(f.shift(-3 * time.Hour))
	if !tampered {
		t.Fatal("перевод на три часа назад не замечен")
	}
	if jump.Delta > -3*time.Hour+time.Second || jump.Delta < -3*time.Hour-time.Second {
		t.Fatalf("сдвиг измерен неверно: %v", jump.Delta)
	}
	if c.Tampers() != 1 {
		t.Fatalf("подкрутка не посчитана: %d", c.Tampers())
	}

	// Исправленное время не должно откатиться назад.
	after := c.Now(f.read())
	if !after.Equal(before) {
		t.Fatalf("исправленное время уехало: было %v, стало %v", before, after)
	}
	if !after.After(time.Date(2026, 9, 18, 20, 0, 0, 0, time.UTC)) {
		t.Fatalf("исправленное время откатилось в прошлое: %v", after)
	}
}

func TestClockForwardIsCaughtAndCompensated(t *testing.T) {
	// Перевод вперёд — способ проскочить окно «отбой» или дождаться нового
	// дневного лимита раньше срока.
	c := New(0)
	f := newFake()
	c.Observe(f.read())
	f.tick(time.Minute)
	before := c.Now(f.read())

	jump, tampered := c.Observe(f.shift(6 * time.Hour))
	if !tampered || jump.Delta < 6*time.Hour-time.Second {
		t.Fatalf("перевод вперёд не замечен: %+v", jump)
	}
	if got := c.Now(f.read()); !got.Equal(before) {
		t.Fatalf("исправленное время скакнуло вперёд: %v вместо %v", got, before)
	}
}

func TestTimeKeepsFlowingAfterTamper(t *testing.T) {
	c := New(0)
	f := newFake()
	c.Observe(f.read())
	c.Observe(f.shift(-2 * time.Hour))
	at := c.Now(f.read())

	// После компенсации время обязано идти дальше обычным ходом.
	c.Observe(f.tick(10 * time.Minute))
	if got := c.Now(f.read()); !got.Equal(at.Add(10 * time.Minute)) {
		t.Fatalf("после компенсации время идёт неверно: %v вместо %v", got, at.Add(10*time.Minute))
	}
}

func TestRepeatedTampersAccumulate(t *testing.T) {
	c := New(0)
	f := newFake()
	c.Observe(f.read())
	start := c.Now(f.read())

	// Часы крутят туда-сюда — каждый сдвиг должен гаситься.
	for _, d := range []time.Duration{-time.Hour, -time.Hour, 30 * time.Minute} {
		if _, tampered := c.Observe(f.shift(d)); !tampered {
			t.Fatalf("сдвиг %v не замечен", d)
		}
	}
	if c.Tampers() != 3 {
		t.Fatalf("подкрутки не посчитаны: %d", c.Tampers())
	}
	if got := c.Now(f.read()); !got.Equal(start) {
		t.Fatalf("накопленные сдвиги не погашены: %v вместо %v", got, start)
	}
}

func TestSyncTakesServerTimeAsTruth(t *testing.T) {
	c := New(0)
	f := newFake()
	c.Observe(f.read())

	// Часы машины отстают на час, и агент об этом не знал: сдвиг произошёл
	// до запуска, монотонный счётчик его не видел.
	server := f.wall.Add(time.Hour)
	corrected := c.Sync(f.read(), server)

	if corrected != time.Hour {
		t.Fatalf("поправка посчитана неверно: %v", corrected)
	}
	if !c.Trusted() {
		t.Fatal("после обмена с сервером поправка должна считаться достоверной")
	}
	if got := c.Now(f.read()); !got.Equal(server) {
		t.Fatalf("исправленное время не равно серверному: %v вместо %v", got, server)
	}
	if !c.Suspicious() {
		t.Fatal("часовое расхождение с сервером должно быть отмечено")
	}
}

func TestSyncOnAccurateClockIsQuiet(t *testing.T) {
	c := New(0)
	f := newFake()
	// Ответ сервера пришёл с сетевой задержкой в доли секунды.
	c.Sync(f.read(), f.wall.Add(300*time.Millisecond))
	if c.Suspicious() {
		t.Fatalf("сетевая задержка принята за подкрутку: поправка %v", c.Offset())
	}
	if !c.Trusted() {
		t.Fatal("поправка должна быть достоверной")
	}
}

func TestSyncReplacesEarlierOffset(t *testing.T) {
	c := New(0)
	f := newFake()
	c.Observe(f.read())
	c.Observe(f.shift(-2 * time.Hour))
	if c.Offset() != 2*time.Hour {
		t.Fatalf("компенсация не накопилась: %v", c.Offset())
	}

	// Связь восстановилась: истина у сервера, старая компенсация отбрасывается.
	server := f.wall.Add(90 * time.Minute)
	c.Sync(f.read(), server)
	if c.Offset() != 90*time.Minute {
		t.Fatalf("поправка сервера не заменила локальную: %v", c.Offset())
	}
	if got := c.Now(f.read()); !got.Equal(server) {
		t.Fatalf("время не совпало с серверным: %v", got)
	}
}

func TestSyncDoesNotReportPhantomJump(t *testing.T) {
	// Sync сдвигает поправку, а не часы: следующий Observe не должен принять
	// это за подкрутку.
	c := New(0)
	f := newFake()
	c.Observe(f.read())
	c.Sync(f.read(), f.wall.Add(3*time.Hour))
	if _, tampered := c.Observe(f.tick(5 * time.Second)); tampered {
		t.Fatal("собственная поправка принята за подкрутку")
	}
}

func TestTamperDropsTrust(t *testing.T) {
	c := New(0)
	f := newFake()
	c.Sync(f.read(), f.wall)
	if !c.Trusted() {
		t.Fatal("после обмена поправка достоверна")
	}
	c.Observe(f.shift(-time.Hour))
	if c.Trusted() {
		t.Fatal("после перевода часов поправка сервера больше не достоверна")
	}
}

func TestSetOffsetSurvivesRestart(t *testing.T) {
	// Иначе перевод часов достаточно было бы дополнить убийством процесса.
	c := New(0)
	f := newFake()
	c.SetOffset(2*time.Hour, true)
	if !c.Trusted() || c.Offset() != 2*time.Hour {
		t.Fatalf("поправка не восстановлена: %v, достоверна=%v", c.Offset(), c.Trusted())
	}
	if got := c.Now(f.read()); !got.Equal(f.wall.Add(2 * time.Hour)) {
		t.Fatalf("восстановленная поправка не применяется: %v", got)
	}
}

func TestThresholdIsRespected(t *testing.T) {
	c := New(time.Hour)
	f := newFake()
	c.Observe(f.read())
	if _, tampered := c.Observe(f.shift(30 * time.Minute)); tampered {
		t.Fatal("сдвиг ниже порога не должен срабатывать")
	}
	if _, tampered := c.Observe(f.shift(90 * time.Minute)); !tampered {
		t.Fatal("сдвиг выше порога должен срабатывать")
	}
}

func TestZeroThresholdMeansDefault(t *testing.T) {
	c := New(0)
	f := newFake()
	c.Observe(f.read())
	// Ровно на пороге срабатывать не обязано, но секундой позже — обязано.
	if _, tampered := c.Observe(f.shift(DefaultThreshold + time.Second)); !tampered {
		t.Fatal("нулевой порог должен означать стандартный, а не «ловить всё»")
	}
}

func TestJumpString(t *testing.T) {
	back := Jump{Delta: -3 * time.Hour}.String()
	if !strings.Contains(back, "назад") || !strings.Contains(back, "3h0m0s") {
		t.Fatalf("сообщение о переводе назад: %q", back)
	}
	fwd := Jump{Delta: 90 * time.Minute}.String()
	if !strings.Contains(fwd, "вперёд") {
		t.Fatalf("сообщение о переводе вперёд: %q", fwd)
	}
}

func TestSystemSourceStripsMonotonicFromWall(t *testing.T) {
	// Wall обязан быть чистым настенным временем: с монотонным хвостом
	// сравнение двух замеров молча пошло бы по монотонным часам, и подкрутка
	// стала бы невидимой.
	src := System()
	a, b := src(), src()
	if a.Wall.Round(0) != a.Wall {
		t.Fatal("Wall несёт монотонное показание")
	}
	if b.Mono < a.Mono {
		t.Fatalf("монотонный счётчик пошёл назад: %v -> %v", a.Mono, b.Mono)
	}
}
