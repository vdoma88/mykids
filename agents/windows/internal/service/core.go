// Package service — то, что делает служба: учёт, политика, связь с сервером,
// состояние на диске.
//
// Вынесено из команды отдельно, потому что этот цикл теперь нужен трижды: под
// службой Windows, в консоли для отладки и в однопроцессном режиме. Три копии
// разошлись бы, и разошлись бы именно в мелочах, которые тут и важны, —
// что сохранять при остановке и в каком порядке.
package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/vdoma88/mykids/agents/windows/internal/agent"
	"github.com/vdoma88/mykids/agents/windows/internal/clock"
	"github.com/vdoma88/mykids/agents/windows/internal/config"
	"github.com/vdoma88/mykids/agents/windows/internal/ipc"
	"github.com/vdoma88/mykids/agents/windows/internal/link"
	"github.com/vdoma88/mykids/agents/windows/internal/screen"
	"github.com/vdoma88/mykids/agents/windows/internal/state"
	"github.com/vdoma88/mykids/agents/windows/internal/usage"
	"github.com/vdoma88/mykids/agents/windows/internal/watchdog"
)

// Intervals — как часто что делать.
type Intervals struct {
	// Tick — опрос рабочего стола.
	Tick time.Duration
	// Sync — обмен с сервером. Реже опроса: расписание меняется редко,
	// а расход копится в очереди.
	Sync time.Duration
	// Save — запись состояния. Между сохранениями теряется не больше этого
	// времени, и по нему же следующий запуск считает пропуск.
	Save time.Duration
	// Watch — проверка, жив ли помощник. Чаще опроса нет смысла: раньше
	// следующего замера его отсутствие всё равно ни на что не влияет.
	Watch time.Duration
}

// DefaultIntervals — значения для боя.
func DefaultIntervals() Intervals {
	return Intervals{
		Tick: 5 * time.Second, Sync: time.Minute,
		Save: 30 * time.Second, Watch: 5 * time.Second,
	}
}

// Options — из чего собирается ядро.
type Options struct {
	Agent  *agent.Agent
	Link   *link.Link
	Clock  *clock.Clock
	Source clock.Source
	// StatePath — куда писать состояние.
	StatePath string
	// LocalPolicy — запасная политика, если сервера не видели ни разу.
	LocalPolicy config.Policy
	// Log — куда сообщать. Под службой это журнал Windows, в консоли — stderr.
	Log func(format string, args ...any)
}

// Core связывает всё это под одним замком.
//
// Замок не формальность: наблюдения приходят из потока соединения с помощником,
// а тики — из таймера, и оба трогают учёт.
type Core struct {
	mu   sync.Mutex
	opt  Options
	last usage.Verdict
	// Подкрутки считаем от значения, с которым запустились: clock.Tampers()
	// растёт за весь запуск, и прибавлять его при каждом сохранении значило бы
	// считать одни и те же сдвиги снова и снова.
	tampersAtStart int
	// ctx — то, что ребёнок видит на закрытом экране: остатки, курс, адрес.
	// Обновляется при обмене с сервером; без связи остаются прошлые значения,
	// и это лучше пустоты — вчерашний остаток кредитов всё ещё ориентир.
	ctx screen.Context
	// watch — сторож помощника. Пусто, если службе некого поднимать
	// (одиночные режимы и тесты).
	watch *watchdog.Watchdog
}

// SetHelpers задаёт, кем поднимать помощника.
func (c *Core) SetHelpers(d watchdog.Desktop) { c.watch = watchdog.New(d) }

// Context — что служба знает про остатки ребёнка прямо сейчас.
func (c *Core) Context() screen.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ctx
}

// SetChildURL задаёт адрес, куда ребёнку идти за заданиями.
func (c *Core) SetChildURL(url string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ctx.ChildURL = url
}

// New собирает ядро.
func New(o Options) *Core {
	if o.Log == nil {
		o.Log = func(string, ...any) {}
	}
	if o.Source == nil {
		o.Source = clock.System()
	}
	return &Core{opt: o, tampersAtStart: o.Agent.State().ClockTampers}
}

// Verdict — последнее решение. Его показывает помощник.
func (c *Core) Verdict() usage.Verdict {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

// Tick — один шаг учёта. Безопасен для вызова из нескольких потоков.
func (c *Core) Tick() usage.Verdict {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tick()
}

func (c *Core) tick() usage.Verdict {
	r := c.opt.Source()
	if jump, tampered := c.opt.Link.Observe(r); tampered {
		c.opt.Log("внимание: %s — учёт продолжается по исправленному времени", jump)
	}

	now := c.opt.Clock.Now(r)
	v, err := c.opt.Agent.Tick(now)
	if err != nil {
		// Наблюдать не получилось. Решение не обновляем: показывать помощнику
		// нечего, а прежнее остаётся в силе.
		c.opt.Log("тик: %v", err)
		return c.last
	}
	if err := c.opt.Link.Record(v.ConsumedSecs, now); err != nil {
		c.opt.Log("%v", err)
	}
	c.last = v
	return v
}

// Sync обменивается с сервером и применяет присланную политику.
func (c *Core) Sync(ctx context.Context) link.Result {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sync(ctx)
}

func (c *Core) sync(ctx context.Context) link.Result {
	res, err := c.opt.Link.Sync(ctx, c.opt.Source(), c.opt.LocalPolicy)
	if err != nil {
		c.opt.Log("обмен с сервером: %v", err)
	}
	changed, setErr := c.opt.Agent.SetPolicy(res.Policy)
	if setErr != nil {
		c.opt.Log("политика отклонена: %v", setErr)
		return res
	}
	if changed {
		c.opt.Log("политика обновлена (%s)", res.Source)
	}

	if res.Online {
		c.ctx.Minutes, c.ctx.Credits = res.Balances.Minutes, res.Balances.Credits
	}
	// Курс запоминаем и не сбрасываем без связи: политика остаётся в силе,
	// и пересчёт кредитов в минуты — тоже.
	if res.CreditsPerMinute > 0 {
		c.ctx.CreditsPerMinute = res.CreditsPerMinute
	}
	c.ctx.TomorrowMinutes = c.opt.Agent.TomorrowLimit(time.Now())
	return res
}

// Save пишет состояние. clean=true означает штатное завершение: следующий
// запуск не будет считать это пропуском и не станет его оплачивать.
func (c *Core) Save(clean bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.save(clean)
}

func (c *Core) save(clean bool) {
	st := c.opt.Agent.State()
	st.CleanShutdown = clean
	// Остаток секунд, очередь сообщений и поправка часов обязаны пережить
	// перезапуск: иначе достаточно убивать агента, чтобы копить время даром.
	st.PendingSeconds = c.opt.Link.Pending()
	st.PendingTampers = c.opt.Link.Tampers()
	// Метка времени нужна следующему запуску: по ней считается пропуск,
	// если этот закончится не штатно.
	st.LastSeenAt = c.opt.Clock.Now(c.opt.Source())
	st.ClockOffsetSeconds = int(c.opt.Clock.Offset().Seconds())
	st.ClockTrusted = c.opt.Clock.Trusted()
	st.ClockTampers = c.tampersAtStart + c.opt.Clock.Tampers()

	if err := state.Save(c.opt.StatePath, st); err != nil {
		c.opt.Log("не удалось сохранить состояние: %v", err)
	}
}

// Recover разбирает последствия нештатной остановки: оплачивает пропуск и
// ставит сообщение родителю в очередь.
//
// Пропуск уходит в ту же очередь расхода, что и обычное время: иначе списание
// осталось бы только в локальном файле, который ребёнок может удалить.
func (c *Core) Recover() agent.Recovery {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.recover()
}

func (c *Core) recover() agent.Recovery {
	now := c.opt.Clock.Now(c.opt.Source())
	r := c.opt.Agent.RecoverUnclean(now)
	if !r.Unclean {
		return r
	}
	if r.ChargedSecs > 0 {
		if err := c.opt.Link.Record(r.ChargedSecs, now); err != nil {
			c.opt.Log("%v", err)
		}
	}
	c.opt.Link.QueueTamper("unclean_stop", UncleanDetail(r), now)
	return r
}

// Watch поднимает помощника, если его нет, и сообщает родителю, если его
// снимают раз за разом.
//
// Без помощника служба слепа: окон не видит, оверлей рисовать некому. Ждать,
// пока помощника запустит кто-то другой, не приходится — запускать его,
// кроме службы, некому.
func (c *Core) Watch(now time.Time) {
	if c.watch == nil {
		return
	}
	a := c.watch.Check(now)
	switch {
	case a.Err != nil:
		c.opt.Log("помощник не запустился: %v", a.Err)
	case a.Started:
		c.opt.Log("помощник запущен в сессии пользователя")
	}
	if a.Tamper {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.opt.Log("внимание: %s", a.Detail)
		c.opt.Link.QueueTamper("helper_killed", a.Detail, c.opt.Clock.Now(c.opt.Source()))
	}
}

// ReportLie сообщает родителю, что помощника поймали на лжи, и закрывает
// экран до следующего решения.
//
// Отдельным событием, а не строкой в журнале: без службы, которая расскажет,
// родитель никогда не узнает, что наблюдение подменили.
func (c *Core) ReportLie(detail string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.opt.Log("внимание: %s", detail)
	c.opt.Link.QueueTamper("helper_lied", detail, c.opt.Clock.Now(c.opt.Source()))
}

// UncleanDetail — что увидит родитель. Списанные минуты названы прямо: отличить
// сбой питания от снятия агента нельзя, и родителю может понадобиться вернуть
// время ручной корректировкой ровно на эту величину.
func UncleanDetail(r agent.Recovery) string {
	if r.ChargedSecs == 0 {
		return fmt.Sprintf("агент не работал %s, списывать было нечего", r.Gap.Round(time.Minute))
	}
	return fmt.Sprintf("агент не работал %s, списано %d мин",
		r.Gap.Round(time.Minute), r.ChargedSecs/60)
}

// Run ведёт цикл до отмены контекста.
//
// Порядок в начале важен и потому закреплён здесь, а не в вызывающем:
// сначала политика — от дневной выдачи считается ограничение списания за
// пропуск, и по локальной политике оно вышло бы не тем, что задал родитель.
func (c *Core) Run(ctx context.Context, iv Intervals) error {
	c.mu.Lock()
	c.sync(ctx)
	rec := c.recover()
	if rec.Unclean {
		c.opt.Log("внимание: %s", UncleanDetail(rec))
	}
	// Сохраняемся сразу: убийство агента в первую же минуту не должно стирать
	// ни списание, ни сообщение родителю.
	c.save(false)
	if rec.Unclean {
		// И сразу отдаём серверу. Ждать минуту до следующего обмена значит
		// оставить родителя в неведении ровно тогда, когда это важнее всего.
		c.sync(ctx)
	}
	c.mu.Unlock()

	tick := time.NewTicker(iv.Tick)
	defer tick.Stop()
	sync := time.NewTicker(iv.Sync)
	defer sync.Stop()
	save := time.NewTicker(iv.Save)
	defer save.Stop()
	// Нулевой период означает «не сторожить»: так собраны одиночные режимы
	// и тесты, и падать на этом циклу незачем. Чтение из нулевого канала
	// просто никогда не сработает.
	var watch <-chan time.Time
	if iv.Watch > 0 {
		t := time.NewTicker(iv.Watch)
		defer t.Stop()
		watch = t.C
		// Помощника поднимаем сразу, не дожидаясь первого срабатывания:
		// иначе после перезагрузки экран ребёнка несколько секунд живёт
		// сам по себе.
		c.Watch(time.Now())
	}

	for {
		select {
		case <-ctx.Done():
			// Штатная остановка: пропуск до следующего запуска оплачивать
			// не за что.
			c.Save(true)
			return nil
		case <-tick.C:
			c.Tick()
		case <-sync.C:
			c.Sync(ctx)
		case <-save.C:
			c.Save(false)
		case <-watch:
			c.Watch(time.Now())
		}
	}
}

// Observer — куда складывать наблюдения помощника. Реализует ipc.Desktop.
type Observer interface {
	Update(ipc.Sample)
}

// Handler делает обработчик наблюдений для одного соединения с помощником.
//
// Здесь собрано всё, что служба делает с тем, что ей сообщили: ловит на лжи,
// правит недостоверное, кладёт в учёт и возвращает решение. Вынесено из
// команды, потому что это логика, а не проводка, и её нужно проверять.
//
// Своя проверка на каждое соединение: перезапущенный помощник начинает с
// чистой репутацией, а подменённый не наследует чужую.
//
// own — собственный источник состояния экрана. Пустой означает, что своего
// мнения о блокировке у службы нет и всё решает помощник; так работает всё,
// что не Windows.
func (c *Core) Handler(remote Observer, own ipc.LockSource, idleThreshold time.Duration, now func() time.Time) func(ipc.Sample) ipc.Verdict {
	if now == nil {
		now = time.Now
	}
	var scrutiny Scrutiny

	return func(s ipc.Sample) ipc.Verdict {
		at := now()
		lock := ipc.LockUnknown
		if own != nil {
			lock = own.SessionLock()
		}
		if f := scrutiny.Observe(s, lock, idleThreshold, at); f.Lying {
			c.ReportLie(f.Detail)
		}
		remote.Update(scrutiny.Correct(s, at))
		// Тик здесь, а не только по таймеру: иначе помощник несколько секунд
		// показывал бы уже отменённое решение.
		return Verdict(c.Tick(), c.Context())
	}
}

// Verdict переводит решение учёта в то, что уходит помощнику.
//
// Текст готовит служба: помощнику незачем знать правила, а подменённому
// помощнику незачем давать сочинять надпись ребёнку.
func Verdict(v usage.Verdict, c screen.Context) ipc.Verdict {
	return ipc.Verdict{
		Allow:    v.Allow,
		Reason:   v.Reason,
		Window:   v.Window,
		LeftSecs: v.LeftSecs,
		WarnSoon: v.WarnSoon,
		Screen:   screen.Build(v, c),
		Tray:     screen.BuildTray(v, c),
	}
}
