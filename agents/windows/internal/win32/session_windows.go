//go:build windows

package win32

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Запуск процесса в чужой сессии — то немногое, чего служба из нулевой сессии
// не может сделать обычными средствами. Здесь только вызовы Win32: когда
// запускать и когда сдаваться, решает internal/watchdog, и это проверено
// тестами на любой ОС.

// noSession — что возвращает Windows, когда консольной сессии нет вовсе.
const noSession = 0xFFFFFFFF

// ConsoleSession — номер консольной сессии, без проверки входа.
//
// Ноль означает, что сессии нет совсем. Этот вопрос можно задать из любой
// программы, в том числе запущенной ребёнком.
func ConsoleSession() uint32 {
	if id := windows.WTSGetActiveConsoleSessionId(); id != noSession {
		return id
	}
	return 0
}

// ActiveSession — сессия, в которой сейчас работает вошедший пользователь.
//
// Ноль означает «никого нет»: либо сессии нет совсем, либо на экране
// приглашение ко входу. Второе важно не меньше первого: там токена ещё не
// существует, и попытка запуска будет падать до самого входа.
//
// Годится только для службы: WTSQueryUserToken требует привилегию SE_TCB_NAME,
// которой у обычной программы нет. Спросив это из-под ребёнка, всегда получишь
// ноль — и решишь, что сессии нет, хотя он прямо за этим компьютером.
func ActiveSession() uint32 {
	id := ConsoleSession()
	if id == 0 {
		return 0
	}
	var token windows.Token
	if err := windows.WTSQueryUserToken(id, &token); err != nil {
		return 0
	}
	if token != 0 {
		token.Close()
	}
	return id
}

// StartInSession запускает программу в сессии пользователя и возвращает
// дескриптор процесса.
//
// Дескриптор нужен, чтобы отличить живого помощника от снятого: другого
// надёжного способа у службы нет — по имени процесса легко ошибиться, если
// ребёнок запустит свою копию.
func StartInSession(session uint32, exe string, args ...string) (windows.Handle, error) {
	var token windows.Token
	if err := windows.WTSQueryUserToken(session, &token); err != nil {
		return 0, fmt.Errorf("токен сессии %d: %w", session, err)
	}
	defer token.Close()

	// Токен сессии нельзя отдать CreateProcessAsUser как есть: нужен
	// первичный, а WTSQueryUserToken отдаёт олицетворяющий.
	var primary windows.Token
	err := windows.DuplicateTokenEx(token, windows.MAXIMUM_ALLOWED, nil,
		windows.SecurityIdentification, windows.TokenPrimary, &primary)
	if err != nil {
		return 0, fmt.Errorf("дублирование токена: %w", err)
	}
	defer primary.Close()

	// Окружение берём от пользователя, а не от службы: у LocalSystem другой
	// профиль, и помощник иначе не нашёл бы ни %APPDATA%, ни temp ребёнка.
	var env *uint16
	if err := windows.CreateEnvironmentBlock(&env, primary, false); err != nil {
		return 0, fmt.Errorf("окружение пользователя: %w", err)
	}
	defer windows.DestroyEnvironmentBlock(env)

	si := windows.StartupInfo{
		// Без этого процесс запустится в невидимом рабочем столе службы, и
		// оверлей появится там, где его никто не увидит.
		Desktop: windows.StringToUTF16Ptr(`winsta0\default`),
	}
	si.Cb = uint32(unsafe.Sizeof(si))

	var pi windows.ProcessInformation
	line := commandLine(exe, args...)
	err = windows.CreateProcessAsUser(primary, nil, windows.StringToUTF16Ptr(line),
		nil, nil, false,
		windows.CREATE_UNICODE_ENVIRONMENT|windows.CREATE_NO_WINDOW,
		env, nil, &si, &pi)
	if err != nil {
		return 0, fmt.Errorf("запуск в сессии %d: %w", session, err)
	}
	// Поток не нужен, а дескриптор его течёт.
	windows.CloseHandle(pi.Thread)
	return pi.Process, nil
}

// Alive — жив ли процесс с таким дескриптором.
func Alive(h windows.Handle) bool {
	if h == 0 {
		return false
	}
	r, err := windows.WaitForSingleObject(h, 0)
	return err == nil && r == uint32(windows.WAIT_TIMEOUT)
}

// commandLine собирает строку так, как её разберёт обратно CommandLineToArgvW.
//
// Отдельной функцией, потому что ошибка здесь тихая: путь с пробелом без
// кавычек превратится в два аргумента, и помощник запустится с чужими
// настройками вместо того, чтобы честно не запуститься.
func commandLine(exe string, args ...string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, syscall.EscapeArg(exe))
	for _, a := range args {
		parts = append(parts, syscall.EscapeArg(a))
	}
	return strings.Join(parts, " ")
}
