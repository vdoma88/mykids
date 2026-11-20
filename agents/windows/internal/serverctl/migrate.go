package serverctl

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Миграции схемы — те же файлы, что применяет Prisma в образе Docker, и
// учёт их — в той же таблице _prisma_migrations, в том же виде. Prisma CLI с
// собой не берём: ему нужен отдельный движок под Windows и ещё одна копия
// Node при каждом запуске. psql уже есть в комплекте PostgreSQL.
//
// Совместимость таблицы — не формальность: перенеся базу с Windows в Docker,
// семья не должна получить «миграции применены неизвестно кем».

// Migration — одна миграция.
type Migration struct {
	Name     string
	Path     string
	Checksum string
}

// ListMigrations — миграции каталога по порядку. Имя начинается с отметки
// времени, поэтому порядок имён и есть порядок применения — как у Prisma.
func ListMigrations(dir string) ([]Migration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []Migration
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name(), "migration.sql")
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("миграция %s: %w", e.Name(), err)
		}
		sum := sha256.Sum256(raw)
		out = append(out, Migration{Name: e.Name(), Path: path, Checksum: hex.EncodeToString(sum[:])})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Pending — какие из миграций ещё не применены.
func Pending(all []Migration, applied map[string]bool) []Migration {
	var out []Migration
	for _, m := range all {
		if !applied[m.Name] {
			out = append(out, m)
		}
	}
	return out
}

// createMigrationsTable — таблица учёта, как её заводит Prisma.
const createMigrationsTable = `CREATE TABLE IF NOT EXISTS "_prisma_migrations" (
    "id"                    VARCHAR(36) PRIMARY KEY NOT NULL,
    "checksum"              VARCHAR(64) NOT NULL,
    "finished_at"           TIMESTAMPTZ,
    "migration_name"        VARCHAR(255) NOT NULL,
    "logs"                  TEXT,
    "rolled_back_at"        TIMESTAMPTZ,
    "started_at"            TIMESTAMPTZ NOT NULL DEFAULT now(),
    "applied_steps_count"   INTEGER NOT NULL DEFAULT 0
);`

// recordMigration — запись об успешно применённой миграции.
func recordMigration(m Migration) string {
	return fmt.Sprintf(`INSERT INTO "_prisma_migrations" `+
		`(id, checksum, migration_name, started_at, finished_at, applied_steps_count) `+
		`VALUES ('%s', '%s', '%s', now(), now(), 1);`,
		uuid4(), m.Checksum, strings.ReplaceAll(m.Name, "'", "''"))
}

func uuid4() string {
	b := make([]byte, 16)
	h, _ := hex.DecodeString(randomHex(16))
	copy(b, h)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	x := hex.EncodeToString(b)
	return x[0:8] + "-" + x[8:12] + "-" + x[12:16] + "-" + x[16:20] + "-" + x[20:32]
}
