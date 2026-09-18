import type { JSX } from 'react';
import { useState, type FormEvent } from 'react';
import { Link } from 'react-router-dom';
import { api, type Me } from '../api.js';
import { ErrorBox, Loading, useAsync } from '../components/Async.js';

function since(iso: string | null): string {
  if (!iso) return 'ни разу';
  const mins = Math.floor((Date.now() - new Date(iso).getTime()) / 60000);
  if (mins < 2) return 'только что';
  if (mins < 60) return `${mins} мин назад`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours} ч назад`;
  return `${Math.floor(hours / 24)} дн назад`;
}

/** Молчащий агент — повод насторожиться: его могли снять. */
function deviceTag(lastSeenAt: string | null): JSX.Element {
  if (!lastSeenAt) return <span className="tag warn">не выходил на связь</span>;
  const hours = (Date.now() - new Date(lastSeenAt).getTime()) / 3600000;
  if (hours > 24) return <span className="tag bad">молчит {since(lastSeenAt)}</span>;
  return <span className="tag ok">{since(lastSeenAt)}</span>;
}

export function ChildrenPage({ me }: { me: Me }): JSX.Element {
  const { data, error, loading, reload } = useAsync(() => api.children());
  const [name, setName] = useState('');
  const [adding, setAdding] = useState(false);
  const [addError, setAddError] = useState<string | null>(null);

  async function add(e: FormEvent): Promise<void> {
    e.preventDefault();
    setAdding(true);
    setAddError(null);
    try {
      await api.addChild({ name: name.trim() });
      setName('');
      reload();
    } catch (err) {
      setAddError(err instanceof Error ? err.message : String(err));
    } finally {
      setAdding(false);
    }
  }

  return (
    <>
      <h2>Дети</h2>
      <p className="sub">Баланс, устройства и правила каждого ребёнка.</p>
      <ErrorBox message={error} />

      {loading && <Loading />}

      {data?.length === 0 && (
        <div className="card">
          <p className="note" style={{ margin: 0 }}>
            Пока никого. Добавьте ребёнка — ему сразу создастся политика по умолчанию.
          </p>
        </div>
      )}

      {data?.map((c) => (
        <div className="card" key={c.id}>
          <div className="row" style={{ justifyContent: 'space-between' }}>
            <div>
              <h3 style={{ marginBottom: 2 }}>
                <Link to={`/children/${c.id}`}>{c.name}</Link>
              </h3>
              <span className="note">
                {c.devices.length === 0
                  ? 'устройств нет'
                  : c.devices.map((d) => `${d.name} (${d.platform})`).join(', ')}
              </span>
            </div>
            <div className="row">
              <div className="stat">
                <div className="num">{c.balances.minutes}</div>
                <div className="lbl">минут</div>
              </div>
              <div className="stat">
                <div className="num">{c.balances.credits}</div>
                <div className="lbl">кредитов</div>
              </div>
            </div>
          </div>
          {c.devices.length > 0 && (
            <table style={{ marginTop: 12 }}>
              <thead>
                <tr><th>Устройство</th><th>Платформа</th><th>Версия</th><th>Связь</th></tr>
              </thead>
              <tbody>
                {c.devices.map((d) => (
                  <tr key={d.id}>
                    <td>{d.name}</td>
                    <td>{d.platform}</td>
                    <td className="note">{d.agentVersion ?? '—'}</td>
                    <td>{deviceTag(d.lastSeenAt)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      ))}

      {me.role !== 'viewer' && (
        <form className="card" onSubmit={(e) => void add(e)}>
          <h3>Добавить ребёнка</h3>
          <ErrorBox message={addError} />
          <div className="row">
            <div className="field">
              <label htmlFor="childName">Имя</label>
              <input id="childName" value={name} required maxLength={60}
                     onChange={(e) => setName(e.target.value)} />
            </div>
            <button type="submit" disabled={adding || !name.trim()}>Добавить</button>
          </div>
        </form>
      )}
    </>
  );
}
