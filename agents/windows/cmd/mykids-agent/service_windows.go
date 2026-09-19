//go:build windows

package main

import (
	"golang.org/x/sys/windows/svc"

	"github.com/vdoma88/mykids/agents/windows/internal/winsvc"
)

// stopService просит диспетчер остановить службу и ждёт, пока та успеет
// сохранить состояние. Не дождавшись, следующий запуск счёл бы остановку
// нештатной и списал бы с ребёнка время ни за что.
func stopService() error {
	return winsvc.Control(svc.Stop, svc.Stopped)
}
