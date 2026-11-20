//go:build !windows

package serverctl

import (
	"os"
	"os/exec"
	"syscall"
)

func hide(*exec.Cmd) {}

func terminate(p *os.Process) { _ = p.Signal(syscall.SIGTERM) }
