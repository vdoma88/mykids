import type { JSX } from 'react';
import { useState, type FormEvent } from 'react';
import { api, type Me, type ShopItem } from '../api.js';
import { num } from '../lib/format.js';
import { describeEffect } from '../lib/labels.js';
import { CardSkeleton, ErrorBox, errorText, useAsync } from '../ui/Async.js';
import { Icon } from '../ui/Icon.js';
import { Badge, Card, Empty, Field, NumberInput, PageHead, Switch, Tabs } from '../ui/kit.js';
import { useToast } from '../ui/Toast.js';

function price(i: Pick<ShopItem, 'costAmount' | 'costCurrency'>): string {
  return `${num(i.costAmount)} ${i.costCurrency === 'credits' ? 'кр.' : 'мин'}`;
}

function ItemCard({ item, canEdit, onToggle }: {
  item: ShopItem; canEdit: boolean; onToggle: (enabled: boolean) => void;
}): JSX.Element {
  const e = item.effect as { kind?: string };
  return (
    <div className="card stack" style={{ opacity: item.enabled ? 1 : 0.6, ['--gap' as string]: '12px' }}>
      <div className="row nowrap top">
        <span className="tile-icon accent"><Icon name={e.kind === 'grant_minutes' ? 'clock' : 'gift'} /></span>
        <div className="grow">
          <h2 style={{ overflowWrap: 'anywhere' }}>{item.title}</h2>
          <div className="small muted">{describeEffect(item.effect)}</div>
        </div>
        <span className="badge accent num" style={{ fontSize: '.86rem', height: 28 }}>{price(item)}</span>
      </div>
      <div className="row" style={{ ['--gap' as string]: '6px' }}>
        {item.maxPerDay ? <Badge>{item.maxPerDay} в день</Badge> : <Badge>без лимита в день</Badge>}
        {item.requiresApproval ? <Badge tone="warning">с одобрением</Badge> : <Badge tone="success">сразу</Badge>}
        {!item.enabled && <Badge>выключен</Badge>}
      </div>
      {canEdit && (
        <div className="row between" style={{ borderTop: '1px solid var(--border)', paddingTop: 12 }}>
          <Switch checked={item.enabled} onChange={onToggle} label={item.enabled ? 'Виден ребёнку' : 'Скрыт от ребёнка'} />
        </div>
      )}
    </div>
  );
}

type EffectKind = 'grant_minutes' | 'custom';

function NewItem({ onCreated }: { onCreated: () => void }): JSX.Element {
  const [title, setTitle] = useState('');
  const [currency, setCurrency] = useState<'credits' | 'minutes'>('credits');
  const [cost, setCost] = useState(60);
  const [kind, setKind] = useState<EffectKind>('grant_minutes');
  const [minutes, setMinutes] = useState(30);
  const [note, setNote] = useState('');
  const [maxPerDay, setMaxPerDay] = useState(2);
  const [approval, setApproval] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const toast = useToast();

  async function add(e: FormEvent): Promise<void> {
    e.preventDefault();
    setErr(null);
    setBusy(true);
    try {
      await api.addStoreItem({
        title: title.trim(),
        cost: { currency, amount: cost },
        effect: kind === 'grant_minutes' ? { kind, minutes } : { kind, note: note.trim() || title.trim() },
        maxPerDay,
        requiresApproval: approval,
        enabled: true,
      });
      toast.success(`Товар «${title.trim()}» добавлен.`);
      setTitle('');
      setNote('');
      onCreated();
    } catch (e2) {
      setErr(errorText(e2));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card title="Новый товар" desc="Самые мотивирующие награды у каждой семьи свои: время, поход в кино, выбор ужина.">
      <form className="stack" onSubmit={(e) => void add(e)}>
        <ErrorBox message={err} />
        <Tabs<EffectKind>
          label="Что даёт товар"
          segmented
          value={kind}
          onChange={setKind}
          tabs={[{ key: 'grant_minutes', label: 'Минуты экрана', icon: 'clock' }, { key: 'custom', label: 'Награда вне экрана', icon: 'gift' }]}
        />
        <div className="grid two">
          <Field label="Название" htmlFor="st-title">
            <input id="st-title" className="input" value={title} required maxLength={80}
                   placeholder={kind === 'grant_minutes' ? '+30 минут игр' : 'Поход в кино'}
                   onChange={(e) => setTitle(e.target.value)} />
          </Field>
          {kind === 'grant_minutes' ? (
            <Field label="Сколько минут даёт" htmlFor="st-min">
              <NumberInput id="st-min" value={minutes} onChange={setMinutes} min={1} max={600} />
            </Field>
          ) : (
            <Field label="Что именно" htmlFor="st-note" hint="Ребёнок увидит это в магазине.">
              <input id="st-note" className="input" value={note} maxLength={200} placeholder="в выходные, с семьёй"
                     onChange={(e) => setNote(e.target.value)} />
            </Field>
          )}
          <Field label="Цена" htmlFor="st-cost">
            <div className="row nowrap" style={{ ['--gap' as string]: '8px' }}>
              <NumberInput id="st-cost" value={cost} onChange={setCost} min={1} max={100000} />
              <select className="select" style={{ width: 150 }} aria-label="Валюта" value={currency}
                      onChange={(e) => setCurrency(e.target.value as 'credits' | 'minutes')}>
                <option value="credits">кредитов</option>
                <option value="minutes">минут</option>
              </select>
            </div>
          </Field>
          <Field label="Сколько раз в день" htmlFor="st-max">
            <NumberInput id="st-max" value={maxPerDay} onChange={setMaxPerDay} min={1} max={50} />
          </Field>
        </div>
        <Switch checked={approval} onChange={setApproval}
                label="Нужно моё одобрение — цена списывается сразу, эффект после одобрения" />
        <div className="row end">
          <button type="submit" className="btn" disabled={busy || !title.trim()}>
            <Icon name="plus" />Добавить
          </button>
        </div>
      </form>
    </Card>
  );
}

export function StorePage({ me }: { me: Me }): JSX.Element {
  const { data, error, loading, reload } = useAsync(() => api.store());
  const toast = useToast();
  const canEdit = me.role !== 'viewer';

  async function toggle(item: ShopItem, enabled: boolean): Promise<void> {
    try {
      await api.setStoreItemEnabled(item.id, enabled);
      toast.success(enabled ? `«${item.title}» снова в магазине.` : `«${item.title}» скрыт от ребёнка.`);
      reload();
    } catch (e) {
      toast.error(errorText(e));
    }
  }

  return (
    <>
      <PageHead title="Магазин" lead="Награды, которые ребёнок покупает за кредиты." />
      <ErrorBox message={error} />
      {loading && <div className="grid cards"><CardSkeleton /><CardSkeleton /></div>}
      {data && data.length === 0 && (
        <Card><Empty icon="bag" title="Магазин пуст">Добавьте первый товар — например, «+30 минут игр» за 60 кредитов.</Empty></Card>
      )}
      {data && data.length > 0 && (
        <div className="grid cards" data-testid="store-items">
          {data.map((i) => <ItemCard key={i.id} item={i} canEdit={canEdit} onToggle={(v) => void toggle(i, v)} />)}
        </div>
      )}
      {canEdit && <NewItem onCreated={reload} />}
    </>
  );
}
