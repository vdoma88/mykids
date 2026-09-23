/**
 * Числа и даты по-русски. Одно место на весь интерфейс: раньше «минут» и
 * «кредитов» писались через строку в каждом компоненте, и «1 минут» или
 * «2 кредита за минуту» на курсе 1 проскакивали то тут, то там.
 */

/** Форма слова для числа: 1 минута, 2 минуты, 5 минут. */
export function plural(n: number, one: string, few: string, many: string): string {
  const abs = Math.abs(n) % 100;
  const last = abs % 10;
  if (abs > 10 && abs < 20) return many;
  if (last === 1) return one;
  if (last >= 2 && last <= 4) return few;
  return many;
}

const nf = new Intl.NumberFormat('ru-RU');
export const num = (n: number): string => nf.format(n);

export const minutesWord = (n: number): string => plural(n, 'минута', 'минуты', 'минут');
export const creditsWord = (n: number): string => plural(n, 'кредит', 'кредита', 'кредитов');
export const tasksWord = (n: number): string => plural(n, 'задание', 'задания', 'заданий');
export const minutes = (n: number): string => `${num(n)} ${minutesWord(n)}`;
export const credits = (n: number): string => `${num(n)} ${creditsWord(n)}`;

/** «1 ч 30 мин» — для длинных промежутков читается легче, чем «90 минут». */
export function duration(totalMinutes: number): string {
  const m = Math.max(0, Math.round(totalMinutes));
  if (m < 60) return `${m} мин`;
  const h = Math.floor(m / 60);
  const rest = m % 60;
  return rest === 0 ? `${h} ч` : `${h} ч ${rest} мин`;
}

const dt = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
const d = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'long' });
export const dateTime = (iso: string | Date): string => dt.format(new Date(iso));
export const date = (iso: string | Date): string => d.format(new Date(iso));

/** «только что», «12 мин назад», «3 ч назад», «вчера», «4 дн назад». */
export function since(iso: string | null, now = Date.now()): string {
  if (!iso) return 'ни разу';
  const mins = Math.floor((now - new Date(iso).getTime()) / 60000);
  if (mins < 2) return 'только что';
  if (mins < 60) return `${mins} мин назад`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours} ч назад`;
  const days = Math.floor(hours / 24);
  if (days === 1) return 'вчера';
  return `${days} дн назад`;
}

/** Цвет аватара — от имени, чтобы у ребёнка он был всегда один и тот же. */
export function hueFor(name: string): string {
  let h = 0;
  for (const ch of name) h = (h * 31 + ch.codePointAt(0)!) % 360;
  return `hsl(${h} 55% 50%)`;
}

export const initial = (name: string): string => (name.trim()[0] ?? '?').toUpperCase();
