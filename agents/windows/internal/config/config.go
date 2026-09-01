// Package config читает политику семьи с диска.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/vdoma88/mykids/agents/windows/internal/schedule"
)

// Policy — то, по чему агент принимает решения офлайн.
type Policy struct {
	Timezone string `json:"timezone"`
	// Дневной лимит минут по дням недели, индекс 0 — воскресенье.
	DailyLimitMinutes []int `json:"dailyLimitMinutes"`
	// Сколько неистраченного переносится на следующий день.
	CarryOverMaxMinutes int               `json:"carryOverMaxMinutes"`
	Windows             []schedule.Window `json:"windows"`
	// Процессы, которые не считаются и никогда не блокируются.
	AlwaysAllowed []string `json:"alwaysAllowed"`
	// Простой дольше этого не засчитывается в экранное время.
	IdleThresholdSeconds int `json:"idleThresholdSeconds"`
	// Предупредить ребёнка за столько минут до блокировки.
	WarnBeforeMinutes int `json:"warnBeforeMinutes"`
}

// Default — политика, с которой агент запускается, если файла ещё нет.
func Default() Policy {
	return Policy{
		Timezone:            "Europe/Moscow",
		DailyLimitMinutes:   []int{120, 60, 60, 60, 60, 90, 120},
		CarryOverMaxMinutes: 30,
		Windows: []schedule.Window{
			{Name: "школа", Days: []int{1, 2, 3, 4, 5}, From: "08:00", To: "14:00", Mode: schedule.ModeBlocked},
			{Name: "отбой", Days: []int{0, 1, 2, 3, 4, 5, 6}, From: "21:30", To: "07:00", Mode: schedule.ModeBlocked},
		},
		// Ребёнок обязан иметь возможность позвонить родителю при нулевом балансе.
		AlwaysAllowed:        []string{"explorer.exe", "mykids-agent.exe", "Taskmgr.exe"},
		IdleThresholdSeconds: 120,
		WarnBeforeMinutes:    5,
	}
}

// Validate ловит то, из-за чего агент повёл бы себя неожиданно.
func (p Policy) Validate() error {
	if len(p.DailyLimitMinutes) != 7 {
		return fmt.Errorf("dailyLimitMinutes: нужно ровно 7 значений, получено %d", len(p.DailyLimitMinutes))
	}
	for i, v := range p.DailyLimitMinutes {
		if v < 0 {
			return fmt.Errorf("dailyLimitMinutes[%d]: отрицательный лимит %d", i, v)
		}
	}
	if p.CarryOverMaxMinutes < 0 {
		return fmt.Errorf("carryOverMaxMinutes: не может быть отрицательным")
	}
	if p.IdleThresholdSeconds <= 0 {
		return fmt.Errorf("idleThresholdSeconds: должен быть положительным")
	}
	if _, err := time.LoadLocation(p.Timezone); err != nil {
		return fmt.Errorf("timezone %q: %w", p.Timezone, err)
	}
	for _, w := range p.Windows {
		if _, err := schedule.ParseTimeOfDay(w.From); err != nil {
			return fmt.Errorf("окно %q: %w", w.Name, err)
		}
		if _, err := schedule.ParseTimeOfDay(w.To); err != nil {
			return fmt.Errorf("окно %q: %w", w.Name, err)
		}
		switch w.Mode {
		case schedule.ModeBlocked, schedule.ModeTasksOnly, schedule.ModeAllowed:
		default:
			return fmt.Errorf("окно %q: неизвестный режим %q", w.Name, w.Mode)
		}
	}
	return nil
}

// Location — пояс семьи.
func (p Policy) Location() (*time.Location, error) {
	return time.LoadLocation(p.Timezone)
}

// LimitFor — дневной лимит для дня недели.
func (p Policy) LimitFor(weekday int) int {
	if weekday < 0 || weekday >= len(p.DailyLimitMinutes) {
		return 0
	}
	return p.DailyLimitMinutes[weekday]
}

// Load читает политику. Отсутствующий файл — не ошибка: создаётся стандартная.
func Load(path string) (Policy, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		p := Default()
		if err := Save(path, p); err != nil {
			return p, fmt.Errorf("не удалось создать %s: %w", path, err)
		}
		return p, nil
	}
	if err != nil {
		return Policy{}, err
	}

	p := Default()
	if err := json.Unmarshal(raw, &p); err != nil {
		return Policy{}, fmt.Errorf("%s: %w", path, err)
	}
	if err := p.Validate(); err != nil {
		return Policy{}, fmt.Errorf("%s: %w", path, err)
	}
	return p, nil
}

// Save записывает политику.
func Save(path string, p Policy) error {
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}
