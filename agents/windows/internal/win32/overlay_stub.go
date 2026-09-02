//go:build !windows

package win32

// Overlay вне Windows не существует; заглушка нужна, чтобы остальной код
// собирался и тестировался на любой системе.
type Overlay struct{}

func ShowOverlay(text string) (*Overlay, error) { return nil, ErrUnsupported }
func (o *Overlay) SetText(text string)          {}
func (o *Overlay) Close()                       {}
