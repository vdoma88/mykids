//go:build windows

package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows/svc"

	"github.com/vdoma88/mykids/agents/windows/internal/serverctl"
	"github.com/vdoma88/mykids/agents/windows/internal/win32"
	"github.com/vdoma88/mykids/agents/windows/internal/winsvc"
)

const panelTitle = "MyKids Сервер"
const fieldPort = "Порт"

var app *win32.App

// panel — окно управления сервером. Управляет службой, поэтому перезапускает
// себя от имени администратора: Windows спросит разрешение сама.
func panel(o options) int {
	if !win32.Elevated() {
		self, err := os.Executable()
		if err == nil {
			err = win32.RunElevated(self, "")
		}
		if err != nil {
			win32.Fail(panelTitle, "Окну управления нужны права администратора: оно управляет службой сервера.\n\n"+err.Error())
		}
		return 0
	}
	cfg, _ := serverctl.ReadConfig(o.L.Config())

	p := &serverPanel{o: o}
	app = win32.NewApp(panelTitle+" "+version, "", []win32.Button{
		{Text: "Обновить", Do: p.refresh},
		{Text: "Открыть админку", Do: p.openAdmin},
		{Text: "Скопировать адрес для ребёнка", Do: p.copyAddress},
		{Text: "Сделать резервную копию", Do: p.backup},
		{Text: "Восстановить из копии…", Do: p.restore},
		{Text: "Перезапустить", Do: p.restart},
		{Text: "Сменить порт", Do: p.changePort},
		{Text: "Журналы", Do: func(win32.Form, func(string)) { _ = win32.Open(o.L.Logs()) }},
		{Text: "Скопировать отчёт", Do: p.copyReport},
	}).WithFields(
		win32.Field{Label: fieldPort, Value: strconv.Itoa(cfg.Port)},
	).WithStart(p.refresh).WithIcon(filepath.Join(o.L.Home, "mykids.ico"))
	if err := win32.RunApp(app); err != nil {
		win32.Fail(panelTitle, err.Error())
		return 1
	}
	return 0
}

type serverPanel struct{ o options }

func (p *serverPanel) config() serverctl.Config {
	cfg, _ := serverctl.ReadConfig(p.o.L.Config())
	return cfg
}

func (p *serverPanel) refresh(_ win32.Form, log func(string)) {
	app.Clear()
	cfg := p.config()
	log("Состояние на " + time.Now().Format("15:04:05"))
	log("")

	state, installed, err := winsvc.State()
	switch {
	case err != nil:
		log("Служба: не удалось узнать — " + err.Error())
	case !installed:
		log("Служба: НЕ УСТАНОВЛЕНА. Запустите установщик MyKids Сервер ещё раз.")
	case state == svc.Running:
		log("Служба: работает и запускается вместе с Windows.")
	default:
		log("Служба: " + winsvc.StateName(state) + ". Нажмите «Перезапустить».")
	}

	if serverctl.Healthy(context.Background(), cfg.Port) {
		log("MyKids отвечает.")
	} else {
		log("MyKids НЕ ОТВЕЧАЕТ на порту " + strconv.Itoa(cfg.Port) + ". Если служба только что запущена, подождите минуту и нажмите «Обновить»; иначе загляните в «Журналы».")
	}
	log("")
	for _, l := range addressLines(cfg.Port) {
		log(l)
	}
	log("")
	log("Адрес «с других устройств дома» вписывают в установщик MyKids на компьютере ребёнка.")
	log("")

	b := serverctl.Backups(p.o.L.Backups())
	if len(b) == 0 {
		log("Резервных копий пока нет: первая появится в течение часа после запуска.")
	} else {
		log(fmt.Sprintf("Резервные копии: %d, последняя — %s. Делаются сами раз в сутки, хранятся последние %d.",
			len(b), b[0].ModTime().Format("02.01.2006 15:04"), serverctl.KeepBackups))
	}
	log("Данные: " + p.o.L.Data)
}

func (p *serverPanel) openAdmin(_ win32.Form, log func(string)) {
	if err := win32.Open("http://localhost:" + strconv.Itoa(p.config().Port)); err != nil {
		log("Не удалось открыть браузер: " + err.Error())
	}
}

func (p *serverPanel) copyAddress(_ win32.Form, log func(string)) {
	urls := serverctl.LANURLs(p.config().Port)
	if len(urls) == 0 {
		log("Адреса в домашней сети нет: компьютер не подключён к сети?")
		return
	}
	if err := win32.CopyToClipboard(urls[0]); err != nil {
		log("Не удалось скопировать: " + err.Error())
		return
	}
	log("Скопировано: " + urls[0] + " — вставьте его в установщик на компьютере ребёнка.")
}

func (p *serverPanel) backup(_ win32.Form, log func(string)) {
	cfg := p.config()
	path := filepath.Join(p.o.L.Backups(), "mykids-"+time.Now().Format("2006-01-02-1504")+"-manual.dump")
	log("Сохраняю базу…")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	db := serverctl.DB{L: p.o.L, C: cfg}
	if err := db.Backup(ctx, path); err != nil {
		log("Не получилось: " + err.Error())
		log("Резервная копия делается из работающей базы — проверьте, что служба работает.")
		return
	}
	log("Готово: " + path)
	if win32.Confirm(panelTitle, "Копия сохранена:\n"+path+"\n\nОткрыть папку? Копию стоит переложить на флешку или в облако — на случай, если умрёт этот диск.") {
		_ = win32.Open(p.o.L.Backups())
	}
}

func (p *serverPanel) restore(_ win32.Form, log func(string)) {
	path, ok := win32.OpenFile("Восстановить MyKids из копии", "Резервные копии MyKids", "*.dump", p.o.L.Backups())
	if !ok {
		return
	}
	if !win32.Confirm(panelTitle, "Вся база MyKids будет заменена содержимым копии:\n"+path+
		"\n\nВсё, что произошло после её создания, — начисления, покупки, учёт — пропадёт. "+
		"Нынешняя база перед этим сохранится отдельной копией.\n\nВосстановить?") {
		return
	}
	app.Clear()
	log("Готовлю восстановление…")
	if err := copyFile(path, p.o.L.RestoreFile()); err != nil {
		log("Не удалось подготовить копию: " + err.Error())
		return
	}
	// Восстанавливает служба при запуске: базу запускает только она.
	p.restartService(log)
	log("")
	for _, l := range tail(filepath.Join(p.o.L.Logs(), "service.log"), 6) {
		log(l)
	}
}

func (p *serverPanel) restart(_ win32.Form, log func(string)) {
	app.Clear()
	p.restartService(log)
}

func (p *serverPanel) restartService(log func(string)) {
	log("Останавливаю службу…")
	if err := serviceCommand("stop", p.o); err != nil {
		log("    " + err.Error())
	}
	log("Запускаю…")
	if err := winsvc.Start(); err != nil {
		log("Не запустилась: " + err.Error())
		return
	}
	port := p.config().Port
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		if serverctl.Healthy(context.Background(), port) {
			log("MyKids снова отвечает.")
			return
		}
		if stopped() {
			break
		}
		time.Sleep(2 * time.Second)
	}
	log("MyKids не ответил. Последние строки журнала:")
	for _, l := range tail(filepath.Join(p.o.L.Logs(), "service.log"), 10) {
		log(l)
	}
}

func (p *serverPanel) changePort(f win32.Form, log func(string)) {
	cfg, err := serverctl.LoadConfig(p.o.L.Config())
	if err != nil {
		log("Настройки: " + err.Error())
		return
	}
	port, err := strconv.Atoi(strings.TrimSpace(f.Value(fieldPort)))
	if err != nil {
		log("Порт — это число, например 3000.")
		return
	}
	if port == cfg.Port {
		log(fmt.Sprintf("MyKids уже работает на порту %d.", port))
		return
	}
	if err := serverctl.ValidPort(port); err != nil {
		log(err.Error())
		return
	}
	if l, err := net.Listen("tcp", ":"+strconv.Itoa(port)); err != nil {
		log(fmt.Sprintf("Порт %d занят другой программой — выберите другой.", port))
		return
	} else {
		l.Close()
	}
	if !win32.Confirm(panelTitle, fmt.Sprintf("Перевести MyKids на порт %d?\n\nНа компьютере ребёнка после этого нужно будет вписать новый адрес: «Панель MyKids» → «Привязать».", port)) {
		return
	}
	cfg.Port = port
	if err := serverctl.SaveConfig(p.o.L.Config(), cfg); err != nil {
		log("Не удалось сохранить: " + err.Error())
		return
	}
	if err := firewall("add", p.o); err != nil {
		log("Брандмауэр: " + err.Error())
	}
	updateAdminShortcut(port, filepath.Join(p.o.L.Home, "mykids.ico"))
	app.Clear()
	p.restartService(log)
	log("")
	for _, l := range addressLines(port) {
		log(l)
	}
}

func (p *serverPanel) copyReport(_ win32.Form, log func(string)) {
	if err := win32.CopyToClipboard(app.Report()); err != nil {
		log("не удалось скопировать: " + err.Error())
		return
	}
	log("отчёт скопирован — его можно переслать тому, кто помогает с настройкой")
}

func copyFile(from, to string) error {
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		os.Remove(to)
		return err
	}
	return dst.Close()
}

// updateAdminShortcut переписывает ярлык «Админка MyKids» в меню «Пуск» под
// новый порт: со старым он открывал бы пустую страницу.
func updateAdminShortcut(port int, icon string) {
	path := filepath.Join(os.Getenv("ProgramData"), `Microsoft\Windows\Start Menu\Programs\MyKids Сервер\Админка MyKids.url`)
	if _, err := os.Stat(path); err != nil {
		return
	}
	body := fmt.Sprintf("[InternetShortcut]\r\nURL=http://localhost:%d\r\nIconFile=%s\r\nIconIndex=0\r\n", port, icon)
	_ = os.WriteFile(path, []byte(body), 0o644)
}
