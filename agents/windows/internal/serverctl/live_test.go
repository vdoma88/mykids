//go:build live

// Живая проверка супервизора: настоящий PostgreSQL, настоящий Node и
// собранный комплект сервера. Запуск — deploy/server-windows/live-test.sh.
package serverctl

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func liveLayout(t *testing.T) Layout {
	t.Helper()
	home := os.Getenv("MYKIDS_SERVER_HOME")
	if home == "" || os.Getenv("MYKIDS_PG_BIN") == "" {
		t.Skip("нужны MYKIDS_SERVER_HOME и MYKIDS_PG_BIN — запускайте через live-test.sh")
	}
	return Layout{Home: home, Data: t.TempDir()}
}

func post(t *testing.T, port int, path, body string) map[string]any {
	t.Helper()
	res, err := http.Post("http://127.0.0.1:"+strconv.Itoa(port)+path, "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	json.NewDecoder(res.Body).Decode(&out)
	return out
}

func runUntilHealthy(t *testing.T, s *Supervisor) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	deadline := time.Now().Add(2 * time.Minute)
	for !Healthy(context.Background(), s.C.Port) {
		select {
		case err := <-done:
			t.Fatalf("супервизор завершился: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("сервер не ответил за две минуты")
		}
		time.Sleep(time.Second)
	}
	return func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("остановка: %v", err)
		}
	}
}

func TestSupervisorFromScratchToWorkingServer(t *testing.T) {
	l := liveLayout(t)
	var logs []string
	cfg, err := LoadConfig(l.Config())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Port, cfg.DBPort = 3297, 54397
	SaveConfig(l.Config(), cfg)
	s := &Supervisor{L: l, C: cfg, Log: func(m string) { logs = append(logs, m) }}

	stop := runUntilHealthy(t, s)
	reg := post(t, cfg.Port, "/auth/register", `{"familyName":"Живая","email":"live@example.com","password":"очень-длинный-пароль"}`)
	if reg["token"] == nil {
		t.Fatalf("регистрация: %v", reg)
	}
	stop()

	db := DB{L: l, C: cfg}
	if db.Running(context.Background()) {
		t.Fatal("база осталась работать после остановки службы")
	}
	if len(Backups(l.Backups())) != 1 {
		t.Errorf("первая резервная копия не сделана: %v", logs)
	}

	// Второй запуск: ни заведения базы, ни миграций, данные на месте.
	logs = nil
	stop = runUntilHealthy(t, s)
	defer stop()
	for _, m := range logs {
		if strings.Contains(m, "Первый запуск") || strings.Contains(m, "Миграция") {
			t.Errorf("второй запуск повторил работу первого: %s", m)
		}
	}
	login := post(t, cfg.Port, "/auth/login", `{"email":"LIVE@example.com","password":"очень-длинный-пароль"}`)
	if login["token"] == nil {
		t.Fatalf("вход после перезапуска: %v", login)
	}

	// Учёт миграций — в формате Prisma: иначе база не переедет в Docker.
	out, err := db.psql(context.Background(), dbName, "-tAc",
		`SELECT count(*) FROM "_prisma_migrations" WHERE finished_at IS NOT NULL AND applied_steps_count = 1`)
	all, _ := ListMigrations(l.Migrations())
	if err != nil || strings.TrimSpace(out) != strconv.Itoa(len(all)) {
		t.Errorf("учёт миграций: %q %v, ожидалось %d", out, err, len(all))
	}
}
