package setup

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf16"
)

func serve(t *testing.T, h http.HandlerFunc) Input {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return Input{Server: srv.URL, Token: "tok-1"}
}

func TestVerifyTokenAcceptedNamesTheChild(t *testing.T) {
	var gotAuth, gotPath string
	in := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("authorization"), r.URL.Path
		w.Header().Set("content-type", "application/json")
		w.Write([]byte(`{"name":" Маша ","balances":{}}`))
	})
	v, p := VerifyToken(context.Background(), http.DefaultClient, in)
	if p != nil {
		t.Fatalf("неожиданная проблема: %+v", p)
	}
	if v.Child != "Маша" {
		t.Errorf("имя ребёнка: %q", v.Child)
	}
	if gotAuth != "Bearer tok-1" || gotPath != "/child/me" {
		t.Errorf("запрос: %q %q", gotAuth, gotPath)
	}
}

func TestVerifyTokenRejectedPointsAtToken(t *testing.T) {
	in := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"unauthorized"}`))
	})
	_, p := VerifyToken(context.Background(), http.DefaultClient, in)
	if p == nil || p.Field != FieldToken || p.Severity != Stop {
		t.Fatalf("ожидался отказ по токену: %+v", p)
	}
	if !strings.Contains(p.Hint, "новый") {
		t.Errorf("подсказка должна вести к новому токену: %q", p.Hint)
	}
}

// Адрес чего-то другого — роутера, другой программы — отвечает 200 со своей
// страницей. Принять это за сервер значило бы «установить» агент, который
// никогда ничего не отправит.
func TestVerifyTokenForeignServerIsNotAccepted(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"html": func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("<html>router</html>")) },
		"404":  func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) },
		"json без имени": func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"ok":true}`))
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, p := VerifyToken(context.Background(), http.DefaultClient, serve(t, h))
			if p == nil || p.Field != FieldServer || !strings.Contains(p.Detail, "не сервер MyKids") {
				t.Fatalf("ожидалось «не сервер MyKids»: %+v", p)
			}
		})
	}
}

func TestVerifyTokenServerError(t *testing.T) {
	in := serve(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) })
	_, p := VerifyToken(context.Background(), http.DefaultClient, in)
	if p == nil || p.Field != FieldServer || !strings.Contains(p.Detail, "503") {
		t.Fatalf("ожидалась ошибка сервера: %+v", p)
	}
}

func TestVerifyTokenUnreachableExplainsWhatToCheck(t *testing.T) {
	// Свободный порт: слушатель открыт и сразу закрыт.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()

	_, p := VerifyToken(context.Background(), http.DefaultClient, Input{Server: "http://" + addr, Token: "t"})
	if p == nil || p.Field != FieldServer || p.Severity != Stop {
		t.Fatalf("ожидалась недоступность: %+v", p)
	}
	if !strings.Contains(p.Detail, "отклонено") {
		t.Errorf("причина: %q", p.Detail)
	}
	if !strings.Contains(p.Hint, "брандмауэр") || !strings.Contains(p.Hint, "http://"+addr) {
		t.Errorf("подсказка должна назвать адрес и брандмауэр: %q", p.Hint)
	}
}

func TestCandidatesSkipAdminsAndDuplicates(t *testing.T) {
	got := Candidates([]Account{
		{Name: "Папа", Admin: true},
		{Name: "маша"},
		{Name: "Маша"},
		{Name: " "},
		{Name: "Андрей"},
	})
	want := []string{"Андрей", "маша"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("кандидаты: %v, ожидались %v", got, want)
	}
}

func TestUTF16RoundTripsCyrillicWithoutBOM(t *testing.T) {
	raw := UTF16([]string{"Маша", "две\nстроки"})
	if raw[0] == 0xFF && raw[1] == 0xFE {
		t.Fatal("BOM в начале: мастер покажет его невидимым символом")
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = uint16(raw[2*i]) | uint16(raw[2*i+1])<<8
	}
	got := string(utf16.Decode(units))
	if got != "Маша\r\nдве\r\nстроки\r\n" {
		t.Errorf("текст: %q", got)
	}
}

func TestDescribeMarksSeverity(t *testing.T) {
	stop := Describe(Problem{FieldToken, Stop, "не принят", "выдайте новый"})
	warn := Describe(Problem{FieldChild, Warn, "не названа", ""})
	if !strings.HasPrefix(stop, "Ошибка") || !strings.Contains(stop, "\nвыдайте новый") {
		t.Errorf("ошибка: %q", stop)
	}
	if !strings.HasPrefix(warn, "Внимание") || strings.Contains(warn, "\n") {
		t.Errorf("предупреждение: %q", warn)
	}
}
