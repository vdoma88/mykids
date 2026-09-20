package selftest

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// fake — машина, какой её видят проверки. По умолчанию исправная.
type fake struct {
	proc     string
	procErr  error
	idle     time.Duration
	idleErr  error
	locked   bool
	session  uint32
	state    string
	stateErr error
	elevated bool
}

func healthy() *fake {
	return &fake{proc: "game.exe", idle: 0, session: 2, state: "работает"}
}

func (f *fake) ForegroundProcess() (string, error) { return f.proc, f.procErr }
func (f *fake) IdleTime() (time.Duration, error)   { return f.idle, f.idleErr }
func (f *fake) SessionLocked() bool                { return f.locked }
func (f *fake) ActiveSession() uint32              { return f.session }
func (f *fake) ServiceState() (string, error)      { return f.state, f.stateErr }
func (f *fake) Elevated() bool                     { return f.elevated }

// rest — «родитель убрал руки»: простой растёт ровно на проспанное время.
func (f *fake) rest(d time.Duration) { f.idle += d }

func byName(rs []Result, name string) Result {
	for _, r := range rs {
		if strings.HasPrefix(r.Name, name) {
			return r
		}
	}
	return Result{Name: "нет проверки " + name}
}

func TestHealthyMachinePassesEverything(t *testing.T) {
	f := healthy()
	rs := Run(f, f.rest)

	if got := Worst(rs); got != Pass {
		for _, r := range rs {
			t.Logf("%s: %s — %s", r.Name, r.Status, r.Detail)
		}
		t.Fatalf("на исправной машине итог %q", got)
	}
	if len(rs) < 5 {
		t.Fatalf("проверок всего %d — слишком мало, чтобы что-то доказать", len(rs))
	}
}

func TestEveryResultExplainsItself(t *testing.T) {
	// Отчёт пересылают родителю, а не читают рядом с автором. Проверка без
	// объяснения, зачем она, — это строчка, с которой нечего делать.
	f := healthy()
	for _, r := range Run(f, f.rest) {
		if r.Why == "" {
			t.Errorf("%q не объясняет, что доказывает", r.Name)
		}
		if r.Detail == "" {
			t.Errorf("%q не говорит, что именно увидели", r.Name)
		}
	}
}

func TestFailureAlwaysSaysWhatToDo(t *testing.T) {
	// «Не работает» без следующего шага — тупик.
	broken := []struct {
		name   string
		break_ func(*fake)
	}{
		{"активное окно", func(f *fake) { f.procErr = errors.New("нет окна") }},
		{"пустое имя", func(f *fake) { f.proc = "" }},
		{"простой врёт", func(f *fake) { f.idle = 40 * time.Hour }},
		{"экран заблокирован", func(f *fake) { f.locked = true }},
		{"нет сессии", func(f *fake) { f.session = 0 }},
	}
	for _, b := range broken {
		f := healthy()
		b.break_(f)
		rs := Run(f, f.rest)
		if Worst(rs) != Fail {
			t.Errorf("%s: поломку не заметили", b.name)
			continue
		}
		for _, r := range rs {
			if r.Status == Fail && r.Hint == "" {
				t.Errorf("%s: проверка %q провалилась и молчит, что делать", b.name, r.Name)
			}
		}
	}
}

func TestHugeIdleIsCaughtAsUnitsMixup(t *testing.T) {
	// Классическая ошибка в этом месте — перепутанные единицы: миллисекунды
	// приняты за секунды. Тогда простой выйдет не на секунды больше, а на
	// сутки, и агент сочтёт отдыхом вообще всё.
	f := healthy()
	f.idle = 40 * time.Hour
	r := byName(Run(f, f.rest), "Время простоя")

	if r.Status != Fail {
		t.Fatalf("простой в сорок часов сразу после нажатия кнопки принят как %s", r.Status)
	}
	if !strings.Contains(r.Detail, "40") {
		t.Fatalf("в отчёте нет самого числа: %q", r.Detail)
	}
}

func TestIdleThatDoesNotGrowIsNotSilentlyAccepted(t *testing.T) {
	// Счётчик, застывший на месте, — это списание времени у ребёнка,
	// который отошёл от компьютера.
	f := healthy()
	rs := Run(f, func(time.Duration) {}) // время идёт, простой стоит
	r := byName(rs, "Время простоя")

	if r.Status == Pass {
		t.Fatalf("застывший счётчик простоя принят как исправный: %q", r.Detail)
	}
	if r.Hint == "" {
		t.Fatal("не сказано, что делать")
	}
}

func TestLockedScreenWhileWorkingIsAFailure(t *testing.T) {
	// Мы только что нажали кнопку в этом окне — экран заблокированным быть
	// не может. Если агент считает иначе, лимит не кончится никогда.
	f := healthy()
	f.locked = true
	if r := byName(Run(f, f.rest), "Состояние экрана"); r.Status != Fail {
		t.Fatalf("заблокированный экран при работе за ним принят как %s", r.Status)
	}
}

func TestMissingServiceIsNotAFailure(t *testing.T) {
	// Проверку гоняют и на машине, где службу ещё не ставили. Пугать
	// красным на этом — значит приучить не смотреть на красное.
	f := healthy()
	f.state = "не установлена"
	r := byName(Run(f, f.rest), "Служба")

	if r.Status != Warn {
		t.Fatalf("отсутствие службы показано как %s", r.Status)
	}
	if !strings.Contains(r.Hint, "install-windows.ps1") {
		t.Fatalf("не сказано, чем её поставить: %q", r.Hint)
	}
}

func TestStoppedServiceIsWorthSaying(t *testing.T) {
	f := healthy()
	f.state = "остановлена"
	if r := byName(Run(f, f.rest), "Служба"); r.Status == Pass {
		t.Fatal("остановленная служба показана как исправная")
	}
}

func TestAdminRightsAreFlagged(t *testing.T) {
	// Проверка должна показывать то, что увидит ребёнок, а он без прав.
	f := healthy()
	f.elevated = true
	r := byName(Run(f, f.rest), "Права")

	if r.Status != Warn {
		t.Fatalf("запуск от администратора показан как %s", r.Status)
	}
	if !strings.Contains(r.Hint, "ребёнка") {
		t.Fatalf("не сказано главное — повторить из учётной записи ребёнка: %q", r.Hint)
	}
}

func TestWorstTakesTheWorst(t *testing.T) {
	cases := []struct {
		in   []Result
		want Status
	}{
		{[]Result{{Status: Pass}, {Status: Pass}}, Pass},
		{[]Result{{Status: Pass}, {Status: Warn}}, Warn},
		{[]Result{{Status: Warn}, {Status: Fail}}, Fail},
		{[]Result{{Status: Fail}, {Status: Pass}}, Fail},
		{nil, Pass},
	}
	for _, c := range cases {
		if got := Worst(c.in); got != c.want {
			t.Errorf("Worst(%v) = %q, ожидалось %q", c.in, got, c.want)
		}
	}
}

func TestHumanReadsAsRussian(t *testing.T) {
	cases := map[time.Duration]string{
		3 * time.Second: "3 с", 90 * time.Second: "1 мин 30 с",
		2 * time.Hour: "2 ч 00 мин",
	}
	for d, want := range cases {
		if got := human(d); got != want {
			t.Errorf("human(%s) = %q, ожидалось %q", d, got, want)
		}
	}
}
