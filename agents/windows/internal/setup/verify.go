package setup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
)

// Проверка токена до установки.
//
// Раньше привязка только записывала токен в файл и ни о чём сервер не
// спрашивала. Опечатка в адресе, отозванный или чужой токен — установщик
// говорил «готово», служба работала автономно, а родитель ждал данных в
// админке и через день начинал искать ошибку там, где её нет. Сервер может
// ответить на всё это сразу, до того как на машине что-то тронуто, — значит,
// спросить надо его.

// VerifyTimeout — сколько ждать сервер. Он стоит в той же квартире: если за
// десять секунд не ответил, он не ответит и через минуту, а мастер установки
// всё это время висит.
const VerifyTimeout = 10 * time.Second

// Verified — что сервер сказал о токене.
type Verified struct {
	// Child — имя ребёнка, к которому привязан токен. Установщик его
	// показывает: токен, выданный не тому ребёнку, иначе не отличить.
	Child string
}

// VerifyToken спрашивает сервер, чьё это устройство.
//
// Ответ сервера — единственный способ узнать, что адрес верный и токен жив.
// /child/me отвечает по тому же токену устройства, что и агент, и из данных
// меняет только отметку «устройство было на связи» — в админке оно появится
// в сети сразу после установки, и это правда.
func VerifyToken(ctx context.Context, hc *http.Client, in Input) (Verified, *Problem) {
	ctx, cancel := context.WithTimeout(ctx, VerifyTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, in.Server+"/child/me", nil)
	if err != nil {
		return Verified{}, &Problem{FieldServer, Stop, "адрес не разобрать: " + err.Error(),
			"Ожидается что-то вроде http://192.168.1.10:3000 — так, как вы открываете админку."}
	}
	req.Header.Set("authorization", "Bearer "+in.Token)
	req.Header.Set("accept", "application/json")

	res, err := hc.Do(req)
	if err != nil {
		return Verified{}, unreachable(in.Server, err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))

	switch {
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		return Verified{}, &Problem{FieldToken, Stop,
			"сервер не принял токен",
			"Токен отозван, уже заменён новым или скопирован не целиком. Выдайте новый на странице ребёнка в админке и вставьте его сюда."}
	case res.StatusCode >= 500:
		return Verified{}, &Problem{FieldServer, Stop,
			fmt.Sprintf("сервер ответил ошибкой %d", res.StatusCode),
			"Сервер запущен, но не готов. Подождите минуту после включения или загляните в его журнал: docker compose -f deploy/docker-compose.yml logs api"}
	case res.StatusCode != http.StatusOK:
		return Verified{}, notMyKids(in.Server, fmt.Sprintf("на запрос ответили кодом %d", res.StatusCode))
	}

	var me struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &me); err != nil || strings.TrimSpace(me.Name) == "" {
		return Verified{}, notMyKids(in.Server, "ответ не похож на ответ MyKids")
	}
	return Verified{Child: strings.TrimSpace(me.Name)}, nil
}

// unreachable — сервер не ответил вовсе. Причина почти всегда одна из трёх, и
// назвать их надо все: родитель не знает, которая его.
func unreachable(server string, err error) *Problem {
	detail := "сервер не отвечает"
	var dnsErr *net.DNSError
	switch {
	case errors.As(err, &dnsErr):
		detail = fmt.Sprintf("имя «%s» в сети не находится", dnsErr.Name)
	case errors.Is(err, context.DeadlineExceeded):
		detail = fmt.Sprintf("сервер не ответил за %d секунд", int(VerifyTimeout.Seconds()))
	case strings.Contains(err.Error(), "refused"):
		detail = "по этому адресу ничего не слушает: соединение отклонено"
	case strings.Contains(err.Error(), "certificate"):
		detail = "сертификат сервера не принят"
	}
	return &Problem{FieldServer, Stop, detail,
		"Откройте " + server + " в браузере прямо на этом компьютере. Не открывается — сервер выключен, адрес неверный или компьютеры в разных сетях. Открывается в браузере, но не здесь — мешает брандмауэр или антивирус."}
}

func notMyKids(server, why string) *Problem {
	return &Problem{FieldServer, Stop,
		"по этому адресу отвечает не сервер MyKids: " + why,
		"Нужен адрес, по которому вы открываете админку MyKids, — например http://192.168.1.10:3000. Проверьте порт: " + server}
}

// Account — учётная запись на компьютере ребёнка.
type Account struct {
	Name  string
	Admin bool
}

// Candidates — кого предложить в поле «учётная запись ребёнка».
//
// Только обычные учётные записи: администратор в этом списке — подсказка
// сделать ровно то, после чего агент бесполезен. Ввести имя руками можно
// по-прежнему, и тогда проверка скажет, что не так.
func Candidates(all []Account) []string {
	seen := map[string]bool{}
	var out []string
	for _, a := range all {
		name := strings.TrimSpace(a.Name)
		key := strings.ToLower(name)
		if a.Admin || name == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, name)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

// Describe — проблема одной строкой для окна мастера.
func Describe(p Problem) string {
	mark := "Ошибка"
	if p.Severity == Warn {
		mark = "Внимание"
	}
	line := fmt.Sprintf("%s — %s: %s.", mark, p.Field, p.Detail)
	if p.Hint != "" {
		line += "\n" + p.Hint
	}
	return line
}

// UTF16 — текст отчёта для мастера установки.
//
// Отчёт мастер читает из файла, а не из вывода программы: вывод он принял бы
// в кодировке ANSI, и всё русское, включая имя ребёнка, превратилось бы в
// кракозябры. UTF-16LE — то, что он читает без перекодировки. Без BOM: строку
// с ним мастер показал бы с невидимым символом в начале.
func UTF16(lines []string) []byte {
	var out []byte
	for _, line := range lines {
		for _, part := range strings.Split(line, "\n") {
			for _, u := range utf16.Encode([]rune(part + "\r\n")) {
				out = append(out, byte(u), byte(u>>8))
			}
		}
	}
	return out
}
