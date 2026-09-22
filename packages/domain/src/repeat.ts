/**
 * Убывающие кредиты за повторы.
 *
 * Cooldown не даёт перерешать задание сегодня, но завтра оно снова приносит
 * полную награду — и послезавтра, и через неделю. Значит, выгоднее всего
 * раз за разом решать одни и те же несколько заданий, которые уже знаешь
 * наизусть. Это и фарм, и худшее, что можно сделать с учёбой: система начинает
 * платить за то, чтобы ничему не учиться.
 *
 * Поэтому второе решение того же задания стоит меньше первого, третье — ещё
 * меньше. Но не ноль и никогда: повторение — это и есть способ запомнить, и
 * платить за него совсем ничего значило бы отучать повторять.
 *
 * Считаются только недавние повторы. Задание, к которому вернулись через два
 * месяца, стоит полной награды: возврат после долгого перерыва — самое
 * полезное повторение из возможных, и наказывать за него было бы ровно
 * наоборот. Это ещё не интервальное повторение, но уже не противоречит ему.
 */

/** Настройки затухания. */
export interface RepeatConfig {
  /**
   * Доли базовой награды за первое, второе, третье решение и так далее.
   * Последняя доля действует и дальше: она же и пол.
   */
  factors: readonly number[];
  /** За какой срок назад считать повторы. */
  windowDays: number;
}

export const defaultRepeatConfig: RepeatConfig = {
  // Половина, потом четверть — и четверть дальше. Четверть, а не ноль:
  // повторение остаётся оплаченным, просто перестаёт быть выгоднее нового.
  factors: [1, 0.5, 0.25],
  windowDays: 30,
};

export interface RepeatInput {
  /** Базовая награда за задание из пакета. */
  credits: number;
  /**
   * Когда это же задание засчитывалось раньше, новейшее первым.
   * По часам сервера: часам устройства здесь верить нельзя.
   */
  awardedAt: readonly Date[];
  now: Date;
  config?: RepeatConfig;
}

export interface RepeatAward {
  credits: number;
  /** Какой это раз за окно: 1 — первый, 2 — второй и так далее. */
  nth: number;
  /** Пусто, если награда полная. Иначе — что сказать ребёнку. */
  note: string;
}

/**
 * Сколько стоит это решение с учётом недавних повторов.
 *
 * Ноль на входе остаётся нулём: за неверный ответ и так ничего не полагается,
 * и объяснять про повторы тут нечего.
 */
export function repeatAward(input: RepeatInput): RepeatAward {
  const config = input.config ?? defaultRepeatConfig;
  const nth = recentAwards(input.awardedAt, input.now, config.windowDays) + 1;

  if (input.credits <= 0 || nth <= 1) {
    return { credits: Math.max(0, input.credits), nth, note: '' };
  }

  const factor = config.factors[Math.min(nth, config.factors.length) - 1] ?? 1;

  // Округляем вверх и не ниже одного кредита: решённое задание, оценённое в
  // ноль, читается как «не засчитано», а оно засчитано.
  const credits = Math.max(1, Math.ceil(input.credits * factor));

  return {
    credits,
    nth,
    note:
      `Это задание ты решаешь ${nthWord(nth)} за месяц — ${credits} ` +
      `${creditWord(credits)} вместо ${input.credits}. ` +
      'Новое задание принесёт полную награду.',
  };
}

/** Сколько раз задание засчитывалось за последние windowDays. */
function recentAwards(awardedAt: readonly Date[], now: Date, windowDays: number): number {
  const from = now.getTime() - windowDays * 24 * 60 * 60 * 1000;
  return awardedAt.filter((at) => at.getTime() >= from && at.getTime() <= now.getTime()).length;
}

function nthWord(n: number): string {
  switch (n) {
    case 2:
      return 'второй раз';
    case 3:
      return 'третий раз';
    case 4:
      return 'четвёртый раз';
    default:
      return `${n}-й раз`;
  }
}

function creditWord(n: number): string {
  const mod100 = n % 100;
  if (mod100 >= 11 && mod100 <= 14) return 'кредитов';
  switch (n % 10) {
    case 1:
      return 'кредит';
    case 2:
    case 3:
    case 4:
      return 'кредита';
    default:
      return 'кредитов';
  }
}
