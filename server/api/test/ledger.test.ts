import { afterAll, beforeEach, describe, expect, it } from 'vitest';
import { LedgerService } from '../src/services/ledger.service.js';
import { prisma, resetDb, seedFamily } from './helpers.js';

const ledger = new LedgerService(prisma);

beforeEach(resetDb);
afterAll(async () => { await prisma.$disconnect(); });

describe('журнал операций', () => {
  it('баланс считается свёрткой, а не хранится полем', async () => {
    const { childId } = await seedFamily();
    const at = new Date('2026-03-09T12:00:00Z');
    await ledger.append([
      { childId, currency: 'minutes', amount: 60, reason: 'daily_grant', deviceId: null, occurredAt: at, seq: 0 },
      { childId, currency: 'minutes', amount: -25, reason: 'screen_usage', deviceId: null, occurredAt: at, seq: 0 },
      { childId, currency: 'credits', amount: 40, reason: 'task_reward', deviceId: null, occurredAt: at, seq: 0 },
    ]);
    expect(await ledger.balances(childId)).toEqual({ minutes: 35, credits: 40 });
  });

  it('повторная отправка очереди агента не удваивает списание', async () => {
    const { childId, deviceId } = await seedFamily();
    const at = new Date('2026-03-09T12:00:00Z');
    const batch = [
      { childId, currency: 'minutes' as const, amount: -5, reason: 'screen_usage' as const, deviceId, occurredAt: at, seq: 1 },
      { childId, currency: 'minutes' as const, amount: -5, reason: 'screen_usage' as const, deviceId, occurredAt: at, seq: 2 },
    ];
    expect(await ledger.append(batch)).toBe(2);
    // Агент не получил подтверждения и отправил то же самое ещё раз
    expect(await ledger.append(batch)).toBe(0);
    expect(await ledger.balance(childId, 'minutes')).toBe(-10);
  });

  it('записи сервера не дедуплицируются: у них нет счётчика устройства', async () => {
    const { childId } = await seedFamily();
    const at = new Date('2026-03-09T12:00:00Z');
    const draft = { childId, currency: 'credits' as const, amount: 5, reason: 'manual_adjust' as const, deviceId: null, occurredAt: at, seq: 0 };
    await ledger.append([draft]);
    await ledger.append([draft]);
    expect(await ledger.balance(childId, 'credits')).toBe(10);
  });

  it('сутки нарезаются по часам сервера, а не устройства', async () => {
    const { childId, deviceId } = await seedFamily();
    // Устройство утверждает, что это вчера — часы подкручены назад
    await prisma.ledgerEntry.create({
      data: {
        childId, currency: 'minutes', amount: -7, reason: 'screen_usage', deviceId,
        occurredAt: new Date('2026-03-08T12:00:00Z'),
        recordedAt: new Date('2026-03-09T12:00:00Z'), seq: 1,
      },
    });
    const today = await ledger.entriesOnLocalDay(childId, '2026-03-09', 'UTC');
    expect(today).toHaveLength(1);
    expect(today[0]?.amount).toBe(-7);
    expect(await ledger.entriesOnLocalDay(childId, '2026-03-08', 'UTC')).toHaveLength(0);
  });

  it('сводка за сутки разделяет выдачу, обмен и заработок', async () => {
    const { childId } = await seedFamily();
    // recordedAt проставляет сервер, поэтому сутки берутся от текущего момента,
    // а не от occurredAt — в этом и смысл защиты от подкрутки часов.
    const at = new Date();
    await ledger.append([
      { childId, currency: 'minutes', amount: 60, reason: 'daily_grant', deviceId: null, occurredAt: at, seq: 0 },
      { childId, currency: 'minutes', amount: 15, reason: 'carry_over', deviceId: null, occurredAt: at, seq: 0 },
      { childId, currency: 'minutes', amount: 10, reason: 'conversion_gain', deviceId: null, occurredAt: at, seq: 0 },
      { childId, currency: 'credits', amount: 30, reason: 'task_reward', deviceId: null, occurredAt: at, seq: 0 },
      { childId, currency: 'credits', amount: -20, reason: 'conversion_spend', deviceId: null, occurredAt: at, seq: 0 },
    ]);
    const s = await ledger.daySummary(childId, at, 'UTC');
    expect(s).toMatchObject({
      day: at.toISOString().slice(0, 10),
      grantedMinutesToday: 75, convertedMinutesToday: 10, creditsEarnedToday: 30,
    });
  });

  it('occurredAt из прошлого не переносит запись в прошлые сутки', async () => {
    const { childId, deviceId } = await seedFamily();
    // Агент с переведёнными назад часами пытается «открыть новый день»
    await ledger.append([{
      childId, currency: 'credits', amount: 99, reason: 'task_reward',
      deviceId, occurredAt: new Date('2020-01-01T00:00:00Z'), seq: 7,
    }]);
    const today = new Date().toISOString().slice(0, 10);
    const s = await ledger.daySummary(childId, new Date(), 'UTC');
    expect(s.day).toBe(today);
    expect(s.creditsEarnedToday).toBe(99);
    expect(await ledger.entriesOnLocalDay(childId, '2020-01-01', 'UTC')).toHaveLength(0);
  });

  it('удаление ребёнка уносит его журнал', async () => {
    const { childId } = await seedFamily();
    await ledger.append([{
      childId, currency: 'credits', amount: 5, reason: 'manual_adjust',
      deviceId: null, occurredAt: new Date(), seq: 0,
    }]);
    await prisma.child.delete({ where: { id: childId } });
    expect(await prisma.ledgerEntry.count({ where: { childId } })).toBe(0);
  });
});
