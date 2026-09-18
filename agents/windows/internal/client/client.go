// Package client — обращения агента к серверу семьи.
//
// Асимметрия офлайна закреплена здесь: расход экрана уходит постфактум и
// принимается сервером, а начисления агент не делает вовсе — их считает сервер.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/vdoma88/mykids/agents/windows/internal/config"
	"github.com/vdoma88/mykids/agents/windows/internal/schedule"
)

// ErrUnauthorized — токен неизвестен или отозван. Повторять бесполезно.
var ErrUnauthorized = errors.New("устройство не авторизовано")

// ErrOffline — сервер недоступен. Не ошибка работы: агент обязан продолжать
// считать время и локально.
var ErrOffline = errors.New("сервер недоступен")

// Client ходит на API от имени устройства.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
	version string
}

// New создаёт клиента. Пустой baseURL означает автономный режим без сервера.
func New(baseURL, token, version string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		version: version,
		// Таймаут короткий: агент опрашивает рабочий стол каждые несколько
		// секунд и не может висеть на сетевом вызове.
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

// Configured сообщает, есть ли куда ходить.
func (c *Client) Configured() bool { return c.baseURL != "" && c.token != "" }

// Balances — остатки ребёнка на сервере.
type Balances struct {
	Minutes int `json:"minutes"`
	Credits int `json:"credits"`
}

// ScreenState — решение сервера о доступности экрана.
type ScreenState struct {
	Allowed     bool   `json:"allowed"`
	Reason      string `json:"reason"`
	Window      string `json:"window,omitempty"`
	MinutesLeft int    `json:"minutesLeft,omitempty"`
}

// ServerPolicy — политика в том виде, в каком её отдаёт сервер.
//
// Это не config.Policy: доменная схема не содержит настроек, нужных только
// агенту. Разбирать ответ в общий тип было бы ошибкой — отсутствующие поля
// стали бы нулями, порог простоя обнулился, а белый список опустел.
type ServerPolicy struct {
	Timezone            string            `json:"timezone"`
	DailyLimitMinutes   []int             `json:"dailyLimitMinutes"`
	CarryOverMaxMinutes int               `json:"carryOverMaxMinutes"`
	Windows             []schedule.Window `json:"windows"`
	Economy             struct {
		CreditsPerMinute          int `json:"creditsPerMinute"`
		MaxConvertedMinutesPerDay int `json:"maxConvertedMinutesPerDay"`
		MinCreditsToConvert       int `json:"minCreditsToConvert"`
		MaxCreditsPerDay          int `json:"maxCreditsPerDay"`
	} `json:"economy"`
}

// AgentSettings — то, что нужно агенту сверх доменной политики.
type AgentSettings struct {
	AlwaysAllowed []string `json:"alwaysAllowed"`
}

// SyncResponse — ответ /agent/sync.
type SyncResponse struct {
	ServerTime time.Time     `json:"serverTime"`
	Policy     ServerPolicy  `json:"policy"`
	Agent      AgentSettings `json:"agent"`
	Balances   Balances      `json:"balances"`
	Screen     ScreenState   `json:"screen"`
}

// Merge накладывает политику сервера на локальную, сохраняя настройки, которых
// у сервера нет: порог простоя и предупреждение — технические, не родительские.
func (r *SyncResponse) Merge(local config.Policy) config.Policy {
	merged := local
	merged.Timezone = r.Policy.Timezone
	merged.DailyLimitMinutes = r.Policy.DailyLimitMinutes
	merged.CarryOverMaxMinutes = r.Policy.CarryOverMaxMinutes
	merged.Windows = r.Policy.Windows
	if len(r.Agent.AlwaysAllowed) > 0 {
		merged.AlwaysAllowed = r.Agent.AlwaysAllowed
	}
	return merged
}

// UsageEntry — одна порция израсходованного времени.
type UsageEntry struct {
	Seq        int       `json:"seq"`
	Minutes    int       `json:"minutes"`
	OccurredAt time.Time `json:"occurredAt"`
}

// UsageResponse — ответ /agent/usage.
type UsageResponse struct {
	Accepted   int         `json:"accepted"`
	Duplicates int         `json:"duplicates"`
	Balances   Balances    `json:"balances"`
	Screen     ScreenState `json:"screen"`
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	if !c.Configured() {
		return ErrOffline
	}

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("сериализация запроса: %w", err)
		}
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("подготовка запроса: %w", err)
	}
	req.Header.Set("authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}

	res, err := c.http.Do(req)
	if err != nil {
		// Сеть отвалилась — это ожидаемое состояние, а не сбой.
		return fmt.Errorf("%w: %v", ErrOffline, err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusUnauthorized {
		return ErrUnauthorized
	}
	if res.StatusCode >= 500 {
		// Пятисотка лечится повтором так же, как обрыв связи.
		return fmt.Errorf("%w: сервер ответил %d", ErrOffline, res.StatusCode)
	}
	if res.StatusCode >= 400 {
		payload, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("сервер отклонил запрос (%d): %s", res.StatusCode, strings.TrimSpace(string(payload)))
	}

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		return fmt.Errorf("разбор ответа: %w", err)
	}
	return nil
}

// Sync забирает политику и текущее состояние. Заодно сервер выдаёт дневной
// лимит, если тот ещё не выдан на эти сутки.
func (c *Client) Sync(ctx context.Context) (*SyncResponse, error) {
	var out SyncResponse
	if err := c.do(ctx, http.MethodGet, "/agent/sync", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PushUsage отправляет накопленный расход.
//
// Счётчики seq берутся из очереди и не переиспользуются: повторная отправка
// после обрыва связи не должна удвоить списание.
func (c *Client) PushUsage(ctx context.Context, entries []UsageEntry) (*UsageResponse, error) {
	if len(entries) == 0 {
		return &UsageResponse{}, nil
	}
	payload := struct {
		AgentVersion string       `json:"agentVersion"`
		Entries      []UsageEntry `json:"entries"`
	}{AgentVersion: c.version, Entries: entries}

	var out UsageResponse
	if err := c.do(ctx, http.MethodPost, "/agent/usage", payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TamperEvent — сообщение о вмешательстве.
//
// Тип вынесен наружу, потому что событие переживает перезапуск: без связи оно
// ждёт в файле состояния. Иначе ребёнку достаточно было бы выдернуть сеть,
// снять агента и вернуть сеть обратно — родитель не узнал бы ничего.
type TamperEvent struct {
	Kind   string    `json:"kind"`
	Detail string    `json:"detail,omitempty"`
	At     time.Time `json:"at"`
}

// ReportTamper сообщает о вмешательстве: остановке агента, подкрутке часов.
func (c *Client) ReportTamper(ctx context.Context, e TamperEvent) error {
	return c.do(ctx, http.MethodPost, "/agent/tamper", e, nil)
}

// IsOffline отличает временную недоступность от отказа в доступе.
func IsOffline(err error) bool { return errors.Is(err, ErrOffline) }
