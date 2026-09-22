import type { Answer } from '@mykids/task-runner/answer';
import { z } from 'zod';

/**
 * Ответ ребёнка на границе API.
 *
 * Схема живёт здесь, а не рядом с типом: тип нужен и браузеру, а zod в сборку
 * страницы ребёнка тащить незачем. Расхождение схемы с типом поймает
 * `satisfies` ниже — он не даст схеме разобрать то, чего в типе нет.
 */
export const answerSchema = z.discriminatedUnion('type', [
  z.object({ type: z.literal('single_choice'), optionIndex: z.number().int().min(0).max(32) }),
  z.object({
    type: z.literal('multi_choice'),
    optionIndexes: z.array(z.number().int().min(0).max(32)).max(32),
  }),
  z.object({ type: z.literal('numeric'), raw: z.string().max(200) }),
  z.object({ type: z.literal('short_text'), raw: z.string().max(2000) }),
  z.object({ type: z.literal('ordering'), order: z.array(z.string().max(500)).max(64) }),
  z.object({
    type: z.literal('matching'),
    assignments: z.record(z.string().max(500), z.string().max(500)),
  }),
  z.object({ type: z.literal('cloze'), blanks: z.record(z.string().max(200), z.string().max(500)) }),
  z.object({ type: z.literal('code'), source: z.string().max(20000) }),
  z.object({ type: z.literal('likert'), value: z.number().int().min(-100).max(100) }),
  z.object({ type: z.literal('reflection'), text: z.string().max(20000) }),
  z.object({ type: z.literal('parent_verified'), requested: z.literal(true) }),
  z.object({
    type: z.literal('interactive'),
    completed: z.boolean(),
    // Виджет крутится на устройстве ребёнка, и эти два числа — единственное,
    // что сервер по-прежнему принимает на слово. Сказать про это прямо честнее,
    // чем делать вид, что проверено всё.
    score: z.number().min(0).max(1).optional(),
    durationSec: z.number().min(0).max(24 * 3600).optional(),
  }),
]) satisfies z.ZodType<Answer, z.ZodTypeDef, unknown>;
