import type { JSX } from 'react';
import { useState } from 'react';
import { api, type Me, type PendingAttempt, type PendingPurchase } from '../api.js';
import { APPROVALS_CHANGED } from '../layout/Shell.js';
import { credits, dateTime, minutes } from '../lib/format.js';
import { describeEffect } from '../lib/labels.js';
import { CardSkeleton, ErrorBox, errorText, useAsync } from '../ui/Async.js';
import { Icon } from '../ui/Icon.js';
import { Alert, Avatar, Card, Empty, PageHead } from '../ui/kit.js';
import { useToast } from '../ui/Toast.js';

function Actions({ busy, onApprove, onReject, readOnly, approveLabel, testId }: {
  busy: boolean; onApprove: () => void; onReject: () => void; readOnly: boolean; approveLabel: string; testId: string;
}): JSX.Element | null {
  if (readOnly) return null;
  return (
    <div className="row nowrap" style={{ ['--gap' as string]: '8px' }}>
      <button type="button" className="btn ghost sm" disabled={busy} onClick={onReject}>Отклонить</button>
      <button type="button" className="btn success sm" disabled={busy} onClick={onApprove} data-testid={testId}>
        <Icon name="check" size={16} />{approveLabel}
      </button>
    </div>
  );
}

export function ApprovalsPage({ me }: { me: Me }): JSX.Element {
  const { data, error, loading, reload } = useAsync(() => api.approvals());
  const [busy, setBusy] = useState<string | null>(null);
  const [note, setNote] = useState<{ tone: 'success' | 'warning'; text: string } | null>(null);
  const toast = useToast();
  const readOnly = me.role === 'viewer';

  async function run(id: string, fn: () => Promise<string | { tone: 'success' | 'warning'; text: string }>): Promise<void> {
    setBusy(id);
    setNote(null);
    try {
      const r = await fn();
      if (typeof r === 'string') toast.success(r); else setNote(r);
    } catch (e) {
      toast.error(errorText(e));
    } finally {
      setBusy(null);
      reload();
      window.dispatchEvent(new Event(APPROVALS_CHANGED));
    }
  }

  const approveAttempt = (a: PendingAttempt): Promise<void> => run(a.id, async () => {
    const res = await api.approveAttempt(a.id);
    // Подтверждение не обходит потолки: если дневной предел уже выбран,
    // задание остаётся в очереди — и сказать об этом надо прямо, иначе
    // родитель нажмёт ещё раз и решит, что кнопка не работает.
    return res.withheldReason
      ? { tone: 'warning', text: `${res.withheldReason} Задание осталось в очереди — подтвердите его завтра.` }
      : { tone: 'success', text: [`Начислено кредитов: ${res.credits}`, res.note].filter(Boolean).join('. ') };
  });

  const purchases = data?.purchases ?? [];
  const attempts = data?.attempts ?? [];
  const total = purchases.length + attempts.length;

  return (
    <>
      <PageHead
        title="Одобрения"
        lead="То, что ждёт вашего решения: задания, которые машиной не проверить, и покупки с одобрением."
      />
      <ErrorBox message={error} />
      {note && (
        <div data-testid="approval-note">
          <Alert tone={note.tone}>{note.text}</Alert>
        </div>
      )}
      {loading && <CardSkeleton lines={3} />}

      {data && total === 0 && (
        <Card><Empty icon="inbox" title="Всё разобрано">Когда ребёнок отправит задание или покупку на одобрение, они появятся здесь.</Empty></Card>
      )}

      {attempts.length > 0 && (
        <Card
          title={`Задания · ${attempts.length}`}
          desc="Кредиты начисляются в момент подтверждения, по цене из пакета и сегодняшним потолкам."
        >
          <div className="list">
            {attempts.map((a) => (
              <div className="list-item" key={a.id} data-testid="pending-attempt">
                <Avatar name={a.child.name} />
                <div className="grow">
                  <div className="title">{a.stem ?? a.itemId}</div>
                  <div className="meta">{a.child.name} · {a.packTitle ?? a.packId} · {dateTime(a.createdAt)}</div>
                </div>
                <Actions
                  readOnly={readOnly}
                  busy={busy !== null}
                  approveLabel="Подтвердить"
                  testId="approve-attempt"
                  onApprove={() => void approveAttempt(a)}
                  onReject={() => void run(a.id, async () => {
                    await api.rejectAttempt(a.id);
                    return 'Задание отклонено.';
                  })}
                />
              </div>
            ))}
          </div>
        </Card>
      )}

      {purchases.length > 0 && (
        <Card
          title={`Покупки · ${purchases.length}`}
          desc="Цена уже списана — иначе те же кредиты можно потратить дважды, пока вы думаете. Отклонив покупку, вы вернёте её целиком."
        >
          <div className="list">
            {purchases.map((p: PendingPurchase) => (
              <div className="list-item" key={p.id} data-testid="pending-purchase">
                <Avatar name={p.child.name} />
                <div className="grow">
                  <div className="title">{p.storeItem.title}</div>
                  <div className="meta">
                    {p.child.name} · {describeEffect(p.storeItem.effect)} · {p.currency === 'credits' ? credits(p.cost) : minutes(p.cost)} · {dateTime(p.createdAt)}
                  </div>
                </div>
                <Actions
                  readOnly={readOnly}
                  busy={busy !== null}
                  approveLabel="Одобрить"
                  testId="approve-purchase"
                  onApprove={() => void run(p.id, async () => {
                    await api.approvePurchase(p.id);
                    return `Покупка «${p.storeItem.title}» одобрена.`;
                  })}
                  onReject={() => void run(p.id, async () => {
                    await api.rejectPurchase(p.id);
                    return `Покупка отклонена, ${p.currency === 'credits' ? credits(p.cost) : minutes(p.cost)} возвращено.`;
                  })}
                />
              </div>
            ))}
          </div>
        </Card>
      )}
    </>
  );
}
