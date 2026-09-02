import type { EconomyConfig, LedgerDraft, Policy, StoreItem } from '@mykids/contracts';
import { hoursBetween } from './time.js';

export type Failure<C extends string> = { ok: false; code: C; message: string };
export type Success<T> = { ok: true } & T;
export type Result<T, C extends string> = Success<T> | Failure<C>;

function fail<C extends string>(code: C, message: string): Failure<C> {
  return { ok: false, code, message };
}

/** Состояние ребёнка на текущие локальные сутки — свёртка журнала. */
export interface DayState {
  creditsBalance: number;
  minutesBalance: number;
  /** Минут, уже полученных конвертацией сегодня. */
  convertedMinutesToday: number;
  /** Кредитов, уже заработанных сегодня из всех источников. */
  creditsEarnedToday: number;
  /** Минут дневного лимита, уже выданных сегодня. */
  grantedMinutesToday: number;
}

// ---------------------------------------------------------------- выдача лимита

export interface GrantInput {
  policy: Policy;
  weekday: number;
  state: Pick<DayState, 'grantedMinutesToday'>;
  /** Неистраченный остаток вчерашнего дня. */
  unspentYesterdayMinutes: number;
}

/**
 * Сколько минут доначислить на сегодня. Идемпотентна: повторный вызов в тех же
 * сутках вернёт ноль, поэтому агент может звать её при каждом запуске.
 */
export function dailyGrant(input: GrantInput): { minutes: number; base: number; carryOver: number } {
  const { policy, weekday, state, unspentYesterdayMinutes } = input;
  const base = policy.dailyLimitMinutes[weekday] ?? 0;
  const carryOver = Math.max(0, Math.min(unspentYesterdayMinutes, policy.carryOverMaxMinutes));
  const total = base + carryOver;
  return {
    minutes: Math.max(0, total - state.grantedMinutesToday),
    base,
    carryOver,
  };
}

// ------------------------------------------------------------------ конвертация

export type ConversionError =
  | 'invalid_amount'
  | 'daily_cap_reached'
  | 'below_minimum'
  | 'insufficient_credits';

export interface ConversionResult {
  minutes: number;
  creditsSpent: number;
  drafts: Omit<LedgerDraft, 'childId' | 'deviceId' | 'seq' | 'occurredAt'>[];
}

/**
 * Обмен кредитов на минуты.
 *
 * Считаем в направлении минуты → кредиты, а не наоборот. Обратное деление
 * потребовало бы округления, а округление вверх превращается в эксплойт:
 * при курсе 2 кредита за минуту повторный обмен по одному кредиту давал бы
 * по половине минуты, округлённой до целой. Здесь цена всегда точная.
 */
export function convertCredits(
  requestedMinutes: number,
  state: DayState,
  economy: EconomyConfig,
): Result<ConversionResult, ConversionError> {
  if (!Number.isInteger(requestedMinutes) || requestedMinutes <= 0) {
    return fail('invalid_amount', 'Количество минут должно быть целым положительным числом.');
  }

  const remainingCap = economy.maxConvertedMinutesPerDay - state.convertedMinutesToday;
  if (remainingCap <= 0) {
    return fail('daily_cap_reached', 'Дневной лимит обмена кредитов на время уже исчерпан.');
  }

  const affordableMinutes = Math.floor(state.creditsBalance / economy.creditsPerMinute);
  const minutes = Math.min(requestedMinutes, remainingCap, affordableMinutes);

  if (minutes <= 0) {
    return fail('insufficient_credits', 'Кредитов не хватает даже на одну минуту.');
  }

  const creditsSpent = minutes * economy.creditsPerMinute;
  if (creditsSpent < economy.minCreditsToConvert) {
    return fail(
      'below_minimum',
      `Минимальный обмен — ${economy.minCreditsToConvert} кредитов.`,
    );
  }

  return {
    ok: true,
    minutes,
    creditsSpent,
    drafts: [
      { currency: 'credits', amount: -creditsSpent, reason: 'conversion_spend' },
      { currency: 'minutes', amount: minutes, reason: 'conversion_gain' },
    ],
  };
}

// ------------------------------------------------------- начисление за задания

export interface TaskAwardInput {
  /** Базовое начисление за задание из пакета. */
  credits: number;
  economy: EconomyConfig;
  state: Pick<DayState, 'creditsEarnedToday'>;
  /** Потолок кредитов с этого пакета в сутки. */
  packDailyCreditCap: number;
  /** Сколько кредитов этот пакет уже принёс сегодня. */
  packCreditsToday: number;
  /** Когда это же задание засчитывалось в прошлый раз. */
  lastAwardedAt?: Date | undefined;
  cooldownHours?: number | undefined;
  now: Date;
}

export type TaskAwardError = 'cooldown_active' | 'pack_cap_reached' | 'daily_cap_reached';

/**
 * Сколько кредитов реально начислить за задание.
 *
 * Три независимых ограничителя: cooldown не даёт перерешивать одно задание,
 * потолок пакета не даёт фармить один лёгкий предмет, дневной потолок
 * ограничивает заработок в целом. Начисление урезается до остатка, а не
 * отклоняется целиком — иначе последнее задание дня пропадало бы впустую.
 */
export function awardTaskCredits(input: TaskAwardInput): Result<{ credits: number }, TaskAwardError> {
  const { credits, economy, state, packDailyCreditCap, packCreditsToday, now } = input;

  if (input.lastAwardedAt && input.cooldownHours && input.cooldownHours > 0) {
    const elapsed = hoursBetween(input.lastAwardedAt, now);
    if (elapsed < input.cooldownHours) {
      const left = Math.ceil(input.cooldownHours - elapsed);
      return fail('cooldown_active', `Это задание снова принесёт кредиты через ${left} ч.`);
    }
  }

  const packRoom = packDailyCreditCap - packCreditsToday;
  if (packRoom <= 0) {
    return fail('pack_cap_reached', 'Дневной лимит кредитов по этому набору заданий исчерпан.');
  }

  const globalRoom = economy.maxCreditsPerDay - state.creditsEarnedToday;
  if (globalRoom <= 0) {
    return fail('daily_cap_reached', 'Дневной лимит кредитов исчерпан.');
  }

  return { ok: true, credits: Math.max(0, Math.min(credits, packRoom, globalRoom)) };
}

// ---------------------------------------------------------------------- магазин

export type PurchaseError =
  | 'item_disabled'
  | 'insufficient_funds'
  | 'daily_limit_reached'
  | 'weekly_limit_reached'
  | 'cooldown_active';

export interface PurchaseHistory {
  purchasesToday: number;
  purchasesThisWeek: number;
  lastPurchaseAt?: Date | undefined;
}

export function canPurchase(
  item: StoreItem,
  state: Pick<DayState, 'creditsBalance' | 'minutesBalance'>,
  history: PurchaseHistory,
  now: Date,
): Result<{ cost: number }, PurchaseError> {
  if (!item.enabled) return fail('item_disabled', 'Товар выключен родителем.');

  const balance = item.cost.currency === 'credits' ? state.creditsBalance : state.minutesBalance;
  if (balance < item.cost.amount) {
    return fail('insufficient_funds', 'Недостаточно средств для покупки.');
  }

  if (item.maxPerDay !== undefined && history.purchasesToday >= item.maxPerDay) {
    return fail('daily_limit_reached', 'Сегодня этот товар уже недоступен.');
  }

  if (item.maxPerWeek !== undefined && history.purchasesThisWeek >= item.maxPerWeek) {
    return fail('weekly_limit_reached', 'На этой неделе этот товар уже недоступен.');
  }

  if (item.cooldownHours !== undefined && history.lastPurchaseAt) {
    const elapsed = hoursBetween(history.lastPurchaseAt, now);
    if (elapsed < item.cooldownHours) {
      const left = Math.ceil(item.cooldownHours - elapsed);
      return fail('cooldown_active', `Этот товар снова можно купить через ${left} ч.`);
    }
  }

  return { ok: true, cost: item.cost.amount };
}
