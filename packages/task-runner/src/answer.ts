import type { ItemType } from '@mykids/contracts';

/** Ответ ребёнка. Форма зависит от типа задания. */
export type Answer =
  | { type: 'single_choice'; optionIndex: number }
  | { type: 'multi_choice'; optionIndexes: number[] }
  | { type: 'numeric'; raw: string }
  | { type: 'short_text'; raw: string }
  | { type: 'ordering'; order: string[] }
  | { type: 'matching'; assignments: Record<string, string> }
  | { type: 'cloze'; blanks: Record<string, string> }
  | { type: 'code'; source: string }
  | { type: 'likert'; value: number }
  | { type: 'reflection'; text: string }
  | { type: 'parent_verified'; requested: true }
  | { type: 'interactive'; completed: boolean; score?: number; durationSec?: number };

export type AnswerFor<T extends ItemType> = Extract<Answer, { type: T }>;
