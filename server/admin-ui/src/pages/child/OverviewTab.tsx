import type { JSX } from 'react';
import { useState, type FormEvent } from 'react';
import { Link } from 'react-router-dom';
import { api, type ChildDetail, type TamperEvent } from '../../api.js';
import { credits, dateTime, minutes, num, plural } from '../../lib/format.js';
import { TAMPER_KINDS } from '../../lib/labels.js';
import { ErrorBox, errorText, useAsync, type AsyncState } from '../../ui/Async.js';
import { Icon } from '../../ui/Icon.js';
import { Alert, Badge, Card, Field, NumberInput, Stat, Tabs } from '../../ui/kit.js';
import { useToast } from '../../ui/Toast.js';
import { deviceHealth } from '../Children.js';
import { LedgerTable } from './LedgerTable.js';

/** Задержка доставки, которую стоит показать отдельной строкой. */
function lateDelivery(occurredAt: string, recordedAt: string): boolean {
  return new Date(recordedAt).getTime() - new Date(occurredAt).getTime() > 5 * 60 * 1000;
}

/**
 * События вмешательства.
 *
 * Отдельно от журнала операций: там движение минут и кредитов, а здесь факты,
 * которые сами по себе ничего не списывают. Решение — наказывать или нет —
 * остаётся за родителем, поэтому единственное действие тут «разобрал».
 */
function TamperLog({ childId, query, readOnly }: {
  childId: string;
  query: AsyncState<{ pending: number; events: TamperEvent[] }>;
  readOnly: boolean;
}): JSX.Element | null {
  const [busy, setBusy] = useState(false);
  const toast = useToast();

  // Пустой список не показываем: у большинства семей он будет пустым всегда,
  // и постоянный «0 событий» приучил бы не замечать этот блок.
  if (!query.data || query.data.events.length === 0) return null;
  const { pending, events } = query.data;

  return (
    <Card
      title={<span className="row" style={{ ['--gap' as string]: '8px 10px' }}>
        <Icon name="shield" />Вмешательство в агент
        {pending > 0 && <Badge tone="danger">{pending} {plural(pending, 'новое', 'новых', 'новых')}</Badge>}
      </span>}
      desc="Само событие минут не списывает. Исключение — нештатная остановка: агент оплачивает пропущенное время сам, и в описании сказано сколько. Если это был сбой питания, а не попытка схитрить, верните время ручной корректировкой."
      actions={!readOnly && pending > 0 && (
        <button type="button" className="btn secondary sm" disabled={busy} onClick={() => {
          setBusy(true);
          void api.reviewTampers(childId, events.filter((e) => !e.reviewedAt).map((e) => e.id))
            .then(() => { query.reload(); toast.success('События отмечены разобранными.'); })
            .catch((e: unknown) => toast.error(errorText(e)))
            .finally(() => setBusy(false));
        }}><Icon name="check" size={16} />Отметить разобранными</button>
      )}
    >
      <div className="list">
        {events.map((e) => (
          <div key={e.id} className="list-item top" style={{ opacity: e.reviewedAt ? 0.55 : 1 }}>
            <span className={`status-dot ${e.reviewedAt ? 'off' : ''}`} aria-hidden />
            <div className="grow">
              <div className="title">{TAMPER_KINDS[e.kind] ?? e.kind}</div>
              {e.detail && <div className="meta">{e.detail}</div>}
              <div className="meta">
                {e.device?.name ?? 'устройство удалено'} · {dateTime(e.occurredAt ?? e.recordedAt)}
                {/* Сообщение могло пролежать в офлайне: тогда момент доставки
                    отличается от момента события, и это стоит показать. */}
                {e.occurredAt && lateDelivery(e.occurredAt, e.recordedAt) && ` · дошло ${dateTime(e.recordedAt)}`}
                {e.reviewedAt && ' · разобрано'}
              </div>
            </div>
          </div>
        ))}
      </div>
    </Card>
  );
}

/** Ручная корректировка. Причина обязательна: журнал должен объяснять себя сам. */
function Adjust({ child, onDone }: { child: ChildDetail; onDone: () => void }): JSX.Element {
  const [currency, setCurrency] = useState<'credits' | 'minutes'>('credits');
  const [amount, setAmount] = useState(10);
  const [note, setNote] = useState('');
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const toast = useToast();

  async function submit(e: FormEvent): Promise<void> {
    e.preventDefault();
    setErr(null);
    setBusy(true);
    try {
      await api.adjust(child.id, { currency, amount, note: note.trim() });
      toast.success(`${amount > 0 ? 'Начислено' : 'Списано'}: ${currency === 'credits' ? credits(Math.abs(amount)) : minutes(Math.abs(amount))}.`);
      setNote('');
      onDone();
    } catch (e2) {
      setErr(errorText(e2));
    } finally {
      setBusy(false);
    }
  }

  const what = currency === 'credits' ? credits(Math.abs(amount)) : minutes(Math.abs(amount));
  return (
    <Card title="Ручная корректировка" desc="Для того, что системе не видно: помощь по дому, штраф, возврат времени после сбоя.">
      <form className="stack" onSubmit={(e) => void submit(e)}>
        <ErrorBox message={err} />
        <Tabs<'credits' | 'minutes'>
          label="Что корректируем"
          segmented
          value={currency}
          onChange={setCurrency}
          tabs={[{ key: 'credits', label: 'Кредиты', icon: 'coins' }, { key: 'minutes', label: 'Минуты', icon: 'clock' }]}
        />
        <div className="grid two">
          <Field label="Сколько" htmlFor="adj-amt" hint="Со знаком минус — списать.">
            <NumberInput id="adj-amt" value={amount} onChange={setAmount} min={-10000} max={10000} />
          </Field>
          <Field label="Причина" htmlFor="adj-note" hint="Обязательна: через полгода журнал должен объяснять себя сам.">
            <input id="adj-note" className="input" value={note} required minLength={3} maxLength={200}
                   placeholder="за помощь по дому" onChange={(e) => setNote(e.target.value)} />
          </Field>
        </div>
        <div className="row between">
          <span className="small muted">
            {amount === 0 ? 'Ноль ничего не изменит.' : `${child.name}: ${amount > 0 ? '+' : '−'}${what}`}
          </span>
          <button type="submit" className="btn" disabled={busy || amount === 0 || note.trim().length < 3}>
            Записать
          </button>
        </div>
      </form>
    </Card>
  );
}

export function OverviewTab({ child, readOnly, tamperQ, onChanged }: {
  child: ChildDetail;
  readOnly: boolean;
  tamperQ: AsyncState<{ pending: number; events: TamperEvent[] }>;
  onChanged: () => void;
}): JSX.Element {
  const ledgerQ = useAsync(() => api.ledger(child.id, 6), [child.id]);
  // «Все на связи» — только если каждое устройство выходило на связь недавно.
  // Ни разу не выходившее — тоже повод проверить, а не «всё в порядке».
  const silent = child.devices.filter((d) => deviceHealth(d).tone !== 'success');

  return (
    <>
      <div className="stats">
        <Stat label="Минут на счету" icon="clock" value={num(child.balances.minutes)} />
        <Stat label="Кредитов" icon="coins" value={num(child.balances.credits)} />
        <Stat label="Устройств" icon="monitor" value={child.devices.length}
              note={child.devices.length === 0 ? 'агент не установлен' : silent.length ? `без связи: ${silent.length}` : 'все на связи'} />
      </div>

      {child.devices.length === 0 && (
        <Alert tone="warning">
          Устройств пока нет — время на компьютере ребёнка не считается.{' '}
          <Link to={`/children/${child.id}/devices`}>Привязать устройство</Link>
        </Alert>
      )}

      <TamperLog childId={child.id} query={tamperQ} readOnly={readOnly} />

      {!readOnly && <Adjust child={child} onDone={() => { onChanged(); ledgerQ.reload(); }} />}

      <Card
        title="Последние операции"
        actions={<Link to={`/children/${child.id}/ledger`} className="btn ghost sm">Весь журнал<Icon name="arrowRight" size={16} /></Link>}
      >
        <ErrorBox message={ledgerQ.error} />
        {ledgerQ.data && ledgerQ.data.entries.length === 0 && <p className="muted small">Пока пусто.</p>}
        {ledgerQ.data && ledgerQ.data.entries.length > 0 && <LedgerTable entries={ledgerQ.data.entries} />}
      </Card>
    </>
  );
}
