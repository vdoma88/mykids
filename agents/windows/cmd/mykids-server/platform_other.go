//go:build !windows

package main

import (
	"errors"
	"fmt"
	"os"
)

var errNotWindows = errors.New("доступно только в Windows")

func panel(options) int {
	fmt.Fprintln(os.Stderr, "mykids-server: окно управления есть только в Windows; здесь — serve, wait, version")
	return 2
}

func serviceCommand(string, options) error { return errNotWindows }
func firewall(string, options) error       { return errNotWindows }
func stopped() bool                        { return false }
