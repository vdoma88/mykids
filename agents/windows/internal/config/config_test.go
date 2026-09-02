package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vdoma88/mykids/agents/windows/internal/schedule"
)

func TestDefaultIsValid(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Errorf("стандартная политика невалидна: %v", err)
	}
}

func TestDefaultAllowsReachingParent(t *testing.T) {
	// При нулевом балансе ребёнок обязан иметь возможность позвонить.
	if len(Default().AlwaysAllowed) == 0 {
		t.Error("белый список пуст: ребёнок останется без связи")
	}
}

func TestLoadCreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	p, err := Load(path)
	if err != nil {
		t.Fatalf("загрузка: %v", err)
	}
	if p.Timezone == "" {
		t.Error("политика пуста")
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("файл не создан: %v", err)
	}
}

func TestValidateRejectsBadInput(t *testing.T) {
	cases := map[string]func(*Policy){
		"шесть дней вместо семи": func(p *Policy) { p.DailyLimitMinutes = []int{1, 2, 3, 4, 5, 6} },
		"отрицательный лимит":    func(p *Policy) { p.DailyLimitMinutes[0] = -1 },
		"нулевой порог простоя":  func(p *Policy) { p.IdleThresholdSeconds = 0 },
		"неизвестный пояс":       func(p *Policy) { p.Timezone = "Марс/Олимп" },
		"кривое время окна":      func(p *Policy) { p.Windows[0].From = "25:00" },
		"неизвестный режим":      func(p *Policy) { p.Windows[0].Mode = schedule.Mode("шалтай") },
	}
	for name, mutate := range cases {
		p := Default()
		mutate(&p)
		if err := p.Validate(); err == nil {
			t.Errorf("%s: ошибки нет, а должна быть", name)
		}
	}
}

func TestLoadRejectsInvalidFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(path, []byte(`{"idleThresholdSeconds":0}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("невалидная политика загрузилась без ошибки")
	}
}

func TestLimitFor(t *testing.T) {
	p := Default()
	if p.LimitFor(1) != 60 || p.LimitFor(0) != 120 {
		t.Errorf("лимиты: пн=%d вс=%d", p.LimitFor(1), p.LimitFor(0))
	}
	if p.LimitFor(9) != 0 || p.LimitFor(-1) != 0 {
		t.Error("индекс вне диапазона должен давать 0")
	}
}
