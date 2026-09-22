//go:build windows

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// Всё, что установщик делает с машиной. Решения — в internal/setup; здесь
// только исполнение, и проверить его можно лишь на настоящей Windows.

const (
	installDir = `C:\Program Files\MyKids`
	agentName  = "mykids-agent.exe"
)

func dataDir() string {
	if p := os.Getenv("ProgramData"); p != "" {
		return filepath.Join(p, "MyKids")
	}
	return `C:\ProgramData\MyKids`
}

// elevated — запущены ли мы от администратора.
func elevated() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

// childIsAdmin — состоит ли учётная запись в локальных администраторах.
//
// Второе значение — удалось ли выяснить. Ложь в нём означает «не знаю»:
// запрещать установку из-за непроверенного условия нельзя, промолчать — тоже.
func childIsAdmin(name string) (isAdmin bool, known bool, err error) {
	sid, _, _, err := windows.LookupSID("", name)
	if err != nil {
		return false, false, fmt.Errorf("учётной записи «%s» на этой машине нет", name)
	}
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return false, false, err
	}
	// Членство читаем через токен: перечислять группу пришлось бы через
	// netapi32, а здесь тот же ответ даёт система сама.
	members, err := groupMembers(admins)
	if err != nil {
		return false, false, err
	}
	for _, m := range members {
		if m.Equals(sid) {
			return true, true, nil
		}
	}
	return false, true, nil
}

// copyAgent кладёт агент туда, где ребёнок его не сотрёт.
func copyAgent(from string) (string, error) {
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(installDir, agentName)

	src, err := os.Open(from)
	if err != nil {
		return "", fmt.Errorf("не найден %s: %w", from, err)
	}
	defer src.Close()

	out, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	defer out.Close()
	if _, err := io.Copy(out, src); err != nil {
		return "", err
	}
	return dst, nil
}

// makeDataDir заводит каталог данных и закрывает его от ребёнка.
//
// Наследование прав снимаем целиком: иначе ребёнок сможет создавать там файлы,
// а в каталоге лежат состояние учёта и очередь расхода. Оставляем двоих —
// SYSTEM (под ним работает служба) и администраторов (под ними родитель).
func makeDataDir(path string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}

	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return err
	}
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return err
	}

	entries := make([]windows.EXPLICIT_ACCESS, 0, 2)
	for _, sid := range []*windows.SID{system, admins} {
		entries = append(entries, windows.EXPLICIT_ACCESS{
			AccessPermissions: windows.GENERIC_ALL,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       windows.CONTAINER_INHERIT_ACE | windows.OBJECT_INHERIT_ACE,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_WELL_KNOWN_GROUP,
				TrusteeValue: windows.TrusteeValueFromSID(sid),
			},
		})
	}
	acl, err := windows.ACLFromEntries(entries, nil)
	if err != nil {
		return err
	}

	// PROTECTED_DACL_SECURITY_INFORMATION и есть снятие наследования: без него
	// унаследованные разрешения остаются, и «Пользователи» сохраняют доступ.
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, acl, nil)
}

// agentRun запускает агент и возвращает его вывод вместе с ошибкой.
//
// Вывод нужен именно при ошибке: «код возврата 1» не говорит ничего, а
// «токен не принят» говорит всё.
func agentRun(exe string, args ...string) (string, error) {
	cmd := exec.Command(exe, args...)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text != "" {
			return text, fmt.Errorf("%s", firstLine(text))
		}
		return text, err
	}
	return text, nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
