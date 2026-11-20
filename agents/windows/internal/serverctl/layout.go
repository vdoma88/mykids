// Package serverctl — домашний сервер MyKids без Docker и без терминала.
//
// Для семьи, у которой сервер — обычный компьютер с Windows, Docker — лишняя
// сущность: его надо поставить, понять и чинить. Поэтому установщик кладёт
// рядом всё, что нужно серверу, — Node.js, PostgreSQL и собранный API, — а
// эта служба поднимает их, следит за ними и делает резервные копии.
//
// Код общий для Windows и Linux: так его можно проверить целиком на живых
// PostgreSQL и Node там, где нет Windows. Различается только то, как
// запускаются процессы.
package serverctl

import (
	"os"
	"path/filepath"
	"runtime"
)

// Layout — где что лежит.
type Layout struct {
	// Home — каталог программы: node/, pgsql/, app/.
	Home string
	// Data — каталог данных: база, настройки, журналы, резервные копии.
	// Отдельно от программы: обновление программы не должно трогать данные.
	Data string
}

func exe(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// Node — node.exe из комплекта. MYKIDS_NODE переопределяет: так проверяют
// на машине, где Node стоит отдельно.
func (l Layout) Node() string {
	if p := os.Getenv("MYKIDS_NODE"); p != "" {
		return p
	}
	return filepath.Join(l.Home, "node", exe("node"))
}

// PgBin — программа PostgreSQL из комплекта. MYKIDS_PG_BIN переопределяет
// каталог.
func (l Layout) PgBin(name string) string {
	dir := os.Getenv("MYKIDS_PG_BIN")
	if dir == "" {
		dir = filepath.Join(l.Home, "pgsql", "bin")
	}
	return filepath.Join(dir, exe(name))
}

func (l Layout) App() string        { return filepath.Join(l.Home, "app") }
func (l Layout) Server() string     { return filepath.Join(l.App(), "server.mjs") }
func (l Layout) Web() string        { return filepath.Join(l.App(), "web") }
func (l Layout) Packs() string      { return filepath.Join(l.App(), "packs") }
func (l Layout) Migrations() string { return filepath.Join(l.App(), "migrations") }

func (l Layout) PgData() string  { return filepath.Join(l.Data, "pgdata") }
func (l Layout) Config() string  { return filepath.Join(l.Data, "config.json") }
func (l Layout) Logs() string    { return filepath.Join(l.Data, "logs") }
func (l Layout) Backups() string { return filepath.Join(l.Data, "backups") }

// DefaultData — каталог данных на машине целиком: служба работает под
// LocalSystem, а окно управления — под родителем, и видеть им надо одно и то
// же.
func DefaultData() string {
	if p := os.Getenv("ProgramData"); p != "" {
		return filepath.Join(p, "MyKids Server")
	}
	return "/var/lib/mykids-server"
}
