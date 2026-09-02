import type { EconomyConfig, TaskItem, TaskPack } from '@mykids/contracts';
import { isScored } from '@mykids/contracts';
import { awardTaskCredits } from '@mykids/domain';
import type { Answer } from './answer.js';
import { grade, type Grade } from './grade.js';

/** Что раннер знает о прошлых попытках. Приходит с сервера, здесь только читается. */
export interface AttemptHistory {
  /** Когда задание в последний раз приносило кредиты. */
  lastAwardedAt: Record<string, Date>;
  /** Сколько кредитов этот пакет уже принёс сегодня. */
  packCreditsToday: number;
  /** Сколько кредитов заработано сегодня из всех источников. */
  creditsEarnedToday: number;
}

export interface SessionOptions {
  pack: TaskPack;
  economy: EconomyConfig;
  history: AttemptHistory;
  now: Date;
  /** Источник случайности; подменяется в тестах. */
  random?: () => number;
}

export interface SubmittedAnswer {
  itemId: string;
  grade: Grade;
  creditsAwarded: number;
  /** Почему кредиты не начислены, хотя задание выполнено. */
  withheldReason?: string;
}

export interface SessionSummary {
  total: number;
  /** Задания с правильным ответом, засчитанные полностью. */
  correct: number;
  /** Средняя доля верности по оцениваемым заданиям. */
  accuracy: number;
  passed: boolean;
  creditsEarned: number;
  bonusAwarded: number;
  pendingApproval: string[];
}

function shuffle<T>(items: readonly T[], random: () => number): T[] {
  const out = [...items];
  for (let i = out.length - 1; i > 0; i--) {
    const j = Math.floor(random() * (i + 1));
    const a = out[i] as T;
    const b = out[j] as T;
    out[i] = b;
    out[j] = a;
  }
  return out;
}

/**
 * Одна сессия прохождения пакета.
 *
 * Кредиты здесь только СЧИТАЮТСЯ, чтобы показать ребёнку результат сразу.
 * Записывает их сервер, пересчитывая теми же функциями домена: раннер крутится
 * на устройстве ребёнка, и доверять его арифметике нельзя.
 */
export class TaskSession {
  private readonly opts: SessionOptions;
  private readonly queue: TaskItem[];
  private readonly submitted: SubmittedAnswer[] = [];
  private cursor = 0;
  private creditsThisSession = 0;

  constructor(opts: SessionOptions) {
    this.opts = opts;
    this.queue = TaskSession.selectItems(opts);
  }

  /**
   * Отбор заданий. Задания на cooldown не исключаются из показа: пройти их
   * можно, просто кредитов они не принесут. Иначе на исходе банка ребёнку
   * нечего было бы делать.
   */
  private static selectItems(opts: SessionOptions): TaskItem[] {
    const { pack, now, history } = opts;
    const random = opts.random ?? Math.random;
    const delivery = pack.manifest.delivery ?? {};
    const limit = delivery.itemsPerSession ?? pack.items.length;

    const cooledDown = (item: TaskItem): boolean => {
      const hours = item.cooldownHours ?? delivery.cooldownHoursPerItem;
      const last = history.lastAwardedAt[item.id];
      if (hours === undefined || hours <= 0 || !last) return true;
      return (now.getTime() - last.getTime()) / 3_600_000 >= hours;
    };

    switch (delivery.selection ?? 'sequential') {
      case 'random':
        return shuffle(pack.items, random).slice(0, limit);
      case 'spaced':
      case 'adaptive':
        // Пока обе стратегии сводятся к «сначала то, что снова приносит кредиты».
        // Полноценный интервальный алгоритм — фаза 5.
        return [...pack.items]
          .sort((a, b) => Number(cooledDown(b)) - Number(cooledDown(a)))
          .slice(0, limit);
      case 'sequential':
      default:
        return pack.items.slice(0, limit);
    }
  }

  get items(): readonly TaskItem[] {
    return this.queue;
  }

  get current(): TaskItem | undefined {
    return this.queue[this.cursor];
  }

  get progress(): { index: number; total: number } {
    return { index: this.cursor, total: this.queue.length };
  }

  get finished(): boolean {
    return this.cursor >= this.queue.length;
  }

  /** Проверяет ответ на текущем задании и считает начисление. */
  submit(answer: Answer): SubmittedAnswer {
    const item = this.current;
    if (!item) throw new Error('сессия уже завершена');

    const result = grade(item, answer);
    const outcome = this.computeAward(item, result);
    this.submitted.push(outcome);
    this.creditsThisSession += outcome.creditsAwarded;
    this.cursor++;
    return outcome;
  }

  private computeAward(item: TaskItem, result: Grade): SubmittedAnswer {
    const { pack, economy, history, now } = this.opts;

    if (result.pendingApproval === true) {
      return {
        itemId: item.id,
        grade: result,
        creditsAwarded: 0,
        withheldReason: 'Ждёт подтверждения родителя.',
      };
    }

    // У оцениваемых типов complete означает «верно полностью», поэтому привязывать
    // начисление к нему нельзя: частичный зачёт заслуживает части кредитов.
    // У типов без правильного ответа платим за факт выполнения.
    const earnsCredits = isScored(item.type) ? result.score > 0 : result.complete;
    if (!earnsCredits) {
      return { itemId: item.id, grade: result, creditsAwarded: 0 };
    }

    const base = item.credits ?? pack.manifest.reward.creditsPerCorrect;
    // Частичный зачёт даёт часть кредитов, но не меньше одного за ненулевой результат.
    const scaled = isScored(item.type)
      ? Math.max(1, Math.round(base * result.score))
      : base;

    const award = awardTaskCredits({
      credits: scaled,
      economy,
      state: { creditsEarnedToday: history.creditsEarnedToday + this.creditsThisSession },
      packDailyCreditCap: pack.manifest.reward.dailyCreditCap,
      packCreditsToday: history.packCreditsToday + this.creditsThisSession,
      lastAwardedAt: history.lastAwardedAt[item.id],
      cooldownHours: item.cooldownHours ?? pack.manifest.delivery?.cooldownHoursPerItem,
      now,
    });

    return award.ok
      ? { itemId: item.id, grade: result, creditsAwarded: award.credits }
      : { itemId: item.id, grade: result, creditsAwarded: 0, withheldReason: award.message };
  }

  /** Итог сессии. Бонус за идеальное прохождение проходит те же дневные потолки. */
  summary(): SessionSummary {
    const scored = this.submitted.filter((s) => {
      const item = this.queue.find((i) => i.id === s.itemId);
      return item !== undefined && isScored(item.type);
    });

    const correct = scored.filter((s) => s.grade.score === 1).length;
    const accuracy = scored.length === 0
      ? 1
      : scored.reduce((sum, s) => sum + s.grade.score, 0) / scored.length;

    const threshold = this.opts.pack.manifest.delivery?.passThreshold ?? 0;
    const passed = this.submitted.length === this.queue.length && accuracy >= threshold;

    let bonus = 0;
    const perfectBonus = this.opts.pack.manifest.reward.bonusOnPerfect ?? 0;
    if (perfectBonus > 0 && accuracy === 1 && this.submitted.length === this.queue.length) {
      const { history, economy, pack, now } = this.opts;
      const award = awardTaskCredits({
        credits: perfectBonus,
        economy,
        state: { creditsEarnedToday: history.creditsEarnedToday + this.creditsThisSession },
        packDailyCreditCap: pack.manifest.reward.dailyCreditCap,
        packCreditsToday: history.packCreditsToday + this.creditsThisSession,
        now,
      });
      if (award.ok) bonus = award.credits;
    }

    return {
      total: this.queue.length,
      correct,
      accuracy,
      passed,
      creditsEarned: this.creditsThisSession + bonus,
      bonusAwarded: bonus,
      pendingApproval: this.submitted
        .filter((s) => s.grade.pendingApproval === true)
        .map((s) => s.itemId),
    };
  }
}
