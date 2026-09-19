// Package link связывает агента с сервером: политика, очередь расхода, часы.
//
// Весь офлайн собран здесь, потому что асимметрия у него одна и её легко
// нарушить по частям: расход уходит на сервер постфактум и принимается,
// начисления агент не делает вовсе, а политика без связи берётся из кэша, а не
// из локального файла, который ребёнку доступен на запись.
package link

import (
	"context"
	"fmt"
	"time"

	"github.com/vdoma88/mykids/agents/windows/internal/client"
	"github.com/vdoma88/mykids/agents/windows/internal/clock"
	"github.com/vdoma88/mykids/agents/windows/internal/config"
	"github.com/vdoma88/mykids/agents/windows/internal/outbox"
	"github.com/vdoma88/mykids/agents/windows/internal/policy"
)

// MaxQueue — сколько записей расхода держим, если сервера нет неделями.
// При минутном шаге это больше суток непрерывного экрана.
const MaxQueue = 2000

// MaxTampers — сколько сообщений о вмешательстве копим без связи. Больше и не
// нужно: родителю важен факт и первые случаи, а не сотый подряд.
const MaxTampers = 50

// Link — состояние связи с сервером.
type Link struct {
	client    *client.Client
	box       *outbox.Outbox
	clk       *clock.Clock
	cachePath string

	// Секунды, не набравшие полной минуты. В очередь уходят только целые
	// минуты: сервер считает минутами, а дробить их значит копить ошибку
	// округления в пользу ребёнка.
	pending int

	// Сообщения о вмешательстве, ещё не принятые сервером.
	tampers []client.TamperEvent
}

// New собирает связь.
func New(c *client.Client, box *outbox.Outbox, clk *clock.Clock, cachePath string) *Link {
	return &Link{client: c, box: box, clk: clk, cachePath: cachePath}
}

// Pending — остаток секунд, не отправленный на сервер.
func (l *Link) Pending() int { return l.pending }

// SetPending восстанавливает остаток из сохранённого состояния.
func (l *Link) SetPending(seconds int) {
	if seconds < 0 {
		seconds = 0
	}
	l.pending = seconds
}

// Queued — сколько минут ждёт отправки.
func (l *Link) Queued() int { return l.box.Total() }

// Tampers возвращает копию очереди сообщений о вмешательстве — её вызывающий
// кладёт в файл состояния.
func (l *Link) Tampers() []client.TamperEvent {
	out := make([]client.TamperEvent, len(l.tampers))
	copy(out, l.tampers)
	return out
}

// SetTampers восстанавливает очередь из сохранённого состояния.
func (l *Link) SetTampers(events []client.TamperEvent) {
	l.tampers = append(l.tampers[:0], events...)
	l.trimTampers()
}

// QueueTamper ставит сообщение в очередь. Отправка — в ближайшем Sync.
func (l *Link) QueueTamper(kind, detail string, at time.Time) {
	l.tampers = append(l.tampers, client.TamperEvent{Kind: kind, Detail: detail, At: at})
	l.trimTampers()
}

// trimTampers отбрасывает самые старые сверх предела. Именно старые: если
// ребёнок завалил очередь однотипными событиями, свежие важнее.
func (l *Link) trimTampers() {
	if len(l.tampers) > MaxTampers {
		l.tampers = append(l.tampers[:0], l.tampers[len(l.tampers)-MaxTampers:]...)
	}
}

// flushTampers отдаёт накопленные сообщения. Неотправленные остаются ждать.
func (l *Link) flushTampers(ctx context.Context) error {
	for len(l.tampers) > 0 {
		if err := l.client.ReportTamper(ctx, l.tampers[0]); err != nil {
			if client.IsOffline(err) {
				return nil // связь оборвалась — очередь ждёт дальше
			}
			// Сервер отверг само сообщение. Держать его вечно нельзя: оно
			// заблокировало бы все следующие.
			l.tampers = l.tampers[1:]
			return err
		}
		l.tampers = l.tampers[1:]
	}
	return nil
}

// Record копит израсходованные секунды и ставит целые минуты в очередь.
func (l *Link) Record(seconds int, at time.Time) error {
	if seconds <= 0 {
		return nil
	}
	l.pending += seconds
	minutes := l.pending / 60
	if minutes == 0 {
		return nil
	}
	l.pending -= minutes * 60
	if _, err := l.box.Add(minutes, at); err != nil {
		// Записать не смогли — возвращаем минуты в остаток, чтобы расход не
		// испарился из-за временной ошибки диска.
		l.pending += minutes * 60
		return fmt.Errorf("очередь расхода: %w", err)
	}
	return nil
}

// Result — что дал сеанс связи.
type Result struct {
	// Policy — по чему агенту работать дальше.
	Policy config.Policy
	Source policy.Source
	// Note — почему источник политики не тот, которого ждали.
	Note string
	// Stale — кэш политики слишком стар: связи не было несколько суток.
	Stale bool

	Online   bool
	Balances client.Balances
	Screen   client.ScreenState
	// CreditsPerMinute — курс обмена с сервера. Нужен, чтобы сказать ребёнку
	// не «120 кредитов», а «это ещё час»: кредиты сами по себе ни о чём
	// не говорят, а минуты говорят.
	CreditsPerMinute int
	// Accepted и Duplicates — это записи очереди, а не минуты: одна запись
	// может нести несколько минут.
	Accepted   int
	Duplicates int
	Dropped    int
	// Corrected — насколько сервер поправил часы в этот раз.
	Corrected time.Duration
}

// Sync обменивается с сервером и возвращает действующую политику.
//
// Офлайн не ошибка: агент обязан продолжать считать время и без сети, поэтому
// недоступный сервер даёт Result с Online=false и нулевой err. Ошибкой
// остаётся только то, что повтором не лечится.
func (l *Link) Sync(ctx context.Context, r clock.Reading, local config.Policy) (Result, error) {
	if !l.client.Configured() {
		return l.offline(local), nil
	}

	res, err := l.client.Sync(ctx)
	if err != nil {
		if client.IsOffline(err) {
			return l.offline(local), nil
		}
		// Отозванный токен — тоже не повод переставать считать: работаем по
		// кэшу, но говорим об этом вслух.
		out := l.offline(local)
		out.Note = err.Error()
		return out, err
	}

	merged := policy.Clamp(res.Merge(local))
	corrected := l.clk.Sync(r, res.ServerTime)

	out := Result{
		Policy:    merged,
		Source:    policy.FromServer,
		Online:    true,
		Balances:  res.Balances,
		Screen:    res.Screen,
		Corrected: corrected,

		CreditsPerMinute: res.Policy.Economy.CreditsPerMinute,
	}

	// Кэш пишем по серверному времени: местные часы могли быть переставлены,
	// и по ним возраст кэша посчитался бы неверно.
	if err := policy.Save(l.cachePath, merged, res.ServerTime); err != nil {
		out.Note = fmt.Sprintf("кэш политики не сохранён: %v", err)
	}

	if err := l.flush(ctx, &out); err != nil {
		if out.Note == "" {
			out.Note = err.Error()
		}
	}
	if err := l.flushTampers(ctx); err != nil && out.Note == "" {
		out.Note = err.Error()
	}
	return out, nil
}

// offline собирает результат без связи: политика берётся из кэша.
func (l *Link) offline(local config.Policy) Result {
	resolved := policy.Resolve(local, l.cachePath)
	return Result{
		Policy: resolved.Policy,
		Source: resolved.Source,
		Note:   resolved.Note,
		Stale:  resolved.Stale(time.Now()),
	}
}

// flush отдаёт накопленный расход и чистит очередь.
func (l *Link) flush(ctx context.Context, out *Result) error {
	pending := l.box.Pending()
	if len(pending) == 0 {
		return nil
	}

	entries := make([]client.UsageEntry, len(pending))
	for i, e := range pending {
		entries[i] = client.UsageEntry{Seq: e.Seq, Minutes: e.Minutes, OccurredAt: e.OccurredAt}
	}

	res, err := l.client.PushUsage(ctx, entries)
	if err != nil {
		if client.IsOffline(err) {
			// Связь оборвалась между двумя запросами: очередь остаётся ждать.
			dropped, trimErr := l.box.Trim(MaxQueue)
			out.Dropped = dropped
			return trimErr
		}
		return err
	}

	out.Accepted, out.Duplicates = res.Accepted, res.Duplicates
	out.Balances, out.Screen = res.Balances, res.Screen

	// Повторы сервер считает принятыми: он уже помнит эти пары
	// устройство+счётчик, и держать их в очереди значит слать их вечно.
	if err := l.box.Ack(pending); err != nil {
		return fmt.Errorf("очередь расхода: %w", err)
	}
	return nil
}

// Observe проверяет часы и, если их переставили, ставит сообщение в очередь.
//
// Отправляем не сразу: связи может не быть именно потому, что её отключили
// перед переводом часов. Само сообщение при этом уже не потеряется.
func (l *Link) Observe(r clock.Reading) (clock.Jump, bool) {
	jump, tampered := l.clk.Observe(r)
	if !tampered {
		return jump, false
	}
	l.QueueTamper("clock", jump.String(), jump.At)
	return jump, true
}
