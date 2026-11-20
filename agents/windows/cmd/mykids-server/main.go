// mykids-server — домашний сервер MyKids на компьютере с Windows.
//
// Двойной щелчок (или «MyKids Сервер» в меню «Пуск») открывает окно
// управления: работает ли сервер, по какому адресу его открывать с других
// устройств дома, резервные копии, смена порта. Служба (serve) держит живыми
// PostgreSQL и API; остальные команды вызывает установщик.
//
// Ни одно действие семьи не требует командной строки: всё, что раньше
// делалось docker compose, git pull и ipconfig, делается здесь кнопками.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/vdoma88/mykids/agents/windows/internal/serverctl"
	"github.com/vdoma88/mykids/agents/windows/internal/setup"
	"github.com/vdoma88/mykids/agents/windows/internal/winsvc"
)

// version подменяется при сборке: build.sh передаёт её через -ldflags.
var version = "dev"

const (
	serviceName = "MyKidsServer"
	displayName = "MyKids — домашний сервер"
	description = "Админка родителя и задания ребёнка для всех устройств дома. " +
		"Хранит учёт экранного времени; остановка службы отключает MyKids во всём доме."
)

type options struct {
	L       serverctl.Layout
	out     string
	timeout time.Duration
}

func main() {
	winsvc.Configure(serviceName, displayName, description, 90*time.Second)
	winsvc.Account = serverctl.ServiceAccount

	home := flag.String("home", exeDir(), "каталог программы: node, pgsql, app")
	data := flag.String("data", serverctl.DefaultData(), "каталог данных")
	out := flag.String("out", "", "отчёт для мастера установки (UTF-16LE)")
	timeout := flag.Duration("timeout", 3*time.Minute, "сколько ждать сервер (wait)")
	flag.Parse()
	// Флаги можно писать и после команды: пакет flag останавливается на
	// первом аргументе без дефиса.
	cmd, sub := flag.Arg(0), flag.Arg(1)
	if rest := flag.Args(); len(rest) > 1 {
		start := 1
		if sub != "" && !strings.HasPrefix(sub, "-") {
			start = 2
		}
		if err := flag.CommandLine.Parse(rest[start:]); err != nil {
			os.Exit(2)
		}
	}

	o := options{L: serverctl.Layout{Home: *home, Data: *data}, out: *out, timeout: *timeout}
	os.Exit(run(cmd, sub, o))
}

func run(cmd, sub string, o options) int {
	switch cmd {
	case "":
		return panel(o)
	case "serve":
		if err := serve(o); err != nil {
			fmt.Fprintln(os.Stderr, "ошибка:", err)
			return 1
		}
		return 0
	case "protect":
		return reportErr(o, serverctl.Protect(o.L.Data), "Каталог данных: "+o.L.Data)
	case "service":
		return reportErr(o, serviceCommand(sub, o), "Служба: "+sub)
	case "firewall":
		return reportErr(o, firewall(sub, o), "Брандмауэр: "+sub)
	case "wait":
		return wait(o)
	case "port":
		cfg, err := serverctl.ReadConfig(o.L.Config())
		if err != nil {
			writeReport(o, []string{err.Error()})
			return 1
		}
		writeReport(o, []string{fmt.Sprint(cfg.Port)})
		return 0
	case "version":
		fmt.Println(version)
		return 0
	}
	fmt.Fprintf(os.Stderr, "неизвестная команда %q\n", cmd)
	return 2
}

func exeDir() string {
	self, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(self)
}

// writeReport пишет отчёт для мастера установки или в вывод.
func writeReport(o options, lines []string) {
	if o.out == "" {
		for _, l := range lines {
			fmt.Println(l)
		}
		return
	}
	_ = os.WriteFile(o.out, setup.UTF16(lines), 0o600)
}

func reportErr(o options, err error, what string) int {
	if err != nil {
		writeReport(o, []string{what + " — ошибка: " + err.Error()})
		return 1
	}
	writeReport(o, []string{what + " — готово"})
	return 0
}

// logger — журнал службы в файле. Своего окна у службы нет, а журнал
// событий Windows родитель не откроет никогда; файл открывается кнопкой из
// окна управления.
func logger(dir string) func(string) {
	var mu sync.Mutex
	path := filepath.Join(dir, "service.log")
	return func(line string) {
		mu.Lock()
		defer mu.Unlock()
		_ = os.MkdirAll(dir, 0o755)
		if st, err := os.Stat(path); err == nil && st.Size() > 5<<20 {
			_ = os.Rename(path, path+".old")
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return
		}
		defer f.Close()
		fmt.Fprintf(f, "%s  %s\n", time.Now().Format("2006-01-02 15:04:05"), line)
	}
}

// serve — работа службы.
func serve(o options) error {
	cfg, err := serverctl.LoadConfig(o.L.Config())
	if err != nil {
		return err
	}
	toFile := logger(o.L.Logs())
	toFile(fmt.Sprintf("MyKids Сервер %s запускается", version))
	body := func(ctx context.Context, logf func(string, ...any)) error {
		sup := &serverctl.Supervisor{L: o.L, C: cfg, Log: func(s string) {
			toFile(s)
			logf("%s", s)
		}}
		err := sup.Run(ctx)
		if err != nil {
			toFile("Остановлен с ошибкой: " + err.Error())
		} else {
			toFile("Остановлен")
		}
		return err
	}
	if winsvc.InService() {
		return winsvc.Run(body)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return body(ctx, func(format string, a ...any) { fmt.Printf(format+"\n", a...) })
}

// wait ждёт, пока сервер ответит. Для мастера установки: «установлено»
// должно значить «открывается», а не «файлы скопированы».
func wait(o options) int {
	cfg, err := serverctl.ReadConfig(o.L.Config())
	if err != nil {
		writeReport(o, []string{"Настройки сервера: " + err.Error()})
		return 1
	}
	deadline := time.Now().Add(o.timeout)
	for time.Now().Before(deadline) {
		if serverctl.Healthy(context.Background(), cfg.Port) {
			writeReport(o, addressLines(cfg.Port))
			return 0
		}
		if stopped() {
			break
		}
		time.Sleep(2 * time.Second)
	}
	lines := []string{fmt.Sprintf("Сервер не ответил на порту %d.", cfg.Port), ""}
	lines = append(lines, tail(filepath.Join(o.L.Logs(), "service.log"), 12)...)
	lines = append(lines, tail(filepath.Join(o.L.Logs(), "server.log"), 8)...)
	lines = append(lines, "", "Журналы: "+o.L.Logs())
	writeReport(o, lines)
	return 1
}

// addressLines — адреса MyKids словами: их родитель увидит в конце
// установки и в окне управления.
func addressLines(port int) []string {
	lines := []string{fmt.Sprintf("Админка на этом компьютере: http://localhost:%d", port)}
	urls := serverctl.LANURLs(port)
	if len(urls) == 0 {
		return append(lines, "Адреса в домашней сети не нашлось — компьютер не подключён к сети?")
	}
	lines = append(lines, "С других устройств дома: "+urls[0])
	for _, u := range urls[1:] {
		lines = append(lines, "    или "+u)
	}
	lines = append(lines, "Задания ребёнка: "+urls[0]+"/child")
	return lines
}

func tail(path string, n int) []string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return append([]string{filepath.Base(path) + ":"}, lines...)
}
