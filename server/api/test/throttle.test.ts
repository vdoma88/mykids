import { afterAll, beforeEach, describe, expect, it } from 'vitest';
import type { FastifyInstance } from 'fastify';
import { buildApp } from '../src/app.js';
import { LoginThrottle, defaultThrottle } from '../src/services/throttle.js';
import { prisma, resetDb } from './helpers.js';

/**
 * Перебор пароля родителя.
 *
 * Сервер стоит дома, и в той же сети живёт подросток. До этих тестов вход
 * отвечал на неверный пароль сколько угодно раз подряд.
 */
describe('LoginThrottle', () => {
  const make = (): { t: LoginThrottle; tick: (ms: number) => void } => {
    let now = 1_000_000;
    return { t: new LoginThrottle(defaultThrottle, () => now), tick: (ms) => { now += ms; } };
  };

  it('опечатки прощаются, перебор — нет', () => {
    const { t } = make();
    for (let i = 0; i < defaultThrottle.freeAttempts; i++) {
      expect(t.check(['a']).ok, `попытка ${i + 1}`).toBe(true);
      t.fail(['a']);
    }
    expect(t.check(['a']).ok).toBe(true);
    t.fail(['a']);
    const v = t.check(['a']);
    expect(v.ok).toBe(false);
    if (!v.ok) expect(v.retryAfterSec).toBe(defaultThrottle.baseLockMs / 1000);
  });

  it('пауза удваивается и упирается в потолок', () => {
    const { t, tick } = make();
    const waits: number[] = [];
    for (let i = 0; i < defaultThrottle.freeAttempts + 8; i++) {
      t.fail(['a']);
      const v = t.check(['a']);
      if (!v.ok) { waits.push(v.retryAfterSec); tick(v.retryAfterSec * 1000); }
    }
    expect(waits.slice(0, 3)).toEqual([30, 60, 120]);
    expect(Math.max(...waits)).toBe(defaultThrottle.maxLockMs / 1000);
  });

  it('пауза кончается сама', () => {
    const { t, tick } = make();
    for (let i = 0; i <= defaultThrottle.freeAttempts; i++) t.fail(['a']);
    expect(t.check(['a']).ok).toBe(false);
    tick(defaultThrottle.baseLockMs);
    expect(t.check(['a']).ok).toBe(true);
  });

  it('удачный вход обнуляет счёт', () => {
    const { t } = make();
    for (let i = 0; i < defaultThrottle.freeAttempts; i++) t.fail(['a']);
    t.succeed(['a']);
    t.fail(['a']);
    expect(t.check(['a']).ok).toBe(true);
  });

  it('запертый ключ запирает и проверку с другими ключами', () => {
    // Перебор одного адреса с разных устройств: адрес заперт, сеть нет.
    const { t } = make();
    for (let i = 0; i <= defaultThrottle.freeAttempts; i++) t.fail(['email:a', 'ip:1']);
    expect(t.check(['email:a', 'ip:2']).ok).toBe(false);
    expect(t.check(['email:b', 'ip:2']).ok).toBe(true);
  });
});

describe('/auth/login', () => {
  let now = 5_000_000;
  const app: FastifyInstance = buildApp(prisma, {
    loginThrottle: new LoginThrottle(defaultThrottle, () => now),
  });
  const password = 'очень-длинный-пароль';

  beforeEach(async () => {
    await resetDb();
    now += 24 * 3600_000; // свежие часы — свежие счётчики
  });
  afterAll(async () => { await app.close(); await prisma.$disconnect(); });

  async function register(email: string): Promise<void> {
    const res = await app.inject({
      method: 'POST', url: '/auth/register', payload: { familyName: 'Семья', email, password },
    });
    expect(res.statusCode, res.body).toBe(201);
  }
  const login = (email: string, pass: string) =>
    app.inject({ method: 'POST', url: '/auth/login', payload: { email, password: pass } });

  it('после серии неверных паролей не пускает даже с верным', async () => {
    await register('parent@example.com');
    for (let i = 0; i <= defaultThrottle.freeAttempts; i++) {
      expect((await login('parent@example.com', `не-тот-${i}`)).statusCode).toBe(401);
    }
    const locked = await login('parent@example.com', password);
    expect(locked.statusCode).toBe(429);
    expect(locked.json()).toMatchObject({ error: 'too_many_attempts' });
    expect(Number(locked.headers['retry-after'])).toBeGreaterThan(0);

    now += defaultThrottle.baseLockMs;
    expect((await login('parent@example.com', password)).statusCode).toBe(200);
  });

  it('адрес с заглавной находит тот же аккаунт', async () => {
    // Телефонная клавиатура начинает адрес с заглавной сама.
    await register('parent@example.com');
    expect((await login('Parent@Example.com', password)).statusCode).toBe(200);
    // И пробел, который дописывает автодополнение, — тоже не повод отказать.
    expect((await login('  parent@example.com ', password)).statusCode).toBe(200);
  });

  it('тот же адрес другим регистром второй раз не заводится', async () => {
    await register('parent@example.com');
    const res = await app.inject({
      method: 'POST', url: '/auth/register',
      payload: { familyName: 'Другая', email: 'PARENT@example.com', password },
    });
    expect(res.json()).toMatchObject({ error: 'email_taken' });
  });
});
