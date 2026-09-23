/**
 * Сквозной тест админки и интерфейса ребёнка.
 *
 *   npm run build -w @mykids/admin-ui
 *   node server/admin-ui/e2e.mjs
 *
 * Поднимает настоящий сервер — тот самый, что уходит в релиз, — и проходит
 * путь родителя целиком: регистрация, ребёнок, политика, устройство,
 * корректировка, — и путь ребёнка: баланс, правила, обмен. Проверяет то,
 * что не видно из юнит-тестов: что фронт и API действительно договариваются.
 *
 * Раньше здесь стоял самодельный сервер статики с проксированием API, и он
 * повторял разбор путей своими руками. Тест против собственной копии сервера
 * проверяет копию: разойтись они могут молча, и разойдётся та, которую реже
 * смотрят. Теперь страницы отдаёт сам API, и проверять надо его.
 */
import { chromium } from 'playwright';
import { spawn } from 'node:child_process';
import assert from 'node:assert/strict';

const API_PORT = 3199;
const DIST = new URL('./dist/', import.meta.url).pathname;
const API_DIR = new URL('../api/', import.meta.url).pathname;
const DB = process.env.DATABASE_URL_E2E
  ?? 'postgresql://postgres@127.0.0.1:5433/mykids_e2e';

/** Чужой процесс на нашем порту сделал бы результат теста бессмысленным. */
async function assertPortFree(port) {
  try {
    const res = await fetch(`http://127.0.0.1:${port}/health`, { signal: AbortSignal.timeout(500) });
    if (res.ok) throw new Error(`порт ${port} уже занят: остановите старый сервер`);
  } catch (e) {
    if (String(e.message).includes('уже занят')) throw e;
  }
}

/**
 * Ждёт, пока на экране появится ожидаемый баланс.
 *
 * Сообщение об успехе и новое число приходят разными кадрами: сообщение
 * рисуется сразу, а баланс — после ответа сервера. Проверять сразу после
 * сообщения значит иногда читать ещё старое число: на медленной машине это
 * ровно то, что и случилось.
 *
 * Ожидание не прячет поломку: если баланс так и не станет верным, проверка
 * упадёт по таймауту и скажет, что именно на экране.
 */
async function expectBalance(page, testid, want) {
  try {
    await page.waitForFunction(
      ([id, value]) => document.querySelector(`[data-testid="${id}"]`)?.textContent === value,
      [testid, want],
      { timeout: 10000 },
    );
  } catch {
    const got = await page.getByTestId(testid).textContent();
    assert.fail(`${testid}: на экране ${got}, ожидалось ${want}`);
  }
}

async function fetchJson(url) {
  const res = await fetch(url);
  if (!res.ok) throw new Error(`${url}: ${res.status}`);
  return res.json();
}

/**
 * Отвечает на задание правильно.
 *
 * Варианты рендерер перемешивает, поэтому ищем по тексту, а не по номеру:
 * тест, завязанный на порядок, начал бы врать ровно тогда, когда порядок
 * поменяют.
 */
async function answerItem(page, item) {
  const host = page.locator('[data-testid="task-host"]');
  switch (item.type) {
    case 'numeric':
      await host.locator('input').first().fill(String(item.answer.value));
      break;
    case 'single_choice':
      await host.locator('label.opt', { hasText: item.options[item.answerIndex].text })
        .locator('input').check();
      break;
    case 'short_text':
      await host.locator('input, textarea').first().fill(item.answer.accepted[0]);
      break;
    default:
      throw new Error(`тест не умеет отвечать на задание типа ${item.type}`);
  }
}

async function getJson(url, token) {
  const res = await fetch(url, { headers: { authorization: `Bearer ${token}` } });
  const json = await res.json();
  if (!res.ok) throw new Error(`${url}: ${res.status} ${JSON.stringify(json)}`);
  return json;
}

/** Запрос от имени устройства ребёнка — тем же способом, что и его страница. */
async function postJson(url, token, body) {
  const res = await fetch(url, {
    method: 'POST',
    headers: { 'content-type': 'application/json', authorization: `Bearer ${token}` },
    body: JSON.stringify(body),
  });
  const json = await res.json();
  if (!res.ok) throw new Error(`${url}: ${res.status} ${JSON.stringify(json)}`);
  return json;
}

async function waitFor(url, tries = 80) {
  for (let i = 0; i < tries; i++) {
    try { if ((await fetch(url)).ok) return; } catch { /* ещё не поднялся */ }
    await new Promise((r) => setTimeout(r, 150));
  }
  throw new Error(`не дождался ${url}`);
}

async function main() {
  // detached: убиваем всю группу процессов. Обычный kill снимает обёртку npx,
  // а сервер остаётся жить и держит порт — следующий прогон тихо ходит
  // в устаревший процесс и проверяет не тот код.
  const api = spawn('npx', ['tsx', 'src/main.ts'], {
    cwd: API_DIR, stdio: 'ignore', detached: true,
    env: {
      ...process.env, DATABASE_URL: DB, PORT: String(API_PORT),
      // Страницы отдаёт сам сервер: проверяем то, что уходит в релиз.
      MYKIDS_WEB_ROOT: DIST,
    },
  });
  const stopApi = () => {
    try { process.kill(-api.pid, 'SIGTERM'); } catch { /* уже мёртв */ }
  };
  process.on('exit', stopApi);
  let browser;
  let page;
  const errors = [];

  try {
    await assertPortFree(API_PORT);
    await waitFor(`http://127.0.0.1:${API_PORT}/health`);

    browser = await chromium.launch({
      executablePath: process.env.CHROMIUM_PATH || undefined,
      args: process.env.CHROMIUM_PATH ? ['--no-sandbox'] : [],
    });
    page = await browser.newContext().then((c) => c.newPage());
    page.setDefaultTimeout(15000);

    page.on('pageerror', (e) => errors.push('pageerror: ' + e.message));
    page.on('console', (m) => {
      if (m.type() === 'error' && !/Failed to load resource/.test(m.text())) {
        errors.push('console: ' + m.text());
      }
    });

    const base = `http://127.0.0.1:${API_PORT}`;
    const email = `p${Date.now()}@example.com`;

    // --- родитель: регистрация
    await page.goto(base);
    await page.getByRole('tab', { name: 'Новая семья' }).click();
    await page.fill('#familyName', 'Тестовая семья');
    await page.fill('#email', email);
    await page.fill('#password', 'очень-длинный-пароль');
    await page.getByRole('button', { name: 'Создать семью' }).click();
    await page.waitForSelector('text=Дети');

    // --- добавление ребёнка. Первого ребёнка страница открывает сразу: у
    // него ещё нет ни устройства, ни пакетов, и настраивать его всё равно там.
    await page.fill('#childName', 'Марк');
    await page.getByRole('button', { name: 'Добавить' }).click();
    await page.waitForURL(/\/children\/[0-9a-f-]{36}$/);
    await page.waitForSelector('h1:has-text("Марк")');

    // --- правила — своя вкладка, со своим адресом
    await page.getByRole('tab', { name: 'Правила' }).click();
    await page.waitForSelector('#lim-1');

    // --- симулятор экономики считает доменной функцией
    await page.fill('#sim', '40');
    assert.equal(await page.getByTestId('sim-minutes').textContent(), '20',
      'симулятор посчитал не по курсу 2 кредита за минуту');

    // --- правка политики сохраняется
    await page.fill('#lim-1', '45');
    await page.getByRole('button', { name: 'Сохранить политику' }).click();
    await page.waitForSelector('text=Политика сохранена');
    // Перезагрузка возвращает на ту же вкладку: адрес её помнит.
    await page.reload();
    await page.waitForSelector('#lim-1');
    assert.equal(await page.inputValue('#lim-1'), '45', 'лимит не сохранился');

    // --- корректировка требует причины и попадает в журнал
    await page.getByRole('tab', { name: /^Обзор/ }).click();
    await page.fill('#adj-amt', '100');
    await page.fill('#adj-note', 'стартовые кредиты');
    await page.getByRole('button', { name: 'Записать' }).click();
    await page.waitForSelector('text=корректировка родителя');
    await expectBalance(page, 'bal-credits', '100');

    // --- токен устройства показывается один раз
    await page.getByRole('tab', { name: 'Устройства' }).click();
    await page.getByRole('button', { name: 'Выдать токен' }).click();
    const token = (await page.getByTestId('device-token').textContent()).trim();
    assert.ok(token.length > 20, 'токен устройства не выдан');

    // --- пакеты заданий: без них ребёнку нечем зарабатывать кредиты,
    // и весь остальной экран у него бессмыслен
    const pack = 'ru.mykids.physics.mechanics.basic';
    // Второй пакет — ради заданий, которые проверить машиной нельзя:
    // «договориться о правилах», «прибраться в комнате». Их подтверждает
    // родитель, и до сих пор эта очередь только показывалась.
    const chores = 'ru.mykids.psychology.week01.attention';
    await page.getByRole('tab', { name: 'Задания' }).click();
    await page.check(`[data-testid="pack-${pack}"]`);
    await page.check(`[data-testid="pack-${chores}"]`);
    await page.getByTestId('save-packs').click();
    await page.waitForSelector('[data-testid="packs-saved"]');

    // --- магазин
    await page.click('a:has-text("Магазин")');
    await page.fill('#st-title', '+30 минут');
    await page.fill('#st-cost', '60');
    await page.getByRole('button', { name: 'Добавить' }).click();
    await page.waitForSelector('[data-testid="store-items"] >> text=+30 минут');

    // --- ребёнок на своём устройстве
    await page.goto(`${base}/child`);
    await page.fill('#devtok', token);
    await page.getByRole('button', { name: 'Подключить' }).click();
    await page.waitForSelector('text=Привет, Марк');

    await expectBalance(page, 'child-credits', '100');
    // Правила показываются ребёнку намеренно
    await page.getByRole('tab', { name: 'Правила' }).click();
    await page.waitForSelector('text=Курс обмена');

    // --- обмен кредитов на минуты
    await page.getByRole('tab', { name: 'Магазин' }).click();
    await page.fill('#conv', '10');
    await page.getByRole('button', { name: 'Обменять' }).click();
    await page.waitForSelector('text=Получено 10 минут');
    await expectBalance(page, 'child-credits', '80');
    await expectBalance(page, 'child-minutes', '10');

    // --- покупка в магазине
    await page.getByRole('button', { name: 'Купить' }).click();
    await page.waitForSelector('text=Куплено');
    await expectBalance(page, 'child-credits', '20');
    await expectBalance(page, 'child-minutes', '40');

    // --- обмен сверх остатка урезается до доступного, а не отклоняется:
    // так задумано в домене, и интерфейс обязан сказать правду о выданном
    await page.fill('#conv', '100');
    await page.getByRole('button', { name: 'Обменять' }).click();
    await page.waitForSelector('text=вместо 100');
    await expectBalance(page, 'child-credits', '0');
    await expectBalance(page, 'child-minutes', '50');

    // --- а когда кредитов нет совсем, сервер отказывает
    await page.fill('#conv', '5');
    await page.getByRole('button', { name: 'Обменять' }).click();
    await page.waitForSelector('[role="alert"]');

    // --- раздел ребёнка переживает перезагрузку. Раньше всё глубже /child/
    // сервер считал запросами к API, и обновлённая страница отвечала 401.
    await page.reload();
    await page.waitForSelector('#conv');
    await page.getByRole('tab', { name: 'Задания' }).click();

    // --- задания: ради них всё и затевалось. Ребёнок с нулём кредитов
    // должен иметь возможность их заработать, не прося у родителя.
    await page.waitForSelector(`[data-testid="pack-${pack}"]`);
    // Пакетов теперь два — кнопку берём у нужного, а не первую попавшуюся.
    await page.getByTestId(`pack-${pack}`).getByRole('button', { name: 'Решать' }).click();
    await page.waitForSelector('[data-testid="task-host"] .stem');

    // Отвечаем правильно, подсмотрев ответ в том же файле, который отдаёт
    // сервер. Проверять надо именно начисление: круг «решил — получил
    // кредиты — обменял на время» и есть вся суть системы, и до сих пор он
    // был разорван, потому что решать было негде.
    const stem = await page.locator('[data-testid="task-host"] .stem').textContent();
    const items = await fetchJson(`${base}/content/packs/${pack}/pack.json`);
    let answered = false;
    for (const rel of items.items) {
      const item = await fetchJson(`${base}/content/packs/${pack}/${rel}`);
      if (item.stem !== stem) continue;
      await answerItem(page, item);
      answered = true;
      break;
    }
    assert.ok(answered, `задание «${stem}» не нашлось в пакете`);

    await page.getByTestId('task-submit').click();
    await page.waitForSelector('[data-testid="task-note"]');
    const note = await page.getByTestId('task-note').textContent();
    assert.match(note, /Верно\. \+\d+ кредит/, `за верный ответ не начислено: ${note}`);
    // Результат остаётся на экране с тем заданием, к которому относится;
    // дальше ребёнок идёт сам.
    await page.getByTestId('task-next').click();
    await page.waitForSelector('[data-testid="task-note"]', { state: 'detached' });

    // --- задание, которое проверить машиной нельзя
    //
    // Отправляем его тем же способом, что и страница ребёнка: токеном
    // устройства. Дойти до него кликами мешает порядок заданий в пакете —
    // а проверить здесь надо не порядок, а то, что нажатие «готово» само по
    // себе не платит и что родителю есть чем это подтвердить.
    const sent = await postJson(`${base}/child/attempts`, token, {
      packId: chores, itemId: 'psy-w01-practice-1',
      answer: { type: 'parent_verified', requested: true },
    });
    assert.equal(sent.credits, 0, `за нажатие кнопки начислено ${sent.credits}`);
    assert.equal(sent.pendingApproval, true, 'задание не встало в очередь к родителю');

    // --- родитель разбирает очередь
    await page.goto(`${base}/approvals`);
    await page.waitForSelector('[data-testid="pending-attempt"]');
    await page.getByTestId('approve-attempt').first().click();
    await page.waitForSelector('[data-testid="approval-note"]');
    const approved = await page.getByTestId('approval-note').textContent();
    assert.match(approved, /Начислено кредитов: 3/, `подтверждение не начислило: ${approved}`);
    // Очередь должна опустеть сама: список, который не обновляется, толкает
    // родителя нажать ещё раз.
    await page.waitForSelector('[data-testid="pending-attempt"]', { state: 'detached' });

    // --- покупка на одобрение, которую родитель отклоняет. Раньше такая
    // покупка списывала цену и висела в очереди навсегда: ни одобрить, ни
    // вернуть кредиты было нечем.
    await page.goto(`${base}/store`);
    await page.fill('#st-title', 'Поход в кино');
    await page.fill('#st-cost', '4');
    await page.getByRole('switch', { name: /Нужно моё одобрение/ }).click();
    await page.getByRole('button', { name: 'Добавить' }).click();
    await page.waitForSelector('[data-testid="store-items"] >> text=Поход в кино');

    const shelf = await getJson(`${base}/child/store`, token);
    const movie = shelf.find((i) => i.title === 'Поход в кино');
    const before = (await getJson(`${base}/child/me`, token)).balances.credits;
    await postJson(`${base}/child/purchases`, token, { storeItemId: movie.id });
    assert.equal((await getJson(`${base}/child/me`, token)).balances.credits, before - 4,
      'цена покупки на одобрение не списалась сразу');

    await page.goto(`${base}/approvals`);
    await page.waitForSelector('[data-testid="pending-purchase"]');
    await page.getByTestId('pending-purchase').getByRole('button', { name: 'Отклонить' }).click();
    await page.waitForSelector('text=возвращено');
    await page.waitForSelector('[data-testid="pending-purchase"]', { state: 'detached' });
    assert.equal((await getJson(`${base}/child/me`, token)).balances.credits, before,
      'после отказа кредиты не вернулись');

    assert.deepEqual(errors, [], 'в консоли есть ошибки приложения');
    console.log('admin-ui e2e: все проверки пройдены');
  } catch (err) {
    // Падение без контекста бесполезно: печатаем, что было на экране.
    if (page) {
      const box = await page.locator('.err').first().textContent().catch(() => null);
      const body = await page.textContent('body').catch(() => '');
      console.error('на странице была ошибка:', box ?? '—');
      console.error('URL:', page.url());
      console.error('текст страницы:', (body ?? '').replace(/\s+/g, ' ').slice(0, 400));
      console.error('ошибки консоли:', errors.length ? errors.join(' | ') : '—');
    }
    throw err;
  } finally {
    await browser?.close();
    stopApi();
  }
}

main().catch((e) => { console.error(e.message ?? e); process.exit(1); });
