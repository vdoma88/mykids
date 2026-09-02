import { describe, expect, it } from 'vitest';
import type { LedgerEntry } from '@mykids/contracts';
import { balanceOf, dedupeByDeviceSeq, entriesOnDay, sumByReason } from '@mykids/domain';

let counter = 0;
const entry = (over: Partial<LedgerEntry>): LedgerEntry => ({
  id: `e${++counter}`,
  childId: 'child-1',
  currency: 'minutes',
  amount: 0,
  reason: 'manual_adjust',
  deviceId: null,
  occurredAt: new Date('2026-03-09T12:00:00Z'),
  recordedAt: new Date('2026-03-09T12:00:00Z'),
  seq: 0,
  ...over,
});

describe('баланс как свёртка журнала', () => {
  it('складывает записи только своей валюты', () => {
    const entries = [
      entry({ currency: 'minutes', amount: 60, reason: 'daily_grant' }),
      entry({ currency: 'minutes', amount: -25, reason: 'screen_usage' }),
      entry({ currency: 'credits', amount: 40, reason: 'task_reward' }),
    ];
    expect(balanceOf(entries, 'minutes')).toBe(35);
    expect(balanceOf(entries, 'credits')).toBe(40);
  });

  it('пустой журнал даёт нулевой баланс', () => {
    expect(balanceOf([], 'minutes')).toBe(0);
  });

  it('допускает отрицательный баланс минут: экран мог отработать в офлайне', () => {
    const entries = [
      entry({ amount: 10, reason: 'daily_grant' }),
      entry({ amount: -18, reason: 'screen_usage' }),
    ];
    expect(balanceOf(entries, 'minutes')).toBe(-8);
  });
});

describe('отбор записей за сутки', () => {
  it('идёт по часам сервера, а не по часам устройства', () => {
    const entries = [
      // Устройство утверждает, что это вчера — часы подкручены назад.
      entry({
        amount: 5,
        occurredAt: new Date('2026-03-08T12:00:00Z'),
        recordedAt: new Date('2026-03-09T12:00:00Z'),
        deviceId: 'dev-1',
      }),
      entry({ amount: 7, recordedAt: new Date('2026-03-08T12:00:00Z') }),
    ];
    const today = entriesOnDay(entries, '2026-03-09', 'UTC');
    expect(today).toHaveLength(1);
    expect(today[0]?.amount).toBe(5);
  });

  it('учитывает часовой пояс при нарезке суток', () => {
    const late = entry({ amount: 3, recordedAt: new Date('2026-03-09T22:30:00Z') });
    expect(entriesOnDay([late], '2026-03-09', 'UTC')).toHaveLength(1);
    expect(entriesOnDay([late], '2026-03-10', 'Europe/Moscow')).toHaveLength(1);
  });
});

describe('sumByReason', () => {
  it('суммирует только указанные причины', () => {
    const entries = [
      entry({ currency: 'credits', amount: 10, reason: 'task_reward' }),
      entry({ currency: 'credits', amount: 5, reason: 'task_reward' }),
      entry({ currency: 'credits', amount: -12, reason: 'conversion_spend' }),
    ];
    expect(sumByReason(entries, 'credits', ['task_reward'])).toBe(15);
    expect(sumByReason(entries, 'credits', ['task_reward', 'conversion_spend'])).toBe(3);
    expect(sumByReason(entries, 'minutes', ['task_reward'])).toBe(0);
  });
});

describe('дедупликация повторной синхронизации', () => {
  it('схлопывает записи с одинаковой парой устройство+счётчик', () => {
    const dup = { deviceId: 'dev-1', seq: 7, amount: -5, reason: 'screen_usage' as const };
    const result = dedupeByDeviceSeq([entry(dup), entry(dup), entry({ ...dup, seq: 8 })]);
    expect(result).toHaveLength(2);
    expect(balanceOf(result, 'minutes')).toBe(-10);
  });

  it('не схлопывает серверные записи: у них нет счётчика устройства', () => {
    const server = { deviceId: null, seq: 0, amount: 10, reason: 'manual_adjust' as const };
    expect(dedupeByDeviceSeq([entry(server), entry(server)])).toHaveLength(2);
  });
});
