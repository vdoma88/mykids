package schedule

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Общий файл векторов с TypeScript-реализацией: логика окон существует в двух
// местах, и разъехаться они не должны.
type vectorFile struct {
	Timezone string   `json:"timezone"`
	Windows  []Window `json:"windows"`
	Cases    []struct {
		At     string  `json:"at"`
		Local  string  `json:"local"`
		Expect *string `json:"expect"`
		Note   string  `json:"note"`
	} `json:"cases"`
}

func TestSharedVectors(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "packages", "contracts", "test-vectors", "schedule.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("не прочитан файл векторов: %v", err)
	}

	var vf vectorFile
	if err := json.Unmarshal(raw, &vf); err != nil {
		t.Fatalf("не разобран файл векторов: %v", err)
	}
	if len(vf.Cases) < 10 {
		t.Fatalf("подозрительно мало векторов: %d", len(vf.Cases))
	}

	loc, err := time.LoadLocation(vf.Timezone)
	if err != nil {
		t.Fatalf("не загружен пояс %s: %v", vf.Timezone, err)
	}

	for _, c := range vf.Cases {
		at, err := time.Parse(time.RFC3339, c.At)
		if err != nil {
			t.Fatalf("не разобрано время %q: %v", c.At, err)
		}
		want := ""
		if c.Expect != nil {
			want = *c.Expect
		}
		if got := FirstMatch(vf.Windows, At(at, loc)); got != want {
			t.Errorf("%s (%s): получено %q, ожидалось %q", c.Local, c.Note, got, want)
		}
	}
}

func TestParseTimeOfDay(t *testing.T) {
	ok := map[string]int{"00:00": 0, "07:30": 450, "23:59": 1439}
	for in, want := range ok {
		got, err := ParseTimeOfDay(in)
		if err != nil || got != want {
			t.Errorf("ParseTimeOfDay(%q) = %d, %v; ожидалось %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "24:00", "12:60", "12", "аб:вг", "12:00:00"} {
		if _, err := ParseTimeOfDay(bad); err == nil {
			t.Errorf("ParseTimeOfDay(%q) не вернул ошибку", bad)
		}
	}
}

func TestEvaluatePrecedence(t *testing.T) {
	windows := []Window{
		{Name: "отбой", Days: []int{1}, From: "21:30", To: "07:00", Mode: ModeBlocked},
		{Name: "уроки", Days: []int{1}, From: "16:00", To: "18:00", Mode: ModeTasksOnly},
		{Name: "кружок", Days: []int{1}, From: "16:30", To: "17:00", Mode: ModeAllowed},
	}
	cases := []struct {
		minutes int
		mode    Mode
		window  string
	}{
		{22 * 60, ModeBlocked, "отбой"},
		{16*60 + 10, ModeTasksOnly, "уроки"},
		{16*60 + 40, ModeAllowed, "кружок"}, // разрешающее перекрывает
		{12 * 60, ModeAllowed, ""},          // вне окон
	}
	for _, c := range cases {
		got := Evaluate(windows, Moment{Weekday: 1, MinutesOfDay: c.minutes})
		if got.Mode != c.mode || got.Window != c.window {
			t.Errorf("минута %d: получено %v/%q, ожидалось %v/%q", c.minutes, got.Mode, got.Window, c.mode, c.window)
		}
	}
}

func TestDegenerateWindow(t *testing.T) {
	w := Window{Name: "нулевое", Days: []int{1}, From: "10:00", To: "10:00", Mode: ModeBlocked}
	if w.Covers(Moment{Weekday: 1, MinutesOfDay: 600}) {
		t.Error("окно нулевой длины не должно срабатывать")
	}
}
