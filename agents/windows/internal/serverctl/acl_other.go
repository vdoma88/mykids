//go:build !windows

package serverctl

import "os"

// ServiceAccount — вне Windows служба запускается от того, кто её запустил.
const ServiceAccount = ""

// Protect — вне Windows достаточно прав каталога.
func Protect(path string) error { return os.MkdirAll(path, 0o700) }
