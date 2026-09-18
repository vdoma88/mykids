// Package outbox хранит расход экрана, ещё не принятый сервером.
//
// Взят файл строк JSON, а не SQLite: в очереди десятки записей, а зависимость
// от CGO сломала бы кросс-сборку под Windows из Linux. Запись атомарна через
// временный файл и переименование — обрыв питания не оставит обрезанный хвост.
package outbox

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Entry — порция израсходованных минут.
type Entry struct {
	// Монотонный счётчик устройства. Сервер отбрасывает повторы по паре
	// устройство+счётчик, поэтому переиспользовать значения нельзя.
	Seq        int       `json:"seq"`
	Minutes    int       `json:"minutes"`
	OccurredAt time.Time `json:"occurredAt"`
}

// Outbox — очередь на диске.
type Outbox struct {
	path    string
	entries []Entry
	nextSeq int
}

// Open читает очередь. Битые строки пропускаются: потерять одну запись лучше,
// чем не подняться вовсе и перестать считать время.
func Open(path string) (*Outbox, error) {
	o := &Outbox{path: path, nextSeq: 1}

	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return o, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		o.entries = append(o.entries, e)
		if e.Seq >= o.nextSeq {
			o.nextSeq = e.Seq + 1
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("чтение %s: %w", path, err)
	}
	return o, nil
}

// NextSeq — какой счётчик получит следующая запись.
func (o *Outbox) NextSeq() int { return o.nextSeq }

// Len — сколько записей ждёт отправки.
func (o *Outbox) Len() int { return len(o.entries) }

// Pending возвращает копию очереди.
func (o *Outbox) Pending() []Entry {
	out := make([]Entry, len(o.entries))
	copy(out, o.entries)
	return out
}

// Total — сколько минут суммарно ждёт отправки.
func (o *Outbox) Total() int {
	sum := 0
	for _, e := range o.entries {
		sum += e.Minutes
	}
	return sum
}

// Add ставит порцию в очередь и сразу пишет её на диск.
func (o *Outbox) Add(minutes int, at time.Time) (Entry, error) {
	if minutes <= 0 {
		return Entry{}, fmt.Errorf("минут должно быть больше нуля, получено %d", minutes)
	}
	e := Entry{Seq: o.nextSeq, Minutes: minutes, OccurredAt: at}
	o.nextSeq++
	o.entries = append(o.entries, e)
	return e, o.flush()
}

// Ack убирает принятые сервером записи.
//
// Счётчик nextSeq не сбрасывается: сервер помнит принятые пары устройство+счётчик,
// и повторное использование номера привело бы к молчаливой потере списания.
func (o *Outbox) Ack(accepted []Entry) error {
	if len(accepted) == 0 {
		return nil
	}
	done := make(map[int]bool, len(accepted))
	for _, e := range accepted {
		done[e.Seq] = true
	}
	kept := o.entries[:0]
	for _, e := range o.entries {
		if !done[e.Seq] {
			kept = append(kept, e)
		}
	}
	o.entries = kept
	return o.flush()
}

// Trim не даёт очереди расти бесконечно, если сервера нет неделями.
// Отбрасываются самые старые: свежий расход важнее.
//
// Результат пишется на диск сразу: иначе очередь отросла бы обратно при
// следующем запуске и ограничение не работало бы вовсе.
func (o *Outbox) Trim(max int) (int, error) {
	if max <= 0 || len(o.entries) <= max {
		return 0, nil
	}
	dropped := len(o.entries) - max
	o.entries = append(o.entries[:0], o.entries[dropped:]...)
	return dropped, o.flush()
}

func (o *Outbox) flush() error {
	if err := os.MkdirAll(filepath.Dir(o.path), 0o755); err != nil {
		return err
	}
	tmp := o.path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}

	writer := bufio.NewWriter(file)
	for _, e := range o.entries {
		raw, err := json.Marshal(e)
		if err != nil {
			file.Close()
			return err
		}
		if _, err := writer.Write(append(raw, '\n')); err != nil {
			file.Close()
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, o.path)
}
