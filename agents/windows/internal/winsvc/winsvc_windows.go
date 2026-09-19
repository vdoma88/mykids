//go:build windows

package winsvc

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"
)

// Body — работа службы. Обязана вернуться, когда контекст отменён.
type Body func(ctx context.Context, log func(string, ...any)) error

// InService сообщает, запущены ли мы диспетчером служб, а не из консоли.
func InService() bool {
	in, err := svc.IsWindowsService()
	return err == nil && in
}

// handler переводит команды диспетчера в отмену контекста.
type handler struct {
	body Body
	log  *eventlog.Log
}

// accepted — какие команды служба принимает.
//
// Shutdown принимаем наравне со Stop: выключение компьютера — это штатное
// завершение, и считать его нештатной остановкой значило бы штрафовать
// ребёнка за то, что он выключил компьютер.
const accepted = svc.AcceptStop | svc.AcceptShutdown | svc.AcceptSessionChange

func (h *handler) Execute(args []string, r <-chan svc.ChangeRequest, s chan<- svc.Status) (bool, uint32) {
	s <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.body(ctx, h.logf) }()

	s <- svc.Status{State: svc.Running, Accepts: accepted}

	for {
		select {
		case err := <-done:
			// Работа кончилась сама: это либо ошибка, либо нечего делать.
			cancel()
			if err != nil {
				h.logf("служба остановлена с ошибкой: %v", err)
				return false, 1
			}
			return false, 0

		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				s <- c.CurrentStatus

			case svc.Stop, svc.Shutdown:
				s <- svc.Status{State: svc.StopPending}
				cancel()
				// Ждём, пока работа доделает своё: она обязана сохранить
				// состояние, иначе следующий запуск сочтёт остановку нештатной
				// и спишет с ребёнка время ни за что.
				select {
				case <-done:
				case <-time.After(20 * time.Second):
					h.logf("работа не завершилась за 20 секунд, выходим")
				}
				return false, 0

			case svc.SessionChange:
				// Вход и выход пользователя, блокировка экрана. Само по себе
				// учёту не нужно — блокировку видит помощник, — но в журнале
				// это помогает понять, что происходило.
				h.logf("событие сессии: %d", c.EventType)

			default:
				h.logf("неизвестная команда диспетчера: %d", c.Cmd)
			}
		}
	}
}

func (h *handler) logf(format string, args ...any) {
	if h.log == nil {
		return
	}
	// Журнал — единственное, что видно у службы: своего окна у неё нет.
	_ = h.log.Info(1, fmt.Sprintf(format, args...))
}

// Run запускает работу под диспетчером служб.
func Run(body Body) error {
	log, err := eventlog.Open(Name)
	if err != nil {
		// Без журнала работать можно, молча — хуже, чем совсем не работать,
		// не будет: учёт важнее записей о нём.
		log = nil
	}
	if log != nil {
		defer log.Close()
	}
	return svc.Run(Name, &handler{body: body, log: log})
}

// Install регистрирует службу с автозапуском.
func Install(args ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("путь к себе: %w", err)
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return err
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("диспетчер служб (нужны права администратора): %w", err)
	}
	defer m.Disconnect()

	if s, err := m.OpenService(Name); err == nil {
		s.Close()
		return fmt.Errorf("служба %s уже установлена", Name)
	}

	s, err := m.CreateService(Name, exe, mgr.Config{
		DisplayName: DisplayName,
		Description: Description,
		StartType:   mgr.StartAutomatic,
	}, args...)
	if err != nil {
		return fmt.Errorf("создание службы: %w", err)
	}
	defer s.Close()

	// Перезапуск после сбоя: служба, которая не поднялась обратно, — это
	// бесплатное время для ребёнка.
	err = s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 15 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 60 * time.Second},
	}, 86400)
	if err != nil {
		return fmt.Errorf("настройка перезапуска: %w", err)
	}

	if err := eventlog.InstallAsEventCreate(Name, eventlog.Error|eventlog.Warning|eventlog.Info); err != nil {
		// Журнал не завёлся — не повод отказываться от уже созданной службы.
		return fmt.Errorf("служба создана, но журнал событий не настроен: %w", err)
	}
	return nil
}

// Uninstall убирает службу из системы.
func Uninstall() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("диспетчер служб (нужны права администратора): %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(Name)
	if err != nil {
		return fmt.Errorf("служба %s не установлена", Name)
	}
	defer s.Close()

	if err := s.Delete(); err != nil {
		return fmt.Errorf("удаление службы: %w", err)
	}
	_ = eventlog.Remove(Name)
	return nil
}

// Control шлёт службе команду и ждёт нужного состояния.
func Control(cmd svc.Cmd, want svc.State) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("диспетчер служб (нужны права администратора): %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(Name)
	if err != nil {
		return fmt.Errorf("служба %s не установлена", Name)
	}
	defer s.Close()

	status, err := s.Control(cmd)
	if err != nil {
		return fmt.Errorf("команда %d: %w", cmd, err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for status.State != want {
		if time.Now().After(deadline) {
			return fmt.Errorf("служба не перешла в состояние %d за 20 секунд", want)
		}
		time.Sleep(300 * time.Millisecond)
		if status, err = s.Query(); err != nil {
			return fmt.Errorf("опрос службы: %w", err)
		}
	}
	return nil
}

// Start запускает установленную службу.
func Start() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("диспетчер служб (нужны права администратора): %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(Name)
	if err != nil {
		return fmt.Errorf("служба %s не установлена", Name)
	}
	defer s.Close()
	return s.Start()
}

// Query — состояние службы словами.
func Query() (string, error) {
	m, err := mgr.Connect()
	if err != nil {
		return "", fmt.Errorf("диспетчер служб: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(Name)
	if err != nil {
		return "не установлена", nil
	}
	defer s.Close()

	status, err := s.Query()
	if err != nil {
		return "", err
	}
	return StateName(status.State), nil
}

// StateName переводит состояние службы на человеческий язык.
func StateName(s svc.State) string {
	switch s {
	case svc.Stopped:
		return "остановлена"
	case svc.StartPending:
		return "запускается"
	case svc.StopPending:
		return "останавливается"
	case svc.Running:
		return "работает"
	case svc.ContinuePending:
		return "возобновляется"
	case svc.PausePending:
		return "приостанавливается"
	case svc.Paused:
		return "приостановлена"
	default:
		return fmt.Sprintf("состояние %d", s)
	}
}
