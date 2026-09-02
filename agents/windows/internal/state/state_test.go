package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vdoma88/mykids/agents/windows/internal/usage"
)

func TestLoadMissingFileIsEmpty(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "нет.json"))
	if err != nil || s.Today.Key != "" {
		t.Errorf("отсутствующий файл дал %+v, %v", s, err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "state.json")
	want := State{Today: usage.Day{Key: "2026-03-09", UsedSeconds: 120, GrantSeconds: 3600}, UncleanStops: 2}
	if err := Save(path, want); err != nil {
		t.Fatalf("сохранение: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("загрузка: %v", err)
	}
	if got != want {
		t.Errorf("получено %+v, ожидалось %+v", got, want)
	}
}

func TestSaveLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := Save(path, State{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("временный файл остался после записи")
	}
}

func TestLoadCorruptFileReportsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{не json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("битый файл не дал ошибки")
	}
}

func TestRollover(t *testing.T) {
	s := State{Today: usage.Day{Key: "2026-03-08", UsedSeconds: 3600, GrantSeconds: 3600}}
	if !s.Rollover("2026-03-09") {
		t.Fatal("смена суток не распознана")
	}
	if s.Yesterday.UsedSeconds != 3600 || s.Today.UsedSeconds != 0 || s.Today.Key != "2026-03-09" {
		t.Errorf("после переноса: вчера %+v, сегодня %+v", s.Yesterday, s.Today)
	}
	if s.Rollover("2026-03-09") {
		t.Error("повторный перенос в тех же сутках")
	}
}
