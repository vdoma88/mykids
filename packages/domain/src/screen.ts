import type { Policy } from '@mykids/contracts';
import { isWithinWindow, localMoment } from './time.js';

export type ScreenState =
  | { allowed: true; reason: 'within_allowance'; minutesLeft: number }
  | { allowed: false; reason: 'window_blocked'; window: string }
  | { allowed: false; reason: 'tasks_only'; window: string }
  | { allowed: false; reason: 'out_of_minutes'; minutesLeft: 0 };

/**
 * Можно ли сейчас пользоваться экраном.
 *
 * Окна проверяются раньше баланса: купленные минуты не должны отменять отбой,
 * иначе магазин превращается в способ обойти расписание. Явное окно `allowed`
 * перекрывает блокирующие — это «белый список» для, например, кружка по субботам.
 */
export function evaluateScreen(policy: Policy, minutesBalance: number, at: Date): ScreenState {
  const moment = localMoment(at, policy.timezone);

  const matched = policy.windows.filter((w) => isWithinWindow(moment, w));
  if (!matched.some((w) => w.mode === 'allowed')) {
    const blocking = matched.find((w) => w.mode === 'blocked');
    if (blocking) return { allowed: false, reason: 'window_blocked', window: blocking.name };

    const tasksOnly = matched.find((w) => w.mode === 'tasks_only');
    if (tasksOnly) return { allowed: false, reason: 'tasks_only', window: tasksOnly.name };
  }

  if (minutesBalance <= 0) return { allowed: false, reason: 'out_of_minutes', minutesLeft: 0 };

  return { allowed: true, reason: 'within_allowance', minutesLeft: minutesBalance };
}
