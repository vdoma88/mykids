import type { LedgerEntry, Policy, TimeWindow } from '@mykids/contracts';
import { policySchema } from '@mykids/contracts';
import type { LedgerEntry as DbLedgerEntry, Policy as DbPolicy } from '@prisma/client';

/** Строка журнала из базы в доменный тип. */
export function toDomainEntry(row: DbLedgerEntry): LedgerEntry {
  return {
    id: row.id,
    childId: row.childId,
    currency: row.currency,
    amount: row.amount,
    reason: row.reason,
    ...(row.refType !== null ? { refType: row.refType } : {}),
    ...(row.refId !== null ? { refId: row.refId } : {}),
    deviceId: row.deviceId,
    occurredAt: row.occurredAt,
    recordedAt: row.recordedAt,
    seq: row.seq,
  };
}

/**
 * Политика из базы в доменный тип.
 *
 * Окна лежат в JSON-колонке, поэтому проходят zod на выходе из базы: битая
 * запись должна падать здесь, а не через три слоя внутри расчёта расписания.
 */
export function toDomainPolicy(row: DbPolicy): Policy {
  return policySchema.parse({
    timezone: row.timezone,
    dailyLimitMinutes: row.dailyLimitMinutes,
    carryOverMaxMinutes: row.carryOverMaxMinutes,
    windows: row.windows as TimeWindow[],
    economy: {
      creditsPerMinute: row.creditsPerMinute,
      maxConvertedMinutesPerDay: row.maxConvertedMinutesPerDay,
      minCreditsToConvert: row.minCreditsToConvert,
      maxCreditsPerDay: row.maxCreditsPerDay,
    },
  });
}
