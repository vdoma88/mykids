package link

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vdoma88/mykids/agents/windows/internal/client"
	"github.com/vdoma88/mykids/agents/windows/internal/clock"
	"github.com/vdoma88/mykids/agents/windows/internal/config"
	"github.com/vdoma88/mykids/agents/windows/internal/outbox"
	"github.com/vdoma88/mykids/agents/windows/internal/policy"
)

var serverTime = time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

const syncBody = `{
  "serverTime": "2026-09-18T12:00:00Z",
  "policy": {
    "timezone": "Europe/Moscow",
    "dailyLimitMinutes": [30,30,30,30,30,30,30],
    "carryOverMaxMinutes": 15,
    "windows": [{"name":"отбой","days":[0,1,2,3,4,5,6],"from":"21:30","to":"07:00","mode":"blocked"}],
    "economy": {"creditsPerMinute":2,"maxConvertedMinutesPerDay":60,"minCreditsToConvert":10,"maxCreditsPerDay":200}
  },
  "agent": {"alwaysAllowed": ["explorer.exe"]},
  "balances": {"minutes": 40, "credits": 100},
  "screen": {"allowed": true, "minutesLeft": 40}
}`

type harness struct {
	link  *Link
	box   *outbox.Outbox
	clk   *clock.Clock
	cache string
	dir   string
	// usage — тела запросов, пришедших на /agent/usage.
	usage []usageBody
	// tampers — сообщения о вмешательстве.
	tampers []string
	calls   map[string]int
}

type usageBody struct {
	AgentVersion string              `json:"agentVersion"`
	Entries      []client.UsageEntry `json:"entries"`
}

// newHarness поднимает фальшивый сервер. handler=nil означает обычный ответ.
func newHarness(t *testing.T, handler http.HandlerFunc) *harness {
	t.Helper()
	dir := t.TempDir()
	h := &harness{dir: dir, cache: filepath.Join(dir, "policy-cache.json"), calls: map[string]int{}}

	mux := http.NewServeMux()
	mux.HandleFunc("/agent/sync", func(w http.ResponseWriter, r *http.Request) {
		h.calls["sync"]++
		io.WriteString(w, syncBody)
	})
	mux.HandleFunc("/agent/usage", func(w http.ResponseWriter, r *http.Request) {
		h.calls["usage"]++
		var b usageBody
		json.NewDecoder(r.Body).Decode(&b)
		h.usage = append(h.usage, b)
		accepted := len(b.Entries)
		w.Header().Set("content-type", "application/json")
		io.WriteString(w, `{"accepted":`+itoa(accepted)+`,"duplicates":0,"balances":{"minutes":40,"credits":100},"screen":{"allowed":true}}`)
	})
	mux.HandleFunc("/agent/tamper", func(w http.ResponseWriter, r *http.Request) {
		h.calls["tamper"]++
		var b struct{ Kind, Detail string }
		json.NewDecoder(r.Body).Decode(&b)
		h.tampers = append(h.tampers, b.Kind+": "+b.Detail)
		w.WriteHeader(http.StatusNoContent)
	})

	var root http.Handler = mux
	if handler != nil {
		root = handler
	}
	srv := httptest.NewServer(root)
	t.Cleanup(srv.Close)

	h.attach(t, srv.URL)
	return h
}

func itoa(n int) string { return string(rune('0' + n)) }

// serve поднимает ещё один обычный сервер — нужен там, где тест сначала
// работает без связи, а потом её возвращает.
func (h *harness) serve(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/agent/tamper":
			h.calls["tamper"]++
			var b struct{ Kind, Detail string }
			json.NewDecoder(r.Body).Decode(&b)
			h.tampers = append(h.tampers, b.Kind+": "+b.Detail)
			w.WriteHeader(http.StatusNoContent)
		default:
			io.WriteString(w, syncBody)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func (h *harness) attach(t *testing.T, baseURL string) {
	t.Helper()
	box, err := outbox.Open(filepath.Join(h.dir, "outbox.jsonl"))
	if err != nil {
		t.Fatalf("outbox.Open: %v", err)
	}
	h.box, h.clk = box, clock.New(0)
	h.link = New(client.New(baseURL, "token", "0.2.0"), box, h.clk, h.cache)
}

func local() config.Policy {
	p := config.Default()
	p.DailyLimitMinutes = []int{999, 999, 999, 999, 999, 999, 999}
	p.Windows = nil
	return p
}

func reading(at time.Time, mono time.Duration) clock.Reading {
	return clock.Reading{Wall: at, Mono: mono}
}

func bg() context.Context { return context.Background() }

func TestSyncTakesPolicyFromServer(t *testing.T) {
	h := newHarness(t, nil)
	res, err := h.link.Sync(bg(), reading(serverTime, 0), local())
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if !res.Online || res.Source != policy.FromServer {
		t.Fatalf("ожидался ответ сервера: online=%v источник=%q", res.Online, res.Source)
	}
	if res.Policy.LimitFor(1) != 30 {
		t.Fatalf("применён правленый локальный лимит: %d", res.Policy.LimitFor(1))
	}
	if len(res.Policy.Windows) != 1 {
		t.Fatalf("окна с сервера потеряны: %+v", res.Policy.Windows)
	}
	if res.Balances.Minutes != 40 || !res.Screen.Allowed {
		t.Fatalf("состояние с сервера разобрано неверно: %+v %+v", res.Balances, res.Screen)
	}
	if res.Note != "" {
		t.Fatalf("неожиданное замечание: %q", res.Note)
	}
}

func TestOfflineUsesCacheNotLocalFile(t *testing.T) {
	h := newHarness(t, nil)
	if _, err := h.link.Sync(bg(), reading(serverTime, 0), local()); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Сеть пропала. Ребёнок к этому моменту уже переписал локальный файл.
	h.attach(t, "http://127.0.0.1:1")
	res, err := h.link.Sync(bg(), reading(serverTime.Add(time.Hour), time.Hour), local())
	if err != nil {
		t.Fatalf("офлайн не должен быть ошибкой: %v", err)
	}
	if res.Online {
		t.Fatal("связи нет, а Online=true")
	}
	if res.Source != policy.FromCache {
		t.Fatalf("политика взята не из кэша: %q", res.Source)
	}
	if res.Policy.LimitFor(1) != 30 {
		t.Fatalf("без сети применён правленый файл: %d", res.Policy.LimitFor(1))
	}
}

func TestOfflineWithoutCacheUsesLocalFile(t *testing.T) {
	// Первый запуск в дороге: сервера не видели ни разу, работать больше не по чему.
	h := newHarness(t, nil)
	h.attach(t, "http://127.0.0.1:1")
	res, err := h.link.Sync(bg(), reading(serverTime, 0), local())
	if err != nil {
		t.Fatalf("офлайн не должен быть ошибкой: %v", err)
	}
	if res.Source != policy.FromLocal || res.Policy.LimitFor(1) != 999 {
		t.Fatalf("ожидался локальный файл: %q, лимит %d", res.Source, res.Policy.LimitFor(1))
	}
}

func TestUnconfiguredClientWorksOffline(t *testing.T) {
	// Автономный режим: адрес сервера ещё не задан.
	h := newHarness(t, nil)
	h.attach(t, "")
	res, err := h.link.Sync(bg(), reading(serverTime, 0), local())
	if err != nil {
		t.Fatalf("автономный режим не ошибка: %v", err)
	}
	if res.Online || res.Source != policy.FromLocal {
		t.Fatalf("неверный результат автономного режима: %+v", res)
	}
	if h.calls["sync"] != 0 {
		t.Fatal("без адреса сервера запросов быть не должно")
	}
}

func TestCacheIsWrittenWithServerTime(t *testing.T) {
	// Часы машины отстают на сутки: по ним возраст кэша посчитался бы неверно.
	h := newHarness(t, nil)
	if _, err := h.link.Sync(bg(), reading(serverTime.Add(-24*time.Hour), 0), local()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	var cached struct {
		FetchedAt time.Time `json:"fetchedAt"`
	}
	raw, err := os.ReadFile(h.cache)
	if err != nil {
		t.Fatalf("кэш не записан: %v", err)
	}
	if err := json.Unmarshal(raw, &cached); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !cached.FetchedAt.Equal(serverTime) {
		t.Fatalf("кэш помечен местным временем: %v", cached.FetchedAt)
	}
}

func TestRevokedTokenIsReportedButKeepsPolicy(t *testing.T) {
	h := newHarness(t, nil)
	if _, err := h.link.Sync(bg(), reading(serverTime, 0), local()); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	h.attach(t, "")
	h.link = New(client.New(unauthorizedServer(t), "token", "v"), h.box, h.clk, h.cache)
	res, err := h.link.Sync(bg(), reading(serverTime, 0), local())
	if err == nil {
		t.Fatal("отозванный токен обязан быть виден как ошибка")
	}
	// Но считать время агент продолжает — по кэшу, не по правленому файлу.
	if res.Source != policy.FromCache || res.Policy.LimitFor(1) != 30 {
		t.Fatalf("после отказа в доступе политика потеряна: %q, лимит %d", res.Source, res.Policy.LimitFor(1))
	}
}

func unauthorizedServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestRecordQueuesWholeMinutesOnly(t *testing.T) {
	h := newHarness(t, nil)
	if err := h.link.Record(30, serverTime); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if h.box.Len() != 0 {
		t.Fatalf("полминуты не должны попадать в очередь: %d", h.box.Len())
	}
	if h.link.Pending() != 30 {
		t.Fatalf("остаток не накоплен: %d", h.link.Pending())
	}

	if err := h.link.Record(45, serverTime.Add(45*time.Second)); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if h.box.Total() != 1 {
		t.Fatalf("минута не попала в очередь: %d", h.box.Total())
	}
	if h.link.Pending() != 15 {
		t.Fatalf("остаток после минуты неверен: %d", h.link.Pending())
	}
}

func TestRecordQueuesSeveralMinutesAtOnce(t *testing.T) {
	// Агент мог простоять на паузе: за один замер набежало несколько минут.
	h := newHarness(t, nil)
	if err := h.link.Record(185, serverTime); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if h.box.Total() != 3 || h.box.Len() != 1 {
		t.Fatalf("ожидалась одна запись на 3 минуты: len=%d total=%d", h.box.Len(), h.box.Total())
	}
	if h.link.Pending() != 5 {
		t.Fatalf("остаток неверен: %d", h.link.Pending())
	}
}

func TestRecordIgnoresNonPositive(t *testing.T) {
	h := newHarness(t, nil)
	for _, s := range []int{0, -10} {
		if err := h.link.Record(s, serverTime); err != nil {
			t.Fatalf("Record(%d): %v", s, err)
		}
	}
	if h.box.Len() != 0 || h.link.Pending() != 0 {
		t.Fatalf("нулевой расход что-то изменил: len=%d остаток=%d", h.box.Len(), h.link.Pending())
	}
}

func TestPendingSurvivesRestart(t *testing.T) {
	// Иначе перезапуск раз в полминуты обнулял бы расход.
	h := newHarness(t, nil)
	h.link.Record(50, serverTime)
	saved := h.link.Pending()

	h.attach(t, "http://127.0.0.1:1")
	h.link.SetPending(saved)
	h.link.Record(20, serverTime.Add(time.Minute))
	if h.box.Total() != 1 {
		t.Fatalf("накопленные до перезапуска секунды потеряны: %d", h.box.Total())
	}
}

func TestSetPendingRejectsNegative(t *testing.T) {
	h := newHarness(t, nil)
	h.link.SetPending(-100)
	if h.link.Pending() != 0 {
		t.Fatalf("битое состояние не должно давать отрицательный остаток: %d", h.link.Pending())
	}
}

func TestSyncFlushesQueueAndClearsIt(t *testing.T) {
	h := newHarness(t, nil)
	h.link.Record(120, serverTime)
	h.link.Record(60, serverTime.Add(2*time.Minute))
	if h.box.Len() != 2 {
		t.Fatalf("подготовка: в очереди %d", h.box.Len())
	}

	res, err := h.link.Sync(bg(), reading(serverTime, 0), local())
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if res.Accepted != 2 {
		t.Fatalf("сервер принял %d записей", res.Accepted)
	}
	if h.box.Len() != 0 {
		t.Fatalf("принятые записи остались в очереди: %d", h.box.Len())
	}
	if len(h.usage) != 1 || len(h.usage[0].Entries) != 2 {
		t.Fatalf("на сервер ушло не то: %+v", h.usage)
	}
	if h.usage[0].AgentVersion != "0.2.0" {
		t.Fatalf("версия агента не передана: %q", h.usage[0].AgentVersion)
	}

	// Следующий обмен не должен слать то же самое ещё раз.
	if _, err := h.link.Sync(bg(), reading(serverTime.Add(time.Minute), time.Minute), local()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if h.calls["usage"] != 1 {
		t.Fatalf("пустая очередь снова ушла на сервер: вызовов %d", h.calls["usage"])
	}
}

func TestSyncWithEmptyQueueSkipsUsage(t *testing.T) {
	h := newHarness(t, nil)
	if _, err := h.link.Sync(bg(), reading(serverTime, 0), local()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if h.calls["usage"] != 0 {
		t.Fatalf("без расхода запрос не нужен: вызовов %d", h.calls["usage"])
	}
}

func TestOfflineKeepsQueue(t *testing.T) {
	h := newHarness(t, nil)
	h.attach(t, "http://127.0.0.1:1")
	h.link.Record(180, serverTime)

	if _, err := h.link.Sync(bg(), reading(serverTime, 0), local()); err != nil {
		t.Fatalf("офлайн не ошибка: %v", err)
	}
	if h.box.Total() != 3 {
		t.Fatalf("без сети расход обязан ждать в очереди: %d", h.box.Total())
	}
}

func TestQueueSurvivesRestartAndIsSentLater(t *testing.T) {
	h := newHarness(t, nil)
	h.attach(t, "http://127.0.0.1:1")
	h.link.Record(120, serverTime)

	// Перезапуск агента, связь восстановилась.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/agent/usage" {
			var b usageBody
			json.NewDecoder(r.Body).Decode(&b)
			h.usage = append(h.usage, b)
			io.WriteString(w, `{"accepted":1,"duplicates":0}`)
			return
		}
		io.WriteString(w, syncBody)
	}))
	t.Cleanup(srv.Close)
	h.attach(t, srv.URL)

	if h.box.Total() != 2 {
		t.Fatalf("очередь не пережила перезапуск: %d", h.box.Total())
	}
	if _, err := h.link.Sync(bg(), reading(serverTime, 0), local()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(h.usage) != 1 || h.usage[0].Entries[0].Minutes != 2 {
		t.Fatalf("накопленный офлайн расход не ушёл: %+v", h.usage)
	}
	if h.box.Len() != 0 {
		t.Fatalf("очередь не очищена: %d", h.box.Len())
	}
}

func TestDuplicatesAreAckedNotResent(t *testing.T) {
	// Сервер уже помнит эти пары устройство+счётчик. Держать их в очереди
	// значило бы слать их вечно.
	var pushes int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/agent/usage" {
			pushes++
			io.WriteString(w, `{"accepted":0,"duplicates":1}`)
			return
		}
		io.WriteString(w, syncBody)
	}))
	t.Cleanup(srv.Close)

	h := newHarness(t, nil)
	h.attach(t, srv.URL)
	h.link.Record(60, serverTime)

	res, err := h.link.Sync(bg(), reading(serverTime, 0), local())
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if res.Duplicates != 1 {
		t.Fatalf("повторы не видны в результате: %+v", res)
	}
	if h.box.Len() != 0 {
		t.Fatalf("повторы остались в очереди: %d", h.box.Len())
	}
	if _, err := h.link.Sync(bg(), reading(serverTime.Add(time.Minute), time.Minute), local()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if pushes != 1 {
		t.Fatalf("повторы отправлены снова: %d", pushes)
	}
}

func TestClockSyncCorrectsTime(t *testing.T) {
	h := newHarness(t, nil)
	// Часы машины отстают на два часа.
	local := serverTime.Add(-2 * time.Hour)
	res, err := h.link.Sync(bg(), reading(local, 0), local2())
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if res.Corrected != 2*time.Hour {
		t.Fatalf("поправка не применена: %v", res.Corrected)
	}
	if got := h.clk.Now(reading(local, 0)); !got.Equal(serverTime) {
		t.Fatalf("исправленное время не совпало с серверным: %v", got)
	}
}

func local2() config.Policy { return local() }

func TestObserveReportsTamper(t *testing.T) {
	h := newHarness(t, nil)
	if _, err := h.link.Sync(bg(), reading(serverTime, 0), local()); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	h.link.Observe(reading(serverTime.Add(time.Minute), time.Minute))
	// Часы переведены на три часа назад: настенное ушло, монотонное нет.
	jump, tampered := h.link.Observe(reading(serverTime.Add(-2*time.Hour), 2*time.Minute))
	if !tampered {
		t.Fatal("перевод часов не замечен")
	}
	if jump.Delta > -2*time.Hour {
		t.Fatalf("сдвиг измерен неверно: %v", jump.Delta)
	}
	// Отправка отложена до ближайшего обмена: связи может не быть именно
	// потому, что её отключили перед переводом часов.
	if h.calls["tamper"] != 0 {
		t.Fatalf("сообщение ушло сразу, минуя очередь: вызовов %d", h.calls["tamper"])
	}
	if len(h.link.Tampers()) != 1 {
		t.Fatalf("сообщение не попало в очередь: %+v", h.link.Tampers())
	}

	if _, err := h.link.Sync(bg(), reading(serverTime.Add(2*time.Minute), 2*time.Minute), local()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if h.calls["tamper"] != 1 {
		t.Fatalf("сервер не уведомлён: вызовов %d", h.calls["tamper"])
	}
	if len(h.tampers) != 1 || h.tampers[0][:5] != "clock" {
		t.Fatalf("сообщение о вмешательстве неверно: %+v", h.tampers)
	}
	if len(h.link.Tampers()) != 0 {
		t.Fatalf("отправленное сообщение осталось в очереди: %+v", h.link.Tampers())
	}
}

func TestTamperSurvivesOfflineAndRestart(t *testing.T) {
	// Ребёнок выдёргивает сеть, переводит часы и возвращает сеть обратно.
	// Родитель обязан всё равно узнать.
	h := newHarness(t, nil)
	offlineURL := "http://127.0.0.1:1"
	h.attach(t, offlineURL)
	h.link.Observe(reading(serverTime, 0))
	h.link.Observe(reading(serverTime.Add(-3*time.Hour), time.Minute))

	if _, err := h.link.Sync(bg(), reading(serverTime, time.Minute), local()); err != nil {
		t.Fatalf("офлайн не ошибка: %v", err)
	}
	saved := h.link.Tampers()
	if len(saved) != 1 {
		t.Fatalf("без сети сообщение потеряно: %+v", saved)
	}

	// Перезапуск агента: очередь восстанавливается из файла состояния.
	srv := h.serve(t)
	h.attach(t, srv)
	h.link.SetTampers(saved)

	if _, err := h.link.Sync(bg(), reading(serverTime, 0), local()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if h.calls["tamper"] != 1 {
		t.Fatalf("сообщение не дошло после восстановления связи: %d", h.calls["tamper"])
	}
	if len(h.link.Tampers()) != 0 {
		t.Fatalf("очередь не очищена: %+v", h.link.Tampers())
	}
}

func TestTamperQueueIsBounded(t *testing.T) {
	// Очередь не должна расти бесконечно и раздувать файл состояния.
	h := newHarness(t, nil)
	h.attach(t, "http://127.0.0.1:1")
	for i := 0; i < MaxTampers+20; i++ {
		h.link.QueueTamper("clock", "сдвиг", serverTime.Add(time.Duration(i)*time.Minute))
	}
	got := h.link.Tampers()
	if len(got) != MaxTampers {
		t.Fatalf("в очереди %d сообщений вместо %d", len(got), MaxTampers)
	}
	// Отбрасываются старые: свежие события важнее сотого подряд.
	if !got[len(got)-1].At.Equal(serverTime.Add(time.Duration(MaxTampers+19) * time.Minute)) {
		t.Fatalf("самое свежее сообщение потеряно: %v", got[len(got)-1].At)
	}
}

func TestTampersAreCopies(t *testing.T) {
	h := newHarness(t, nil)
	h.link.QueueTamper("clock", "сдвиг", serverTime)
	got := h.link.Tampers()
	got[0].Kind = "подменено"
	if h.link.Tampers()[0].Kind != "clock" {
		t.Fatal("Tampers отдал ссылку на внутренний срез")
	}
}

func TestRejectedTamperDoesNotBlockQueue(t *testing.T) {
	// Сервер отверг сообщение (скажем, слишком длинное). Держать его вечно
	// нельзя: оно заблокировало бы все следующие.
	var seen int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/agent/tamper" {
			seen++
			if seen == 1 {
				w.WriteHeader(http.StatusBadRequest)
				io.WriteString(w, `{"message":"слишком длинно"}`)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		io.WriteString(w, syncBody)
	}))
	t.Cleanup(srv.Close)

	h := newHarness(t, nil)
	h.attach(t, srv.URL)
	h.link.QueueTamper("clock", "первое", serverTime)
	h.link.QueueTamper("clock", "второе", serverTime.Add(time.Minute))

	if _, err := h.link.Sync(bg(), reading(serverTime, 0), local()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(h.link.Tampers()) != 1 {
		t.Fatalf("отвергнутое сообщение не выброшено: %+v", h.link.Tampers())
	}

	if _, err := h.link.Sync(bg(), reading(serverTime.Add(time.Minute), time.Minute), local()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(h.link.Tampers()) != 0 {
		t.Fatalf("второе сообщение не ушло: %+v", h.link.Tampers())
	}
}

func TestObserveQuietWhenClockIsFine(t *testing.T) {
	h := newHarness(t, nil)
	h.link.Observe(reading(serverTime, 0))
	if _, tampered := h.link.Observe(reading(serverTime.Add(5*time.Second), 5*time.Second)); tampered {
		t.Fatal("ровный ход принят за подкрутку")
	}
	if len(h.link.Tampers()) != 0 {
		t.Fatalf("лишнее сообщение в очереди: %+v", h.link.Tampers())
	}
}

func TestObserveSurvivesUnreachableServer(t *testing.T) {
	// Компенсация важнее уведомления: недоступный сервер не должен мешать.
	h := newHarness(t, nil)
	h.attach(t, "http://127.0.0.1:1")
	h.link.Observe(reading(serverTime, 0))
	if _, tampered := h.link.Observe(reading(serverTime.Add(-3*time.Hour), time.Minute)); !tampered {
		t.Fatal("без сети подкрутка всё равно должна ловиться")
	}
	if h.clk.Offset() != 3*time.Hour+time.Minute {
		t.Fatalf("сдвиг не скомпенсирован: %v", h.clk.Offset())
	}
}

func TestStaleCacheIsFlagged(t *testing.T) {
	h := newHarness(t, nil)
	if err := policy.Save(h.cache, config.Default(), time.Now().Add(-10*24*time.Hour)); err != nil {
		t.Fatalf("подготовка: %v", err)
	}
	h.attach(t, "http://127.0.0.1:1")
	res, err := h.link.Sync(bg(), reading(serverTime, 0), local())
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if !res.Stale {
		t.Fatal("десятидневный кэш должен быть отмечен как устаревший")
	}
	// Но применяться он обязан: отключённая сеть не снимает ограничения.
	if res.Source != policy.FromCache {
		t.Fatalf("устаревший кэш не применён: %q", res.Source)
	}
}

func TestQueuedReportsMinutes(t *testing.T) {
	h := newHarness(t, nil)
	h.link.Record(300, serverTime)
	if h.link.Queued() != 5 {
		t.Fatalf("Queued вернул %d", h.link.Queued())
	}
}

func TestTamperSurvivesConnectionDropMidSync(t *testing.T) {
	// Обмен прошёл, а на отправке сообщения связь оборвалась. Выбросить его
	// нельзя: родитель узнал бы о снятии агента только если повезёт с сетью.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/agent/tamper" {
			// Рвём соединение, как это делает пропавшая сеть.
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				conn.Close()
			}
			return
		}
		io.WriteString(w, syncBody)
	}))
	t.Cleanup(srv.Close)

	h := newHarness(t, nil)
	h.attach(t, srv.URL)
	h.link.QueueTamper("unclean_stop", "агент не работал 40m0s", serverTime)

	if _, err := h.link.Sync(bg(), reading(serverTime, 0), local()); err != nil {
		t.Fatalf("обрыв на отправке сообщения не должен валить обмен: %v", err)
	}
	if len(h.link.Tampers()) != 1 {
		t.Fatalf("сообщение выброшено при обрыве связи: %+v", h.link.Tampers())
	}
}
