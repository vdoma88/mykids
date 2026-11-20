//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"

	"github.com/vdoma88/mykids/agents/windows/internal/config"
	"github.com/vdoma88/mykids/agents/windows/internal/setup"
	"github.com/vdoma88/mykids/agents/windows/internal/winsvc"
)

func dispatch(cmd string, a cliArgs, r *report) (int, error) {
	switch cmd {
	case "accounts":
		return cmdAccounts(r), nil
	case "status":
		return cmdStatus(r), nil
	case "verify":
		return cmdVerify(a, r), nil
	case "stop":
		return cmdStop(r), nil
	case "apply":
		return cmdApply(a, r), nil
	}
	r.add("неизвестная команда %q", cmd)
	return exitUsage, nil
}

func enrollmentPath() string { return filepath.Join(dataDir(), "enrollment.json") }

// bound — нынешняя привязка, если она есть.
func bound() (config.Enrollment, bool) {
	e, err := config.LoadEnrollment(enrollmentPath())
	if err != nil || !e.Configured() {
		return config.Enrollment{}, false
	}
	return e, true
}

// effective — что на самом деле ставить: введённое плюс нынешняя привязка.
func effective(a cliArgs) (setup.Input, bool) {
	e, ok := bound()
	if !ok {
		return setup.Resolve(a.in, nil)
	}
	return setup.Resolve(a.in, &setup.Binding{Server: e.ServerURL, Token: e.DeviceToken, ChildURL: e.ChildURL})
}

// cmdAccounts — обычные учётные записи для подсказки в мастере, по одной в
// строке. Не узнали ничего — пустой список: имя можно ввести и руками.
func cmdAccounts(r *report) int {
	all, err := localAccounts()
	if err != nil {
		toFile("учётные записи: " + err.Error())
		return exitOK
	}
	for _, name := range setup.Candidates(all) {
		r.add("%s", name)
	}
	return exitOK
}

// localAccounts — члены группы «Пользователи», с пометкой администраторов.
//
// Обычные учётные записи Windows создаёт членами этой группы — и локальные, и
// связанные с учётной записью Microsoft. Служебные (Гость, DefaultAccount) в
// неё не входят, и показывать их ребёнком было бы странно.
func localAccounts() ([]setup.Account, error) {
	users, err := windows.CreateWellKnownSid(windows.WinBuiltinUsersSid)
	if err != nil {
		return nil, err
	}
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return nil, err
	}
	members, err := groupMembers(users)
	if err != nil {
		return nil, err
	}
	adminMembers, err := groupMembers(admins)
	if err != nil {
		return nil, err
	}
	host, _ := windows.ComputerName()

	var out []setup.Account
	for _, m := range members {
		name, domain, typ, err := m.LookupAccount("")
		if err != nil || typ != windows.SidTypeUser {
			continue
		}
		// Чужой домен называем полностью: «Маша» с этой машины и «Маша» из
		// домена — разные учётные записи.
		if domain != "" && !strings.EqualFold(domain, host) {
			name = domain + `\` + name
		}
		admin := false
		for _, a := range adminMembers {
			if a.Equals(m) {
				admin = true
				break
			}
		}
		out = append(out, setup.Account{Name: name, Admin: admin})
	}
	return out, nil
}

// cmdStatus — нынешняя привязка: адрес сервера и адрес заданий.
// Токен не выводится никогда: отчёт лежит во временном каталоге.
func cmdStatus(r *report) int {
	e, ok := bound()
	if !ok {
		return exitNotBound
	}
	child := e.ChildURL
	if child == "" {
		child = e.ServerURL + "/child"
	}
	r.add("%s", e.ServerURL)
	r.add("%s", child)
	return exitOK
}

// cmdVerify — всё, что можно проверить, ничего не трогая.
func cmdVerify(a cliArgs, r *report) int {
	in, kept := effective(a)
	problems := setup.Check(in)
	if !setup.Blocking(problems) {
		problems = append(problems, childProblems(in.ChildUser)...)
	}

	var v setup.Verified
	if !setup.Blocking(problems) {
		hc := &http.Client{Timeout: setup.VerifyTimeout}
		var p *setup.Problem
		v, p = setup.VerifyToken(context.Background(), hc, in)
		if p != nil {
			if kept && p.Field == setup.FieldToken {
				// Отказали прежнему токену — значит, устройство отвязали в
				// админке. Сказать надо именно это, а не «токен неверный»:
				// родитель никакого токена сейчас не вводил.
				p.Detail = "сервер больше не принимает прежнюю привязку этого компьютера"
				p.Hint = "Устройство отвязано в админке. Выдайте новый токен на странице ребёнка и вставьте его в поле «Токен устройства»."
			}
			problems = append(problems, *p)
		}
	}

	if setup.Blocking(problems) {
		for _, p := range problems {
			if p.Severity == setup.Stop {
				r.add("%s", setup.Describe(p))
			}
		}
		return exitStop
	}

	if kept {
		r.add("Привязка сохраняется: это компьютер ребёнка «%s», сервер %s.", v.Child, in.Server)
	} else {
		r.add("Сервер %s принял токен: это будет компьютер ребёнка «%s».", in.Server, v.Child)
	}
	if in.ChildUser != "" {
		r.add("Учётная запись «%s» — обычная, без прав администратора: так и надо.", in.ChildUser)
	}
	for _, p := range problems {
		r.add("%s", setup.Describe(p))
	}
	return exitOK
}

// childProblems — права учётной записи ребёнка.
func childProblems(name string) []setup.Problem {
	if name == "" {
		return nil // о пустом имени уже сказал setup.Check
	}
	isAdmin, known, err := childIsAdmin(name)
	var missing noAccount
	switch {
	case errors.As(err, &missing):
		return []setup.Problem{{Field: setup.FieldChild, Severity: setup.Stop,
			Detail: err.Error(),
			Hint:   "Проверьте написание — это имя, под которым ребёнок входит в Windows. Если учётной записи ещё нет, создайте её: Параметры → Учётные записи → Семья и другие пользователи."}}
	case err != nil || !known:
		return []setup.Problem{{Field: setup.FieldChild, Severity: setup.Warn,
			Detail: "проверить права не удалось",
			Hint:   "Убедитесь сами, что учётная запись обычная, а не администратор."}}
	case isAdmin:
		return []setup.Problem{{Field: setup.FieldChild, Severity: setup.Stop,
			Detail: fmt.Sprintf("«%s» — администратор этого компьютера", name),
			Hint:   "Ребёнок-администратор остановит службу за минуту. Сделайте учётную запись обычной (Параметры → Учётные записи → Семья и другие пользователи → Изменить тип учётной записи) и повторите."}}
	}
	return nil
}

// cmdStop готовит машину к замене файлов: снимает службу и закрывает
// процессы агента.
//
// Служба снимается, а не просто останавливается: новая версия может лежать в
// другом каталоге, и регистрировать её надо заново, с новым путём.
//
// Помощник в сессии ребёнка переживает остановку службы — так и задумано: без
// службы он через минуту закрывает экран. Но он держит открытым файл агента,
// и новая версия встать на место старой не смогла бы. А при удалении он
// остался бы работать без службы и закрыл бы ребёнку экран до перезагрузки.
func cmdStop(r *report) int {
	if !elevated() {
		r.add("Нужны права администратора.")
		return exitStop
	}
	_, installed, err := winsvc.State()
	if err != nil {
		r.add("Служба MyKids: %v", err)
		return exitStop
	}
	if installed {
		if err := winsvc.Uninstall(); err != nil {
			r.add("Службу MyKids снять не удалось: %v", err)
			return exitStop
		}
		r.add("Служба MyKids остановлена и снята.")
	}
	if n := killAgents(); n > 0 {
		r.add("Закрыто процессов MyKids: %d.", n)
	}
	return exitOK
}

// killAgents завершает процессы агента и проверки, кроме себя.
func killAgents() int {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0
	}
	defer windows.CloseHandle(snap)

	self := windows.GetCurrentProcessId()
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	killed := 0
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		name := strings.ToLower(windows.UTF16ToString(e.ExeFile[:]))
		if e.ProcessID == self || (name != agentName && name != "mykids-check.exe") {
			continue
		}
		h, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, e.ProcessID)
		if err != nil {
			continue
		}
		if windows.TerminateProcess(h, 1) == nil {
			// Ждём: файл освобождается только с концом процесса.
			windows.WaitForSingleObject(h, 5000)
			killed++
		}
		windows.CloseHandle(h)
	}
	return killed
}

// cmdApply — привязка и служба поверх уже разложенных файлов.
func cmdApply(a cliArgs, r *report) int {
	if !elevated() {
		r.add("%s", setup.Explain("Права администратора", fmt.Errorf("программа запущена от обычного пользователя")))
		return exitStop
	}
	in, kept := effective(a)
	if problems := setup.Check(in); setup.Blocking(problems) {
		for _, p := range problems {
			r.add("%s", setup.Describe(p))
		}
		return exitStop
	}
	exe := filepath.Join(a.dir, agentName)
	if _, err := os.Stat(exe); err != nil {
		r.add("%s", setup.Explain("Копирование агента", fmt.Errorf("нет %s", exe)))
		return exitStop
	}

	fail := func(step string, err error) int {
		r.add("%s", setup.Explain(step, err))
		return exitStop
	}

	data := dataDir()
	if err := makeDataDir(data); err != nil {
		return fail("Каталог данных", err)
	}
	r.add("Каталог данных: %s — доступ только у службы и администраторов.", data)

	if _, err := agentRun(exe, "-data", data, "enroll",
		"-server", in.Server, "-token", in.Token, "-child-url", in.ChildURL); err != nil {
		return fail("Привязка устройства", err)
	}
	if kept {
		r.add("Привязка сохранена: %s.", in.Server)
	} else {
		r.add("Устройство привязано: %s.", in.Server)
	}

	// Служба могла остаться от прошлой установки, если мастер запустили не
	// по порядку. Зарегистрированная со старым путём, она запускала бы
	// старый файл — снимаем и ставим заново.
	if _, installed, _ := winsvc.State(); installed {
		if err := winsvc.Uninstall(); err != nil {
			return fail("Регистрация службы", err)
		}
	}
	if _, err := agentRun(exe, "-data", data, "service", "install"); err != nil {
		return fail("Регистрация службы", err)
	}
	if _, err := agentRun(exe, "-data", data, "service", "start"); err != nil {
		return fail("Запуск службы", err)
	}
	if !waitRunning(15 * time.Second) {
		return fail("Запуск службы", fmt.Errorf("служба не перешла в рабочее состояние за 15 секунд"))
	}
	r.add("Служба MyKids работает и запускается вместе с Windows.")
	return exitOK
}

func waitRunning(limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	for {
		state, installed, err := winsvc.State()
		if err == nil && installed && state == svc.Running {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(300 * time.Millisecond)
	}
}
