//go:build windows

package main

import (
	"golang.org/x/sys/windows"

	"github.com/vdoma88/mykids/agents/windows/internal/watchdog"
	"github.com/vdoma88/mykids/agents/windows/internal/win32"
)

// winHelpers поднимает помощника в сессии ребёнка.
//
// Служба живёт в нулевой сессии, помощник должен жить в сессии ребёнка —
// значит обычным exec.Command его не запустить.
type winHelpers struct {
	exe  string
	args []string
	proc windows.Handle
}

func (h *winHelpers) ActiveSession() watchdog.Session {
	return watchdog.Session(win32.ActiveSession())
}

func (h *winHelpers) HelperAlive() bool { return win32.Alive(h.proc) }

func (h *winHelpers) StartHelper(s watchdog.Session) error {
	// Дескриптор прошлого помощника закрываем: процесса уже нет, а дескриптор
	// держал бы запись о нём в таблице ядра до перезапуска службы.
	if h.proc != 0 {
		windows.CloseHandle(h.proc)
		h.proc = 0
	}
	proc, err := win32.StartInSession(uint32(s), h.exe, h.args...)
	if err != nil {
		return err
	}
	h.proc = proc
	return nil
}

func newHelpers(exe string, args []string) watchdog.Desktop {
	return &winHelpers{exe: exe, args: args}
}
