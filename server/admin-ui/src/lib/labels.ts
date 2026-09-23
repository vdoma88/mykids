import type { TimeWindow } from '@mykids/contracts';

/** Дни недели в порядке Date.getDay(): индекс 0 — воскресенье. */
export const DAYS_SHORT = ['вс', 'пн', 'вт', 'ср', 'чт', 'пт', 'сб'];
/** Порядок показа — с понедельника, как в любом русском календаре. */
export const WEEK_ORDER = [1, 2, 3, 4, 5, 6, 0];

export const REASONS: Record<string, string> = {
  daily_grant: 'Дневная выдача',
  carry_over: 'Перенос со вчера',
  task_reward: 'За задание',
  conversion_spend: 'Обмен: списание кредитов',
  conversion_gain: 'Обмен: начисление минут',
  purchase: 'Покупка',
  purchase_grant: 'Эффект покупки',
  purchase_refund: 'Возврат за отклонённую покупку',
  screen_usage: 'Использование экрана',
  manual_adjust: 'Корректировка родителя',
  tamper_penalty: 'Вмешательство в агент',
  expiry: 'Сгорело',
};

export const TAMPER_KINDS: Record<string, string> = {
  clock: 'Переведены системные часы',
  unclean_stop: 'Агент остановлен нештатно',
  permissions: 'Отозваны разрешения агента',
  helper_lied: 'Наблюдатель на компьютере сообщает неправду',
  helper_killed: 'Наблюдатель на компьютере снимают раз за разом',
};

export const PLATFORMS: Record<string, string> = { windows: 'Windows', android: 'Android', web: 'Браузер' };

export const ROLES: Record<string, string> = { owner: 'Владелец', parent: 'Родитель', viewer: 'Только чтение' };

export const MODES: Record<TimeWindow['mode'], string> = {
  blocked: 'Экран закрыт',
  tasks_only: 'Только задания',
  allowed: 'Экран открыт',
};

export const SUBJECTS: Record<string, string> = {
  psychology: 'Психология', math: 'Математика', physics: 'Физика', chemistry: 'Химия',
  biology: 'Биология', geography: 'География', history: 'История', language: 'Язык',
  literature: 'Литература', cs: 'Информатика', logic: 'Логика', finance: 'Финансы', other: 'Другое',
};

/** «пн–пт», «каждый день», «сб, вс». */
export function describeDays(days: readonly number[]): string {
  const set = new Set(days);
  if (set.size === 7) return 'каждый день';
  const weekdays = [1, 2, 3, 4, 5];
  if (set.size === 5 && weekdays.every((d) => set.has(d))) return 'пн–пт';
  if (set.size === 2 && set.has(6) && set.has(0)) return 'выходные';
  return WEEK_ORDER.filter((d) => set.has(d)).map((d) => DAYS_SHORT[d]).join(', ');
}

/** Эффект товара словами. */
export function describeEffect(effect: unknown): string {
  const e = effect as { kind?: string; minutes?: number; note?: string };
  if (e.kind === 'grant_minutes' && typeof e.minutes === 'number') return `+${e.minutes} мин экрана`;
  return e.note ?? 'Награда вне экрана';
}
