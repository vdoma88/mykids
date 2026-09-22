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
  // Явное «| undefined»: при exactOptionalPropertyTypes отсутствующее поле и
  // поле со значением undefined — разные типы, а приходит это и так, и так:
  // из виджета в браузере и из разобранного на сервере JSON.
  | { type: 'interactive'; completed: boolean; score?: number | undefined; durationSec?: number | undefined };

export type AnswerFor<T extends ItemType> = Extract<Answer, { type: T }>;
