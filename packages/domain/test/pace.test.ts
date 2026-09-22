import { describe, expect, it } from 'vitest';
import { checkPace, defaultPaceConfig } from '../src/pace.js';

const t0 = new Date('2026-09-22T12:00:00Z');

/** Попытки с постоянным промежутком, новейшая первой. */
function every(seconds: number, count: number, from = t0): Date[] {
  return Array.from({ length: count }, (_, i) => new Date(from.getTime() - (i + 1) * seconds * 1000));
}

describe('темп ответов', () => {
  it('прокликанный наугад пакет перестаёт приносить кредиты', () => {
    // Ради этого всё и сделано: десять заданий за полминуты — это угадывание,
    // и четверть попаданий на четырёх вариантах ответа случайна.
    const v = checkPace({ previousAt: every(2, 9), now: t0 });
    expect(v.tooFast).toBe(true);
  });

  it('одна быстрая попытка ничего не значит', () => {
    // Ребёнок, который знает ответ, отвечает быстро. Это цель, а не нарушение.
    const v = checkPace({ previousAt: [new Date(t0.getTime() - 1000), ...every(60, 5, new Date(t0.getTime() - 1000))], now: t0 });
    expect(v.tooFast).toBe(false);
    expect(v.fastStreak).toBe(1);
  });

  it('серия рвётся, стоит один раз подумать', () => {
    // Две быстрых, потом нормальная пауза, потом ещё быстрая — это не серия.
    const previousAt = [
      new Date(t0.getTime() - 2_000),
      new Date(t0.getTime() - 4_000),
      new Date(t0.getTime() - 90_000),
      new Date(t0.getTime() - 92_000),
    ];
    const v = checkPace({ previousAt, now: t0 });
    expect(v.tooFast).toBe(false);
  });

  it('спокойная работа не трогается вовсе', () => {
    const v = checkPace({ previousAt: every(45, 20), now: t0 });
    expect(v).toEqual({ tooFast: false, fastStreak: 0 });
  });

  it('первая попытка за день не считается быстрой', () => {
    // Сравнивать не с чем, и придумывать сравнение нельзя.
    expect(checkPace({ previousAt: [], now: t0 })).toEqual({ tooFast: false, fastStreak: 0 });
  });

  it('порог серии соблюдается ровно', () => {
    const { streak, minSecondsBetween } = defaultPaceConfig;
    const fast = minSecondsBetween - 1;
    expect(checkPace({ previousAt: every(fast, streak - 1), now: t0 }).tooFast).toBe(false);
    expect(checkPace({ previousAt: every(fast, streak), now: t0 }).tooFast).toBe(true);
  });

  it('промежуток ровно в порог быстрым не считается', () => {
    // Граница включительно в пользу ребёнка: спорный случай не наказываем.
    const v = checkPace({ previousAt: every(defaultPaceConfig.minSecondsBetween, 10), now: t0 });
    expect(v.tooFast).toBe(false);
  });

  it('сообщение называет правило, а не ставит диагноз', () => {
    const v = checkPace({ previousAt: every(1, 5), now: t0 });
    if (!v.tooFast) throw new Error('ожидался слишком быстрый темп');

    // Обвинение, прочитанное подростком незаслуженно, дороже пропущенной
    // попытки. Поэтому слов про обман в сообщении быть не должно.
    for (const word of ['обман', 'жульнич', 'читер', 'нечестн', 'врёшь', 'списыва']) {
      expect(v.message.toLowerCase()).not.toContain(word);
    }
    // И оно обязано говорить, что делать, а не только что не так.
    expect(v.message).toContain('не торопясь');
  });

  it('перепутанный порядок попыток не принимается за угадывание', () => {
    // Отрицательный промежуток — ошибка в данных, а не поведение ребёнка.
    //
    // Все попытки в будущем: без защиты каждый промежуток отрицателен, то есть
    // «меньше порога», и серия набралась бы до обвинения на пустом месте.
    // Одной перепутанной пары для этой проверки мало — она оборвала бы серию
    // сама собой и прошла бы даже без защиты.
    const previousAt = [1, 2, 3, 4, 5].map((s) => new Date(t0.getTime() + s * 1000));
    const v = checkPace({ previousAt, now: t0 });
    expect(v.tooFast).toBe(false);
    expect(v.fastStreak).toBe(0);
  });

  it('настройки можно задать построже', () => {
    const previousAt = every(10, 5);
    expect(checkPace({ previousAt, now: t0 }).tooFast).toBe(false);
    const strict = checkPace({ previousAt, now: t0, config: { minSecondsBetween: 30, streak: 3 } });
    expect(strict.tooFast).toBe(true);
  });

  it('длина серии видна родителю, а не только вердикт', () => {
    // В журнале «отклонено» ничего не объясняет, а «пять ответов за пять
    // секунд» объясняет всё.
    const v = checkPace({ previousAt: every(1, 5), now: t0 });
    expect(v.fastStreak).toBe(5);
  });
});
