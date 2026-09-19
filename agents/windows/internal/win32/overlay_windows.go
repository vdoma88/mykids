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
	dtWordBreak    = 0x00000010
	dtCalcRect     = 0x00000400
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
	mu   sync.Mutex
	hwnd windows.HWND
	text string
	done chan struct{}
	// compact — это предупреждение полосой, а не закрытый экран.
	compact bool
	classOK bool
}

// Compact сообщает, полоса это или полноэкранный оверлей.
func (o *Overlay) Compact() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.compact
}

var (
	overlayOnce  sync.Once
	overlayClass *uint16
	overlayReg   error
	// Окон бывает два сразу: полоса предупреждения и полноэкранный оверлей.
	// Поэтому не «текущее окно», а таблица: иначе полоса рисовала бы текст
	// оверлея, и предупреждение врало бы ребёнку чужими словами.
	shownMu sync.Mutex
	shown   = map[windows.HWND]*Overlay{}
)

// byWindow находит оверлей, которому принадлежит окно.
func byWindow(hwnd windows.HWND) *Overlay {
	shownMu.Lock()
	defer shownMu.Unlock()
	return shown[hwnd]
}

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

	text, compact := "", false
	if o := byWindow(hwnd); o != nil {
		o.mu.Lock()
		text, compact = o.text, o.compact
		o.mu.Unlock()
	}

	// Полоса — 460 на 120 точек, и заголовок в сорок четыре пункта в неё
	// просто не поместится: подросток увидит обрезанное слово вместо
	// предупреждения.
	height, pad := ^uintptr(43), int32(64) // -44
	if compact {
		height, pad = ^uintptr(21), 16 // -22
	}
	font, _, _ := procCreateFontW.Call(
		height, 0, 0, 0, 600, 0, 0, 0, 0, 0, 0, 0, 0, // полужирный
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("Segoe UI"))))
	if font != 0 {
		procSelectObject.Call(hdc, font)
		defer procDeleteObject.Call(font)
	}

	procSetTextColor.Call(hdc, 0x00F0EAF5)
	procSetBkMode.Call(hdc, transparentBk)

	area := ps.rcPaint
	area.left += pad
	area.right -= pad
	area.top += pad
	area.bottom -= pad

	// DT_VCENTER работает только с одной строкой, а тут их несколько. Поэтому
	// сначала меряем текст, потом сдвигаем прямоугольник: иначе объяснение
	// прижимается к верхней кромке экрана, где его не читают.
	utf16 := windows.StringToUTF16Ptr(text)
	measured := area
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(utf16)), ^uintptr(0),
		uintptr(unsafe.Pointer(&measured)), dtCalcRect|dtCenter|dtWordBreak)
	if h := measured.bottom - measured.top; h < area.bottom-area.top {
		area.top += (area.bottom - area.top - h) / 2
	}

	procDrawTextW.Call(hdc,
		uintptr(unsafe.Pointer(utf16)), ^uintptr(0),
		uintptr(unsafe.Pointer(&area)), dtCenter|dtWordBreak)
}

// ShowOverlay создаёт полноэкранный оверлей и возвращает управление сразу.
func ShowOverlay(text string) (*Overlay, error) {
	return show(text, false)
}

// ShowBar показывает предупреждение полосой внизу справа.
//
// Не во весь экран намеренно: предупреждение говорит «успей сохраниться», и
// перекрыть им экран ровно в тот момент, когда надо спешить, значит сделать
// его бесполезным и обидным.
func ShowBar(text string) (*Overlay, error) {
	return show(text, true)
}

func show(text string, compact bool) (*Overlay, error) {
	overlayOnce.Do(registerClass)
	if overlayReg != nil {
		return nil, overlayReg
	}

	o := &Overlay{text: text, done: make(chan struct{}), classOK: true, compact: compact}
	ready := make(chan error, 1)

	go func() {
		// Цикл сообщений обязан жить в одном потоке ОС со своим окном.
		runtimeLockOSThread()
		defer runtimeUnlockOSThread()
		defer close(o.done)
		// Убираем за собой и когда цикл сообщений оборвался сам, а не по Close:
		// иначе запись в таблице переживёт окно и будет отвечать за чужой hwnd,
		// который Windows выдаст следующему.
		defer func() {
			shownMu.Lock()
			delete(shown, windows.HWND(hwndOf(o)))
			shownMu.Unlock()
		}()

		screenX, _, _ := procGetSystemMetrics.Call(smCxScreen)
		screenY, _, _ := procGetSystemMetrics.Call(smCyScreen)
		x, y, cx, cy := uintptr(0), uintptr(0), screenX, screenY
		if compact {
			// Полоса у нижнего правого угла, с отступом от края.
			cx, cy = 460, 120
			x, y = screenX-cx-32, screenY-cy-64
		}

		var inst windows.Handle
		_ = windows.GetModuleHandleEx(0, nil, &inst)

		hwnd, _, err := procCreateWindowExW.Call(
			wsExTopmost|wsExToolWindow,
			uintptr(unsafe.Pointer(overlayClass)),
			uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("MyKids"))),
			wsPopup, x, y, cx, cy, 0, 0, uintptr(inst), 0)
		if hwnd == 0 {
			ready <- fmt.Errorf("CreateWindowExW: %w", err)
			return
		}

		o.mu.Lock()
		o.hwnd = windows.HWND(hwnd)
		o.mu.Unlock()

		shownMu.Lock()
		shown[windows.HWND(hwnd)] = o
		shownMu.Unlock()

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

	shownMu.Lock()
	delete(shown, hwnd)
	shownMu.Unlock()
}

// hwndOf — окно оверлея, ноль если уже закрыто.
func hwndOf(o *Overlay) uintptr {
	o.mu.Lock()
	defer o.mu.Unlock()
	return uintptr(o.hwnd)
}

// Alive сообщает, жив ли ещё цикл сообщений окна.
//
// Нужно тому, кто показывает: окно могло умереть само — например, цикл
// сообщений оборвался ошибкой. Молча оставить экран открытым в этом случае
// нельзя, поэтому оверлей создаётся заново.
func (o *Overlay) Alive() bool {
	select {
	case <-o.done:
		return false
	default:
		return true
	}
}

// Text возвращает текущую надпись.
func (o *Overlay) Text() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.text
}
