package ipc

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// listen поднимает службу на временном адресе и обслуживает всех помощников.
func listen(t *testing.T, h Handler) (string, *Desktop) {
	t.Helper()
	addr := filepath.Join(t.TempDir(), "agent.sock")
	l, err := Listen(addr)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	desktop := NewDesktop(0, time.Now)
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return // слушатель закрыт — служба выключается
			}
			go func() {
				_ = Serve(conn, func(s Sample) Verdict {
					desktop.Update(s)
					return h(s)
				})
			}()
		}
	}()
	return addr, desktop
}

func TestTwoProcessSplitEndToEnd(t *testing.T) {
	// То, ради чего всё разделение: помощник видит рабочий стол, служба решает.
	addr, desktop := listen(t, func(s Sample) Verdict {
		return Verdict{Allow: s.Process != "game.exe", LeftSecs: 600, Message: "хватит"}
	})

	conn, err := DialAddr(addr)
	if err != nil {
		t.Fatalf("DialAddr: %v", err)
	}
	c := Dial(conn)
	defer c.Close()

	v, err := c.Exchange(Sample{At: time.Now(), Process: "word.exe", IdleSeconds: 2})
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if !v.Allow {
		t.Fatalf("разрешённое приложение закрыто: %+v", v)
	}

	v, err = c.Exchange(Sample{At: time.Now(), Process: "game.exe"})
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if v.Allow || v.Message != "хватит" {
		t.Fatalf("решение службы не дошло: %+v", v)
	}

	// Служба видит рабочий стол только через помощника — и видит верно.
	if proc, _ := desktop.ForegroundProcess(); proc != "game.exe" {
		t.Fatalf("служба видит %q", proc)
	}
	if desktop.Silent() {
		t.Fatal("помощник на связи, а служба считает иначе")
	}
}

func TestServiceOutlivesHelperRestart(t *testing.T) {
	// Ребёнок снимает помощника из диспетчера задач. Служба обязана жить
	// дальше и принять его обратно после перезапуска.
	addr, desktop := listen(t, func(Sample) Verdict { return Verdict{Allow: true} })

	first, err := DialAddr(addr)
	if err != nil {
		t.Fatalf("DialAddr: %v", err)
	}
	c1 := Dial(first)
	if _, err := c1.Exchange(Sample{At: time.Now(), Process: "game.exe"}); err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	c1.Close()

	second, err := DialAddr(addr)
	if err != nil {
		t.Fatalf("служба не приняла новый помощник: %v", err)
	}
	c2 := Dial(second)
	defer c2.Close()
	if _, err := c2.Exchange(Sample{At: time.Now(), Process: "word.exe"}); err != nil {
		t.Fatalf("обмен после перезапуска: %v", err)
	}
	if proc, _ := desktop.ForegroundProcess(); proc != "word.exe" {
		t.Fatalf("служба не приняла наблюдения нового помощника: %q", proc)
	}
}

func TestHelperSeesServiceGone(t *testing.T) {
	// Обратная сторона: помощник обязан понять, что службы нет, а не считать
	// молчание разрешением.
	addr := filepath.Join(t.TempDir(), "agent.sock")
	l, err := Listen(addr)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		conn.Close() // служба падает сразу после соединения
	}()

	conn, err := DialAddr(addr)
	if err != nil {
		t.Fatalf("DialAddr: %v", err)
	}
	c := Dial(conn)
	defer c.Close()

	if _, err := c.Exchange(Sample{At: time.Now(), Process: "game.exe"}); err == nil {
		t.Fatal("помощник не заметил, что службы нет")
	}
	l.Close()
}

func TestListenFreesOrphanedSocket(t *testing.T) {
	// После нештатной остановки файл сокета остаётся. Служба обязана
	// подняться — ровно тогда, когда это важнее всего.
	addr := filepath.Join(t.TempDir(), "agent.sock")
	if err := os.WriteFile(addr, []byte("мусор от прошлого запуска"), 0o600); err != nil {
		t.Fatalf("подготовка: %v", err)
	}

	l, err := Listen(addr)
	if err != nil {
		t.Fatalf("осиротевший файл не дал подняться: %v", err)
	}
	l.Close()
}

func TestSocketIsPrivate(t *testing.T) {
	addr := filepath.Join(t.TempDir(), "agent.sock")
	l, err := Listen(addr)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer l.Close()

	info, err := os.Stat(addr)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("сокет открыт посторонним: %o", mode)
	}
}

func TestDialMissingServiceFails(t *testing.T) {
	// Помощник запустился раньше службы: это ошибка, а не тишина.
	_, err := DialAddr(filepath.Join(t.TempDir(), "нет.sock"))
	if err == nil {
		t.Fatal("подключение к несуществующей службе должно быть ошибкой")
	}
}

func TestManySessionsAtOnce(t *testing.T) {
	// На компьютере может быть несколько вошедших пользователей.
	addr, _ := listen(t, func(s Sample) Verdict { return Verdict{Allow: true, Window: s.Process} })

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			conn, err := DialAddr(addr)
			if err != nil {
				t.Errorf("сессия %d: %v", i, err)
				return
			}
			c := Dial(conn)
			defer c.Close()
			name := string(rune('a'+i)) + ".exe"
			for j := 0; j < 10; j++ {
				v, err := c.Exchange(Sample{At: time.Now(), Process: name})
				if err != nil {
					t.Errorf("сессия %d: %v", i, err)
					return
				}
				if v.Window != name {
					t.Errorf("сессия %d получила ответ для %q", i, v.Window)
					return
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestClosedListenerStopsAccepting(t *testing.T) {
	addr := filepath.Join(t.TempDir(), "agent.sock")
	l, err := Listen(addr)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	l.Close()

	if _, err := l.Accept(); err == nil {
		t.Fatal("закрытый слушатель принял соединение")
	} else if !errors.Is(err, net.ErrClosed) {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
}
