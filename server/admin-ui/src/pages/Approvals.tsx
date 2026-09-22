import type { JSX } from 'react';
import { useState } from 'react';
import { ApiError, api } from '../api.js';
import { ErrorBox, Loading, useAsync } from '../components/Async.js';

interface PendingPurchase {
  id: string; cost: number; currency: string; createdAt: string;
  storeItem: { title: string };
  child: { id: string; name: string };
}

interface PendingAttempt {
  id: string; itemId: string; packId: string; createdAt: string;
  child: { id: string; name: string };
}

export function ApprovalsPage(): JSX.Element {
  const { data, error, loading, reload } = useAsync(() => api.approvals());
  const purchases = (data?.purchases ?? []) as PendingPurchase[];
  const attempts = (data?.attempts ?? []) as PendingAttempt[];
  const [busy, setBusy] = useState<string | null>(null);
  const [note, setNote] = useState<string | null>(null);

  async function decide(id: string, approve: boolean): Promise<void> {
    setBusy(id);
    setNote(null);
    try {
      if (!approve) {
        await api.rejectAttempt(id);
      } else {
        const res = await api.approveAttempt(id);
        // Подтверждение не обходит потолки: если дневной предел уже выбран,
        // задание остаётся в очереди, и сказать об этом надо прямо — иначе
        // родитель нажмёт ещё раз и решит, что кнопка не работает.
        setNote(res.withheldReason ?? [
          `Начислено кредитов: ${res.credits}`, res.note,
        ].filter(Boolean).join('. '));
      }
    } catch (e) {
      setNote(e instanceof ApiError ? e.message : String(e));
    } finally {
      setBusy(null);
      reload();
    }
  }

  return (
    <>
      <h2>Одобрения</h2>
      <p className="sub">
        Покупки и задания, которые ждут вашего подтверждения.
      </p>
      <ErrorBox message={error} />
      {loading && <Loading />}

      <div className="card">
        <h3>Покупки</h3>
        {purchases.length === 0
          ? <p className="note">Очередь пуста.</p>
          : (
            <table>
              <thead><tr><th>Ребёнок</th><th>Товар</th><th>Цена</th><th>Когда</th></tr></thead>
              <tbody>
                {purchases.map((p) => (
                  <tr key={p.id}>
                    <td>{p.child.name}</td>
                    <td>{p.storeItem.title}</td>
                    <td>{p.cost} {p.currency === 'credits' ? 'кр.' : 'мин.'}</td>
                    <td className="note">{new Date(p.createdAt).toLocaleString('ru-RU')}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        <p className="note" style={{ marginTop: 10 }}>
          Цена списывается в момент покупки, даже пока она ждёт одобрения: иначе
          те же кредиты можно потратить дважды.
        </p>
      </div>

      <div className="card">
        <h3>Задания с подтверждением</h3>
        {attempts.length === 0
          ? <p className="note">Очередь пуста.</p>
          : (
            <table>
              <thead>
                <tr><th>Ребёнок</th><th>Задание</th><th>Когда</th><th /></tr>
              </thead>
              <tbody>
                {attempts.map((a) => (
                  <tr key={a.id} data-testid="pending-attempt">
                    <td>{a.child.name}</td>
                    <td className="mono">{a.itemId}</td>
                    <td className="note">{new Date(a.createdAt).toLocaleString('ru-RU')}</td>
                    <td>
                      <button
                        disabled={busy !== null}
                        onClick={() => void decide(a.id, true)}
                        data-testid="approve-attempt"
                      >
                        Подтвердить
                      </button>
                      {' '}
                      <button
                        className="ghost"
                        disabled={busy !== null}
                        onClick={() => void decide(a.id, false)}
                      >
                        Отклонить
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        {note && <p className="note" data-testid="approval-note">{note}</p>}
        <p className="note" style={{ marginTop: 10 }}>
          Это задания, которые проверить машиной нельзя: «прибрался в комнате»,
          «позвонил бабушке». Кредиты за них начисляются в момент подтверждения
          и проходят те же дневные потолки, что и остальные.
        </p>
      </div>
    </>
  );
}
