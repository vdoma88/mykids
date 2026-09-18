import type { Currency, LedgerDraft, LedgerEntry } from '@mykids/contracts';
import { balanceOf, entriesOnDay, localDay, sumByReason } from '@mykids/domain';
import type { PrismaClient } from '@prisma/client';
import { toDomainEntry } from '../mapping.js';

/**
 * Доступ к журналу операций.
 *
 * Балансы считаются свёрткой записей, а не хранятся отдельным полем. Для семьи
 * это тысячи строк в год — свёртка дешевле, чем разбор рассогласования между
 * счётчиком и историей после офлайн-синхронизации. Если журнал вырастет,
 * добавятся периодические снимки, но инвариант «истина в журнале» останется.
 */
export class LedgerService {
  constructor(private readonly prisma: PrismaClient) {}

  async entriesFor(childId: string): Promise<LedgerEntry[]> {
    const rows = await this.prisma.ledgerEntry.findMany({
      where: { childId },
      orderBy: { recordedAt: 'asc' },
    });
    return rows.map(toDomainEntry);
  }

  async balance(childId: string, currency: Currency): Promise<number> {
    const rows = await this.prisma.ledgerEntry.findMany({
      where: { childId, currency },
      select: { amount: true },
    });
    return rows.reduce((sum, r) => sum + r.amount, 0);
  }

  async balances(childId: string): Promise<{ minutes: number; credits: number }> {
    const entries = await this.entriesFor(childId);
    return {
      minutes: balanceOf(entries, 'minutes'),
      credits: balanceOf(entries, 'credits'),
    };
  }

  /** Записи за локальные сутки семьи. Отбор идёт по часам сервера. */
  async entriesOnLocalDay(childId: string, day: string, timezone: string): Promise<LedgerEntry[]> {
    const entries = await this.entriesFor(childId);
    return entriesOnDay(entries, day, timezone);
  }

  /** Сводка за сутки, нужная экономике для применения потолков. */
  async daySummary(
    childId: string,
    at: Date,
    timezone: string,
  ): Promise<{
    day: string;
    grantedMinutesToday: number;
    convertedMinutesToday: number;
    creditsEarnedToday: number;
  }> {
    const day = localDay(at, timezone);
    const today = await this.entriesOnLocalDay(childId, day, timezone);
    return {
      day,
      grantedMinutesToday: sumByReason(today, 'minutes', ['daily_grant', 'carry_over']),
      convertedMinutesToday: sumByReason(today, 'minutes', ['conversion_gain']),
      creditsEarnedToday: sumByReason(today, 'credits', ['task_reward']),
    };
  }

  /**
   * Добавляет записи одной транзакцией.
   *
   * Повтор очереди синхронизации от агента не должен удваивать списание,
   * поэтому пары устройство+счётчик пропускаются при конфликте. Записи сервера
   * идут без deviceId и под это правило не подпадают.
   */
  async append(drafts: LedgerDraft[]): Promise<number> {
    if (drafts.length === 0) return 0;
    const result = await this.prisma.ledgerEntry.createMany({
      data: drafts.map((d) => ({
        childId: d.childId,
        currency: d.currency,
        amount: d.amount,
        reason: d.reason,
        refType: d.refType ?? null,
        refId: d.refId ?? null,
        deviceId: d.deviceId,
        occurredAt: d.occurredAt,
        seq: d.seq,
      })),
      skipDuplicates: true,
    });
    return result.count;
  }
}
