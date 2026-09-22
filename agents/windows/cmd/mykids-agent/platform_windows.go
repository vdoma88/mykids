//go:build windows

package main

import (
	"fmt"
	"os"
	"time"

	"github.com/vdoma88/mykids/agents/windows/internal/agent"
	"github.com/vdoma88/mykids/agents/windows/internal/ipc"
	"github.com/vdoma88/mykids/agents/windows/internal/screen"
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
	// Помощник зовёт Block на каждом замере, а не только при смене решения.
	// Перерисовывать одно и то же нельзя: экран будет мигать раз в пять
	// секунд, и подросток решит, что программа сломана.
	if e.overlay != nil && e.overlay.Alive() {
		if e.overlay.Text() != message {
			e.overlay.SetText(message)
		}
		return nil
	}
	e.overlay = nil
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

// bar — полоса предупреждения. Живёт отдельно от оверлея: они показываются
// в разное время и закрываются независимо.
var bar *win32.Overlay

// warn показывает предупреждение внизу справа, не перекрывая экран.
//
// Ошибку не поднимаем и работу не останавливаем: не показать предупреждение
// неприятно, но это не повод переставать считать время.
func warn(s screen.Screen) {
	text := s.Title
	if s.Hint != "" {
		text += "\n" + s.Hint
	}
	if bar != nil && bar.Alive() {
		if bar.Text() != text {
			bar.SetText(text)
		}
		return
	}
	bar = nil
	b, err := win32.ShowBar(text)
	if err != nil {
		return
	}
	bar = b
}

// hideWarning убирает полосу, когда предупреждать больше не о чем.
func hideWarning() {
	if bar != nil {
		bar.Close()
		bar = nil
	}
}

// newLockSource — собственный источник состояния экрана для службы.
//
// Спрашиваем про консольную сессию: это та, за которой физически сидят. При
// переключении пользователей консольной становится чужая сессия, и ответ
// «открыт» по ней — правильный ответ: за компьютером работают, пусть и не
// ребёнок. Вопрос о втором пользователе решается не здесь.
func newLockSource() ipc.LockSource {
	return ipc.LockFunc(func() ipc.LockState {
		session := win32.ConsoleSession()
		if session == 0 {
			// Никто не вошёл: блокировать нечего и незачем.
			return ipc.LockUnknown
		}
		locked, known := win32.SessionLockedWTS(session)
		switch {
		case !known:
			return ipc.LockUnknown
		case locked:
			return ipc.LockOn
		default:
			return ipc.LockOff
		}
	})
}

// tray — значок в трее. Один на помощника: он и есть «программа в трее».
var tray *win32.Tray

// showTray вешает или обновляет значок.
//
// Ошибку не поднимаем: без значка агент считает время ровно так же, а ронять
// помощника из-за подписи под курсором — менять полезное на красивое.
func showTray(s screen.Tray) {
	if tray != nil && tray.Alive() {
		tray.Update(s)
		return
	}
	tray = nil
	t, err := win32.ShowTray(s)
	if err != nil {
		fmt.Fprintf(os.Stderr, "значок в трее: %v\n", err)
		return
	}
	tray = t
}

// hideTray убирает значок при выходе.
func hideTray() {
	if tray != nil {
		tray.Close()
		tray = nil
	}
}
