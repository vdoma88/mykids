//go:build windows

package win32

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Окно проверки: несколько кнопок и текстовое поле под отчёт.
//
// Обычные элементы управления Windows, а не своя отрисовка: это окно родитель
// увидит один раз, и выглядеть оно должно как любое другое окно системы.
// Своя отрисовка означала бы свои же ошибки в полосах прокрутки, выделении
// текста и масштабировании — там, где система всё это уже умеет.

var (
	procUpdateWindow        = user32.NewProc("UpdateWindow")
	procSendMessageW        = user32.NewProc("SendMessageW")
	procMoveWindow          = user32.NewProc("MoveWindow")
	procGetClientRect       = user32.NewProc("GetClientRect")
	procLoadCursorW         = user32.NewProc("LoadCursorW")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procEnableWindow        = user32.NewProc("EnableWindow")
	procGetDC               = user32.NewProc("GetDC")
	procReleaseDC           = user32.NewProc("ReleaseDC")
	procGetDeviceCaps       = gdi32.NewProc("GetDeviceCaps")
)

const (
	wsChild         = 0x40000000
	wsVisible       = 0x10000000
	wsVScroll       = 0x00200000
	wsTabStop       = 0x00010000
	wsBorder        = 0x00800000
	wsOverlapped    = 0x00CF0000 // WS_OVERLAPPEDWINDOW
	bsPushButton    = 0x00000000
	esMultiline     = 0x0004
	esReadOnly      = 0x0800
	esAutoVScroll   = 0x0040
	wmCommand       = 0x0111
	wmSize          = 0x0005
	wmSetFont       = 0x0030
	wmSetText       = 0x000C
	emSetSel        = 0x00B1
	emScrollCaret   = 0x00B7
	idcArrow        = 32512
	swShowNormal    = 1
	logPixelsY      = 90
	wmAppLog        = wmApp + 10
	wmAppDone       = wmApp + 11
	colorWindowFace = 5 + 1 // COLOR_WINDOW + 1 для hbrBackground
)

// Button — кнопка окна.
type Button struct {
	Text string
	// Do выполняется в отдельной горутине: окно не должно застывать, пока
	// проверка ждёт три секунды покоя.
	//
	// log дописывает строку в отчёт и безопасен для вызова откуда угодно.
	Do func(log func(string))
}

// App — окно с кнопками и отчётом.
type App struct {
	title   string
	intro   string
	buttons []Button

	hwnd  windows.HWND
	edit  windows.HWND
	ctrls []windows.HWND
	font  uintptr

	mu sync.Mutex
	// text — накопленный отчёт, dirty — есть ли неотрисованные строки.
	text  []string
	dirty bool
	busy  bool
}

// Единственное окно на процесс: оконная процедура статична, а передавать
// указатель через SetWindowLongPtr ради одного окна — лишняя машинерия.
var theApp *App

// NewApp собирает окно. Показывает его RunApp.
func NewApp(title, intro string, buttons []Button) *App {
	return &App{title: title, intro: intro, buttons: buttons}
}

// Log дописывает строку в отчёт. Можно звать из любой горутины.
//
// Сообщение окну шлём только если предыдущее ещё не разобрано: проверка
// выдаёт десятки строк подряд, и перерисовывать весь отчёт на каждую значит
// делать одну и ту же работу десятки раз. Читателю от этого ничего не
// достаётся — он всё равно увидит только последнее состояние.
func (a *App) Log(line string) {
	a.mu.Lock()
	a.text = append(a.text, line)
	pending := a.dirty
	a.dirty = true
	a.mu.Unlock()

	if a.hwnd != 0 && !pending {
		procPostMessageW.Call(uintptr(a.hwnd), wmAppLog, 0, 0)
	}
}

// Report — весь отчёт одной строкой. Нужен для копирования в буфер обмена.
func (a *App) Report() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return strings.Join(a.text, "\n")
}

func (a *App) render() {
	a.mu.Lock()
	text := strings.Join(a.text, "\r\n")
	a.dirty = false
	a.mu.Unlock()

	buf := utf16(text)
	procSendMessageW.Call(uintptr(a.edit), wmSetText, 0, uintptr(unsafe.Pointer(&buf[0])))
	runtime.KeepAlive(buf)
	// Прокручиваем к концу: отчёт читают с последней строки.
	procSendMessageW.Call(uintptr(a.edit), emSetSel, ^uintptr(0), ^uintptr(0))
	procSendMessageW.Call(uintptr(a.edit), emScrollCaret, 0, 0)
}

func (a *App) enable(on bool) {
	var v uintptr
	if on {
		v = 1
	}
	for _, h := range a.ctrls {
		procEnableWindow.Call(uintptr(h), v)
	}
}

// RunApp показывает окно и не возвращается, пока его не закроют.
func RunApp(a *App) error {
	// Окно и его сообщения обязаны жить в одном потоке ОС.
	runtimeLockOSThread()
	defer runtimeUnlockOSThread()

	theApp = a
	if err := a.register(); err != nil {
		return err
	}
	if err := a.create(); err != nil {
		return err
	}

	a.Log(a.intro)
	a.render()

	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return nil
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func (a *App) register() error {
	var inst windows.Handle
	if err := windows.GetModuleHandleEx(0, nil, &inst); err != nil {
		return fmt.Errorf("GetModuleHandleEx: %w", err)
	}
	cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
	class := utf16("MyKidsCheck")

	wc := wndClassEx{
		cbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		lpfnWndProc:   syscall.NewCallback(appProc),
		hInstance:     inst,
		hCursor:       windows.Handle(cursor),
		hbrBackground: windows.Handle(colorWindowFace),
		lpszClassName: &class[0],
	}
	r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	runtime.KeepAlive(class)
	if r == 0 {
		return fmt.Errorf("RegisterClassExW: %w", err)
	}
	return nil
}

func (a *App) create() error {
	var inst windows.Handle
	_ = windows.GetModuleHandleEx(0, nil, &inst)

	class, title := utf16("MyKidsCheck"), utf16(a.title)
	hwnd, _, err := procCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(&class[0])),
		uintptr(unsafe.Pointer(&title[0])),
		wsOverlapped|wsVisible,
		uintptr(0x80000000), uintptr(0x80000000), 960, 640, // CW_USEDEFAULT
		0, 0, uintptr(inst), 0)
	runtime.KeepAlive(class)
	runtime.KeepAlive(title)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowExW: %w", err)
	}
	a.hwnd = windows.HWND(hwnd)
	a.font = guiFont()

	button := utf16("BUTTON")
	for i, b := range a.buttons {
		caption := utf16(b.Text)
		h, _, _ := procCreateWindowExW.Call(0,
			uintptr(unsafe.Pointer(&button[0])),
			uintptr(unsafe.Pointer(&caption[0])),
			wsChild|wsVisible|wsTabStop|bsPushButton,
			0, 0, 10, 10, hwnd, uintptr(100+i), uintptr(inst), 0)
		runtime.KeepAlive(caption)
		a.ctrls = append(a.ctrls, windows.HWND(h))
		procSendMessageW.Call(h, wmSetFont, a.font, 1)
	}

	editClass := utf16("EDIT")
	edit, _, _ := procCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(&editClass[0])),
		0,
		wsChild|wsVisible|wsVScroll|wsBorder|wsTabStop|esMultiline|esReadOnly|esAutoVScroll,
		0, 0, 10, 10, hwnd, 1, uintptr(inst), 0)
	runtime.KeepAlive(button)
	runtime.KeepAlive(editClass)
	a.edit = windows.HWND(edit)
	procSendMessageW.Call(edit, wmSetFont, a.font, 1)

	a.layout()
	procShowWindow.Call(hwnd, swShowNormal)
	procUpdateWindow.Call(hwnd)
	procSetForegroundWindow.Call(hwnd)
	return nil
}

// guiFont — шрифт интерфейса под текущий масштаб экрана.
//
// Без пересчёта под DPI на экране с масштабом 150% подписи не помещаются
// в кнопки, и окно выглядит сломанным.
func guiFont() uintptr {
	dc, _, _ := procGetDC.Call(0)
	dpi, _, _ := procGetDeviceCaps.Call(dc, logPixelsY)
	procReleaseDC.Call(0, dc)
	if dpi == 0 {
		dpi = 96
	}
	height := -int32(10 * dpi / 72)
	face := utf16("Segoe UI")
	f, _, _ := procCreateFontW.Call(
		uintptr(height), 0, 0, 0, 400, 0, 0, 0, 0, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(&face[0])))
	runtime.KeepAlive(face)
	return f
}

// layout раскладывает кнопки рядами сверху, отчёт — на всё остальное.
func (a *App) layout() {
	var rc rect
	procGetClientRect.Call(uintptr(a.hwnd), uintptr(unsafe.Pointer(&rc)))
	width := rc.right - rc.left
	if width <= 0 {
		return
	}

	const pad, rowH, gap = 12, 34, 8
	x, y := int32(pad), int32(pad)

	// Ширину кнопки считаем по длине подписи: русские надписи длиннее
	// английских, и фиксированная ширина обрезала бы их.
	//
	// Не поместившиеся переносим на следующий ряд — вместе с y. Перенос
	// без переноса y укладывает кнопку поверх первой, и одна из них
	// становится недоступна: так и случилось на первом же запуске, где
	// «Проверить всё» оказалась накрыта «Скопировать отчёт».
	for i, b := range a.buttons {
		w := int32(len([]rune(b.Text))*9 + 34)
		if x > pad && x+w > width-pad {
			x, y = pad, y+rowH+gap
		}
		procMoveWindow.Call(uintptr(a.ctrls[i]), uintptr(x), uintptr(y), uintptr(w), rowH, 1)
		x += w + gap
	}

	top := y + rowH + pad
	height := rc.bottom - top - pad
	if height < 0 {
		height = 0
	}
	procMoveWindow.Call(uintptr(a.edit), pad, uintptr(top),
		uintptr(width-2*pad), uintptr(height), 1)
}

func appProc(hwnd windows.HWND, message uint32, wParam, lParam uintptr) uintptr {
	a := theApp
	switch message {
	case wmSize:
		if a != nil {
			a.layout()
		}
		return 0
	case wmAppLog:
		if a != nil {
			a.render()
		}
		return 0
	case wmAppDone:
		if a != nil {
			a.mu.Lock()
			a.busy = false
			a.mu.Unlock()
			// Последние строки могли прийти, пока окно рисовало предыдущие.
			a.render()
			a.enable(true)
		}
		return 0
	case wmCommand:
		if a != nil {
			a.click(int(wParam & 0xFFFF))
		}
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(message), wParam, lParam)
	return r
}

func (a *App) click(id int) {
	i := id - 100
	if i < 0 || i >= len(a.buttons) {
		return
	}

	a.mu.Lock()
	if a.busy {
		a.mu.Unlock()
		return
	}
	a.busy = true
	a.mu.Unlock()

	// Кнопки гасим на время работы: второй запуск поверх первого перемешал
	// бы отчёт, и понять в нём что-нибудь стало бы нельзя.
	a.enable(false)
	do := a.buttons[i].Do
	go func() {
		defer procPostMessageW.Call(uintptr(a.hwnd), wmAppDone, 0, 0)
		// Окно, которое молча исчезает, — худшее, что может случиться с
		// программой, которую запустили как раз чтобы понять, что не так.
		// Поэтому сбой проверки попадает в отчёт, а не уносит окно с собой.
		defer func() {
			if v := recover(); v != nil {
				a.Log("")
				a.Log(fmt.Sprintf("[ !! ] Внутренняя ошибка: %v", v))
				a.Log("    что делать: пришлите отчёт целиком — это ошибка самой проверки.")
				a.Log(string(debug.Stack()))
			}
		}()
		if do != nil {
			do(a.Log)
		}
	}()
}
