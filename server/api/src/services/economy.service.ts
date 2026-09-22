import type { LedgerDraft, Policy, StoreItem } from '@mykids/contracts';
import {
  awardTaskCredits,
  canPurchase,
  checkPace,
  defaultPaceConfig,
  defaultRepeatConfig,
  convertCredits,
  dailyGrant,
  evaluateScreen,
  localMoment,
  type DayState,
  type ScreenState,
} from '@mykids/domain';
import type { PrismaClient } from '@prisma/client';
import { toDomainPolicy } from '../mapping.js';
import { LedgerService } from './ledger.service.js';

/** Начало окна повторов: отметки старше него на награду уже не влияют. */
function windowStart(at: Date, days: number): Date {
  return new Date(at.getTime() - days * 24 * 60 * 60 * 1000);
}

export class NotFoundError extends Error {}
export class RuleError extends Error {
  constructor(readonly code: string, message: string) {
    super(message);
  }
}

/**
 * Экономика ребёнка. Все решения принимаются здесь, на сервере.
 *
 * Раннер и агент работают на устройстве ребёнка и считают то же самое только
 * чтобы сразу показать результат. Записывает начисления исключительно сервер,
 * пересчитывая их теми же функциями домена.
 */
export class EconomyService {
  private readonly ledger: LedgerService;

  constructor(private readonly prisma: PrismaClient) {
    this.ledger = new LedgerService(prisma);
  }

  async policyFor(childId: string): Promise<Policy> {
    const row = await this.prisma.policy.findUnique({ where: { childId } });
    if (!row) throw new NotFoundError(`политика ребёнка ${childId} не найдена`);
    return toDomainPolicy(row);
  }

  /** Состояние на текущие сутки: балансы плюс всё, к чему применяются потолки. */
  async dayState(childId: string, at: Date, policy: Policy): Promise<DayState & { day: string }> {
    const [balances, summary] = await Promise.all([
      this.ledger.balances(childId),
      this.ledger.daySummary(childId, at, policy.timezone),
    ]);
    return {
      creditsBalance: balances.credits,
      minutesBalance: balances.minutes,
      convertedMinutesToday: summary.convertedMinutesToday,
      creditsEarnedToday: summary.creditsEarnedToday,
      grantedMinutesToday: summary.grantedMinutesToday,
      day: summary.day,
    };
  }

  /**
   * Начисляет дневной лимит, если он ещё не выдан на эти сутки.
   * Идемпотентна: агент может звать её при каждом запуске.
   */
  async ensureDailyGrant(childId: string, at: Date): Promise<number> {
    const policy = await this.policyFor(childId);
    const moment = localMoment(at, policy.timezone);
    const state = await this.dayState(childId, at, policy);

    const unspentYesterday = await this.unspentYesterday(childId, at, policy);
    const grant = dailyGrant({
      policy,
      weekday: moment.weekday,
      state: { grantedMinutesToday: state.grantedMinutesToday },
      unspentYesterdayMinutes: unspentYesterday,
    });
    if (grant.minutes <= 0) return 0;

    // Перенос пишется отдельной строкой: в журнале должно быть видно, откуда
    // взялись минуты сверх дневного лимита.
    const drafts: LedgerDraft[] = [];
    const base = Math.min(grant.minutes, grant.base);
    if (base > 0) {
      drafts.push(this.serverDraft(childId, 'minutes', base, 'daily_grant', at));
    }
    const carry = grant.minutes - base;
    if (carry > 0) {
      drafts.push(this.serverDraft(childId, 'minutes', carry, 'carry_over', at));
    }
    await this.ledger.append(drafts);
    return grant.minutes;
  }

  /** Неистраченный остаток вчерашних суток. */
  private async unspentYesterday(childId: string, at: Date, policy: Policy): Promise<number> {
    const yesterday = new Date(at.getTime() - 24 * 3600 * 1000);
    const day = localMoment(yesterday, policy.timezone).day;
    const entries = await this.ledger.entriesOnLocalDay(childId, day, policy.timezone);
    const minutes = entries
      .filter((e) => e.currency === 'minutes')
      .reduce((sum, e) => sum + e.amount, 0);
    return Math.max(0, minutes);
  }

  /** Обмен кредитов на минуты. */
  async convert(childId: string, minutes: number, at: Date): Promise<{ minutes: number; creditsSpent: number }> {
    const policy = await this.policyFor(childId);
    const state = await this.dayState(childId, at, policy);

    const result = convertCredits(minutes, state, policy.economy);
    if (!result.ok) throw new RuleError(result.code, result.message);

    await this.ledger.append(
      result.drafts.map((d) =>
        this.serverDraft(childId, d.currency, d.amount, d.reason, at),
      ),
    );
    return { minutes: result.minutes, creditsSpent: result.creditsSpent };
  }

  /**
   * Начисляет кредиты за выполненное задание.
   *
   * Клиент присылает результат проверки, но сумму назначает сервер: потолки
   * пакета и суток, а также cooldown применяются здесь.
   */
  // Время попытки пишем из input.at, а не оставляем часам базы.
  //
  // Проверка темпа сравнивает момент нынешней попытки с моментами прошлых, и
  // два разных источника времени сделали бы это сравнение бессмысленным.
  // Безопасности это не трогает: at ставит сервер, из тела запроса он не
  // приходит — иначе ребёнок сам назначал бы себе промежутки.
  async awardTask(input: {
    childId: string;
    packId: string;
    itemId: string;
    score: number;
    baseCredits: number;
    packDailyCreditCap: number;
    cooldownHours?: number | undefined;
    at: Date;
  }): Promise<{ credits: number; withheldReason?: string; note?: string }> {
    const policy = await this.policyFor(input.childId);
    const state = await this.dayState(input.childId, input.at, policy);

    const [priorAwards, packToday, recentAttempts] = await Promise.all([
      // Не одна последняя отметка, а все за окно повторов: первая решает,
      // кончился ли cooldown, остальные — насколько урезать награду за то,
      // что это же задание уже решали недавно.
      //
      // Ограничение по дате здесь — чтобы не тащить всю историю задания за
      // год; отбрасывает старые отметки всё равно домен, и правило там одно.
      // Убрав это условие, поведения не изменишь, только запрос растолстеет.
      this.prisma.attempt.findMany({
        where: {
          childId: input.childId,
          itemId: input.itemId,
          credits: { gt: 0 },
          createdAt: { gte: windowStart(input.at, defaultRepeatConfig.windowDays) },
        },
        orderBy: { createdAt: 'desc' },
        select: { createdAt: true },
      }),
      this.creditsFromPackToday(input.childId, input.packId, input.at, policy.timezone),
      // Время прихода последних попыток — по часам сервера. Их и берёт
      // проверка темпа: длительность, измеренная браузером ребёнка, ничего не
      // стоит, а промежутки между приходами подделать нельзя, не замедлившись
      // на самом деле.
      //
      // Берём на одну больше порога серии: длиннее для решения не нужно.
      this.prisma.attempt.findMany({
        where: { childId: input.childId },
        orderBy: { createdAt: 'desc' },
        take: defaultPaceConfig.streak + 1,
        select: { createdAt: true },
      }),
    ]);

    // Темп проверяем до потолков: ответы, прокликанные наугад, не должны
    // съедать дневной лимит — иначе угадывание мешало бы ещё и честной работе
    // в тот же день.
    const pace = checkPace({
      previousAt: recentAttempts.map((a) => a.createdAt),
      now: input.at,
    });
    if (pace.tooFast) {
      await this.prisma.attempt.create({
        data: {
          childId: input.childId, packId: input.packId, itemId: input.itemId,
          score: input.score, credits: 0, status: 'graded', createdAt: input.at,
        },
      });
      return { credits: 0, withheldReason: pace.message };
    }

    const award = awardTaskCredits({
      credits: input.baseCredits,
      economy: policy.economy,
      state: { creditsEarnedToday: state.creditsEarnedToday },
      packDailyCreditCap: input.packDailyCreditCap,
      packCreditsToday: packToday,
      awardedAt: priorAwards.map((a) => a.createdAt),
      cooldownHours: input.cooldownHours,
      now: input.at,
    });

    if (!award.ok) {
      await this.prisma.attempt.create({
        data: {
          childId: input.childId, packId: input.packId, itemId: input.itemId,
          score: input.score, credits: 0, status: 'graded', createdAt: input.at,
        },
      });
      return { credits: 0, withheldReason: award.message };
    }

    // Попытка и запись журнала должны появиться вместе: иначе кредиты окажутся
    // начислены без следа о том, за что.
    await this.prisma.$transaction(async (tx) => {
      const attempt = await tx.attempt.create({
        data: {
          childId: input.childId, packId: input.packId, itemId: input.itemId,
          score: input.score, credits: award.credits, status: 'graded', createdAt: input.at,
        },
      });
      if (award.credits > 0) {
        await tx.ledgerEntry.create({
          data: {
            childId: input.childId, currency: 'credits', amount: award.credits,
            reason: 'task_reward', refType: 'attempt', refId: attempt.id,
            deviceId: null, occurredAt: input.at, seq: 0,
          },
        });
      }
    });
    return award.note ? { credits: award.credits, note: award.note } : { credits: award.credits };
  }

  private async creditsFromPackToday(
    childId: string, packId: string, at: Date, timezone: string,
  ): Promise<number> {
    const day = localMoment(at, timezone).day;
    const attempts = await this.prisma.attempt.findMany({
      where: { childId, packId, credits: { gt: 0 } },
      select: { credits: true, createdAt: true },
    });
    return attempts
      .filter((a) => localMoment(a.createdAt, timezone).day === day)
      .reduce((sum, a) => sum + a.credits, 0);
  }

  /** Покупка в магазине. */
  async purchase(childId: string, storeItemId: string, at: Date): Promise<{ purchaseId: string }> {
    const policy = await this.policyFor(childId);
    const state = await this.dayState(childId, at, policy);

    const row = await this.prisma.storeItem.findUnique({ where: { id: storeItemId } });
    if (!row) throw new NotFoundError(`товар ${storeItemId} не найден`);

    const item: StoreItem = {
      id: row.id, title: row.title,
      ...(row.description !== null ? { description: row.description } : {}),
      cost: { currency: row.costCurrency, amount: row.costAmount },
      effect: row.effect as StoreItem['effect'],
      ...(row.maxPerDay !== null ? { maxPerDay: row.maxPerDay } : {}),
      ...(row.maxPerWeek !== null ? { maxPerWeek: row.maxPerWeek } : {}),
      ...(row.cooldownHours !== null ? { cooldownHours: row.cooldownHours } : {}),
      requiresApproval: row.requiresApproval,
      enabled: row.enabled,
    };

    const history = await this.purchaseHistory(childId, storeItemId, at, policy.timezone);
    const check = canPurchase(item, state, history, at);
    if (!check.ok) throw new RuleError(check.code, check.message);

    const purchase = await this.prisma.$transaction(async (tx) => {
      const created = await tx.purchase.create({
        data: {
          childId, storeItemId,
          status: item.requiresApproval ? 'pending' : 'approved',
          cost: item.cost.amount, currency: item.cost.currency,
          ...(item.requiresApproval ? {} : { decidedAt: at }),
        },
      });
      // Цена списывается сразу, в том числе у покупок на одобрение: иначе те же
      // кредиты можно потратить дважды, пока родитель думает.
      await tx.ledgerEntry.create({
        data: {
          childId, currency: item.cost.currency, amount: -item.cost.amount,
          reason: 'purchase', refType: 'purchase', refId: created.id,
          deviceId: null, occurredAt: at, seq: 0,
        },
      });
      if (!item.requiresApproval && item.effect.kind === 'grant_minutes') {
        await tx.ledgerEntry.create({
          data: {
            childId, currency: 'minutes', amount: item.effect.minutes,
            reason: 'purchase_grant', refType: 'purchase', refId: created.id,
            deviceId: null, occurredAt: at, seq: 0,
          },
        });
      }
      return created;
    });
    return { purchaseId: purchase.id };
  }

  private async purchaseHistory(
    childId: string, storeItemId: string, at: Date, timezone: string,
  ): Promise<{ purchasesToday: number; purchasesThisWeek: number; lastPurchaseAt?: Date }> {
    const weekAgo = new Date(at.getTime() - 7 * 24 * 3600 * 1000);
    const rows = await this.prisma.purchase.findMany({
      where: { childId, storeItemId, status: { not: 'rejected' }, createdAt: { gte: weekAgo } },
      orderBy: { createdAt: 'desc' },
      select: { createdAt: true },
    });
    const day = localMoment(at, timezone).day;
    const last = rows[0]?.createdAt;
    return {
      purchasesToday: rows.filter((r) => localMoment(r.createdAt, timezone).day === day).length,
      purchasesThisWeek: rows.length,
      ...(last ? { lastPurchaseAt: last } : {}),
    };
  }

  /** Можно ли сейчас пользоваться экраном. Это и спрашивает агент. */
  async screenState(childId: string, at: Date): Promise<ScreenState> {
    const policy = await this.policyFor(childId);
    const balances = await this.ledger.balances(childId);
    return evaluateScreen(policy, balances.minutes, at);
  }

  private serverDraft(
    childId: string, currency: LedgerDraft['currency'], amount: number,
    reason: LedgerDraft['reason'], at: Date,
  ): LedgerDraft {
    return { childId, currency, amount, reason, deviceId: null, occurredAt: at, seq: 0 };
  }
}
