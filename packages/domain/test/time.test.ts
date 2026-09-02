import { describe, expect, it } from 'vitest';
import type { TimeWindow } from '@mykids/contracts';
import { isWithinWindow, localDay, localMoment, parseTimeOfDay } from '@mykids/domain';

const MOSCOW = 'Europe/Moscow'; // UTC+3, без перехода на летнее время

describe('локальные сутки', () => {
  it('считаются по часовому поясу семьи, а не по UTC', () => {
    // 22:30 UTC — это уже следующий день в Москве
    const at = new Date('2026-03-10T22:30:00Z');
    expect(localDay(at, 'UTC')).toBe('2026-03-10');
    expect(localDay(at, MOSCOW)).toBe('2026-03-11');
  });

  it('полночь по локальному времени даёт нулевую минуту, а не 24 час', () => {
    const at = new Date('2026-03-10T21:00:00Z'); // 00:00 по Москве
    expect(localMoment(at, MOSCOW).minutesOfDay).toBe(0);
  });

  it('переживает переход на летнее время', () => {
    // В Берлине 29 марта 2026 в 02:00 часы прыгают на 03:00
    const before = new Date('2026-03-29T00:30:00Z'); // 01:30 CET
    const after = new Date('2026-03-29T01:30:00Z'); // 03:30 CEST
    expect(localMoment(before, 'Europe/Berlin').minutesOfDay).toBe(90);
    expect(localMoment(after, 'Europe/Berlin').minutesOfDay).toBe(210);
  });

  it('день недели совпадает с нумерацией Date.getDay()', () => {
    // 2026-03-08 — воскресенье
    expect(localMoment(new Date('2026-03-08T12:00:00Z'), 'UTC').weekday).toBe(0);
    expect(localMoment(new Date('2026-03-09T12:00:00Z'), 'UTC').weekday).toBe(1);
  });
});

describe('parseTimeOfDay', () => {
  it('переводит HH:MM в минуты от полуночи', () => {
    expect(parseTimeOfDay('00:00')).toBe(0);
    expect(parseTimeOfDay('21:30')).toBe(1290);
    expect(parseTimeOfDay('23:59')).toBe(1439);
  });
});

describe('окна расписания', () => {
  const school: TimeWindow = {
    name: 'школа', days: [1, 2, 3, 4, 5], from: '08:00', to: '14:00', mode: 'blocked',
  };
  const bedtime: TimeWindow = {
    name: 'отбой', days: [1, 2, 3, 4, 5], from: '21:30', to: '07:00', mode: 'blocked',
  };

  const moment = (weekday: number, hhmm: string) => ({
    day: '2026-03-09', weekday, minutesOfDay: parseTimeOfDay(hhmm),
  });

  it('обычное окно ловит время внутри и не ловит снаружи', () => {
    expect(isWithinWindow(moment(1, '09:00'), school)).toBe(true);
    expect(isWithinWindow(moment(1, '07:59'), school)).toBe(false);
    expect(isWithinWindow(moment(1, '14:00'), school)).toBe(false); // верхняя граница исключена
    expect(isWithinWindow(moment(0, '09:00'), school)).toBe(false); // воскресенье
  });

  it('окно через полночь действует вечером дня из списка', () => {
    expect(isWithinWindow(moment(1, '22:00'), bedtime)).toBe(true);
    expect(isWithinWindow(moment(1, '21:29'), bedtime)).toBe(false);
  });

  it('утренняя часть ночного окна относится к предыдущему дню', () => {
    // Суббота 06:00 — это продолжение пятничной ночи, отбой ещё действует
    expect(isWithinWindow(moment(6, '06:00'), bedtime)).toBe(true);
    // А вечер субботы уже свободен: субботы нет в списке дней
    expect(isWithinWindow(moment(6, '22:00'), bedtime)).toBe(false);
    // Утро понедельника — продолжение воскресной ночи, отбой не действует
    expect(isWithinWindow(moment(1, '06:00'), bedtime)).toBe(false);
  });

  it('вырожденное окно нулевой длины никогда не срабатывает', () => {
    const zero: TimeWindow = { ...bedtime, from: '10:00', to: '10:00' };
    expect(isWithinWindow(moment(1, '10:00'), zero)).toBe(false);
  });
});
