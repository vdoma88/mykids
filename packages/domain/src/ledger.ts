import type { Currency, LedgerEntry } from '@mykids/contracts';
import { localDay } from './time.js';

/**
 * Баланс — свёртка журнала, а не хранимое поле. Пересчёт дешевле, чем разбор
 * рассогласования между балансом и историей после офлайн-синхронизации.
 */
export function balanceOf(entries: readonly LedgerEntry[], currency: Currency): number {
  let sum = 0;
  for (const e of entries) {
    if (e.currency === currency) sum += e.amount;
  }
  return sum;
}

/**
 * Записи за конкретные локальные сутки.
 *
 * Отбор идёт по recordedAt — часам сервера. occurredAt приходит с устройства и
 * может быть подкручен: если считать суточные потолки по нему, переводом часов
 * назад открывается новый «день» и все антифарм-лимиты обнуляются.
 */
export function entriesOnDay(
  entries: readonly LedgerEntry[],
  day: string,
  timezone: string,
): LedgerEntry[] {
  return entries.filter((e) => localDay(e.recordedAt, timezone) === day);
}

/** Сумма записей с указанной причиной, по модулю знака. */
export function sumByReason(
  entries: readonly LedgerEntry[],
  currency: Currency,
  reasons: readonly LedgerEntry['reason'][],
): number {
  const wanted = new Set(reasons);
  let sum = 0;
  for (const e of entries) {
    if (e.currency === currency && wanted.has(e.reason)) sum += e.amount;
  }
  return sum;
}

/**
 * Дубли по (deviceId, seq) при повторной отправке очереди синхронизации.
 * Записи сервера (deviceId === null) не дедуплицируются: у них нет счётчика устройства.
 */
export function dedupeByDeviceSeq(entries: readonly LedgerEntry[]): LedgerEntry[] {
  const seen = new Set<string>();
  const out: LedgerEntry[] = [];
  for (const e of entries) {
    if (e.deviceId === null) {
      out.push(e);
      continue;
    }
    const key = `${e.deviceId}#${e.seq}`;
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(e);
  }
  return out;
}
