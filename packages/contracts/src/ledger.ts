import { z } from 'zod';

/**
 * Две валюты разведены намеренно. Кредиты зарабатываются заданиями и тратятся
 * в магазине; минуты расходуются экраном. Курс и потолок конвертации между ними
 * настраиваются независимо от того, сколько ребёнок способен заработать.
 */
export const currencySchema = z.enum(['minutes', 'credits']);
export type Currency = z.infer<typeof currencySchema>;

/**
 * Причина записи. Знак суммы задаётся самой записью, а не причиной:
 * ручная корректировка может быть и в плюс, и в минус.
 */
export const ledgerReasonSchema = z.enum([
  'daily_grant',     // выдача дневного лимита минут
  'carry_over',      // перенос неистраченного с прошлого дня
  'task_reward',     // кредиты за выполненное задание
  'conversion_spend', // списание кредитов при обмене
  'conversion_gain',  // зачисление минут при обмене
  'purchase',        // трата в магазине
  'purchase_grant',  // эффект покупки, начисляющий минуты
  'screen_usage',    // списание минут за использование экрана
  'manual_adjust',   // ручная корректировка родителем
  'tamper_penalty',  // штраф за вмешательство в работу агента
  'expiry',          // сгорание неперенесённого остатка
]);
export type LedgerReason = z.infer<typeof ledgerReasonSchema>;

/**
 * Запись журнала. Журнал только дополняется: балансы считаются свёрткой,
 * а не хранятся отдельным изменяемым полем. Это даёт аудит, разрешение
 * конфликтов офлайн-синхронизации и откат ошибочных начислений.
 */
export const ledgerEntrySchema = z.object({
  id: z.string().min(1),
  childId: z.string().min(1),
  currency: currencySchema,
  /** Целое со знаком. Минуты и кредиты не дробятся — это исключает эксплойты округления. */
  amount: z.number().int(),
  reason: ledgerReasonSchema,
  refType: z.string().min(1).optional(),
  refId: z.string().min(1).optional(),
  /** Устройство-источник; null для записей, созданных сервером. */
  deviceId: z.string().min(1).nullable(),
  /** Часы устройства в момент события. Может быть подкручено — не доверять. */
  occurredAt: z.date(),
  /** Часы сервера в момент приёма. Источник истины для суточных окон. */
  recordedAt: z.date(),
  /** Монотонный счётчик устройства: ловит переупорядочивание и дубли. */
  seq: z.number().int().nonnegative(),
});
export type LedgerEntry = z.infer<typeof ledgerEntrySchema>;

/** Заготовка записи: id и recordedAt проставляет слой хранения. */
export type LedgerDraft = Omit<LedgerEntry, 'id' | 'recordedAt'>;
