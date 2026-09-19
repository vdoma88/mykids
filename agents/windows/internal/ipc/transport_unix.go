//go:build !windows

package ipc

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// DefaultAddr — путь сокета вне Windows.
//
// Агент предназначен для Windows, но разделение службы и помощника проверяется
// и здесь: так сквозной тест двух процессов идёт в CI, где Windows нет.
var DefaultAddr = filepath.Join(os.TempDir(), "mykids-agent.sock")

// Listen открывает сокет для помощника.
//
// Осиротевший файл от прошлого запуска удаляем: иначе служба не поднимется
// после нештатной остановки — ровно тогда, когда подняться важнее всего.
func Listen(addr string) (net.Listener, error) {
	if err := os.Remove(addr); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("освобождение %s: %w", addr, err)
	}
	l, err := net.Listen("unix", addr)
	if err != nil {
		return nil, err
	}
	// Доступ только владельцу: на Windows то же делает список доступа канала.
	if err := os.Chmod(addr, 0o600); err != nil {
		l.Close()
		return nil, err
	}
	return l, nil
}

// DialAddr подключается к службе.
func DialAddr(addr string) (net.Conn, error) {
	return net.DialTimeout("unix", addr, 5*time.Second)
}
