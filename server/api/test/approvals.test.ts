import { fileURLToPath } from 'node:url';
import { afterAll, beforeEach, describe, expect, it } from 'vitest';
import type { FastifyInstance } from 'fastify';
import type { PrismaClient } from '@prisma/client';
import { buildApp } from '../src/app.js';
import { ContentService } from '../src/services/content.service.js';
import { EconomyService } from '../src/services/economy.service.js';
import { prisma, resetDb } from './helpers.js';

/**
 * Очередь одобрений.
 *
 * Покупка «на одобрение» списывала цену сразу — и дальше висела навсегда:
 * одобрить или отклонить её было нечем. Ребёнок платил и не получал ничего,
 * а родитель видел очередь, с которой ничего не мог сделать.
 */
const contentRoot = fileURLToPath(new URL('../../../content/packs', import.meta.url));
const app: FastifyInstance = buildApp(prisma, { contentRoot });

beforeEach(resetDb);
afterAll(async () => { await app.close(); await prisma.$disconnect(); });

interface Ctx { auth: Record<string, string>; device: Record<string, string>; childId: string }

async function family(): Promise<Ctx> {
  const reg = await app.inject({
    method: 'POST', url: '/auth/register',
    payload: { familyName: 'Семья', email: `p${Math.random()}@example.com`, password: 'очень-длинный-пароль' },
  });
  const auth = { authorization: `Bearer ${(reg.json() as { token: string }).token}` };
  const child = await app.inject({ method: 'POST', url: '/admin/children', headers: auth, payload: { name: 'Марк' } });
  const childId = (child.json() as { id: string }).id;
  const dev = await app.inject({
    method: 'POST', url: `/admin/children/${childId}/devices`, headers: auth,
    payload: { platform: 'windows', name: 'ПК' },
  });
  const device = { authorization: `Bearer ${(dev.json() as { token: string }).token}` };
  await app.inject({
    method: 'POST', url: `/admin/children/${childId}/adjust`, headers: auth,
    payload: { currency: 'credits', amount: 500, note: 'на покупки' },
  });
  return { auth, device, childId };
}

async function storeItem(ctx: Ctx, over: Record<string, unknown> = {}): Promise<string> {
  const res = await app.inject({
    method: 'POST', url: '/admin/store', headers: ctx.auth,
    payload: {
      title: '+30 минут', cost: { currency: 'credits', amount: 60 },
      effect: { kind: 'grant_minutes', minutes: 30 }, requiresApproval: true, enabled: true, ...over,
    },
  });
  return (res.json() as { id: string }).id;
}

async function buy(ctx: Ctx, itemId: string): Promise<string> {
  const res = await app.inject({
    method: 'POST', url: '/child/purchases', headers: ctx.device, payload: { storeItemId: itemId },
  });
  expect(res.statusCode, res.body).toBe(201);
  return (res.json() as { purchaseId: string }).purchaseId;
}

async function balances(ctx: Ctx): Promise<{ minutes: number; credits: number }> {
  const res = await app.inject({ method: 'GET', url: '/child/me', headers: ctx.device });
  return (res.json() as { balances: { minutes: number; credits: number } }).balances;
}

describe('покупки на одобрение', () => {
  it('одобрение выдаёт эффект, а цену второй раз не берёт', async () => {
    const ctx = await family();
    const id = await buy(ctx, await storeItem(ctx));
    const before = await balances(ctx);
    // Цена списана сразу, минут ещё нет.
    expect(before.credits).toBe(440);

    const res = await app.inject({ method: 'POST', url: `/admin/purchases/${id}/approve`, headers: ctx.auth });
    expect(res.statusCode).toBe(204);

    const after = await balances(ctx);
    expect(after.credits).toBe(440);
    expect(after.minutes - before.minutes).toBe(30);
    expect((await prisma.purchase.findUniqueOrThrow({ where: { id } })).status).toBe('approved');
  });

  it('отклонение возвращает цену отдельной строкой журнала', async () => {
    const ctx = await family();
    const id = await buy(ctx, await storeItem(ctx));

    await app.inject({ method: 'POST', url: `/admin/purchases/${id}/reject`, headers: ctx.auth });

    expect((await balances(ctx)).credits).toBe(500);
    const refund = await prisma.ledgerEntry.findFirstOrThrow({ where: { reason: 'purchase_refund' } });
    expect(refund.amount).toBe(60);
    // Возврат ссылается на покупку: через месяц видно, откуда эти кредиты.
    expect(refund.refId).toBe(id);
    expect((await prisma.purchase.findUniqueOrThrow({ where: { id } })).status).toBe('rejected');
  });

  it('двойной щелчок не выдаёт эффект дважды', async () => {
    // Два запроса подряд оба проходят проверку «ждёт ли покупка», если
    // проверка и смена статуса — два разных шага.
    const ctx = await family();
    const id = await buy(ctx, await storeItem(ctx));
    const url = `/admin/purchases/${id}/approve`;
    const [a, b] = await Promise.all([
      app.inject({ method: 'POST', url, headers: ctx.auth }),
      app.inject({ method: 'POST', url, headers: ctx.auth }),
    ]);
    expect([a.statusCode, b.statusCode].sort()).toEqual([204, 404]);
    expect(await prisma.ledgerEntry.count({ where: { reason: 'purchase_grant' } })).toBe(1);
  });

  it('двойной возврат не возвращает цену дважды', async () => {
    const ctx = await family();
    const id = await buy(ctx, await storeItem(ctx));
    const url = `/admin/purchases/${id}/reject`;
    await Promise.all([
      app.inject({ method: 'POST', url, headers: ctx.auth }),
      app.inject({ method: 'POST', url, headers: ctx.auth }),
    ]);
    expect(await prisma.ledgerEntry.count({ where: { reason: 'purchase_refund' } })).toBe(1);
    expect((await balances(ctx)).credits).toBe(500);
  });

  it('чужой родитель не одобрит и не вернёт', async () => {
    const ctx = await family();
    const id = await buy(ctx, await storeItem(ctx));
    const stranger = await family();
    for (const verb of ['approve', 'reject']) {
      const res = await app.inject({
        method: 'POST', url: `/admin/purchases/${id}/${verb}`, headers: stranger.auth,
      });
      expect(res.statusCode, verb).toBe(404);
    }
    expect((await prisma.purchase.findUniqueOrThrow({ where: { id } })).status).toBe('pending');
  });
});

describe('подтверждение заданий', () => {
  it('двойной щелчок не платит дважды', async () => {
    const ctx = await family();
    await app.inject({
      method: 'PUT', url: `/admin/children/${ctx.childId}/packs`, headers: ctx.auth,
      payload: { packs: ['ru.mykids.psychology.week01.attention'] },
    });
    await app.inject({
      method: 'POST', url: '/child/attempts', headers: ctx.device,
      payload: {
        packId: 'ru.mykids.psychology.week01.attention', itemId: 'psy-w01-practice-1',
        answer: { type: 'parent_verified', requested: true },
      },
    });
    const attempt = await prisma.attempt.findFirstOrThrow({ where: { status: 'pending_approval' } });
    const url = `/admin/attempts/${attempt.id}/approve`;
    await Promise.all([
      app.inject({ method: 'POST', url, headers: ctx.auth }),
      app.inject({ method: 'POST', url, headers: ctx.auth }),
    ]);
    expect(await prisma.ledgerEntry.count({ where: { reason: 'task_reward' } })).toBe(1);
  });

  it('подтверждение, которое опередили, не платит', async () => {
    // Параллельные запросы этого не ловят: второй запрос обычно видит уже
    // записанное начисление и упирается в cooldown задания — то есть защищает
    // случайное совпадение, а не условие на статус. Здесь «другой запрос»
    // подтверждает задание ровно между проверкой и записью, всегда.
    const ctx = await family();
    await app.inject({
      method: 'PUT', url: `/admin/children/${ctx.childId}/packs`, headers: ctx.auth,
      payload: { packs: ['ru.mykids.psychology.week01.attention'] },
    });
    await app.inject({
      method: 'POST', url: '/child/attempts', headers: ctx.device,
      payload: {
        packId: 'ru.mykids.psychology.week01.attention', itemId: 'psy-w01-practice-1',
        answer: { type: 'parent_verified', requested: true },
      },
    });
    const attempt = await prisma.attempt.findFirstOrThrow({ where: { status: 'pending_approval' } });
    const { familyId } = await prisma.child.findUniqueOrThrow({ where: { id: ctx.childId } });

    const bind = <T extends object>(target: T, prop: string | symbol): unknown => {
      const v = Reflect.get(target, prop) as unknown;
      return typeof v === 'function' ? (v as (...a: unknown[]) => unknown).bind(target) : v;
    };
    const racing = new Proxy(prisma, {
      get(target, prop) {
        if (prop !== 'attempt') return bind(target, prop);
        return new Proxy(target.attempt, {
          get(a, p) {
            if (p !== 'findFirst') return bind(a, p);
            return async (args: Parameters<typeof a.findFirst>[0]) => {
              const row = await a.findFirst(args);
              if (row) await prisma.attempt.update({ where: { id: row.id }, data: { status: 'approved' } });
              return row;
            };
          },
        });
      },
    });
    const economy = new EconomyService(racing as PrismaClient, new ContentService(contentRoot));

    await expect(economy.approveAttempt(attempt.id, familyId, new Date())).rejects.toThrow(/уже разобрано/);
    expect(await prisma.ledgerEntry.count({ where: { reason: 'task_reward' } })).toBe(0);
  });

  it('родителю видно текст задания, а не его идентификатор', async () => {
    const ctx = await family();
    await app.inject({
      method: 'PUT', url: `/admin/children/${ctx.childId}/packs`, headers: ctx.auth,
      payload: { packs: ['ru.mykids.psychology.week01.attention'] },
    });
    await app.inject({
      method: 'POST', url: '/child/attempts', headers: ctx.device,
      payload: {
        packId: 'ru.mykids.psychology.week01.attention', itemId: 'psy-w01-practice-1',
        answer: { type: 'parent_verified', requested: true },
      },
    });
    const queue = (await app.inject({ method: 'GET', url: '/admin/approvals', headers: ctx.auth }))
      .json() as { attempts: { stem: string | null; packTitle: string | null }[] };
    expect(queue.attempts[0]?.stem).toBe('Договориться о 3 правилах группы');
    expect(queue.attempts[0]?.packTitle).toBeTruthy();
  });
});

describe('права «только чтение»', () => {
  it('наблюдатель не подтверждает задания и не разбирает покупки', async () => {
    // Раньше подтверждение задания не проверяло роль вовсе: родитель с
    // правами только на чтение мог начислять кредиты.
    const ctx = await family();
    const id = await buy(ctx, await storeItem(ctx));
    await app.inject({
      method: 'POST', url: '/auth/guardians', headers: ctx.auth,
      payload: { email: 'viewer@example.com', password: 'очень-длинный-пароль', role: 'viewer' },
    });
    const login = await app.inject({
      method: 'POST', url: '/auth/login',
      payload: { email: 'viewer@example.com', password: 'очень-длинный-пароль' },
    });
    const viewer = { authorization: `Bearer ${(login.json() as { token: string }).token}` };

    for (const url of [
      `/admin/purchases/${id}/approve`, `/admin/purchases/${id}/reject`,
      '/admin/attempts/00000000-0000-4000-8000-000000000000/approve',
      '/admin/attempts/00000000-0000-4000-8000-000000000000/reject',
    ]) {
      const res = await app.inject({ method: 'POST', url, headers: viewer });
      expect(res.statusCode, url).toBe(403);
    }
  });
});

describe('товары магазина', () => {
  it('выключенный товар пропадает у ребёнка и не покупается', async () => {
    const ctx = await family();
    const itemId = await storeItem(ctx, { requiresApproval: false });

    const off = await app.inject({
      method: 'PATCH', url: `/admin/store/${itemId}`, headers: ctx.auth, payload: { enabled: false },
    });
    expect(off.statusCode).toBe(200);

    const shelf = (await app.inject({ method: 'GET', url: '/child/store', headers: ctx.device }))
      .json() as { id: string }[];
    expect(shelf.map((i) => i.id)).not.toContain(itemId);

    const res = await app.inject({
      method: 'POST', url: '/child/purchases', headers: ctx.device, payload: { storeItemId: itemId },
    });
    expect(res.statusCode).toBeGreaterThanOrEqual(400);
  });

  it('чужой товар не выключить', async () => {
    const ctx = await family();
    const itemId = await storeItem(ctx);
    const stranger = await family();
    const res = await app.inject({
      method: 'PATCH', url: `/admin/store/${itemId}`, headers: stranger.auth, payload: { enabled: false },
    });
    expect(res.statusCode).toBe(404);
  });
});

describe('страница ребёнка', () => {
  it('знает имя ребёнка', async () => {
    const ctx = await family();
    const res = await app.inject({ method: 'GET', url: `/admin/children/${ctx.childId}`, headers: ctx.auth });
    expect(res.json()).toMatchObject({ name: 'Марк', balances: { credits: 500 } });
  });

  it('чужого ребёнка не показывает', async () => {
    const ctx = await family();
    const stranger = await family();
    const res = await app.inject({ method: 'GET', url: `/admin/children/${ctx.childId}`, headers: stranger.auth });
    expect(res.statusCode).toBe(404);
  });
});

describe('сохранение правил', () => {
  it('не стирает белый список агента', async () => {
    // Админка этот список не редактирует и раньше присылала пустой — и сервер
    // стирал «explorer.exe» и сам агент из «всегда разрешённого» при первой
    // же правке лимитов.
    const ctx = await family();
    const before = (await app.inject({ method: 'GET', url: '/agent/sync', headers: ctx.device }))
      .json() as { agent: { alwaysAllowed: string[] } };
    expect(before.agent.alwaysAllowed).toContain('explorer.exe');

    const policy = (await app.inject({
      method: 'GET', url: `/admin/children/${ctx.childId}/policy`, headers: ctx.auth,
    })).json() as Record<string, unknown>;
    const res = await app.inject({
      method: 'PUT', url: `/admin/children/${ctx.childId}/policy`, headers: ctx.auth,
      payload: { ...policy, dailyLimitMinutes: [90, 60, 60, 60, 60, 60, 90] },
    });
    expect(res.statusCode, res.body).toBe(200);

    const after = (await app.inject({ method: 'GET', url: '/agent/sync', headers: ctx.device }))
      .json() as { agent: { alwaysAllowed: string[] } };
    expect(after.agent.alwaysAllowed).toEqual(before.agent.alwaysAllowed);
  });

  it('ошибка в данных приходит фразой, а не дампом разбора', async () => {
    const ctx = await family();
    const policy = (await app.inject({
      method: 'GET', url: `/admin/children/${ctx.childId}/policy`, headers: ctx.auth,
    })).json() as Record<string, unknown>;
    const res = await app.inject({
      method: 'PUT', url: `/admin/children/${ctx.childId}/policy`, headers: ctx.auth,
      payload: { ...policy, windows: [{ name: 'Отбой', days: [], from: '21:00', to: '07:00', mode: 'blocked' }] },
    });
    expect(res.statusCode).toBe(400);
    const body = res.json() as { message: string };
    expect(body.message).toContain('windows.0.days');
    expect(body.message).not.toContain('[');
  });
});
