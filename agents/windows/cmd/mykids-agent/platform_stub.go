//go:build !windows

package main

import (
	"fmt"
	"os"
	"time"

	"github.com/vdoma88/mykids/agents/windows/internal/agent"
	"github.com/vdoma88/mykids/agents/windows/internal/screen"
)

// Вне Windows агент собирается и запускается, но рабочего стола не видит.
// Это нужно, чтобы кросс-сборка и тесты шли на любой системе.
type stubDesktop struct{}

func (stubDesktop) ForegroundProcess() (string, error) {
	// Имя задаётся переменной окружения: так разделение службы и помощника
	// проверяется целиком там, где Windows нет.
	if p := os.Getenv("MYKIDS_FAKE_PROCESS"); p != "" {
		return p, nil
	}
	return "", fmt.Errorf("наблюдение за рабочим столом доступно только в Windows")
}

func (stubDesktop) IdleTime() (time.Duration, error) { return 0, nil }
func (stubDesktop) SessionLocked() bool              { return false }

// printEnforcer печатает то, что настоящий оверлей нарисовал бы поверх экрана.
//
// Не заглушка ради компиляции: без него помощника нельзя было бы запустить
// вне Windows, а значит нельзя было бы проверить два процесса вместе.
// Закрыть экран он, разумеется, не может, и молчать об этом не должен.
type printEnforcer struct{ blocked bool }

func (e *printEnforcer) Block(message string) error {
	if !e.blocked {
		fmt.Printf("ЭКРАН ЗАКРЫТ (вне Windows — только сообщение): %s\n", message)
		e.blocked = true
	}
	return nil
}

func (e *printEnforcer) Unblock() {
	if e.blocked {
		fmt.Println("экран открыт")
		e.blocked = false
	}
}

// shownWarning — что уже напечатано. Помощник зовёт warn на каждом замере, и
// без этого в консоли будет одна и та же строка раз в секунду: в таком выводе
// не видно, когда предупреждение сменилось, а ради этого его и смотрят.
var shownWarning string

// warn показывает ненавязчивое предупреждение. Вне Windows — строкой.
func warn(s screen.Screen) {
	line := s.Title
	if s.Hint != "" {
		line += " — " + s.Hint
	}
	if line == shownWarning {
		return
	}
	shownWarning = line
	fmt.Println("ПРЕДУПРЕЖДЕНИЕ:", line)
}

// hideWarning убирает предупреждение. Вне Windows убирать нечего, кроме памяти
// о том, что уже напечатано.
func hideWarning() { shownWarning = "" }

func newDesktop() agent.Desktop   { return stubDesktop{} }
func newEnforcer() agent.Enforcer { return &printEnforcer{} }
