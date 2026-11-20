package serverctl

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Supervisor держит сервер живым: база, миграции, API, резервные копии.
type Supervisor struct {
	L   Layout
	C   Config
	Log func(string)

	// Now и BackupEvery — для тестов.
	Now         func() time.Time
	BackupEvery time.Duration
}

func (s *Supervisor) log(format string, a ...any) {
	if s.Log != nil {
		s.Log(fmt.Sprintf(format, a...))
	}
}

func (s *Supervisor) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Supervisor) db() DB { return DB{L: s.L, C: s.C, Log: s.Log} }

// Prepare поднимает базу и доводит схему до текущей. Отдельно от Run: окну
// управления нужна база и без API — например, чтобы восстановить копию.
func (s *Supervisor) Prepare(ctx context.Context) error {
	// Каталог данных заводит и закрывает установщик (Protect): у службы нет
	// прав ни создать его в ProgramData, ни поменять ему доступ.
	for _, dir := range []string{s.L.Data, s.L.Logs(), s.L.Backups()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	db := s.db()
	if err := db.Init(ctx); err != nil {
		return fmt.Errorf("заведение базы: %w", err)
	}
	if err := db.Start(ctx); err != nil {
		return fmt.Errorf("запуск базы: %w", err)
	}
	if err := db.EnsureDatabase(ctx); err != nil {
		return fmt.Errorf("база mykids: %w", err)
	}
	if err := s.restoreRequested(ctx); err != nil {
		return err
	}
	applied, err := db.Migrate(ctx)
	if err != nil {
		return err
	}
	if len(applied) > 0 {
		s.log("Применено миграций: %d", len(applied))
	}
	return nil
}

// RestoreFile — куда окно управления кладёт копию, которую надо
// восстановить. Восстанавливает служба при следующем запуске: базу запускает
// только она, и только у неё права на её файлы.
func (l Layout) RestoreFile() string { return filepath.Join(l.Data, "restore.dump") }

// restoreRequested восстанавливает базу из копии, если окно управления об
// этом попросило. Перед этим — копия того, что есть сейчас: восстановление
// не того файла не должно стоить семье всей истории.
func (s *Supervisor) restoreRequested(ctx context.Context) error {
	path := s.L.RestoreFile()
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	db := s.db()
	before := filepath.Join(s.L.Backups(), "mykids-"+s.now().Format("2006-01-02-1504")+"-before-restore.dump")
	if err := db.Backup(ctx, before); err != nil {
		os.Remove(path)
		return fmt.Errorf("перед восстановлением не удалось сохранить нынешнюю базу, восстановление отменено: %w", err)
	}
	s.log("Нынешняя база сохранена: %s", before)
	err := db.Restore(ctx, path)
	os.Remove(path)
	if err != nil {
		s.log("Восстановление не удалось, база осталась прежней: %v", err)
		return nil
	}
	s.log("База восстановлена из копии")
	return nil
}

// Run — работа службы: до отмены контекста. Базу останавливает сам.
func (s *Supervisor) Run(ctx context.Context) error {
	if err := s.Prepare(ctx); err != nil {
		return err
	}
	defer func() {
		if err := s.db().Stop(); err != nil {
			s.log("Остановка базы: %v", err)
		}
	}()
	s.log("База готова, запускаю MyKids на порту %d", s.C.Port)

	every := s.BackupEvery
	if every == 0 {
		every = time.Hour
	}
	backups := time.NewTicker(every)
	defer backups.Stop()
	s.maybeBackup(ctx)

	backoff := 2 * time.Second
	for {
		started := s.now()
		exited, stop, err := s.startAPI(ctx)
		if err != nil {
			s.log("API не запустился: %v", err)
		} else {
		wait:
			for {
				select {
				case <-ctx.Done():
					stop()
					return nil
				case err := <-exited:
					s.log("API завершился: %v", err)
					break wait
				case <-backups.C:
					s.maybeBackup(ctx)
				}
			}
		}
		// Проработал долго — значит, падение случайное, и перезапуск сразу.
		// Падает сразу после старта — ждём всё дольше, чтобы не крутить
		// перезапуски впустую и не забить журнал.
		if s.now().Sub(started) > 5*time.Minute {
			backoff = 2 * time.Second
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		if backoff < time.Minute {
			backoff *= 2
		}
	}
}

// startAPI запускает node с API. stop останавливает его и ждёт конца.
func (s *Supervisor) startAPI(ctx context.Context) (<-chan error, func(), error) {
	logFile, err := os.OpenFile(filepath.Join(s.L.Logs(), "server.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, err
	}
	rotate(logFile, filepath.Join(s.L.Logs(), "server.log"))

	cmd := exec.Command(s.L.Node(), s.L.Server())
	cmd.Dir = s.L.App()
	cmd.Env = append(os.Environ(), s.apiEnv()...)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	hide(cmd)
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return nil, nil, err
	}
	exited := make(chan error, 1)
	go func() {
		exited <- cmd.Wait()
		logFile.Close()
	}()
	stop := func() {
		terminate(cmd.Process)
		select {
		case <-exited:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-exited
		}
	}
	return exited, stop, nil
}

func (s *Supervisor) apiEnv() []string {
	return []string{
		"DATABASE_URL=" + s.C.DatabaseURL(),
		"PORT=" + strconv.Itoa(s.C.Port),
		"MYKIDS_WEB_ROOT=" + s.L.Web(),
		"MYKIDS_CONTENT_ROOT=" + s.L.Packs(),
		"NODE_ENV=production",
		"LOG_LEVEL=warn",
	}
}

// rotate обрезает журнал, если он разросся: на домашней машине он не должен
// съесть диск за год работы.
func rotate(f *os.File, path string) {
	if st, err := f.Stat(); err == nil && st.Size() > 10<<20 {
		_ = f.Truncate(0)
		_, _ = f.Seek(0, io.SeekStart)
		fmt.Fprintf(f, "журнал обрезан %s: был больше 10 МБ\n", time.Now().Format(time.RFC3339))
	}
}

// KeepBackups — сколько ежедневных копий хранить.
const KeepBackups = 7

// maybeBackup делает копию, если последней больше суток. Копия — каждый
// день сама: о резервных копиях вспоминают, когда диск уже умер.
func (s *Supervisor) maybeBackup(ctx context.Context) {
	last := LastBackup(s.L.Backups())
	if !last.IsZero() && s.now().Sub(last) < 24*time.Hour {
		return
	}
	path := filepath.Join(s.L.Backups(), "mykids-"+s.now().Format("2006-01-02-1504")+".dump")
	if err := s.db().Backup(ctx, path); err != nil {
		s.log("Резервная копия не сделана: %v", err)
		os.Remove(path)
		return
	}
	s.log("Резервная копия: %s", path)
	PruneBackups(s.L.Backups(), KeepBackups)
}

// Backups — копии в каталоге, от новых к старым.
func Backups(dir string) []os.FileInfo {
	entries, _ := os.ReadDir(dir)
	var out []os.FileInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".dump") || !strings.HasPrefix(e.Name(), "mykids-") {
			continue
		}
		if info, err := e.Info(); err == nil {
			out = append(out, info)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime().After(out[j].ModTime()) })
	return out
}

// LastBackup — время последней копии; нуль, если копий нет.
func LastBackup(dir string) time.Time {
	b := Backups(dir)
	if len(b) == 0 {
		return time.Time{}
	}
	return b[0].ModTime()
}

// PruneBackups оставляет keep последних копий.
func PruneBackups(dir string, keep int) {
	for i, b := range Backups(dir) {
		if i >= keep {
			os.Remove(filepath.Join(dir, b.Name()))
		}
	}
}

// Healthy — отвечает ли MyKids на этом компьютере.
func Healthy(ctx context.Context, port int) bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://127.0.0.1:"+strconv.Itoa(port)+"/health", nil)
	if err != nil {
		return false
	}
	res, err := http.DefaultTransport.RoundTrip(req) // без прокси: адрес свой
	if err != nil {
		return false
	}
	res.Body.Close()
	return res.StatusCode == http.StatusOK
}

// LANURLs — адреса, по которым MyKids видно из домашней сети. Именно их
// родитель вписывает в установщик на компьютере ребёнка, и именно их раньше
// приходилось выяснять командой ipconfig.
func LANURLs(port int) []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []string
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			if u := lanURL(ipnet.IP, port); u != "" {
				out = append(out, u)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return lanRank(out[i]) < lanRank(out[j]) })
	return out
}

// lanURL — адрес для домашней сети или пусто, если адрес не годится.
func lanURL(ip net.IP, port int) string {
	ip4 := ip.To4()
	// IPv6 в домашней сети вписывать руками неудобно и незачем; адреса
	// 169.254.* — признак того, что сеть не настроена, а не адрес.
	if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() {
		return ""
	}
	return "http://" + ip4.String() + ":" + strconv.Itoa(port)
}

// lanRank ставит обычные домашние сети вперёд: 192.168.* почти всегда
// роутер, а 172.* — часто виртуальная сеть Hyper-V или WSL.
func lanRank(u string) int {
	switch {
	case strings.Contains(u, "//192.168."):
		return 0
	case strings.Contains(u, "//10."):
		return 1
	default:
		return 2
	}
}
