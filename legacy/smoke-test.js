/**
 * Дымовой тест legacy-приложения UKids Home.
 *
 *   npm install playwright && npx playwright install chromium
 *   node legacy/smoke-test.js
 *
 * Проверяет то, что было сломано до переноса приложения из хоста артефактов
 * в обычный браузер: сохранение прогресса между перезагрузками, отсутствие
 * мусорных ключей в progress и невозможность внедрить разметку через
 * подложенный файл резервной копии.
 */
const { chromium } = require('playwright');
const path = require('path');
const assert = require('assert');

const APP_URL = 'file://' + path.resolve(__dirname, 'ukids-app.html');
// Google Fonts недоступны в офлайн-окружении; шрифтовые стеки это переживают.
const IGNORED_CONSOLE = /Failed to load resource/;

async function main() {
  const browser = await chromium.launch({
    executablePath: process.env.CHROMIUM_PATH || undefined,
    args: process.env.CHROMIUM_PATH ? ['--no-sandbox'] : []
  });
  const page = await browser.newContext().then(c => c.newPage());

  const errors = [];
  page.on('pageerror', e => errors.push('pageerror: ' + e.message));
  page.on('console', m => {
    if (m.type() === 'error' && !IGNORED_CONSOLE.test(m.text())) {
      errors.push('console: ' + m.text());
    }
  });

  await page.goto(APP_URL);
  await page.waitForSelector('.empty h2');

  await page.click('[data-action="add-child"]');
  await page.fill('#acName', 'Марк');
  await page.selectOption('#acAge', '12');
  await page.click('#acSave');
  await page.waitForSelector('.station');

  assert.strictEqual(await page.locator('.station').count(), 12, 'на маршруте должно быть 12 недель');

  // Главная регрессия: раньше данные жили в window.storage и в браузере терялись.
  await page.reload();
  await page.waitForSelector('.profile-chip');
  assert.ok((await page.textContent('.profile-chip')).includes('Марк'), 'профиль не пережил перезагрузку');

  await page.click('[data-action="open-session"][data-week="1"][data-kind="wd"]');
  await page.waitForSelector('.schulte-cell');
  assert.strictEqual(await page.locator('.schulte-cell').count(), 25, 'в таблице Шульте должно быть 25 клеток');

  // Рестарт упражнения раньше вызывался без ребёнка и недели и плодил
  // ключи progress[undefined][undefined], попадавшие в резервную копию.
  await page.click('[data-action="schulte-restart"]');
  await page.waitForTimeout(100);
  await page.check('#doneCheck');
  await page.click('[data-action="save-session"]');
  await page.waitForSelector('.station.done, .check');

  const saved = await page.evaluate(() => JSON.parse(localStorage.getItem('ukids-app-data')));
  const childId = saved.children[0].id;
  assert.strictEqual(saved.progress[childId]['1'].wd.done, true, 'отметка о занятии не сохранилась');
  assert.deepStrictEqual(
    Object.keys(saved.progress).filter(k => k === 'undefined' || k === 'null'), [],
    'в progress появились мусорные ключи'
  );

  // Резервная копия — недоверенный ввод: разметка из неё не должна исполняться.
  await page.evaluate(() => {
    localStorage.setItem('ukids-app-data', JSON.stringify({
      children: [{ id: 'x1', name: 'Тест', age: '<img src=x onerror=window.__pwned=1>' }],
      activeChildId: 'x1',
      journal: { x1: [{ id: 'j1', mood: '<img src=x onerror=window.__pwned=1>',
                        date: '01.01.2026', grateful: 'ок', strength: '', win: '' }] }
    }));
  });
  await page.reload();
  await page.click('[data-action="nav"][data-view="journal"]');
  await page.waitForSelector('.journal-entry');
  assert.strictEqual(await page.evaluate(() => !!window.__pwned), false,
    'разметка из резервной копии исполнилась');

  // Частичная копия без progress раньше роняла рендер в loadData.
  await page.evaluate(() => {
    localStorage.setItem('ukids-app-data', JSON.stringify({ children: [{ id: 'y1', name: 'Аня', age: '11' }] }));
  });
  await page.reload();
  await page.waitForSelector('.station');

  assert.deepStrictEqual(errors, [], 'в консоли есть ошибки приложения');

  await browser.close();
  console.log('legacy smoke: все проверки пройдены');
}

main().catch(e => { console.error(e); process.exit(1); });
