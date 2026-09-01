import { describe, expect, it } from 'vitest';
import type { TaskItem } from '@mykids/contracts';
import { grade, normalizeText, parseNumericAnswer } from '@mykids/task-runner';

describe('parseNumericAnswer', () => {
  it('читает число с единицами и без', () => {
    expect(parseNumericAnswer('6')).toEqual({ value: 6, unit: '' });
    expect(parseNumericAnswer('6 Н')).toEqual({ value: 6, unit: 'Н' });
    expect(parseNumericAnswer('6Н')).toEqual({ value: 6, unit: 'Н' });
    expect(parseNumericAnswer('  -2.5 м/с  ')).toEqual({ value: -2.5, unit: 'м/с' });
  });

  it('принимает запятую как десятичный разделитель', () => {
    // На русской раскладке ребёнок наберёт запятую; отказ по такому поводу — придирка
    expect(parseNumericAnswer('7,9 г/см³')).toEqual({ value: 7.9, unit: 'г/см³' });
  });

  it('возвращает null на нечисловом вводе', () => {
    expect(parseNumericAnswer('не знаю')).toBeNull();
    expect(parseNumericAnswer('')).toBeNull();
  });
});

describe('normalizeText', () => {
  it('схлопывает регистр, пробелы и ё', () => {
    expect(normalizeText('  Ёлка   Зелёная ')).toBe('елка зеленая');
  });
  it('уважает caseSensitive', () => {
    expect(normalizeText('Париж', { caseSensitive: true })).toBe('Париж');
  });
  it('можно отключить схлопывание ё', () => {
    expect(normalizeText('ёж', { ignoreYo: false })).toBe('ёж');
  });
});

describe('numeric', () => {
  const item: TaskItem = {
    id: 'phys-001', type: 'numeric', stem: 'F = ma при m=2, a=3?',
    answer: { value: 6, unit: 'Н', tolerance: 0.01, acceptedUnits: ['N', 'н'] },
  };

  it('засчитывает точный ответ с единицами и без', () => {
    expect(grade(item, { type: 'numeric', raw: '6 Н' }).complete).toBe(true);
    expect(grade(item, { type: 'numeric', raw: '6' }).complete).toBe(true);
  });

  it('принимает альтернативные написания единиц', () => {
    expect(grade(item, { type: 'numeric', raw: '6 N' }).complete).toBe(true);
    expect(grade(item, { type: 'numeric', raw: '6 н' }).complete).toBe(true);
  });

  it('отвергает верное число в неверных единицах', () => {
    const r = grade(item, { type: 'numeric', raw: '6 кг' });
    expect(r.complete).toBe(false);
    expect(r.feedback).toContain('единицы');
  });

  it('соблюдает абсолютный допуск', () => {
    expect(grade(item, { type: 'numeric', raw: '6.005' }).complete).toBe(true);
    expect(grade(item, { type: 'numeric', raw: '6.02' }).complete).toBe(false);
  });

  it('поддерживает относительный допуск', () => {
    const rel: TaskItem = {
      id: 'phys-002', type: 'numeric', stem: 'g?',
      answer: { value: 9.8, relativeTolerance: 0.05 },
    };
    expect(grade(rel, { type: 'numeric', raw: '10' }).complete).toBe(true);  // 2% отклонения
    expect(grade(rel, { type: 'numeric', raw: '11' }).complete).toBe(false); // 12%
  });

  it('без допуска сверяет точно', () => {
    const exact: TaskItem = { id: 'm-001', type: 'numeric', stem: '2+2?', answer: { value: 4 } };
    expect(grade(exact, { type: 'numeric', raw: '4' }).complete).toBe(true);
    expect(grade(exact, { type: 'numeric', raw: '4.0001' }).complete).toBe(false);
  });

  it('не падает на мусорном вводе', () => {
    expect(grade(item, { type: 'numeric', raw: 'шесть' }).complete).toBe(false);
  });
});

describe('single_choice', () => {
  const item: TaskItem = {
    id: 'ph-003', type: 'single_choice', stem: 'Паскаль — единица чего?',
    options: [
      { text: 'Давления', correct: true, feedback: '1 Па = 1 Н/м².' },
      { text: 'Силы', correct: false, feedback: 'Сила измеряется в ньютонах.' },
    ],
  };

  it('засчитывает верный вариант и отдаёт его пояснение', () => {
    const r = grade(item, { type: 'single_choice', optionIndex: 0 });
    expect(r).toMatchObject({ score: 1, complete: true, feedback: '1 Па = 1 Н/м².' });
  });

  it('объясняет неверный выбор', () => {
    const r = grade(item, { type: 'single_choice', optionIndex: 1 });
    expect(r).toMatchObject({ score: 0, complete: false });
    expect(r.feedback).toContain('ньютонах');
  });

  it('не падает на индексе вне диапазона', () => {
    expect(grade(item, { type: 'single_choice', optionIndex: 99 }).complete).toBe(false);
    expect(grade(item, { type: 'single_choice', optionIndex: -1 }).complete).toBe(false);
  });
});

describe('multi_choice', () => {
  const base = {
    id: 'mc-001', type: 'multi_choice' as const, stem: 'Что верно?',
    options: [
      { text: 'A', correct: true }, { text: 'B', correct: true },
      { text: 'C', correct: false }, { text: 'D', correct: false },
    ],
  };

  it('засчитывает полное совпадение', () => {
    expect(grade(base, { type: 'multi_choice', optionIndexes: [0, 1] }).score).toBe(1);
  });

  it('без частичного зачёта неполный набор не проходит', () => {
    expect(grade(base, { type: 'multi_choice', optionIndexes: [0] }).score).toBe(0);
  });

  it('с частичным зачётом даёт долю', () => {
    const partial: TaskItem = { ...base, partialCredit: true };
    expect(grade(partial, { type: 'multi_choice', optionIndexes: [0] }).score).toBe(0.5);
  });

  it('штрафует лишние отметки, иначе выгодно отметить всё', () => {
    const partial: TaskItem = { ...base, partialCredit: true };
    // отмечено всё: 2 верных минус 2 лишних = 0
    expect(grade(partial, { type: 'multi_choice', optionIndexes: [0, 1, 2, 3] }).score).toBe(0);
  });

  it('пустой ответ не засчитывается', () => {
    expect(grade(base, { type: 'multi_choice', optionIndexes: [] }).score).toBe(0);
  });
});

describe('short_text', () => {
  it('сверяет по списку вариантов, игнорируя регистр и ё', () => {
    const item: TaskItem = {
      id: 'st-001', type: 'short_text', stem: 'Столица Франции?',
      answer: { accepted: ['Париж'] },
    };
    expect(grade(item, { type: 'short_text', raw: '  париж ' }).complete).toBe(true);
    expect(grade(item, { type: 'short_text', raw: 'Лондон' }).complete).toBe(false);
    expect(grade(item, { type: 'short_text', raw: '' }).complete).toBe(false);
  });

  it('поддерживает regex с юникод-классами', () => {
    const item: TaskItem = {
      id: 'st-002', type: 'short_text', stem: 'Любое слово на «мо»',
      answer: { pattern: '^мо\\p{L}+$' },
    };
    expect(grade(item, { type: 'short_text', raw: 'море' }).complete).toBe(true);
    expect(grade(item, { type: 'short_text', raw: 'река' }).complete).toBe(false);
  });

  it('\\w остаётся ASCII-классом — для кириллицы нужен \\p{L}', () => {
    // Ловушка для авторов заданий на русском: «^мо\\w+» не матчит «море».
    // Тест фиксирует поведение, чтобы оно не менялось молча.
    const asciiOnly: TaskItem = {
      id: 'st-003', type: 'short_text', stem: 'Слово на «мо»',
      answer: { pattern: '^мо\\w+$' },
    };
    expect(grade(asciiOnly, { type: 'short_text', raw: 'море' }).complete).toBe(false);
    expect(grade(asciiOnly, { type: 'short_text', raw: 'moon' }).complete).toBe(false);
  });
});

describe('ordering, matching, cloze', () => {
  it('ordering требует точного порядка', () => {
    const item: TaskItem = { id: 'or-001', type: 'ordering', stem: 'По возрастанию', sequence: ['1', '2', '3'] };
    expect(grade(item, { type: 'ordering', order: ['1', '2', '3'] }).score).toBe(1);
    expect(grade(item, { type: 'ordering', order: ['1', '3', '2'] }).score).toBe(0);
    expect(grade(item, { type: 'ordering', order: ['1', '2'] }).score).toBe(0);
  });

  it('matching считает долю верных пар', () => {
    const item: TaskItem = {
      id: 'ma-001', type: 'matching', stem: 'Сопоставь',
      pairs: [
        { left: 'Сила', right: 'ньютон' },
        { left: 'Работа', right: 'джоуль' },
      ],
    };
    expect(grade(item, { type: 'matching', assignments: { Сила: 'ньютон', Работа: 'джоуль' } }).score).toBe(1);
    const half = grade(item, { type: 'matching', assignments: { Сила: 'ньютон', Работа: 'ватт' } });
    expect(half.score).toBe(0.5);
    expect(half.complete).toBe(false);
  });

  it('cloze считает долю верных пропусков', () => {
    const item: TaskItem = {
      id: 'cl-001', type: 'cloze', stem: 'Вода кипит при {{t}} градусах',
      blanks: [{ id: 't', accepted: ['100', 'ста'] }],
    };
    expect(grade(item, { type: 'cloze', blanks: { t: '100' } }).score).toBe(1);
    expect(grade(item, { type: 'cloze', blanks: { t: 'ста' } }).score).toBe(1);
    expect(grade(item, { type: 'cloze', blanks: {} }).score).toBe(0);
  });
});

describe('типы без правильного ответа', () => {
  it('likert засчитывает любое значение внутри шкалы', () => {
    const item: TaskItem = {
      id: 'lk-001', type: 'likert', stem: 'Мне легко сосредоточиться',
      scale: { min: 1, max: 5 },
    };
    expect(grade(item, { type: 'likert', value: 1 }).complete).toBe(true);
    expect(grade(item, { type: 'likert', value: 5 }).complete).toBe(true);
    expect(grade(item, { type: 'likert', value: 6 }).complete).toBe(false);
  });

  it('reflection требует минимальной длины и говорит, сколько не хватает', () => {
    const item: TaskItem = { id: 'rf-001', type: 'reflection', stem: 'Как прошёл день?', minChars: 20 };
    expect(grade(item, { type: 'reflection', text: 'Хорошо' }).complete).toBe(false);
    expect(grade(item, { type: 'reflection', text: 'x'.repeat(20) }).complete).toBe(true);
    expect(grade(item, { type: 'reflection', text: 'коротко' }).feedback).toContain('20');
  });

  it('parent_verified уходит на подтверждение, а не засчитывается сразу', () => {
    const item: TaskItem = {
      id: 'pv-001', type: 'parent_verified', stem: 'Расскажи родителю о своей суперсиле',
      verificationPrompt: 'Рассказал?',
    };
    const r = grade(item, { type: 'parent_verified', requested: true });
    expect(r.complete).toBe(false);
    expect(r.pendingApproval).toBe(true);
  });

  it('interactive проверяет завершённость и порог времени', () => {
    const item: TaskItem = {
      id: 'in-001', type: 'interactive', stem: 'Таблица Шульте',
      widget: 'schulte', completionRule: { minDurationSec: 30 },
    };
    expect(grade(item, { type: 'interactive', completed: false }).complete).toBe(false);
    // Мгновенное «прохождение» — попытка сфармить кредиты, не выполняя упражнение
    const rushed = grade(item, { type: 'interactive', completed: true, durationSec: 2 });
    expect(rushed.complete).toBe(false);
    expect(rushed.feedback).toContain('быстро');
    expect(grade(item, { type: 'interactive', completed: true, durationSec: 45 }).complete).toBe(true);
  });
});

describe('защита от рассогласования', () => {
  it('ответ не того типа — это ошибка программиста, а не тихий ноль', () => {
    const item: TaskItem = { id: 'nm-001', type: 'numeric', stem: '2+2?', answer: { value: 4 } };
    expect(() => grade(item, { type: 'short_text', raw: '4' })).toThrow(/не подходит/);
  });
});
