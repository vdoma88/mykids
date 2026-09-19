package service

import (
	"strings"
	"testing"
	"time"

	"github.com/vdoma88/mykids/agents/windows/internal/ipc"
)

const threshold = 2 * time.Minute

var t0 = time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

// idle — наблюдение с заявленным простоем.
func idle(proc string) ipc.Sample {
	return ipc.Sample{Process: proc, IdleSeconds: 9999}
}

// active — наблюдение с работой за компьютером.
func active(proc string) ipc.Sample {
	return ipc.Sample{Process: proc, IdleSeconds: 1}
}

func TestHonestIdleIsNotSuspected(t *testing.T) {
	// Ребёнок отошёл: окно не меняется, простой растёт. Это норма.
	var s Scrutiny
	for i := 0; i < 50; i++ {
		if f := s.Observe(idle("game.exe"), threshold, t0); f.Lying {
			t.Fatalf("честный простой принят за ложь на замере %d: %s", i, f.Detail)
		}
	}
	if s.Distrusted(t0) {
		t.Fatal("честному помощнику перестали верить")
	}
}

func TestSingleWindowChangeDuringIdleIsTolerated(t *testing.T) {
	// Окно умеет меняться и само: всплывшее уведомление, задача по расписанию.
	// Одна смена ничего не доказывает.
	var s Scrutiny
	s.Observe(idle("game.exe"), threshold, t0)
	if f := s.Observe(idle("notify.exe"), threshold, t0); f.Lying {
		t.Fatalf("одна смена окна принята за ложь: %s", f.Detail)
	}
	if s.Distrusted(t0) {
		t.Fatal("одна смена окна лишила доверия")
	}
}

func TestWindowsChangingDuringClaimedIdleIsCaught(t *testing.T) {
	// Главное: помощник заявляет простой, а окна сменяются одно за другим.
	// Без ввода окна не переключаются.
	var s Scrutiny
	s.Observe(idle("game.exe"), threshold, t0)

	var caught Finding
	for _, proc := range []string{"chrome.exe", "steam.exe", "discord.exe"} {
		if f := s.Observe(idle(proc), threshold, t0); f.Lying {
			caught = f
		}
	}
	if !caught.Lying {
		t.Fatal("ложь о простое не поймана")
	}
	if !strings.Contains(caught.Detail, "простой") {
		t.Fatalf("родителю не объяснили, в чём ложь: %q", caught.Detail)
	}
	if !s.Distrusted(t0) {
		t.Fatal("после пойманной лжи простою всё ещё верят")
	}
}

func TestLockedScreenLieIsCaughtToo(t *testing.T) {
	// Заблокированный экран тоже не тратит время, и соврать о нём так же выгодно.
	var s Scrutiny
	locked := func(p string) ipc.Sample {
		return ipc.Sample{Process: p, SessionLocked: true, IdleSeconds: 1}
	}
	s.Observe(locked("logonui.exe"), threshold, t0)

	var caught Finding
	for _, proc := range []string{"chrome.exe", "steam.exe", "game.exe"} {
		if f := s.Observe(locked(proc), threshold, t0); f.Lying {
			caught = f
		}
	}
	if !caught.Lying {
		t.Fatal("ложь о блокировке не поймана")
	}
	if !strings.Contains(caught.Detail, "заблокированный") {
		t.Fatalf("в сообщении не сказано, о чём именно соврали: %q", caught.Detail)
	}
}

func TestActivityResetsTheCount(t *testing.T) {
	// Окна меняются, когда за компьютером работают, — это и есть норма.
	// Считать смены нужно только внутри непрерывного заявленного покоя.
	var s Scrutiny
	for i, proc := range []string{"a.exe", "b.exe", "c.exe", "d.exe", "e.exe"} {
		if f := s.Observe(active(proc), threshold, t0); f.Lying {
			t.Fatalf("обычная работа принята за ложь на замере %d: %s", i, f.Detail)
		}
	}
	// И после работы одна смена в покое всё ещё ничего не значит.
	s.Observe(idle("f.exe"), threshold, t0)
	if f := s.Observe(idle("g.exe"), threshold, t0); f.Lying {
		t.Fatalf("счёт не сбросился после работы: %s", f.Detail)
	}
}

func TestIdleInterruptedByWorkStartsOver(t *testing.T) {
	// Ребёнок поработал, отошёл, вернулся, снова отошёл. Смены, разделённые
	// работой, не складываются в обвинение.
	var s Scrutiny
	for i := 0; i < 5; i++ {
		s.Observe(idle("game.exe"), threshold, t0)
		s.Observe(idle("chrome.exe"), threshold, t0) // одна смена в покое
		s.Observe(active("chrome.exe"), threshold, t0)
	}
	if s.Distrusted(t0) {
		t.Fatal("обычное поведение сложилось в обвинение")
	}
}

func TestDistrustZeroesOutTheClaimedRest(t *testing.T) {
	// Поймав на лжи, служба перестаёт засчитывать покой: время идёт как
	// потраченное.
	var s Scrutiny
	s.Observe(idle("game.exe"), threshold, t0)
	for _, proc := range []string{"a.exe", "b.exe", "c.exe"} {
		s.Observe(idle(proc), threshold, t0)
	}

	got := s.Correct(ipc.Sample{Process: "game.exe", IdleSeconds: 9999, SessionLocked: true}, t0)
	if got.IdleSeconds != 0 {
		t.Fatalf("простою всё ещё верят: %d", got.IdleSeconds)
	}
	if got.SessionLocked {
		t.Fatal("блокировке всё ещё верят")
	}
	// А вот активное окно остаётся: о нём помощник врать не может, не попавшись.
	if got.Process != "game.exe" {
		t.Fatalf("активное окно выброшено: %q", got.Process)
	}
}

func TestDistrustExpires(t *testing.T) {
	// Не навсегда: вечное недоверие означало бы, что ребёнок больше никогда
	// не сможет отойти от компьютера без списания.
	var s Scrutiny
	s.Observe(idle("game.exe"), threshold, t0)
	for _, proc := range []string{"a.exe", "b.exe", "c.exe"} {
		s.Observe(idle(proc), threshold, t0)
	}
	if !s.Distrusted(t0.Add(DistrustFor - time.Second)) {
		t.Fatal("недоверие кончилось слишком рано")
	}
	if s.Distrusted(t0.Add(DistrustFor + time.Second)) {
		t.Fatal("недоверие не кончается вовсе")
	}

	after := t0.Add(DistrustFor + time.Minute)
	if got := s.Correct(idle("game.exe"), after); got.IdleSeconds == 0 {
		t.Fatal("после истечения недоверия простой всё ещё обнуляется")
	}
}

func TestHonestSampleIsNotAltered(t *testing.T) {
	var s Scrutiny
	in := ipc.Sample{Process: "game.exe", IdleSeconds: 300, SessionLocked: true}
	if got := s.Correct(in, t0); got != in {
		t.Fatalf("наблюдение изменено без причины: %+v", got)
	}
}

func TestRepeatedLieDoesNotSpamParent(t *testing.T) {
	// Сообщать родителю одно и то же каждую секунду — значит, что он перестанет
	// это читать.
	var s Scrutiny
	s.Observe(idle("game.exe"), threshold, t0)

	lies := 0
	procs := []string{"a.exe", "b.exe", "c.exe", "d.exe", "e.exe", "f.exe"}
	for _, proc := range procs {
		if f := s.Observe(idle(proc), threshold, t0); f.Lying {
			lies++
		}
	}
	if lies != len(procs)/LieThreshold {
		t.Fatalf("на %d смен пришлось %d сообщений, ожидалось %d",
			len(procs), lies, len(procs)/LieThreshold)
	}
}

func TestEmptyProcessIsNotACharge(t *testing.T) {
	// Пустое имя означает «окна нет» — заблокированный экран или системное
	// окно. Считать это сменой значило бы обвинять на ровном месте.
	//
	// Чередуем с настоящим именем: подряд идущие пустые совпадают друг с
	// другом и прошли бы проверку даже без защиты. Ловит здесь именно переход
	// «окно — нет окна — окно».
	var s Scrutiny
	s.Observe(idle("game.exe"), threshold, t0)
	for i := 0; i < 10; i++ {
		name := ""
		if i%2 == 1 {
			name = "game.exe"
		}
		if f := s.Observe(idle(name), threshold, t0); f.Lying {
			t.Fatalf("исчезновение окна принято за смену на замере %d: %s", i, f.Detail)
		}
	}
}

func TestIdleBelowThresholdIsNotRest(t *testing.T) {
	// Учёт списывает время, пока простой меньше порога: покоя нет, и считать
	// смены окон незачем.
	var s Scrutiny
	short := func(p string) ipc.Sample {
		return ipc.Sample{Process: p, IdleSeconds: int(threshold.Seconds()) - 1}
	}
	s.Observe(short("game.exe"), threshold, t0)
	for _, proc := range []string{"a.exe", "b.exe", "c.exe", "d.exe"} {
		if f := s.Observe(short(proc), threshold, t0); f.Lying {
			t.Fatalf("простой ниже порога принят за покой: %s", f.Detail)
		}
	}
}
