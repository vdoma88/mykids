import { z } from 'zod';
import { currencySchema } from './ledger.js';

export const storeEffectSchema = z.discriminatedUnion('kind', [
  z.object({ kind: z.literal('grant_minutes'), minutes: z.number().int().positive() }),
  z.object({ kind: z.literal('unlock_app'), appId: z.string().min(1), minutes: z.number().int().positive() }),
  z.object({ kind: z.literal('shift_bedtime'), minutes: z.number().int().positive() }),
  z.object({ kind: z.literal('weekend_pass') }),
  /** Награда вне экрана: исполняет родитель, система только списывает цену. */
  z.object({ kind: z.literal('custom'), note: z.string().min(1) }),
]);
export type StoreEffect = z.infer<typeof storeEffectSchema>;

export const storeItemSchema = z
  .object({
    id: z.string().min(1),
    title: z.string().min(1),
    description: z.string().optional(),
    cost: z.object({ currency: currencySchema, amount: z.number().int().positive() }),
    effect: storeEffectSchema,
    maxPerDay: z.number().int().positive().optional(),
    maxPerWeek: z.number().int().positive().optional(),
    cooldownHours: z.number().nonnegative().optional(),
    requiresApproval: z.boolean().default(false),
    enabled: z.boolean().default(true),
  })
  .strict();
export type StoreItem = z.infer<typeof storeItemSchema>;
