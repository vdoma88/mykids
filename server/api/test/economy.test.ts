import { afterAll, beforeEach, describe, expect, it } from 'vitest';
import { EconomyService, RuleError } from '../src/services/economy.service.js';
import { LedgerService } from '../src/services/ledger.service.js';
import { giveCredits, prisma, resetDb, seedFamily } from './helpers.js';

const economy = new EconomyService(prisma);
const ledger = new LedgerService(prisma);

beforeEach(resetDb);
afterAll(async () => { await prisma.$disconnect(); });

describe('дневная выдача', () => {
  it('выдаёт лимит и не повторяет выдачу в те же сутки', async () => {
    const { childId } = await seedFamily();
    const at = new Date();
    expect(await economy.ensureDailyGrant(childId, at)).toBe(60);
    expect(await economy.ensureDailyGrant(childId, at)).toBe(0);
    expect(await ledger.balance(childId, 'minutes')).toBe(60);
  });

  it('перенос пишется отдельной строкой журнала', async () => {
    const { childId } = await seedFamily({ carryOverMaxMinutes: 30 });
    const at = new Date();
    const yesterday = new Date(at.getTime() - 24 * 3600 * 1000);
    // Вчера осталось 20 неистраченных минут
    await prisma.ledgerEntry.create({
      data: {
        childId, currency: 'minutes', amount: 20, reason: 'daily_grant',
        deviceId: null, occurredAt: yesterday, recordedAt: yesterday, seq: 0,
      },
    });
    expect(await economy.ensureDailyGrant(childId, at)).toBe(80);

    const rows = await prisma.ledgerEntry.findMany({
      where: { childId, reason: { in: ['daily_grant', 'carry_over'] }, recordedAt: { gte: at } },
    });
    const byReason = Object.fromEntries(rows.map((r) => [r.reason, r.amount]));
    expect(byReason).toEqual({ daily_grant: 60, carry_over: 20 });
  });

  it('перенос ограничен потолком', async () => {
    const { childId } = await seedFamily({ carryOverMaxMinutes: 15 });
    const at = new Date();
    const yesterday = new Date(at.getTime() - 24 * 3600 * 1000);
    await prisma.ledgerEntry.create({
      data: {
        childId, currency: 'minutes', amount: 500, reason: 'daily_grant',
        deviceId: null, occurredAt: yesterday, recordedAt: yesterday, seq: 0,
      },
    });
    expect(await economy.ensureDailyGrant(childId, at)).toBe(75); // 60 + 15
  });
});

describe('обмен кредитов на минуты', () => {
  it('списывает точную цену и начисляет минуты', async () => {
    const { childId } = await seedFamily();
    await giveCredits(childId, 100);
    const r = await economy.convert(childId, 20, new Date());
    expect(r).toEqual({ minutes: 20, creditsSpent: 40 });
    expect(await ledger.balances(childId)).toEqual({ minutes: 20, credits: 60 });
  });

  it('повторные мелкие обмены не создают минут из воздуха', async () => {
    const { childId } = await seedFamily({ minCreditsToConvert: 0 });
    await giveCredits(childId, 9); // при курсе 2 это 4 полных минуты
    for (let i = 0; i < 10; i++) {
      try { await economy.convert(childId, 1, new Date()); } catch { break; }
    }
    expect(await ledger.balances(childId)).toEqual({ minutes: 4, credits: 1 });
  });

  it('соблюдает дневной потолок обмена', async () => {
    const { childId } = await seedFamily({ maxConvertedMinutesPerDay: 10 });
    await giveCredits(childId, 1000);
    expect((await economy.convert(childId, 30, new Date())).minutes).toBe(10);
    await expect(economy.convert(childId, 5, new Date())).rejects.toThrow(RuleError);
  });

  it('отказывает при нехватке кредитов', async () => {
    const { childId } = await seedFamily();
    await giveCredits(childId, 1);
    await expect(economy.convert(childId, 1, new Date())).rejects.toMatchObject({ code: 'insufficient_credits' });
  });
});

describe('начисление за задания', () => {
  const task = (childId: string, over = {}) => ({
    childId, packId: 'ru.test.pack', itemId: 'item-1', score: 1,
    baseCredits: 10, packDailyCreditCap: 40, at: new Date(), ...over,
  });

  it('начисляет и записывает попытку вместе с журналом', async () => {
    const { childId } = await seedFamily();
    expect(await economy.awardTask(task(childId))).toEqual({ credits: 10 });
    expect(await ledger.balance(childId, 'credits')).toBe(10);

    const attempt = await prisma.attempt.findFirstOrThrow({ where: { childId } });
    const entry = await prisma.ledgerEntry.findFirstOrThrow({ where: { childId, reason: 'task_reward' } });
    expect(entry.refId).toBe(attempt.id); // начисление связано с тем, за что дано
  });

  it('урезает до остатка потолка пакета', async () => {
    const { childId } = await seedFamily();
    // Задания решаются по-человечески, с паузами: иначе сработает проверка
    // темпа, и потолок пакета проверить не удастся — до него дело не дойдёт.
    const start = new Date('2026-09-22T12:00:00Z');
    const after = (minutes: number) => new Date(start.getTime() + minutes * 60_000);
    for (let i = 0; i < 4; i++) {
      await economy.awardTask(task(childId, { itemId: `item-${i}`, at: after(i) }));
    }
    // 40 из 40 выбрано, следующее задание ничего не принесёт
    const r = await economy.awardTask(task(childId, { itemId: 'item-9', at: after(4) }));
    expect(r.credits).toBe(0);
    expect(r.withheldReason).toContain('набору');
  });

  it('cooldown не даёт перерешать одно задание', async () => {
    const { childId } = await seedFamily();
    await economy.awardTask(task(childId, { cooldownHours: 24 }));
    const again = await economy.awardTask(task(childId, { cooldownHours: 24 }));
    expect(again.credits).toBe(0);
    expect(again.withheldReason).toContain('через');
    expect(await ledger.balance(childId, 'credits')).toBe(10);
  });

  it('прокликанный наугад пакет не приносит кредитов', async () => {
    // Десять заданий с четырьмя вариантами ответа можно прокликать за
    // полминуты: четверть попаданий случайна, а кредиты за них начислялись бы
    // наравне с настоящими.
    const { childId } = await seedFamily();
    const start = new Date('2026-09-22T12:00:00Z');
    const results = [];
    for (let i = 0; i < 6; i++) {
      results.push(await economy.awardTask(task(childId, {
        itemId: `item-${i}`,
        at: new Date(start.getTime() + i * 1500), // полтора секунды на задание
      })));
    }
    const withheld = results.filter((r) => r.credits === 0);
    expect(withheld.length).toBeGreaterThan(0);
    expect(withheld[0]!.withheldReason).toContain('угаданное');
  });

  it('быстрые ответы не съедают дневной лимит', async () => {
    // Иначе угадывание мешало бы ещё и честной работе в тот же день: потолок
    // выбран, а заработано ничего.
    const { childId } = await seedFamily();
    const start = new Date('2026-09-22T12:00:00Z');
    for (let i = 0; i < 6; i++) {
      await economy.awardTask(task(childId, { itemId: `item-${i}`, at: new Date(start.getTime() + i * 1500) }));
    }
    // Одумался и решил по-человечески — кредиты идут.
    const slow = await economy.awardTask(task(childId, {
      itemId: 'item-slow', at: new Date(start.getTime() + 10 * 60_000),
    }));
    expect(slow.credits).toBe(10);
  });

  it('один быстрый верный ответ засчитывается', async () => {
    // Ребёнок, который знает ответ, отвечает быстро. Это цель, а не нарушение.
    const { childId } = await seedFamily();
    const start = new Date('2026-09-22T12:00:00Z');
    await economy.awardTask(task(childId, { itemId: 'item-a', at: start }));
    const quick = await economy.awardTask(task(childId, {
      itemId: 'item-b', at: new Date(start.getTime() + 1000),
    }));
    expect(quick.credits).toBe(10);
  });

  it('отклонённая по темпу попытка остаётся в журнале', async () => {
    // Родитель должен видеть, что происходило, а не только итог.
    const { childId } = await seedFamily();
    const start = new Date('2026-09-22T12:00:00Z');
    for (let i = 0; i < 6; i++) {
      await economy.awardTask(task(childId, { itemId: `item-${i}`, at: new Date(start.getTime() + i * 1500) }));
    }
    const attempts = await prisma.attempt.findMany({ where: { childId } });
    expect(attempts).toHaveLength(6);
    expect(attempts.filter((a) => a.credits === 0).length).toBeGreaterThan(0);
  });

  it('то же задание назавтра приносит вдвое меньше', async () => {
    // Cooldown не даёт перерешать сегодня, но завтра задание снова стоило бы
    // полной награды — и так бесконечно. Выгоднее всего было бы решать одни и
    // те же несколько заданий, которые уже знаешь наизусть.
    const { childId } = await seedFamily();
    const day1 = new Date('2026-09-20T12:00:00Z');
    const day2 = new Date('2026-09-21T13:00:00Z');

    const first = await economy.awardTask(task(childId, { at: day1, cooldownHours: 24 }));
    expect(first.credits).toBe(10);

    const second = await economy.awardTask(task(childId, { at: day2, cooldownHours: 24 }));
    expect(second.credits).toBe(5);
    expect(second.note).toContain('второй раз');
  });

  it('третье решение стоит ещё меньше второго', async () => {
    // Одной прошлой отметки для этого мало: с ней второй и третий раз
    // неразличимы, и служба, шлющая в домен только последнюю, прошла бы
    // проверку. Нужны три дня подряд.
    const { childId } = await seedFamily();
    const day = (n: number) => new Date(`2026-09-2${n}T12:00:00Z`);

    expect((await economy.awardTask(task(childId, { at: day(0), cooldownHours: 24 }))).credits).toBe(10);
    expect((await economy.awardTask(task(childId, { at: day(1), cooldownHours: 24 }))).credits).toBe(5);

    const third = await economy.awardTask(task(childId, { at: day(2), cooldownHours: 24 }));
    expect(third.credits).toBe(3);
    expect(third.note).toContain('третий раз');
  });

  it('задание, забытое на месяц, снова стоит полной награды', async () => {
    // Возврат после долгого перерыва — самое полезное повторение из
    // возможных, и наказывать за него было бы ровно наоборот.
    const { childId } = await seedFamily();
    await economy.awardTask(task(childId, { at: new Date('2026-07-01T12:00:00Z') }));
    const later = await economy.awardTask(task(childId, { at: new Date('2026-09-22T12:00:00Z') }));
    expect(later.credits).toBe(10);
    expect(later.note).toBeUndefined();
  });

  it('разные задания друг друга не удешевляют', async () => {
    // Затухание привязано к заданию, а не к ребёнку: решать новое всегда
    // выгоднее, чем повторять старое, — в этом весь смысл.
    const { childId } = await seedFamily();
    const start = new Date('2026-09-22T12:00:00Z');
    for (let i = 0; i < 3; i++) {
      const r = await economy.awardTask(task(childId, {
        itemId: `item-${i}`, at: new Date(start.getTime() + i * 60_000),
      }));
      expect(r.credits).toBe(10);
    }
  });

  it('неудачная попытка всё равно фиксируется', async () => {
    const { childId } = await seedFamily();
    await economy.awardTask(task(childId, { cooldownHours: 24 }));
    await economy.awardTask(task(childId, { cooldownHours: 24 }));
    expect(await prisma.attempt.count({ where: { childId } })).toBe(2);
  });

  it('соблюдает общий дневной потолок кредитов', async () => {
    const { childId } = await seedFamily({ maxCreditsPerDay: 25 });
    await economy.awardTask(task(childId, { itemId: 'a', packDailyCreditCap: 500 }));
    await economy.awardTask(task(childId, { itemId: 'b', packDailyCreditCap: 500 }));
    const third = await economy.awardTask(task(childId, { itemId: 'c', packDailyCreditCap: 500 }));
    expect(third.credits).toBe(5); // 25 - 20
    expect(await ledger.balance(childId, 'credits')).toBe(25);
  });
});

describe('магазин', () => {
  async function seedItem(familyId: string, over = {}) {
    return prisma.storeItem.create({
      data: {
        familyId, title: '+30 минут', costCurrency: 'credits', costAmount: 60,
        effect: { kind: 'grant_minutes', minutes: 30 }, ...over,
      },
    });
  }

  it('списывает цену и выдаёт эффект', async () => {
    const { familyId, childId } = await seedFamily();
    const item = await seedItem(familyId);
    await giveCredits(childId, 100);
    await economy.purchase(childId, item.id, new Date());
    expect(await ledger.balances(childId)).toEqual({ minutes: 30, credits: 40 });
  });

  it('отказывает при нехватке средств', async () => {
    const { familyId, childId } = await seedFamily();
    const item = await seedItem(familyId);
    await giveCredits(childId, 59);
    await expect(economy.purchase(childId, item.id, new Date()))
      .rejects.toMatchObject({ code: 'insufficient_funds' });
    expect(await ledger.balance(childId, 'credits')).toBe(59); // ничего не списано
  });

  it('покупка на одобрение списывает цену сразу, но не даёт эффект', async () => {
    const { familyId, childId } = await seedFamily();
    const item = await seedItem(familyId, { requiresApproval: true });
    await giveCredits(childId, 100);
    await economy.purchase(childId, item.id, new Date());
    // Иначе те же кредиты можно потратить дважды, пока родитель думает
    expect(await ledger.balances(childId)).toEqual({ minutes: 0, credits: 40 });
    expect(await prisma.purchase.findFirstOrThrow({ where: { childId } })).toMatchObject({ status: 'pending' });
  });

  it('соблюдает дневной лимит покупок', async () => {
    const { familyId, childId } = await seedFamily();
    const item = await seedItem(familyId, { maxPerDay: 1 });
    await giveCredits(childId, 500);
    await economy.purchase(childId, item.id, new Date());
    await expect(economy.purchase(childId, item.id, new Date()))
      .rejects.toMatchObject({ code: 'daily_limit_reached' });
  });

  it('не продаёт выключенный товар', async () => {
    const { familyId, childId } = await seedFamily();
    const item = await seedItem(familyId, { enabled: false });
    await giveCredits(childId, 500);
    await expect(economy.purchase(childId, item.id, new Date()))
      .rejects.toMatchObject({ code: 'item_disabled' });
  });
});

describe('доступность экрана', () => {
  it('расписание важнее баланса', async () => {
    const { childId } = await seedFamily({
      windows: [{ name: 'отбой', days: [0, 1, 2, 3, 4, 5, 6], from: '00:00', to: '23:59', mode: 'blocked' }],
    });
    await economy.ensureDailyGrant(childId, new Date());
    const state = await economy.screenState(childId, new Date());
    expect(state).toMatchObject({ allowed: false, reason: 'window_blocked', window: 'отбой' });
  });

  it('при нулевом балансе экран закрыт', async () => {
    const { childId } = await seedFamily();
    expect(await economy.screenState(childId, new Date()))
      .toMatchObject({ allowed: false, reason: 'out_of_minutes' });
  });

  it('с выданным лимитом и вне окон экран открыт', async () => {
    const { childId } = await seedFamily();
    await economy.ensureDailyGrant(childId, new Date());
    expect(await economy.screenState(childId, new Date()))
      .toMatchObject({ allowed: true, minutesLeft: 60 });
  });
});
