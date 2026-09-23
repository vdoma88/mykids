import type { JSX } from 'react';
import { useState } from 'react';
import { api, type ChildMe, type ShopItem } from '../api.js';
import { credits, minutes as minutesText, num } from '../lib/format.js';
import { describeEffect } from '../lib/labels.js';
import { errorText } from '../ui/Async.js';
import { Icon } from '../ui/Icon.js';
import { Alert, Card, Empty } from '../ui/kit.js';

type Outcome = { tone: 'success' | 'danger'; text: string } | null;

/** Обмен кредитов на минуты. */
function Convert({ me, onChanged }: { me: ChildMe; onChanged: () => void }): JSX.Element {
  const { creditsPerMinute, maxConvertedMinutesPerDay, minCreditsToConvert } = me.policy.economy;
  const affordable = Math.floor(me.balances.credits / creditsPerMinute);
  const [amount, setAmount] = useState(() => Math.max(1, Math.min(10, affordable || 10)));
  const [busy, setBusy] = useState(false);
  const [outcome, setOutcome] = useState<Outcome>(null);
  const cost = amount * creditsPerMinute;
  const clamp = (n: number): number => Math.max(1, Math.min(600, Math.trunc(n) || 1));

  async function convert(): Promise<void> {
    setBusy(true);
    setOutcome(null);
    try {
      const r = await api.childConvert(amount);
      // Сообщение строится по ответу сервера, а не по запрошенному: обмен
      // урезается до доступного, и обещать больше выданного нельзя.
      setOutcome({
        tone: 'success',
        text: r.minutes < amount
          ? `Получено ${minutesText(r.minutes)} вместо ${amount}: больше не хватило кредитов или упёрлось в дневной потолок.`
          : `Получено ${minutesText(r.minutes)} за ${credits(r.creditsSpent)}.`,
      });
      onChanged();
    } catch (e) {
      setOutcome({ tone: 'danger', text: errorText(e) });
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card title="Обменять кредиты на время" desc={`Курс: ${credits(creditsPerMinute)} за минуту. За день обменом — не больше ${minutesText(maxConvertedMinutesPerDay)}.`}>
      <div className="stack">
        <div className="row">
          <label htmlFor="conv" className="small muted">Сколько минут</label>
          <div className="stepper">
            <button type="button" aria-label="Меньше" onClick={() => setAmount((a) => clamp(a - 5))}>−</button>
            <input id="conv" type="number" inputMode="numeric" min={1} max={600} value={amount}
                   onChange={(e) => setAmount(clamp(e.target.valueAsNumber))} />
            <button type="button" aria-label="Больше" onClick={() => setAmount((a) => clamp(a + 5))}>+</button>
          </div>
          <span className="small muted">= {credits(cost)}</span>
          <span className="spacer" />
          <button type="button" className="btn" disabled={busy} onClick={() => void convert()}>
            <Icon name="swap" />Обменять
          </button>
        </div>
        {me.balances.credits < minCreditsToConvert && (
          <p className="small muted">Обмен — от {credits(minCreditsToConvert)}. Сейчас у тебя {num(me.balances.credits)}.</p>
        )}
        {outcome && <Alert tone={outcome.tone}>{outcome.text}</Alert>}
      </div>
    </Card>
  );
}

function Store({ me, items, onChanged }: { me: ChildMe; items: ShopItem[] | null; onChanged: () => void }): JSX.Element {
  const [outcome, setOutcome] = useState<Outcome>(null);
  const [busy, setBusy] = useState<string | null>(null);

  async function buy(i: ShopItem): Promise<void> {
    setBusy(i.id);
    setOutcome(null);
    try {
      await api.childBuy(i.id);
      setOutcome({
        tone: 'success',
        text: i.requiresApproval ? `«${i.title}»: отправлено на одобрение родителю.` : `Куплено: «${i.title}».`,
      });
      onChanged();
    } catch (e) {
      setOutcome({ tone: 'danger', text: errorText(e) });
    } finally {
      setBusy(null);
    }
  }

  return (
    <Card title="Магазин" desc="Цена товара с одобрением списывается сразу; если родитель откажет, кредиты вернутся.">
      <div className="stack">
        {outcome && <Alert tone={outcome.tone}>{outcome.text}</Alert>}
        {items?.length === 0 && <Empty icon="bag" title="Пока пусто">Товары заводит родитель.</Empty>}
        <div className="list">
          {items?.map((i) => {
            const have = i.costCurrency === 'credits' ? me.balances.credits : me.balances.minutes;
            const short = i.costAmount - have;
            return (
              <div className="list-item shop-item" key={i.id}>
                <span className="tile-icon accent">
                  <Icon name={(i.effect as { kind?: string }).kind === 'grant_minutes' ? 'clock' : 'gift'} />
                </span>
                <div className="grow">
                  <div className="title">{i.title}</div>
                  <div className="meta">
                    {describeEffect(i.effect)}
                    {i.requiresApproval && ' · с одобрением'}
                    {short > 0 && ` · не хватает ${num(short)}`}
                  </div>
                </div>
                <span className="price">{num(i.costAmount)} {i.costCurrency === 'credits' ? 'кр.' : 'мин'}</span>
                <button type="button" className={short > 0 ? 'btn secondary sm' : 'btn sm'}
                        disabled={busy !== null} onClick={() => void buy(i)}>
                  Купить
                </button>
              </div>
            );
          })}
        </div>
      </div>
    </Card>
  );
}

export function KidShop({ me, items, onChanged }: { me: ChildMe; items: ShopItem[] | null; onChanged: () => void }): JSX.Element {
  return (
    <>
      <Convert me={me} onChanged={onChanged} />
      <Store me={me} items={items} onChanged={onChanged} />
    </>
  );
}
