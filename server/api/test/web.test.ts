import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterAll, describe, expect, it } from 'vitest';
import type { FastifyInstance } from 'fastify';
import { buildApp } from '../src/app.js';
import { prisma } from './helpers.js';

/**
 * Интерфейс и API живут по одному адресу. Проверяется здесь не React, а
 * разделение: где кончается страница и начинается API. Ошибиться тут легко
 * и дорого — сервер, отвечающий страницей на /agent/sync, выглядит рабочим,
 * а агент при этом молча теряет связь с семьёй.
 */
const webRoot = mkdtempSync(join(tmpdir(), 'mykids-web-'));
writeFileSync(join(webRoot, 'index.html'), '<!doctype html><title>MyKids</title>');
mkdirSync(join(webRoot, 'assets'));
writeFileSync(join(webRoot, 'assets', 'app.js'), 'export const ok = 1;\n');

const app: FastifyInstance = buildApp(prisma, { webRoot });
const page = { accept: 'text/html,application/xhtml+xml' };

afterAll(async () => {
  await app.close();
  await prisma.$disconnect();
  rmSync(webRoot, { recursive: true, force: true });
});

describe('интерфейс с того же адреса, что и API', () => {
  it('корень отдаёт страницу', async () => {
    const res = await app.inject({ method: 'GET', url: '/', headers: page });
    expect(res.statusCode).toBe(200);
    expect(res.body).toContain('MyKids');
  });

  it('файлы сборки отдаются', async () => {
    const res = await app.inject({ method: 'GET', url: '/assets/app.js' });
    expect(res.statusCode).toBe(200);
    expect(res.body).toContain('export const ok');
  });

  it('ссылка вглубь интерфейса открывается напрямую', async () => {
    // Роутинг у React свой: /children/42 сервер видит впервые, но это
    // ссылка, которую родитель мог сохранить в закладки.
    for (const url of ['/children', '/children/42', '/store', '/child']) {
      const res = await app.inject({ method: 'GET', url, headers: page });
      expect(res.statusCode, url).toBe(200);
      expect(res.body, url).toContain('MyKids');
    }
  });

  it('API остаётся API', async () => {
    // Здоровье — не страница, иначе мониторинг «видит» живой сервер всегда.
    const health = await app.inject({ method: 'GET', url: '/health', headers: page });
    expect(health.json()).toEqual({ ok: true });

    // Без токена — 401, а не страница входа: агент разбирает код ответа.
    const child = await app.inject({ method: 'GET', url: '/child/me', headers: page });
    expect(child.statusCode).toBe(401);
    expect(child.json()).toMatchObject({ error: 'unauthorized' });
  });

  it('опечатка в адресе API остаётся ошибкой', async () => {
    // Ответить страницей на /agent/sinc значило бы спрятать опечатку от
    // того, кто её сделал: агент получил бы 200 и HTML вместо разбора.
    // Какая именно ошибка — не важно: под закрытым префиксом честнее
    // ответить «не пущу», не рассказывая, какие пути там есть.
    for (const url of ['/agent/sinc', '/admin/детей', '/auth/logn']) {
      const res = await app.inject({ method: 'GET', url, headers: page });
      expect(res.statusCode, url).toBeGreaterThanOrEqual(400);
      expect(res.body, url).not.toContain('<!doctype');
      expect(res.json(), url).toHaveProperty('error');
    }
  });

  it('список детей — страница родителя, а не запрос ребёнка', async () => {
    // «/children» начинается с «/child», и проверка без границы пути
    // отдавала родителю 401 на его собственном списке детей.
    const res = await app.inject({ method: 'GET', url: '/children', headers: page });
    expect(res.statusCode).toBe(200);
  });

  it('запрос JSON не получает страницу', async () => {
    const res = await app.inject({
      method: 'GET', url: '/children', headers: { accept: 'application/json' },
    });
    expect(res.statusCode).toBe(404);
  });
});

describe('без собранного интерфейса', () => {
  it('сервер поднимается и отдаёт API', async () => {
    // Разработчик запускает API без сборки фронта постоянно.
    const bare = buildApp(prisma, { webRoot: join(webRoot, 'нет-такого') });
    expect((await bare.inject({ method: 'GET', url: '/health' })).json()).toEqual({ ok: true });
    await bare.close();
  });
});
