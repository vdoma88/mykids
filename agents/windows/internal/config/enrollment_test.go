package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadEnrollmentMissingFile(t *testing.T) {
	// Агент обязан работать автономно, а не падать без привязки.
	e, err := LoadEnrollment(filepath.Join(t.TempDir(), "enrollment.json"))
	if err != nil {
		t.Fatalf("отсутствие файла не ошибка: %v", err)
	}
	if e.Configured() {
		t.Fatalf("пустая привязка не может быть настроенной: %+v", e)
	}
}

func TestLoadEnrollmentFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "enrollment.json")
	if err := SaveEnrollment(path, Enrollment{ServerURL: "https://api.example/", DeviceToken: "  t  "}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	e, err := LoadEnrollment(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if e.ServerURL != "https://api.example" {
		t.Fatalf("слеш на конце не убран: %q", e.ServerURL)
	}
	if e.DeviceToken != "t" {
		t.Fatalf("пробелы в токене не убраны: %q", e.DeviceToken)
	}
	if !e.Configured() {
		t.Fatal("привязка должна считаться настроенной")
	}
}

func TestEnvOverridesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "enrollment.json")
	if err := SaveEnrollment(path, Enrollment{ServerURL: "https://prod", DeviceToken: "prod-token"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	t.Setenv("MYKIDS_SERVER", "http://127.0.0.1:3000")
	t.Setenv("MYKIDS_DEVICE_TOKEN", "test-token")

	e, err := LoadEnrollment(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if e.ServerURL != "http://127.0.0.1:3000" || e.DeviceToken != "test-token" {
		t.Fatalf("окружение не перекрыло файл: %+v", e)
	}
}

func TestEmptyEnvDoesNotWipeFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "enrollment.json")
	if err := SaveEnrollment(path, Enrollment{ServerURL: "https://prod", DeviceToken: "prod-token"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	t.Setenv("MYKIDS_SERVER", "")
	t.Setenv("MYKIDS_DEVICE_TOKEN", "   ")

	e, err := LoadEnrollment(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !e.Configured() {
		t.Fatalf("пустая переменная стёрла привязку: %+v", e)
	}
}

func TestEnrollmentFileIsPrivate(t *testing.T) {
	// Токен устройства даёт право списывать время от его имени.
	path := filepath.Join(t.TempDir(), "enrollment.json")
	if err := SaveEnrollment(path, Enrollment{ServerURL: "https://a", DeviceToken: "secret"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("файл привязки открыт всем: %o", mode)
	}
}

func TestRedactedHidesToken(t *testing.T) {
	e := Enrollment{ServerURL: "https://api.example", DeviceToken: "s3cret"}
	got := e.Redacted()
	if strings.Contains(got, "s3cret") {
		t.Fatalf("токен попал в диагностику: %q", got)
	}
	if !strings.Contains(got, "api.example") {
		t.Fatalf("адрес сервера потерян: %q", got)
	}
	if !strings.Contains(Enrollment{}.Redacted(), "автономный") {
		t.Fatalf("автономный режим не назван: %q", Enrollment{}.Redacted())
	}
	if !strings.Contains(Enrollment{ServerURL: "https://a"}.Redacted(), "токен устройства не задан") {
		t.Fatal("отсутствие токена должно быть видно в диагностике")
	}
}

func TestBrokenEnrollmentIsError(t *testing.T) {
	// Битую привязку молча считать автономным режимом нельзя: ребёнок сломал
	// бы файл и остался без серверной политики.
	path := filepath.Join(t.TempDir(), "enrollment.json")
	if err := os.WriteFile(path, []byte("{сломано"), 0o600); err != nil {
		t.Fatalf("подготовка: %v", err)
	}
	if _, err := LoadEnrollment(path); err == nil {
		t.Fatal("битый файл привязки должен быть ошибкой")
	}
}
