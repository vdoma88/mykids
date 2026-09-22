//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/vdoma88/mykids/agents/windows/internal/setup"
	"github.com/vdoma88/mykids/agents/windows/internal/win32"
)

var app *win32.App

// form — то, что установщику нужно от окна. Свой псевдоним, чтобы обёртка
// журнала не тянула за собой весь пакет win32 в своей сигнатуре.
type form = win32.Form

func run() error {
	app = win32.NewApp(
		"MyKids — установка "+version,
		intro(),
		[]win32.Button{
			{Text: "Установить", Do: alsoToFile(install)},
			{Text: "Скопировать отчёт", Do: func(_ win32.Form, log func(string)) {
				copyReport(log)
			}},
		},
	).WithFields(
		win32.Field{Label: setup.FieldServer},
		win32.Field{Label: setup.FieldToken, Secret: true},
		win32.Field{Label: setup.FieldChild},
	)
	return win32.RunApp(app)
}

func intro() string {
	lines := []string{
		"Заполните три поля и нажмите «Установить».",
		"",
		"  " + setup.FieldServer + " — тот, по которому вы открываете админку MyKids.",
		"  " + setup.FieldToken + " — кнопка «Выдать токен» на странице ребёнка. Показывается один раз.",
		"  " + setup.FieldChild + " — имя учётной записи, в которой работает ребёнок.",
		"",
	}
	lines = append(lines,
		"Всё, что здесь появится, пишется и в файл:",
		"  "+logPath(),
		"Если окно вдруг закроется само — отчёт останется там.",
		"")
	if !elevated() {
		lines = append(lines,
			"Сейчас программа запущена без прав администратора, и установить ничего не сможет:",
			"служба регистрируется на всю машину. Закройте окно и запустите её правой кнопкой →",
			"«Запуск от имени администратора».",
			"")
	}
	return join(lines)
}

// install — всё, что делает установщик. Идёт в отдельной горутине: окно не
// должно застывать, пока привязка ждёт ответа сервера.
func install(f form, log func(string)) {
	in := setup.Normalize(setup.Input{
		Server:    f.Value(setup.FieldServer),
		Token:     f.Value(setup.FieldToken),
		ChildUser: f.Value(setup.FieldChild),
	})

	log("")
	log("Проверяю введённое…")
	problems := setup.Check(in)
	for _, p := range problems {
		mark := "[ !! ]"
		if p.Severity == setup.Warn {
			mark = "[ ?? ]"
		}
		log(fmt.Sprintf("%s %s: %s", mark, p.Field, p.Detail))
		if p.Hint != "" {
			log("    " + p.Hint)
		}
	}
	if setup.Blocking(problems) {
		log("")
		log("Ничего не тронуто. Исправьте отмеченное и нажмите «Установить» ещё раз.")
		return
	}

	// Адрес, дописанный за родителя, называем вслух: молча исправленный адрес —
	// это адрес, которого он не вводил, и ошибку потом он будет искать не там.
	log("")
	log("Сервер:          " + in.Server)
	log("Задания ребёнка: " + in.ChildURL)
	log("")

	steps := setup.Steps()
	do := func(name string, fn func() error) bool {
		for _, s := range steps {
			if s.Name != name {
				continue
			}
			log("→ " + s.Name)
			if err := fn(); err != nil {
				log("")
				log("[ !! ] " + setup.Explain(s.Name, err))
				log("    зачем этот шаг: " + s.Why)
				return false
			}
			return true
		}
		return false
	}

	if !do("Права администратора", func() error {
		if !elevated() {
			return fmt.Errorf("программа запущена от обычного пользователя")
		}
		log("    есть")
		return nil
	}) {
		return
	}

	if !do("Учётная запись ребёнка", func() error { return checkChild(in.ChildUser, log) }) {
		return
	}

	var exe string
	if !do("Копирование агента", func() error {
		src, err := agentBeside()
		if err != nil {
			return err
		}
		exe, err = copyAgent(src)
		if err != nil {
			return err
		}
		log("    " + exe)
		return nil
	}) {
		return
	}

	data := dataDir()
	if !do("Каталог данных", func() error {
		if err := makeDataDir(data); err != nil {
			return err
		}
		log("    " + data + " — доступ только службе и администраторам")
		return nil
	}) {
		return
	}

	if !do("Привязка устройства", func() error {
		out, err := agentRun(exe, "-data", data, "enroll",
			"-server", in.Server, "-token", in.Token, "-child-url", in.ChildURL)
		if out != "" {
			log("    " + out)
		}
		return err
	}) {
		return
	}

	if !do("Регистрация службы", func() error {
		out, err := agentRun(exe, "-data", data, "service", "install")
		if out != "" {
			log("    " + out)
		}
		return err
	}) {
		return
	}

	if !do("Запуск службы", func() error {
		out, err := agentRun(exe, "-data", data, "service", "start")
		if out != "" {
			log("    " + out)
		}
		return err
	}) {
		return
	}

	log("")
	for _, line := range setup.Done(in) {
		log(line)
	}
}

// checkChild — единственное условие, без которого всё остальное бессмысленно.
func checkChild(name string, log func(string)) error {
	if name == "" {
		log("    имя не указано — проверить не могу, проверьте сами")
		return nil
	}
	isAdmin, known, err := childIsAdmin(name)
	switch {
	case err != nil:
		// Не сумев проверить, установку не срываем: невозможность проверить —
		// не то же самое, что провал проверки. Но и молчать не имеем права.
		log("    не удалось проверить: " + err.Error())
		log("    убедитесь сами, что учётная запись обычная, а не администратор")
		return nil
	case !known:
		log("    проверить не удалось — убедитесь сами")
		return nil
	case isAdmin:
		return fmt.Errorf("учётная запись «%s» состоит в администраторах", name)
	}
	log(fmt.Sprintf("    «%s» — обычная, без прав администратора: так и надо", name))
	return nil
}

// agentBeside ищет агент рядом с установщиком: их кладут в одну папку со
// страницы релиза, и просить родителя указать путь незачем.
func agentBeside() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	path := filepath.Join(filepath.Dir(self), agentName)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("рядом с установщиком нет %s", agentName)
	}
	return path, nil
}

func copyReport(log func(string)) {
	if err := win32.CopyToClipboard(app.Report()); err != nil {
		log("не удалось скопировать: " + err.Error())
		return
	}
	log("отчёт скопирован — его можно переслать тому, кто помогает с настройкой")
}

func join(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out
}
