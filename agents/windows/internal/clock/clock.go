// Package clock ловит подкрутку системных часов и держит поправку к ним.
//
// Расписание живёт по настенным часам: «отбой с 21:30» — это про то, что
// показывают часы. Переведя их на три часа назад, ребёнок открыл бы уже
// закрытое окно. Поэтому агент не доверяет системному времени напрямую, а
// держит к нему поправку: на сервере её задаёт ответ /agent/sync, без сети —
// монотонный счётчик, который перевод часов не сдвигает.
package clock

import (
	"fmt"
	"time"
)

// Reading — один замер: настенные часы и независимый от них счётчик.
//
// Два источника разведены намеренно. Настенное время можно переставить,
// монотонное — нет: оно считает от запуска машины и назад не идёт.
type Reading struct {
	Wall time.Time
	Mono time.Duration
}

// Source выдаёт замеры. Отдельный тип нужен, чтобы тесты могли двигать часы
// и счётчик независимо друг от друга — именно так и выглядит подкрутка.
type Source func() Reading

// System — настоящие часы машины.
func System() Source {
	start := time.Now()
	return func() Reading {
		now := time.Now()
		// now.Sub(start) считается по монотонному показанию: перевод часов
		// на него не влияет. Round(0) у Wall это показание отбрасывает,
		// оставляя именно то, что покажет пользователю проводник.
		return Reading{Wall: now.Round(0), Mono: now.Sub(start)}
	}
}

// DefaultThreshold — с какого расхождения считаем, что часы переставили.
// Меньше — ложные срабатывания на поправках NTP и торможении виртуалки.
const DefaultThreshold = 2 * time.Minute

// Jump — обнаруженный сдвиг часов.
type Jump struct {
	// Delta — насколько часы уехали. Отрицательная — перевели назад.
	Delta time.Duration
	At    time.Time
}

func (j Jump) String() string {
	dir := "вперёд"
	d := j.Delta
	if d < 0 {
		dir, d = "назад", -d
	}
	return fmt.Sprintf("системные часы переведены %s на %s", dir, d.Round(time.Second))
}

// Clock — поправка к системным часам и счётчик подкруток.
type Clock struct {
	threshold time.Duration

	offset  time.Duration
	trusted bool

	last    Reading
	started bool

	tampers int
}

// New создаёт часы с порогом срабатывания. Нулевой порог означает стандартный.
func New(threshold time.Duration) *Clock {
	if threshold <= 0 {
		threshold = DefaultThreshold
	}
	return &Clock{threshold: threshold}
}

// Offset — накопленная поправка к системному времени.
func (c *Clock) Offset() time.Duration { return c.offset }

// SetOffset восстанавливает поправку из сохранённого состояния.
//
// Без этого перезапуск агента обнулял бы поправку, и перевод часов достаточно
// было бы дополнить убийством процесса.
func (c *Clock) SetOffset(d time.Duration, trusted bool) {
	c.offset, c.trusted = d, trusted
}

// Trusted сообщает, получена ли поправка от сервера. Без сети поправка лишь
// компенсирует замеченные сдвиги и не знает настоящего времени.
func (c *Clock) Trusted() bool { return c.trusted }

// Tampers — сколько раз часы переставляли за время работы.
func (c *Clock) Tampers() int { return c.tampers }

// Now — исправленное время по замеру.
func (c *Clock) Now(r Reading) time.Time { return r.Wall.Add(c.offset) }

// Observe принимает очередной замер и сообщает о подкрутке.
//
// Сдвиг гасится поправкой: исправленное время продолжает идти ровно, как шло
// до перевода. Иначе перевод часов на час назад дал бы час экрана в уже
// закрытом окне.
func (c *Clock) Observe(r Reading) (Jump, bool) {
	if !c.started {
		c.last, c.started = r, true
		return Jump{}, false
	}
	prev := c.last
	c.last = r

	elapsedMono := r.Mono - prev.Mono
	elapsedWall := r.Wall.Sub(prev.Wall)
	drift := elapsedWall - elapsedMono

	if drift > -c.threshold && drift < c.threshold {
		return Jump{}, false
	}

	// Поправка идёт в минус ровно на величину сдвига: исправленное время в
	// этот момент равно тому, каким оно было бы без перевода.
	c.offset -= drift
	c.tampers++
	// Поправка сервера этим сдвигом испорчена: она была привязана к прежним
	// показаниям часов. До следующей связи считаем её лишь компенсацией.
	c.trusted = false
	return Jump{Delta: drift, At: c.Now(r)}, true
}

// Sync принимает время сервера как истину и пересчитывает поправку.
//
// Сеть добавляет к ответу задержку, но она измеряется миллисекундами, а
// решения принимаются по минутам — учитывать её незачем.
func (c *Clock) Sync(r Reading, serverTime time.Time) (corrected time.Duration) {
	want := serverTime.Sub(r.Wall)
	corrected = want - c.offset
	c.offset = want
	c.trusted = true
	c.last = r
	c.started = true
	return corrected
}

// Suspicious сообщает, что расхождение с сервером слишком велико, чтобы быть
// естественным. Родителю это стоит показать: часы могли переставить до того,
// как агент успел сделать первый замер.
func (c *Clock) Suspicious() bool {
	d := c.offset
	if d < 0 {
		d = -d
	}
	return d >= c.threshold
}
