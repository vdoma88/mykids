import type { JSX, ReactNode } from 'react';
import { useEffect, useId, useState } from 'react';
import { hueFor, initial } from '../lib/format.js';
import { Icon, type IconName } from './Icon.js';
import { useToast } from './Toast.js';

export function PageHead({ title, lead, actions, back }: {
  title: ReactNode; lead?: ReactNode; actions?: ReactNode; back?: ReactNode;
}): JSX.Element {
  return (
    <header>
      {back}
      <div className="page-head">
        <div>
          <h1>{title}</h1>
          {lead && <p className="lead">{lead}</p>}
        </div>
        {actions && <div className="row">{actions}</div>}
      </div>
    </header>
  );
}

export function Card({ title, desc, actions, children, className = '', id }: {
  title?: ReactNode; desc?: ReactNode; actions?: ReactNode; children?: ReactNode; className?: string; id?: string;
}): JSX.Element {
  return (
    <section className={`card ${className}`} id={id} aria-label={typeof title === 'string' ? title : undefined}>
      {(title || actions) && (
        <div className="card-head">
          <div>
            {title && <h2>{title}</h2>}
            {desc && <p className="desc">{desc}</p>}
          </div>
          {actions}
        </div>
      )}
      {children}
    </section>
  );
}

export function Stat({ label, value, icon, note, testId }: {
  label: string; value: ReactNode; icon?: IconName; note?: ReactNode; testId?: string;
}): JSX.Element {
  return (
    <div className="stat">
      <span className="stat-label">{icon && <Icon name={icon} />}{label}</span>
      <span className="stat-value" data-testid={testId}>{value}</span>
      {note && <span className="stat-note">{note}</span>}
    </div>
  );
}

export function Badge({ tone, children, dot }: {
  tone?: 'accent' | 'success' | 'danger' | 'warning'; children: ReactNode; dot?: boolean;
}): JSX.Element {
  return <span className={`badge ${tone ?? ''}`}>{dot && <span className="dot" />}{children}</span>;
}

export function Alert({ tone, children, icon }: {
  tone: 'danger' | 'success' | 'warning' | 'info'; children: ReactNode; icon?: IconName;
}): JSX.Element {
  const fallback: IconName = tone === 'success' ? 'check' : tone === 'info' ? 'info' : 'alert';
  return (
    <div className={`alert ${tone}`} role={tone === 'danger' ? 'alert' : 'status'}>
      <Icon name={icon ?? fallback} />
      <div>{children}</div>
    </div>
  );
}

export function Empty({ icon, title, children, action }: {
  icon: IconName; title: string; children?: ReactNode; action?: ReactNode;
}): JSX.Element {
  return (
    <div className="empty">
      <div className="empty-icon"><Icon name={icon} /></div>
      <h3>{title}</h3>
      {children && <p>{children}</p>}
      {action}
    </div>
  );
}

export function Avatar({ name, large }: { name: string; large?: boolean }): JSX.Element {
  return (
    <span className={`avatar ${large ? 'lg' : ''}`} style={{ ['--hue' as string]: hueFor(name) }} aria-hidden>
      {initial(name)}
    </span>
  );
}

/** Поле формы: подпись связана с полем, подсказка и ошибка — под ним. */
export function Field({ label, hint, error, children, htmlFor, className = '' }: {
  label: ReactNode; hint?: ReactNode; error?: string | null; children: ReactNode; htmlFor?: string; className?: string;
}): JSX.Element {
  return (
    <div className={`field ${className}`}>
      <label htmlFor={htmlFor}>{label}</label>
      {children}
      {error ? <span className="field-error">{error}</span> : hint ? <span className="hint">{hint}</span> : null}
    </div>
  );
}

export function Switch({ checked, onChange, label, disabled }: {
  checked: boolean; onChange: (v: boolean) => void; label: ReactNode; disabled?: boolean;
}): JSX.Element {
  return (
    <label className="switch">
      <input type="checkbox" role="switch" checked={checked} disabled={disabled}
             onChange={(e) => onChange(e.target.checked)} />
      <span className="track" />
      <span className="small">{label}</span>
    </label>
  );
}

export interface TabDef<K extends string> { key: K; label: string; icon?: IconName; count?: number }

/** Вкладки по правилам WAI-ARIA: стрелки переключают, Tab уходит в содержимое. */
export function Tabs<K extends string>({ tabs, value, onChange, label, segmented }: {
  tabs: TabDef<K>[]; value: K; onChange: (k: K) => void; label: string; segmented?: boolean;
}): JSX.Element {
  const id = useId();
  return (
    <div className={`tabs ${segmented ? 'segmented' : ''}`} role="tablist" aria-label={label}>
      {tabs.map((t, i) => (
        <button
          key={t.key}
          type="button"
          role="tab"
          id={`${id}-${t.key}`}
          className="tab"
          aria-selected={t.key === value}
          tabIndex={t.key === value ? 0 : -1}
          onClick={() => onChange(t.key)}
          onKeyDown={(e) => {
            const dir = e.key === 'ArrowRight' ? 1 : e.key === 'ArrowLeft' ? -1 : 0;
            if (!dir) return;
            e.preventDefault();
            const next = tabs[(i + dir + tabs.length) % tabs.length]!;
            onChange(next.key);
            document.getElementById(`${id}-${next.key}`)?.focus();
          }}
        >
          {t.icon && <Icon name={t.icon} />}
          {t.label}
          {t.count ? <span className="count">{t.count}</span> : null}
        </button>
      ))}
    </div>
  );
}

/**
 * Кнопка необратимого действия: первое нажатие просит подтвердить, второе —
 * делает. Без модального окна: оно перекрывало бы то, о чём спрашивает.
 */
export function ConfirmButton({ children, confirm, onConfirm, className = 'btn danger-ghost sm', disabled }: {
  children: ReactNode; confirm: string; onConfirm: () => void | Promise<void>; className?: string; disabled?: boolean;
}): JSX.Element {
  const [armed, setArmed] = useState(false);
  useEffect(() => {
    if (!armed) return;
    const t = window.setTimeout(() => setArmed(false), 4000);
    return () => window.clearTimeout(t);
  }, [armed]);
  return armed ? (
    <button type="button" className="btn danger sm" disabled={disabled}
            onClick={() => { setArmed(false); void onConfirm(); }}>
      {confirm}
    </button>
  ) : (
    <button type="button" className={className} disabled={disabled} onClick={() => setArmed(true)}>
      {children}
    </button>
  );
}

export function CopyButton({ text, label = 'Скопировать' }: { text: string; label?: string }): JSX.Element {
  const toast = useToast();
  return (
    <button
      type="button"
      className="btn secondary sm"
      onClick={() => {
        void navigator.clipboard?.writeText(text)
          .then(() => toast.success('Скопировано.'))
          // На http:// без localhost буфер обмена браузер не даёт: сказать,
          // что делать руками, лучше, чем молча не скопировать.
          .catch(() => toast.error('Браузер не дал скопировать — выделите текст и скопируйте вручную.'));
        if (!navigator.clipboard) toast.error('Браузер не дал скопировать — выделите текст и скопируйте вручную.');
      }}
    >
      <Icon name="copy" size={16} />{label}
    </button>
  );
}

/**
 * Числовое поле, которое можно очистить и набрать заново.
 *
 * Раньше пустое поле тут же превращалось в 0, и чтобы поменять «60» на «90»,
 * приходилось воевать с ведущим нулём. Здесь поле хранит набранный текст, а
 * наверх отдаёт число только когда текст — число.
 */
export function NumberInput({ id, value, onChange, min, max, disabled, className = 'input num', ariaLabel }: {
  id?: string; value: number; onChange: (n: number) => void; min?: number; max?: number;
  disabled?: boolean; className?: string; ariaLabel?: string;
}): JSX.Element {
  const [text, setText] = useState(String(value));
  useEffect(() => {
    // Внешнее значение сменилось (сброс черновика, загрузка) — показываем его.
    if (Number(text) !== value) setText(String(value));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value]);
  const clamp = (n: number): number => Math.min(max ?? Infinity, Math.max(min ?? -Infinity, n));
  return (
    <input
      id={id}
      className={className}
      type="number"
      inputMode="numeric"
      value={text}
      min={min}
      max={max}
      disabled={disabled}
      aria-label={ariaLabel}
      onChange={(e) => {
        setText(e.target.value);
        const n = e.target.valueAsNumber;
        if (Number.isFinite(n)) onChange(clamp(Math.trunc(n)));
      }}
      onBlur={() => {
        const n = Number(text);
        if (text.trim() === '' || !Number.isFinite(n)) setText(String(value));
        else if (clamp(Math.trunc(n)) !== n) setText(String(clamp(Math.trunc(n))));
      }}
    />
  );
}
