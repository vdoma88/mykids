import { afterAll, beforeEach, describe, expect, it } from 'vitest';
import type { FastifyInstance } from 'fastify';
import { buildApp } from '../src/app.js';
import { prisma, resetDb } from './helpers.js';

const app: FastifyInstance = buildApp(prisma);

beforeEach(resetDb);
afterAll(async () => { await app.close(); await prisma.$disconnect(); });

const creds = { familyName: 'Семья', email: 'p@example.com', password: 'очень-длинный-пароль' };

/** Регистрирует родителя и возвращает заголовок авторизации. */
async function asGuardian(): Promise<{ auth: Record<string, string>; familyId: string }> {
  const res = await app.inject({ method: 'POST', url: '/auth/register', payload: creds });
  const body = res.json() as { token: string; familyId: string };
  return { auth: { authorization: `Bearer ${body.token}` }, familyId: body.familyId };
}

/** Создаёт ребёнка и устройство, возвращает оба заголовка. */
async function withChild(): Promise<{
  auth: Record<string, string>; device: Record<string, string>; childId: string;
}> {
  const { auth } = await asGuardian();
  const child = await app.inject({
    method: 'POST', url: '/admin/children', headers: auth, payload: { name: 'Марк' },
  });
  const childId = (child.json() as { id: string }).id;
  const dev = await app.inject({
    method: 'POST', url: `/admin/children/${childId}/devices`,
    headers: auth, payload: { platform: 'windows', name: 'ПК' },
  });
  const token = (dev.json() as { token: string }).token;
  return { auth, device: { authorization: `Bearer ${token}` }, childId };
}

describe('здоровье и доступ', () => {
  it('health отвечает без авторизации', async () => {
    const res = await app.inject({ method: 'GET', url: '/health' });
    expect(res.statusCode).toBe(200);
  });

  it('админка без токена отдаёт 401', async () => {
    const res = await app.inject({ method: 'GET', url: '/admin/children' });
    expect(res.statusCode).toBe(401);
    expect(res.json()).toMatchObject({ error: 'unauthorized' });
  });

  it('токен устройства не пускает в админку', async () => {
    const { device } = await withChild();
    const res = await app.inject({ method: 'GET', url: '/admin/children', headers: device });
    expect(res.statusCode).toBe(401);
  });

  it('токен родителя не пускает в детские маршруты', async () => {
    const { auth } = await withChild();
    const res = await app.inject({ method: 'GET', url: '/child/me', headers: auth });
    expect(res.statusCode).toBe(401);
  });
});

describe('регистрация и вход', () => {
  it('регистрация сразу выдаёт сессию', async () => {
    const res = await app.inject({ method: 'POST', url: '/auth/register', payload: creds });
    expect(res.statusCode).toBe(201);
    expect(res.json()).toMatchObject({ token: expect.any(String) });
  });

  it('короткий пароль отклоняется с 400', async () => {
    const res = await app.inject({
      method: 'POST', url: '/auth/register', payload: { ...creds, password: 'мало' },
    });
    expect(res.statusCode).toBe(400);
  });

  it('неверный пароль даёт 401', async () => {
    await asGuardian();
    const res = await app.inject({
      method: 'POST', url: '/auth/login', payload: { email: creds.email, password: 'не-тот' },
    });
    expect(res.statusCode).toBe(401);
    expect(res.json()).toMatchObject({ error: 'invalid_credentials' });
  });

  it('выход убивает сессию', async () => {
    const { auth } = await asGuardian();
    expect((await app.inject({ method: 'POST', url: '/auth/logout', headers: auth })).statusCode).toBe(204);
    expect((await app.inject({ method: 'GET', url: '/auth/me', headers: auth })).statusCode).toBe(401);
  });
});

describe('изоляция семей', () => {
  it('чужой ребёнок не виден и не редактируется', async () => {
    const { childId } = await withChild();
    const other = await app.inject({
      method: 'POST', url: '/auth/register',
      payload: { ...creds, email: 'other@example.com' },
    });
    const otherAuth = { authorization: `Bearer ${(other.json() as { token: string }).token}` };

    expect((await app.inject({ method: 'GET', url: '/admin/children', headers: otherAuth })).json()).toEqual([]);
    const res = await app.inject({
      method: 'GET', url: `/admin/children/${childId}/policy`, headers: otherAuth,
    });
    expect(res.statusCode).toBe(404);
  });
});

describe('ребёнок и агент', () => {
  it('новый ребёнок получает политику по умолчанию с непустым белым списком', async () => {
    const { auth, childId } = await withChild();
    const res = await app.inject({ method: 'GET', url: `/admin/children/${childId}/policy`, headers: auth });
    expect(res.statusCode).toBe(200);
    const policy = res.json() as { dailyLimitMinutes: number[] };
    expect(policy.dailyLimitMinutes).toHaveLength(7);

    const row = await prisma.policy.findUniqueOrThrow({ where: { childId } });
    // Ребёнок должен иметь возможность позвонить родителю при нулевом балансе
    expect(row.alwaysAllowed.length).toBeGreaterThan(0);
  });

  it('sync выдаёт дневной лимит и не повторяет выдачу', async () => {
    const { device } = await withChild();
    const first = await app.inject({ method: 'GET', url: '/agent/sync', headers: device });
    expect(first.statusCode).toBe(200);
    const b1 = (first.json() as { balances: { minutes: number } }).balances.minutes;
    expect(b1).toBeGreaterThan(0);

    const second = await app.inject({ method: 'GET', url: '/agent/sync', headers: device });
    expect((second.json() as { balances: { minutes: number } }).balances.minutes).toBe(b1);
  });

  it('sync отдаёт агенту белый список приложений', async () => {
    const { device } = await withChild();
    const res = await app.inject({ method: 'GET', url: '/agent/sync', headers: device });
    const body = res.json() as { agent: { alwaysAllowed: string[] } };
    // Без него ребёнок не позвонит родителю при нулевом балансе
    expect(body.agent.alwaysAllowed.length).toBeGreaterThan(0);
  });

  it('повторная отправка расхода не удваивает списание', async () => {
    const { device } = await withChild();
    await app.inject({ method: 'GET', url: '/agent/sync', headers: device });
    const payload = {
      agentVersion: '0.1.1',
      entries: [
        { seq: 1, minutes: 5, occurredAt: new Date().toISOString() },
        { seq: 2, minutes: 5, occurredAt: new Date().toISOString() },
      ],
    };
    const first = await app.inject({ method: 'POST', url: '/agent/usage', headers: device, payload });
    expect(first.json()).toMatchObject({ accepted: 2, duplicates: 0 });

    const retry = await app.inject({ method: 'POST', url: '/agent/usage', headers: device, payload });
    expect(retry.json()).toMatchObject({ accepted: 0, duplicates: 2 });
  });

  it('повторное сообщение о вмешательстве не ломается о счётчик устройства', async () => {
    const { device } = await withChild();
    // Подкрутку часов ребёнок может повторить хоть десять раз подряд, и каждая
    // обязана записаться: событий вмешательства, а не одно на устройство.
    for (const detail of ['часы переведены назад на 3h', 'часы переведены вперёд на 2h']) {
      const res = await app.inject({
        method: 'POST', url: '/agent/tamper', headers: device,
        payload: { kind: 'clock', detail },
      });
      expect(res.statusCode).toBe(201);
    }
  });

  it('вмешательство не трогает баланс, пока родитель не назначил штраф', async () => {
    const { device } = await withChild();
    const before = (await app.inject({ method: 'GET', url: '/agent/sync', headers: device }))
      .json() as { balances: { minutes: number } };

    await app.inject({
      method: 'POST', url: '/agent/tamper', headers: device,
      payload: { kind: 'clock', detail: 'часы переведены назад на 3h' },
    });

    const after = (await app.inject({ method: 'GET', url: '/agent/sync', headers: device }))
      .json() as { balances: { minutes: number } };
    // Величину штрафа решает родитель; сам факт минут не списывает
    expect(after.balances.minutes).toBe(before.balances.minutes);
  });

  it('родитель видит события вмешательства и может их закрыть', async () => {
    const { auth, device, childId } = await withChild();
    for (const detail of ['часы назад на 3h', 'часы вперёд на 2h']) {
      await app.inject({
        method: 'POST', url: '/agent/tamper', headers: device, payload: { kind: 'clock', detail },
      });
    }

    const list = await app.inject({
      method: 'GET', url: `/admin/children/${childId}/tampers`, headers: auth,
    });
    expect(list.statusCode).toBe(200);
    const body = list.json() as { pending: number; events: { id: string; kind: string }[] };
    expect(body.pending).toBe(2);
    expect(body.events[0]!.kind).toBe('clock');

    const review = await app.inject({
      method: 'POST', url: `/admin/children/${childId}/tampers/review`, headers: auth,
      payload: { ids: body.events.map((e) => e.id) },
    });
    expect(review.json()).toMatchObject({ reviewed: 2, pending: 0 });

    const after = await app.inject({
      method: 'GET', url: `/admin/children/${childId}/tampers?unreviewed=true`, headers: auth,
    });
    expect((after.json() as { events: unknown[] }).events).toHaveLength(0);
  });

  it('чужие события вмешательства закрыть нельзя', async () => {
    const theirs = await withChild();
    // Второй родитель — другая семья: тот же адрес почты занят первым.
    const other = await app.inject({
      method: 'POST', url: '/auth/register', payload: { ...creds, email: 'other@example.com' },
    });
    const mineAuth = { authorization: `Bearer ${(other.json() as { token: string }).token}` };
    const mineChild = await app.inject({
      method: 'POST', url: '/admin/children', headers: mineAuth, payload: { name: 'Аня' },
    });
    const mine = { auth: mineAuth, childId: (mineChild.json() as { id: string }).id };

    await app.inject({
      method: 'POST', url: '/agent/tamper', headers: theirs.device, payload: { kind: 'clock' },
    });
    const list = await app.inject({
      method: 'GET', url: `/admin/children/${theirs.childId}/tampers`, headers: theirs.auth,
    });
    const ids = (list.json() as { events: { id: string }[] }).events.map((e) => e.id);

    // Подставив чужой идентификатор события под своего ребёнка, родитель не
    // должен закрыть его: условие обязано держаться и на childId.
    const res = await app.inject({
      method: 'POST', url: `/admin/children/${mine.childId}/tampers/review`, headers: mine.auth,
      payload: { ids },
    });
    expect(res.json()).toMatchObject({ reviewed: 0 });
    const still = await app.inject({
      method: 'GET', url: `/admin/children/${theirs.childId}/tampers`, headers: theirs.auth,
    });
    expect((still.json() as { pending: number }).pending).toBe(1);
  });

  it('ребёнок видит свои правила, а не только баланс', async () => {
    const { device } = await withChild();
    const res = await app.inject({ method: 'GET', url: '/child/me', headers: device });
    expect(res.statusCode).toBe(200);
    // Скрытая механика провоцирует искать обход вместо игры по правилам
    expect(res.json()).toMatchObject({ name: 'Марк', policy: expect.any(Object) });
  });

  it('кредиты за задание назначает сервер, а не клиент', async () => {
    const { device } = await withChild();
    const res = await app.inject({
      method: 'POST', url: '/child/attempts', headers: device,
      payload: {
        packId: 'ru.test', itemId: 'i1', score: 1,
        baseCredits: 10, packDailyCreditCap: 12,
      },
    });
    expect(res.json()).toMatchObject({ credits: 10 });

    // Второе задание упрётся в потолок пакета — сервер урежет до остатка
    const second = await app.inject({
      method: 'POST', url: '/child/attempts', headers: device,
      payload: { packId: 'ru.test', itemId: 'i2', score: 1, baseCredits: 10, packDailyCreditCap: 12 },
    });
    expect(second.json()).toMatchObject({ credits: 2 });
  });

  it('обмен без кредитов отдаёт 409 с кодом правила', async () => {
    const { device } = await withChild();
    const res = await app.inject({
      method: 'POST', url: '/child/convert', headers: device, payload: { minutes: 10 },
    });
    expect(res.statusCode).toBe(409);
    expect(res.json()).toMatchObject({ error: 'insufficient_credits' });
  });
});

describe('правка политики', () => {
  it('сохраняется целиком и переживает перечитывание', async () => {
    const { auth, childId } = await withChild();
    const current = (await app.inject({
      method: 'GET', url: `/admin/children/${childId}/policy`, headers: auth,
    })).json() as Record<string, unknown>;

    const res = await app.inject({
      method: 'PUT', url: `/admin/children/${childId}/policy`, headers: auth,
      payload: {
        ...current,
        dailyLimitMinutes: [10, 45, 20, 20, 20, 30, 40],
        // Поле есть в базе, но не входит в доменную схему: раньше strict-схема
        // на нём падала и политику вообще нельзя было сохранить из админки.
        alwaysAllowed: ['explorer.exe', 'chrome.exe'],
        windows: [{ name: 'отбой', days: [1, 2, 3], from: '21:30', to: '07:00', mode: 'blocked' }],
      },
    });
    expect(res.statusCode).toBe(200);

    const again = (await app.inject({
      method: 'GET', url: `/admin/children/${childId}/policy`, headers: auth,
    })).json() as { dailyLimitMinutes: number[]; windows: unknown[] };
    expect(again.dailyLimitMinutes[1]).toBe(45);
    expect(again.windows).toHaveLength(1);

    const row = await prisma.policy.findUniqueOrThrow({ where: { childId } });
    expect(row.alwaysAllowed).toEqual(['explorer.exe', 'chrome.exe']);
  });

  it('отвергает битое окно расписания', async () => {
    const { auth, childId } = await withChild();
    const current = (await app.inject({
      method: 'GET', url: `/admin/children/${childId}/policy`, headers: auth,
    })).json() as Record<string, unknown>;
    const res = await app.inject({
      method: 'PUT', url: `/admin/children/${childId}/policy`, headers: auth,
      payload: { ...current, alwaysAllowed: [], windows: [{ name: 'кривое', days: [1], from: '25:00', to: '07:00', mode: 'blocked' }] },
    });
    expect(res.statusCode).toBe(400);
  });
});

describe('ручная корректировка', () => {
  it('требует комментарий и попадает в журнал', async () => {
    const { auth, childId } = await withChild();
    const bad = await app.inject({
      method: 'POST', url: `/admin/children/${childId}/adjust`,
      headers: auth, payload: { currency: 'credits', amount: 10 },
    });
    expect(bad.statusCode).toBe(400);

    const ok = await app.inject({
      method: 'POST', url: `/admin/children/${childId}/adjust`,
      headers: auth, payload: { currency: 'credits', amount: 10, note: 'за помощь по дому' },
    });
    expect(ok.statusCode).toBe(201);
    const row = await prisma.ledgerEntry.findFirstOrThrow({ where: { childId, reason: 'manual_adjust' } });
    expect(row.note).toBe('за помощь по дому');
  });
});

describe('отзыв устройства', () => {
  it('отозванный агент перестаёт синхронизироваться', async () => {
    const { auth, device, childId } = await withChild();
    const list = await app.inject({ method: 'GET', url: '/admin/children', headers: auth });
    const deviceId = (list.json() as { id: string; devices: { id: string }[] }[])
      .find((c) => c.id === childId)!.devices[0]!.id;

    expect((await app.inject({ method: 'DELETE', url: `/admin/devices/${deviceId}`, headers: auth })).statusCode).toBe(204);
    expect((await app.inject({ method: 'GET', url: '/agent/sync', headers: device })).statusCode).toBe(401);
  });
});
