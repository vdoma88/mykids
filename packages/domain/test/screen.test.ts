import { describe, expect, it } from 'vitest';
import type { Policy } from '@mykids/contracts';
import { evaluateScreen } from '@mykids/domain';

const policy: Policy = {
  timezone: 'Europe/Moscow',
  dailyLimitMinutes: [120, 60, 60, 60, 60, 90, 120],
  carryOverMaxMinutes: 30,
  windows: [
    { name: 'школа', days: [1, 2, 3, 4, 5], from: '08:00', to: '14:00', mode: 'blocked' },
    { name: 'отбой', days: [0, 1, 2, 3, 4, 5, 6], from: '21:30', to: '07:00', mode: 'blocked' },
    { name: 'уроки', days: [1, 2, 3, 4, 5], from: '16:00', to: '18:00', mode: 'tasks_only' },
  ],
  economy: {
    creditsPerMinute: 2, maxConvertedMinutesPerDay: 45,
    minCreditsToConvert: 10, maxCreditsPerDay: 120,
  },
};

// Москва — UTC+3, поэтому из локального времени вычитаем 3 часа.
const msk = (iso: string) => new Date(iso);

describe('доступность экрана', () => {
  it('разрешает при положительном балансе вне окон', () => {
    const r = evaluateScreen(policy, 40, msk('2026-03-09T16:00:00Z')); // 19:00 пн
    expect(r).toEqual({ allowed: true, reason: 'within_allowance', minutesLeft: 40 });
  });

  it('блокирует в школьные часы даже при полном балансе', () => {
    const r = evaluateScreen(policy, 120, msk('2026-03-09T07:00:00Z')); // 10:00 пн
    expect(r).toMatchObject({ allowed: false, reason: 'window_blocked', window: 'школа' });
  });

  it('окно важнее баланса: купленные минуты не отменяют отбой', () => {
    const r = evaluateScreen(policy, 500, msk('2026-03-09T19:00:00Z')); // 22:00 пн
    expect(r).toMatchObject({ allowed: false, reason: 'window_blocked', window: 'отбой' });
  });

  it('ночное окно продолжает действовать после полуночи', () => {
    const r = evaluateScreen(policy, 500, msk('2026-03-09T23:00:00Z')); // 02:00 вт
    expect(r).toMatchObject({ allowed: false, window: 'отбой' });
  });

  it('в окне «только задания» сообщает об этом отдельно от блокировки', () => {
    const r = evaluateScreen(policy, 60, msk('2026-03-09T14:00:00Z')); // 17:00 пн
    expect(r).toMatchObject({ allowed: false, reason: 'tasks_only', window: 'уроки' });
  });

  it('явное разрешающее окно перекрывает блокирующее', () => {
    const withClub: Policy = {
      ...policy,
      windows: [
        ...policy.windows,
        { name: 'кружок', days: [1], from: '09:00', to: '11:00', mode: 'allowed' },
      ],
    };
    const r = evaluateScreen(withClub, 30, msk('2026-03-09T07:00:00Z')); // 10:00 пн
    expect(r).toMatchObject({ allowed: true });
  });

  it('при нулевом и отрицательном балансе экран закрыт', () => {
    expect(evaluateScreen(policy, 0, msk('2026-03-09T16:00:00Z'))).toMatchObject({ reason: 'out_of_minutes' });
    expect(evaluateScreen(policy, -5, msk('2026-03-09T16:00:00Z'))).toMatchObject({ reason: 'out_of_minutes' });
  });
});
