import type { TimeWindow } from '@mykids/contracts';

/**
 * Сутки, неделя и окна расписания считаются в часовом поясе семьи, а не в UTC.
 * Ребёнок в UTC+3 не должен получать новый дневной лимит в три часа ночи, и
 * «отбой в 21:30» обязан означать 21:30 по его часам.
 */

const PARTS = new Map<string, Intl.DateTimeFormat>();

function formatter(timezone: string): Intl.DateTimeFormat {
  let fmt = PARTS.get(timezone);
  if (!fmt) {
    fmt = new Intl.DateTimeFormat('en-US', {
      timeZone: timezone,
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      hourCycle: 'h23',
      weekday: 'short',
    });
    PARTS.set(timezone, fmt);
  }
  return fmt;
}

const WEEKDAYS: Record<string, number> = {
  Sun: 0, Mon: 1, Tue: 2, Wed: 3, Thu: 4, Fri: 5, Sat: 6,
};

export interface LocalMoment {
  /** Ключ суток "YYYY-MM-DD" в локальном поясе. */
  day: string;
  /** 0 — воскресенье, как у Date.getDay(). */
  weekday: number;
  /** Минут от локальной полуночи, 0..1439. */
  minutesOfDay: number;
}

export function localMoment(at: Date, timezone: string): LocalMoment {
  const parts = formatter(timezone).formatToParts(at);
  const get = (type: Intl.DateTimeFormatPartTypes): string => {
    const found = parts.find((p) => p.type === type);
    if (!found) throw new Error(`не удалось разобрать часть даты: ${type}`);
    return found.value;
  };

  const weekday = WEEKDAYS[get('weekday')];
  if (weekday === undefined) throw new Error(`неизвестный день недели: ${get('weekday')}`);

  return {
    day: `${get('year')}-${get('month')}-${get('day')}`,
    weekday,
    minutesOfDay: Number(get('hour')) * 60 + Number(get('minute')),
  };
}

export function localDay(at: Date, timezone: string): string {
  return localMoment(at, timezone).day;
}

/** "HH:MM" → минуты от полуночи. */
export function parseTimeOfDay(value: string): number {
  const [h, m] = value.split(':');
  return Number(h) * 60 + Number(m);
}

/**
 * Попадает ли момент в окно.
 *
 * Окно с `to` меньше `from` пересекает полночь. Его дни недели относятся к дню
 * НАЧАЛА окна: отбой 21:30–07:00 по будням действует и в 06:00 субботы, потому
 * что это продолжение пятничной ночи. Обратная трактовка — типичная ошибка,
 * из-за которой подросток получает свободное утро субботы и заблокированное
 * утро понедельника.
 */
export function isWithinWindow(moment: LocalMoment, window: TimeWindow): boolean {
  const from = parseTimeOfDay(window.from);
  const to = parseTimeOfDay(window.to);
  const days = new Set(window.days);

  if (from === to) return false; // вырожденное окно нулевой длины

  if (from < to) {
    return days.has(moment.weekday) && moment.minutesOfDay >= from && moment.minutesOfDay < to;
  }

  if (moment.minutesOfDay >= from) return days.has(moment.weekday);
  if (moment.minutesOfDay < to) return days.has((moment.weekday + 6) % 7);
  return false;
}

/** Сколько часов прошло между двумя моментами. */
export function hoursBetween(from: Date, to: Date): number {
  return (to.getTime() - from.getTime()) / 3_600_000;
}
