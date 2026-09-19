// Package agent связывает учёт, расписание и принуждение.
package agent

import (
	"fmt"
	"reflect"
	"time"

	"github.com/vdoma88/mykids/agents/windows/internal/config"
	"github.com/vdoma88/mykids/agents/windows/internal/schedule"
	"github.com/vdoma88/mykids/agents/windows/internal/screen"
	"github.com/vdoma88/mykids/agents/windows/internal/state"
	"github.com/vdoma88/mykids/agents/windows/internal/usage"
)

// Desktop — то, что агенту нужно знать о рабочем столе. Интерфейс введён
// ради тестируемости: настоящая реализация живёт в win32 и на Linux не собирается.
type Desktop interface {
	ForegroundProcess() (string, error)
	IdleTime() (time.Duration, error)
	SessionLocked() bool
}

// Enforcer закрывает экран, когда пользоваться им нельзя.
type Enforcer interface {
	Block(message string) error
	Unblock()
}

// Agent — один цикл наблюдения и принуждения.
type Agent struct {
	Policy   config.Policy
	Desktop  Desktop
	Enforcer Enforcer
	Location *time.Location

	acc     *usage.Accountant
	st      state.State
	blocked bool
	// screen — остатки и курс для надписи на закрытом экране. Пустое значение
	// даёт осмысленный текст: просто без чисел, которых агент ещё не знает.
	screen screen.Context
}

// New собирает агента.
func New(p config.Policy, d Desktop, e Enforcer, st state.State) (*Agent, error) {
	loc, err := p.Location()
	if err != nil {
		return nil, err
	}
	// Разрыв между замерами ограничиваем минутой: сон ноутбука или остановка
	// агента не должны списываться целиком.
	acc := usage.New(
		time.Duration(p.IdleThresholdSeconds)*time.Second,
		p.AlwaysAllowed,
		time.Minute,
	)
	return &Agent{Policy: p, Desktop: d, Enforcer: e, Location: loc, acc: acc, st: st}, nil
}

// SetPolicy подменяет политику на лету — так применяется присланная сервером.
//
// Неизменившаяся политика не трогает ничего. Это не оптимизация: пересборка
// учётчика сбрасывает точку отсчёта, и следующий замер списывает ноль. Обмен с
// сервером идёт раз в минуту, так что безусловная пересборка дарила бы ребёнку
// по нескольку секунд каждую минуту.
func (a *Agent) SetPolicy(p config.Policy) (bool, error) {
	if reflect.DeepEqual(a.Policy, p) {
		return false, nil
	}
	loc, err := p.Location()
	if err != nil {
		return false, err
	}
	a.Policy, a.Location = p, loc
	// Порог простоя и белый список зашиты в учётчик при создании: без
	// пересборки новые значения не подействовали бы.
	a.acc = usage.New(
		time.Duration(p.IdleThresholdSeconds)*time.Second,
		p.AlwaysAllowed, time.Minute)
	return true, nil
}

// State возвращает текущее состояние для сохранения.
func (a *Agent) State() state.State { return a.st }

// IsAllowlisted сообщает, освобождён ли процесс от учёта и блокировки.
func (a *Agent) IsAllowlisted(process string) bool { return a.acc.IsAllowlisted(process) }

// ensureDay переводит учёт на текущие сутки и проставляет дневную выдачу.
func (a *Agent) ensureDay(now time.Time) schedule.Moment {
	moment := schedule.At(now, a.Location)

	if a.st.Rollover(moment.Day) {
		a.acc = usage.New(
			time.Duration(a.Policy.IdleThresholdSeconds)*time.Second,
			a.Policy.AlwaysAllowed, time.Minute)
	}

	usage.Grant(&a.st.Today,
		a.Policy.LimitFor(moment.Weekday),
		usage.CarryOver(a.st.Yesterday, a.Policy.CarryOverMaxMinutes))
	return moment
}

// Recovery — что агент сделал с последствиями нештатной остановки.
type Recovery struct {
	// Unclean — прошлый запуск не завершился штатно.
	Unclean bool
	// Gap — сколько времени агент не работал.
	Gap time.Duration
	// ChargedSecs — сколько из этого списано.
	ChargedSecs int
}

// RecoverUnclean оплачивает время, пропущенное после нештатной остановки.
//
// Вызывается один раз при запуске, до цикла, и только теми командами, которые
// потом сохранят состояние. Отличить сбой питания от снятия агента здесь
// нельзя, поэтому вызывающий обязан сообщить о списании родителю: вернуть
// время или нет — решает он.
func (a *Agent) RecoverUnclean(now time.Time) Recovery {
	return a.recover(now, true)
}

// PendingRecovery показывает, что будет списано, ничего не меняя.
//
// Нужна диагностике: списать пропуск и не сохранить состояние значило бы
// списать его второй раз при следующем запуске.
func (a *Agent) PendingRecovery(now time.Time) Recovery {
	return a.recover(now, false)
}

func (a *Agent) recover(now time.Time, apply bool) Recovery {
	if a.st.CleanShutdown || a.st.LastSeenAt.IsZero() {
		return Recovery{}
	}
	r := Recovery{Unclean: true, Gap: now.Sub(a.st.LastSeenAt)}

	if !apply {
		// Считаем на копии: поля состояния — значения, так что правки внутри
		// probe до настоящего агента не доходят.
		probe := *a
		probe.ensureDay(now)
		r.ChargedSecs = usage.GapCharge(probe.st.Today, r.Gap)
		return r
	}

	// Выдачу проставляем до списания: ограничение считается от неё, и без
	// этого при первом за сутки запуске списывать было бы не из чего.
	a.ensureDay(now)
	r.ChargedSecs = usage.ChargeGap(&a.st.Today, r.Gap)
	a.st.UncleanStops++
	return r
}

// Tick — один шаг цикла. Возвращает вердикт, чтобы вызывающий мог его показать.
func (a *Agent) Tick(now time.Time) (usage.Verdict, error) {
	moment := a.ensureDay(now)

	proc, err := a.Desktop.ForegroundProcess()
	if err != nil {
		return usage.Verdict{}, fmt.Errorf("активное окно: %w", err)
	}
	idle, err := a.Desktop.IdleTime()
	if err != nil {
		return usage.Verdict{}, fmt.Errorf("время простоя: %w", err)
	}

	consumed := a.acc.Observe(usage.Sample{
		At: now, Process: proc, Idle: idle, SessionLock: a.Desktop.SessionLocked(),
	}, &a.st.Today, moment.Day)

	verdict := usage.Decide(a.Policy.Windows, moment, a.st.Today,
		time.Duration(a.Policy.WarnBeforeMinutes)*time.Minute)
	verdict.ConsumedSecs = consumed

	// Приложения из белого списка не блокируются никогда: ребёнок должен иметь
	// возможность позвонить родителю при нулевом балансе.
	if !verdict.Allow && a.acc.IsAllowlisted(proc) {
		verdict.Allow = true
		verdict.Reason = "приложение в белом списке"
	}

	if err := a.applyEnforcement(verdict); err != nil {
		return verdict, err
	}
	return verdict, nil
}

func (a *Agent) applyEnforcement(v usage.Verdict) error {
	if a.Enforcer == nil {
		return nil
	}
	if v.Allow {
		if a.blocked {
			a.Enforcer.Unblock()
			a.blocked = false
		}
		return nil
	}

	message := screen.Render(screen.Build(v, a.screen))
	if !a.blocked {
		if err := a.Enforcer.Block(message); err != nil {
			return fmt.Errorf("блокировка: %w", err)
		}
		a.blocked = true
		return nil
	}
	// Уже заблокировано: только обновляем текст.
	return a.Enforcer.Block(message)
}

// SetScreen задаёт то, что агент знает про остатки ребёнка: без этого на
// закрытом экране не из чего написать, сколько есть кредитов и что будет
// завтра.
func (a *Agent) SetScreen(c screen.Context) { a.screen = c }

// TomorrowLimit — дневной лимит на завтра по действующей политике.
func (a *Agent) TomorrowLimit(now time.Time) int {
	loc, err := a.Policy.Location()
	if err != nil {
		return 0
	}
	return a.Policy.LimitFor(int(now.In(loc).AddDate(0, 0, 1).Weekday()))
}

// FormatLeft — остаток в виде «1 ч 05 мин».
func FormatLeft(seconds int) string {
	if seconds < 0 {
		seconds = 0
	}
	h, m := seconds/3600, (seconds%3600)/60
	if h > 0 {
		return fmt.Sprintf("%d ч %02d мин", h, m)
	}
	return fmt.Sprintf("%d мин", m)
}
