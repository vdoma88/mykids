package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Enrollment — привязка устройства к семье.
//
// Лежит отдельно от политики: политику агент перезаписывает сам при каждом
// обмене, а привязку задают один раз при установке. Смешав их в одном файле,
// мы бы затирали токен при каждом обновлении политики.
type Enrollment struct {
	ServerURL   string `json:"serverUrl"`
	DeviceToken string `json:"deviceToken"`
	// ChildURL — адрес интерфейса ребёнка: заданий и магазина. Задаётся
	// отдельно от ServerURL, потому что это разные вещи: API и страница живут
	// на разных портах, а то и на разных машинах. Угадывать адрес нельзя:
	// написать на закрытом экране ссылку, которая не открывается, — это
	// ровно то, из-за чего подросток перестаёт верить надписям вообще.
	ChildURL string `json:"childUrl,omitempty"`
}

// Configured сообщает, есть ли куда ходить.
func (e Enrollment) Configured() bool {
	return e.ServerURL != "" && e.DeviceToken != ""
}

// Redacted — адрес сервера и признак наличия токена, без самого токена.
// Нужно для диагностики: печатать токен в консоль нельзя, он даёт право
// списывать время от имени устройства.
func (e Enrollment) Redacted() string {
	if e.ServerURL == "" {
		return "не задан (автономный режим)"
	}
	if e.DeviceToken == "" {
		return e.ServerURL + " (токен устройства не задан)"
	}
	return e.ServerURL + " (токен задан)"
}

// LoadEnrollment читает привязку. Отсутствующий файл — не ошибка: агент
// работает и автономно, по локальной политике.
//
// Переменные окружения перекрывают файл: так удобнее проверять агента против
// тестового сервера, не трогая установленную привязку.
func LoadEnrollment(path string) (Enrollment, error) {
	var e Enrollment

	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &e); err != nil {
			return Enrollment{}, fmt.Errorf("%s: %w", path, err)
		}
	case !os.IsNotExist(err):
		return Enrollment{}, err
	}

	if v := strings.TrimSpace(os.Getenv("MYKIDS_SERVER")); v != "" {
		e.ServerURL = v
	}
	if v := strings.TrimSpace(os.Getenv("MYKIDS_DEVICE_TOKEN")); v != "" {
		e.DeviceToken = v
	}
	if v := strings.TrimSpace(os.Getenv("MYKIDS_CHILD_URL")); v != "" {
		e.ChildURL = v
	}
	e.ServerURL = strings.TrimRight(strings.TrimSpace(e.ServerURL), "/")
	e.DeviceToken = strings.TrimSpace(e.DeviceToken)
	e.ChildURL = strings.TrimRight(strings.TrimSpace(e.ChildURL), "/")
	return e, nil
}

// SaveEnrollment записывает привязку. Права 0600: токен устройства — секрет.
func SaveEnrollment(path string, e Enrollment) error {
	raw, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o600)
}
