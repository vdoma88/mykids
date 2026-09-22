import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import type { TaskItem } from '@mykids/contracts';

/**
 * Условия начисления, прочитанные из пакета на диске.
 *
 * Раньше их присылал браузер ребёнка вместе с результатом: и цену задания, и
 * потолок пакета, и cooldown. Пакеты сервер отдаёт сам, с собственного диска —
 * то есть он всё это время мог посмотреть сам, а вместо этого спрашивал.
 */
export interface Terms {
  item: TaskItem;
  /** Кредиты за полностью верный ответ. */
  creditsPerCorrect: number;
  /** Суточный потолок кредитов с этого пакета. */
  dailyCreditCap: number;
  cooldownHours?: number | undefined;
}

/** Пакета или задания нет там, где сервер их держит. */
export class ContentError extends Error {
  constructor(readonly code: string, message: string) {
    super(message);
  }
}

/** Обратная доменная нотация — та же, что в схеме пакета. */
const PACK_ID = /^[a-z0-9]+(\.[a-z0-9-]+)+$/;

/**
 * Файл задания внутри пакета. Путь приходит из манифеста на диске, а не из
 * запроса, но проверяется всё равно: пакет кладёт в каталог человек, и
 * «items/../../../etc/passwd» в манифесте не должен ничего значить.
 */
const ITEM_PATH = /^items\/[A-Za-z0-9_.-]+\.json$/;

interface Manifest {
  reward: { creditsPerCorrect: number; dailyCreditCap: number };
  delivery?: { cooldownHoursPerItem?: number };
  items: string[];
}

/**
 * Пакеты заданий, как их видит сервер.
 *
 * Без кэша — намеренно, по той же причине, что и каталог: пакет, добавленный
 * в каталог, должен работать без перезапуска сервера. Файлов на одну попытку
 * читается столько, сколько заданий в пакете, — это десятки килобайт.
 */
export class ContentService {
  constructor(private readonly root: string | undefined) {}

  /** Есть ли у сервера пакеты вообще. */
  get available(): boolean {
    return this.root !== undefined && existsSync(join(this.root, 'index.json'));
  }

  /**
   * Условия для одного задания.
   *
   * Идентификатор задания в путь не подставляется: он лежит внутри файла, а
   * не в его имени, поэтому нужные файлы перебираются по манифесту. Заодно
   * это значит, что из запроса в путь не попадает ничего.
   */
  terms(packId: string, itemId: string): Terms {
    const manifest = this.manifest(packId);
    for (const rel of manifest.items) {
      if (!ITEM_PATH.test(rel)) continue;
      const item = this.readJson<TaskItem>(join(this.root!, packId, rel));
      if (item === null || item.id !== itemId) continue;
      return {
        item,
        creditsPerCorrect: item.credits ?? manifest.reward.creditsPerCorrect,
        dailyCreditCap: manifest.reward.dailyCreditCap,
        cooldownHours: item.cooldownHours ?? manifest.delivery?.cooldownHoursPerItem,
      };
    }
    throw new ContentError('unknown_item', `В пакете «${packId}» нет задания «${itemId}».`);
  }

  private manifest(packId: string): Manifest {
    if (this.root === undefined) {
      throw new ContentError('no_content', 'Сервер не отдаёт пакеты заданий.');
    }
    if (!PACK_ID.test(packId)) {
      throw new ContentError('unknown_pack', `Пакета «${packId}» нет.`);
    }
    const manifest = this.readJson<Manifest>(join(this.root, packId, 'pack.json'));
    if (manifest === null || !Array.isArray(manifest.items) || !manifest.reward) {
      throw new ContentError('unknown_pack', `Пакета «${packId}» нет.`);
    }
    return manifest;
  }

  private readJson<T>(path: string): T | null {
    if (!existsSync(path)) return null;
    try {
      return JSON.parse(readFileSync(path, 'utf8')) as T;
    } catch {
      // Битый файл — это «задания нет», а не пятисотая: один испорченный
      // пакет не должен ронять начисление за все остальные.
      return null;
    }
  }
}
