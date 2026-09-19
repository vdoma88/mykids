//go:build !windows

package main

import (
	"os"
	"os/exec"
	"sync"

	"github.com/vdoma88/mykids/agents/windows/internal/watchdog"
)

// procHelpers поднимает помощника обычным процессом.
//
// Вне Windows сессий нет, и «активная сессия» здесь всегда одна и та же.
// Это не заглушка ради компиляции: так служба на любой машине действительно
// поднимает помощника сама, и двухпроцессный прогон проверяет то же
// поведение, на которое семья полагается в бою.
type procHelpers struct {
	exe  string
	args []string

	mu sync.Mutex
	// done закрывается, когда помощник вышел. Отдельным каналом, а не
	// опросом процесса: os.Process.Signal(nil) в Go не значит «проверить,
	// жив ли» — это приёмом из C, и Go на него отвечает ошибкой всегда.
	// Живой прогон показал, к чему это приводит: служба считала помощника
	// мёртвым каждый раз и запускала нового поверх живого.
	done chan struct{}
}

func (h *procHelpers) ActiveSession() watchdog.Session { return 1 }

func (h *procHelpers) HelperAlive() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.done == nil {
		return false
	}
	select {
	case <-h.done:
		return false
	default:
		return true
	}
}

func (h *procHelpers) StartHelper(watchdog.Session) error {
	cmd := exec.Command(h.exe, h.args...)
	// Вывод помощника идёт туда же, куда вывод службы. Иначе он уходит в
	// никуда: os/exec подставляет пустому Stdout устройство-пустышку, и
	// родитель, запустивший службу из консоли, не увидит ни предупреждения,
	// ни закрытого экрана — как не увидел их и живой прогон.
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}

	done := make(chan struct{})
	h.mu.Lock()
	h.done = done
	h.mu.Unlock()

	// Статус забираем в своей горутине: без этого помощник останется зомби,
	// но ждать его в цикле службы нельзя — цикл в это время считает время.
	// Именно на этом служба и зависла на живом прогоне.
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	return nil
}

func newHelpers(exe string, args []string) watchdog.Desktop {
	return &procHelpers{exe: exe, args: args}
}
