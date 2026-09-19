import type { JSX } from 'react';
import { useState, type FormEvent } from 'react';
import { useParams } from 'react-router-dom';
import type { Policy, TimeWindow } from '@mykids/contracts';
import { convertCredits } from '@mykids/domain';
import { api, type Me, type TamperEvent } from '../api.js';
import { ErrorBox, Loading, useAsync } from '../components/Async.js';

const DAYS = ['вс', 'пн', 'вт', 'ср', 'чт', 'пт', 'сб'];
const REASONS: Record<string, string> = {
  daily_grant: 'дневная выдача', carry_over: 'перенос со вчера',
  task_reward: 'за задание', conversion_spend: 'обмен: списание кредитов',
  conversion_gain: 'обмен: начисление минут', purchase: 'покупка',
  purchase_grant: 'эффект покупки', screen_usage: 'использование экрана',
  manual_adjust: 'корректировка родителя', tamper_penalty: 'вмешательство в агент',
  expiry: 'сгорело',
};

/**
 * Симулятор экономики.
 *
 * Самая полезная штука на странице: курс обмена невозможно настроить наугад.
 * Считает теми же функциями домена, что и сервер, поэтому цифра здесь и
 * начисление в журнале не разойдутся.
 */
function Simulator({ policy }: { policy: Policy }): JSX.Element {
  const [credits, setCredits] = useState(40);
  const result = convertCredits(
    Math.floor(credits / policy.economy.creditsPerMinute) || 1,
    {
      creditsBalance: credits, minutesBalance: 0,
      convertedMinutesToday: 0, creditsEarnedToday: 0, grantedMinutesToday: 0,
    },
    policy.economy,
  );

  const minutes = result.ok ? result.minutes : 0;
  const capped = minutes >= policy.economy.maxConvertedMinutesPerDay;

  return (
    <div className="card">
      <h3>Проверка курса</h3>
      <div className="row">
        <div className="field">
          <label htmlFor="sim">Заработано кредитов за день</label>
          <input id="sim" type="number" min={0} max={1000} value={credits}
                 onChange={(e) => setCredits(Number(e.target.value))} />
        </div>
        <p style={{ margin: 0 }}>
          → <strong data-testid="sim-minutes">{minutes}</strong> минут экрана
          {capped && <span className="tag warn" style={{ marginLeft: 8 }}>упёрлось в дневной потолок обмена</span>}
          {!result.ok && <span className="note" style={{ marginLeft: 8 }}>{result.message}</span>}
        </p>
      </div>
      <p className="note" style={{ marginTop: 10, marginBottom: 0 }}>
        Дневной лимит и покупки сюда не входят — это только обмен заработанного.
      </p>
    </div>
  );
}

function WindowsEditor({
  windows, onChange, disabled,
}: { windows: TimeWindow[]; onChange: (w: TimeWindow[]) => void; disabled: boolean }): JSX.Element {
  const update = (i: number, patch: Partial<TimeWindow>): void => {
    onChange(windows.map((w, idx) => (idx === i ? { ...w, ...patch } : w)));
  };

  return (
    <>
      {windows.map((w, i) => (
        <div className="win-row" key={i}>
          <input value={w.name} disabled={disabled} aria-label="Название окна"
                 onChange={(e) => update(i, { name: e.target.value })} />
          <div className="days">
            {DAYS.map((d, day) => (
              <label key={day}>
                <input type="checkbox" disabled={disabled} checked={w.days.includes(day)}
                       onChange={(e) => update(i, {
                         days: (e.target.checked
                           ? [...w.days, day]
                           : w.days.filter((x) => x !== day)) as TimeWindow['days'],
                       })} />
                {d}
              </label>
            ))}
          </div>
          <input value={w.from} disabled={disabled} aria-label="Начало"
                 onChange={(e) => update(i, { from: e.target.value })} />
          <input value={w.to} disabled={disabled} aria-label="Конец"
                 onChange={(e) => update(i, { to: e.target.value })} />
          <select value={w.mode} disabled={disabled} aria-label="Режим"
                  onChange={(e) => update(i, { mode: e.target.value as TimeWindow['mode'] })}>
            <option value="blocked">блокировать</option>
            <option value="tasks_only">только задания</option>
            <option value="allowed">разрешить</option>
          </select>
          <button type="button" className="danger" disabled={disabled}
                  onClick={() => onChange(windows.filter((_, idx) => idx !== i))}>×</button>
        </div>
      ))}
      <p className="note">
        Окно, у которого конец меньше начала, пересекает полночь. Его дни относятся
        ко дню начала: отбой «21:30–07:00 по будням» действует и в 06:00 субботы.
      </p>
      <button type="button" className="ghost" disabled={disabled}
              onClick={() => onChange([...windows, {
                name: 'новое окно', days: [1, 2, 3, 4, 5],
                from: '21:30', to: '07:00', mode: 'blocked',
              } as TimeWindow])}>
        Добавить окно
      </button>
    </>
  );
}

/** Задержка доставки, которую стоит показать отдельной строкой. */
function delivered(occurredAt: string, recordedAt: string): boolean {
  return new Date(recordedAt).getTime() - new Date(occurredAt).getTime() > 5 * 60 * 1000;
}

const TAMPER_KINDS: Record<string, string> = {
  clock: 'переведены системные часы',
  unclean_stop: 'агент остановлен нештатно',
  permissions: 'отозваны разрешения агента',
  helper_lied: 'наблюдатель на компьютере сообщает неправду',
};

/**
 * События вмешательства.
 *
 * Отдельно от журнала операций: там движение минут и кредитов, а здесь факты,
 * которые сами по себе ничего не списывают. Решение — наказывать или нет —
 * остаётся за родителем, поэтому единственное действие тут «разобрал».
 */
function TamperLog({ childId, query, readOnly }: {
  childId: string;
  query: ReturnType<typeof useAsync<{ pending: number; events: TamperEvent[] }>>;
  readOnly: boolean;
}): JSX.Element | null {
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  // Пустой список не показываем: у большинства семей он будет пустым всегда,
  // и постоянный «0 событий» приучил бы не замечать этот блок.
  if (!query.data || query.data.events.length === 0) return null;
  const { pending, events } = query.data;

  return (
    <div className="card">
      <h3>Вмешательство в агент{pending > 0 ? ` — ${pending} неразобранных` : ''}</h3>
      <ErrorBox message={err ?? query.error} />
      <table>
        <thead>
          <tr><th>Когда</th><th>Устройство</th><th>Что случилось</th><th /></tr>
        </thead>
        <tbody>
          {events.map((e) => (
            <tr key={e.id} style={{ opacity: e.reviewedAt ? 0.5 : 1 }}>
              <td className="note">
                {new Date(e.occurredAt ?? e.recordedAt).toLocaleString('ru-RU')}
                {/* Сообщение могло пролежать в офлайне: тогда момент доставки
                    отличается от момента события, и это стоит показать. */}
                {e.occurredAt && delivered(e.occurredAt, e.recordedAt) && (
                  <div>доставлено {new Date(e.recordedAt).toLocaleString('ru-RU')}</div>
                )}
              </td>
              <td>{e.device?.name ?? '—'}</td>
              <td>
                {TAMPER_KINDS[e.kind] ?? e.kind}
                {e.detail && <span className="note"> — {e.detail}</span>}
              </td>
              <td className="note">{e.reviewedAt ? 'разобрано' : ''}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {!readOnly && pending > 0 && (
        <button type="button" disabled={busy} style={{ marginTop: 12 }} onClick={() => {
          setBusy(true);
          setErr(null);
          void api.reviewTampers(childId, events.filter((e) => !e.reviewedAt).map((e) => e.id))
            .then(() => query.reload())
            .catch((e: unknown) => setErr(e instanceof Error ? e.message : String(e)))
            .finally(() => setBusy(false));
        }}>Отметить разобранными</button>
      )}
      <p className="note" style={{ marginTop: 8 }}>
        Само событие минут не списывает — кроме одного случая: после нештатной остановки
        агент оплачивает пропущенное время сам, и в описании написано сколько. Отличить
        сбой питания от снятия агента он не может, поэтому если это была не попытка
        схитрить — верните время ручной корректировкой на ту же величину.
      </p>
    </div>
  );
}

export function ChildPage({ me }: { me: Me }): JSX.Element {
  const { childId = '' } = useParams();
  const readOnly = me.role === 'viewer';

  const policyQ = useAsync(() => api.policy(childId), [childId]);
  const ledgerQ = useAsync(() => api.ledger(childId), [childId]);
  const tamperQ = useAsync(() => api.tampers(childId), [childId]);

  const [draft, setDraft] = useState<Policy | null>(null);
  const [saveMsg, setSaveMsg] = useState<string | null>(null);
  const [saveErr, setSaveErr] = useState<string | null>(null);
  const policy = draft ?? policyQ.data;

  const [adjust, setAdjust] = useState({ currency: 'credits' as 'credits' | 'minutes', amount: 10, note: '' });
  const [adjustErr, setAdjustErr] = useState<string | null>(null);
  const [deviceName, setDeviceName] = useState('ПК ребёнка');
  const [devicePlatform, setDevicePlatform] = useState<'windows' | 'android' | 'web'>('windows');
  const [issuedToken, setIssuedToken] = useState<string | null>(null);
  const [deviceErr, setDeviceErr] = useState<string | null>(null);

  async function savePolicy(e: FormEvent): Promise<void> {
    e.preventDefault();
    if (!policy) return;
    setSaveErr(null);
    setSaveMsg(null);
    try {
      await api.savePolicy(childId, {
        timezone: policy.timezone,
        dailyLimitMinutes: policy.dailyLimitMinutes,
        carryOverMaxMinutes: policy.carryOverMaxMinutes,
        windows: policy.windows,
        alwaysAllowed: [],
        economy: policy.economy,
      });
      setDraft(null);
      policyQ.reload();
      setSaveMsg('Политика сохранена.');
    } catch (err) {
      setSaveErr(err instanceof Error ? err.message : String(err));
    }
  }

  async function doAdjust(e: FormEvent): Promise<void> {
    e.preventDefault();
    setAdjustErr(null);
    try {
      await api.adjust(childId, adjust);
      setAdjust({ ...adjust, note: '' });
      ledgerQ.reload();
    } catch (err) {
      setAdjustErr(err instanceof Error ? err.message : String(err));
    }
  }

  return (
    <>
      <h2>Ребёнок</h2>
      <p className="sub">Правила, журнал операций и устройства.</p>
      <ErrorBox message={policyQ.error} />

      {ledgerQ.data && (
        <div className="card">
          <div className="row">
            <div className="stat">
              <div className="num" data-testid="bal-minutes">{ledgerQ.data.balances.minutes}</div>
              <div className="lbl">минут на счету</div>
            </div>
            <div className="stat">
              <div className="num" data-testid="bal-credits">{ledgerQ.data.balances.credits}</div>
              <div className="lbl">кредитов</div>
            </div>
          </div>
        </div>
      )}

      {policyQ.loading && <Loading />}

      {policy && (
        <>
          <form className="card" onSubmit={(e) => void savePolicy(e)}>
            <h3>Дневные лимиты</h3>
            <ErrorBox message={saveErr} />
            {saveMsg && <div className="ok-box">{saveMsg}</div>}
            <div className="row">
              {DAYS.map((d, i) => (
                <div className="field" key={d} style={{ minWidth: 70 }}>
                  <label htmlFor={`lim-${i}`}>{d}</label>
                  <input id={`lim-${i}`} type="number" min={0} max={1440} disabled={readOnly}
                         value={policy.dailyLimitMinutes[i] ?? 0}
                         onChange={(e) => setDraft({
                           ...policy,
                           dailyLimitMinutes: policy.dailyLimitMinutes.map(
                             (v, idx) => (idx === i ? Number(e.target.value) : v)),
                         })} />
                </div>
              ))}
              <div className="field" style={{ minWidth: 120 }}>
                <label htmlFor="carry">перенос, макс</label>
                <input id="carry" type="number" min={0} max={600} disabled={readOnly}
                       value={policy.carryOverMaxMinutes}
                       onChange={(e) => setDraft({ ...policy, carryOverMaxMinutes: Number(e.target.value) })} />
              </div>
            </div>

            <h3 style={{ marginTop: 20 }}>Расписание</h3>
            <WindowsEditor windows={policy.windows} disabled={readOnly}
                           onChange={(windows) => setDraft({ ...policy, windows })} />

            <h3 style={{ marginTop: 20 }}>Экономика</h3>
            <div className="row">
              <div className="field">
                <label htmlFor="cpm">кредитов за минуту</label>
                <input id="cpm" type="number" min={1} max={100} disabled={readOnly}
                       value={policy.economy.creditsPerMinute}
                       onChange={(e) => setDraft({
                         ...policy,
                         economy: { ...policy.economy, creditsPerMinute: Number(e.target.value) },
                       })} />
              </div>
              <div className="field">
                <label htmlFor="cap">потолок обмена в день, мин</label>
                <input id="cap" type="number" min={0} max={600} disabled={readOnly}
                       value={policy.economy.maxConvertedMinutesPerDay}
                       onChange={(e) => setDraft({
                         ...policy,
                         economy: { ...policy.economy, maxConvertedMinutesPerDay: Number(e.target.value) },
                       })} />
              </div>
              <div className="field">
                <label htmlFor="mincr">минимум обмена, кредитов</label>
                <input id="mincr" type="number" min={0} max={200} disabled={readOnly}
                       value={policy.economy.minCreditsToConvert}
                       onChange={(e) => setDraft({
                         ...policy,
                         economy: { ...policy.economy, minCreditsToConvert: Number(e.target.value) },
                       })} />
              </div>
              <div className="field">
                <label htmlFor="maxcr">потолок кредитов в день</label>
                <input id="maxcr" type="number" min={1} max={1000} disabled={readOnly}
                       value={policy.economy.maxCreditsPerDay}
                       onChange={(e) => setDraft({
                         ...policy,
                         economy: { ...policy.economy, maxCreditsPerDay: Number(e.target.value) },
                       })} />
              </div>
            </div>
            <p className="note" style={{ marginTop: 10 }}>
              Потолок обмена — главная защита от фарма: без него ребёнок решает
              двести однотипных примеров и играет весь день.
            </p>
            {!readOnly && (
              <button type="submit" style={{ marginTop: 12 }} disabled={draft === null}>
                Сохранить политику
              </button>
            )}
          </form>

          <Simulator policy={policy} />
        </>
      )}

      {!readOnly && (
        <form className="card" onSubmit={(e) => void doAdjust(e)}>
          <h3>Ручная корректировка</h3>
          <ErrorBox message={adjustErr} />
          <div className="row">
            <div className="field" style={{ minWidth: 130 }}>
              <label htmlFor="adj-cur">Что</label>
              <select id="adj-cur" value={adjust.currency}
                      onChange={(e) => setAdjust({ ...adjust, currency: e.target.value as 'credits' | 'minutes' })}>
                <option value="credits">кредиты</option>
                <option value="minutes">минуты</option>
              </select>
            </div>
            <div className="field" style={{ minWidth: 110 }}>
              <label htmlFor="adj-amt">Сколько</label>
              <input id="adj-amt" type="number" value={adjust.amount}
                     onChange={(e) => setAdjust({ ...adjust, amount: Number(e.target.value) })} />
            </div>
            <div className="field" style={{ flex: 1, minWidth: 220 }}>
              <label htmlFor="adj-note">Причина</label>
              <input id="adj-note" value={adjust.note} required minLength={3}
                     placeholder="за помощь по дому"
                     onChange={(e) => setAdjust({ ...adjust, note: e.target.value })} />
            </div>
            <button type="submit" disabled={adjust.note.trim().length < 3}>Записать</button>
          </div>
          <p className="note" style={{ marginTop: 8 }}>
            Причина обязательна: журнал должен объяснять сам себя через полгода.
          </p>
        </form>
      )}

      {!readOnly && (
        <div className="card">
          <h3>Привязать устройство</h3>
          <ErrorBox message={deviceErr} />
          <div className="row">
            <div className="field" style={{ minWidth: 130 }}>
              <label htmlFor="dev-pl">Платформа</label>
              <select id="dev-pl" value={devicePlatform}
                      onChange={(e) => setDevicePlatform(e.target.value as 'windows' | 'android' | 'web')}>
                <option value="windows">Windows</option>
                <option value="android">Android</option>
                <option value="web">браузер</option>
              </select>
            </div>
            <div className="field">
              <label htmlFor="dev-name">Название</label>
              <input id="dev-name" value={deviceName} onChange={(e) => setDeviceName(e.target.value)} />
            </div>
            <button type="button" onClick={() => {
              setDeviceErr(null);
              void api.enrollDevice(childId, { platform: devicePlatform, name: deviceName })
                .then((r) => setIssuedToken(r.token))
                .catch((e: unknown) => {
                  setIssuedToken(null);
                  setDeviceErr(e instanceof Error ? e.message : String(e));
                });
            }}>Выдать токен</button>
          </div>
          {issuedToken && (
            <div className="ok-box" style={{ marginTop: 12 }}>
              <p style={{ margin: '0 0 6px' }}>
                Токен показывается один раз — в базе остаётся только его хеш.
              </p>
              <p className="mono" style={{ margin: 0 }} data-testid="device-token">{issuedToken}</p>
            </div>
          )}
        </div>
      )}

      <TamperLog childId={childId} query={tamperQ} readOnly={readOnly} />

      <div className="card">
        <h3>Журнал операций</h3>
        <ErrorBox message={ledgerQ.error} />
        {ledgerQ.loading && <Loading />}
        {ledgerQ.data?.entries.length === 0 && <p className="note">Пока пусто.</p>}
        {ledgerQ.data && ledgerQ.data.entries.length > 0 && (
          <table>
            <thead>
              <tr><th>Когда</th><th>Что</th><th>Сколько</th><th>Почему</th></tr>
            </thead>
            <tbody>
              {ledgerQ.data.entries.map((e) => (
                <tr key={e.id}>
                  <td className="note">{new Date(e.recordedAt).toLocaleString('ru-RU')}</td>
                  <td>{e.currency === 'minutes' ? 'минуты' : 'кредиты'}</td>
                  <td style={{ color: e.amount < 0 ? 'var(--bad)' : 'var(--ok)' }}>
                    {e.amount > 0 ? '+' : ''}{e.amount}
                  </td>
                  <td>
                    {REASONS[e.reason] ?? e.reason}
                    {e.note && <span className="note"> — {e.note}</span>}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </>
  );
}
