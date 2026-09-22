package setup

import (
	"errors"
	"strings"
	"testing"
)

func good() Input {
	return Input{
		Server:    "http://192.168.1.10:3000",
		Token:     "abc123",
		ChildUser: "Марк",
	}
}

func find(ps []Problem, field string) (Problem, bool) {
	for _, p := range ps {
		if p.Field == field {
			return p, true
		}
	}
	return Problem{}, false
}

func TestGoodInputPassesClean(t *testing.T) {
	if ps := Check(Normalize(good())); len(ps) != 0 {
		t.Fatalf("на исправном вводе придрались: %+v", ps)
	}
}

func TestLocalhostIsCaught(t *testing.T) {
	// Главная ловушка, и совершенно незаметная: родитель поднимает сервер у
	// себя, открывает его как localhost и переносит этот адрес сюда. Здесь
	// localhost — сам компьютер ребёнка. Привязка упала бы на «соединение
	// отклонено», и винил бы родитель токен.
	for _, host := range []string{
		"http://localhost:3000", "http://127.0.0.1:3000",
		"https://LOCALHOST", "http://127.5.5.5:3000", "http://[::1]:3000",
	} {
		in := good()
		in.Server = host
		p, ok := find(Check(Normalize(in)), FieldServer)
		if !ok || p.Severity != Stop {
			t.Fatalf("%s принят как адрес сервера", host)
		}
		if !strings.Contains(p.Hint, "192.168") {
			t.Fatalf("родителю не показали, как выглядит правильный адрес: %q", p.Hint)
		}
	}
}

func TestRealAddressesAreNotMistakenForLoopback(t *testing.T) {
	// Проверка на localhost не должна ловить нормальные адреса. «127» внутри
	// имени или октета — не петля.
	for _, host := range []string{
		"http://192.168.1.10:3000", "http://10.0.0.5", "http://дом:3000",
		"https://mykids.example.com", "http://192.168.127.10:3000",
	} {
		in := good()
		in.Server = host
		if p, ok := find(Check(Normalize(in)), FieldServer); ok {
			t.Fatalf("%s забракован: %s", host, p.Detail)
		}
	}
}

func TestSchemeIsAddedNotDemanded(t *testing.T) {
	// «дом:3000» — это то, что родитель видит в адресной строке. Требовать от
	// него «http://» значит требовать знания не по теме.
	in := good()
	in.Server = "дом:3000"
	got := Normalize(in)
	if got.Server != "http://дом:3000" {
		t.Fatalf("схема не подставлена: %q", got.Server)
	}
	if ps := Check(got); len(ps) != 0 {
		t.Fatalf("после подстановки всё равно придрались: %+v", ps)
	}
}

func TestChildURLDefaultsToTheSameServer(t *testing.T) {
	in := Normalize(good())
	if in.ChildURL != "http://192.168.1.10:3000/child" {
		t.Fatalf("адрес заданий не выведен из адреса сервера: %q", in.ChildURL)
	}
	// А указанный явно — не трогаем.
	other := good()
	other.ChildURL = "http://дом:8080/child"
	if got := Normalize(other).ChildURL; got != "http://дом:8080/child" {
		t.Fatalf("явный адрес заданий перезаписан: %q", got)
	}
}

func TestTrailingSlashDoesNotDoubleUp(t *testing.T) {
	in := good()
	in.Server = "http://дом:3000/"
	if got := Normalize(in).ChildURL; strings.Contains(got, "//child") {
		t.Fatalf("две косые в адресе заданий: %q", got)
	}
}

func TestPastedTokenWithSpacesIsCaught(t *testing.T) {
	// Токен копируют мышью, и захватить лишнее легко. Пробелы по краям
	// срезаем молча, а внутри — это уже не токен.
	in := good()
	in.Token = "  abc123  "
	if ps := Check(Normalize(in)); len(ps) != 0 {
		t.Fatalf("пробелы по краям не срезаны: %+v", ps)
	}

	in.Token = "abc 123"
	p, ok := find(Check(Normalize(in)), FieldToken)
	if !ok || p.Severity != Stop {
		t.Fatal("токен с пробелом внутри принят")
	}
}

func TestMissingChildAccountWarnsButDoesNotBlock(t *testing.T) {
	// Проверить учётную запись — не то же самое, что установить агент.
	// Запрещать установку из-за непроверенного условия было бы подменой,
	// а молчать нельзя: без обычной учётной записи всё остальное бессмысленно.
	in := good()
	in.ChildUser = ""
	ps := Check(Normalize(in))
	p, ok := find(ps, FieldChild)
	if !ok {
		t.Fatal("о непроверенной учётной записи промолчали")
	}
	if p.Severity != Warn {
		t.Fatal("отсутствие имени учётной записи запретило установку")
	}
	if Blocking(ps) {
		t.Fatal("установка заблокирована предупреждением")
	}
	if !strings.Contains(p.Hint, "администратор") {
		t.Fatalf("не сказано, что именно проверить самому: %q", p.Hint)
	}
}

func TestEveryStopTellsWhatToDo(t *testing.T) {
	// Строка, с которой нечего делать, оставляет родителя там же, где он был.
	inputs := []Input{
		{},
		{Server: "ftp://дом", Token: "t", ChildUser: "М"},
		{Server: "http://localhost", Token: "t", ChildUser: "М"},
		{Server: "http://дом:3000", Token: "a b", ChildUser: "М"},
		{Server: "http://:::", Token: "t", ChildUser: "М"},
	}
	for _, in := range inputs {
		for _, p := range Check(Normalize(in)) {
			if p.Severity != Stop {
				continue
			}
			if p.Hint == "" {
				t.Fatalf("у запрета нет следующего шага: %+v", p)
			}
			if p.Field == "" {
				t.Fatalf("не сказано, какое поле править: %+v", p)
			}
		}
	}
}

func TestEnrollHappensBeforeTheServiceIsRegistered(t *testing.T) {
	// Наоборот было бы естественнее, но неверный токен оставил бы на машине
	// службу, которая работает автономно и ничего не шлёт. Выглядит как
	// «установилось», а обнаружится через день.
	var enroll, register int = -1, -1
	for i, s := range Steps() {
		switch s.Name {
		case "Привязка устройства":
			enroll = i
		case "Регистрация службы":
			register = i
		}
	}
	if enroll < 0 || register < 0 {
		t.Fatal("в плане нет привязки или регистрации службы")
	}
	if enroll > register {
		t.Fatal("служба регистрируется раньше привязки: неверный токен оставит за собой рабочую службу без связи")
	}
}

func TestAccountIsCheckedBeforeAnythingIsWritten(t *testing.T) {
	// Останавливаться на непригодной учётной записи после того, как половина
	// разложена по дискам, значит оставить за собой мусор.
	order := map[string]int{}
	for i, s := range Steps() {
		order[s.Name] = i
	}
	for _, writes := range []string{"Копирование агента", "Каталог данных"} {
		if order["Учётная запись ребёнка"] > order[writes] {
			t.Fatalf("«%s» идёт раньше проверки учётной записи", writes)
		}
	}
}

func TestEveryStepSaysWhyItExists(t *testing.T) {
	// Отчёт пересылают тому, кто настраивает; шаг без объяснения ему ни о чём
	// не говорит.
	for _, s := range Steps() {
		if s.Why == "" {
			t.Fatalf("шаг «%s» не объясняет себя", s.Name)
		}
	}
}

func TestEveryFailureHasANextStep(t *testing.T) {
	for _, s := range Steps() {
		got := Explain(s.Name, errors.New("отказано в доступе"))
		if !strings.Contains(got, "что делать:") {
			t.Fatalf("неудача шага «%s» не говорит, что делать: %q", s.Name, got)
		}
		if !strings.Contains(got, "отказано в доступе") {
			t.Fatalf("в сообщении потеряна причина: %q", got)
		}
	}
}

func TestDoneNamesTheAddressToOpen(t *testing.T) {
	// «Готово» само по себе оставляет родителя гадать, готово ли.
	lines := strings.Join(Done(Normalize(good())), "\n")
	if !strings.Contains(lines, "http://192.168.1.10:3000/child") {
		t.Fatalf("не сказано, что открыть: %q", lines)
	}
	if !strings.Contains(lines, "mykids-check") {
		t.Fatalf("не предложено проверить установку: %q", lines)
	}
}
