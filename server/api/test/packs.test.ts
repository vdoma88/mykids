import { fileURLToPath } from 'node:url';
import { afterAll, beforeEach, describe, expect, it } from 'vitest';
import type { FastifyInstance } from 'fastify';
import { buildApp } from '../src/app.js';
import { prisma, resetDb } from './helpers.js';

/**
 * Назначение пакетов — то, без чего вся экономика мертва: ребёнок видит
 * баланс и курс обмена, но заработать ему нечем. Записи о назначении в базе
 * были, создавать их было некому.
 *
 * Каталог берём настоящий, а не выдуманный: проверять назначение против
 * пакетов, которых нет в репозитории, значит не проверять ничего.
 */
const contentRoot = fileURLToPath(new URL('../../../content/packs', import.meta.url));
const app: FastifyInstance = buildApp(prisma, { contentRoot });

beforeEach(resetDb);
afterAll(async () => { await app.close(); await prisma.$disconnect(); });

const creds = { familyName: 'Семья', email: 'p@example.com', password: 'очень-длинный-пароль' };

async function withChild(): Promise<{
  auth: Record<string, string>; device: Record<string, string>; childId: string;
}> {
  const reg = await app.inject({ method: 'POST', url: '/auth/register', payload: creds });
  const auth = { authorization: `Bearer ${(reg.json() as { token: string }).token}` };

  const child = await app.inject({
    method: 'POST', url: '/admin/children', headers: auth, payload: { name: 'Марк' },
  });
  const childId = (child.json() as { id: string }).id;

  const dev = await app.inject({
    method: 'POST', url: `/admin/children/${childId}/devices`, headers: auth,
    payload: { platform: 'windows', name: 'ПК' },
  });
  const device = { authorization: `Bearer ${(dev.json() as { token: string }).token}` };
  return { auth, device, childId };
}

describe('пакеты заданий', () => {
  it('каталог виден родителю и берётся с диска', async () => {
    const { auth, childId } = await withChild();
    const res = await app.inject({
      method: 'GET', url: `/admin/children/${childId}/packs`, headers: auth,
    });
    const body = res.json() as { assigned: string[]; catalog: { id: string; title: string }[] };

    expect(body.assigned).toEqual([]);
    expect(body.catalog.length).toBeGreaterThan(0);
    // Названия, а не идентификаторы: родитель выбирает глазами.
    expect(body.catalog[0]?.title).toBeTruthy();
  });

  it('назначенное доходит до ребёнка', async () => {
    const { auth, device, childId } = await withChild();
    const catalog = (await app.inject({
      method: 'GET', url: `/admin/children/${childId}/packs`, headers: auth,
    })).json() as { catalog: { id: string }[] };
    const pick = catalog.catalog.slice(0, 2).map((p) => p.id);

    await app.inject({
      method: 'PUT', url: `/admin/children/${childId}/packs`, headers: auth,
      payload: { packs: pick },
    });

    const mine = await app.inject({ method: 'GET', url: '/child/packs', headers: device });
    expect((mine.json() as { packs: string[] }).packs.sort()).toEqual([...pick].sort());
  });

  it('снятый пакет пропадает у ребёнка', async () => {
    const { auth, device, childId } = await withChild();
    const catalog = (await app.inject({
      method: 'GET', url: `/admin/children/${childId}/packs`, headers: auth,
    })).json() as { catalog: { id: string }[] };
    const [first, second] = catalog.catalog.map((p) => p.id);

    const put = async (packs: string[]): Promise<void> => {
      await app.inject({
        method: 'PUT', url: `/admin/children/${childId}/packs`, headers: auth, payload: { packs },
      });
    };
    await put([first!, second!]);
    await put([first!]);

    const mine = await app.inject({ method: 'GET', url: '/child/packs', headers: device });
    expect((mine.json() as { packs: string[] }).packs).toEqual([first]);
  });

  it('несуществующий пакет назначить нельзя', async () => {
    // Иначе ребёнок увидит пакет, который не загрузится, и решит,
    // что сломалось у него.
    const { auth, childId } = await withChild();
    const res = await app.inject({
      method: 'PUT', url: `/admin/children/${childId}/packs`, headers: auth,
      payload: { packs: ['ru.mykids.нет.такого'] },
    });
    expect(res.statusCode).toBe(400);
    expect(res.json()).toMatchObject({ error: 'unknown_pack' });
  });

  it('чужому ребёнку пакеты не назначить', async () => {
    const { childId } = await withChild();
    const other = await app.inject({
      method: 'POST', url: '/auth/register',
      payload: { ...creds, email: 'neighbour@example.com', familyName: 'Соседи' },
    });
    expect(other.statusCode, JSON.stringify(other.json())).toBe(201);
    const alien = { authorization: `Bearer ${(other.json() as { token: string }).token}` };

    const res = await app.inject({
      method: 'PUT', url: `/admin/children/${childId}/packs`, headers: alien,
      payload: { packs: [] },
    });
    expect(res.statusCode).toBe(404);
  });

  it('содержимое пакета отдаётся целиком', async () => {
    // Раннер в браузере ребёнка читает pack.json и файлы заданий по одному;
    // если сервер не отдаёт их, решать нечего.
    const catalog = (await app.inject({ method: 'GET', url: '/content/packs/index.json' })).json() as {
      packs: { id: string }[];
    };
    const id = catalog.packs[0]!.id;

    const manifest = await app.inject({ method: 'GET', url: `/content/packs/${id}/pack.json` });
    expect(manifest.statusCode).toBe(200);
    const items = (manifest.json() as { items: string[] }).items;
    expect(items.length).toBeGreaterThan(0);

    for (const rel of items) {
      const item = await app.inject({ method: 'GET', url: `/content/packs/${id}/${rel}` });
      expect(item.statusCode, rel).toBe(200);
      expect(item.json()).toHaveProperty('type');
    }
  });

  it('за пределы каталога заданий выйти нельзя', async () => {
    for (const url of [
      '/content/packs/../../server/api/prisma/schema.prisma',
      '/content/packs/%2e%2e%2f%2e%2e%2fpackage.json',
    ]) {
      const res = await app.inject({ method: 'GET', url });
      expect(res.statusCode, url).toBeGreaterThanOrEqual(400);
    }
  });
});
