package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/vdoma88/mykids/agents/windows/internal/setup"
)

// Команды для мастера установки.
//
// Окно мастера рисует NSIS: страницы, ярлыки, запись в «Программах и
// компонентах», удаление. Решать за него, что считать ошибкой и что с ней
// делать, NSIS не умеет и не должен — это уже написано здесь, проверено
// тестами и одинаково для мастера и для окна этой программы. Поэтому мастер
// вызывает эту же программу с командой и читает её отчёт.
//
// Отчёт идёт в файл (-out) в UTF-16LE, а не в вывод: см. setup.UTF16.
// Результат — кодом возврата.

// Коды возврата. Мастер различает их, а не разбирает текст.
const (
	exitOK = 0
	// exitStop — делать дальше нечего: родитель должен что-то исправить.
	exitStop = 1
	// exitUsage — мастер вызвал программу не так. Это ошибка сборки, а не
	// родителя.
	exitUsage = 2
	// exitNotBound — устройство ещё не привязано (команда status).
	exitNotBound = 3
)

// cliArgs — всё, что мастер может передать.
type cliArgs struct {
	out   string
	dir   string
	in    setup.Input
	purge bool
}

// report собирает строки отчёта и пишет их одним файлом в конце.
type report struct {
	path  string
	lines []string
}

func (r *report) add(format string, a ...any) {
	line := fmt.Sprintf(format, a...)
	r.lines = append(r.lines, line)
	toFile(line)
}

func (r *report) flush() {
	if r.path == "" {
		for _, l := range r.lines {
			fmt.Println(l)
		}
		return
	}
	_ = os.WriteFile(r.path, setup.UTF16(r.lines), 0o600)
}

// isCommand — запущены ли мы мастером, а не двойным щелчком.
func isCommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "accounts", "status", "verify", "stop", "apply":
		return true
	}
	return false
}

func parseCLI(args []string) (string, cliArgs, error) {
	var a cliArgs
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&a.out, "out", "", "файл отчёта (UTF-16LE)")
	fs.StringVar(&a.dir, "dir", "", "каталог установленных программ")
	fs.StringVar(&a.in.Server, "server", "", "адрес сервера")
	fs.StringVar(&a.in.Token, "token", "", "токен устройства")
	fs.StringVar(&a.in.ChildUser, "child", "", "учётная запись ребёнка")
	fs.StringVar(&a.in.ChildURL, "child-url", "", "адрес заданий для ребёнка")
	if err := fs.Parse(args[1:]); err != nil {
		return "", a, err
	}
	if fs.NArg() > 0 {
		return "", a, fmt.Errorf("лишние аргументы: %v", fs.Args())
	}
	return args[0], a, nil
}

// runCommand выполняет команду мастера и возвращает код возврата.
func runCommand(args []string) int {
	cmd, a, err := parseCLI(args)
	r := &report{path: a.out}
	defer r.flush()
	if err != nil {
		r.add("Мастер установки вызвал программу неверно: %v", err)
		return exitUsage
	}
	code, err := dispatch(cmd, a, r)
	if errors.Is(err, errNotWindows) {
		r.add("Установщик MyKids работает только в Windows.")
		return exitStop
	}
	return code
}

var errNotWindows = errors.New("не Windows")
