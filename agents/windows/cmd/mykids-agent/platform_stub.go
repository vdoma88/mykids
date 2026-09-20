//go:build !windows

package main

import (
	"fmt"
	"os"
	"time"

	"github.com/vdoma88/mykids/agents/windows/internal/agent"
	"github.com/vdoma88/mykids/agents/windows/internal/ipc"
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

// SessionLocked — то, что помощник заявляет о блокировке экрана.
//
// Переменная окружения позволяет ему соврать. Это не украшение: заявленная
// блокировка при неподвижном окне — самая выгодная ложь из возможных, старой
// проверкой на противоречии она не ловится вовсе, и проверять защиту от неё
// нужно на двух настоящих процессах. Переменная отдельная от MYKIDS_FAKE_LOCK
// именно затем, чтобы помощник и служба могли разойтись в показаниях.
func (stubDesktop) SessionLocked() bool { return os.Getenv("MYKIDS_FAKE_HELPER_LOCK") == "1" }

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

// newLockSource — собственный источник состояния экрана.
//
// Вне Windows спросить некого, и по умолчанию источника нет вовсе: всё решает
// помощник, как и до встречной проверки. Переменная окружения задаёт ответ —
// тем же способом, что и MYKIDS_FAKE_PROCESS, и ровно затем же: чтобы
// поведение двух процессов вместе проверялось там, где Windows нет.
func newLockSource() ipc.LockSource {
	return ipc.LockFunc(func() ipc.LockState {
		switch os.Getenv("MYKIDS_FAKE_LOCK") {
		case "on":
			return ipc.LockOn
		case "off":
			return ipc.LockOff
		default:
			return ipc.LockUnknown
		}
	})
}
