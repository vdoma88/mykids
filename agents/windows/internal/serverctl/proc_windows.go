//go:build windows

package serverctl

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// hide — без консольного окна: служба и окно управления запускают консольные
// программы PostgreSQL и Node, и каждая открыла бы чёрное окно.
func hide(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}

// terminate — в Windows мягко остановить чужую консольную программу без
// консоли нечем; API от этого ничего не теряет: всё, что важно, он хранит в
// базе, а база останавливается своим порядком.
func terminate(p *os.Process) { _ = p.Kill() }
