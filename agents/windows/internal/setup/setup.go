// Package setup — решения об установке агента.
//
// Всё, что установщик делает с машиной, требует Windows и проверено быть не
// может. Всё, что он решает, — что считать неверным вводом, в каком порядке
// действовать и что сказать, когда шаг не удался, — лежит здесь и проверяется
// на любой ОС.
//
// Отдельным пакетом, потому что цена ошибки здесь выше обычной: установку
// делают один раз, в чужой учётной записи, и неверно понятая ошибка означает
// не «попробую ещё раз», а «эта программа не работает».
package setup

import (
	"fmt"
	"net/url"
	"strings"
)

// Input — что ввёл родитель.
type Input struct {
	Server    string
	Token     string
	ChildUser string
	// ChildURL — адрес заданий и магазина. Пусто означает «тот же сервер»:
	// страницы отдаёт он сам.
	ChildURL string
}

// Severity — можно ли продолжать.
type Severity int

const (
	// Stop — установка бессмысленна, пока это не исправлено.
	Stop Severity = iota
	// Warn — продолжить можно, но родитель должен об этом знать.
	Warn
)

// Problem — что не так с введённым, до того как что-то тронули.
type Problem struct {
	// Field — подпись поля, чтобы родитель понял, где искать.
	Field    string
	Severity Severity
	Detail   string
	// Hint — что сделать. У Stop обязательна: строка, с которой нечего
	// делать, оставляет родителя там же, где он был.
	Hint string
}

// Подписи полей. Строками, а не числами: поля переставляют, и привязка по
// номеру разъезжается молча.
const (
	FieldServer = "Адрес сервера"
	FieldToken  = "Токен устройства"
	FieldChild  = "Учётная запись ребёнка"
)

// Normalize приводит введённое к тому виду, в котором им можно пользоваться.
//
// Подставленное здесь установщик обязан назвать вслух: молча исправленный
// адрес — это адрес, которого родитель не вводил, и искать потом ошибку он
// будет не там.
func Normalize(in Input) Input {
	in.Server = strings.TrimSpace(in.Server)
	in.Token = strings.TrimSpace(in.Token)
	in.ChildUser = strings.TrimSpace(in.ChildUser)
	in.ChildURL = strings.TrimSpace(in.ChildURL)

	// Схему дописываем: «дом:3000» — это то, что родитель видит в адресной
	// строке, и требовать от него «http://» значит требовать знания, которое
	// к воспитанию детей отношения не имеет.
	if in.Server != "" && !strings.Contains(in.Server, "://") {
		in.Server = "http://" + in.Server
	}
	in.Server = strings.TrimRight(in.Server, "/")

	if in.ChildURL == "" && in.Server != "" {
		in.ChildURL = in.Server + "/child"
	}
	return in
}

// Check ищет то, что видно до установки.
//
// Порядок важен: сначала то, из-за чего установка бессмысленна, потом то,
// о чём надо знать. Родитель читает сверху вниз и останавливается на первом.
func Check(in Input) []Problem {
	var ps []Problem

	switch {
	case in.Server == "":
		ps = append(ps, Problem{FieldServer, Stop,
			"адрес не указан",
			"Это адрес, по которому вы открываете админку MyKids, например http://192.168.1.10:3000"})
	default:
		ps = append(ps, checkServer(in.Server)...)
	}

	switch {
	case in.Token == "":
		ps = append(ps, Problem{FieldToken, Stop,
			"токен не указан",
			"Откройте страницу ребёнка в админке и нажмите «Выдать токен». Он показывается один раз."})
	case strings.ContainsAny(in.Token, " \t\n"):
		ps = append(ps, Problem{FieldToken, Stop,
			"в токене есть пробелы",
			"Похоже, при копировании захватился лишний текст. Скопируйте токен целиком и без пробелов."})
	}

	if in.ChildUser == "" {
		// Не Stop: проверить учётную запись — не то же самое, что установить
		// агент, и запрещать установку из-за непроверенного условия было бы
		// подменой. Но и промолчать нельзя: без обычной учётной записи вся
		// остальная установка бессмысленна.
		ps = append(ps, Problem{FieldChild, Warn,
			"учётная запись не названа — проверить её права не могу",
			"Убедитесь сами, что ребёнок работает в обычной учётной записи, а не в учётной записи администратора."})
	}
	return ps
}

// checkServer — ловушки в адресе.
func checkServer(raw string) []Problem {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return []Problem{{FieldServer, Stop,
			"адрес не разобрать",
			"Ожидается что-то вроде http://192.168.1.10:3000 — так, как вы открываете админку."}}
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return []Problem{{FieldServer, Stop,
			fmt.Sprintf("«%s» — не адрес сайта", u.Scheme),
			"Ожидается http:// или https://."}}
	}

	// Главная ловушка, и совершенно незаметная. Родитель поднимает сервер у
	// себя, открывает его как http://localhost:3000 и переносит этот адрес
	// сюда. Но здесь, на компьютере ребёнка, localhost — это сам компьютер
	// ребёнка, где сервера нет. Привязка упадёт на «соединение отклонено», и
	// винить родитель будет токен.
	if host := strings.ToLower(hostOnly(u.Host)); isLoopback(host) {
		return []Problem{{FieldServer, Stop,
			fmt.Sprintf("«%s» — это сам компьютер ребёнка, а не ваш сервер", host),
			"На вашей машине этот адрес работает, здесь — нет. Возьмите адрес сервера в домашней сети: например http://192.168.1.10:3000."}}
	}
	return nil
}

func hostOnly(host string) string {
	if i := strings.LastIndex(host, ":"); i > 0 && !strings.Contains(host[i:], "]") {
		return strings.Trim(host[:i], "[]")
	}
	return strings.Trim(host, "[]")
}

func isLoopback(host string) bool {
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return strings.HasPrefix(host, "127.")
}

// Worst — можно ли устанавливать.
func Worst(ps []Problem) Severity {
	for _, p := range ps {
		if p.Severity == Stop {
			return Stop
		}
	}
	return Warn
}

// Blocking — есть ли среди найденного то, из-за чего установка бессмысленна.
func Blocking(ps []Problem) bool {
	for _, p := range ps {
		if p.Severity == Stop {
			return true
		}
	}
	return false
}
