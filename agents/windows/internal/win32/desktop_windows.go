//go:build windows

// Package win32 — тонкая обёртка над теми вызовами Windows, которые нужны
// агенту. Всё остальное держится платформенно-нейтральным и тестируется на любой
// системе; здесь собрано то, что проверить можно только на Windows.
package win32

import (
	"fmt"
	"path/filepath"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                     = windows.NewLazySystemDLL("user32.dll")
	kernel32                   = windows.NewLazySystemDLL("kernel32.dll")
	procGetForegroundWindow    = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcess = user32.NewProc("GetWindowThreadProcessId")
	procGetLastInputInfo       = user32.NewProc("GetLastInputInfo")
	procLockWorkStation        = user32.NewProc("LockWorkStation")
	procGetTickCount64         = kernel32.NewProc("GetTickCount64")
	procOpenInputDesktop       = user32.NewProc("OpenInputDesktop")
	procCloseDesktop           = user32.NewProc("CloseDesktop")
)

type lastInputInfo struct {
	cbSize uint32
	dwTime uint32
}

// ForegroundProcess возвращает имя исполняемого файла активного окна.
//
// Пустая строка без ошибки — нормальный случай: на экране блокировки и при
// переключении рабочих столов активного окна просто нет.
func ForegroundProcess() (string, error) {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return "", nil
	}

	var pid uint32
	procGetWindowThreadProcess.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 {
		return "", nil
	}

	// PROCESS_QUERY_LIMITED_INFORMATION хватает для имени и доступен без прав
	// администратора — это важно, агент работает под учёткой ребёнка.
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		// Процессы уровня системы недоступны обычному пользователю. Это не сбой.
		return "", nil
	}
	defer windows.CloseHandle(h)

	buf := make([]uint16, windows.MAX_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return "", fmt.Errorf("QueryFullProcessImageName: %w", err)
	}
	return filepath.Base(windows.UTF16ToString(buf[:size])), nil
}

// IdleTime — сколько прошло с последнего ввода мышью или клавиатурой.
//
// GetLastInputInfo и GetTickCount64 считают от старта системы, поэтому перевод
// системных часов на это значение не влияет — именно то, что нужно для учёта.
func IdleTime() (time.Duration, error) {
	info := lastInputInfo{cbSize: uint32(unsafe.Sizeof(lastInputInfo{}))}
	r, _, err := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		return 0, fmt.Errorf("GetLastInputInfo: %w", err)
	}

	ticks, _, _ := procGetTickCount64.Call()
	now := uint32(ticks) // сравниваем в той же 32-битной шкале, что и dwTime
	if now < info.dwTime {
		// Счётчик переполнился (примерно раз в 49 суток аптайма).
		return 0, nil
	}
	return time.Duration(now-info.dwTime) * time.Millisecond, nil
}

// SessionLocked сообщает, заблокирован ли рабочий стол.
//
// На экране блокировки активным становится защищённый рабочий стол Winlogon,
// и OpenInputDesktop из обычного процесса на него не пускает. Отсутствие
// доступа и есть признак блокировки.
func SessionLocked() bool {
	const desktopSwitchDesktop = 0x0100
	h, _, _ := procOpenInputDesktop.Call(0, 0, desktopSwitchDesktop)
	if h == 0 {
		return true
	}
	procCloseDesktop.Call(h)
	return false
}

// LockWorkstation блокирует рабочий стол. Самая простая форма принуждения:
// один вызов без окон и цикла сообщений, ломаться нечему.
func LockWorkstation() error {
	r, _, err := procLockWorkStation.Call()
	if r == 0 {
		return fmt.Errorf("LockWorkStation: %w", err)
	}
	return nil
}
