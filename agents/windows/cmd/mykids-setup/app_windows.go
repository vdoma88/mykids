//go:build windows

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"

	"github.com/vdoma88/mykids/agents/windows/internal/setup"
	"github.com/vdoma88/mykids/agents/windows/internal/win32"
	"github.com/vdoma88/mykids/agents/windows/internal/winsvc"
)

var app *win32.App

// form — то, что панели нужно от окна. Свой псевдоним, чтобы обёртка
// журнала не тянула за собой весь пакет win32 в своей сигнатуре.
type form = win32.Form

const panelTitle = "Панель MyKids"

// run — панель. Она управляет службой, а это делает только администратор:
// не запущенная от его имени, панель перезапускает себя сама — Windows
// спросит разрешение. Помнить про «правую кнопку → от имени администратора»
// родитель не обязан.
func run() error {
	if !win32.Elevated() {
		self, err := os.Executable()
		if err == nil {
			err = win32.RunElevated(self, "")
		}
		if err != nil {
			win32.Fail(panelTitle, "Панели нужны права администратора: она управляет службой MyKids.\n\nWindows не дала их: "+err.Error())
		}
		return nil
	}

	e, _ := bound()
	app = win32.NewApp(panelTitle+" "+version, "", []win32.Button{
		{Text: "Обновить", Do: refresh},
		{Text: "Привязать", Do: rebind},
		{Text: "Перезапустить службу", Do: alsoToFile(restart)},
		{Text: "Открыть админку", Do: openAdmin},
		{Text: "Скопировать отчёт", Do: func(_ win32.Form, log func(string)) { copyReport(log) }},
	}).WithFields(
		win32.Field{Label: setup.FieldServer, Value: e.ServerURL},
		win32.Field{Label: setup.FieldToken, Secret: true},
		win32.Field{Label: setup.FieldChild, Value: childAccount()},
	).WithStart(refresh).WithIcon(besideSelf("mykids.ico"))
	return win32.RunApp(app)
}

func besideSelf(name string) string {
	self, err := os.Executable()
	if err != nil {
		return name
	}
	return filepath.Join(filepath.Dir(self), name)
}

// childAccount — учётная запись ребёнка, которую запомнил установщик.
func childAccount() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`Software\Microsoft\Windows\CurrentVersion\Uninstall\MyKids`, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, _ := k.GetStringValue("ChildAccount")
	return v
}

// refresh — состояние словами. Ничего не меняет: только смотрит.
func refresh(_ form, log func(string)) {
	app.Clear()
	log("Состояние на " + time.Now().Format("15:04:05"))
	log("")

	state, installed, err := winsvc.State()
	switch {
	case err != nil:
		log("Служба: не удалось узнать — " + err.Error())
	case !installed:
		log("Служба: НЕ УСТАНОВЛЕНА — время не считается.")
		log("    Запустите установщик MyKids-Setup ещё раз.")
	case state == svc.Running:
		log("Служба: работает и запускается вместе с Windows.")
	default:
		log("Служба: " + winsvc.StateName(state) + " — пока она не работает, время не считается.")
		log("    Нажмите «Перезапустить службу».")
	}

	e, ok := bound()
	if !ok {
		log("Привязка: компьютер не привязан к серверу — учёт никуда не уходит.")
		log("    Впишите адрес сервера и токен выше и нажмите «Привязать».")
		log("")
		log("Отчёт пишется и в " + logPath())
		return
	}
	child := e.ChildURL
	if child == "" {
		child = e.ServerURL + "/child"
	}
	log("Сервер: " + e.ServerURL)
	log("Задания ребёнка: " + child)

	in := setup.Input{Server: e.ServerURL, Token: e.DeviceToken}
	v, p := setup.VerifyToken(context.Background(), &http.Client{Timeout: setup.VerifyTimeout}, in)
	if p != nil {
		log("")
		log("Связь с сервером: НЕТ")
		log(setup.Describe(*p))
	} else {
		log("Связь с сервером: есть, это компьютер ребёнка «" + v.Child + "».")
		log(fmt.Sprintf("Сейчас у ребёнка: %d мин экранного времени, %d кредитов.", v.Minutes, v.Credits))
	}
	if name := childAccount(); name != "" {
		log("Учётная запись ребёнка в Windows: " + name)
	}
	log("")
	log("Что видит сам ребёнок, покажет «Проверка MyKids» — её запускают из его учётной записи.")
	log("Отчёт пишется и в " + logPath())
}

// rebind — привязать заново: новый токен, новый адрес сервера или другая
// учётная запись. Те же проверки и те же шаги, что в мастере установки.
func rebind(f form, log func(string)) {
	a := cliArgs{dir: filepath.Dir(besideSelf(agentName))}
	a.in = setup.Input{
		Server:    f.Value(setup.FieldServer),
		Token:     f.Value(setup.FieldToken),
		ChildUser: f.Value(setup.FieldChild),
	}

	app.Clear()
	log("Проверяю…")
	r := &report{echo: log}
	if cmdVerify(a, r) != exitOK {
		log("")
		log("Ничего не тронуто. Исправьте отмеченное и нажмите «Привязать» ещё раз.")
		return
	}
	if !win32.Confirm(panelTitle, strings.Join(r.lines, "\n")+"\n\nПривязать этот компьютер? Служба перезапустится.") {
		log("")
		log("Отменено — ничего не тронуто.")
		return
	}

	log("")
	r = &report{echo: log}
	if cmdApply(a, r) != exitOK {
		return
	}
	saveChildAccount(a.in.ChildUser)
	log("")
	log("Готово.")
}

func saveChildAccount(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`Software\Microsoft\Windows\CurrentVersion\Uninstall\MyKids`, registry.SET_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return
	}
	defer k.Close()
	_ = k.SetStringValue("ChildAccount", name)
}

func restart(_ form, log func(string)) {
	app.Clear()
	exe := besideSelf(agentName)
	log("Останавливаю службу…")
	if _, err := agentRun(exe, "-data", dataDir(), "service", "stop"); err != nil {
		log("    " + err.Error())
	}
	log("Запускаю…")
	if _, err := agentRun(exe, "-data", dataDir(), "service", "start"); err != nil {
		log(setup.Explain("Запуск службы", err))
		return
	}
	if !waitRunning(15 * time.Second) {
		log(setup.Explain("Запуск службы", fmt.Errorf("служба не перешла в рабочее состояние за 15 секунд")))
		return
	}
	log("Служба работает.")
}

func openAdmin(f form, log func(string)) {
	server := setup.Normalize(setup.Input{Server: f.Value(setup.FieldServer)}).Server
	if server == "" {
		log("Адрес сервера не указан.")
		return
	}
	if err := win32.Open(server); err != nil {
		log("Не удалось открыть браузер: " + err.Error())
	}
}

func copyReport(log func(string)) {
	if err := win32.CopyToClipboard(app.Report()); err != nil {
		log("не удалось скопировать: " + err.Error())
		return
	}
	log("отчёт скопирован — его можно переслать тому, кто помогает с настройкой")
}
