import { describe, expect, it } from 'vitest';
import type { EconomyConfig, TaskItem, TaskPack } from '@mykids/contracts';
import { TaskSession, type AttemptHistory } from '@mykids/task-runner';

const economy: EconomyConfig = {
  creditsPerMinute: 2,
  maxConvertedMinutesPerDay: 45,
  minCreditsToConvert: 10,
  maxCreditsPerDay: 120,
};

const NOW = new Date('2026-03-09T12:00:00Z');

const numeric = (id: string, value: number): TaskItem => ({
  id, type: 'numeric', stem: `Сколько будет ${value}?`,
  answer: { value, tolerance: 0.01 }, credits: 5,
});

const pack = (items: TaskItem[], over: Partial<TaskPack['manifest']> = {}): TaskPack => ({
  manifest: {
    id: 'ru.mykids.test.pack', schemaVersion: 1, version: '1.0.0',
    title: 'Тест', subject: 'math', locale: 'ru-RU',
    reward: { creditsPerCorrect: 5, dailyCreditCap: 40 },
    items: items.map((_, i) => `items/${i}.json`),
    ...over,
  },
  items,
});

const history = (over: Partial<AttemptHistory> = {}): AttemptHistory => ({
  lastAwardedAt: {}, packCreditsToday: 0, creditsEarnedToday: 0, ...over,
});

describe('прохождение сессии', () => {
  it('идёт по заданиям и начисляет за верные ответы', () => {
    const s = new TaskSession({
      pack: pack([numeric('n1', 4), numeric('n2', 6)]),
      economy, history: history(), now: NOW,
    });

    expect(s.progress).toEqual({ index: 0, total: 2 });
    expect(s.submit({ type: 'numeric', raw: '4' }).creditsAwarded).toBe(5);
    expect(s.submit({ type: 'numeric', raw: '999' }).creditsAwarded).toBe(0);
    expect(s.finished).toBe(true);

    const sum = s.summary();
    expect(sum).toMatchObject({ total: 2, correct: 1, accuracy: 0.5, creditsEarned: 5 });
  });

  it('бросает исключение при ответе после конца сессии', () => {
    const s = new TaskSession({ pack: pack([numeric('n1', 4)]), economy, history: history(), now: NOW });
    s.submit({ type: 'numeric', raw: '4' });
    expect(() => s.submit({ type: 'numeric', raw: '4' })).toThrow(/завершена/);
  });
});

describe('потолки и cooldown', () => {
  it('урезает начисление остатком потолка пакета', () => {
    const s = new TaskSession({
      pack: pack([numeric('n1', 4)]),
      economy, history: history({ packCreditsToday: 37 }), now: NOW,
    });
    expect(s.submit({ type: 'numeric', raw: '4' }).creditsAwarded).toBe(3); // 40 - 37
  });

  it('потолок учитывает кредиты, набранные в этой же сессии', () => {
    const s = new TaskSession({
      pack: pack([numeric('n1', 1), numeric('n2', 2), numeric('n3', 3)],
                 { reward: { creditsPerCorrect: 5, dailyCreditCap: 12 } }),
      economy, history: history(), now: NOW,
    });
    expect(s.submit({ type: 'numeric', raw: '1' }).creditsAwarded).toBe(5);
    expect(s.submit({ type: 'numeric', raw: '2' }).creditsAwarded).toBe(5);
    // Осталось 2 из 12, хотя задание стоит 5
    expect(s.submit({ type: 'numeric', raw: '3' }).creditsAwarded).toBe(2);
    expect(s.summary().creditsEarned).toBe(12);
  });

  it('не начисляет за задание на cooldown, но даёт его пройти', () => {
    const item: TaskItem = { ...numeric('n1', 4), cooldownHours: 24 };
    const s = new TaskSession({
      pack: pack([item]),
      economy,
      history: history({ lastAwardedAt: { n1: new Date('2026-03-09T06:00:00Z') } }),
      now: NOW,
    });
    const r = s.submit({ type: 'numeric', raw: '4' });
    expect(r.grade.complete).toBe(true);
    expect(r.creditsAwarded).toBe(0);
    expect(r.withheldReason).toContain('кредиты');
  });

  it('соблюдает общий дневной потолок кредитов', () => {
    const s = new TaskSession({
      pack: pack([numeric('n1', 4)]),
      economy, history: history({ creditsEarnedToday: 118 }), now: NOW,
    });
    expect(s.submit({ type: 'numeric', raw: '4' }).creditsAwarded).toBe(2); // 120 - 118
  });
});

describe('частичный зачёт', () => {
  it('масштабирует кредиты долей верности, но не до нуля', () => {
    const matching: TaskItem = {
      id: 'm1', type: 'matching', stem: 'Сопоставь', credits: 10,
      pairs: [
        { left: 'A', right: '1' }, { left: 'B', right: '2' },
        { left: 'C', right: '3' }, { left: 'D', right: '4' },
      ],
    };
    const s = new TaskSession({ pack: pack([matching]), economy, history: history(), now: NOW });
    // Три пары из четырёх — 0.75 от 10 кредитов
    const r = s.submit({ type: 'matching', assignments: { A: '1', B: '2', C: '3', D: 'x' } });
    expect(r.grade.score).toBe(0.75);
    expect(r.creditsAwarded).toBe(8);
  });

  it('минимальная ненулевая доля всё равно даёт хотя бы один кредит', () => {
    const matching: TaskItem = {
      id: 'm2', type: 'matching', stem: 'Сопоставь', credits: 2,
      pairs: Array.from({ length: 5 }, (_, i) => ({ left: `L${i}`, right: `R${i}` })),
    };
    const s = new TaskSession({ pack: pack([matching]), economy, history: history(), now: NOW });
    const r = s.submit({ type: 'matching', assignments: { L0: 'R0' } }); // 0.2 * 2 = 0.4
    expect(r.creditsAwarded).toBe(1);
  });
});

describe('итог сессии', () => {
  it('даёт бонус за идеальное прохождение', () => {
    const s = new TaskSession({
      pack: pack([numeric('n1', 4), numeric('n2', 6)],
                 { reward: { creditsPerCorrect: 5, bonusOnPerfect: 7, dailyCreditCap: 40 } }),
      economy, history: history(), now: NOW,
    });
    s.submit({ type: 'numeric', raw: '4' });
    s.submit({ type: 'numeric', raw: '6' });
    const sum = s.summary();
    expect(sum.bonusAwarded).toBe(7);
    expect(sum.creditsEarned).toBe(17);
  });

  it('не даёт бонус при единственной ошибке', () => {
    const s = new TaskSession({
      pack: pack([numeric('n1', 4), numeric('n2', 6)],
                 { reward: { creditsPerCorrect: 5, bonusOnPerfect: 7, dailyCreditCap: 40 } }),
      economy, history: history(), now: NOW,
    });
    s.submit({ type: 'numeric', raw: '4' });
    s.submit({ type: 'numeric', raw: '0' });
    expect(s.summary().bonusAwarded).toBe(0);
  });

  it('бонус тоже проходит через дневной потолок', () => {
    const s = new TaskSession({
      pack: pack([numeric('n1', 4)],
                 { reward: { creditsPerCorrect: 5, bonusOnPerfect: 50, dailyCreditCap: 8 } }),
      economy, history: history(), now: NOW,
    });
    s.submit({ type: 'numeric', raw: '4' });
    const sum = s.summary();
    expect(sum.bonusAwarded).toBe(3); // 8 - 5, а не 50
    expect(sum.creditsEarned).toBe(8);
  });

  it('порог прохождения считается по оцениваемым заданиям', () => {
    const s = new TaskSession({
      pack: pack([numeric('n1', 4), numeric('n2', 6)], { delivery: { passThreshold: 0.7 } }),
      economy, history: history(), now: NOW,
    });
    s.submit({ type: 'numeric', raw: '4' });
    s.submit({ type: 'numeric', raw: '0' });
    expect(s.summary().passed).toBe(false); // 0.5 < 0.7
  });

  it('пакет без оцениваемых заданий считается пройденным', () => {
    const reflection: TaskItem = { id: 'r1', type: 'reflection', stem: 'Как день?', minChars: 5, credits: 3 };
    const s = new TaskSession({
      pack: pack([reflection], { delivery: { passThreshold: 0.7 } }),
      economy, history: history(), now: NOW,
    });
    s.submit({ type: 'reflection', text: 'Всё хорошо, спасибо' });
    const sum = s.summary();
    expect(sum.accuracy).toBe(1);
    expect(sum.passed).toBe(true);
    expect(sum.creditsEarned).toBe(3);
  });

  it('задания на подтверждение родителем не приносят кредиты сразу', () => {
    const pv: TaskItem = {
      id: 'p1', type: 'parent_verified', stem: 'Расскажи родителю',
      verificationPrompt: 'Рассказал?', credits: 5,
    };
    const s = new TaskSession({ pack: pack([pv]), economy, history: history(), now: NOW });
    const r = s.submit({ type: 'parent_verified', requested: true });
    expect(r.creditsAwarded).toBe(0);
    expect(s.summary().pendingApproval).toEqual(['p1']);
  });
});

describe('отбор заданий', () => {
  const many = Array.from({ length: 10 }, (_, i) => numeric(`n${i}`, i));

  it('sequential берёт первые N по порядку', () => {
    const s = new TaskSession({
      pack: pack(many, { delivery: { itemsPerSession: 3, selection: 'sequential' } }),
      economy, history: history(), now: NOW,
    });
    expect(s.items.map((i) => i.id)).toEqual(['n0', 'n1', 'n2']);
  });

  it('random перемешивает и берёт N', () => {
    const s = new TaskSession({
      pack: pack(many, { delivery: { itemsPerSession: 3, selection: 'random' } }),
      economy, history: history(), now: NOW,
      random: () => 0.99,
    });
    expect(s.items).toHaveLength(3);
    expect(new Set(s.items.map((i) => i.id)).size).toBe(3);
  });

  it('spaced ставит вперёд то, что снова приносит кредиты', () => {
    const s = new TaskSession({
      pack: pack(many, { delivery: { itemsPerSession: 3, selection: 'spaced', cooldownHoursPerItem: 24 } }),
      economy,
      history: history({
        lastAwardedAt: {
          n0: new Date('2026-03-09T11:00:00Z'), // час назад — ещё на cooldown
          n1: new Date('2026-03-09T11:00:00Z'),
          n2: new Date('2026-03-09T11:00:00Z'),
        },
      }),
      now: NOW,
    });
    // n0..n2 на cooldown, поэтому в сессию попадают более «свежие» задания
    expect(s.items.map((i) => i.id)).not.toContain('n0');
  });
});
