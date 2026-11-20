package serverctl

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DB — PostgreSQL из комплекта.
//
// Запускается через pg_ctl, а не напрямую postgres.exe, и это не дело вкуса:
// PostgreSQL отказывается работать с правами администратора, а служба
// работает под LocalSystem. pg_ctl и initdb в Windows сами сбрасывают лишние
// права перед запуском сервера; postgres.exe напрямую просто не стартовал бы.
type DB struct {
	L   Layout
	C   Config
	Log func(string)
}

func (d DB) log(format string, a ...any) {
	if d.Log != nil {
		d.Log(fmt.Sprintf(format, a...))
	}
}

// run выполняет программу PostgreSQL и возвращает её вывод.
func (d DB) run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, d.L.PgBin(name), args...)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+d.C.DBPassword, "PGCLIENTENCODING=UTF8")
	hide(cmd)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	text := strings.TrimSpace(out.String())
	if err != nil {
		if text != "" {
			return text, fmt.Errorf("%s: %s", name, lastLines(text, 6))
		}
		return text, fmt.Errorf("%s: %w", name, err)
	}
	return text, nil
}

// Initialized — заведена ли база.
func (d DB) Initialized() bool {
	_, err := os.Stat(filepath.Join(d.L.PgData(), "PG_VERSION"))
	return err == nil
}

// Init заводит кластер базы, если его ещё нет.
func (d DB) Init(ctx context.Context) error {
	if d.Initialized() {
		return nil
	}
	d.log("Первый запуск: завожу базу в %s", d.L.PgData())
	if err := os.MkdirAll(d.L.Data, 0o755); err != nil {
		return err
	}
	pw, err := os.CreateTemp(d.L.Data, "pw-*")
	if err != nil {
		return err
	}
	pwPath := pw.Name()
	defer os.Remove(pwPath)
	_, _ = pw.WriteString(d.C.DBPassword + "\n")
	pw.Close()

	// --no-locale: имена локалей в Windows и Linux разные, а сортировка
	// по-русски серверу не нужна — порядок задаёт сам API.
	_, err = d.run(ctx, "initdb", "-D", d.L.PgData(), "-U", dbUser, "--pwfile", pwPath,
		"-A", "scram-sha-256", "-E", "UTF8", "--no-locale")
	if err != nil {
		// Наполовину заведённый каталог помешал бы следующей попытке.
		os.RemoveAll(d.L.PgData())
		return err
	}
	return nil
}

// Running — отвечает ли база.
func (d DB) Running(ctx context.Context) bool {
	_, err := d.run(ctx, "pg_ctl", "status", "-D", d.L.PgData())
	return err == nil
}

// Start запускает базу и ждёт, пока она начнёт принимать подключения.
func (d DB) Start(ctx context.Context) error {
	if d.Running(ctx) {
		return nil
	}
	if err := os.MkdirAll(d.L.Logs(), 0o755); err != nil {
		return err
	}
	_, err := d.run(ctx, "pg_ctl", "start", "-D", d.L.PgData(), "-w", "-t", "120",
		"-l", filepath.Join(d.L.Logs(), "postgres.log"),
		"-o", "-p "+strconv.Itoa(d.C.DBPort)+" -c listen_addresses=127.0.0.1")
	return err
}

// Stop останавливает базу: незавершённые запросы прерываются, данные
// сохраняются.
func (d DB) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if !d.Running(ctx) {
		return nil
	}
	_, err := d.run(ctx, "pg_ctl", "stop", "-D", d.L.PgData(), "-m", "fast", "-w", "-t", "60")
	return err
}

func (d DB) psql(ctx context.Context, database string, args ...string) (string, error) {
	base := []string{"-X", "-q", "-v", "ON_ERROR_STOP=1", "-h", "127.0.0.1",
		"-p", strconv.Itoa(d.C.DBPort), "-U", dbUser, "-d", database}
	return d.run(ctx, "psql", append(base, args...)...)
}

// EnsureDatabase заводит базу mykids, если её нет.
func (d DB) EnsureDatabase(ctx context.Context) error {
	out, err := d.psql(ctx, "postgres", "-tAc", "SELECT 1 FROM pg_database WHERE datname = '"+dbName+"'")
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) == "1" {
		return nil
	}
	_, err = d.psql(ctx, "postgres", "-c", "CREATE DATABASE "+dbName)
	return err
}

// Migrate применяет недостающие миграции и возвращает их имена.
func (d DB) Migrate(ctx context.Context) ([]string, error) {
	all, err := ListMigrations(d.L.Migrations())
	if err != nil {
		return nil, err
	}
	if _, err := d.psql(ctx, dbName, "-c", createMigrationsTable); err != nil {
		return nil, err
	}
	out, err := d.psql(ctx, dbName, "-tAc",
		`SELECT migration_name FROM "_prisma_migrations" WHERE finished_at IS NOT NULL AND rolled_back_at IS NULL`)
	if err != nil {
		return nil, err
	}
	applied := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			applied[line] = true
		}
	}

	var done []string
	for _, m := range Pending(all, applied) {
		d.log("Миграция базы: %s", m.Name)
		// Как и Prisma, файл миграции выполняется как есть, без общей
		// транзакции: некоторые изменения схемы в транзакции невозможны.
		if _, err := d.psql(ctx, dbName, "-f", m.Path); err != nil {
			return done, fmt.Errorf("миграция %s не применилась: %w", m.Name, err)
		}
		if _, err := d.psql(ctx, dbName, "-c", recordMigration(m)); err != nil {
			return done, err
		}
		done = append(done, m.Name)
	}
	return done, nil
}

// Backup сохраняет базу целиком в файл.
func (d DB) Backup(ctx context.Context, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	_, err := d.run(ctx, "pg_dump", "-h", "127.0.0.1", "-p", strconv.Itoa(d.C.DBPort),
		"-U", dbUser, "-Fc", "-f", path, dbName)
	return err
}

// Restore заменяет базу содержимым резервной копии. Одной транзакцией:
// копия, которая не легла целиком, не должна оставить базу наполовину
// старой, наполовину новой.
func (d DB) Restore(ctx context.Context, path string) error {
	_, err := d.run(ctx, "pg_restore", "-h", "127.0.0.1", "-p", strconv.Itoa(d.C.DBPort),
		"-U", dbUser, "-d", dbName, "--clean", "--if-exists", "--no-owner", "--single-transaction", path)
	return err
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
