//go:build windows

package main

import (
	"os"
	"path/filepath"

	"github.com/vdoma88/mykids/agents/windows/internal/win32"
)

// openPanel открывает «Панель MyKids» — она лежит рядом с агентом.
func openPanel() bool {
	self, err := os.Executable()
	if err != nil {
		return false
	}
	panel := filepath.Join(filepath.Dir(self), "mykids-setup.exe")
	if _, err := os.Stat(panel); err != nil {
		win32.Info("MyKids", "Это служба MyKids — она работает сама и окна не открывает.\n\nСостояние показывает «Панель MyKids» в меню «Пуск».")
		return true
	}
	return win32.Start(panel, "") == nil
}
