package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vdoma88/mykids/agents/windows/internal/config"
	"github.com/vdoma88/mykids/agents/windows/internal/schedule"
)

func ctx(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return c
}

// serve поднимает фальшивый сервер и возвращает клиента к нему.
func serve(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(srv.URL, "device-token", "0.2.0")
}

const syncBody = `{
  "serverTime": "2026-09-18T12:00:00Z",
  "policy": {
    "timezone": "Europe/Moscow",
    "dailyLimitMinutes": [120, 60, 60, 60, 60, 90, 120],
    "carryOverMaxMinutes": 30,
    "windows": [{"name": "отбой", "days": [0,1,2,3,4,5,6], "from": "21:30", "to": "07:00", "mode": "blocked"}],
    "economy": {"creditsPerMinute": 2, "maxConvertedMinutesPerDay": 60, "minCreditsToConvert": 10, "maxCreditsPerDay": 200}
  },
  "agent": {"alwaysAllowed": ["explorer.exe", "phone.exe"]},
  "balances": {"minutes": 42, "credits": 130},
  "screen": {"allowed": true, "reason": "ok", "minutesLeft": 42}
}`

func TestConfigured(t *testing.T) {
	cases := []struct {
		name, base, token string
		want              bool
	}{
		{"всё есть", "https://api.example", "t", true},
		{"нет адреса", "", "t", false},
		{"нет токена", "https://api.example", "", false},
	}
	for _, c := range cases {
		if got := New(c.base, c.token, "v").Configured(); got != c.want {
			t.Fatalf("%s: Configured=%v", c.name, got)
		}
	}
}

func TestUnconfiguredIsOfflineNotError(t *testing.T) {
	// Агент без сервера обязан продолжать считать время, а не падать.
	_, err := New("", "", "v").Sync(ctx(t))
	if !IsOffline(err) {
		t.Fatalf("автономный режим должен выглядеть как офлайн, получено %v", err)
	}
}

func TestSyncParsesResponse(t *testing.T) {
	var gotAuth, gotPath string
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("authorization"), r.URL.Path
		io.WriteString(w, syncBody)
	})

	res, err := c.Sync(ctx(t))
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if gotPath != "/agent/sync" {
		t.Fatalf("запрошен %q", gotPath)
	}
	if gotAuth != "Bearer device-token" {
		t.Fatalf("токен устройства не передан: %q", gotAuth)
	}
	if !res.ServerTime.Equal(time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("время сервера разобрано неверно: %v", res.ServerTime)
	}
	if res.Balances.Minutes != 42 || res.Balances.Credits != 130 {
		t.Fatalf("остатки разобраны неверно: %+v", res.Balances)
	}
	if !res.Screen.Allowed || res.Screen.MinutesLeft != 42 {
		t.Fatalf("состояние экрана разобрано неверно: %+v", res.Screen)
	}
	if len(res.Policy.Windows) != 1 || res.Policy.Windows[0].Mode != schedule.ModeBlocked {
		t.Fatalf("окна разобраны неверно: %+v", res.Policy.Windows)
	}
	if res.Policy.Economy.CreditsPerMinute != 2 {
		t.Fatalf("экономика разобрана неверно: %+v", res.Policy.Economy)
	}
	if len(res.Agent.AlwaysAllowed) != 2 {
		t.Fatalf("белый список разобран неверно: %+v", res.Agent.AlwaysAllowed)
	}
}

func TestMergeKeepsAgentOnlySettings(t *testing.T) {
	var res SyncResponse
	if err := json.Unmarshal([]byte(syncBody), &res); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	local := config.Default()
	local.IdleThresholdSeconds = 90
	local.WarnBeforeMinutes = 3
	local.Timezone = "Asia/Tokyo"
	local.CarryOverMaxMinutes = 999

	merged := res.Merge(local)

	// Технические настройки сервер не знает — они обязаны пережить слияние.
	if merged.IdleThresholdSeconds != 90 || merged.WarnBeforeMinutes != 3 {
		t.Fatalf("агентские настройки потеряны: %+v", merged)
	}
	// Родительские — обязаны замениться серверными.
	if merged.Timezone != "Europe/Moscow" {
		t.Fatalf("пояс не взят с сервера: %q", merged.Timezone)
	}
	if merged.CarryOverMaxMinutes != 30 {
		t.Fatalf("перенос не взят с сервера: %d", merged.CarryOverMaxMinutes)
	}
	if len(merged.Windows) != 1 || merged.Windows[0].Name != "отбой" {
		t.Fatalf("окна не взяты с сервера: %+v", merged.Windows)
	}
	if len(merged.AlwaysAllowed) != 2 || merged.AlwaysAllowed[1] != "phone.exe" {
		t.Fatalf("белый список не взят с сервера: %+v", merged.AlwaysAllowed)
	}
	if err := merged.Validate(); err != nil {
		t.Fatalf("слитая политика не проходит проверку: %v", err)
	}
}

func TestMergeKeepsLocalAllowlistWhenServerSendsNone(t *testing.T) {
	// Пустой список у старого сервера не должен оставить ребёнка без
	// возможности запустить проводник и позвонить родителю.
	res := SyncResponse{}
	local := config.Default()
	merged := res.Merge(local)
	if len(merged.AlwaysAllowed) != len(local.AlwaysAllowed) {
		t.Fatalf("белый список обнулён пустым ответом: %+v", merged.AlwaysAllowed)
	}
}

func TestUnauthorizedIsNotOffline(t *testing.T) {
	// Отозванный токен повторять бесполезно — это не обрыв связи.
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	_, err := c.Sync(ctx(t))
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("ожидался ErrUnauthorized, получено %v", err)
	}
	if IsOffline(err) {
		t.Fatal("отказ в доступе не должен выглядеть как офлайн")
	}
}

func TestServerErrorIsOffline(t *testing.T) {
	// Пятисотка лечится повтором так же, как обрыв связи.
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	if _, err := c.Sync(ctx(t)); !IsOffline(err) {
		t.Fatalf("502 должна считаться офлайном, получено %v", err)
	}
}

func TestClientErrorIsReportedNotSwallowed(t *testing.T) {
	// 400 — наша ошибка, повтор её не вылечит: она обязана быть видна.
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"message":"seq уже принят"}`)
	})
	_, err := c.PushUsage(ctx(t), []UsageEntry{{Seq: 1, Minutes: 5}})
	if err == nil {
		t.Fatal("400 должна быть ошибкой")
	}
	if IsOffline(err) || errors.Is(err, ErrUnauthorized) {
		t.Fatalf("400 не офлайн и не отказ в доступе: %v", err)
	}
	if !strings.Contains(err.Error(), "seq уже принят") {
		t.Fatalf("ответ сервера потерян: %v", err)
	}
}

func TestUnreachableServerIsOffline(t *testing.T) {
	// Порт, на котором заведомо никто не слушает.
	c := New("http://127.0.0.1:1", "token", "v")
	if _, err := c.Sync(ctx(t)); !IsOffline(err) {
		t.Fatalf("недоступный сервер должен давать офлайн, получено %v", err)
	}
}

func TestPushUsageSendsEntriesAndVersion(t *testing.T) {
	var body struct {
		AgentVersion string       `json:"agentVersion"`
		Entries      []UsageEntry `json:"entries"`
	}
	var method, path, ctype string
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		method, path, ctype = r.Method, r.URL.Path, r.Header.Get("content-type")
		json.NewDecoder(r.Body).Decode(&body)
		io.WriteString(w, `{"accepted":2,"duplicates":1,"balances":{"minutes":7,"credits":0},"screen":{"allowed":true}}`)
	})

	entries := []UsageEntry{
		{Seq: 4, Minutes: 5, OccurredAt: time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)},
		{Seq: 5, Minutes: 3, OccurredAt: time.Date(2026, 9, 18, 9, 5, 0, 0, time.UTC)},
	}
	res, err := c.PushUsage(ctx(t), entries)
	if err != nil {
		t.Fatalf("PushUsage: %v", err)
	}
	if method != http.MethodPost || path != "/agent/usage" {
		t.Fatalf("запрос ушёл не туда: %s %s", method, path)
	}
	if ctype != "application/json" {
		t.Fatalf("тип тела не указан: %q", ctype)
	}
	if body.AgentVersion != "0.2.0" {
		t.Fatalf("версия агента не передана: %q", body.AgentVersion)
	}
	if len(body.Entries) != 2 || body.Entries[0].Seq != 4 || body.Entries[1].Minutes != 3 {
		t.Fatalf("записи переданы неверно: %+v", body.Entries)
	}
	if !body.Entries[0].OccurredAt.Equal(entries[0].OccurredAt) {
		t.Fatalf("время расхода потеряно: %v", body.Entries[0].OccurredAt)
	}
	if res.Accepted != 2 || res.Duplicates != 1 || res.Balances.Minutes != 7 {
		t.Fatalf("ответ разобран неверно: %+v", res)
	}
}

func TestPushUsageEmptySkipsRequest(t *testing.T) {
	calls := 0
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		io.WriteString(w, `{}`)
	})
	res, err := c.PushUsage(ctx(t), nil)
	if err != nil {
		t.Fatalf("PushUsage: %v", err)
	}
	if calls != 0 {
		t.Fatalf("пустая очередь не должна ходить на сервер, вызовов %d", calls)
	}
	if res == nil {
		t.Fatal("ответ не должен быть nil — вызывающий читает его поля")
	}
}

func TestReportTamper(t *testing.T) {
	var body struct{ Kind, Detail string }
	var path string
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusNoContent)
	})

	if err := c.ReportTamper(ctx(t), "clock", "часы сдвинуты на -3ч"); err != nil {
		t.Fatalf("ReportTamper: %v", err)
	}
	if path != "/agent/tamper" {
		t.Fatalf("запрос ушёл не туда: %s", path)
	}
	if body.Kind != "clock" || !strings.Contains(body.Detail, "-3ч") {
		t.Fatalf("сообщение о вмешательстве передано неверно: %+v", body)
	}
}

func TestBaseURLTrailingSlash(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		io.WriteString(w, syncBody)
	}))
	t.Cleanup(srv.Close)

	// Родитель скопирует адрес со слешом на конце — двойной слеш дал бы 404.
	if _, err := New(srv.URL+"/", "t", "v").Sync(ctx(t)); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if path != "/agent/sync" {
		t.Fatalf("слеш на конце адреса сломал путь: %q", path)
	}
}

func TestGarbageResponseIsError(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "<html>прокси перехватил запрос</html>")
	})
	if _, err := c.Sync(ctx(t)); err == nil {
		t.Fatal("нераспознанный ответ должен быть ошибкой, а не пустой политикой")
	}
}
