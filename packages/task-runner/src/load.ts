import { packManifestSchema, taskItemSchema, type TaskItem, type TaskPack } from '@mykids/contracts';

export interface PackSummary {
  id: string; title: string; subject: string; version: string;
  itemCount: number; description?: string;
}

/** Читает пакеты по HTTP. Схемы применяются на клиенте: битый пакет должен падать явно. */
export async function listPacks(baseUrl: string): Promise<PackSummary[]> {
  const res = await fetch(`${baseUrl}/index.json`);
  if (!res.ok) throw new Error(`не удалось прочитать index.json: ${res.status}`);
  const data = (await res.json()) as { packs: PackSummary[] };
  return data.packs;
}

export async function loadPack(baseUrl: string, packId: string): Promise<TaskPack> {
  const base = `${baseUrl}/${packId}`;
  const manifestRes = await fetch(`${base}/pack.json`);
  if (!manifestRes.ok) throw new Error(`пакет ${packId} не найден: ${manifestRes.status}`);
  const manifest = packManifestSchema.parse(await manifestRes.json());

  const items: TaskItem[] = [];
  for (const rel of manifest.items) {
    const res = await fetch(`${base}/${rel}`);
    if (!res.ok) throw new Error(`задание ${rel} не найдено: ${res.status}`);
    items.push(taskItemSchema.parse(await res.json()));
  }
  return { manifest, items };
}
