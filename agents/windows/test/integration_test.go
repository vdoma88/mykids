//go:build integration

// Проверка агента против настоящего API.
//
// Юнит-тесты клиента ходят на фальшивый сервер, чьи ответы написаны здесь же.
// Это ловит ошибки разбора, но не ловит главного: что сервер отвечает не тем,
// чего агент ждёт. Поэтому тест поднимается на живом API с настоящей базой.
//
//	agents/windows/integration.sh
package test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vdoma88/mykids/agents/windows/internal/client"
	"github.com/vdoma88/mykids/agents/windows/internal/clock"
	"github.com/vdoma88/mykids/agents/windows/internal/config"
	"github.com/vdoma88/mykids/agents/windows/internal/link"
	"github.com/vdoma88/mykids/agents/windows/internal/outbox"
	"github.com/vdoma88/mykids/agents/windows/internal/policy"
)

const version = "test"

func env(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Fatalf("нужна переменная %s — запускайте через integration.sh", key)
	}
	return v
}

func newClient(t *testing.T) *client.Client {
	t.Helper()
	return client.New(env(t, "MYKIDS_SERVER"), env(t, "MYKIDS_DEVICE_TOKEN"), version)
}

func newLink(t *testing.T) (*link.Link, *clock.Clock, *outbox.Outbox, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "outbox.jsonl")

	// Каждый тест берёт свежую очередь, а счётчик в ней начинается с единицы —
	// и сервер справедливо считает такие записи повторами предыдущего теста.
	// На настоящем устройстве очередь одна и счётчик не откатывается, поэтому
	// задаём тесту свой диапазон, а не ослабляем проверку на сервере.
	seed := fmt.Sprintf(`{"seq":%d,"minutes":1,"occurredAt":%q}`+"\n",
		time.Now().UnixNano()%1_000_000_000, time.Now().Format(time.RFC3339))
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatalf("подготовка очереди: %v", err)
	}
	box, err := outbox.Open(path)
	if err != nil {
		t.Fatalf("outbox.Open: %v", err)
	}
	if err := box.Ack(box.Pending()); err != nil {
		t.Fatalf("подготовка очереди: %v", err)
	}
	clk := clock.New(0)
	cache := filepath.Join(dir, "policy-cache.json")
	return link.New(newClient(t), box, clk, cache), clk, box, cache
}

func ctx(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return c
}

// Настоящий источник времени: подделывать его здесь нечем и незачем — тест
// проверяет разговор с сервером, а не арифметику часов.
var source = clock.System()

func reading() clock.Reading { return source() }

// TestSyncShapeMatchesServer — то, ради чего тест и написан: поля, которых
// агент ждёт, действительно приходят и не нулевые.
func TestSyncShapeMatchesServer(t *testing.T) {
	res, err := newClient(t).Sync(ctx(t))
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if res.ServerTime.IsZero() {
		t.Fatal("serverTime не пришло — поправка часов работать не будет")
	}
	if time.Since(res.ServerTime).Abs() > time.Hour {
		t.Fatalf("serverTime разобрано неверно: %v", res.ServerTime)
	}
	if res.Policy.Timezone == "" {
		t.Fatal("политика пришла без пояса")
	}
	if len(res.Policy.DailyLimitMinutes) != 7 {
		t.Fatalf("дневных лимитов %d вместо 7: доменная схема разъехалась с агентом",
			len(res.Policy.DailyLimitMinutes))
	}
	if res.Policy.Economy.CreditsPerMinute <= 0 {
		t.Fatalf("экономика не разобрана: %+v", res.Policy.Economy)
	}
	if len(res.Agent.AlwaysAllowed) == 0 {
		// Без белого списка ребёнок не позвонит родителю при нулевом балансе.
		t.Fatal("белый список пуст — агент возьмёт его из правимого локального файла")
	}
	if res.Balances.Minutes <= 0 {
		t.Fatalf("сервер не выдал дневной лимит: %+v", res.Balances)
	}
}

// TestMergedPolicyIsUsable — слитая политика обязана проходить собственную
// проверку агента: иначе агент отвергнет то, что прислал сервер.
func TestMergedPolicyIsUsable(t *testing.T) {
	res, err := newClient(t).Sync(ctx(t))
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	merged := policy.Clamp(res.Merge(config.Default()))
	if err := merged.Validate(); err != nil {
		t.Fatalf("политика сервера не проходит проверку агента: %v", err)
	}
	if _, err := merged.Location(); err != nil {
		t.Fatalf("пояс сервера агент не понимает: %v", err)
	}
}

// TestUsageIsDeductedOnce — сервер обязан отбрасывать повтор очереди по паре
// устройство+счётчик, иначе обрыв связи удваивал бы списание.
func TestUsageIsDeductedOnce(t *testing.T) {
	c := newClient(t)
	before, err := c.Sync(ctx(t))
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	seq := int(time.Now().UnixNano() % 1_000_000)
	entries := []client.UsageEntry{
		{Seq: seq, Minutes: 3, OccurredAt: time.Now()},
		{Seq: seq + 1, Minutes: 2, OccurredAt: time.Now()},
	}

	first, err := c.PushUsage(ctx(t), entries)
	if err != nil {
		t.Fatalf("PushUsage: %v", err)
	}
	if first.Accepted != 2 || first.Duplicates != 0 {
		t.Fatalf("первая отправка: принято %d, повторов %d", first.Accepted, first.Duplicates)
	}
	if want := before.Balances.Minutes - 5; first.Balances.Minutes != want {
		t.Fatalf("списано неверно: остаток %d вместо %d", first.Balances.Minutes, want)
	}

	// Повтор ровно тех же записей: так выглядит отправка после обрыва связи.
	again, err := c.PushUsage(ctx(t), entries)
	if err != nil {
		t.Fatalf("повторная PushUsage: %v", err)
	}
	if again.Accepted != 0 || again.Duplicates != 2 {
		t.Fatalf("повтор: принято %d, повторов %d", again.Accepted, again.Duplicates)
	}
	if again.Balances.Minutes != first.Balances.Minutes {
		t.Fatalf("повтор списал ещё раз: %d -> %d", first.Balances.Minutes, again.Balances.Minutes)
	}
}

// TestLinkRoundTrip — весь путь: расход копится, уходит на сервер, очередь
// чистится, политика ложится в кэш.
func TestLinkRoundTrip(t *testing.T) {
	lnk, clk, box, cache := newLink(t)

	// Две минуты экрана и хвост, не набравший минуты.
	if err := lnk.Record(150, time.Now()); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if box.Total() != 2 || lnk.Pending() != 30 {
		t.Fatalf("очередь собрана неверно: %d мин, остаток %d с", box.Total(), lnk.Pending())
	}

	before, err := newClient(t).Sync(ctx(t))
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	res, err := lnk.Sync(ctx(t), reading(), config.Default())
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if !res.Online || res.Source != policy.FromServer {
		t.Fatalf("обмен не состоялся: online=%v источник=%q замечание=%q", res.Online, res.Source, res.Note)
	}
	// Две минуты ушли одной записью, поэтому Accepted здесь 1, а не 2:
	// сервер считает записи, а списывает минуты.
	if res.Accepted != 1 {
		t.Fatalf("сервер принял %d записей вместо 1", res.Accepted)
	}
	if want := before.Balances.Minutes - 2; res.Balances.Minutes != want {
		t.Fatalf("списано неверно: остаток %d вместо %d", res.Balances.Minutes, want)
	}
	// Курс обмена доходит до того, кто пишет текст ребёнку. Без него на
	// закрытом экране вместо «это ещё 2 часа» останутся голые кредиты,
	// которые сами по себе ни о чём не говорят.
	if res.CreditsPerMinute != before.Policy.Economy.CreditsPerMinute {
		t.Fatalf("курс обмена не дошёл: %d вместо %d",
			res.CreditsPerMinute, before.Policy.Economy.CreditsPerMinute)
	}
	if box.Len() != 0 {
		t.Fatalf("очередь не очищена: %d", box.Len())
	}
	if lnk.Pending() != 30 {
		t.Fatalf("хвост секунд потерян: %d", lnk.Pending())
	}
	if _, err := os.Stat(cache); err != nil {
		t.Fatalf("кэш политики не записан: %v", err)
	}

	// Поправка часов по времени сервера. Машина и сервер здесь одни и те же,
	// так что расхождение обязано быть в пределах сетевой задержки.
	if clk.Suspicious() {
		t.Fatalf("поправка к часам неправдоподобна: %v", clk.Offset())
	}
	if !clk.Trusted() {
		t.Fatal("после обмена поправка должна считаться достоверной")
	}
}

// TestOfflineFallsBackToCacheNotLocalFile — главная защита: отключив сеть,
// ребёнок не должен вернуть агента к правимому локальному файлу.
func TestOfflineFallsBackToCacheNotLocalFile(t *testing.T) {
	lnk, clk, box, cache := newLink(t)
	if _, err := lnk.Sync(ctx(t), reading(), config.Default()); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	tampered := config.Default()
	tampered.DailyLimitMinutes = []int{999, 999, 999, 999, 999, 999, 999}
	tampered.Windows = nil

	offline := link.New(client.New("http://127.0.0.1:1", "token", version), box, clk, cache)
	res, err := offline.Sync(ctx(t), reading(), tampered)
	if err != nil {
		t.Fatalf("офлайн не должен быть ошибкой: %v", err)
	}
	if res.Source != policy.FromCache {
		t.Fatalf("без сети политика взята не из кэша: %q", res.Source)
	}
	if res.Policy.LimitFor(1) == 999 {
		t.Fatal("без сети применилась правленая локальная политика")
	}
}

// TestTamperIsAcceptedRepeatedly — часы можно крутить сколько угодно раз, и
// каждое сообщение обязано дойти.
func TestTamperIsAcceptedRepeatedly(t *testing.T) {
	c := newClient(t)
	for i := 0; i < 3; i++ {
		err := c.ReportTamper(ctx(t), client.TamperEvent{
			Kind:   "clock",
			Detail: "системные часы переведены назад на 3h0m0s",
			At:     time.Now(),
		})
		if err != nil {
			t.Fatalf("сообщение %d отклонено: %v", i+1, err)
		}
	}
}

// tamperEvent — событие вмешательства глазами родителя.
type tamperEvent struct {
	Kind       string     `json:"kind"`
	Detail     string     `json:"detail"`
	OccurredAt *time.Time `json:"occurredAt"`
	ReviewedAt *time.Time `json:"reviewedAt"`
}

// asParent читает страницу ребёнка тем же REST, которым пользуется админка.
func asParent(t *testing.T, path string, out any) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx(t), http.MethodGet,
		env(t, "MYKIDS_SERVER")+path, nil)
	if err != nil {
		t.Fatalf("запрос: %v", err)
	}
	req.Header.Set("authorization", "Bearer "+env(t, "MYKIDS_PARENT_TOKEN"))

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("запрос родителя: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		t.Fatalf("родитель получил %d: %s", res.StatusCode, body)
	}
	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}
}

// TestQueuedTamperReachesParent — главная проверка: ребёнок выдёргивает сеть,
// снимает агента и возвращает сеть обратно. Родитель обязан всё равно узнать.
func TestQueuedTamperReachesParent(t *testing.T) {
	lnk, _, _, _ := newLink(t)
	if _, err := lnk.Sync(ctx(t), reading(), config.Default()); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Событие случилось час назад, пока сети не было.
	happened := time.Now().Add(-time.Hour).Truncate(time.Second)
	mark := "проверка доставки " + happened.Format(time.RFC3339Nano)
	lnk.QueueTamper("unclean_stop", mark, happened)

	if _, err := lnk.Sync(ctx(t), reading(), config.Default()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if n := len(lnk.Tampers()); n != 0 {
		t.Fatalf("сообщение осталось в очереди: %d", n)
	}

	var page struct {
		Pending int           `json:"pending"`
		Events  []tamperEvent `json:"events"`
	}
	asParent(t, "/admin/children/"+env(t, "MYKIDS_CHILD_ID")+"/tampers", &page)

	var found *tamperEvent
	for i := range page.Events {
		if page.Events[i].Detail == mark {
			found = &page.Events[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("родитель не увидел сообщение, всего событий %d", len(page.Events))
	}
	if found.Kind != "unclean_stop" {
		t.Fatalf("вид события искажён: %q", found.Kind)
	}
	// Время события, а не доставки: иначе после недели офлайна родитель увидел
	// бы только момент, когда агент дозвонился.
	if found.OccurredAt == nil || !found.OccurredAt.Equal(happened) {
		t.Fatalf("время события потеряно: %v вместо %v", found.OccurredAt, happened)
	}
	if found.ReviewedAt != nil {
		t.Fatal("новое событие не может быть уже разобранным")
	}
	if page.Pending == 0 {
		t.Fatal("счётчик неразобранных не вырос")
	}
}

// TestGapChargeReachesServer — пропуск после снятия агента должен списаться и
// на сервере, а не только в локальном файле, который ребёнок может удалить.
func TestGapChargeReachesServer(t *testing.T) {
	lnk, _, _, _ := newLink(t)
	before, err := newClient(t).Sync(ctx(t))
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Ровно то, что делает агент, обнаружив нештатную остановку.
	if err := lnk.Record(10*60, time.Now()); err != nil {
		t.Fatalf("Record: %v", err)
	}
	res, err := lnk.Sync(ctx(t), reading(), config.Default())
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if want := before.Balances.Minutes - 10; res.Balances.Minutes != want {
		t.Fatalf("остаток %d вместо %d: пропуск не списан на сервере",
			res.Balances.Minutes, want)
	}
}

// TestRevokedTokenIsUnauthorized — отозванный токен обязан выглядеть отказом в
// доступе, а не обрывом связи: повторять его бесполезно.
func TestRevokedTokenIsUnauthorized(t *testing.T) {
	c := client.New(env(t, "MYKIDS_SERVER"), "заведомо-неизвестный-токен", version)
	_, err := c.Sync(ctx(t))
	if err == nil {
		t.Fatal("чужой токен принят")
	}
	if client.IsOffline(err) {
		t.Fatalf("отказ в доступе принят за офлайн: %v", err)
	}
}
