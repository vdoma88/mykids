// Package agent связывает учёт, расписание и принуждение.
package agent

import (
	"fmt"
	"time"

	"github.com/vdoma88/mykids/agents/windows/internal/config"
	"github.com/vdoma88/mykids/agents/windows/internal/schedule"
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

// State возвращает текущее состояние для сохранения.
func (a *Agent) State() state.State { return a.st }

// Tick — один шаг цикла. Возвращает вердикт, чтобы вызывающий мог его показать.
func (a *Agent) Tick(now time.Time) (usage.Verdict, error) {
	moment := schedule.At(now, a.Location)

	if a.st.Rollover(moment.Day) {
		a.acc = usage.New(
			time.Duration(a.Policy.IdleThresholdSeconds)*time.Second,
			a.Policy.AlwaysAllowed, time.Minute)
	}

	usage.Grant(&a.st.Today,
		a.Policy.LimitFor(moment.Weekday),
		usage.CarryOver(a.st.Yesterday, a.Policy.CarryOverMaxMinutes))

	proc, err := a.Desktop.ForegroundProcess()
	if err != nil {
		return usage.Verdict{}, fmt.Errorf("активное окно: %w", err)
	}
	idle, err := a.Desktop.IdleTime()
	if err != nil {
		return usage.Verdict{}, fmt.Errorf("время простоя: %w", err)
	}

	a.acc.Observe(usage.Sample{
		At: now, Process: proc, Idle: idle, SessionLock: a.Desktop.SessionLocked(),
	}, &a.st.Today, moment.Day)

	verdict := usage.Decide(a.Policy.Windows, moment, a.st.Today,
		time.Duration(a.Policy.WarnBeforeMinutes)*time.Minute)

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

	message := BlockMessage(v)
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

// BlockMessage — что видит ребёнок на закрытом экране.
func BlockMessage(v usage.Verdict) string {
	switch {
	case v.Window != "" && v.TasksOnly:
		return fmt.Sprintf("Сейчас «%s» — время для заданий.\nЭкран откроется после окна.", v.Window)
	case v.Window != "":
		return fmt.Sprintf("Сейчас «%s».\nЭкран закрыт по расписанию.", v.Window)
	default:
		return "Экранное время на сегодня закончилось.\nМожно заработать ещё, решив задания."
	}
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
