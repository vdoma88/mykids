package watchdog

import (
	"errors"
	"strings"
	"testing"
	"time"
)

type fake struct {
	session Session
	alive   bool
	err     error
	starts  int
}

func (f *fake) ActiveSession() Session { return f.session }
func (f *fake) HelperAlive() bool      { return f.alive }
func (f *fake) StartHelper(Session) error {
	if f.err != nil {
		return f.err
	}
	f.starts++
	f.alive = true
	return nil
}

func at(seconds int) time.Time {
	return time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC).Add(time.Duration(seconds) * time.Second)
}

func TestHelperIsStartedWhenMissing(t *testing.T) {
	// Главное, ради чего сторож и нужен: до сих пор помощника не запускал
	// никто, и боевая установка была слепой.
	f := &fake{session: 7}
	a := New(f).Check(at(0))

	if !a.Started || f.starts != 1 {
		t.Fatalf("помощник не поднят: %+v, запусков %d", a, f.starts)
	}
}

func TestLiveHelperIsLeftAlone(t *testing.T) {
	f := &fake{session: 7, alive: true}
	if a := New(f).Check(at(0)); a.Started || f.starts != 0 {
		t.Fatalf("живого помощника перезапустили: %+v", a)
	}
}

func TestNobodyLoggedInIsNotAFailure(t *testing.T) {
	// Пустая машина — не сбой: экран никто не смотрит, поднимать нечего.
	f := &fake{session: 0}
	a := New(f).Check(at(0))
	if a.Started || a.Err != nil || a.Tamper {
		t.Fatalf("на пустой машине сторож что-то сделал: %+v", a)
	}
}

func TestFailedStartBacksOff(t *testing.T) {
	// При входе в систему рабочий стол ещё не готов, и первая попытка часто
	// падает просто потому, что рано. Крутиться в этот момент в цикле —
	// верный способ забить журнал ошибками, которые прошли бы сами.
	f := &fake{session: 7, err: errors.New("рабочий стол не готов")}
	w := New(f)

	if a := w.Check(at(0)); a.Err == nil {
		t.Fatal("ошибка запуска потеряна")
	}
	// Внутри паузы не пробуем.
	tries := 0
	f.err = nil
	for s := 1; s < int(FirstDelay.Seconds()); s++ {
		if w.Check(at(s)).Started {
			tries++
		}
	}
	if tries != 0 {
		t.Fatalf("сторож пробовал %d раз внутри паузы", tries)
	}
	if a := w.Check(at(int(FirstDelay.Seconds()))); !a.Started {
		t.Fatalf("после паузы помощник так и не поднят: %+v", a)
	}
}

func TestBackoffGrowsButStopsAtCeiling(t *testing.T) {
	// Растить паузу до бесконечности нельзя: помощника нет, значит экран
	// закрыт, и ребёнок ждёт.
	f := &fake{session: 7, err: errors.New("не вышло")}
	w := New(f)

	now := 0
	var last time.Duration
	for i := 0; i < 20; i++ {
		w.Check(at(now))
		last = w.delay
		now += int(w.delay.Seconds())
	}
	if last != MaxDelay {
		t.Fatalf("пауза дошла до %s вместо %s", last, MaxDelay)
	}
}

func TestRepeatedRestartsAreReportedAsTampering(t *testing.T) {
	// Один-два перезапуска — починка, о которой родителю знать незачем.
	// Помощник, умирающий раз за разом, — это ребёнок с диспетчером задач.
	f := &fake{session: 7}
	w := New(f)

	var tampers int
	for i := 0; i < Restarts; i++ {
		f.alive = false // ребёнок снял помощника
		a := w.Check(at(i * 10))
		if !a.Started {
			t.Fatalf("перезапуск %d не состоялся: %+v", i, a)
		}
		if a.Tamper {
			tampers++
			if i+1 < Restarts {
				t.Fatalf("о снятии сообщено на %d-м перезапуске, порог %d", i+1, Restarts)
			}
			if !strings.Contains(a.Detail, "помощник") {
				t.Fatalf("родителю непонятно, что случилось: %q", a.Detail)
			}
		}
	}
	if tampers != 1 {
		t.Fatalf("событий о снятии %d, ожидалось одно", tampers)
	}
}

func TestTamperIsReportedOnce(t *testing.T) {
	// Поток одинаковых событий хуже одного: в нём тонет всё остальное.
	f := &fake{session: 7}
	w := New(f)

	tampers := 0
	for i := 0; i < Restarts*3; i++ {
		f.alive = false
		if w.Check(at(i * 10)).Tamper {
			tampers++
		}
	}
	if tampers != 1 {
		t.Fatalf("событий о снятии %d, ожидалось одно", tampers)
	}
}

func TestRestartsOutsideWindowDoNotAccumulate(t *testing.T) {
	// Иначе машина, работающая месяцами, рано или поздно «уличит» ребёнка
	// в снятии помощника просто по сумме случайных сбоев.
	f := &fake{session: 7}
	w := New(f)

	step := int(Window.Seconds())
	for i := 0; i < Restarts*2; i++ {
		f.alive = false
		if a := w.Check(at(i * step)); a.Tamper {
			t.Fatalf("редкие перезапуски сочтены снятием на %d-м", i+1)
		}
	}
}

func TestLogoutForgetsTheCount(t *testing.T) {
	// Выход из системы и вход обратно — не снятие помощника.
	f := &fake{session: 7}
	w := New(f)
	for i := 0; i < Restarts-1; i++ {
		f.alive = false
		w.Check(at(i * 10))
	}

	f.session, f.alive = 0, false
	w.Check(at(100)) // ребёнок вышел

	f.session = 9
	for i := 0; i < Restarts-1; i++ {
		f.alive = false
		if a := w.Check(at(200 + i*10)); a.Tamper {
			t.Fatalf("перезапуски до выхода зачлись после входа (на %d-м)", i+1)
		}
	}
}

func TestCountDeclines(t *testing.T) {
	cases := map[int]string{1: "1 раз", 2: "2 раза", 5: "5 раз", 11: "11 раз", 22: "22 раза"}
	for n, want := range cases {
		if got := count(n); got != want {
			t.Errorf("count(%d) = %q, ожидалось %q", n, got, want)
		}
	}
}
