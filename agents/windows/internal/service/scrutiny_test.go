package service

import (
	"strings"
	"testing"
	"time"

	"github.com/vdoma88/mykids/agents/windows/internal/ipc"
)

const threshold = 2 * time.Minute

var t0 = time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

// observe — наблюдение без собственного источника: своего мнения о блокировке
// у службы нет, и всё решает помощник. Так работает всё, что не Windows, и так
// же — Windows, когда спросить не удалось. Тесты встречной проверки зовут
// Observe напрямую.
func observe(s *Scrutiny, sample ipc.Sample, th time.Duration, now time.Time) Finding {
	return s.Observe(sample, ipc.LockUnknown, th, now)
}

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
		if f := observe(&s, idle("game.exe"), threshold, t0); f.Lying {
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
	observe(&s, idle("game.exe"), threshold, t0)
	if f := observe(&s, idle("notify.exe"), threshold, t0); f.Lying {
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
	observe(&s, idle("game.exe"), threshold, t0)

	var caught Finding
	for _, proc := range []string{"chrome.exe", "steam.exe", "discord.exe"} {
		if f := observe(&s, idle(proc), threshold, t0); f.Lying {
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
	observe(&s, locked("logonui.exe"), threshold, t0)

	var caught Finding
	for _, proc := range []string{"chrome.exe", "steam.exe", "game.exe"} {
		if f := observe(&s, locked(proc), threshold, t0); f.Lying {
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
		if f := observe(&s, active(proc), threshold, t0); f.Lying {
			t.Fatalf("обычная работа принята за ложь на замере %d: %s", i, f.Detail)
		}
	}
	// И после работы одна смена в покое всё ещё ничего не значит.
	observe(&s, idle("f.exe"), threshold, t0)
	if f := observe(&s, idle("g.exe"), threshold, t0); f.Lying {
		t.Fatalf("счёт не сбросился после работы: %s", f.Detail)
	}
}

func TestIdleInterruptedByWorkStartsOver(t *testing.T) {
	// Ребёнок поработал, отошёл, вернулся, снова отошёл. Смены, разделённые
	// работой, не складываются в обвинение.
	var s Scrutiny
	for i := 0; i < 5; i++ {
		observe(&s, idle("game.exe"), threshold, t0)
		observe(&s, idle("chrome.exe"), threshold, t0) // одна смена в покое
		observe(&s, active("chrome.exe"), threshold, t0)
	}
	if s.Distrusted(t0) {
		t.Fatal("обычное поведение сложилось в обвинение")
	}
}

func TestDistrustZeroesOutTheClaimedRest(t *testing.T) {
	// Поймав на лжи, служба перестаёт засчитывать покой: время идёт как
	// потраченное.
	var s Scrutiny
	observe(&s, idle("game.exe"), threshold, t0)
	for _, proc := range []string{"a.exe", "b.exe", "c.exe"} {
		observe(&s, idle(proc), threshold, t0)
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
	observe(&s, idle("game.exe"), threshold, t0)
	for _, proc := range []string{"a.exe", "b.exe", "c.exe"} {
		observe(&s, idle(proc), threshold, t0)
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
	observe(&s, idle("game.exe"), threshold, t0)

	lies := 0
	procs := []string{"a.exe", "b.exe", "c.exe", "d.exe", "e.exe", "f.exe"}
	for _, proc := range procs {
		if f := observe(&s, idle(proc), threshold, t0); f.Lying {
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
	observe(&s, idle("game.exe"), threshold, t0)
	for i := 0; i < 10; i++ {
		name := ""
		if i%2 == 1 {
			name = "game.exe"
		}
		if f := observe(&s, idle(name), threshold, t0); f.Lying {
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
	observe(&s, short("game.exe"), threshold, t0)
	for _, proc := range []string{"a.exe", "b.exe", "c.exe", "d.exe"} {
		if f := observe(&s, short(proc), threshold, t0); f.Lying {
			t.Fatalf("простой ниже порога принят за покой: %s", f.Detail)
		}
	}
}

// --- Встречная проверка состояния экрана ---
//
// Проверка на противоречии выше ловит помощника, который заявляет покой и
// переключает окна. Помощника, который заявляет блокировку и замирает на одном
// окне, она не ловит вовсе: противоречия нет. Здесь — про второй источник,
// который ловит именно это.

// claimsLocked — помощник заявляет заблокированный экран и не меняет окно.
// Самая выгодная ложь: времени не тратится никогда, а поймать не на чем.
func claimsLocked(proc string) ipc.Sample {
	return ipc.Sample{Process: proc, SessionLocked: true, IdleSeconds: 1}
}

func TestClaimedLockIsDroppedWhenWindowsSaysOpen(t *testing.T) {
	// Главное в этой проверке — и оно происходит сразу, без всякого счёта.
	// Помощник заявил блокировку, Windows ответила «сессия открыта», и
	// заявленная блокировка снимается с первого же замера: пока идёт спор,
	// время должно списываться, а не стоять.
	var s Scrutiny
	f := s.Observe(claimsLocked("game.exe"), ipc.LockOff, threshold, t0)
	if f.Lying {
		t.Fatalf("на одном расхождении сразу обвинили: %s", f.Detail)
	}
	got := s.Correct(claimsLocked("game.exe"), t0)
	if got.SessionLocked {
		t.Fatal("заявленная блокировка пережила ответ Windows «открыт» — это и есть бесплатное время")
	}
}

func TestPersistentLockLieIsReportedToParent(t *testing.T) {
	var s Scrutiny
	var caught Finding
	for i := 0; i < LockLieThreshold; i++ {
		if f := s.Observe(claimsLocked("game.exe"), ipc.LockOff, threshold, t0); f.Lying {
			caught = f
		}
	}
	if !caught.Lying {
		t.Fatal("упорная ложь о блокировке не дошла до родителя")
	}
	for _, want := range []string{"заблокированный экран", "Windows"} {
		if !strings.Contains(caught.Detail, want) {
			t.Fatalf("родителю не объяснили, кто кого уличил (нет %q): %q", want, caught.Detail)
		}
	}
	if !s.Distrusted(t0) {
		t.Fatal("соврав о блокировке, помощник сохранил доверие к простою")
	}
}

func TestSingleLockDisagreementIsARace(t *testing.T) {
	// Ребёнок разблокировал экран ровно между замером помощника и вопросом
	// службы. Расхождение честное, и обвинять за него нельзя.
	var s Scrutiny
	if f := s.Observe(claimsLocked("game.exe"), ipc.LockOff, threshold, t0); f.Lying {
		t.Fatalf("гонка принята за ложь: %s", f.Detail)
	}
	if s.Distrusted(t0) {
		t.Fatal("одно расхождение лишило доверия")
	}
}

func TestLockDisagreementCountResets(t *testing.T) {
	// Расхождения, разделённые согласием, не складываются в обвинение: иначе
	// ребёнок, который блокирует экран по десять раз на дню, рано или поздно
	// получил бы обвинение из одних гонок.
	var s Scrutiny
	for i := 0; i < 10; i++ {
		s.Observe(claimsLocked("game.exe"), ipc.LockOff, threshold, t0) // гонка
		s.Observe(claimsLocked("game.exe"), ipc.LockOn, threshold, t0)  // согласие
	}
	if s.Distrusted(t0) {
		t.Fatal("чередование гонок и согласия сложилось в обвинение")
	}
}

func TestWindowsConfirmingLockIsNotALie(t *testing.T) {
	// Помощник говорит правду, и это норма, а не повод для счёта.
	var s Scrutiny
	for i := 0; i < 50; i++ {
		if f := s.Observe(claimsLocked("game.exe"), ipc.LockOn, threshold, t0); f.Lying {
			t.Fatalf("подтверждённая блокировка принята за ложь на замере %d: %s", i, f.Detail)
		}
	}
	if s.Distrusted(t0) {
		t.Fatal("честному помощнику перестали верить")
	}
}

func TestWindowsLockIsAcceptedOverHelpersDenial(t *testing.T) {
	// Обратная сторона: помощник говорит «открыт», Windows — «заблокирован».
	// Верим Windows. Заблокировать экран и одновременно им пользоваться
	// нельзя, а значит списывать это время не за что.
	var s Scrutiny
	in := ipc.Sample{Process: "game.exe", IdleSeconds: 1}
	s.Observe(in, ipc.LockOn, threshold, t0)
	if got := s.Correct(in, t0); !got.SessionLocked {
		t.Fatal("служба не приняла собственный ответ Windows о блокировке")
	}
}

func TestHelperDenyingLockIsNotAccused(t *testing.T) {
	// И обвинять за эту сторону расхождения не за что: она ребёнку невыгодна,
	// а помощник мог замерить экран за мгновение до блокировки.
	var s Scrutiny
	in := ipc.Sample{Process: "game.exe", IdleSeconds: 1}
	for i := 0; i < 20; i++ {
		if f := s.Observe(in, ipc.LockOn, threshold, t0); f.Lying {
			t.Fatalf("обвинили за расхождение не в свою пользу: %s", f.Detail)
		}
	}
	if s.Distrusted(t0) {
		t.Fatal("расхождение не в свою пользу лишило доверия")
	}
}

func TestUnknownLockChangesNothing(t *testing.T) {
	// Windows не ответила — значит, всё как до встречной проверки: слово
	// помощника. Иначе на неудачном вызове система вела бы себя случайно.
	var s Scrutiny
	for i := 0; i < 20; i++ {
		if f := s.Observe(claimsLocked("game.exe"), ipc.LockUnknown, threshold, t0); f.Lying {
			t.Fatalf("незнание принято за улику: %s", f.Detail)
		}
	}
	got := s.Correct(claimsLocked("game.exe"), t0)
	if !got.SessionLocked {
		t.Fatal("незнание отменило заявленную блокировку — так у помощника отняли бы слово без причины")
	}
}

func TestFalseLockCannotHideWindowChanges(t *testing.T) {
	// Помощник заявляет блокировку, чтобы спрятаться за ней от проверки на
	// противоречии, а окна при этом переключает. Снятая блокировка возвращает
	// его под ту же проверку: покой теперь только по простою, а простоя нет.
	var s Scrutiny
	for _, proc := range []string{"a.exe", "b.exe", "c.exe", "d.exe"} {
		s.Observe(ipc.Sample{Process: proc, SessionLocked: true, IdleSeconds: 1},
			ipc.LockOff, threshold, t0)
	}
	got := s.Correct(ipc.Sample{Process: "a.exe", SessionLocked: true, IdleSeconds: 9999}, t0)
	if got.SessionLocked {
		t.Fatal("ложная блокировка осталась")
	}
	if got.IdleSeconds != 0 {
		t.Fatal("простою всё ещё верят: помощник, пойманный на блокировке, доверия к простою не сохраняет")
	}
}
