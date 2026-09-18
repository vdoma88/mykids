package outbox

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func at(min int) time.Time {
	return time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC).Add(time.Duration(min) * time.Minute)
}

func open(t *testing.T, path string) *Outbox {
	t.Helper()
	o, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return o
}

func add(t *testing.T, o *Outbox, minutes int, when time.Time) Entry {
	t.Helper()
	e, err := o.Add(minutes, when)
	if err != nil {
		t.Fatalf("Add(%d): %v", minutes, err)
	}
	return e
}

func TestOpenMissingFile(t *testing.T) {
	o := open(t, filepath.Join(t.TempDir(), "nested", "outbox.jsonl"))
	if o.Len() != 0 || o.Total() != 0 {
		t.Fatalf("пустая очередь: Len=%d Total=%d", o.Len(), o.Total())
	}
	if o.NextSeq() != 1 {
		t.Fatalf("счётчик начинается с 1, получено %d", o.NextSeq())
	}
}

func TestAddSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox.jsonl")
	o := open(t, path)
	add(t, o, 5, at(0))
	add(t, o, 7, at(5))

	// Перезапуск агента: очередь читается с диска.
	again := open(t, path)
	if again.Len() != 2 {
		t.Fatalf("после перезапуска ожидалось 2 записи, получено %d", again.Len())
	}
	if again.Total() != 12 {
		t.Fatalf("после перезапуска ожидалось 12 минут, получено %d", again.Total())
	}
	pending := again.Pending()
	if pending[0].Seq != 1 || pending[1].Seq != 2 {
		t.Fatalf("счётчики не сохранились: %+v", pending)
	}
	if !pending[0].OccurredAt.Equal(at(0)) {
		t.Fatalf("время записи не сохранилось: %v", pending[0].OccurredAt)
	}
}

func TestSeqNeverRepeatsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox.jsonl")
	o := open(t, path)
	add(t, o, 1, at(0))
	add(t, o, 1, at(1))

	// Сервер отбрасывает повторы по паре устройство+счётчик: если после
	// перезапуска агент начнёт нумерацию заново, списание молча пропадёт.
	again := open(t, path)
	third := add(t, again, 1, at(2))
	if third.Seq != 3 {
		t.Fatalf("после перезапуска ожидался счётчик 3, получен %d", third.Seq)
	}
}

func TestSeqNeverRepeatsAfterAck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox.jsonl")
	o := open(t, path)
	first := add(t, o, 3, at(0))
	second := add(t, o, 4, at(3))

	if err := o.Ack([]Entry{first, second}); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	if o.Len() != 0 {
		t.Fatalf("после Ack очередь должна опустеть, осталось %d", o.Len())
	}

	next := add(t, o, 5, at(7))
	if next.Seq != 3 {
		t.Fatalf("Ack не должен сбрасывать счётчик, получено %d", next.Seq)
	}

	// И на диске тоже: перезапуск не должен воскресить принятые записи.
	again := open(t, path)
	if again.Len() != 1 || again.Pending()[0].Seq != 3 {
		t.Fatalf("после перезапуска ожидалась одна запись со счётчиком 3, получено %+v", again.Pending())
	}
	if again.NextSeq() != 4 {
		t.Fatalf("счётчик после перезапуска должен быть 4, получено %d", again.NextSeq())
	}
}

func TestAckPartial(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox.jsonl")
	o := open(t, path)
	first := add(t, o, 3, at(0))
	add(t, o, 4, at(3))
	third := add(t, o, 5, at(7))

	// Сервер принял не всё: непринятые записи остаются ждать.
	if err := o.Ack([]Entry{first, third}); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	pending := o.Pending()
	if len(pending) != 1 || pending[0].Seq != 2 || pending[0].Minutes != 4 {
		t.Fatalf("должна остаться только вторая запись, получено %+v", pending)
	}
	if o.Total() != 4 {
		t.Fatalf("ожидалось 4 минуты в очереди, получено %d", o.Total())
	}
}

func TestAckEmptyKeepsQueue(t *testing.T) {
	o := open(t, filepath.Join(t.TempDir(), "outbox.jsonl"))
	add(t, o, 3, at(0))
	if err := o.Ack(nil); err != nil {
		t.Fatalf("Ack(nil): %v", err)
	}
	if o.Len() != 1 {
		t.Fatalf("пустой Ack не должен трогать очередь, осталось %d", o.Len())
	}
}

func TestAckUnknownSeqIgnored(t *testing.T) {
	o := open(t, filepath.Join(t.TempDir(), "outbox.jsonl"))
	kept := add(t, o, 3, at(0))
	if err := o.Ack([]Entry{{Seq: 999, Minutes: 1}}); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	pending := o.Pending()
	if len(pending) != 1 || pending[0].Seq != kept.Seq {
		t.Fatalf("чужой счётчик не должен ничего удалять, получено %+v", pending)
	}
}

func TestPendingIsCopy(t *testing.T) {
	o := open(t, filepath.Join(t.TempDir(), "outbox.jsonl"))
	add(t, o, 3, at(0))

	pending := o.Pending()
	pending[0].Minutes = 999

	if o.Total() != 3 {
		t.Fatalf("Pending отдал ссылку на внутренний срез: Total=%d", o.Total())
	}
}

func TestAddRejectsNonPositive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox.jsonl")
	o := open(t, path)
	for _, minutes := range []int{0, -1} {
		if _, err := o.Add(minutes, at(0)); err == nil {
			t.Fatalf("Add(%d) должен быть ошибкой", minutes)
		}
	}
	if o.Len() != 0 {
		t.Fatalf("отклонённая запись не должна попадать в очередь, Len=%d", o.Len())
	}
	if o.NextSeq() != 1 {
		t.Fatalf("отклонённая запись не должна тратить счётчик, NextSeq=%d", o.NextSeq())
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("отклонённая запись не должна создавать файл")
	}
}

func TestOpenSkipsCorruptLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox.jsonl")
	raw := `{"seq":1,"minutes":3,"occurredAt":"2026-09-18T10:00:00Z"}
не json вовсе

{"seq":2,"minutes":4,"occurr
{"seq":3,"minutes":5,"occurredAt":"2026-09-18T10:07:00Z"}
`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("подготовка файла: %v", err)
	}

	// Потерять одну запись лучше, чем не подняться вовсе и перестать считать время.
	o := open(t, path)
	if o.Len() != 2 {
		t.Fatalf("ожидались 2 уцелевшие записи, получено %d", o.Len())
	}
	if o.Total() != 8 {
		t.Fatalf("ожидалось 8 минут, получено %d", o.Total())
	}
	if o.NextSeq() != 4 {
		t.Fatalf("счётчик должен продолжиться с 4, получено %d", o.NextSeq())
	}
}

func TestOpenTakesMaxSeqNotCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox.jsonl")
	// В файле осталась одна запись с большим счётчиком: остальные приняты.
	raw := `{"seq":42,"minutes":3,"occurredAt":"2026-09-18T10:00:00Z"}` + "\n"
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("подготовка файла: %v", err)
	}
	o := open(t, path)
	if o.NextSeq() != 43 {
		t.Fatalf("счётчик должен идти от максимального, получено %d", o.NextSeq())
	}
}

func TestTrimDropsOldest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox.jsonl")
	o := open(t, path)
	for i := 1; i <= 5; i++ {
		add(t, o, i, at(i))
	}

	// Свежий расход важнее: отбрасываем самые старые.
	dropped, err := o.Trim(2)
	if err != nil {
		t.Fatalf("Trim: %v", err)
	}
	if dropped != 3 {
		t.Fatalf("ожидалось 3 отброшенных, получено %d", dropped)
	}
	pending := o.Pending()
	if len(pending) != 2 || pending[0].Seq != 4 || pending[1].Seq != 5 {
		t.Fatalf("должны остаться последние две записи, получено %+v", pending)
	}
	if o.Total() != 9 {
		t.Fatalf("ожидалось 9 минут, получено %d", o.Total())
	}
}

func TestTrimSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox.jsonl")
	o := open(t, path)
	for i := 1; i <= 5; i++ {
		add(t, o, i, at(i))
	}
	if _, err := o.Trim(2); err != nil {
		t.Fatalf("Trim: %v", err)
	}

	// Если Trim не пишет на диск, очередь отрастает обратно при перезапуске
	// и ограничение не работает вовсе.
	again := open(t, path)
	if again.Len() != 2 {
		t.Fatalf("после перезапуска ожидались 2 записи, получено %d", again.Len())
	}
	if again.NextSeq() != 6 {
		t.Fatalf("счётчик должен остаться 6, получено %d", again.NextSeq())
	}
}

func TestTrimNoopCases(t *testing.T) {
	o := open(t, filepath.Join(t.TempDir(), "outbox.jsonl"))
	for i := 1; i <= 3; i++ {
		add(t, o, 1, at(i))
	}
	if dropped, err := o.Trim(3); err != nil || dropped != 0 {
		t.Fatalf("Trim по границе не должен ничего трогать: %d, %v", dropped, err)
	}
	if dropped, err := o.Trim(10); err != nil || dropped != 0 {
		t.Fatalf("Trim выше длины не должен ничего трогать: %d, %v", dropped, err)
	}
	if dropped, err := o.Trim(0); err != nil || dropped != 0 {
		t.Fatalf("Trim(0) выключен: %d, %v", dropped, err)
	}
	if o.Len() != 3 {
		t.Fatalf("очередь не должна была измениться, Len=%d", o.Len())
	}
}

func TestFlushLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "outbox.jsonl")
	o := open(t, path)
	add(t, o, 3, at(0))

	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Fatal("временный файл должен быть переименован, а не оставлен рядом")
	}
	names, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(names) != 1 || names[0].Name() != "outbox.jsonl" {
		t.Fatalf("в каталоге должен быть один файл очереди, получено %v", names)
	}
}

func TestOpenCreatesNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "outbox.jsonl")
	open(t, path)
	if _, err := os.Stat(path); err == nil {
		t.Fatal("чтение пустой очереди не должно создавать файл")
	}
}
