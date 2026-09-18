// Package policy решает, по какой политике агент работает прямо сейчас.
//
// Локальный файл политики лежит в профиле пользователя, и ребёнок может его
// править. Поэтому он не источник правды, а запасной вариант: как только агент
// хоть раз поговорил с сервером, решения принимаются по серверной копии, а
// правка файла руками перестаёт что-либо значить.
package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/vdoma88/mykids/agents/windows/internal/config"
)

// Source — откуда взялась действующая политика.
type Source string

const (
	// FromServer — только что полученная по сети.
	FromServer Source = "сервер"
	// FromCache — серверная копия с прошлого сеанса связи.
	FromCache Source = "кэш"
	// FromLocal — сервера не видели ни разу.
	FromLocal Source = "локальный файл"
)

// StaleAfter — после какого возраста кэш стоит показать родителю как
// подозрительный. Устаревший кэш всё равно применяется: связь могла пропасть
// не по вине ребёнка, а снимать ограничения из-за этого нельзя.
const StaleAfter = 72 * time.Hour

// Границы настроек, которых нет на сервере.
//
// Порог простоя приходит из локального файла, и без ограничения снизу ребёнок
// занизил бы его до секунды: чтение и видео перестали бы тратить лимит, потому
// что клавиатура в эти минуты молчит. Сверху ограничиваем, чтобы забытый в
// файле час не превратил любой перерыв в потраченное время.
const (
	MinIdleThresholdSeconds = 30
	MaxIdleThresholdSeconds = 600
	MaxWarnBeforeMinutes    = 30
)

// Resolved — действующая политика и её происхождение.
type Resolved struct {
	Policy    config.Policy
	Source    Source
	FetchedAt time.Time
	// Note — почему источник не тот, которого ждали: битый кэш, отказ проверки.
	Note string
}

// Age — сколько прошло с последнего успешного обмена с сервером.
func (r Resolved) Age(now time.Time) time.Duration {
	if r.FetchedAt.IsZero() {
		return 0
	}
	return now.Sub(r.FetchedAt)
}

// Stale сообщает, что связи не было слишком давно.
func (r Resolved) Stale(now time.Time) bool {
	return r.Source == FromCache && r.Age(now) > StaleAfter
}

// Clamp приводит агентские настройки к безопасным границам.
func Clamp(p config.Policy) config.Policy {
	if p.IdleThresholdSeconds < MinIdleThresholdSeconds {
		p.IdleThresholdSeconds = MinIdleThresholdSeconds
	}
	if p.IdleThresholdSeconds > MaxIdleThresholdSeconds {
		p.IdleThresholdSeconds = MaxIdleThresholdSeconds
	}
	if p.WarnBeforeMinutes < 0 {
		p.WarnBeforeMinutes = 0
	}
	if p.WarnBeforeMinutes > MaxWarnBeforeMinutes {
		p.WarnBeforeMinutes = MaxWarnBeforeMinutes
	}
	return p
}

type cacheFile struct {
	FetchedAt time.Time     `json:"fetchedAt"`
	Policy    config.Policy `json:"policy"`
}

// Save кладёт серверную политику в кэш. Запись атомарна: обрыв питания не
// должен оставить обрезанный файл, из-за которого агент откатится к
// локальному и снимет ограничения.
func Save(path string, p config.Policy, fetchedAt time.Time) error {
	raw, err := json.MarshalIndent(cacheFile{FetchedAt: fetchedAt, Policy: Clamp(p)}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(raw, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Resolve выбирает политику для текущего запуска.
//
// Кэш выигрывает у локального файла всегда, даже устаревший: иначе достаточно
// было бы отключить сеть, чтобы вернуться к правленому вручную файлу.
// Уступает он только если прочитать его нельзя — тогда работать не по чему,
// кроме локального.
func Resolve(local config.Policy, cachePath string) Resolved {
	fallback := Resolved{Policy: Clamp(local), Source: FromLocal}

	raw, err := os.ReadFile(cachePath)
	if os.IsNotExist(err) {
		return fallback
	}
	if err != nil {
		fallback.Note = fmt.Sprintf("кэш политики не прочитан: %v", err)
		return fallback
	}

	var cached cacheFile
	if err := json.Unmarshal(raw, &cached); err != nil {
		fallback.Note = fmt.Sprintf("кэш политики повреждён: %v", err)
		return fallback
	}
	if err := cached.Policy.Validate(); err != nil {
		// Невалидный кэш опаснее отсутствующего: агент принимал бы решения
		// по нулевым лимитам и заблокировал бы экран навсегда.
		fallback.Note = fmt.Sprintf("кэш политики не прошёл проверку: %v", err)
		return fallback
	}

	return Resolved{
		Policy:    Clamp(cached.Policy),
		Source:    FromCache,
		FetchedAt: cached.FetchedAt,
	}
}
