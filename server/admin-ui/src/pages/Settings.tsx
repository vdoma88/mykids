import type { JSX } from 'react';
import { useState, type FormEvent } from 'react';
import { api, type Me } from '../api.js';
import { ROLES } from '../lib/labels.js';
import { applyTheme, readTheme, type ThemeChoice } from '../lib/theme.js';
import { ErrorBox, errorText } from '../ui/Async.js';
import { Icon } from '../ui/Icon.js';
import { Alert, Avatar, Badge, Card, CopyButton, Field, PageHead, Tabs } from '../ui/kit.js';
import { useToast } from '../ui/Toast.js';

/** Секрет группами по четыре: переписать его в приложение руками проще. */
const groups = (s: string): string => s.replace(/(.{4})/g, '$1 ').trim();

/**
 * Второй фактор.
 *
 * Интерфейс давно напоминал «второй фактор выключен», а включить его было
 * негде: маршруты на сервере были, страницы — нет.
 */
function TwoFactor({ me, onChanged }: { me: Me; onChanged: (me: Me) => void }): JSX.Element {
  const [setup, setSetup] = useState<{ secret: string; uri: string } | null>(null);
  const [code, setCode] = useState('');
  const [password, setPassword] = useState('');
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const toast = useToast();

  async function act(fn: () => Promise<void>): Promise<void> {
    setErr(null);
    setBusy(true);
    try { await fn(); } catch (e) { setErr(errorText(e)); } finally { setBusy(false); }
  }

  if (me.totpEnabled) {
    return (
      <Card title={<span className="row nowrap" style={{ ['--gap' as string]: '10px' }}>Второй фактор <Badge tone="success" dot>включён</Badge></span>}
            desc="При входе, кроме пароля, нужен код из приложения-аутентификатора.">
        <form className="row top" onSubmit={(e: FormEvent) => {
          e.preventDefault();
          void act(async () => {
            await api.totpDisable(password);
            setPassword('');
            onChanged(await api.me());
            toast.success('Второй фактор выключен.');
          });
        }}>
          <Field label="Пароль, чтобы выключить" htmlFor="totp-off-pass" className="grow" error={err}>
            <input id="totp-off-pass" className="input" type="password" autoComplete="current-password"
                   value={password} onChange={(e) => setPassword(e.target.value)} />
          </Field>
          <button type="submit" className="btn danger-ghost" style={{ marginTop: 26 }} disabled={busy || !password}>Выключить</button>
        </form>
      </Card>
    );
  }

  return (
    <Card
      title={<span className="row nowrap" style={{ ['--gap' as string]: '10px' }}>Второй фактор <Badge tone="warning" dot>выключен</Badge></span>}
      desc="Без него пароля достаточно, чтобы войти и поменять правила. Подросток, подсмотревший пароль, — ровно тот случай."
    >
      <div className="stack">
        <ErrorBox message={err} />
        {!setup ? (
          <div>
            <button type="button" className="btn" disabled={busy} onClick={() => void act(async () => setSetup(await api.totpBegin()))}>
              <Icon name="shield" />Включить
            </button>
          </div>
        ) : (
          <>
            <ol className="small" style={{ margin: 0, paddingLeft: 18, display: 'grid', gap: 6 }}>
              <li>Откройте приложение-аутентификатор (Google Authenticator, Яндекс Ключ, Aegis и т. п.).</li>
              <li>Добавьте аккаунт вручную и введите этот ключ — или откройте ссылку на телефоне.</li>
              <li>Впишите шестизначный код, который покажет приложение.</li>
            </ol>
            <div className="code-box">
              <span className="mono" style={{ letterSpacing: '.08em' }}>{groups(setup.secret)}</span>
              <CopyButton text={setup.secret} label="Ключ" />
            </div>
            <a className="small" href={setup.uri}>Открыть в приложении на этом устройстве</a>
            <form className="row top" onSubmit={(e: FormEvent) => {
              e.preventDefault();
              void act(async () => {
                await api.totpConfirm(setup.secret, code);
                setSetup(null);
                setCode('');
                onChanged(await api.me());
                toast.success('Второй фактор включён.');
              });
            }}>
              <Field label="Код из приложения" htmlFor="totp-code" className="grow">
                <input id="totp-code" className="input num" inputMode="numeric" autoComplete="one-time-code"
                       maxLength={6} value={code} onChange={(e) => setCode(e.target.value.replace(/\D/g, ''))} />
              </Field>
              <button type="submit" className="btn" style={{ marginTop: 26 }} disabled={busy || code.length !== 6}>Подтвердить</button>
              <button type="button" className="btn ghost" style={{ marginTop: 26 }} onClick={() => { setSetup(null); setErr(null); }}>Отмена</button>
            </form>
          </>
        )}
      </div>
    </Card>
  );
}

/** Второй родитель или наблюдатель. Только для владельца. */
function Guardians(): JSX.Element {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [role, setRole] = useState<'parent' | 'viewer'>('parent');
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const toast = useToast();

  async function submit(e: FormEvent): Promise<void> {
    e.preventDefault();
    setErr(null);
    setBusy(true);
    try {
      await api.addGuardian({ email: email.trim(), password, role });
      toast.success(`${email.trim()} может входить в семью.`);
      setEmail('');
      setPassword('');
    } catch (e2) {
      setErr(errorText(e2));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card title="Добавить взрослого" desc="Второй родитель правит правила наравне с вами; наблюдатель только смотрит.">
      <form className="stack" onSubmit={(e) => void submit(e)}>
        <ErrorBox message={err} />
        <Tabs<'parent' | 'viewer'>
          label="Права"
          segmented
          value={role}
          onChange={setRole}
          tabs={[{ key: 'parent', label: 'Родитель' }, { key: 'viewer', label: 'Только чтение' }]}
        />
        <div className="grid two">
          <Field label="Почта" htmlFor="g-email">
            <input id="g-email" className="input" type="email" autoComplete="off" required value={email}
                   onChange={(e) => setEmail(e.target.value)} />
          </Field>
          <Field label="Временный пароль" htmlFor="g-pass" hint="Не короче 10 символов. Передайте его лично.">
            <input id="g-pass" className="input" type="text" autoComplete="new-password" required minLength={10}
                   value={password} onChange={(e) => setPassword(e.target.value)} />
          </Field>
        </div>
        <div className="row end">
          <button type="submit" className="btn" disabled={busy || !email.trim() || password.length < 10}>
            <Icon name="userPlus" />Добавить
          </button>
        </div>
      </form>
    </Card>
  );
}

function Appearance(): JSX.Element {
  const [theme, setTheme] = useState<ThemeChoice>(readTheme);
  return (
    <Card title="Оформление" desc="Запоминается в этом браузере.">
      <Tabs<ThemeChoice>
        label="Тема"
        segmented
        value={theme}
        onChange={(t) => { applyTheme(t); setTheme(t); }}
        tabs={[
          { key: 'system', label: 'Как в системе', icon: 'monitor' },
          { key: 'light', label: 'Светлая', icon: 'sun' },
          { key: 'dark', label: 'Тёмная', icon: 'moon' },
        ]}
      />
    </Card>
  );
}

export function SettingsPage({ me, onChanged }: { me: Me; onChanged: (me: Me) => void }): JSX.Element {
  return (
    <>
      <PageHead title="Настройки" lead="Аккаунт, безопасность и оформление." />
      <Card>
        <div className="row nowrap">
          <Avatar name={me.email} large />
          <div className="grow">
            <h2 className="truncate">{me.email}</h2>
            <div className="small muted">{ROLES[me.role] ?? me.role}</div>
          </div>
        </div>
      </Card>
      {me.role === 'viewer' && <Alert tone="info">У вас права только на чтение: смотреть можно всё, менять — ничего.</Alert>}
      <TwoFactor me={me} onChanged={onChanged} />
      {me.role === 'owner' && <Guardians />}
      <Appearance />
    </>
  );
}
