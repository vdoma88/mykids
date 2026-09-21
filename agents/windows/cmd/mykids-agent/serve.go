package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/vdoma88/mykids/agents/windows/internal/helper"
	"github.com/vdoma88/mykids/agents/windows/internal/ipc"
	"github.com/vdoma88/mykids/agents/windows/internal/screen"
	"github.com/vdoma88/mykids/agents/windows/internal/service"
	"github.com/vdoma88/mykids/agents/windows/internal/winsvc"
)

// serve — тело службы: учёт, связь с сервером и ответы помощнику.
//
// Одна и та же работа запускается и под диспетчером служб, и из консоли. Из
// консоли — чтобы родитель мог посмотреть, что происходит, не читая журнал
// событий.
func serve(a assembled, o options) error {
	body := func(ctx context.Context, log func(string, ...any)) error {
		core := service.New(service.Options{
			Agent: a.agent, Link: a.link, Clock: a.clock, Source: a.source,
			StatePath: a.paths.state, LocalPolicy: a.localPolicy, Log: log,
		})
		core.SetChildURL(a.childURL)

		// Помощника поднимает служба: больше некому. Он живёт в сессии
		// ребёнка, а служба — в нулевой, и без него служба слепа.
		if exe, err := os.Executable(); err != nil {
			log("не найти собственный путь, помощник не будет подниматься: %v", err)
		} else {
			core.SetHelpers(newHelpers(exe, helperArgs(a.paths.dataDir, o)))
		}

		l, err := ipc.Listen(o.pipe)
		if err != nil {
			return fmt.Errorf("канал %s: %w", o.pipe, err)
		}
		defer l.Close()
		log("служба слушает %s", o.pipe)

		// Свой источник состояния экрана — единственное, что служба может
		// узнать о рабочем столе, не спрашивая помощника. Подключается и к
		// наблюдениям (чтобы молчание помощника не стоило ребёнку времени,
		// пока экран честно заблокирован), и к проверке на ложь.
		own := newLockSource()
		a.remote.SetLockSource(own)

		go acceptHelpers(l, core, a.remote, own, a.agent.Policy.IdleThresholdSeconds, log)

		iv := service.DefaultIntervals()
		if o.interval > 0 {
			iv.Tick = o.interval
		}
		return core.Run(ctx, iv)
	}

	if winsvc.InService() {
		return winsvc.Run(body)
	}

	// Из консоли: Ctrl+C означает штатную остановку, как команда диспетчера.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Printf("mykids-agent %s · служба в консоли · канал: %s\n", version, o.pipe)
	fmt.Println("остановка — Ctrl+C")
	err := body(ctx, func(format string, args ...any) {
		fmt.Printf("%s  %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
	})
	fmt.Println("остановлена, состояние сохранено")
	return err
}

// helperArgs — с какими флагами служба запускает помощника.
//
// Те же каталог и канал, что у неё самой: помощник, запущенный с чужими
// настройками, подключится не туда и будет молчать — а молчание помощника
// служба считает расходом.
func helperArgs(dataDir string, o options) []string {
	args := []string{"-data", dataDir, "-pipe", o.pipe}
	if o.interval > 0 {
		args = append(args, "-interval", o.interval.String())
	}
	return append(args, "helper")
}

// acceptHelpers принимает помощников, пока служба жива.
//
// Каждое соединение обслуживается отдельно: помощник может перезапуститься,
// а пользовательских сессий бывает несколько.
func acceptHelpers(l net.Listener, core *service.Core, remote *ipc.Desktop,
	own ipc.LockSource, idleThresholdSeconds int, log func(string, ...any)) {

	idleThreshold := time.Duration(idleThresholdSeconds) * time.Second
	for {
		conn, err := l.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				log("приём помощника: %v", err)
			}
			return // слушатель закрыт — служба выключается
		}
		go func() {
			err := ipc.Serve(conn, core.Handler(remote, own, idleThreshold, time.Now))
			if err != nil && !errors.Is(err, ipc.ErrClosed) {
				// Мусор в канале — либо сломанный помощник, либо подменённый.
				log("соединение с помощником разорвано: %v", err)
			}
		}()
	}
}

// runHelper — наблюдатель в сессии пользователя.
//
// Всё, что он решает сам, — что делать, когда службы не слышно. Остальное
// приходит готовым: и вердикт, и текст на оверлее.
func runHelper(o options) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	desktop := newDesktop()
	enforcer := newEnforcer()

	interval := o.interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	fmt.Printf("mykids-agent %s · помощник · канал: %s\n", version, o.pipe)

	var conn *ipc.Conn
	var lostSince time.Time
	blocked := false

	// Предупреждение экран не перекрывает: отнять его у подростка ровно тогда,
	// когда он спешит сохраниться, — значит сделать предупреждение бесполезным.
	apply := func(s screen.Screen) {
		switch {
		case s.Kind == screen.Block:
			hideWarning()
			if err := enforcer.Block(screen.Render(s)); err != nil {
				fmt.Fprintf(os.Stderr, "блокировка: %v\n", err)
				return
			}
			blocked = true
		case s.Kind == screen.Warn:
			if blocked {
				enforcer.Unblock()
				blocked = false
			}
			warn(s)
		default:
			hideWarning()
			if blocked {
				enforcer.Unblock()
				blocked = false
			}
		}
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if conn != nil {
				conn.Close()
			}
			hideWarning()
			enforcer.Unblock()
			return nil

		case <-ticker.C:
			if conn == nil {
				c, err := ipc.DialAddr(o.pipe)
				if err != nil {
					if lostSince.IsZero() {
						lostSince = time.Now()
					}
					apply(helper.OnServiceLost(time.Since(lostSince)))
					continue
				}
				conn = ipc.Dial(c)
				lostSince = time.Time{}
			}

			proc, err := desktop.ForegroundProcess()
			if err != nil {
				fmt.Fprintf(os.Stderr, "активное окно: %v\n", err)
			}
			idle, err := desktop.IdleTime()
			if err != nil {
				fmt.Fprintf(os.Stderr, "время простоя: %v\n", err)
			}

			v, err := conn.Exchange(ipc.Sample{
				At:            time.Now(),
				Process:       proc,
				IdleSeconds:   int(idle.Seconds()),
				SessionLocked: desktop.SessionLocked(),
			})
			if err != nil {
				// Служба пропала посреди разговора: соединение переоткроем,
				// а экран закроем, если её не будет слишком долго.
				conn.Close()
				conn = nil
				if lostSince.IsZero() {
					lostSince = time.Now()
				}
				apply(helper.OnServiceLost(time.Since(lostSince)))
				continue
			}
			apply(helper.OnVerdict(v))
		}
	}
}
