import { z } from 'zod';

/**
 * Типы заданий и пакетов. Держатся здесь, потому что нужны и раннеру, и
 * инструментам контента, и будущему серверу.
 *
 * Канонический источник для CI — JSON Schema в docs/content-format. Тест
 * content-schema.test.ts следит, чтобы эти определения с ней не разошлись.
 */

export const itemTypeSchema = z.enum([
  // оцениваемые
  'single_choice', 'multi_choice', 'numeric', 'short_text',
  'ordering', 'matching', 'cloze', 'code',
  // без правильного ответа: кредиты за выполнение
  'likert', 'reflection', 'parent_verified', 'interactive',
]);
export type ItemType = z.infer<typeof itemTypeSchema>;

/** Типы, у которых есть правильный ответ и которые дают долю верности 0..1. */
export const SCORED_TYPES = [
  'single_choice', 'multi_choice', 'numeric', 'short_text',
  'ordering', 'matching', 'cloze', 'code',
] as const satisfies readonly ItemType[];

export function isScored(type: ItemType): boolean {
  return (SCORED_TYPES as readonly string[]).includes(type);
}

const baseItem = {
  id: z.string().regex(/^[a-z0-9][a-z0-9-]{2,63}$/),
  stem: z.string().min(1),
  difficulty: z.number().int().min(1).max(5).optional(),
  credits: z.number().int().min(0).max(50).optional(),
  topics: z.array(z.string()).optional(),
  hints: z.array(z.string()).optional(),
  solution: z.string().optional(),
  media: z.string().startsWith('assets/').optional(),
  cooldownHours: z.number().nonnegative().optional(),
};

const choiceOption = z.object({
  text: z.string().min(1),
  correct: z.boolean(),
  feedback: z.string().optional(),
});

export const taskItemSchema = z.discriminatedUnion('type', [
  z.object({
    ...baseItem, type: z.literal('single_choice'),
    options: z.array(choiceOption).min(2).max(8),
  }),
  z.object({
    ...baseItem, type: z.literal('multi_choice'),
    options: z.array(choiceOption).min(2).max(10),
    partialCredit: z.boolean().optional(),
  }),
  z.object({
    ...baseItem, type: z.literal('numeric'),
    answer: z.object({
      value: z.number(),
      unit: z.string().optional(),
      tolerance: z.number().nonnegative().optional(),
      relativeTolerance: z.number().min(0).max(1).optional(),
      acceptedUnits: z.array(z.string()).optional(),
    }),
  }),
  z.object({
    ...baseItem, type: z.literal('short_text'),
    answer: z.object({
      accepted: z.array(z.string()).min(1).optional(),
      pattern: z.string().optional(),
      caseSensitive: z.boolean().optional(),
      ignoreYo: z.boolean().optional(),
    }),
  }),
  z.object({
    ...baseItem, type: z.literal('ordering'),
    sequence: z.array(z.string()).min(2),
  }),
  z.object({
    ...baseItem, type: z.literal('matching'),
    pairs: z.array(z.object({ left: z.string(), right: z.string() })).min(2),
  }),
  z.object({
    ...baseItem, type: z.literal('cloze'),
    blanks: z.array(z.object({
      id: z.string(),
      accepted: z.array(z.string()).min(1),
    })).min(1),
  }),
  z.object({
    ...baseItem, type: z.literal('code'),
    language: z.enum(['python', 'javascript']),
    starterCode: z.string().optional(),
    tests: z.array(z.object({
      input: z.unknown(),
      expected: z.unknown(),
      hidden: z.boolean().optional(),
    })).min(1),
  }),
  z.object({
    ...baseItem, type: z.literal('likert'),
    scale: z.object({
      min: z.number().int(),
      max: z.number().int(),
      minLabel: z.string().optional(),
      maxLabel: z.string().optional(),
      reverse: z.boolean().optional(),
    }),
    dimension: z.string().optional(),
  }),
  z.object({
    ...baseItem, type: z.literal('reflection'),
    minChars: z.number().int().positive().optional(),
    prompts: z.array(z.string()).optional(),
  }),
  z.object({
    ...baseItem, type: z.literal('parent_verified'),
    verificationPrompt: z.string().min(1),
  }),
  z.object({
    ...baseItem, type: z.literal('interactive'),
    widget: z.string().min(1),
    props: z.record(z.unknown()).optional(),
    completionRule: z.object({
      minScore: z.number().optional(),
      minDurationSec: z.number().int().optional(),
    }).optional(),
  }),
]);
export type TaskItem = z.infer<typeof taskItemSchema>;

export const packRewardSchema = z.object({
  creditsPerCorrect: z.number().int().min(0).max(50),
  bonusOnPerfect: z.number().int().min(0).max(100).optional(),
  dailyCreditCap: z.number().int().min(1).max(500),
  penaltyPerWrong: z.number().int().min(0).max(50).optional(),
});
export type PackReward = z.infer<typeof packRewardSchema>;

export const packDeliverySchema = z.object({
  itemsPerSession: z.number().int().min(1).max(100).optional(),
  selection: z.enum(['random', 'sequential', 'adaptive', 'spaced']).optional(),
  passThreshold: z.number().min(0).max(1).optional(),
  cooldownHoursPerItem: z.number().nonnegative().optional(),
  timeLimitSec: z.number().int().min(10).optional(),
});
export type PackDelivery = z.infer<typeof packDeliverySchema>;

export const packManifestSchema = z.object({
  id: z.string().regex(/^[a-z0-9]+(\.[a-z0-9-]+)+$/),
  schemaVersion: z.literal(1),
  version: z.string().regex(/^\d+\.\d+\.\d+$/),
  title: z.string().min(1).max(120),
  description: z.string().max(600).optional(),
  subject: z.enum([
    'psychology', 'math', 'physics', 'chemistry', 'biology', 'geography',
    'history', 'language', 'literature', 'cs', 'logic', 'finance', 'other',
  ]),
  gradeRange: z.tuple([z.number().int(), z.number().int()]).optional(),
  locale: z.string().regex(/^[a-z]{2}(-[A-Z]{2})?$/),
  author: z.string().optional(),
  license: z.string().optional(),
  tags: z.array(z.string()).max(20).optional(),
  reward: packRewardSchema,
  delivery: packDeliverySchema.optional(),
  items: z.array(z.string()).min(1),
});
export type PackManifest = z.infer<typeof packManifestSchema>;

/** Пакет с уже загруженными заданиями. */
export interface TaskPack {
  manifest: PackManifest;
  items: TaskItem[];
}
