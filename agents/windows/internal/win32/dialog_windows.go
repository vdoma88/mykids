//go:build windows

package win32

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procMessageBoxW      = user32.NewProc("MessageBoxW")
	procOpenClipboard    = user32.NewProc("OpenClipboard")
	procCloseClipboard   = user32.NewProc("CloseClipboard")
	procEmptyClipboard   = user32.NewProc("EmptyClipboard")
	procSetClipboardData = user32.NewProc("SetClipboardData")
	procGlobalAlloc      = kernel32.NewProc("GlobalAlloc")
	procGlobalLock       = kernel32.NewProc("GlobalLock")
	procGlobalUnlock     = kernel32.NewProc("GlobalUnlock")
	procGlobalFree       = kernel32.NewProc("GlobalFree")
	procRtlMoveMemory    = kernel32.NewProc("RtlMoveMemory")
)

const (
	mbYesNo       = 0x00000004
	mbIconQuest   = 0x00000020
	mbTopMost     = 0x00040000
	idYes         = 6
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
)

// Ask задаёт вопрос «да/нет» и возвращает ответ.
//
// Нужен для проверок, которые машина сама проверить не может: оверлей либо
// появился на экране, либо нет, и знает об этом только человек.
func Ask(title, text string) bool {
	// Поверх всех окон: вопрос задаётся сразу после полноэкранного оверлея,
	// и спрятать его за ним — верный способ подвесить проверку.
	body, head := utf16(text), utf16(title)
	r, _, _ := procMessageBoxW.Call(0,
		uintptr(unsafe.Pointer(&body[0])),
		uintptr(unsafe.Pointer(&head[0])),
		mbYesNo|mbIconQuest|mbTopMost)
	runtime.KeepAlive(body)
	runtime.KeepAlive(head)
	return r == idYes
}

// CopyToClipboard кладёт текст в буфер обмена.
//
// Отчёт нужен не на экране, а в сообщении родителя: без копирования его
// пришлось бы переписывать руками, а значит — не переписывать вовсе.
func CopyToClipboard(s string) error {
	buf, err := windows.UTF16FromString(s)
	if err != nil {
		return err
	}
	size := uintptr(len(buf) * 2)

	mem, _, err := procGlobalAlloc.Call(gmemMoveable, size)
	if mem == 0 {
		return fmt.Errorf("GlobalAlloc: %w", err)
	}
	// Освобождаем только до передачи в буфер: после SetClipboardData память
	// принадлежит системе, и освобождать её нельзя.
	owned := false
	defer func() {
		if !owned {
			procGlobalFree.Call(mem)
		}
	}()

	ptr, _, _ := procGlobalLock.Call(mem)
	if ptr == 0 {
		return fmt.Errorf("GlobalLock не удался")
	}
	// Копируем системным вызовом, а не превращая число в указатель: адрес,
	// пришедший из GlobalLock, для сборщика мусора Go не указатель, и
	// притворяться, что указатель, — способ однажды получить мусор в буфере.
	procRtlMoveMemory.Call(ptr, uintptr(unsafe.Pointer(&buf[0])), size)
	runtime.KeepAlive(buf)
	procGlobalUnlock.Call(mem)

	if r, _, err := procOpenClipboard.Call(0); r == 0 {
		return fmt.Errorf("буфер обмена занят другой программой: %w", err)
	}
	defer procCloseClipboard.Call()

	procEmptyClipboard.Call()
	if r, _, err := procSetClipboardData.Call(cfUnicodeText, mem); r == 0 {
		return fmt.Errorf("SetClipboardData: %w", err)
	}
	owned = true
	return nil
}
