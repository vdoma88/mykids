// Команда mykids-agent — агент контроля экранного времени для Windows.
//
//	mykids-agent status   один замер: что агент видит прямо сейчас
//	mykids-agent watch    наблюдение без блокировки
//	mykids-agent run      наблюдение с блокировкой экрана
//	mykids-agent enroll   привязать устройство к семье
//
// Подкоманды разделены намеренно: сначала убедиться, что учёт видит нужное,
// и только потом отдавать ему право закрывать экран.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	_ "time/tzdata" // база часовых поясов внутрь бинарника: в Windows своей нет

	"github.com/vdoma88/mykids/agents/windows/internal/agent"
	"github.com/vdoma88/mykids/agents/windows/internal/client"
	"github.com/vdoma88/mykids/agents/windows/internal/clock"
	"github.com/vdoma88/mykids/agents/windows/internal/config"
	"github.com/vdoma88/mykids/agents/windows/internal/ipc"
	"github.com/vdoma88/mykids/agents/windows/internal/link"
	"github.com/vdoma88/mykids/agents/windows/internal/outbox"
	"github.com/vdoma88/mykids/agents/windows/internal/policy"
	"github.com/vdoma88/mykids/agents/windows/internal/screen"
	"github.com/vdoma88/mykids/agents/windows/internal/state"
)

// version — версия агента.
//
// Не константа намеренно: релизная сборка подменяет её через
// -ldflags "-X main.version=...", а линковщик умеет это только с переменными и
// на константу не действует — молча, без единого предупреждения.
var version = "0.3.0"

// syncEvery — как часто агент ходит на сервер. Реже, чем опрашивает рабочий
// стол: расписание меняется редко, а расход всё равно копится в очереди.
const syncEvery = time.Minute

func main() {
	dataDir := flag.String("data", defaultDataDir(), "каталог с политикой и состоянием")
	interval := flag.Duration("interval", 5*time.Second, "период опроса рабочего стола")
	server := flag.String("server", "", "адрес сервера семьи (для enroll)")
	token := flag.String("token", "", "токен устройства (для enroll)")
	childURL := flag.String("child-url", "", "адрес заданий и магазина для ребёнка (для enroll)")
	pipe := flag.String("pipe", ipc.DefaultAddr, "канал между службой и помощником")
	flag.Usage = printUsage
	flag.Parse()

	cmd := flag.Arg(0)
	if cmd == "" {
		printUsage()
		os.Exit(2)
	}

	// Флаги разбираем ещё раз после имени команды: пакет flag останавливается
	// на первом аргументе, не начинающемся с дефиса, а справка обещает
	// «enroll -server ... -token ...» — именно так их и напишут.
	if rest := flag.Args()[1:]; len(rest) > 0 {
		if err := flag.CommandLine.Parse(rest); err != nil {
			os.Exit(2)
		}
	}

	opts := options{
		dataDir: *dataDir, interval: *interval,
		server: *server, token: *token, childURL: *childURL, pipe: *pipe,
		sub: flag.Arg(1),
	}
	if err := run(cmd, opts); err != nil {
		fmt.Fprintf(os.Stderr, "ошибка: %v\n", err)
		os.Exit(1)
	}
}

type options struct {
	dataDir  string
	interval time.Duration
	server   string
	token    string
	childURL string
	pipe     string
	// sub — вторая часть команды, например install у service.
	sub string
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `mykids-agent %s — учёт и ограничение экранного времени

Использование:
  mykids-agent [флаги] <команда>

Команды:
  status    один замер: активное окно, простой, остаток времени, связь
  watch     наблюдение и учёт без блокировки экрана
  run       наблюдение с блокировкой экрана в одном процессе
  enroll    привязать устройство: -server <адрес> -token <токен>
            [-child-url <адрес>] — что написать ребёнку на закрытом экране
  version   версия

Боевой режим — два процесса:
  service install    зарегистрировать службу (нужны права администратора)
  service uninstall  убрать службу
  service start      запустить
  service stop       остановить
  service status     состояние службы
  serve              тело службы; из консоли — для отладки
  helper             наблюдатель в сессии пользователя, рисует оверлей

Служба живёт в нулевой сессии и рабочего стола не видит: наблюдает помощник,
решает служба. Остановить её ребёнок без прав администратора не может.

Флаги:
`, version)
	flag.PrintDefaults()
}

func defaultDataDir() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "MyKids")
	}
	return "."
}

// paths — где лежат файлы агента.
type paths struct {
	policy     string
	state      string
	enrollment string
	cache      string
	outbox     string
}

func pathsIn(dataDir string) paths {
	return paths{
		policy:     filepath.Join(dataDir, "policy.json"),
		state:      filepath.Join(dataDir, "state.json"),
		enrollment: filepath.Join(dataDir, "enrollment.json"),
		// Кэш серверной политики намеренно отделён от policy.json: тот файл
		// ребёнок может править, а этот агент перезаписывает сам и с него же
		// работает, пока связи нет.
		cache:  filepath.Join(dataDir, "policy-cache.json"),
		outbox: filepath.Join(dataDir, "outbox.jsonl"),
	}
}

func run(cmd string, o options) error {
	if cmd == "version" {
		fmt.Println(version)
		return nil
	}

	if err := os.MkdirAll(o.dataDir, 0o755); err != nil {
		return fmt.Errorf("каталог данных %s: %w", o.dataDir, err)
	}
	p := pathsIn(o.dataDir)

	if cmd == "enroll" {
		return enroll(p.enrollment, o.server, o.token, o.childURL)
	}
	if cmd == "service" {
		return serviceCommand(o.sub)
	}
	if cmd == "helper" {
		return runHelper(o)
	}

	localPolicy, err := config.Load(p.policy)
	if err != nil {
		return err
	}
	enrollment, err := config.LoadEnrollment(p.enrollment)
	if err != nil {
		return err
	}

	st, err := state.Load(p.state)
	if err != nil {
		// Битое состояние не повод не работать: начинаем день заново и говорим об этом.
		fmt.Fprintf(os.Stderr, "предупреждение: %v — состояние сброшено\n", err)
		st = state.State{}
	}
	// Нештатную остановку разбирает уже сам агент: ему нужна политика, чтобы
	// знать дневную выдачу, от которой считается ограничение списания.

	box, err := outbox.Open(p.outbox)
	if err != nil {
		return fmt.Errorf("очередь расхода: %w", err)
	}

	clk := clock.New(0)
	clk.SetOffset(time.Duration(st.ClockOffsetSeconds)*time.Second, st.ClockTrusted)
	lnk := link.New(client.New(enrollment.ServerURL, enrollment.DeviceToken, version), box, clk, p.cache)
	lnk.SetPending(st.PendingSeconds)
	lnk.SetTampers(st.PendingTampers)

	// Политика действующая, а не локальная: кэш серверной выигрывает у файла,
	// который ребёнку доступен на запись.
	resolved := policy.Resolve(localPolicy, p.cache)

	// Откуда агент узнаёт о рабочем столе, зависит от команды: под службой это
	// помощник по каналу, в однопроцессных режимах — win32 напрямую.
	var desktop agent.Desktop = newDesktop()
	var remote *ipc.Desktop
	if cmd == "serve" {
		remote = ipc.NewDesktop(0, time.Now)
		desktop = remote
	}

	// Оверлей рисует тот, кто видит рабочий стол. У службы его нет.
	var enforcer agent.Enforcer
	if cmd == "run" {
		enforcer = newEnforcer()
	}

	a, err := agent.New(resolved.Policy, desktop, enforcer, st)
	if err != nil {
		return err
	}

	source := clock.System()

	switch cmd {
	case "status":
		return status(a, lnk, clk, source, localPolicy, enrollment, p)
	case "watch", "run":
		return loop(a, lnk, clk, source, localPolicy, p, o.interval, cmd == "run", enrollment.ChildURL)
	case "serve":
		return serve(assembled{
			agent: a, link: lnk, clock: clk, source: source,
			localPolicy: localPolicy, paths: p, remote: remote,
			childURL: enrollment.ChildURL,
		}, o)
	default:
		printUsage()
		return fmt.Errorf("неизвестная команда %q", cmd)
	}
}

// assembled — собранный агент со всем, что ему нужно. Отдельный тип, чтобы
// список не разрастался в сигнатурах команд.
type assembled struct {
	agent       *agent.Agent
	link        *link.Link
	clock       *clock.Clock
	source      clock.Source
	localPolicy config.Policy
	paths       paths
	// remote — рабочий стол, который наполняет помощник. Только у serve.
	remote *ipc.Desktop
	// childURL — что написать ребёнку на закрытом экране. Пусто, если адрес
	// заданий при привязке не назвали.
	childURL string
}

func enroll(path, server, token, childURL string) error {
	if server == "" || token == "" {
		return fmt.Errorf("нужны оба флага: -server и -token")
	}
	e := config.Enrollment{ServerURL: server, DeviceToken: token, ChildURL: childURL}
	if err := config.SaveEnrollment(path, e); err != nil {
		return err
	}
	fmt.Printf("устройство привязано: %s\n", e.Redacted())
	if e.ChildURL == "" {
		fmt.Println("адрес заданий не задан: на закрытом экране ребёнку не будет сказано, куда идти")
	}
	fmt.Printf("файл: %s\n", path)
	return nil
}

// recover разбирает последствия нештатной остановки: оплачивает пропуск и
// ставит сообщение родителю в очередь.
//
// Пропуск уходит в ту же очередь расхода, что и обычное время: иначе списание
// осталось бы только в локальном файле, который ребёнок может удалить.
func recoverUnclean(a *agent.Agent, lnk *link.Link, now time.Time) agent.Recovery {
	r := a.RecoverUnclean(now)
	if !r.Unclean {
		return r
	}
	if r.ChargedSecs > 0 {
		if err := lnk.Record(r.ChargedSecs, now); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
		}
	}
	lnk.QueueTamper("unclean_stop", uncleanDetail(r), now)
	return r
}

// uncleanDetail — что увидит родитель. Списанные минуты названы прямо: отличить
// сбой питания от снятия агента нельзя, и родителю может понадобиться вернуть
// время ручной корректировкой ровно на эту величину.
func uncleanDetail(r agent.Recovery) string {
	if r.ChargedSecs == 0 {
		return fmt.Sprintf("агент не работал %s, списывать было нечего", r.Gap.Round(time.Minute))
	}
	return fmt.Sprintf("агент не работал %s, списано %d мин",
		r.Gap.Round(time.Minute), r.ChargedSecs/60)
}

func status(a *agent.Agent, lnk *link.Link, clk *clock.Clock, source clock.Source,
	localPolicy config.Policy, enrollment config.Enrollment, p paths) error {

	r := source()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Обмен с сервером делаем и здесь: чаще всего status запускают именно
	// чтобы понять, почему связи нет.
	res, syncErr := lnk.Sync(ctx, r, localPolicy)
	if syncErr == nil {
		if _, err := a.SetPolicy(res.Policy); err != nil {
			fmt.Fprintf(os.Stderr, "предупреждение: политика сервера отклонена: %v\n", err)
		}
	}

	now := clk.Now(r)
	// Именно PendingRecovery: status ничего не меняет и не сохраняет, а списать
	// пропуск без сохранения значило бы списать его повторно при запуске.
	rec := a.PendingRecovery(now)
	// Ошибку наблюдения не возвращаем: диагностическая команда обязана
	// напечатать всё, что смогла узнать, и показать саму ошибку — иначе
	// от неё нет пользы ровно в тот момент, когда что-то сломалось.
	v, tickErr := a.Tick(now)
	printStatus(a, v, now, a.Policy, p.policy, p.state, tickErr)
	printLink(res, syncErr, clk, lnk, enrollment, rec)
	return nil
}

func loop(a *agent.Agent, lnk *link.Link, clk *clock.Clock, source clock.Source,
	localPolicy config.Policy, p paths, interval time.Duration, enforcing bool,
	childURL string) error {

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	mode := "наблюдение"
	if enforcing {
		mode = "наблюдение с блокировкой"
	}
	fmt.Printf("mykids-agent %s · режим: %s · период: %s\n", version, mode, interval)
	fmt.Println("остановка — Ctrl+C")

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	syncTicker := time.NewTicker(syncEvery)
	defer syncTicker.Stop()
	saveEvery := time.NewTicker(30 * time.Second)
	defer saveEvery.Stop()

	// Подкрутки считаем от значения, с которым запустились: clk.Tampers()
	// растёт за весь запуск, и прибавлять его при каждом сохранении значило бы
	// считать одни и те же сдвиги снова и снова.
	tampersAtStart := a.State().ClockTampers

	save := func(clean bool) {
		st := a.State()
		st.CleanShutdown = clean
		// Остаток секунд и поправка часов обязаны пережить перезапуск: иначе
		// достаточно было бы убивать агента, чтобы копить время бесплатно.
		st.PendingSeconds = lnk.Pending()
		st.PendingTampers = lnk.Tampers()
		// Метка времени нужна следующему запуску: по ней считается пропуск,
		// если этот запуск закончится не штатно.
		st.LastSeenAt = clk.Now(source())
		st.ClockOffsetSeconds = int(clk.Offset().Seconds())
		st.ClockTrusted = clk.Trusted()
		st.ClockTampers = tampersAtStart + clk.Tampers()
		if err := state.Save(p.state, st); err != nil {
			fmt.Fprintf(os.Stderr, "не удалось сохранить состояние: %v\n", err)
		}
	}

	sync := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		res, err := lnk.Sync(ctx, source(), localPolicy)
		if err != nil {
			fmt.Fprintf(os.Stderr, "обмен с сервером: %v\n", err)
		}
		changed, setErr := a.SetPolicy(res.Policy)
		if setErr != nil {
			fmt.Fprintf(os.Stderr, "политика отклонена: %v\n", setErr)
			return
		}
		if changed {
			fmt.Printf("политика обновлена (%s)\n", res.Source)
		}

		// То, что окажется на закрытом экране. Обновляем здесь, а не при
		// блокировке: в момент блокировки ходить на сервер уже поздно.
		sc := screen.Context{
			CreditsPerMinute: res.CreditsPerMinute,
			TomorrowMinutes:  a.TomorrowLimit(time.Now()),
			ChildURL:         childURL,
		}
		if res.Online {
			sc.Minutes, sc.Credits = res.Balances.Minutes, res.Balances.Credits
		}
		a.SetScreen(sc)
	}
	// Сначала политика: от дневной выдачи считается ограничение списания, и
	// по локальной оно вышло бы не тем, что задал родитель.
	sync()

	if rec := recoverUnclean(a, lnk, clk.Now(source())); rec.Unclean {
		fmt.Fprintf(os.Stderr, "внимание: %s\n", uncleanDetail(rec))
		// Сохраняемся сразу: убийство агента в первую же минуту не должно
		// стирать ни списание, ни сообщение родителю.
		save(false)
		// И сразу отдаём серверу. Ждать минуту до следующего обмена значит
		// оставить родителя в неведении ровно тогда, когда это важнее всего.
		sync()
	} else {
		save(false)
	}

	var lastLine string
	for {
		select {
		case <-stop:
			save(true)
			fmt.Println("\nостановлен, состояние сохранено")
			return nil

		case <-saveEvery.C:
			save(false)

		case <-syncTicker.C:
			sync()

		case <-ticker.C:
			r := source()
			jump, tampered := lnk.Observe(r)
			if tampered {
				fmt.Fprintf(os.Stderr, "внимание: %s — учёт продолжается по исправленному времени\n", jump)
			}

			now := clk.Now(r)
			v, err := a.Tick(now)
			if err != nil {
				fmt.Fprintf(os.Stderr, "тик: %v\n", err)
				continue
			}
			if err := lnk.Record(v.ConsumedSecs, now); err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
			}

			line := describe(v)
			if line != lastLine {
				fmt.Printf("%s  %s\n", now.Format("15:04:05"), line)
				lastLine = line
			}
		}
	}
}
