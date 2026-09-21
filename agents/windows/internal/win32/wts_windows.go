//go:build windows

package win32

import (
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Встречная проверка состояния сессии — единственное, что служба может узнать
// о рабочем столе ребёнка, не спрашивая помощника.
//
// OpenInputDesktop, которым пользуется SessionLocked, работает только изнутри
// сессии: службе в нулевой сессии он всегда отвечает «заблокировано». WTS —
// наоборот: спрашивает не рабочий стол, а диспетчер сессий, и отвечает про
// любую сессию, в том числе чужую.

var (
	wtsapi32                       = windows.NewLazySystemDLL("wtsapi32.dll")
	procWTSQuerySessionInformation = wtsapi32.NewProc("WTSQuerySessionInformationW")
	procWTSFreeMemory              = wtsapi32.NewProc("WTSFreeMemory")
)

const (
	// wtsSessionInfoEx — класс WTS_INFO_CLASS, в котором лежит состояние блокировки.
	wtsSessionInfoEx = 25
	// wtsCurrentServer — дескриптор «этот компьютер».
	wtsCurrentServer = 0

	// Флаги состояния сессии из WTSINFOEX_LEVEL1.
	wtsSessionStateLock    = 0
	wtsSessionStateUnlock  = 1
	wtsSessionStateUnknown = 0xFFFFFFFF
)

// wtsInfoExLevel1 — WTSINFOEX_LEVEL1_W.
//
// Нужно из неё одно поле — SessionFlags, — но добраться до него можно только
// разложив всю структуру: смещение считает компилятор, а не мы. Поля названы
// как в Windows, чтобы их можно было сверить с документацией глазами.
type wtsInfoExLevel1 struct {
	SessionID             uint32
	SessionState          uint32
	SessionFlags          int32
	WinStationName        [33]uint16
	UserName              [21]uint16
	DomainName            [18]uint16
	LogonTime             int64
	ConnectTime           int64
	DisconnectTime        int64
	LastInputTime         int64
	CurrentTime           int64
	IncomingBytes         uint32
	OutgoingBytes         uint32
	IncomingFrames        uint32
	OutgoingFrames        uint32
	IncomingCompressedSze uint32
	OutgoingCompressedSze uint32
}

// wtsInfoEx — WTSINFOEX_W: номер уровня и объединение, у которого уровень
// пока ровно один.
type wtsInfoEx struct {
	Level uint32
	// Выравнивание: за uint32 в объединении идёт структура с полями по 8
	// байт, и компилятор Windows вставляет сюда четыре байта. Без них всё
	// поле уезжает, и SessionFlags читается из середины SessionID.
	_    uint32
	Data wtsInfoExLevel1
}

// SessionLockedWTS — заблокирован ли экран в сессии, по данным диспетчера сессий.
//
// Второе значение — удалось ли узнать. Ложь в нём означает «неизвестно», а не
// «открыт»: решение, что делать с незнанием, принимается не здесь.
func SessionLockedWTS(session uint32) (locked bool, known bool) {
	var buf *wtsInfoEx
	var size uint32

	r, _, _ := procWTSQuerySessionInformation.Call(
		uintptr(wtsCurrentServer),
		uintptr(session),
		uintptr(wtsSessionInfoEx),
		uintptr(unsafe.Pointer(&buf)),
		uintptr(unsafe.Pointer(&size)),
	)
	// Указатели живут до конца вызова: превращённые в uintptr, для сборщика
	// мусора они указателями быть перестают, а правило «аргумент системного
	// вызова продлевает жизнь» через LazyProc.Call не действует.
	runtime.KeepAlive(&buf)
	runtime.KeepAlive(&size)

	if r == 0 || buf == nil {
		return false, false
	}
	defer procWTSFreeMemory.Call(uintptr(unsafe.Pointer(buf)))

	if size < uint32(unsafe.Sizeof(wtsInfoEx{})) || buf.Level != 1 {
		return false, false
	}

	flags := uint32(buf.Data.SessionFlags)
	if flags == wtsSessionStateUnknown {
		// Так бывает на консольной сессии сразу после входа: ответ есть, а
		// состояния в нём ещё нет.
		return false, false
	}

	// У этого вызова есть задокументированный дефект: на Windows 7 и
	// Server 2008 R2 флаги блокировки перепутаны местами — WTS_SESSIONSTATE_LOCK
	// означает «открыт», и наоборот. Windows 7 версии 6.1; Windows 8 и 8.1 —
	// 6.2 и 6.3 — дефекта уже не имеют, поэтому сравнение точное, а не «меньше».
	//
	// Мы целимся в Windows 10 и 11, и соблазн просто не думать об этом велик.
	// Но ошибка вышла бы тихой и дорогой сразу в обе стороны: заблокированный
	// экран читался бы как открытый — ребёнок платил бы за то, чего не делал,
	// — а открытый как заблокированный, и это уже бесплатное время.
	if v := windows.RtlGetVersion(); v != nil && v.MajorVersion == 6 && v.MinorVersion == 1 {
		switch flags {
		case wtsSessionStateLock:
			return false, true
		case wtsSessionStateUnlock:
			return true, true
		}
	}

	switch flags {
	case wtsSessionStateLock:
		return true, true
	case wtsSessionStateUnlock:
		return false, true
	}
	return false, false
}
