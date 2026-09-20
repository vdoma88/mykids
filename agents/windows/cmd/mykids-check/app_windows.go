//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"

	"github.com/vdoma88/mykids/agents/windows/internal/screen"
	"github.com/vdoma88/mykids/agents/windows/internal/selftest"
	"github.com/vdoma88/mykids/agents/windows/internal/usage"
	"github.com/vdoma88/mykids/agents/windows/internal/win32"
	"github.com/vdoma88/mykids/agents/windows/internal/winsvc"
)

// probe — то же самое, что спрашивает у Windows агент. Ровно те же вызовы:
// проверка, которая спрашивает по-своему, проверяет себя, а не агента.
type probe struct{}

func (probe) ForegroundProcess() (string, error) { return win32.ForegroundProcess() }
func (probe) IdleTime() (time.Duration, error)   { return win32.IdleTime() }
func (probe) SessionLocked() bool                { return win32.SessionLocked() }

// Консольная сессия, а не ActiveSession: та спрашивает токен пользователя,
// на что нужна привилегия службы. Из-под ребёнка она всегда вернула бы ноль,
// и проверка ругалась бы на исправную машину.
func (probe) ActiveSession() uint32         { return win32.ConsoleSession() }
func (probe) ServiceState() (string, error) { return winsvc.Query() }

func (probe) Elevated() bool {
	token := windows.GetCurrentProcessToken()
	return token.IsElevated()
}

func intro() string {
	return `Это проверка MyKids. Она ничего не меняет на компьютере и ничего
никуда не отправляет — только смотрит, видит ли агент то, что ему нужно видеть.

Порядок такой:
  1. «Проверить всё» — уберите руки от мыши на несколько секунд.
  2. «Показать предупреждение» и «Закрыть экран» — посмотрите своими глазами.
  3. «Скопировать отчёт» — и пришлите его тем, кто настраивает.

Проверку лучше запускать из учётной записи ребёнка: важно то, что увидит он.

Всё, что появится ниже, одновременно пишется в файл:
` + logPath() + `
Если это окно вдруг закроется само — пришлите этот файл: в нём будет видно,
на чём всё оборвалось.`
}

// logPath — куда писать отчёт помимо окна.
//
// Окно можно закрыть случайно, а программу — уронить; файл остаётся. Для
// проверки, которую затем и запускают, что что-то не работает, это не
// перестраховка: именно её собственный сбой скрыть легче всего.
func logPath() string {
	dir := os.Getenv("TEMP")
	if dir == "" {
		dir = "."
	}
	return filepath.Join(dir, "mykids-check.log")
}

// toFile дописывает строку в файл отчёта. Ошибки записи глотаем: не мочь
// вести файл — не повод прекращать проверку, ради которой всё и затевалось.
func toFile(line string) {
	f, err := os.OpenFile(logPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintln(f, line)
}

func run() error {
	var app *win32.App
	app = win32.NewApp(
		"MyKids — проверка "+version,
		intro(),
		[]win32.Button{
			{Text: "Проверить всё", Do: alsoToFile(checkAll)},
			{Text: "Показать предупреждение", Do: alsoToFile(showWarning)},
			{Text: "Закрыть экран на 5 секунд", Do: alsoToFile(showBlock)},
			{Text: "Скопировать отчёт", Do: func(log func(string)) { copyReport(app, log) }},
		},
	)
	return win32.RunApp(app)
}

func fail(text string) { win32.Ask("MyKids", text) }

// alsoToFile дублирует всё, что проверка пишет в окно, в файл.
func alsoToFile(do func(func(string))) func(func(string)) {
	return func(log func(string)) {
		do(func(line string) {
			toFile(line)
			log(line)
		})
	}
}

func checkAll(log func(string)) {
	log("")
	log("── Проверка " + time.Now().Format("15:04:05") + " ───────────────────────")
	// По-русски: Duration печатает «3s», и в отчёте для родителя это выглядит
	// как кусок чужого лога.
	log("Уберите руки от мыши и клавиатуры на " + screen.Duration(selftest.RestQuiet) + ".")

	results := selftest.Run(probe{}, time.Sleep)
	for _, r := range results {
		log("")
		log(mark(r.Status) + " " + r.Name + ": " + r.Detail)
		log("    зачем: " + r.Why)
		if r.Hint != "" {
			log("    что делать: " + r.Hint)
		}
	}

	log("")
	switch selftest.Worst(results) {
	case selftest.Pass:
		log("Итог: всё работает. Осталось посмотреть глазами — кнопки рядом.")
	case selftest.Warn:
		log("Итог: работает, но есть замечания выше.")
	default:
		log("Итог: что-то не работает. Пришлите отчёт — кнопка «Скопировать отчёт».")
	}
}

// mark — значок вместо цвета: раскрасить текст в обычном поле нельзя, а
// отличать «ок» от «не работает» с одного взгляда нужно.
func mark(s selftest.Status) string {
	switch s {
	case selftest.Pass:
		return "[ ок ]"
	case selftest.Warn:
		return "[ ?? ]"
	default:
		return "[ !! ]"
	}
}

func showWarning(log func(string)) {
	// Тот же текст, что увидит ребёнок: показывать на проверке другое
	// значит проверять не то, что работает в бою.
	s := screen.Build(usage.Verdict{Allow: true, LeftSecs: 300, WarnSoon: true}, demoContext())
	log("")
	log("Показываю предупреждение на 5 секунд — внизу справа.")

	bar, err := win32.ShowBar(screen.Render(s))
	if err != nil {
		log("[ !! ] Полоса не появилась: " + err.Error())
		log("    что делать: без неё ребёнок теряет экран без предупреждения. Пришлите отчёт.")
		return
	}
	time.Sleep(5 * time.Second)
	bar.Close()

	if win32.Ask("Проверка", "Видели полосу с надписью «Осталось 5 минут» внизу справа?") {
		log("[ ок ] Предупреждение видно.")
		return
	}
	log("[ !! ] Полосы не было видно.")
	log("    что делать: окно создалось, но на экране не появилось. Пришлите отчёт.")
}

func showBlock(log func(string)) {
	s := screen.Build(usage.Verdict{}, demoContext())
	log("")
	log("Закрываю экран на 5 секунд. Он откроется сам.")

	over, err := win32.ShowOverlay(screen.Render(s))
	if err != nil {
		log("[ !! ] Экран закрыть не удалось: " + err.Error())
		log("    что делать: агент откатится на блокировку рабочего стола — это грубее. Пришлите отчёт.")
		return
	}
	time.Sleep(5 * time.Second)
	over.Close()

	if win32.Ask("Проверка", "Экран был закрыт, и текст на нём читался целиком?") {
		log("[ ок ] Закрытый экран работает.")
		return
	}
	log("[ !! ] С закрытым экраном что-то не так.")
	log("    что делать: напишите, что именно — не появился, мигал или текст не поместился.")
}

// demoContext — правдоподобные числа для показа. Настоящие лежат на сервере,
// а проверку запускают и до привязки к нему.
func demoContext() screen.Context {
	return screen.Context{
		Credits: 240, CreditsPerMinute: 2, TomorrowMinutes: 90,
		ChildURL: "http://адрес-сервера:3000/child",
	}
}

func copyReport(app *win32.App, log func(string)) {
	report := app.Report()
	if strings.TrimSpace(report) == "" {
		log("Отчёт пуст — сначала нажмите «Проверить всё».")
		return
	}
	header := fmt.Sprintf("MyKids %s, проверка от %s\n\n", version, time.Now().Format("2006-01-02 15:04"))
	if err := win32.CopyToClipboard(header + report); err != nil {
		log("Скопировать не вышло: " + err.Error())
		return
	}
	log("")
	log("Отчёт скопирован. Вставьте его в сообщение — Ctrl+V.")
}
