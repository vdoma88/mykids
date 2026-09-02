// Package state хранит состояние агента между запусками.
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/vdoma88/mykids/agents/windows/internal/usage"
)

// State — то, что переживает перезапуск.
type State struct {
	Today     usage.Day `json:"today"`
	Yesterday usage.Day `json:"yesterday"`
	// Сколько раз агент запускался после нештатного завершения. Растёт, когда
	// предыдущий запуск не закрылся чисто — признак попытки его убить.
	UncleanStops int `json:"uncleanStops"`
	// Признак чистого завершения; сбрасывается при старте, ставится при выходе.
	CleanShutdown bool `json:"cleanShutdown"`
}

// Load читает состояние. Отсутствующий или битый файл — не повод падать:
// агент должен подниматься и продолжать считать.
func Load(path string) (State, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return State{}, nil
	}
	if err != nil {
		return State{}, err
	}
	var s State
	if err := json.Unmarshal(raw, &s); err != nil {
		return State{}, fmt.Errorf("%s повреждён: %w", path, err)
	}
	return s, nil
}

// Save пишет состояние атомарно: обрыв питания посреди записи не должен
// оставить обрезанный файл, из-за которого потерялся бы расход за день.
func Save(path string, s State) error {
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(raw, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Rollover переносит сегодняшний день во вчерашний при смене суток.
func (s *State) Rollover(newDay string) bool {
	if s.Today.Key == newDay {
		return false
	}
	if s.Today.Key != "" {
		s.Yesterday = s.Today
	}
	s.Today = usage.Day{Key: newDay}
	return true
}
