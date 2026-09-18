package policy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vdoma88/mykids/agents/windows/internal/config"
	"github.com/vdoma88/mykids/agents/windows/internal/schedule"
)

var now = time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

// server — политика, какой её задал родитель.
func server() config.Policy {
	p := config.Default()
	p.DailyLimitMinutes = []int{30, 30, 30, 30, 30, 30, 30}
	p.Windows = []schedule.Window{
		{Name: "отбой", Days: []int{0, 1, 2, 3, 4, 5, 6}, From: "21:00", To: "07:00", Mode: schedule.ModeBlocked},
	}
	return p
}

// tampered — тот же файл после правки ребёнком.
func tampered() config.Policy {
	p := config.Default()
	p.DailyLimitMinutes = []int{999, 999, 999, 999, 999, 999, 999}
	p.Windows = nil
	return p
}

func TestResolveWithoutCacheUsesLocal(t *testing.T) {
	r := Resolve(server(), filepath.Join(t.TempDir(), "policy-cache.json"))
	if r.Source != FromLocal {
		t.Fatalf("без кэша источник должен быть локальным, получено %q", r.Source)
	}
	if r.Note != "" {
		t.Fatalf("отсутствие кэша не повод для замечания: %q", r.Note)
	}
	if r.Policy.LimitFor(1) != 30 {
		t.Fatalf("лимит не взят из локального файла: %d", r.Policy.LimitFor(1))
	}
}

func TestCacheBeatsEditedLocalFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy-cache.json")
	if err := Save(path, server(), now); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Ребёнок переписал локальный файл — это не должно ничего дать.
	r := Resolve(tampered(), path)
	if r.Source != FromCache {
		t.Fatalf("должен победить кэш, получено %q", r.Source)
	}
	if r.Policy.LimitFor(1) != 30 {
		t.Fatalf("применён правленый лимит: %d", r.Policy.LimitFor(1))
	}
	if len(r.Policy.Windows) != 1 {
		t.Fatalf("окна из кэша потеряны: %+v", r.Policy.Windows)
	}
	if !r.FetchedAt.Equal(now) {
		t.Fatalf("время последнего обмена не сохранилось: %v", r.FetchedAt)
	}
}

func TestStaleCacheStillBeatsLocal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy-cache.json")
	long := now.Add(-30 * 24 * time.Hour)
	if err := Save(path, server(), long); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Отключить сеть на месяц не должно возвращать к локальному файлу.
	r := Resolve(tampered(), path)
	if r.Source != FromCache || r.Policy.LimitFor(1) != 30 {
		t.Fatalf("устаревший кэш должен применяться: %q, лимит %d", r.Source, r.Policy.LimitFor(1))
	}
	if !r.Stale(now) {
		t.Fatal("месячный кэш должен считаться устаревшим")
	}
	if r.Age(now) < 29*24*time.Hour {
		t.Fatalf("возраст кэша посчитан неверно: %v", r.Age(now))
	}
}

func TestFreshCacheIsNotStale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy-cache.json")
	if err := Save(path, server(), now.Add(-time.Hour)); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if r := Resolve(server(), path); r.Stale(now) {
		t.Fatal("часовой кэш не устарел")
	}
}

func TestLocalPolicyIsNeverStale(t *testing.T) {
	r := Resolve(server(), filepath.Join(t.TempDir(), "нет.json"))
	if r.Stale(now) {
		t.Fatal("локальная политика не может устареть — её никто не получал")
	}
	if r.Age(now) != 0 {
		t.Fatalf("возраст локальной политики должен быть нулевым, получено %v", r.Age(now))
	}
}

func TestCorruptCacheFallsBackWithNote(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy-cache.json")
	if err := os.WriteFile(path, []byte("{это не json"), 0o600); err != nil {
		t.Fatalf("подготовка: %v", err)
	}

	r := Resolve(server(), path)
	if r.Source != FromLocal {
		t.Fatalf("битый кэш должен уступать локальному файлу, получено %q", r.Source)
	}
	if r.Note == "" {
		t.Fatal("битый кэш должен быть отмечен в Note — иначе родитель не узнает")
	}
	if r.Policy.LimitFor(1) != 30 {
		t.Fatalf("локальная политика не применилась: %d", r.Policy.LimitFor(1))
	}
}

func TestInvalidCacheFallsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy-cache.json")
	// Синтаксически верный, но с четырьмя днями недели вместо семи: работать
	// по нему значило бы заблокировать экран навсегда.
	broken := map[string]any{
		"fetchedAt": now,
		"policy": map[string]any{
			"timezone":             "Europe/Moscow",
			"dailyLimitMinutes":    []int{10, 10, 10, 10},
			"idleThresholdSeconds": 120,
		},
	}
	raw, _ := json.Marshal(broken)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("подготовка: %v", err)
	}

	r := Resolve(server(), path)
	if r.Source != FromLocal {
		t.Fatalf("невалидный кэш должен уступать локальному файлу, получено %q", r.Source)
	}
	if r.Note == "" {
		t.Fatal("отказ проверки должен быть отмечен в Note")
	}
}

func TestClampIdleThreshold(t *testing.T) {
	// Секундный порог простоя — способ не тратить лимит на чтение и видео.
	low := server()
	low.IdleThresholdSeconds = 1
	if got := Clamp(low).IdleThresholdSeconds; got != MinIdleThresholdSeconds {
		t.Fatalf("порог снизу не ограничен: %d", got)
	}

	high := server()
	high.IdleThresholdSeconds = 36000
	if got := Clamp(high).IdleThresholdSeconds; got != MaxIdleThresholdSeconds {
		t.Fatalf("порог сверху не ограничен: %d", got)
	}

	ok := server()
	ok.IdleThresholdSeconds = 120
	if got := Clamp(ok).IdleThresholdSeconds; got != 120 {
		t.Fatalf("разумное значение изменено: %d", got)
	}
}

func TestClampWarnBefore(t *testing.T) {
	p := server()
	p.WarnBeforeMinutes = -5
	if got := Clamp(p).WarnBeforeMinutes; got != 0 {
		t.Fatalf("отрицательное предупреждение не обнулено: %d", got)
	}
	p.WarnBeforeMinutes = 500
	if got := Clamp(p).WarnBeforeMinutes; got != MaxWarnBeforeMinutes {
		t.Fatalf("предупреждение сверху не ограничено: %d", got)
	}
}

func TestResolveClampsBothSources(t *testing.T) {
	dir := t.TempDir()
	local := server()
	local.IdleThresholdSeconds = 1

	// Локальная ветка.
	if got := Resolve(local, filepath.Join(dir, "нет.json")).Policy.IdleThresholdSeconds; got != MinIdleThresholdSeconds {
		t.Fatalf("локальная политика не ограничена: %d", got)
	}

	// Ветка кэша: значение могло попасть туда до появления границ.
	path := filepath.Join(dir, "policy-cache.json")
	raw, _ := json.Marshal(cacheFile{FetchedAt: now, Policy: local})
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("подготовка: %v", err)
	}
	if got := Resolve(server(), path).Policy.IdleThresholdSeconds; got != MinIdleThresholdSeconds {
		t.Fatalf("политика из кэша не ограничена: %d", got)
	}
}

func TestSaveClampsBeforeWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy-cache.json")
	p := server()
	p.IdleThresholdSeconds = 1
	if err := Save(path, p, now); err != nil {
		t.Fatalf("Save: %v", err)
	}

	var cached cacheFile
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if err := json.Unmarshal(raw, &cached); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if cached.Policy.IdleThresholdSeconds != MinIdleThresholdSeconds {
		t.Fatalf("в кэш записано неограниченное значение: %d", cached.Policy.IdleThresholdSeconds)
	}
}

func TestSaveIsAtomicAndPrivate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "policy-cache.json")
	if err := Save(path, server(), now); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Fatal("временный файл должен быть переименован")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("кэш должен быть закрыт от посторонних, права %o", mode)
	}
}

func TestSaveOverwritesPreviousCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy-cache.json")
	if err := Save(path, server(), now.Add(-time.Hour)); err != nil {
		t.Fatalf("Save: %v", err)
	}
	later := server()
	later.DailyLimitMinutes = []int{45, 45, 45, 45, 45, 45, 45}
	if err := Save(path, later, now); err != nil {
		t.Fatalf("Save: %v", err)
	}

	r := Resolve(tampered(), path)
	if r.Policy.LimitFor(1) != 45 || !r.FetchedAt.Equal(now) {
		t.Fatalf("новая политика не заменила старую: лимит %d, время %v", r.Policy.LimitFor(1), r.FetchedAt)
	}
}
