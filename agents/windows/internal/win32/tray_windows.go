//go:build windows

package win32

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"github.com/vdoma88/mykids/agents/windows/internal/screen"
	"golang.org/x/sys/windows"
)

// Значок в трее.
//
// Здесь только рисование: что написать в подписи и какие пункты показать,
// решает internal/screen, и это проверено тестами на любой ОС. Причина та же,
// что и у экрана: слова — это разговор с ребёнком, и они не должны зависеть от
// того, какая из двух программ их сочиняет.
//
// Окно у значка есть, хотя его и не видно: Shell_NotifyIcon шлёт щелчки
// обычным оконным сообщением, и принять его без окна негде.

var (
	shell32              = windows.NewLazySystemDLL("shell32.dll")
	procShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")
	procShellExecuteW    = shell32.NewProc("ShellExecuteW")

	procCreatePopupMenu    = user32.NewProc("CreatePopupMenu")
	procAppendMenuW        = user32.NewProc("AppendMenuW")
	procDestroyMenu        = user32.NewProc("DestroyMenu")
	procTrackPopupMenu     = user32.NewProc("TrackPopupMenu")
	procGetCursorPos       = user32.NewProc("GetCursorPos")
	procLoadIconW          = user32.NewProc("LoadIconW")
	procSetMenuDefaultItem = user32.NewProc("SetMenuDefaultItem")
)

const (
	nimAdd    = 0x00000000
	nimModify = 0x00000001
	nimDelete = 0x00000002

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	// wmTrayIcon — своё сообщение для щелчков по значку. Номер из диапазона
	// WM_APP, который система оставляет программам.
	wmTrayIcon   = 0x0400 + 200
	wmAppRefresh = 0x0400 + 201
	wmAppRemove  = 0x0400 + 202

	wmRButtonUp = 0x0205
	wmLButtonUp = 0x0202

	mfString    = 0x00000000
	mfSeparator = 0x00000800
	mfGrayed    = 0x00000001

	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100
	tpmNonotify    = 0x0080
	idiApplication = 32512

	// firstItemID — с какого номера нумеруем пункты меню. Ноль TrackPopupMenu
	// возвращает при отмене, и путать его с пунктом нельзя.
	firstItemID = 100
)

type notifyIconData struct {
	cbSize           uint32
	hWnd             windows.HWND
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            windows.Handle
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         windows.GUID
	hBalloonIcon     windows.Handle
}

type point struct{ x, y int32 }

// Tray — значок, пока он висит.
type Tray struct {
	hwnd windows.HWND
	mu   sync.Mutex
	// state — что показывать. Читает его поток окна, пишет — помощник.
	state screen.Tray
	dead  bool
}

var (
	trayOnce  sync.Once
	trayClass *uint16
	trayName  []uint16
	trayReg   error

	trayMu sync.Mutex
	trayOf = map[windows.HWND]*Tray{}
)

// ShowTray вешает значок в трее и возвращает управление.
//
// Окно и цикл сообщений живут в своей горутине, прибитой к потоку: оконные
// сообщения приходят тому потоку, который окно создал, и без LockOSThread Go
// переставил бы горутину на другой поток, а щелчки перестали бы доходить.
func ShowTray(s screen.Tray) (*Tray, error) {
	trayOnce.Do(registerTrayClass)
	if trayReg != nil {
		return nil, trayReg
	}

	t := &Tray{state: s}
	ready := make(chan error, 1)

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		hwnd, err := createTrayWindow()
		if err != nil {
			ready <- err
			return
		}
		t.hwnd = hwnd

		trayMu.Lock()
		trayOf[hwnd] = t
		trayMu.Unlock()
		defer func() {
			trayMu.Lock()
			delete(trayOf, hwnd)
			trayMu.Unlock()
		}()

		if err := t.notify(nimAdd); err != nil {
			procDestroyWindow.Call(uintptr(hwnd))
			ready <- err
			return
		}
		ready <- nil

		var m msg
		for {
			r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if r == 0 || int32(r) == -1 {
				break
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}

		t.notify(nimDelete)
		t.mu.Lock()
		t.dead = true
		t.mu.Unlock()
	}()

	if err := <-ready; err != nil {
		return nil, err
	}
	return t, nil
}

// Update меняет подпись и меню. Значок при этом не мигает и не создаётся
// заново: помощник зовёт это на каждом замере.
func (t *Tray) Update(s screen.Tray) {
	t.mu.Lock()
	same := sameTray(t.state, s)
	t.state = s
	t.mu.Unlock()
	if same {
		return
	}
	procPostMessageW.Call(uintptr(t.hwnd), wmAppRefresh, 0, 0)
}

// Close убирает значок.
func (t *Tray) Close() {
	if t == nil {
		return
	}
	procPostMessageW.Call(uintptr(t.hwnd), wmAppRemove, 0, 0)
}

// Alive — висит ли значок ещё.
func (t *Tray) Alive() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return !t.dead
}

func (t *Tray) snapshot() screen.Tray {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.state
}

// notify отправляет значку команду Shell_NotifyIcon.
func (t *Tray) notify(action uint32) error {
	icon, _, _ := procLoadIconW.Call(0, uintptr(idiApplication))

	data := notifyIconData{
		cbSize:           uint32(unsafe.Sizeof(notifyIconData{})),
		hWnd:             t.hwnd,
		uID:              1,
		uFlags:           nifMessage | nifIcon | nifTip,
		uCallbackMessage: wmTrayIcon,
		hIcon:            windows.Handle(icon),
	}
	// Подпись обрезана ещё в screen по числу знаков; здесь остаётся уложить её
	// в массив, оставив место завершающему нулю.
	tip := utf16(t.snapshot().Tip)
	copy(data.szTip[:len(data.szTip)-1], tip)

	r, _, err := procShellNotifyIconW.Call(uintptr(action), uintptr(unsafe.Pointer(&data)))
	runtime.KeepAlive(tip)
	runtime.KeepAlive(&data)
	if r == 0 && action != nimDelete {
		return fmt.Errorf("Shell_NotifyIcon: %w", err)
	}
	return nil
}

func registerTrayClass() {
	trayName = utf16("MyKidsTray")
	trayClass = &trayName[0]
	var inst windows.Handle
	if err := windows.GetModuleHandleEx(0, nil, &inst); err != nil {
		trayReg = fmt.Errorf("GetModuleHandleEx: %w", err)
		return
	}
	wc := wndClassEx{
		cbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		lpfnWndProc:   syscall.NewCallback(trayProc),
		hInstance:     inst,
		lpszClassName: trayClass,
	}
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		trayReg = fmt.Errorf("RegisterClassExW: %w", err)
	}
	runtime.KeepAlive(trayName)
}

// createTrayWindow делает окно, которого не видно: оно нужно только затем,
// чтобы было куда присылать щелчки по значку.
func createTrayWindow() (windows.HWND, error) {
	title := utf16("MyKids")
	hwnd, _, err := procCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(trayClass)),
		uintptr(unsafe.Pointer(&title[0])),
		0, 0, 0, 0, 0, 0, 0, 0, 0)
	runtime.KeepAlive(title)
	if hwnd == 0 {
		return 0, fmt.Errorf("CreateWindowExW: %w", err)
	}
	return windows.HWND(hwnd), nil
}

func trayProc(hwnd windows.HWND, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmTrayIcon:
		// И правая, и левая кнопка открывают меню. Левая — потому что
		// единственное действие значка и есть это меню: значок, который на
		// щелчок не отвечает ничем, читается как сломанный.
		switch uint32(lParam) {
		case wmRButtonUp, wmLButtonUp:
			if t := trayByWindow(hwnd); t != nil {
				t.popup()
			}
		}
		return 0

	case wmAppRefresh:
		if t := trayByWindow(hwnd); t != nil {
			t.notify(nimModify)
		}
		return 0

	case wmAppRemove:
		procDestroyWindow.Call(uintptr(hwnd))
		return 0

	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(message), wParam, lParam)
	return r
}

// popup показывает меню и выполняет выбранное.
func (t *Tray) popup() {
	items := t.snapshot().Items

	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)

	for i, it := range items {
		if it.Separator {
			procAppendMenuW.Call(menu, mfSeparator, 0, 0)
			continue
		}
		flags := uintptr(mfString)
		if it.URL == "" {
			// Строка только для чтения: серая, но видимая. Убирать её нельзя —
			// ради остатка времени значок и открывают.
			flags |= mfGrayed
		}
		label := utf16(it.Label)
		procAppendMenuW.Call(menu, flags, uintptr(firstItemID+i),
			uintptr(unsafe.Pointer(&label[0])))
		runtime.KeepAlive(label)
	}

	// Меню трея закрывается по щелчку мимо только если его окно — переднее.
	// Иначе оно повисает на экране до следующего щелчка по значку.
	procSetForegroundWindow.Call(uintptr(t.hwnd))

	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))

	chosen, _, _ := procTrackPopupMenu.Call(menu,
		tpmRightButton|tpmReturnCmd|tpmNonotify,
		uintptr(pt.x), uintptr(pt.y), 0, uintptr(t.hwnd), 0)
	runtime.KeepAlive(&pt)

	if chosen == 0 {
		return // отменили
	}
	if i := int(chosen) - firstItemID; i >= 0 && i < len(items) && items[i].URL != "" {
		openURL(items[i].URL)
	}
}

// openURL открывает адрес в браузере по умолчанию.
func openURL(url string) {
	verb := utf16("open")
	target := utf16(url)
	procShellExecuteW.Call(0,
		uintptr(unsafe.Pointer(&verb[0])),
		uintptr(unsafe.Pointer(&target[0])),
		0, 0, swShowNormal)
	runtime.KeepAlive(verb)
	runtime.KeepAlive(target)
}

func trayByWindow(hwnd windows.HWND) *Tray {
	trayMu.Lock()
	defer trayMu.Unlock()
	return trayOf[hwnd]
}

// sameTray — совпадают ли подпись и меню. Нужна, чтобы не дёргать значок
// впустую: помощник зовёт Update раз в несколько секунд, а меняется она раз
// в минуту.
func sameTray(a, b screen.Tray) bool {
	if a.Tip != b.Tip || len(a.Items) != len(b.Items) {
		return false
	}
	for i := range a.Items {
		if a.Items[i] != b.Items[i] {
			return false
		}
	}
	return true
}
