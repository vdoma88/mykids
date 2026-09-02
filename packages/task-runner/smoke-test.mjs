/**
 * Дымовой тест раннера: поднимает dev-сервер и проходит пакеты в Chromium.
 *
 *   npm run build && node packages/task-runner/smoke-test.mjs
 *
 * Порядок заданий в пакете зависит от стратегии выдачи, поэтому тест не
 * рассчитывает на конкретный тип на конкретном шаге, а определяет его по DOM.
 */
import { chromium } from 'playwright';
import { spawn } from 'node:child_process';
import assert from 'node:assert/strict';

const PORT = 5199;
const BASE = `http://127.0.0.1:${PORT}`;

/** Отвечает на текущее задание. correct=false — заведомо неверный ответ. */
async function answerCurrent(page, { correct = true } = {}) {
  if (await page.locator('input[name=opt]').count()) {
    const options = page.locator('input[name=opt]');
    await options.nth(correct ? 0 : (await options.count()) - 1).check();
    return 'single_choice';
  }
  if (await page.locator('.answer-input').count()) {
    await page.fill('.answer-input', correct ? '6 Н' : 'нет');
    return 'numeric_or_text';
  }
  if (await page.locator('select[data-left]').count()) {
    for (const sel of await page.locator('select[data-left]').all()) {
      await sel.selectOption({ index: 1 });
    }
    return 'matching';
  }
  if (await page.locator('input[name=likert]').count()) {
    await page.locator('input[name=likert]').first().check();
    return 'likert';
  }
  if (await page.locator('.answer-text').count()) {
    await page.fill('.answer-text', 'Сегодня получилось спокойно доделать уроки и не отвлекаться на телефон.');
    return 'reflection';
  }
  if (await page.locator('.ask-parent').count()) {
    await page.check('.ask-parent');
    return 'parent_verified';
  }
  if (await page.locator('.schulte-cell').count()) {
    // Проходим таблицу целиком: клетки подписаны своими числами
    const total = await page.locator('.schulte-cell').count();
    for (let n = 1; n <= total; n++) await page.click(`.schulte-cell[data-n="${n}"]`);
    return 'interactive';
  }
  if (await page.locator('.timed-done').count()) return 'timed_placeholder';
  return 'unknown';
}

/** Проходит пакет до конца, возвращает текст итога. */
async function playPack(page, packId, { firstWrong = false } = {}) {
  await page.click(`.pack[data-id="${packId}"]`);
  await page.waitForSelector('#item');

  const seen = [];
  for (let guard = 0; guard < 30; guard++) {
    if (await page.locator('#summary').count()) break;
    const kind = await answerCurrent(page, { correct: !(firstWrong && seen.length === 0) });
    seen.push(kind);
    await page.click('#submit');
    if (await page.locator('#next').count()) await page.click('#next');
    else break;
  }

  await page.waitForSelector('#summary');
  return { summary: await page.textContent('#summary'), seen };
}

async function main() {
  const server = spawn(process.execPath,
    [new URL('./dev-server.mjs', import.meta.url).pathname, String(PORT)], { stdio: 'ignore' });
  let browser;

  try {
    for (let i = 0; i < 50; i++) {
      try { if ((await fetch(BASE + '/')).ok) break; } catch { /* ещё не поднялся */ }
      await new Promise((r) => setTimeout(r, 100));
    }

    browser = await chromium.launch({
      executablePath: process.env.CHROMIUM_PATH || undefined,
      args: process.env.CHROMIUM_PATH ? ['--no-sandbox'] : [],
    });
    const page = await browser.newContext().then((c) => c.newPage());
    page.setDefaultTimeout(10000);

    const errors = [];
    page.on('pageerror', (e) => errors.push('pageerror: ' + e.message));
    page.on('console', (m) => { if (m.type() === 'error') errors.push('console: ' + m.text()); });

    await page.goto(BASE + '/');
    await page.waitForSelector('.pack');
    assert.equal(await page.locator('.pack').count(), 14, 'ожидалось 14 пакетов');

    // Пакет по механике: оцениваемые типы, первый ответ намеренно неверный
    const physics = await playPack(page, 'ru.mykids.physics.mechanics.basic', { firstWrong: true });
    assert.match(physics.summary, /Верно: \d+ из 5/, 'нет итога сессии');
    assert.match(physics.summary, /Начислено кредитов/, 'нет строки о кредитах');
    const correct = Number(/Верно: (\d+) из 5/.exec(physics.summary)[1]);
    assert.ok(correct < 5, 'неверный ответ засчитан как верный');
    await page.click('#back');

    // Баланс переживает перезагрузку: состояние в localStorage
    const before = await page.textContent('#balance');
    assert.ok(!before.startsWith('0 кредитов'), `кредиты не начислены: ${before}`);
    await page.reload();
    await page.waitForSelector('.pack');
    assert.equal(await page.textContent('#balance'), before, 'баланс не пережил перезагрузку');

    // Психологический пакет: интерактив, шкала, рефлексия, подтверждение родителя
    const psy = await playPack(page, 'ru.mykids.psychology.week01.attention');
    assert.ok(psy.seen.includes('interactive'), `таблица Шульте не отрисовалась: ${psy.seen}`);
    assert.ok(psy.seen.includes('likert'), `шкала не отрисовалась: ${psy.seen}`);
    assert.ok(psy.seen.includes('parent_verified'), `подтверждение родителя не отрисовалось: ${psy.seen}`);
    assert.match(psy.summary, /Ждут подтверждения родителя/, 'нет упоминания об ожидании родителя');

    assert.deepEqual(errors, [], 'в консоли есть ошибки приложения');
    console.log(`runner smoke: все проверки пройдены (типы: ${[...new Set([...physics.seen, ...psy.seen])].join(', ')})`);
  } finally {
    await browser?.close();
    server.kill();
  }
}

main().catch((e) => { console.error(e.message ?? e); process.exit(1); });
