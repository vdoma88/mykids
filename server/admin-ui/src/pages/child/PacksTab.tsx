import type { JSX } from 'react';
import { useState } from 'react';
import { api } from '../../api.js';
import { tasksWord } from '../../lib/format.js';
import { SUBJECTS } from '../../lib/labels.js';
import { CardSkeleton, ErrorBox, errorText, useAsync } from '../../ui/Async.js';
import { Badge, Card, Empty } from '../../ui/kit.js';
import { useToast } from '../../ui/Toast.js';

/**
 * Пакеты заданий ребёнка. Без них остальная страница бессмысленна: ребёнок
 * видит баланс и курс обмена, но заработать ему нечем.
 */
export function PacksTab({ childId, readOnly }: { childId: string; readOnly: boolean }): JSX.Element {
  const packsQ = useAsync(() => api.packs(childId), [childId]);
  const [chosen, setChosen] = useState<Set<string> | null>(null);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const toast = useToast();

  if (!packsQ.data) return packsQ.error ? <ErrorBox message={packsQ.error} /> : <CardSkeleton lines={6} />;
  const { catalog } = packsQ.data;
  const assigned = chosen ?? new Set(packsQ.data.assigned);
  const dirty = chosen !== null;

  function toggle(id: string): void {
    const next = new Set(assigned);
    if (next.has(id)) next.delete(id); else next.add(id);
    setChosen(next);
    setSaved(null);
  }

  async function save(): Promise<void> {
    setErr(null);
    setSaving(true);
    try {
      const res = await api.savePacks(childId, [...assigned]);
      const msg = res.assigned.length === 0
        ? 'Пакеты сняты: зарабатывать кредиты ребёнку пока нечем.'
        : `Назначено пакетов: ${res.assigned.length}.`;
      setSaved(msg);
      setChosen(null);
      packsQ.reload();
      toast.success(msg);
    } catch (e) {
      setErr(errorText(e));
    } finally {
      setSaving(false);
    }
  }

  // По предметам: четырнадцать пакетов сплошным списком выбирать трудно.
  const bySubject = new Map<string, typeof catalog>();
  for (const p of catalog) bySubject.set(p.subject, [...(bySubject.get(p.subject) ?? []), p]);

  return (
    <Card
      title="Пакеты заданий"
      desc={`Назначено ${assigned.size} из ${catalog.length}. Ребёнок видит только назначенные и зарабатывает только на них.`}
    >
      <ErrorBox message={err} />
      {catalog.length === 0 && (
        <Empty icon="book" title="Каталог пуст">Сервер не видит пакетов заданий в своём каталоге.</Empty>
      )}
      <div className="stack">
        {[...bySubject.entries()].map(([subject, packs]) => (
          <div key={subject} className="stack" style={{ ['--gap' as string]: '8px' }}>
            <span className="section-title">{SUBJECTS[subject] ?? subject}</span>
            {packs.map((p) => (
              <label className="pack-option" key={p.id}>
                <input type="checkbox" data-testid={`pack-${p.id}`} disabled={readOnly}
                       checked={assigned.has(p.id)} onChange={() => toggle(p.id)} />
                <span style={{ minWidth: 0 }}>
                  <span className="t">{p.title}</span>{' '}
                  <Badge>{p.itemCount} {tasksWord(p.itemCount)}</Badge>
                  {p.description && <span className="d" style={{ display: 'block' }}>{p.description}</span>}
                </span>
              </label>
            ))}
          </div>
        ))}
      </div>
      {!readOnly && catalog.length > 0 && (
        <div className="card-foot">
          {saved && !dirty && <span className="small muted grow" data-testid="packs-saved">{saved}</span>}
          {dirty && <span className="small muted grow">Есть несохранённые изменения</span>}
          {dirty && <button type="button" className="btn ghost" onClick={() => setChosen(null)}>Отменить</button>}
          <button type="button" className="btn" disabled={!dirty || saving} onClick={() => void save()} data-testid="save-packs">
            {saving ? 'Сохраняю…' : 'Сохранить пакеты'}
          </button>
        </div>
      )}
    </Card>
  );
}
