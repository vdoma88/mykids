import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import type { TimeWindow } from '@mykids/contracts';
import { isWithinWindow, localMoment } from '@mykids/domain';

/**
 * Те же векторы прогоняет агент на Go (agents/windows/internal/schedule).
 * Логика окон реализована дважды — сервер считает онлайн, агент обязан уметь
 * офлайн. Общий файл не даёт реализациям разъехаться.
 */
const vectors = JSON.parse(
  readFileSync(new URL('../../contracts/test-vectors/schedule.json', import.meta.url), 'utf8'),
) as {
  timezone: string;
  windows: TimeWindow[];
  cases: { at: string; local: string; expect: string | null; note: string }[];
};

/** Первое сработавшее окно, в порядке объявления. */
function matchWindow(at: Date): string | null {
  const moment = localMoment(at, vectors.timezone);
  const hit = vectors.windows.find((w) => isWithinWindow(moment, w));
  return hit?.name ?? null;
}

describe('векторы расписания', () => {
  it('файл разобран и не пуст', () => {
    expect(vectors.cases.length).toBeGreaterThan(10);
  });

  it.each(vectors.cases)('$local — $note', ({ at, expect: want }) => {
    expect(matchWindow(new Date(at))).toBe(want);
  });
});
