import type { JSX } from 'react';
import { useState, type FormEvent } from 'react';
import { api, type Me } from '../api.js';
import { ErrorBox, Loading, useAsync } from '../components/Async.js';

export function StorePage({ me }: { me: Me }): JSX.Element {
  const { data, error, loading, reload } = useAsync(() => api.store());
  const [form, setForm] = useState({
    title: '', costCurrency: 'credits' as 'credits' | 'minutes', costAmount: 60,
    effectKind: 'grant_minutes' as 'grant_minutes' | 'custom',
    minutes: 30, note: '', maxPerDay: 2, requiresApproval: false,
  });
  const [addErr, setAddErr] = useState<string | null>(null);

  async function add(e: FormEvent): Promise<void> {
    e.preventDefault();
    setAddErr(null);
    try {
      await api.addStoreItem({
        title: form.title,
        cost: { currency: form.costCurrency, amount: form.costAmount },
        effect: form.effectKind === 'grant_minutes'
          ? { kind: 'grant_minutes', minutes: form.minutes }
          : { kind: 'custom', note: form.note || form.title },
        maxPerDay: form.maxPerDay,
        requiresApproval: form.requiresApproval,
        enabled: true,
      });
      setForm({ ...form, title: '' });
      reload();
    } catch (err) {
      setAddErr(err instanceof Error ? err.message : String(err));
    }
  }

  return (
    <>
      <h2>Магазин</h2>
      <p className="sub">
        Награды, которые ребёнок покупает за кредиты. Самые мотивирующие у каждой
        семьи свои — заводите собственные.
      </p>
      <ErrorBox message={error} />
      {loading && <Loading />}

      <div className="card">
        {data?.length === 0 && <p className="note">Пока пусто.</p>}
        {data && data.length > 0 && (
          <table>
            <thead>
              <tr><th>Товар</th><th>Цена</th><th>Эффект</th><th>Лимит</th><th>Одобрение</th></tr>
            </thead>
            <tbody>
              {data.map((i) => {
                const effect = i.effect as { kind: string; minutes?: number; note?: string };
                return (
                  <tr key={i.id}>
                    <td>{i.title}</td>
                    <td>{i.costAmount} {i.costCurrency === 'credits' ? 'кр.' : 'мин.'}</td>
                    <td className="note">
                      {effect.kind === 'grant_minutes'
                        ? `+${effect.minutes} минут`
                        : effect.note ?? effect.kind}
                    </td>
                    <td className="note">{i.maxPerDay ? `${i.maxPerDay} в день` : '—'}</td>
                    <td>{i.requiresApproval
                      ? <span className="tag warn">нужно</span>
                      : <span className="tag ok">сразу</span>}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </div>

      {me.role !== 'viewer' && (
        <form className="card" onSubmit={(e) => void add(e)}>
          <h3>Новый товар</h3>
          <ErrorBox message={addErr} />
          <div className="row">
            <div className="field" style={{ flex: 1, minWidth: 200 }}>
              <label htmlFor="st-title">Название</label>
              <input id="st-title" value={form.title} required
                     onChange={(e) => setForm({ ...form, title: e.target.value })} />
            </div>
            <div className="field" style={{ minWidth: 120 }}>
              <label htmlFor="st-cur">Валюта</label>
              <select id="st-cur" value={form.costCurrency}
                      onChange={(e) => setForm({ ...form, costCurrency: e.target.value as 'credits' | 'minutes' })}>
                <option value="credits">кредиты</option>
                <option value="minutes">минуты</option>
              </select>
            </div>
            <div className="field" style={{ minWidth: 100 }}>
              <label htmlFor="st-cost">Цена</label>
              <input id="st-cost" type="number" min={1} value={form.costAmount}
                     onChange={(e) => setForm({ ...form, costAmount: Number(e.target.value) })} />
            </div>
          </div>
          <div className="row" style={{ marginTop: 12 }}>
            <div className="field" style={{ minWidth: 170 }}>
              <label htmlFor="st-eff">Эффект</label>
              <select id="st-eff" value={form.effectKind}
                      onChange={(e) => setForm({ ...form, effectKind: e.target.value as 'grant_minutes' | 'custom' })}>
                <option value="grant_minutes">добавить минуты</option>
                <option value="custom">награда вне экрана</option>
              </select>
            </div>
            {form.effectKind === 'grant_minutes' ? (
              <div className="field" style={{ minWidth: 110 }}>
                <label htmlFor="st-min">Минут</label>
                <input id="st-min" type="number" min={1} value={form.minutes}
                       onChange={(e) => setForm({ ...form, minutes: Number(e.target.value) })} />
              </div>
            ) : (
              <div className="field" style={{ flex: 1, minWidth: 200 }}>
                <label htmlFor="st-note">Что именно</label>
                <input id="st-note" value={form.note} placeholder="поход в кино"
                       onChange={(e) => setForm({ ...form, note: e.target.value })} />
              </div>
            )}
            <div className="field" style={{ minWidth: 120 }}>
              <label htmlFor="st-max">Раз в день</label>
              <input id="st-max" type="number" min={1} value={form.maxPerDay}
                     onChange={(e) => setForm({ ...form, maxPerDay: Number(e.target.value) })} />
            </div>
            <label style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 14 }}>
              <input type="checkbox" style={{ width: 'auto' }} checked={form.requiresApproval}
                     onChange={(e) => setForm({ ...form, requiresApproval: e.target.checked })} />
              нужно одобрение
            </label>
            <button type="submit" disabled={!form.title.trim()}>Добавить</button>
          </div>
        </form>
      )}
    </>
  );
}
