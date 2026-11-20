//go:build windows

package serverctl

import (
	"os"

	"golang.org/x/sys/windows"
)

// ServiceAccount — под кем работает служба сервера. Не LocalSystem:
// PostgreSQL отказывается работать с правами администратора, и обходить это
// незачем — серверу они и не нужны.
const ServiceAccount = `NT AUTHORITY\LocalService`

// Protect заводит каталог данных и закрывает его от всех, кроме системы,
// администраторов и службы сервера. В нём пароль базы и сама база: ребёнок,
// работающий на этом же компьютере, не должен уметь ни прочесть пароль, ни
// подменить файлы базы.
func Protect(path string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	var sids []*windows.SID
	for _, t := range []windows.WELL_KNOWN_SID_TYPE{
		windows.WinLocalSystemSid, windows.WinBuiltinAdministratorsSid, windows.WinLocalServiceSid,
	} {
		sid, err := windows.CreateWellKnownSid(t)
		if err != nil {
			return err
		}
		sids = append(sids, sid)
	}
	entries := make([]windows.EXPLICIT_ACCESS, 0, len(sids))
	for _, sid := range sids {
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
	// PROTECTED — снять унаследованное от ProgramData «Пользователям можно
	// читать»: иначе пароль базы прочёл бы кто угодно на этом компьютере.
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, acl, nil)
}
