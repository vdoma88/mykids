import { describe, expect, it } from 'vitest';
import { defaultRepeatConfig, repeatAward } from '../src/repeat.js';

const now = new Date('2026-09-22T12:00:00Z');
const daysAgo = (d: number) => new Date(now.getTime() - d * 24 * 60 * 60 * 1000);

describe('повторы', () => {
  it('первое решение приносит полную награду', () => {
    const r = repeatAward({ credits: 10, awardedAt: [], now });
    expect(r).toEqual({ credits: 10, nth: 1, note: '' });
  });

  it('повтор стоит меньше', () => {
    // Ради этого всё и сделано: иначе выгоднее всего решать одни и те же
    // несколько заданий, которые уже знаешь наизусть.
    const second = repeatAward({ credits: 10, awardedAt: [daysAgo(1)], now });
    const third = repeatAward({ credits: 10, awardedAt: [daysAgo(1), daysAgo(2)], now });
    expect(second.credits).toBe(5);
    expect(third.credits).toBe(3);
    expect(second.credits).toBeLessThan(10);
    expect(third.credits).toBeLessThan(second.credits);
  });

  it('но никогда не до нуля', () => {
    // Повторение — это и есть способ запомнить. Платить за него совсем ничего
    // значило бы отучать повторять.
    const many = Array.from({ length: 20 }, (_, i) => daysAgo(i + 1));
    const r = repeatAward({ credits: 10, awardedAt: many, now });
    expect(r.credits).toBeGreaterThan(0);
    // И дальше четверти не падает: последняя доля — это пол.
    expect(r.credits).toBe(3);
  });

  it('маленькая награда не обнуляется округлением', () => {
    // Задание в один кредит на второй раз дало бы 0.5 → округление вниз
    // превратило бы решённое в «не засчитано».
    expect(repeatAward({ credits: 1, awardedAt: [daysAgo(1)], now }).credits).toBe(1);
    expect(repeatAward({ credits: 2, awardedAt: [daysAgo(1), daysAgo(2)], now }).credits).toBe(1);
  });

  it('возврат после долгого перерыва стоит полной награды', () => {
    // Самое полезное повторение из возможных. Наказывать за него было бы
    // ровно наоборот тому, ради чего всё это.
    const long = daysAgo(defaultRepeatConfig.windowDays + 1);
    const r = repeatAward({ credits: 10, awardedAt: [long], now });
    expect(r.credits).toBe(10);
    expect(r.nth).toBe(1);
    expect(r.note).toBe('');
  });

  it('старые повторы не складываются с недавними', () => {
    const r = repeatAward({
      credits: 10,
      awardedAt: [daysAgo(1), daysAgo(100), daysAgo(200), daysAgo(300)],
      now,
    });
    expect(r.nth).toBe(2); // только вчерашний попал в окно
    expect(r.credits).toBe(5);
  });

  it('неверный ответ остаётся без награды и без нотаций', () => {
    const r = repeatAward({ credits: 0, awardedAt: [daysAgo(1)], now });
    expect(r.credits).toBe(0);
    expect(r.note).toBe('');
  });

  it('объяснение называет числа и не бранится', () => {
    const r = repeatAward({ credits: 10, awardedAt: [daysAgo(1)], now });
    expect(r.note).toContain('5');
    expect(r.note).toContain('10');
    // Тон тот же, что и везде: правило, а не упрёк.
    for (const bad of ['нельзя', 'хватит', 'опять', 'снова ты', 'жульнич', 'обман']) {
      expect(r.note.toLowerCase()).not.toContain(bad);
    }
    // И говорит, что делать вместо этого.
    expect(r.note).toContain('Новое задание');
  });

  it('склонение кредитов по-русски', () => {
    const say = (credits: number) =>
      repeatAward({ credits: credits * 2, awardedAt: [daysAgo(1)], now }).note;
    expect(say(1)).toContain('1 кредит ');
    expect(say(2)).toContain('2 кредита ');
    expect(say(5)).toContain('5 кредитов ');
    expect(say(11)).toContain('11 кредитов ');
  });

  it('будущие отметки не считаются повторами', () => {
    // Перепутанный порядок — ошибка в данных, а не поведение ребёнка.
    const r = repeatAward({ credits: 10, awardedAt: [new Date(now.getTime() + 60_000)], now });
    expect(r.nth).toBe(1);
    expect(r.credits).toBe(10);
  });

  it('настройки можно сделать строже', () => {
    const r = repeatAward({
      credits: 100,
      awardedAt: [daysAgo(1)],
      now,
      config: { factors: [1, 0.1], windowDays: 7 },
    });
    expect(r.credits).toBe(10);
  });
});
