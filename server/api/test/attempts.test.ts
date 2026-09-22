import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { afterAll, beforeEach, describe, expect, it } from 'vitest';
import type { FastifyInstance } from 'fastify';
import { buildApp } from '../src/app.js';
import { ContentError, ContentService } from '../src/services/content.service.js';
import { prisma, resetDb } from './helpers.js';

/**
 * Что сервер принимает от ребёнка на слово.
 *
 * До этих тестов — всё: и оценку ответа, и цену задания, и потолок пакета.
 * Браузер присылал число, сервер его начислял. Весь антифарм — темп, потолки,
 * затухание за повтор — обходился одним запросом мимо страницы.
 *
 * Пакеты сервер отдаёт со своего диска, и правильный ответ лежит там же.
 * Каталог здесь настоящий, из репозитория: проверять это против выдуманного
 * пакета значило бы не проверять ничего.
 */
const contentRoot = fileURLToPath(new URL('../../../content/packs', import.meta.url));
const app: FastifyInstance = buildApp(prisma, { contentRoot });

const PHYS = 'ru.mykids.physics.mechanics.basic';
const PSY = 'ru.mykids.psychology.week01.attention';

beforeEach(resetDb);
afterAll(async () => { await app.close(); await prisma.$disconnect(); });

interface Ctx {
  auth: Record<string, string>;
  device: Record<string, string>;
  childId: string;
}

async function withChild(packs: string[] = [PHYS]): Promise<Ctx> {
  const reg = await app.inject({
    method: 'POST', url: '/auth/register',
    payload: { familyName: 'Семья', email: `p${Math.random()}@example.com`, password: 'очень-длинный-пароль' },
  });
  const auth = { authorization: `Bearer ${(reg.json() as { token: string }).token}` };

  const child = await app.inject({
    method: 'POST', url: '/admin/children', headers: auth, payload: { name: 'Марк' },
  });
  const childId = (child.json() as { id: string }).id;

  const dev = await app.inject({
    method: 'POST', url: `/admin/children/${childId}/devices`, headers: auth,
    payload: { platform: 'windows', name: 'ПК' },
  });
  const device = { authorization: `Bearer ${(dev.json() as { token: string }).token}` };

  await app.inject({
    method: 'PUT', url: `/admin/children/${childId}/packs`, headers: auth, payload: { packs },
  });
  return { auth, device, childId };
}

type Body = Record<string, unknown>;

async function answer(ctx: Ctx, payload: Body): Promise<{ status: number; body: Body }> {
  const res = await app.inject({
    method: 'POST', url: '/child/attempts', headers: ctx.device, payload,
  });
  return { status: res.statusCode, body: res.json() as Body };
}

describe('ответ проверяет сервер', () => {
  it('цену задания называет пакет, а не запрос', async () => {
    const ctx = await withChild();

    // Вместе с ответом уходит всё, чем раньше можно было назначить себе цену:
    // своя оценка, своя стоимость, свой потолок пакета. Ни одно из этих полей
    // больше не читается — начислено должно быть ровно то, что в пакете.
    const { status, body } = await answer(ctx, {
      packId: PHYS, itemId: 'phys-mech-001',
      answer: { type: 'numeric', raw: '6 Н' },
      score: 1, baseCredits: 50, packDailyCreditCap: 500, cooldownHours: 0,
    });

    expect(status).toBe(201);
    expect(body).toMatchObject({ credits: 2, score: 1 });
  });

  it('неверный ответ не стоит ничего, и причина названа', async () => {
    const ctx = await withChild();
    const { body } = await answer(ctx, {
      packId: PHYS, itemId: 'phys-mech-001',
      answer: { type: 'numeric', raw: '99 Н' },
    });
    expect(body).toMatchObject({ credits: 0, score: 0 });
    expect(body['feedback']).toBeTruthy();
  });

  it('частично верный ответ стоит части', async () => {
    const ctx = await withChild();
    // Две пары из четырёх: 0.5 от двух кредитов — один.
    const { body } = await answer(ctx, {
      packId: PHYS, itemId: 'phys-mech-005',
      answer: {
        type: 'matching',
        assignments: { 'Сила': 'ньютон', 'Работа': 'джоуль', 'Мощность': 'паскаль', 'Давление': 'ватт' },
      },
    });
    expect(body).toMatchObject({ credits: 1, score: 0.5 });
  });

  it('неназначенный пакет не приносит кредитов', async () => {
    // Пакет есть в каталоге и лежит на диске — но этому ребёнку его не
    // назначали. Раньше кредиты приносил любой пакет из каталога, включая
    // снятые родителем.
    const ctx = await withChild([PHYS]);
    const { status, body } = await answer(ctx, {
      packId: PSY, itemId: 'psy-w01-practice-1',
      answer: { type: 'parent_verified', requested: true },
    });
    expect(status).toBe(403);
    expect(body).toMatchObject({ error: 'pack_not_assigned' });
    expect(await prisma.attempt.count()).toBe(0);
  });

  it('задания, которого нет в пакете, не существует', async () => {
    const ctx = await withChild();
    const { status, body } = await answer(ctx, {
      packId: PHYS, itemId: 'выдуманное',
      answer: { type: 'numeric', raw: '6' },
    });
    expect(status).toBe(404);
    expect(body).toMatchObject({ error: 'unknown_item' });
  });

  it('ответ не того типа — это ошибка запроса, а не ноль кредитов', async () => {
    // Ноль здесь был бы хуже: ребёнок с поломанной страницей молча терял бы
    // попытки и не понимал почему.
    const ctx = await withChild();
    const { status, body } = await answer(ctx, {
      packId: PHYS, itemId: 'phys-mech-001',
      answer: { type: 'reflection', text: 'ответ не от того задания' },
    });
    expect(status).toBe(400);
    expect(body).toMatchObject({ error: 'answer_type_mismatch' });
  });

  it('суточный потолок пакета — тоже из пакета', async () => {
    const ctx = await withChild();
    // Пакет разрешает 40 кредитов в сутки; 39 из них уже набраны. Значит,
    // задание за 2 кредита должно быть урезано до одного — и урезать его
    // обязан потолок из пакета, а не число, присланное вместе с ответом.
    //
    // Набираем задним числом на считаные секунды: попытки, пришедшие в одну
    // секунду, сошли бы за серию слишком быстрых ответов и до потолка бы не
    // дошли. К границе суток это отношения не имеет.
    const secondsAgo = (s: number): Date => new Date(Date.now() - s * 1000);
    await prisma.attempt.createMany({
      data: [
        { childId: ctx.childId, packId: PHYS, itemId: 'прошлое-1', score: 1, credits: 20, createdAt: secondsAgo(12) },
        { childId: ctx.childId, packId: PHYS, itemId: 'прошлое-2', score: 1, credits: 19, createdAt: secondsAgo(6) },
      ],
    });

    const { body } = await answer(ctx, {
      packId: PHYS, itemId: 'phys-mech-001',
      answer: { type: 'numeric', raw: '6 Н' },
      packDailyCreditCap: 500,
    });
    expect(body).toMatchObject({ credits: 1 });
  });

  it('ответ ребёнка сохраняется: родитель видит, за что платит', async () => {
    const ctx = await withChild();
    await answer(ctx, {
      packId: PHYS, itemId: 'phys-mech-003',
      answer: { type: 'single_choice', optionIndex: 0 },
    });
    const row = await prisma.attempt.findFirstOrThrow({ where: { childId: ctx.childId } });
    expect(row.answer).toEqual({ type: 'single_choice', optionIndex: 0 });
  });
});

describe('задания под подтверждение родителя', () => {
  it('нажатие кнопки не платит само по себе', async () => {
    // Таких заданий в пакетах больше половины: «прибрался в комнате»,
    // «позвонил бабушке». Раньше каждое из них стоило одного нажатия
    // «готово» — проверить это машиной нельзя, и сервер просто верил.
    const ctx = await withChild([PSY]);
    const { status, body } = await answer(ctx, {
      packId: PSY, itemId: 'psy-w01-practice-1',
      answer: { type: 'parent_verified', requested: true },
    });

    expect(status).toBe(201);
    expect(body).toMatchObject({ credits: 0, pendingApproval: true });

    const row = await prisma.attempt.findFirstOrThrow({ where: { childId: ctx.childId } });
    expect(row.status).toBe('pending_approval');
    expect(await prisma.ledgerEntry.count({ where: { reason: 'task_reward' } })).toBe(0);
  });

  it('родитель подтверждает — кредиты приходят по цене из пакета', async () => {
    const ctx = await withChild([PSY]);
    await answer(ctx, {
      packId: PSY, itemId: 'psy-w01-practice-1',
      answer: { type: 'parent_verified', requested: true },
    });

    const queue = (await app.inject({
      method: 'GET', url: '/admin/approvals', headers: ctx.auth,
    })).json() as { attempts: { id: string }[] };
    expect(queue.attempts).toHaveLength(1);

    const res = await app.inject({
      method: 'POST', url: `/admin/attempts/${queue.attempts[0]!.id}/approve`, headers: ctx.auth,
    });
    expect(res.json()).toMatchObject({ credits: 3 });

    const row = await prisma.attempt.findFirstOrThrow({ where: { childId: ctx.childId } });
    expect(row.status).toBe('approved');
    expect(row.credits).toBe(3);

    const entry = await prisma.ledgerEntry.findFirstOrThrow({ where: { reason: 'task_reward' } });
    expect(entry.amount).toBe(3);
    // Начисление должно указывать на попытку: иначе в журнале видно «+3»,
    // а за что — нет.
    expect(entry.refId).toBe(row.id);
  });

  it('подтверждённое второй раз не подтвердить', async () => {
    const ctx = await withChild([PSY]);
    await answer(ctx, {
      packId: PSY, itemId: 'psy-w01-practice-1',
      answer: { type: 'parent_verified', requested: true },
    });
    const queue = (await app.inject({
      method: 'GET', url: '/admin/approvals', headers: ctx.auth,
    })).json() as { attempts: { id: string }[] };
    const id = queue.attempts[0]!.id;

    const url = `/admin/attempts/${id}/approve`;
    expect((await app.inject({ method: 'POST', url, headers: ctx.auth })).statusCode).toBe(200);
    expect((await app.inject({ method: 'POST', url, headers: ctx.auth })).statusCode).toBe(404);
    expect(await prisma.ledgerEntry.count({ where: { reason: 'task_reward' } })).toBe(1);
  });

  it('отклонённое не платит и из очереди уходит', async () => {
    const ctx = await withChild([PSY]);
    await answer(ctx, {
      packId: PSY, itemId: 'psy-w01-practice-1',
      answer: { type: 'parent_verified', requested: true },
    });
    const queue = (await app.inject({
      method: 'GET', url: '/admin/approvals', headers: ctx.auth,
    })).json() as { attempts: { id: string }[] };

    await app.inject({
      method: 'POST', url: `/admin/attempts/${queue.attempts[0]!.id}/reject`, headers: ctx.auth,
    });

    const after = (await app.inject({
      method: 'GET', url: '/admin/approvals', headers: ctx.auth,
    })).json() as { attempts: unknown[] };
    expect(after.attempts).toHaveLength(0);
    expect(await prisma.ledgerEntry.count({ where: { reason: 'task_reward' } })).toBe(0);
  });

  it('чужой родитель не подтвердит', async () => {
    const ctx = await withChild([PSY]);
    await answer(ctx, {
      packId: PSY, itemId: 'psy-w01-practice-1',
      answer: { type: 'parent_verified', requested: true },
    });
    const queue = (await app.inject({
      method: 'GET', url: '/admin/approvals', headers: ctx.auth,
    })).json() as { attempts: { id: string }[] };

    const stranger = await withChild([PSY]);
    const id = queue.attempts[0]!.id;
    for (const verb of ['approve', 'reject']) {
      const res = await app.inject({
        method: 'POST', url: `/admin/attempts/${id}/${verb}`, headers: stranger.auth,
      });
      expect(res.statusCode, verb).toBe(404);
    }
    expect(await prisma.ledgerEntry.count({ where: { reason: 'task_reward' } })).toBe(0);
    // И не закрыл её чужому ребёнку: отклонить — тоже решение.
    const row = await prisma.attempt.findUniqueOrThrow({ where: { id } });
    expect(row.status).toBe('pending_approval');
  });
});

describe('пакеты на диске', () => {
  const content = new ContentService(contentRoot);

  it('цена, потолок и cooldown берутся из пакета', async () => {
    const terms = content.terms(PHYS, 'phys-mech-004');
    // У задания своя цена — она главнее общей по пакету.
    expect(terms.creditsPerCorrect).toBe(3);
    expect(terms.dailyCreditCap).toBe(40);
    expect(terms.cooldownHours).toBe(24);
  });

  it('из каталога заданий не выйти именем пакета', () => {
    // Проверять это против несуществующих путей бессмысленно: «../../../etc»
    // не откроется и без всякой защиты, и тест прошёл бы на пустом месте.
    // Поэтому рядом с каталогом кладём настоящий пакет — такой, который
    // открылся бы, если идентификатор просто подставить в путь.
    const tmp = mkdtempSync(join(tmpdir(), 'mykids-content-'));
    const manifest = {
      reward: { creditsPerCorrect: 50, dailyCreditCap: 500 },
      items: ['items/001.json'],
    };
    mkdirSync(join(tmp, 'снаружи', 'items'), { recursive: true });
    mkdirSync(join(tmp, 'packs'), { recursive: true });
    writeFileSync(join(tmp, 'снаружи', 'pack.json'), JSON.stringify(manifest));
    writeFileSync(
      join(tmp, 'снаружи', 'items', '001.json'),
      JSON.stringify({ id: 'чужое', type: 'numeric', stem: '?', answer: { value: 1 } }),
    );

    const guarded = new ContentService(join(tmp, 'packs'));
    for (const bad of ['../снаружи', 'ru.mykids/../../снаружи', '..', '/etc/passwd']) {
      expect(() => guarded.terms(bad, 'чужое'), bad).toThrow(ContentError);
    }
    rmSync(tmp, { recursive: true, force: true });
  });

  it('из каталога не выйти и манифестом пакета', () => {
    // Пути к заданиям берутся из файла на диске, а не из запроса, — но кладёт
    // этот файл в каталог человек, и «../../чужое.json» в манифесте не должно
    // значить ничего. Без проверки такой пакет открыл бы файл снаружи и
    // назначил себе цену в 50 кредитов.
    const tmp = mkdtempSync(join(tmpdir(), 'mykids-content-'));
    mkdirSync(join(tmp, 'packs', 'ru.test.pack'), { recursive: true });
    writeFileSync(join(tmp, 'packs', 'ru.test.pack', 'pack.json'), JSON.stringify({
      reward: { creditsPerCorrect: 50, dailyCreditCap: 500 },
      items: ['../../снаружи.json'],
    }));
    writeFileSync(
      join(tmp, 'снаружи.json'),
      JSON.stringify({ id: 'чужое', type: 'numeric', stem: '?', answer: { value: 1 } }),
    );

    const guarded = new ContentService(join(tmp, 'packs'));
    expect(() => guarded.terms('ru.test.pack', 'чужое')).toThrow(ContentError);
    rmSync(tmp, { recursive: true, force: true });
  });

  it('идентификатор задания в путь не подставляется вовсе', () => {
    // Он лежит внутри файла, а не в его имени: нужный файл ищется перебором
    // по манифесту. Поэтому подставить в путь через него нечего.
    expect(() => content.terms(PHYS, '../../../etc/passwd')).toThrow(ContentError);
    expect(() => content.terms(PHYS, 'items/001.json')).toThrow(ContentError);
  });

  it('без каталога пакетов кредиты не начисляются', () => {
    // Асимметрия: не увидев пакета, сервер обязан отказать. Начислить
    // «на всякий случай» стоило бы часов экрана, отказ — одной попытки.
    const blind = new ContentService(undefined);
    expect(() => blind.terms(PHYS, 'phys-mech-001')).toThrow(ContentError);
  });
});
