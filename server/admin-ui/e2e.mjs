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
    await page.getByRole('button', { name: 'Создать семью' }).click();
    await page.fill('#familyName', 'Тестовая семья');
    await page.fill('#email', email);
    await page.fill('#password', 'очень-длинный-пароль');
    await page.getByRole('button', { name: 'Создать семью' }).click();
    await page.waitForSelector('text=Дети');

    // --- добавление ребёнка
    await page.fill('#childName', 'Марк');
    await page.getByRole('button', { name: 'Добавить' }).click();
    await page.waitForSelector('text=Марк');

    await page.click('a:has-text("Марк")');
    await page.waitForSelector('#lim-1');

    // --- симулятор экономики считает доменной функцией
    await page.fill('#sim', '40');
    assert.equal(await page.getByTestId('sim-minutes').textContent(), '20',
      'симулятор посчитал не по курсу 2 кредита за минуту');

    // --- правка политики сохраняется
    await page.fill('#lim-1', '45');
    await page.getByRole('button', { name: 'Сохранить политику' }).click();
    await page.waitForSelector('text=Политика сохранена');
    await page.reload();
    await page.waitForSelector('#lim-1');
    assert.equal(await page.inputValue('#lim-1'), '45', 'лимит не сохранился');

    // --- корректировка требует причины и попадает в журнал
    await page.fill('#adj-amt', '100');
    await page.fill('#adj-note', 'стартовые кредиты');
    await page.getByRole('button', { name: 'Записать' }).click();
    await page.waitForSelector('text=корректировка родителя');
    assert.equal(await page.getByTestId('bal-credits').textContent(), '100');

    // --- токен устройства показывается один раз
    await page.getByRole('button', { name: 'Выдать токен' }).click();
    const token = (await page.getByTestId('device-token').textContent()).trim();
    assert.ok(token.length > 20, 'токен устройства не выдан');

    // --- магазин
    await page.click('a:has-text("Магазин")');
    await page.fill('#st-title', '+30 минут');
    await page.fill('#st-cost', '60');
    await page.getByRole('button', { name: 'Добавить' }).click();
    await page.waitForSelector('td:has-text("+30 минут")');

    // --- ребёнок на своём устройстве
    await page.goto(`${base}/child`);
    await page.fill('#devtok', token);
    await page.getByRole('button', { name: 'Подключить' }).click();
    await page.waitForSelector('text=Привет, Марк');

    await expectBalance(page, 'child-credits', '100');
    // Правила показываются ребёнку намеренно
    await page.waitForSelector('text=Курс обмена');

    // --- обмен кредитов на минуты
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
    await page.waitForSelector('.err');

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
