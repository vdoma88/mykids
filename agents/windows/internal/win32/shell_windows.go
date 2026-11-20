//go:build windows

package win32

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Запуск других программ и ссылок так, как это делает Проводник.
//
// Всё, что родитель делает в MyKids, должно делаться мышью. Командная строка —
// это знание, которое к воспитанию детей отношения не имеет, и каждое место,
// где оно требовалось, было местом, где установка или проверка обрывались.

var (
	procLoadImageW = user32.NewProc("LoadImageW")
)

const (
	imageIcon      = 1
	lrLoadFromFile = 0x00000010
	lrDefaultSize  = 0x00000040
	wmSetIcon      = 0x0080
	mbOK           = 0x00000000
	mbIconInfo     = 0x00000040
	mbIconWarning  = 0x00000030
	mbIconError    = 0x00000010
	mbOKCancel     = 0x00000001
	idOK           = 1
)

func shellExecute(verb, file, args string) error {
	v, f := utf16(verb), utf16(file)
	var a *uint16
	if args != "" {
		a = &utf16(args)[0]
	}
	r, _, _ := procShellExecuteW.Call(0,
		uintptr(unsafe.Pointer(&v[0])),
		uintptr(unsafe.Pointer(&f[0])),
		uintptr(unsafe.Pointer(a)),
		0, swShowNormal)
	runtime.KeepAlive(v)
	runtime.KeepAlive(f)
	runtime.KeepAlive(a)
	// ShellExecute возвращает число больше 32 при успехе — так уж она устроена.
	if r <= 32 {
		return fmt.Errorf("ShellExecute %s: код %d", file, r)
	}
	return nil
}

// Open открывает ссылку или файл программой по умолчанию.
func Open(target string) error { return shellExecute("open", target, "") }

// RunElevated запускает программу с правами администратора: Windows сама
// спросит разрешение. Нужен окнам, которые управляют службой, — и запускать
// их правой кнопкой «от имени администратора» родитель не обязан помнить.
func RunElevated(exe, args string) error { return shellExecute("runas", exe, args) }

// Start запускает программу без повышения прав и не ждёт её.
func Start(exe, args string) error { return shellExecute("open", exe, args) }

// Elevated — запущен ли процесс с правами администратора.
func Elevated() bool { return windows.GetCurrentProcessToken().IsElevated() }

func messageBox(title, text string, flags uintptr) uintptr {
	body, head := utf16(text), utf16(title)
	r, _, _ := procMessageBoxW.Call(0,
		uintptr(unsafe.Pointer(&body[0])),
		uintptr(unsafe.Pointer(&head[0])),
		flags|mbTopMost)
	runtime.KeepAlive(body)
	runtime.KeepAlive(head)
	return r
}

// Info — сообщение с одной кнопкой «ОК».
func Info(title, text string) { messageBox(title, text, mbOK|mbIconInfo) }

// Warn — предупреждение с одной кнопкой «ОК».
func Warn(title, text string) { messageBox(title, text, mbOK|mbIconWarning) }

// Fail — сообщение об ошибке с одной кнопкой «ОК».
func Fail(title, text string) { messageBox(title, text, mbOK|mbIconError) }

// Confirm — «ОК / Отмена»; истина, если нажали «ОК».
func Confirm(title, text string) bool {
	return messageBox(title, text, mbOKCancel|mbIconQuest) == idOK
}

// loadIcon читает иконку из файла. Нуль, если файла нет: окно без своей
// иконки хуже, но работает.
func loadIcon(path string) uintptr {
	if path == "" {
		return 0
	}
	p := utf16(path)
	h, _, _ := procLoadImageW.Call(0, uintptr(unsafe.Pointer(&p[0])), imageIcon, 0, 0,
		lrLoadFromFile|lrDefaultSize)
	runtime.KeepAlive(p)
	return h
}
