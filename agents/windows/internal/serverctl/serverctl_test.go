package serverctl

import (
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestConfigCreatedOnceWithRandomPassword(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.json")
	a, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if a.Port != DefaultPort || a.DBPort != DefaultDBPort || len(a.DBPassword) < 32 {
		t.Fatalf("настройки по умолчанию: %+v", a)
	}
	b, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("второе чтение завело новые настройки — пароль базы потерян бы при каждом запуске")
	}
	other, _ := LoadConfig(filepath.Join(dir, "other.json"))
	if other.DBPassword == a.DBPassword {
		t.Fatal("пароли двух установок совпали")
	}
}

func TestReadConfigDoesNotCreate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c, err := ReadConfig(path)
	if err != nil || c.Port != DefaultPort {
		t.Fatalf("%+v %v", c, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("окно управления завело настройки — пароль базы разошёлся бы с тем, что знает служба")
	}
}

func TestBrokenConfigIsAnErrorNotANewPassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(path, []byte(`{"port":3000}`), 0o600)
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("настройки без пароля приняты — служба потеряла бы доступ к своей же базе")
	}
}

func TestDatabaseURLEscapesPassword(t *testing.T) {
	c := Config{Port: 3000, DBPort: 54329, DBPassword: "a/b@c:d"}
	u, err := url.Parse(c.DatabaseURL())
	if err != nil {
		t.Fatal(err)
	}
	pw, _ := u.User.Password()
	if pw != "a/b@c:d" || u.Host != "127.0.0.1:54329" || u.Path != "/mykids" {
		t.Fatalf("строка подключения: %s", c.DatabaseURL())
	}
}

func TestValidPort(t *testing.T) {
	for _, p := range []int{80, 0, 70000, DefaultDBPort} {
		if ValidPort(p) == nil {
			t.Errorf("порт %d принят", p)
		}
	}
	for _, p := range []int{3000, 8080} {
		if err := ValidPort(p); err != nil {
			t.Errorf("порт %d отвергнут: %v", p, err)
		}
	}
}

func TestListMigrationsMatchesRepository(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "..", "server", "api", "prisma", "migrations")
	all, err := ListMigrations(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) < 5 {
		t.Fatalf("миграций: %d", len(all))
	}
	hex := regexp.MustCompile(`^[0-9a-f]{64}$`)
	for i, m := range all {
		if i > 0 && all[i-1].Name >= m.Name {
			t.Errorf("порядок нарушен: %s после %s", m.Name, all[i-1].Name)
		}
		if !hex.MatchString(m.Checksum) {
			t.Errorf("сумма %s: %q", m.Name, m.Checksum)
		}
	}
	if all[0].Name != "20260918081357_init" {
		t.Errorf("первая миграция: %s", all[0].Name)
	}
	pending := Pending(all, map[string]bool{all[0].Name: true})
	if len(pending) != len(all)-1 || pending[0].Name != all[1].Name {
		t.Errorf("недостающие: %v", pending)
	}
}

func TestRecordMigrationQuotesName(t *testing.T) {
	sql := recordMigration(Migration{Name: "x'; DROP TABLE y; --", Checksum: "ab"})
	if strings.Contains(sql, "'x';") || !strings.Contains(sql, "'x''; DROP TABLE y; --'") {
		t.Errorf("имя не экранировано: %s", sql)
	}
}

func TestUUID4Format(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if a, b := uuid4(), uuid4(); !re.MatchString(a) || a == b {
		t.Errorf("uuid: %s %s", a, b)
	}
}

func TestBackupsNewestFirstAndPruned(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC)
	for i := 0; i < 10; i++ {
		p := filepath.Join(dir, "mykids-"+base.AddDate(0, 0, i).Format("2006-01-02")+".dump")
		os.WriteFile(p, []byte("x"), 0o600)
		os.Chtimes(p, base.AddDate(0, 0, i), base.AddDate(0, 0, i))
	}
	os.WriteFile(filepath.Join(dir, "чужой.dump"), []byte("x"), 0o600)
	os.WriteFile(filepath.Join(dir, "restore.dump"), []byte("x"), 0o600)

	if got := LastBackup(dir); !got.Equal(base.AddDate(0, 0, 9)) {
		t.Errorf("последняя копия: %v", got)
	}
	PruneBackups(dir, 7)
	b := Backups(dir)
	if len(b) != 7 || b[0].Name() != "mykids-2026-01-10.dump" || b[6].Name() != "mykids-2026-01-04.dump" {
		names := []string{}
		for _, x := range b {
			names = append(names, x.Name())
		}
		t.Errorf("после чистки: %v", names)
	}
	if _, err := os.Stat(filepath.Join(dir, "чужой.dump")); err != nil {
		t.Error("чистка тронула чужой файл")
	}
	if !LastBackup(t.TempDir()).IsZero() {
		t.Error("в пустом каталоге нашлась копия")
	}
}

func TestLANURLs(t *testing.T) {
	cases := map[string]string{
		"192.168.1.10": "http://192.168.1.10:3000",
		"10.0.0.5":     "http://10.0.0.5:3000",
		"127.0.0.1":    "",
		"169.254.3.4":  "",
		"fe80::1":      "",
	}
	for ip, want := range cases {
		if got := lanURL(net.ParseIP(ip), 3000); got != want {
			t.Errorf("%s: %q, ожидалось %q", ip, got, want)
		}
	}
	if !(lanRank("http://192.168.0.2:1") < lanRank("http://10.0.0.1:1") &&
		lanRank("http://10.0.0.1:1") < lanRank("http://172.20.0.1:1")) {
		t.Error("домашняя сеть должна идти первой: 172.* — обычно виртуальная сеть WSL")
	}
}
