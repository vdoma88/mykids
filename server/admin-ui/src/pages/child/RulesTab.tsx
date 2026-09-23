import type { JSX } from 'react';
import { useEffect, useMemo, useState } from 'react';
import type { Policy, TimeWindow } from '@mykids/contracts';
import { convertCredits } from '@mykids/domain';
import { api } from '../../api.js';
import { duration, minutes as minutesText, num } from '../../lib/format.js';
import { DAYS_SHORT, WEEK_ORDER } from '../../lib/labels.js';
import { CardSkeleton, ErrorBox, errorText, useAsync } from '../../ui/Async.js';
import { Icon } from '../../ui/Icon.js';
import { Alert, Badge, Card, Field, NumberInput } from '../../ui/kit.js';
import { useToast } from '../../ui/Toast.js';

/**
 * Часовые пояса для выбора. Полный список браузера — если он его отдаёт,
 * иначе короткий российский.
 */
function timezones(current: string): string[] {
  const intl = Intl as unknown as { supportedValuesOf?: (k: string) => string[] };
  const all = intl.supportedValuesOf?.('timeZone') ?? [
    'Europe/Kaliningrad', 'Europe/Moscow', 'Europe/Samara', 'Asia/Yekaterinburg', 'Asia/Omsk',
    'Asia/Novosibirsk', 'Asia/Krasnoyarsk', 'Asia/Irkutsk', 'Asia/Yakutsk', 'Asia/Vladivostok',
    'Asia/Magadan', 'Asia/Kamchatka',
  ];
  return all.includes(current) ? all : [current, ...all];
}

/** Что не так с окном расписания — до отправки, а не ответом сервера. */
function windowProblem(w: TimeWindow): string | null {
  if (!w.name.trim()) return 'Назовите окно — по названию ребёнок узнает его у себя в правилах.';
  if (w.days.length === 0) return 'Отметьте хотя бы один день.';
  if (!/^\d{2}:\d{2}$/.test(w.from) || !/^\d{2}:\d{2}$/.test(w.to)) return 'Укажите время начала и конца.';
  if (w.from === w.to) return 'Начало и конец совпадают — окно пустое.';
  return null;
}

function WindowsEditor({ windows, onChange, disabled }: {
  windows: TimeWindow[]; onChange: (w: TimeWindow[]) => void; disabled: boolean;
}): JSX.Element {
  const update = (i: number, patch: Partial<TimeWindow>): void => {
    onChange(windows.map((w, idx) => (idx === i ? { ...w, ...patch } : w)));
  };

  return (
    <div className="stack" style={{ ['--gap' as string]: '12px' }}>
      {windows.length === 0 && (
        <p className="muted small">Окон нет: экран открыт в любое время, пока хватает минут.</p>
      )}
      {windows.map((w, i) => {
        const problem = windowProblem(w);
        const overnight = w.to < w.from;
        return (
          <div className="window-card" key={i}>
            <div className="window-top">
              <Field label="Название" className="wname">
                <input className="input" value={w.name} disabled={disabled} maxLength={40}
                       aria-label="Название окна" onChange={(e) => update(i, { name: e.target.value })} />
              </Field>
              <Field label="С" className="wfrom">
                <input className="input" type="time" value={w.from} disabled={disabled} aria-label="Начало"
                       onChange={(e) => update(i, { from: e.target.value.slice(0, 5) })} />
              </Field>
              <Field label="До" className="wto">
                <input className="input" type="time" value={w.to} disabled={disabled} aria-label="Конец"
                       onChange={(e) => update(i, { to: e.target.value.slice(0, 5) })} />
              </Field>
              <Field label="Режим" className="wmode">
                <select className="select" value={w.mode} disabled={disabled} aria-label="Режим"
                        onChange={(e) => update(i, { mode: e.target.value as TimeWindow['mode'] })}>
                  <option value="blocked">Экран закрыт</option>
                  <option value="tasks_only">Только задания</option>
                  <option value="allowed">Экран открыт</option>
                </select>
              </Field>
              {!disabled && (
                <button type="button" className="btn ghost icon-only wdel" aria-label={`Удалить окно «${w.name}»`}
                        onClick={() => onChange(windows.filter((_, idx) => idx !== i))}>
                  <Icon name="trash" />
                </button>
              )}
            </div>
            <div className="row between">
              <div className="day-chips" role="group" aria-label="Дни недели">
                {WEEK_ORDER.map((day) => (
                  <label className="day-chip" key={day}>
                    <input type="checkbox" disabled={disabled} checked={w.days.includes(day as never)}
                           aria-label={DAYS_SHORT[day]}
                           onChange={(e) => update(i, {
                             days: (e.target.checked
                               ? [...w.days, day].sort()
                               : w.days.filter((x) => x !== day)) as TimeWindow['days'],
                           })} />
                    <span>{DAYS_SHORT[day]}</span>
                  </label>
                ))}
              </div>
              {overnight && !problem && <Badge>через полночь</Badge>}
            </div>
            {problem && <span className="small" style={{ color: 'var(--danger)' }}>{problem}</span>}
          </div>
        );
      })}
      {!disabled && (
        <div>
          <button type="button" className="btn secondary sm"
                  onClick={() => onChange([...windows, {
                    name: 'Отбой', days: [0, 1, 2, 3, 4], from: '21:30', to: '07:00', mode: 'blocked',
                  } as TimeWindow])}>
            <Icon name="plus" size={16} />Добавить окно
          </button>
        </div>
      )}
      <p className="small muted">
        Окно, у которого конец раньше начала, идёт через полночь, и его дни — это дни
        начала: отбой «21:30–07:00 по будням» действует и в 06:00 субботы.
      </p>
    </div>
  );
}

/**
 * Проверка курса. Считает теми же функциями домена, что и сервер, поэтому
 * цифра здесь и начисление в журнале не разойдутся.
 */
function Simulator({ economy }: { economy: Policy['economy'] }): JSX.Element {
  const [earned, setEarned] = useState(40);
  const result = convertCredits(
    Math.floor(earned / economy.creditsPerMinute) || 1,
    {
      creditsBalance: earned, minutesBalance: 0,
      convertedMinutesToday: 0, creditsEarnedToday: 0, grantedMinutesToday: 0,
    },
    economy,
  );
  const got = result.ok ? result.minutes : 0;
  const capped = got >= economy.maxConvertedMinutesPerDay;

  return (
    <div className="stat" style={{ gap: 10 }}>
      <span className="stat-label"><Icon name="swap" />Проверка курса</span>
      <div className="row" style={{ ['--gap' as string]: '10px' }}>
        <label htmlFor="sim" className="small muted">Заработал за день</label>
        <NumberInput id="sim" className="input sm num inline" value={earned} onChange={setEarned} min={0} max={5000} />
        <span className="small muted">кредитов →</span>
        <strong className="num" style={{ fontSize: '1.2rem' }}><span data-testid="sim-minutes">{got}</span> мин</strong>
        {capped && <Badge tone="warning">упёрлось в потолок обмена</Badge>}
      </div>
      {!result.ok && <span className="small muted">{result.message}</span>}
    </div>
  );
}

export function RulesTab({ childId, readOnly }: { childId: string; readOnly: boolean }): JSX.Element {
  const policyQ = useAsync(() => api.policy(childId), [childId]);
  const [draft, setDraft] = useState<Policy | null>(null);
  const [saveErr, setSaveErr] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const toast = useToast();
  const policy = draft ?? policyQ.data;
  const browserTz = useMemo(() => Intl.DateTimeFormat().resolvedOptions().timeZone, []);

  // Уходя со страницы с несохранёнными правилами, человек обычно не знает,
  // что они не сохранились: браузер спросит.
  useEffect(() => {
    if (!draft) return;
    const warn = (e: BeforeUnloadEvent): void => { e.preventDefault(); };
    window.addEventListener('beforeunload', warn);
    return () => window.removeEventListener('beforeunload', warn);
  }, [draft]);

  if (!policy) return policyQ.error ? <ErrorBox message={policyQ.error} /> : <CardSkeleton lines={5} />;

  const set = (next: Policy): void => { setDraft(next); setSaveErr(null); };
  const setEconomy = (patch: Partial<Policy['economy']>): void =>
    set({ ...policy, economy: { ...policy.economy, ...patch } });
  const problems = policy.windows.map(windowProblem).filter(Boolean);
  const weekTotal = policy.dailyLimitMinutes.reduce((a, b) => a + b, 0);

  async function save(): Promise<void> {
    if (!policy || problems.length) return;
    setSaveErr(null);
    setSaving(true);
    try {
      await api.savePolicy(childId, {
        timezone: policy.timezone,
        dailyLimitMinutes: policy.dailyLimitMinutes,
        carryOverMaxMinutes: policy.carryOverMaxMinutes,
        windows: policy.windows,
        economy: policy.economy,
      });
      setDraft(null);
      policyQ.reload();
      toast.success('Политика сохранена.');
    } catch (err) {
      setSaveErr(errorText(err));
    } finally {
      setSaving(false);
    }
  }

  return (
    <>
      <Card title="Дневные лимиты" desc={`Минуты экрана, которые выдаются каждый день. За неделю — ${duration(weekTotal)}.`}>
        <div className="days-grid">
          {WEEK_ORDER.map((i) => (
            <div className={`day-cell ${i === 0 || i === 6 ? 'weekend' : ''}`} key={i}>
              <label htmlFor={`lim-${i}`}>{DAYS_SHORT[i]}</label>
              <NumberInput id={`lim-${i}`} disabled={readOnly} min={0} max={1440} className="input num"
                           value={policy.dailyLimitMinutes[i] ?? 0}
                           onChange={(n) => set({
                             ...policy,
                             dailyLimitMinutes: policy.dailyLimitMinutes.map((v, idx) => (idx === i ? n : v)),
                           })} />
            </div>
          ))}
        </div>
        <div className="grid two" style={{ marginTop: 18 }}>
          <Field label="Перенос на завтра, максимум" htmlFor="carry"
                 hint="Сколько неистраченных минут переходит на следующий день.">
            <NumberInput id="carry" disabled={readOnly} min={0} max={600} value={policy.carryOverMaxMinutes}
                         onChange={(n) => set({ ...policy, carryOverMaxMinutes: n })} />
          </Field>
          <Field label="Часовой пояс" htmlFor="tz" hint="По нему начинаются и кончаются сутки ребёнка.">
            <select id="tz" className="select" value={policy.timezone} disabled={readOnly}
                    onChange={(e) => set({ ...policy, timezone: e.target.value })}>
              {timezones(policy.timezone).map((z) => <option key={z} value={z}>{z.replace(/_/g, ' ')}</option>)}
            </select>
          </Field>
        </div>
        {!readOnly && browserTz && browserTz !== policy.timezone && (
          <div style={{ marginTop: 12 }}>
            <Alert tone="info" icon="globe">
              Сутки ребёнка считаются по поясу {policy.timezone}, а у вас в браузере — {browserTz}.{' '}
              <button type="button" className="linkish" onClick={() => set({ ...policy, timezone: browserTz })}>
                Поставить {browserTz}
              </button>
            </Alert>
          </div>
        )}
      </Card>

      <Card title="Расписание" desc="Окна, когда экран закрыт или открыт только для заданий, — независимо от остатка минут.">
        <WindowsEditor windows={policy.windows} disabled={readOnly}
                       onChange={(windows) => set({ ...policy, windows })} />
      </Card>

      <Card title="Экономика" desc="Курс обмена кредитов на минуты и потолки, которые не дают фармить.">
        <div className="grid two">
          <Field label="Кредитов за минуту" htmlFor="cpm">
            <NumberInput id="cpm" disabled={readOnly} min={1} max={100} value={policy.economy.creditsPerMinute}
                         onChange={(n) => setEconomy({ creditsPerMinute: n })} />
          </Field>
          <Field label="Потолок обмена в день, минут" htmlFor="cap"
                 hint="Главная защита от фарма: без неё двести однотипных примеров дают весь день игр.">
            <NumberInput id="cap" disabled={readOnly} min={0} max={600} value={policy.economy.maxConvertedMinutesPerDay}
                         onChange={(n) => setEconomy({ maxConvertedMinutesPerDay: n })} />
          </Field>
          <Field label="Минимум для обмена, кредитов" htmlFor="mincr">
            <NumberInput id="mincr" disabled={readOnly} min={0} max={200} value={policy.economy.minCreditsToConvert}
                         onChange={(n) => setEconomy({ minCreditsToConvert: n })} />
          </Field>
          <Field label="Потолок заработка в день, кредитов" htmlFor="maxcr">
            <NumberInput id="maxcr" disabled={readOnly} min={1} max={1000} value={policy.economy.maxCreditsPerDay}
                         onChange={(n) => setEconomy({ maxCreditsPerDay: n })} />
          </Field>
        </div>
        <div style={{ marginTop: 16 }}>
          <Simulator economy={policy.economy} />
        </div>
        <p className="small muted" style={{ marginTop: 10 }}>
          Больше всего за день обменом: {minutesText(policy.economy.maxConvertedMinutesPerDay)}
          {' '}— это {num(policy.economy.maxConvertedMinutesPerDay * policy.economy.creditsPerMinute)} кредитов.
        </p>
      </Card>

      {saveErr && <ErrorBox message={saveErr} />}

      {!readOnly && draft && (
        <div className="savebar" role="region" aria-label="Несохранённые изменения">
          <span className="grow">
            {problems.length ? 'Исправьте расписание, чтобы сохранить.' : 'Есть несохранённые изменения'}
          </span>
          <button type="button" className="btn ghost sm" onClick={() => { setDraft(null); setSaveErr(null); }}>
            Отменить
          </button>
          <button type="button" className="btn sm" disabled={saving || problems.length > 0} onClick={() => void save()}>
            {saving ? 'Сохраняю…' : 'Сохранить политику'}
          </button>
        </div>
      )}
    </>
  );
}
