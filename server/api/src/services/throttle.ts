/**
 * Ограничение попыток входа.
 *
 * Сервер стоит дома, и в той же сети живёт тот, от кого защищены правила:
 * подросток. Без ограничения пароль родителя можно перебирать с той скоростью,
 * с какой отвечает argon2, а шестизначный код второго фактора — миллион
 * вариантов, из которых в каждый момент подходят три, — перебирается и того
 * быстрее, уже зная пароль.
 *
 * Первые несколько ошибок бесплатны: опечатку делает каждый. Дальше — пауза,
 * которая удваивается с каждой новой ошибкой до потолка. Считается отдельно
 * по адресу почты и по адресу в сети: первое не даёт перебирать один аккаунт
 * с разных устройств, второе — перебирать много аккаунтов с одного.
 *
 * В памяти, а не в базе: перезапуск сервера сбрасывает счётчики, но
 * перезапустить сервер подросток не может, а запись в базу на каждую попытку
 * сделала бы сам перебор нагрузкой на базу.
 */
export interface ThrottleOptions {
  /** Сколько ошибок подряд прощается без паузы. */
  freeAttempts: number;
  /** Пауза после первой ошибки сверх бесплатных. */
  baseLockMs: number;
  /** Потолок паузы. */
  maxLockMs: number;
  /** Через сколько тишины счётчик забывается. */
  forgetAfterMs: number;
}

export const defaultThrottle: ThrottleOptions = {
  freeAttempts: 5,
  baseLockMs: 30_000,
  maxLockMs: 15 * 60_000,
  forgetAfterMs: 60 * 60_000,
};

interface Entry { failures: number; lockedUntil: number; lastAt: number }

export type ThrottleVerdict = { ok: true } | { ok: false; retryAfterSec: number };

export class LoginThrottle {
  private readonly entries = new Map<string, Entry>();

  constructor(
    private readonly opts: ThrottleOptions = defaultThrottle,
    private readonly now: () => number = Date.now,
  ) {}

  /** Можно ли сейчас пробовать войти под всеми этими ключами. */
  check(keys: readonly string[]): ThrottleVerdict {
    const t = this.now();
    let wait = 0;
    for (const k of keys) {
      const e = this.entry(k, t);
      if (e && e.lockedUntil > t) wait = Math.max(wait, e.lockedUntil - t);
    }
    return wait > 0 ? { ok: false, retryAfterSec: Math.ceil(wait / 1000) } : { ok: true };
  }

  /** Неудачная попытка: считается для каждого ключа. */
  fail(keys: readonly string[]): void {
    const t = this.now();
    for (const k of keys) {
      const e = this.entry(k, t) ?? { failures: 0, lockedUntil: 0, lastAt: t };
      e.failures += 1;
      e.lastAt = t;
      const over = e.failures - this.opts.freeAttempts;
      if (over > 0) {
        const lock = Math.min(this.opts.maxLockMs, this.opts.baseLockMs * 2 ** (over - 1));
        e.lockedUntil = t + lock;
      }
      this.entries.set(k, e);
    }
    this.prune(t);
  }

  /** Удачный вход снимает счётчики: прошлые опечатки больше не в счёт. */
  succeed(keys: readonly string[]): void {
    for (const k of keys) this.entries.delete(k);
  }

  private entry(key: string, t: number): Entry | undefined {
    const e = this.entries.get(key);
    if (e && e.lockedUntil <= t && t - e.lastAt > this.opts.forgetAfterMs) {
      this.entries.delete(key);
      return undefined;
    }
    return e;
  }

  /** Память не растёт без конца: забытые ключи вычищаются. */
  private prune(t: number): void {
    if (this.entries.size < 1000) return;
    for (const [k, e] of this.entries) {
      if (e.lockedUntil <= t && t - e.lastAt > this.opts.forgetAfterMs) this.entries.delete(k);
    }
  }
}
