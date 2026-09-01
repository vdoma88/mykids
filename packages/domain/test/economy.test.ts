import { describe, expect, it } from 'vitest';
import type { EconomyConfig, Policy, StoreItem } from '@mykids/contracts';
import { awardTaskCredits, canPurchase, convertCredits, dailyGrant, type DayState } from '@mykids/domain';

const economy: EconomyConfig = {
  creditsPerMinute: 2,
  maxConvertedMinutesPerDay: 45,
  minCreditsToConvert: 10,
  maxCreditsPerDay: 120,
};

const state = (over: Partial<DayState> = {}): DayState => ({
  creditsBalance: 0,
  minutesBalance: 0,
  convertedMinutesToday: 0,
  creditsEarnedToday: 0,
  grantedMinutesToday: 0,
  ...over,
});

describe('конвертация кредитов в минуты', () => {
  it('списывает точную цену и начисляет запрошенные минуты', () => {
    const r = convertCredits(20, state({ creditsBalance: 100 }), economy);
    expect(r.ok).toBe(true);
    if (!r.ok) return;
    expect(r.minutes).toBe(20);
    expect(r.creditsSpent).toBe(40);
    expect(r.drafts).toEqual([
      { currency: 'credits', amount: -40, reason: 'conversion_spend' },
      { currency: 'minutes', amount: 20, reason: 'conversion_gain' },
    ]);
  });

  it('не даёт получить минуту дешевле курса через округление', () => {
    // 1 кредит при курсе 2 за минуту — это половина минуты. Округления вверх быть не должно.
    const r = convertCredits(1, state({ creditsBalance: 1 }), economy);
    expect(r.ok).toBe(false);
    if (r.ok) return;
    expect(r.code).toBe('insufficient_credits');
  });

  it('повторные мелкие обмены не создают минут из воздуха', () => {
    let credits = 9; // при курсе 2 это 4 полных минуты и один «висячий» кредит
    let minutes = 0;
    for (let i = 0; i < 20; i++) {
      const r = convertCredits(1, state({ creditsBalance: credits }), { ...economy, minCreditsToConvert: 0 });
      if (!r.ok) break;
      credits -= r.creditsSpent;
      minutes += r.minutes;
    }
    expect(minutes).toBe(4);
    expect(credits).toBe(1);
  });

  it('урезает выдачу до дневного потолка обмена', () => {
    const r = convertCredits(30, state({ creditsBalance: 1000, convertedMinutesToday: 40 }), economy);
    expect(r.ok).toBe(true);
    if (!r.ok) return;
    expect(r.minutes).toBe(5); // 45 - 40
    expect(r.creditsSpent).toBe(10);
  });

  it('отказывает, когда дневной потолок обмена исчерпан', () => {
    const r = convertCredits(10, state({ creditsBalance: 1000, convertedMinutesToday: 45 }), economy);
    expect(r.ok).toBe(false);
    if (r.ok) return;
    expect(r.code).toBe('daily_cap_reached');
  });

  it('урезает выдачу до того, что реально по карману', () => {
    const r = convertCredits(30, state({ creditsBalance: 25 }), economy);
    expect(r.ok).toBe(true);
    if (!r.ok) return;
    expect(r.minutes).toBe(12); // floor(25 / 2)
    expect(r.creditsSpent).toBe(24);
  });

  it('соблюдает минимальный размер обмена', () => {
    const r = convertCredits(2, state({ creditsBalance: 100 }), economy); // 4 кредита < 10
    expect(r.ok).toBe(false);
    if (r.ok) return;
    expect(r.code).toBe('below_minimum');
  });

  it('отвергает дробные и неположительные запросы', () => {
    for (const bad of [0, -5, 1.5, NaN, Infinity]) {
      const r = convertCredits(bad, state({ creditsBalance: 1000 }), economy);
      expect(r.ok).toBe(false);
      if (!r.ok) expect(r.code).toBe('invalid_amount');
    }
  });
});

describe('дневная выдача минут', () => {
  const policy = {
    timezone: 'Europe/Moscow',
    dailyLimitMinutes: [120, 60, 60, 60, 60, 90, 120], // вс..сб
    carryOverMaxMinutes: 30,
    windows: [],
    economy,
  } satisfies Policy;

  it('выдаёт лимит дня недели плюс перенос', () => {
    const r = dailyGrant({ policy, weekday: 1, state: state(), unspentYesterdayMinutes: 20 });
    expect(r).toEqual({ minutes: 80, base: 60, carryOver: 20 });
  });

  it('ограничивает перенос потолком', () => {
    const r = dailyGrant({ policy, weekday: 1, state: state(), unspentYesterdayMinutes: 500 });
    expect(r.carryOver).toBe(30);
    expect(r.minutes).toBe(90);
  });

  it('идемпотентна: повторный вызов в тех же сутках ничего не добавляет', () => {
    const first = dailyGrant({ policy, weekday: 1, state: state(), unspentYesterdayMinutes: 0 });
    const second = dailyGrant({
      policy, weekday: 1,
      state: state({ grantedMinutesToday: first.minutes }),
      unspentYesterdayMinutes: 0,
    });
    expect(first.minutes).toBe(60);
    expect(second.minutes).toBe(0);
  });

  it('не уходит в минус, если лимит успели снизить после выдачи', () => {
    const r = dailyGrant({ policy, weekday: 1, state: state({ grantedMinutesToday: 200 }), unspentYesterdayMinutes: 0 });
    expect(r.minutes).toBe(0);
  });

  it('отрицательный остаток вчера не съедает сегодняшний лимит', () => {
    const r = dailyGrant({ policy, weekday: 1, state: state(), unspentYesterdayMinutes: -50 });
    expect(r.carryOver).toBe(0);
    expect(r.minutes).toBe(60);
  });
});

describe('начисление кредитов за задания', () => {
  const base = {
    credits: 10,
    economy,
    state: state(),
    packDailyCreditCap: 40,
    packCreditsToday: 0,
    now: new Date('2026-03-09T12:00:00Z'),
  };

  it('начисляет полную сумму, когда лимиты не мешают', () => {
    const r = awardTaskCredits(base);
    expect(r.ok && r.credits).toBe(10);
  });

  it('урезает до остатка потолка пакета, а не отклоняет целиком', () => {
    const r = awardTaskCredits({ ...base, packCreditsToday: 36 });
    expect(r.ok && r.credits).toBe(4);
  });

  it('урезает до остатка общего дневного потолка', () => {
    const r = awardTaskCredits({ ...base, state: state({ creditsEarnedToday: 115 }) });
    expect(r.ok && r.credits).toBe(5);
  });

  it('отказывает при исчерпанном потолке пакета', () => {
    const r = awardTaskCredits({ ...base, packCreditsToday: 40 });
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.code).toBe('pack_cap_reached');
  });

  it('отказывает при исчерпанном общем потолке', () => {
    const r = awardTaskCredits({ ...base, state: state({ creditsEarnedToday: 120 }) });
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.code).toBe('daily_cap_reached');
  });

  it('не даёт перерешать задание до истечения cooldown', () => {
    const r = awardTaskCredits({
      ...base,
      lastAwardedAt: new Date('2026-03-09T10:00:00Z'),
      cooldownHours: 24,
    });
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.code).toBe('cooldown_active');
  });

  it('снова засчитывает задание после истечения cooldown', () => {
    const r = awardTaskCredits({
      ...base,
      lastAwardedAt: new Date('2026-03-08T11:00:00Z'),
      cooldownHours: 24,
    });
    expect(r.ok && r.credits).toBe(10);
  });
});

describe('покупки в магазине', () => {
  const item: StoreItem = {
    id: 'extra-30',
    title: '+30 минут',
    cost: { currency: 'credits', amount: 60 },
    effect: { kind: 'grant_minutes', minutes: 30 },
    maxPerDay: 2,
    maxPerWeek: 5,
    cooldownHours: 3,
    requiresApproval: false,
    enabled: true,
  };
  const now = new Date('2026-03-09T18:00:00Z');
  const history = { purchasesToday: 0, purchasesThisWeek: 0 };

  it('пропускает покупку при достаточном балансе', () => {
    const r = canPurchase(item, state({ creditsBalance: 60 }), history, now);
    expect(r.ok && r.cost).toBe(60);
  });

  it('отказывает при нехватке средств', () => {
    const r = canPurchase(item, state({ creditsBalance: 59 }), history, now);
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.code).toBe('insufficient_funds');
  });

  it('смотрит на баланс той валюты, в которой указана цена', () => {
    const inMinutes: StoreItem = { ...item, cost: { currency: 'minutes', amount: 10 } };
    const r = canPurchase(inMinutes, state({ creditsBalance: 1000, minutesBalance: 5 }), history, now);
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.code).toBe('insufficient_funds');
  });

  it('соблюдает дневной и недельный лимиты и cooldown', () => {
    const rich = state({ creditsBalance: 1000 });
    expect(canPurchase(item, rich, { ...history, purchasesToday: 2 }, now)).toMatchObject({ code: 'daily_limit_reached' });
    expect(canPurchase(item, rich, { ...history, purchasesThisWeek: 5 }, now)).toMatchObject({ code: 'weekly_limit_reached' });
    expect(canPurchase(item, rich, { ...history, lastPurchaseAt: new Date('2026-03-09T16:00:00Z') }, now))
      .toMatchObject({ code: 'cooldown_active' });
  });

  it('не продаёт выключенный товар', () => {
    const r = canPurchase({ ...item, enabled: false }, state({ creditsBalance: 1000 }), history, now);
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.code).toBe('item_disabled');
  });
});
