//go:build !windows

package serverctl

import (
	"os"
	"os/exec"
	"syscall"
)

func hide(*exec.Cmd) {}

// socketOption — без Unix-сокета. Подключения всё равно идут по TCP на
// 127.0.0.1, а каталог сокетов по умолчанию (/var/run/postgresql) пишется
// только пользователем postgres: от любого другого база не запустилась бы.
const socketOption = " -c unix_socket_directories=''"

func terminate(p *os.Process) { _ = p.Signal(syscall.SIGTERM) }
