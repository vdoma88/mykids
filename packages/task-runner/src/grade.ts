import type { TaskItem } from '@mykids/contracts';
import { isScored } from '@mykids/contracts';
import type { Answer } from './answer.js';

export interface Grade {
  /** Доля верности 0..1. У неоцениваемых типов — 1 при выполнении, 0 иначе. */
  score: number;
  /**
   * У оцениваемых типов — «верно полностью» (score === 1), у остальных — факт
   * выполнения. Начисление кредитов на это поле НЕ завязано: частичный зачёт
   * даёт часть кредитов при complete === false. Решает TaskSession.computeAward.
   */
  complete: boolean;
  /** Пояснение для ребёнка. */
  feedback?: string;
  /** Ждёт подтверждения родителя — кредиты начисляются позже. */
  pendingApproval?: boolean;
}

/** Нормализация текста: регистр, пробелы, ё и типографские кавычки. */
export function normalizeText(
  value: string,
  opts: { caseSensitive?: boolean | undefined; ignoreYo?: boolean | undefined } = {},
): string {
  let out = value.trim().replace(/\s+/g, ' ').replace(/[«»""]/g, '"').replace(/[''`]/g, "'");
  if (!opts.caseSensitive) out = out.toLowerCase();
  // «ё» и «е» ребёнок печатает как придётся; по умолчанию считаем их одной буквой
  if (opts.ignoreYo !== false) out = out.replace(/ё/g, 'е').replace(/Ё/g, 'Е');
  return out;
}

/**
 * Разбор числового ответа: "6", "6 Н", "6Н", "6,5 м/с" — всё это валидные формы.
 * Запятая как десятичный разделитель обязательна: на русской раскладке ребёнок
 * наберёт именно её, и отказ по такому поводу выглядит придиркой.
 */
export function parseNumericAnswer(raw: string): { value: number; unit: string } | null {
  const text = raw.trim().replace(',', '.');
  const match = /^([+-]?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)\s*(.*)$/.exec(text);
  if (!match) return null;

  const value = Number(match[1]);
  if (!Number.isFinite(value)) return null;

  return { value, unit: (match[2] ?? '').trim() };
}

function gradeNumeric(item: Extract<TaskItem, { type: 'numeric' }>, raw: string): Grade {
  const parsed = parseNumericAnswer(raw);
  if (!parsed) return { score: 0, complete: false, feedback: 'Не удалось прочитать число.' };

  const { answer } = item;
  const absolute = answer.tolerance ?? 0;
  const relative = answer.relativeTolerance !== undefined
    ? Math.abs(answer.value) * answer.relativeTolerance
    : 0;
  const allowed = Math.max(absolute, relative);

  if (Math.abs(parsed.value - answer.value) > allowed) {
    return { score: 0, complete: false, feedback: 'Число не сходится.' };
  }

  // Единицы проверяем, только если задание их требует. Пустой ввод единиц
  // засчитываем: ребёнок мог посчитать верно и не написать «Н».
  if (answer.unit !== undefined && parsed.unit !== '') {
    const accepted = [answer.unit, ...(answer.acceptedUnits ?? [])].map((u) => normalizeText(u));
    if (!accepted.includes(normalizeText(parsed.unit))) {
      return {
        score: 0,
        complete: false,
        feedback: `Число верное, но единицы не те — ожидались ${answer.unit}.`,
      };
    }
  }

  return { score: 1, complete: true };
}

function gradeShortText(item: Extract<TaskItem, { type: 'short_text' }>, raw: string): Grade {
  const { answer } = item;
  const opts = { caseSensitive: answer.caseSensitive, ignoreYo: answer.ignoreYo };
  const given = normalizeText(raw, opts);

  if (given === '') return { score: 0, complete: false, feedback: 'Ответ пустой.' };

  if (answer.accepted?.some((a) => normalizeText(a, opts) === given)) {
    return { score: 1, complete: true };
  }

  if (answer.pattern !== undefined) {
    // Флаг u обязателен: без него \p{L} не работает, а \w не покрывает кириллицу,
    // из-за чего «^мо\w+» не матчит «море». Авторам заданий на русском нужен \p{L}.
    const re = new RegExp(answer.pattern, answer.caseSensitive ? 'u' : 'iu');
    if (re.test(raw.trim())) return { score: 1, complete: true };
  }

  return { score: 0, complete: false, feedback: 'Не то.' };
}

function gradeMultiChoice(
  item: Extract<TaskItem, { type: 'multi_choice' }>,
  chosen: number[],
): Grade {
  const picked = new Set(chosen);
  const correct = new Set(item.options.flatMap((o, i) => (o.correct ? [i] : [])));

  let hits = 0;
  let misses = 0;
  item.options.forEach((_, i) => {
    if (picked.has(i) && correct.has(i)) hits++;
    if (picked.has(i) !== correct.has(i)) misses++;
  });

  if (misses === 0) return { score: 1, complete: true };

  if (item.partialCredit === true && correct.size > 0) {
    // Лишние отметки штрафуют, иначе выгодно отметить всё подряд
    const wrongPicks = [...picked].filter((i) => !correct.has(i)).length;
    const score = Math.max(0, (hits - wrongPicks) / correct.size);
    return {
      score,
      complete: score > 0,
      feedback: score > 0 ? 'Засчитано частично.' : 'Не то.',
    };
  }

  return { score: 0, complete: false, feedback: 'Отмечено не всё верно.' };
}

function gradeOrdering(item: Extract<TaskItem, { type: 'ordering' }>, order: string[]): Grade {
  const correct = item.sequence.length === order.length
    && item.sequence.every((v, i) => v === order[i]);
  return correct
    ? { score: 1, complete: true }
    : { score: 0, complete: false, feedback: 'Порядок неверный.' };
}

function gradeMatching(
  item: Extract<TaskItem, { type: 'matching' }>,
  assignments: Record<string, string>,
): Grade {
  let hits = 0;
  for (const pair of item.pairs) {
    if (assignments[pair.left] === pair.right) hits++;
  }
  const score = hits / item.pairs.length;
  return {
    score,
    complete: score === 1,
    ...(score < 1 ? { feedback: `Верных пар: ${hits} из ${item.pairs.length}.` } : {}),
  };
}

function gradeCloze(
  item: Extract<TaskItem, { type: 'cloze' }>,
  blanks: Record<string, string>,
): Grade {
  let hits = 0;
  for (const blank of item.blanks) {
    const given = normalizeText(blanks[blank.id] ?? '');
    if (given !== '' && blank.accepted.some((a) => normalizeText(a) === given)) hits++;
  }
  const score = hits / item.blanks.length;
  return {
    score,
    complete: score === 1,
    ...(score < 1 ? { feedback: `Верных пропусков: ${hits} из ${item.blanks.length}.` } : {}),
  };
}

/**
 * Проверка ответа. Чистая функция: ни сети, ни времени, ни начисления кредитов —
 * начисление считает домен, и только на сервере.
 */
export function grade(item: TaskItem, answer: Answer): Grade {
  if (item.type !== answer.type) {
    throw new Error(`ответ типа "${answer.type}" не подходит заданию типа "${item.type}"`);
  }

  switch (answer.type) {
    case 'single_choice': {
      const option = item.type === 'single_choice' ? item.options[answer.optionIndex] : undefined;
      if (!option) return { score: 0, complete: false, feedback: 'Вариант не выбран.' };
      return option.correct
        ? { score: 1, complete: true, ...(option.feedback ? { feedback: option.feedback } : {}) }
        : { score: 0, complete: false, ...(option.feedback ? { feedback: option.feedback } : {}) };
    }
    case 'multi_choice':
      return item.type === 'multi_choice'
        ? gradeMultiChoice(item, answer.optionIndexes)
        : { score: 0, complete: false };
    case 'numeric':
      return item.type === 'numeric' ? gradeNumeric(item, answer.raw) : { score: 0, complete: false };
    case 'short_text':
      return item.type === 'short_text' ? gradeShortText(item, answer.raw) : { score: 0, complete: false };
    case 'ordering':
      return item.type === 'ordering' ? gradeOrdering(item, answer.order) : { score: 0, complete: false };
    case 'matching':
      return item.type === 'matching' ? gradeMatching(item, answer.assignments) : { score: 0, complete: false };
    case 'cloze':
      return item.type === 'cloze' ? gradeCloze(item, answer.blanks) : { score: 0, complete: false };
    case 'code':
      // Исполнение кода в песочнице — фаза 5. До неё задание не засчитывается автоматически.
      return { score: 0, complete: false, feedback: 'Проверка кода пока не реализована.' };

    // Ниже — типы без правильного ответа: кредиты за выполнение, а не за верность.
    case 'likert': {
      if (item.type !== 'likert') return { score: 0, complete: false };
      const inRange = answer.value >= item.scale.min && answer.value <= item.scale.max;
      return inRange
        ? { score: 1, complete: true }
        : { score: 0, complete: false, feedback: 'Значение вне шкалы.' };
    }
    case 'reflection': {
      if (item.type !== 'reflection') return { score: 0, complete: false };
      const required = item.minChars ?? 40;
      const length = answer.text.trim().length;
      return length >= required
        ? { score: 1, complete: true }
        : {
            score: 0,
            complete: false,
            feedback: `Напиши хотя бы ${required} символов — сейчас ${length}.`,
          };
    }
    case 'parent_verified':
      // Кредиты начислятся, когда родитель подтвердит в админке.
      return { score: 1, complete: false, pendingApproval: true, feedback: 'Отправлено на подтверждение родителю.' };
    case 'interactive': {
      if (item.type !== 'interactive') return { score: 0, complete: false };
      const rule = item.completionRule;
      if (!answer.completed) return { score: 0, complete: false, feedback: 'Упражнение не завершено.' };
      if (rule?.minDurationSec !== undefined && (answer.durationSec ?? 0) < rule.minDurationSec) {
        return { score: 0, complete: false, feedback: 'Слишком быстро — попробуй пройти всерьёз.' };
      }
      if (rule?.minScore !== undefined && (answer.score ?? 0) < rule.minScore) {
        return { score: answer.score ?? 0, complete: false, feedback: 'Результат ниже порога.' };
      }
      return { score: 1, complete: true };
    }
  }
}

export { isScored };
