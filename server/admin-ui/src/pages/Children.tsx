import type { JSX } from 'react';
import { useState, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { api, type ChildSummary, type Device, type Me } from '../api.js';
import { num, since } from '../lib/format.js';
import { ErrorBox, CardSkeleton, errorText, useAsync } from '../ui/Async.js';
import { Icon } from '../ui/Icon.js';
import { Avatar, Badge, Card, Empty, Field, PageHead } from '../ui/kit.js';

/** Молчащий агент — повод насторожиться: его могли снять. */
export function deviceHealth(d: Pick<Device, 'lastSeenAt'>): { tone: 'success' | 'warning' | 'danger'; text: string } {
  if (!d.lastSeenAt) return { tone: 'warning', text: 'не выходил на связь' };
  const hours = (Date.now() - new Date(d.lastSeenAt).getTime()) / 3_600_000;
  if (hours > 24) return { tone: 'danger', text: `молчит · ${since(d.lastSeenAt)}` };
  return { tone: 'success', text: since(d.lastSeenAt) };
}

function ChildCard({ c }: { c: ChildSummary }): JSX.Element {
  const silent = c.devices.filter((d) => deviceHealth(d).tone !== 'success');
  return (
    <Link to={`/children/${c.id}`} className="card interactive stack" style={{ textDecoration: 'none', color: 'inherit' }}>
      <div className="row nowrap">
        <Avatar name={c.name} />
        <div style={{ minWidth: 0, flex: 1 }}>
          <h2 className="truncate">{c.name}</h2>
          <div className="small muted">
            {c.devices.length === 0 ? 'Устройств нет' : `Устройств: ${c.devices.length}`}
          </div>
        </div>
        <Icon name="chevronRight" />
      </div>
      <div className="stats">
        <div className="stat">
          <span className="stat-label"><Icon name="clock" />Минуты</span>
          <span className="stat-value">{num(c.balances.minutes)}</span>
        </div>
        <div className="stat">
          <span className="stat-label"><Icon name="coins" />Кредиты</span>
          <span className="stat-value">{num(c.balances.credits)}</span>
        </div>
      </div>
      {c.devices.length === 0 ? (
        <span><Badge tone="warning" dot>Агент не установлен</Badge></span>
      ) : silent.length === 0 ? (
        <span><Badge tone="success" dot>Все устройства на связи</Badge></span>
      ) : (
        <div className="row" style={{ ['--gap' as string]: '6px' }}>
          {silent.map((d) => {
            const h = deviceHealth(d);
            return <Badge key={d.id} tone={h.tone} dot>{d.name}: {h.text}</Badge>;
          })}
        </div>
      )}
    </Link>
  );
}

export function ChildrenPage({ me }: { me: Me }): JSX.Element {
  const { data, error, loading, reload } = useAsync(() => api.children());
  const navigate = useNavigate();
  const [name, setName] = useState('');
  const [adding, setAdding] = useState(false);
  const [addError, setAddError] = useState<string | null>(null);

  async function add(e: FormEvent): Promise<void> {
    e.preventDefault();
    setAdding(true);
    setAddError(null);
    try {
      const created = await api.addChild({ name: name.trim() });
      setName('');
      reload();
      // Сразу к настройке: у нового ребёнка ещё нет ни устройства, ни пакетов,
      // и без них он ничего не заработает.
      if (data?.length === 0) navigate(`/children/${created.id}`);
    } catch (err) {
      setAddError(errorText(err));
    } finally {
      setAdding(false);
    }
  }

  const canEdit = me.role !== 'viewer';

  return (
    <>
      <PageHead title="Дети" lead="Баланс, устройства и правила каждого ребёнка." />
      <ErrorBox message={error} />

      {loading && <div className="grid cards"><CardSkeleton /><CardSkeleton /></div>}

      {data && data.length === 0 && (
        <Card>
          <Empty icon="users" title="Пока никого">
            Добавьте ребёнка — ему сразу создадутся правила по умолчанию. Потом
            привяжете устройство и назначите задания.
          </Empty>
        </Card>
      )}

      {data && data.length > 0 && (
        <div className="grid cards">{data.map((c) => <ChildCard key={c.id} c={c} />)}</div>
      )}

      {canEdit && (
        <Card title="Добавить ребёнка">
          <form className="row top" onSubmit={(e) => void add(e)}>
            <Field label="Имя" htmlFor="childName" className="grow" error={addError}>
              <input id="childName" className="input" value={name} required maxLength={60}
                     placeholder="Как зовут" onChange={(e) => setName(e.target.value)} />
            </Field>
            <button type="submit" className="btn" style={{ marginTop: 26 }} disabled={adding || !name.trim()}>
              <Icon name="plus" />Добавить
            </button>
          </form>
        </Card>
      )}
    </>
  );
}
