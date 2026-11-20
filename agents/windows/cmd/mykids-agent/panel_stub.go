//go:build !windows

package main

// openPanel — панели вне Windows нет: агент там запускают из терминала.
func openPanel() bool { return false }
