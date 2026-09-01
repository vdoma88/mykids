import { z } from 'zod';

/** Дни недели в нотации IANA-подобных сокращений; 0 — воскресенье, как в Date.getDay(). */
export const weekdaySchema = z.number().int().min(0).max(6);

/** "HH:MM" в локальном времени семьи. */
export const timeOfDaySchema = z.string().regex(/^([01]\d|2[0-3]):[0-5]\d$/);

export const windowModeSchema = z.enum([
  'blocked',    // экран недоступен вовсе
  'tasks_only', // доступны только задания
  'allowed',    // явное разрешение, перекрывает более общие правила
]);
export type WindowMode = z.infer<typeof windowModeSchema>;

/**
 * Временное окно. Окно, у которого `to` меньше `from`, пересекает полночь
 * (например отбой 21:30–07:00) — это нормальный и частый случай.
 */
export const timeWindowSchema = z.object({
  name: z.string().min(1),
  days: z.array(weekdaySchema).nonempty(),
  from: timeOfDaySchema,
  to: timeOfDaySchema,
  mode: windowModeSchema,
});
export type TimeWindow = z.infer<typeof timeWindowSchema>;

export const appCategorySchema = z.enum([
  'always_allowed', // не считается и не блокируется: звонки, сообщения, часы
  'metered',        // считается и блокируется по исчерпании
  'educational',    // считается, но не блокируется
  'blocked',        // запрещено всегда
]);
export type AppCategory = z.infer<typeof appCategorySchema>;

export const economyConfigSchema = z
  .object({
    /** Сколько кредитов стоит одна минута. */
    creditsPerMinute: z.number().int().positive(),
    /** Потолок минут, получаемых конвертацией за сутки. Главная антифарм-мера. */
    maxConvertedMinutesPerDay: z.number().int().nonnegative(),
    /** Порог, ниже которого обмен не имеет смысла. */
    minCreditsToConvert: z.number().int().nonnegative(),
    /** Потолок кредитов из всех источников за сутки. */
    maxCreditsPerDay: z.number().int().positive(),
  })
  .strict();
export type EconomyConfig = z.infer<typeof economyConfigSchema>;

export const policySchema = z
  .object({
    /** Часовой пояс семьи в формате IANA: границы суток считаются по нему, не по UTC. */
    timezone: z.string().min(1),
    /** Дневной лимит минут по дням недели: индекс совпадает с Date.getDay(). */
    dailyLimitMinutes: z.array(z.number().int().nonnegative()).length(7),
    /** Сколько неистраченного переносится на следующий день. */
    carryOverMaxMinutes: z.number().int().nonnegative(),
    windows: z.array(timeWindowSchema),
    economy: economyConfigSchema,
  })
  .strict();
export type Policy = z.infer<typeof policySchema>;
