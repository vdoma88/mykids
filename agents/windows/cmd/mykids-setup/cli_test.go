package main

import (
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"
)

func TestIsCommandLeavesDoubleClickToTheWindow(t *testing.T) {
	if isCommand(nil) {
		t.Error("без аргументов — это двойной щелчок, а не команда")
	}
	if isCommand([]string{"-server", "x"}) {
		t.Error("флаги без команды — не команда мастера")
	}
	for _, c := range []string{"accounts", "status", "verify", "stop", "apply"} {
		if !isCommand([]string{c}) {
			t.Errorf("%s — команда мастера", c)
		}
	}
}

func TestParseCLI(t *testing.T) {
	cmd, a, err := parseCLI([]string{"apply", "-dir", `C:\Program Files\MyKids`,
		"-server", "дом:3000", "-token", "t", "-child", "Маша", "-out", "r.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != "apply" || a.dir != `C:\Program Files\MyKids` || a.in.Server != "дом:3000" ||
		a.in.Token != "t" || a.in.ChildUser != "Маша" || a.out != "r.txt" {
		t.Errorf("разбор: %q %+v", cmd, a)
	}
	if _, _, err := parseCLI([]string{"verify", "-unknown"}); err == nil {
		t.Error("неизвестный флаг должен быть ошибкой")
	}
	if _, _, err := parseCLI([]string{"verify", "лишнее"}); err == nil {
		t.Error("лишний аргумент должен быть ошибкой")
	}
}

// Отчёт пишется и при неверном вызове: иначе мастер показал бы пустое окно
// ошибки, а это хуже любого текста.
func TestRunCommandWritesReportEvenOnUsageError(t *testing.T) {
	out := filepath.Join(t.TempDir(), "r.txt")
	if code := runCommand([]string{"verify", "-bogus", "-out", out}); code != exitUsage {
		// -out стоит после неизвестного флага и не разбирается; отчёт
		// тогда уходит в вывод. Проверяем оба порядка.
		t.Fatalf("код: %d", code)
	}
	if code := runCommand([]string{"verify", "-out", out, "-bogus"}); code != exitUsage {
		t.Fatalf("код: %d", code)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = uint16(raw[2*i]) | uint16(raw[2*i+1])<<8
	}
	if got := string(utf16.Decode(units)); len(got) == 0 || got[:len("Мастер")] != "Мастер" {
		t.Errorf("отчёт: %q", got)
	}
}
