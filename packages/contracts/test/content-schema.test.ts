import { describe, expect, it } from 'vitest';
import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import { itemTypeSchema, packManifestSchema, taskItemSchema } from '@mykids/contracts';

/**
 * Определения контента живут в двух местах: zod-схемы здесь (для рантайма и типов)
 * и JSON Schema в docs/content-format (для валидатора пакетов на ajv). Расхождение
 * между ними — тихая поломка: пакет пройдёт один барьер и упадёт на другом.
 * Тесты ниже сверяют оба определения между собой и с реальными пакетами.
 */

const REPO = new URL('../../..', import.meta.url).pathname;
const PACKS_DIR = join(REPO, 'content/packs');
const SCHEMA_DIR = join(REPO, 'docs/content-format');

const readJson = (path: string): unknown => JSON.parse(readFileSync(path, 'utf8'));

const packDirs = readdirSync(PACKS_DIR, { withFileTypes: true })
  .filter((d) => d.isDirectory())
  .map((d) => d.name);

describe('согласованность zod и JSON Schema', () => {
  it('перечень типов заданий совпадает', () => {
    const json = readJson(join(SCHEMA_DIR, 'task-item.schema.json')) as {
      properties: { type: { enum: string[] } };
    };
    expect([...json.properties.type.enum].sort()).toEqual([...itemTypeSchema.options].sort());
  });

  it('перечень предметов совпадает', () => {
    const json = readJson(join(SCHEMA_DIR, 'task-pack.schema.json')) as {
      properties: { subject: { enum: string[] } };
    };
    const zodSubjects = packManifestSchema.shape.subject.options;
    expect([...json.properties.subject.enum].sort()).toEqual([...zodSubjects].sort());
  });
});

describe('пакеты в репозитории', () => {
  it('их вообще нашли', () => {
    expect(packDirs.length).toBeGreaterThan(10);
  });

  it.each(packDirs)('%s: манифест и задания проходят zod-схемы', (dir) => {
    const packDir = join(PACKS_DIR, dir);
    const manifest = packManifestSchema.parse(readJson(join(packDir, 'pack.json')));
    expect(manifest.id).toBe(dir);

    for (const rel of manifest.items) {
      const item = taskItemSchema.parse(readJson(join(packDir, rel)));
      expect(item.id.length).toBeGreaterThan(2);
    }
  });

  it('идентификаторы заданий уникальны по всему репозиторию', () => {
    const seen = new Map<string, string>();
    for (const dir of packDirs) {
      const packDir = join(PACKS_DIR, dir);
      const manifest = packManifestSchema.parse(readJson(join(packDir, 'pack.json')));
      for (const rel of manifest.items) {
        const item = taskItemSchema.parse(readJson(join(packDir, rel)));
        expect(seen.get(item.id) ?? dir).toBe(dir);
        seen.set(item.id, dir);
      }
    }
    expect(seen.size).toBeGreaterThan(80);
  });
});
