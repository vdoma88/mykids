//go:build !windows

// Заглушка для сборки и тестов вне Windows: платформенно-нейтральные пакеты
// должны компилироваться и проверяться на любой системе.
package win32

import (
	"errors"
	"time"
)

// ErrUnsupported возвращается вне Windows.
var ErrUnsupported = errors.New("доступно только в Windows")

func ForegroundProcess() (string, error)  { return "", ErrUnsupported }
func IdleTime() (time.Duration, error)    { return 0, ErrUnsupported }
func SessionLocked() bool                 { return false }
func LockWorkstation() error              { return ErrUnsupported }
