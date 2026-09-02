//go:build windows

package win32

import (
	"fmt"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	gdi32                = windows.NewLazySystemDLL("gdi32.dll")
	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procShowWindow       = user32.NewProc("ShowWindow")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procPostMessageW     = user32.NewProc("PostMessageW")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	procBeginPaint       = user32.NewProc("BeginPaint")
	procEndPaint         = user32.NewProc("EndPaint")
	procDrawTextW        = user32.NewProc("DrawTextW")
	procFillRect         = user32.NewProc("FillRect")
	procSetTextColor     = gdi32.NewProc("SetTextColor")
	procSetBkMode        = gdi32.NewProc("SetBkMode")
	procCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")
	procDeleteObject     = gdi32.NewProc("DeleteObject")
	procCreateFontW      = gdi32.NewProc("CreateFontW")
	procSelectObject     = gdi32.NewProc("SelectObject")
	procInvalidateRect   = user32.NewProc("InvalidateRect")
	procSetWindowPos     = user32.NewProc("SetWindowPos")
)

const (
	wsExTopmost    = 0x00000008
	wsExToolWindow = 0x00000080
	wsPopup        = 0x80000000
	swShow         = 5
	wmDestroy      = 0x0002
	wmPaint        = 0x000F
	wmClose        = 0x0010
	wmApp          = 0x8000
	wmAppClose     = wmApp + 1
	wmAppRepaint   = wmApp + 2
	smCxScreen     = 0
	smCyScreen     = 1
	dtCenter       = 0x00000001
	dtVCenter      = 0x00000004
	dtWordBreak    = 0x00000010
	transparentBk  = 1
	hwndTopmost    = ^uintptr(0) // (HWND)-1
	swpNoMove      = 0x0002
	swpNoSize      = 0x0001
	swpNoActivate  = 0x0010
)

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     windows.Handle
	hIcon         windows.Handle
	hCursor       windows.Handle
	hbrBackground windows.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       windows.Handle
}

type msg struct {
	hwnd    windows.HWND
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
}

type rect struct{ left, top, right, bottom int32 }

type paintStruct struct {
	hdc         windows.Handle
	fErase      int32
	rcPaint     rect
	fRestore    int32
	fIncUpdate  int32
	rgbReserved [32]byte
}

// Overlay — полноэкранное окно поверх всего, закрывающее экран при блокировке.
//
// Живёт в своей горутине с собственным циклом сообщений: Windows требует,
// чтобы сообщения окна обрабатывались в том же потоке, где оно создано.
type Overlay struct {
	mu      sync.Mutex
	hwnd    windows.HWND
	text    string
	done    chan struct{}
	classOK bool
}

var (
	overlayOnce  sync.Once
	overlayClass *uint16
	overlayReg   error
	activeMu     sync.Mutex
	active       *Overlay
)

func registerClass() {
	overlayClass = windows.StringToUTF16Ptr("MyKidsOverlay")
	var inst windows.Handle
	if err := windows.GetModuleHandleEx(0, nil, &inst); err != nil {
		overlayReg = fmt.Errorf("GetModuleHandleEx: %w", err)
		return
	}
	brush, _, _ := procCreateSolidBrush.Call(0x00291E1B) // тёмно-баклажановый в BGR

	wc := wndClassEx{
		cbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		lpfnWndProc:   syscall.NewCallback(wndProc),
		hInstance:     inst,
		hbrBackground: windows.Handle(brush),
		lpszClassName: overlayClass,
	}
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		overlayReg = fmt.Errorf("RegisterClassExW: %w", err)
	}
}

func wndProc(hwnd windows.HWND, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmClose:
		// Оверлей не закрывается по Alt+F4: в этом весь смысл блокировки.
		return 0
	case wmAppClose:
		procDestroyWindow.Call(uintptr(hwnd))
		return 0
	case wmAppRepaint:
		procInvalidateRect.Call(uintptr(hwnd), 0, 1)
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	case wmPaint:
		paintOverlay(hwnd)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(message), wParam, lParam)
	return r
}

func paintOverlay(hwnd windows.HWND) {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
	defer procEndPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))

	brush, _, _ := procCreateSolidBrush.Call(0x00291E1B)
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&ps.rcPaint)), brush)
	procDeleteObject.Call(brush)

	font, _, _ := procCreateFontW.Call(
		^uintptr(43), 0, 0, 0, 600, 0, 0, 0, 0, 0, 0, 0, 0, // высота -44, полужирный
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("Segoe UI"))))
	if font != 0 {
		procSelectObject.Call(hdc, font)
		defer procDeleteObject.Call(font)
	}

	procSetTextColor.Call(hdc, 0x00F0EAF5)
	procSetBkMode.Call(hdc, transparentBk)

	activeMu.Lock()
	text := ""
	if active != nil {
		active.mu.Lock()
		text = active.text
		active.mu.Unlock()
	}
	activeMu.Unlock()

	area := ps.rcPaint
	procDrawTextW.Call(hdc,
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(text))), ^uintptr(0),
		uintptr(unsafe.Pointer(&area)), dtCenter|dtVCenter|dtWordBreak)
}

// ShowOverlay создаёт полноэкранный оверлей и возвращает управление сразу.
func ShowOverlay(text string) (*Overlay, error) {
	overlayOnce.Do(registerClass)
	if overlayReg != nil {
		return nil, overlayReg
	}

	o := &Overlay{text: text, done: make(chan struct{}), classOK: true}
	ready := make(chan error, 1)

	go func() {
		// Цикл сообщений обязан жить в одном потоке ОС со своим окном.
		runtimeLockOSThread()
		defer runtimeUnlockOSThread()
		defer close(o.done)

		cx, _, _ := procGetSystemMetrics.Call(smCxScreen)
		cy, _, _ := procGetSystemMetrics.Call(smCyScreen)
		var inst windows.Handle
		_ = windows.GetModuleHandleEx(0, nil, &inst)

		hwnd, _, err := procCreateWindowExW.Call(
			wsExTopmost|wsExToolWindow,
			uintptr(unsafe.Pointer(overlayClass)),
			uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("MyKids"))),
			wsPopup, 0, 0, cx, cy, 0, 0, uintptr(inst), 0)
		if hwnd == 0 {
			ready <- fmt.Errorf("CreateWindowExW: %w", err)
			return
		}

		o.mu.Lock()
		o.hwnd = windows.HWND(hwnd)
		o.mu.Unlock()

		activeMu.Lock()
		active = o
		activeMu.Unlock()

		procShowWindow.Call(hwnd, swShow)
		procSetWindowPos.Call(hwnd, hwndTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
		ready <- nil

		var m msg
		for {
			r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if int32(r) <= 0 {
				return
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}
	}()

	if err := <-ready; err != nil {
		return nil, err
	}
	return o, nil
}

// SetText меняет надпись на оверлее.
func (o *Overlay) SetText(text string) {
	o.mu.Lock()
	o.text = text
	hwnd := o.hwnd
	o.mu.Unlock()
	if hwnd != 0 {
		procPostMessageW.Call(uintptr(hwnd), wmAppRepaint, 0, 0)
	}
}

// Close убирает оверлей и дожидается завершения его цикла сообщений.
func (o *Overlay) Close() {
	o.mu.Lock()
	hwnd := o.hwnd
	o.hwnd = 0
	o.mu.Unlock()
	if hwnd == 0 {
		return
	}
	procPostMessageW.Call(uintptr(hwnd), wmAppClose, 0, 0)
	<-o.done

	activeMu.Lock()
	if active == o {
		active = nil
	}
	activeMu.Unlock()
}
