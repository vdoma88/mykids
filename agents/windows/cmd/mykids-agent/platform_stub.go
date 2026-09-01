//go:build !windows

package main

import (
	"fmt"
	"time"

	"github.com/vdoma88/mykids/agents/windows/internal/agent"
)

// Вне Windows агент собирается и запускается, но рабочего стола не видит.
// Это нужно, чтобы кросс-сборка и тесты шли на любой системе.
type stubDesktop struct{}

func (stubDesktop) ForegroundProcess() (string, error) {
	return "", fmt.Errorf("наблюдение за рабочим столом доступно только в Windows")
}
func (stubDesktop) IdleTime() (time.Duration, error) { return 0, nil }
func (stubDesktop) SessionLocked() bool              { return false }

func newDesktop() agent.Desktop  { return stubDesktop{} }
func newEnforcer() agent.Enforcer { return nil }
