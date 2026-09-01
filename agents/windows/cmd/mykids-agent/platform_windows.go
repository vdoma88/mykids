//go:build windows

package main

import (
	"time"

	"github.com/vdoma88/mykids/agents/windows/internal/agent"
	"github.com/vdoma88/mykids/agents/windows/internal/win32"
)

type winDesktop struct{}

func (winDesktop) ForegroundProcess() (string, error) { return win32.ForegroundProcess() }
func (winDesktop) IdleTime() (time.Duration, error)   { return win32.IdleTime() }
func (winDesktop) SessionLocked() bool                { return win32.SessionLocked() }

func newDesktop() agent.Desktop { return winDesktop{} }

// overlayEnforcer закрывает экран полноэкранным окном поверх всего.
//
// Если создать окно не удалось, откатываемся на блокировку рабочего стола:
// она грубее, но состоит из одного вызова и сработает почти наверняка.
type overlayEnforcer struct {
	overlay *win32.Overlay
	locked  bool
}

func (e *overlayEnforcer) Block(message string) error {
	if e.overlay != nil {
		e.overlay.SetText(message)
		return nil
	}
	o, err := win32.ShowOverlay(message)
	if err != nil {
		if !e.locked {
			e.locked = true
			return win32.LockWorkstation()
		}
		return nil
	}
	e.overlay = o
	return nil
}

func (e *overlayEnforcer) Unblock() {
	if e.overlay != nil {
		e.overlay.Close()
		e.overlay = nil
	}
	e.locked = false
}

func newEnforcer() agent.Enforcer { return &overlayEnforcer{} }
