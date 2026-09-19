//go:build !windows

package main

import (
	"os"
	"path/filepath"
)

// defaultDataDir — каталог агента вне Windows: профиль пользователя.
//
// Машинного каталога, общего для службы и родителя, здесь не нужно: вне
// Windows агент запускают только для разработки и тестов.
func defaultDataDir() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "MyKids")
	}
	return "."
}
