import type { JSX } from 'react';
import { useState, type FormEvent } from 'react';
import type { TimeWindow } from '@mykids/contracts';
import { api, deviceToken, ApiError } from '../api.js';
import { Tasks } from './Tasks.js';
import { ErrorBox, Loading, useAsync } from '../components/Async.js';

const DAYS = ['вс', 'пн', 'вт', 'ср', 'чт', 'пт', 'сб'];

function describeWindow(w: TimeWindow): string {
  const days = w.days.length === 7 ? 'каждый день' : w.days.map((d) => DAYS[d]).join(', ');
  const mode = w.mode === 'blocked' ? 'экран закрыт'
    : w.mode === 'tasks_only' ? 'только задания' : 'экран открыт';
  return `${days}, ${w.from}–${w.to} — ${mode}`;
}

function TokenForm({ onSaved, invalid = false }: { onSaved: () => void; invalid?: boolean }): JSX.Element {
  const [token, setToken] = useState('');
  return (
    <div className="auth">
      <h2>Это устройство</h2>
      <p className="sub">Вставьте токен, который выдал родитель в админке.</p>
      {invalid && <div className="err">Токен не подошёл или был отозван — попросите новый.</div>}
      <form className="card" onSubmit={(e: FormEvent) => {
        e.preventDefault();
        deviceToken.set(token.trim());
        onSaved();
      }}>
        <div className="field" style={{ marginBottom: 12 }}>
          <label htmlFor="devtok">Токен устройства</label>
          <input id="devtok" value={token} required onChange={(e) => setToken(e.target.value)} />
        </div>
        <button type="submit">Подключить</button>
      </form>
    </div>
  );
}

export function ChildApp(): JSX.Element {
  const [hasToken, setHasToken] = useState(() => deviceToken.get() !== null);
  const [reloadKey, setReloadKey] = useState(0);
  // Без токена запрос не делаем вовсе: иначе первый 401 прилетает раньше, чем
  // ребёнок успел ввести токен, и стирает его.
  const me = useAsync(() => api.childMe(), [reloadKey, hasToken], { enabled: hasToken });
  const store = useAsync(() => api.childStore(), [reloadKey, hasToken], { enabled: hasToken });

  const [minutes, setMinutes] = useState(10);
  const [actionErr, setActionErr] = useState<string | null>(null);
  const [actionOk, setActionOk] = useState<string | null>(null);

  if (!hasToken) return <TokenForm onSaved={() => setHasToken(true)} />;

  // Неверный или отозванный токен возвращает к вводу, а не показывает пустой
  // экран. Сравниваем код ответа, а не текст сообщения: текст меняется.
  if (me.status === 401) {
    return (
      <TokenForm
        invalid
        onSaved={() => { setHasToken(true); setReloadKey((k) => k + 1); }}
      />
    );
  }

  async function act<T>(fn: () => Promise<T>, success: (result: T) => string): Promise<void> {
    setActionErr(null);
    setActionOk(null);
    try {
      // Сообщение строится по ответу сервера, а не по тому, что запросили:
      // обмен урезается до доступного, и обещать больше выданного нельзя.
      setActionOk(success(await fn()));
      setReloadKey((k) => k + 1);
    } catch (err) {
      setActionErr(err instanceof ApiError ? err.message : String(err));
    }
  }

  const screen = me.data?.screen;

  return (
    <main className="main" style={{ margin: '0 auto' }}>
      <h2>{me.data ? `Привет, ${me.data.name}` : 'Загрузка…'}</h2>
      <ErrorBox message={me.error} />
      {me.loading && <Loading />}

      {me.data && (
        <>
          <div className="card">
            <div className="row">
              <div className="stat">
                <div className="num" data-testid="child-minutes">{me.data.balances.minutes}</div>
                <div className="lbl">минут экрана</div>
              </div>
              <div className="stat">
                <div className="num" data-testid="child-credits">{me.data.balances.credits}</div>
                <div className="lbl">кредитов</div>
              </div>
              <div className="stat" style={{ minWidth: 200 }}>
                {screen?.allowed
                  ? <span className="tag ok">экран открыт</span>
                  : <span className="tag bad">
                      {screen?.reason === 'window_blocked' ? `закрыт: ${screen.window}`
                        : screen?.reason === 'tasks_only' ? `сейчас только задания: ${screen.window}`
                        : 'время на сегодня кончилось'}
                    </span>}
              </div>
            </div>
          </div>

          {actionErr && <div className="err">{actionErr}</div>}
          {actionOk && <div className="ok-box">{actionOk}</div>}

          <div className="card">
            <Tasks onEarned={() => setReloadKey((k) => k + 1)} />
          </div>

          <div className="card">
            <h3>Обменять кредиты на время</h3>
            <div className="row">
              <div className="field" style={{ minWidth: 120 }}>
                <label htmlFor="conv">Сколько минут</label>
                <input id="conv" type="number" min={1} max={600} value={minutes}
                       onChange={(e) => setMinutes(Number(e.target.value))} />
              </div>
              <p style={{ margin: 0 }} className="note">
                Это будет стоить {minutes * me.data.policy.economy.creditsPerMinute} кредитов
              </p>
              <button onClick={() => void act(
                () => api.childConvert(minutes),
                (r) => (r.minutes < minutes
                  ? `Получено ${r.minutes} минут вместо ${minutes}: больше не хватило кредитов или упёрлось в дневной потолок.`
                  : `Получено ${r.minutes} минут за ${r.creditsSpent} кредитов.`),
              )}>Обменять</button>
            </div>
            <p className="note" style={{ marginTop: 8 }}>
              За день обменом можно получить не больше {me.data.policy.economy.maxConvertedMinutesPerDay} минут.
            </p>
          </div>

          <div className="card">
            <h3>Магазин</h3>
            {store.data?.length === 0 && <p className="note">Пока пусто.</p>}
            {store.data?.map((i) => {
              const effect = i.effect as { kind: string; minutes?: number; note?: string };
              return (
                <div className="row" key={i.id}
                     style={{ justifyContent: 'space-between', paddingBottom: 10, marginBottom: 10,
                              borderBottom: '1px solid var(--line)' }}>
                  <div>
                    <strong>{i.title}</strong>
                    <div className="note">
                      {effect.kind === 'grant_minutes' ? `+${effect.minutes} минут` : effect.note}
                      {i.requiresApproval && ' · нужно одобрение родителя'}
                    </div>
                  </div>
                  <div className="row">
                    <span>{i.costAmount} {i.costCurrency === 'credits' ? 'кр.' : 'мин.'}</span>
                    <button onClick={() => void act(
                      () => api.childBuy(i.id),
                      () => (i.requiresApproval ? 'Отправлено на одобрение родителю.' : 'Куплено.'),
                    )}>Купить</button>
                  </div>
                </div>
              );
            })}
          </div>

          <div className="card">
            <h3>Правила</h3>
            {/* Правила показываются намеренно: скрытая механика воспринимается
                как несправедливость и провоцирует искать обход. */}
            <table>
              <tbody>
                <tr>
                  <td>Лимит на сегодня</td>
                  <td>{me.data.policy.dailyLimitMinutes[new Date().getDay()]} минут</td>
                </tr>
                <tr>
                  <td>Переносится на завтра</td>
                  <td>не больше {me.data.policy.carryOverMaxMinutes} минут</td>
                </tr>
                <tr>
                  <td>Курс обмена</td>
                  <td>{me.data.policy.economy.creditsPerMinute} кредита за минуту</td>
                </tr>
                <tr>
                  <td>Можно заработать за день</td>
                  <td>до {me.data.policy.economy.maxCreditsPerDay} кредитов</td>
                </tr>
              </tbody>
            </table>
            {me.data.policy.windows.length > 0 && (
              <>
                <h3 style={{ marginTop: 16 }}>Расписание</h3>
                <ul className="note" style={{ margin: 0, paddingLeft: 18 }}>
                  {me.data.policy.windows.map((w, i) => (
                    <li key={i}><strong>{w.name}</strong>: {describeWindow(w)}</li>
                  ))}
                </ul>
              </>
            )}
          </div>
        </>
      )}
    </main>
  );
}
