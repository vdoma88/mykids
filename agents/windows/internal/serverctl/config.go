package serverctl

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
)

// Config — настройки сервера. Заводятся сами при первом запуске: родитель
// ничего не придумывает и никуда не вписывает.
type Config struct {
	// Port — порт, по которому семья открывает MyKids.
	Port int `json:"port"`
	// DBPort — порт базы. Слушает она только этот компьютер, а номер
	// нестандартный, чтобы не столкнуться с PostgreSQL, если он уже стоит.
	DBPort int `json:"dbPort"`
	// DBPassword — пароль базы, случайный. Человеку он не нужен никогда.
	DBPassword string `json:"dbPassword"`
}

const (
	DefaultPort   = 3000
	DefaultDBPort = 54329
	dbUser        = "mykids"
	dbName        = "mykids"
)

// LoadConfig читает настройки, а если их нет — заводит и сохраняет.
func LoadConfig(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		c := Config{Port: DefaultPort, DBPort: DefaultDBPort, DBPassword: randomHex(24)}
		return c, SaveConfig(path, c)
	}
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	if c.Port == 0 {
		c.Port = DefaultPort
	}
	if c.DBPort == 0 {
		c.DBPort = DefaultDBPort
	}
	if c.DBPassword == "" {
		return Config{}, fmt.Errorf("%s: нет пароля базы — файл повреждён", path)
	}
	return c, nil
}

// ReadConfig читает настройки, ничего не заводя. Нет файла — значения по
// умолчанию: окно управления показывает их, а заведёт файл служба.
func ReadConfig(path string) (Config, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return Config{Port: DefaultPort, DBPort: DefaultDBPort}, nil
	}
	return LoadConfig(path)
}

// SaveConfig пишет настройки. Права 0600: в файле пароль базы.
func SaveConfig(path string, c Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ValidPort — можно ли слушать такой порт без прав, которых у службы нет
// смысла просить, и не столкнуться с базой.
func ValidPort(p int) error {
	if p < 1024 || p > 65535 {
		return fmt.Errorf("порт должен быть от 1024 до 65535")
	}
	if p == DefaultDBPort {
		return fmt.Errorf("порт %d занят базой MyKids", p)
	}
	return nil
}

// DatabaseURL — строка подключения для API.
func (c Config) DatabaseURL() string {
	u := url.URL{
		Scheme: "postgresql",
		User:   url.UserPassword(dbUser, c.DBPassword),
		Host:   "127.0.0.1:" + strconv.Itoa(c.DBPort),
		Path:   "/" + dbName,
	}
	return u.String()
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // без случайных чисел система неработоспособна вообще
	}
	return hex.EncodeToString(b)
}
