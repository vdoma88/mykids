//go:build windows

package win32

import (
	"runtime"
	"strings"
	utf16pkg "unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	comdlg32             = windows.NewLazySystemDLL("comdlg32.dll")
	ole32                = windows.NewLazySystemDLL("ole32.dll")
	procGetOpenFileNameW = comdlg32.NewProc("GetOpenFileNameW")
	procCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	procCoUninitialize   = ole32.NewProc("CoUninitialize")
)

// openFileName — OPENFILENAMEW. Порядок и типы полей — как в commdlg.h:
// размер структуры Windows проверяет, и неверный означает молча не
// открывшееся окно.
type openFileName struct {
	lStructSize       uint32
	hwndOwner         uintptr
	hInstance         uintptr
	lpstrFilter       *uint16
	lpstrCustomFilter *uint16
	nMaxCustFilter    uint32
	nFilterIndex      uint32
	lpstrFile         *uint16
	nMaxFile          uint32
	lpstrFileTitle    *uint16
	nMaxFileTitle     uint32
	lpstrInitialDir   *uint16
	lpstrTitle        *uint16
	flags             uint32
	nFileOffset       uint16
	nFileExtension    uint16
	lpstrDefExt       *uint16
	lCustData         uintptr
	lpfnHook          uintptr
	lpTemplateName    *uint16
	pvReserved        uintptr
	dwReserved        uint32
	flagsEx           uint32
}

const (
	ofnFileMustExist = 0x00001000
	ofnPathMustExist = 0x00000800
	ofnNoChangeDir   = 0x00000008
	ofnExplorer      = 0x00080000
	coinitApartment  = 0x2
)

// OpenFile — обычное окно «Открыть» Windows. pattern — например "*.dump".
// Второе значение ложно, если окно закрыли, ничего не выбрав.
func OpenFile(title, kind, pattern, initialDir string) (string, bool) {
	// Окно выбора файла — объект COM, и потоку нужна своя квартира COM.
	// Кнопки окна управления работают в горутинах, поэтому поток
	// закрепляем на время вызова.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	procCoInitializeEx.Call(0, coinitApartment)
	defer procCoUninitialize.Call()

	// Фильтр — пары «подпись\0шаблон\0», в конце ещё один \0.
	// windows.StringToUTF16 здесь не годится: на нулевом символе внутри
	// строки он паникует, а фильтр из них и состоит.
	filter := append(utf16pkg.Encode([]rune(kind+" ("+pattern+")\x00"+pattern+"\x00Все файлы\x00*.*\x00")), 0)
	file := make([]uint16, 1024)
	t := utf16(title)
	dir := utf16(initialDir)

	ofn := openFileName{
		lpstrFilter:     &filter[0],
		lpstrFile:       &file[0],
		nMaxFile:        uint32(len(file)),
		lpstrTitle:      &t[0],
		lpstrInitialDir: &dir[0],
		flags:           ofnFileMustExist | ofnPathMustExist | ofnNoChangeDir | ofnExplorer,
	}
	ofn.lStructSize = uint32(unsafe.Sizeof(ofn))
	r, _, _ := procGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	runtime.KeepAlive(filter)
	runtime.KeepAlive(file)
	runtime.KeepAlive(t)
	runtime.KeepAlive(dir)
	if r == 0 {
		return "", false
	}
	path := windows.UTF16ToString(file)
	return path, strings.TrimSpace(path) != ""
}
