//go:build windows

package main

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Обход локальной группы: в x/sys/windows его нет, а знать состав
// администраторов установщику необходимо. Это единственное условие, без
// которого вся остальная установка бессмысленна.

var (
	netapi32                    = windows.NewLazySystemDLL("netapi32.dll")
	procNetLocalGroupGetMembers = netapi32.NewProc("NetLocalGroupGetMembers")
	procNetApiBufferFree        = netapi32.NewProc("NetApiBufferFree")
)

// maxPreferredLength — «выдели сколько нужно сам».
const maxPreferredLength = 0xFFFFFFFF

// localGroupMembersInfo0 — LOCALGROUP_MEMBERS_INFO_0: только SID.
type localGroupMembersInfo0 struct {
	sid *windows.SID
}

// groupMembers — SID'ы членов локальной группы.
//
// Имя группы берём из её же SID, а не строкой: на русской Windows она
// называется «Администраторы», на английской Administrators, и зашитое имя
// не нашлось бы на половине машин.
func groupMembers(group *windows.SID) ([]*windows.SID, error) {
	name, _, _, err := group.LookupAccount("")
	if err != nil {
		return nil, fmt.Errorf("имя группы администраторов: %w", err)
	}
	wname, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}

	// Буфер сразу типизированный: считать по нему смещения вручную значило бы
	// заниматься арифметикой по указателю, на которую go vet ругается
	// справедливо — размер структуры знает компилятор, а не мы.
	var buf *localGroupMembersInfo0
	var read, total uint32
	r, _, _ := procNetLocalGroupGetMembers.Call(
		0, // этот компьютер
		uintptr(unsafe.Pointer(wname)),
		0, // уровень 0: только SID
		uintptr(unsafe.Pointer(&buf)),
		uintptr(maxPreferredLength),
		uintptr(unsafe.Pointer(&read)),
		uintptr(unsafe.Pointer(&total)),
		0,
	)
	runtime.KeepAlive(wname)
	if r != 0 {
		return nil, fmt.Errorf("NetLocalGroupGetMembers: код %d", r)
	}
	if buf == nil || read == 0 {
		return nil, nil
	}
	defer procNetApiBufferFree.Call(uintptr(unsafe.Pointer(buf)))

	// Копируем SID'ы себе: буфер освобождается, а ссылки на него пережили бы
	// освобождение и указывали бы в никуда.
	out := make([]*windows.SID, 0, read)
	for _, entry := range unsafe.Slice(buf, read) {
		if entry.sid == nil {
			continue
		}
		copied, err := entry.sid.Copy()
		if err != nil {
			continue
		}
		out = append(out, copied)
	}
	return out, nil
}
