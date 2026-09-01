// Команда mykids-agent — агент контроля экранного времени для Windows.
//
//	mykids-agent status   один замер: что агент видит прямо сейчас
//	mykids-agent watch    наблюдение без блокировки
//	mykids-agent run      наблюдение с блокировкой экрана
//
// Подкоманды разделены намеренно: сначала убедиться, что учёт видит нужное,
// и только потом отдавать ему право закрывать экран.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	_ "time/tzdata" // база часовых поясов внутрь бинарника: в Windows своей нет

	"github.com/vdoma88/mykids/agents/windows/internal/agent"
	"github.com/vdoma88/mykids/agents/windows/internal/config"
	"github.com/vdoma88/mykids/agents/windows/internal/state"
)

const version = "0.1.0"

func main() {
	dataDir := flag.String("data", defaultDataDir(), "каталог с политикой и состоянием")
	interval := flag.Duration("interval", 5*time.Second, "период опроса рабочего стола")
	flag.Usage = printUsage
	flag.Parse()

	cmd := flag.Arg(0)
	if cmd == "" {
		printUsage()
		os.Exit(2)
	}

	if err := run(cmd, *dataDir, *interval); err != nil {
		fmt.Fprintf(os.Stderr, "ошибка: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `mykids-agent %s — учёт и ограничение экранного времени

Использование:
  mykids-agent [флаги] <команда>

Команды:
  status    один замер: активное окно, простой, остаток времени
  watch     наблюдение и учёт без блокировки экрана
  run       наблюдение с блокировкой экрана
  version   версия

Флаги:
`, version)
	flag.PrintDefaults()
}

func defaultDataDir() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "MyKids")
	}
	return "."
}

func run(cmd, dataDir string, interval time.Duration) error {
	if cmd == "version" {
		fmt.Println(version)
		return nil
	}

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("каталог данных %s: %w", dataDir, err)
	}
	policyPath := filepath.Join(dataDir, "policy.json")
	statePath := filepath.Join(dataDir, "state.json")

	policy, err := config.Load(policyPath)
	if err != nil {
		return err
	}
	st, err := state.Load(statePath)
	if err != nil {
		// Битое состояние не повод не работать: начинаем день заново и говорим об этом.
		fmt.Fprintf(os.Stderr, "предупреждение: %v — состояние сброшено\n", err)
		st = state.State{}
	}
	if !st.CleanShutdown && st.Today.Key != "" {
		// Прошлый запуск не завершился штатно. Это может быть и сбой питания,
		// и попытка снять агента, поэтому просто считаем.
		st.UncleanStops++
	}
	st.CleanShutdown = false

	var enforcer agent.Enforcer
	if cmd == "run" {
		enforcer = newEnforcer()
	}

	a, err := agent.New(policy, newDesktop(), enforcer, st)
	if err != nil {
		return err
	}

	switch cmd {
	case "status":
		v, err := a.Tick(time.Now())
		if err != nil {
			return err
		}
		fmt.Printf("политика:   %s\n", policyPath)
		fmt.Printf("состояние:  %s\n", statePath)
		fmt.Printf("пояс:       %s\n", policy.Timezone)
		fmt.Printf("состояние:  %s\n", describe(v))
		if st.UncleanStops > 0 {
			fmt.Printf("нештатных остановок: %d\n", st.UncleanStops)
		}
		return nil
	case "watch", "run":
		return loop(a, statePath, interval, cmd == "run")
	default:
		printUsage()
		return fmt.Errorf("неизвестная команда %q", cmd)
	}
}

func loop(a *agent.Agent, statePath string, interval time.Duration, enforcing bool) error {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	mode := "наблюдение"
	if enforcing {
		mode = "наблюдение с блокировкой"
	}
	fmt.Printf("mykids-agent %s · режим: %s · период: %s\n", version, mode, interval)
	fmt.Println("остановка — Ctrl+C")

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	saveEvery := time.NewTicker(30 * time.Second)
	defer saveEvery.Stop()

	save := func(clean bool) {
		st := a.State()
		st.CleanShutdown = clean
		if err := state.Save(statePath, st); err != nil {
			fmt.Fprintf(os.Stderr, "не удалось сохранить состояние: %v\n", err)
		}
	}

	var lastLine string
	for {
		select {
		case <-stop:
			save(true)
			fmt.Println("\nостановлен, состояние сохранено")
			return nil

		case <-saveEvery.C:
			save(false)

		case now := <-ticker.C:
			v, err := a.Tick(now)
			if err != nil {
				fmt.Fprintf(os.Stderr, "тик: %v\n", err)
				continue
			}
			line := describe(v)
			if line != lastLine {
				fmt.Printf("%s  %s\n", now.Format("15:04:05"), line)
				lastLine = line
			}
		}
	}
}
