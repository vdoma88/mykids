/**
 * Сквозной тест админки и интерфейса ребёнка.
 *
 *   npm run build -w @mykids/admin-ui
 *   node server/admin-ui/e2e.mjs
 *
 * Поднимает API и статику собранной админки, затем проходит путь родителя
 * целиком: регистрация, ребёнок, политика, устройство, корректировка — и путь
 * ребёнка: баланс, правила, обмен. Проверяет то, что не видно из юнит-тестов:
 * что фронт и API действительно договариваются.
 */
import { chromium } from 'playwright';
import { spawn } from 'node:child_process';
import { createReadStream, existsSync, statSync } from 'node:fs';
import { createServer, request as httpRequest } from 'node:http';
import { extname, join, normalize } from 'node:path';
import assert from 'node:assert/strict';

const API_PORT = 3199;
const UI_PORT = 5199;
const DIST = new URL('./dist/', import.meta.url).pathname;
const API_DIR = new URL('../api/', import.meta.url).pathname;
const DB = process.env.DATABASE_URL_E2E
  ?? 'postgresql://postgres@127.0.0.1:5433/mykids_e2e';

const TYPES = { '.html': 'text/html; charset=utf-8', '.js': 'text/javascript; charset=utf-8',
                '.css': 'text/css; charset=utf-8', '.json': 'application/json; charset=utf-8' };

/** Статика админки с проксированием API: так же, как за обратным прокси в бою. */
function startUi() {
  return createServer((req, res) => {
    const url = new URL(req.url ?? '/', 'http://localhost');
    const path = decodeURIComponent(url.pathname);

    if (/^\/(auth|admin|agent|health)(\/|$)/.test(path)
        || (path.startsWith('/child/') && req.method !== 'GET')
        || ['/child/me', '/child/store', '/child/packs'].includes(path)) {
      const upstream = httpRequest(
        { host: '127.0.0.1', port: API_PORT, path: req.url, method: req.method, headers: req.headers },
        (up) => { res.writeHead(up.statusCode ?? 502, up.headers); up.pipe(res); },
      );
      upstream.on('error', () => { res.writeHead(502).end('нет API'); });
      req.pipe(upstream);
      return;
    }

    const file = normalize(join(DIST, path));
    if (file.startsWith(DIST) && existsSync(file) && statSync(file).isFile()) {
      res.writeHead(200, { 'content-type': TYPES[extname(file)] ?? 'application/octet-stream' });
      createReadStream(file).pipe(res);
      return;
    }
    // SPA: любой неизвестный путь отдаёт index.html
    res.writeHead(200, { 'content-type': TYPES['.html'] });
    createReadStream(join(DIST, 'index.html')).pipe(res);
  }).listen(UI_PORT);
}

/** Чужой процесс на нашем порту сделал бы результат теста бессмысленным. */
async function assertPortFree(port) {
  try {
    const res = await fetch(`http://127.0.0.1:${port}/health`, { signal: AbortSignal.timeout(500) });
    if (res.ok) throw new Error(`порт ${port} уже занят: остановите старый сервер`);
  } catch (e) {
    if (String(e.message).includes('уже занят')) throw e;
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
    env: { ...process.env, DATABASE_URL: DB, PORT: String(API_PORT) },
  });
  const stopApi = () => {
    try { process.kill(-api.pid, 'SIGTERM'); } catch { /* уже мёртв */ }
  };
  process.on('exit', stopApi);
  const ui = startUi();
  let browser;
  let page;
  const errors = [];

  try {
    await assertPortFree(API_PORT);
    await waitFor(`http://127.0.0.1:${API_PORT}/health`);
    await waitFor(`http://127.0.0.1:${UI_PORT}/`);

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

    const base = `http://127.0.0.1:${UI_PORT}`;
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

    assert.equal(await page.getByTestId('child-credits').textContent(), '100');
    // Правила показываются ребёнку намеренно
    await page.waitForSelector('text=Курс обмена');

    // --- обмен кредитов на минуты
    await page.fill('#conv', '10');
    await page.getByRole('button', { name: 'Обменять' }).click();
    await page.waitForSelector('text=Получено 10 минут');
    assert.equal(await page.getByTestId('child-credits').textContent(), '80');
    assert.equal(await page.getByTestId('child-minutes').textContent(), '10');

    // --- покупка в магазине
    await page.getByRole('button', { name: 'Купить' }).click();
    await page.waitForSelector('text=Куплено');
    assert.equal(await page.getByTestId('child-credits').textContent(), '20');
    assert.equal(await page.getByTestId('child-minutes').textContent(), '40');

    // --- обмен сверх остатка урезается до доступного, а не отклоняется:
    // так задумано в домене, и интерфейс обязан сказать правду о выданном
    await page.fill('#conv', '100');
    await page.getByRole('button', { name: 'Обменять' }).click();
    await page.waitForSelector('text=вместо 100');
    assert.equal(await page.getByTestId('child-credits').textContent(), '0');
    assert.equal(await page.getByTestId('child-minutes').textContent(), '50');

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
    ui.close();
    stopApi();
  }
}

main().catch((e) => { console.error(e.message ?? e); process.exit(1); });
