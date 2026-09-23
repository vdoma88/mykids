import type { JSX } from 'react';
import { useState, type FormEvent } from 'react';
import { api, parentToken, ApiError, type Me } from '../api.js';
import { errorText } from '../ui/Async.js';
import { Icon } from '../ui/Icon.js';
import { Alert, Field, Tabs } from '../ui/kit.js';

type Mode = 'login' | 'register';

export function LoginPage({ onSignedIn }: { onSignedIn: (me: Me) => void }): JSX.Element {
  const [mode, setMode] = useState<Mode>('login');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [familyName, setFamilyName] = useState('');
  const [totp, setTotp] = useState('');
  const [needsTotp, setNeedsTotp] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent): Promise<void> {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const result = mode === 'login'
        ? await api.login({ email, password, ...(totp ? { totp } : {}) })
        : await api.register({ familyName, email, password });
      parentToken.set(result.token);
      onSignedIn(await api.me());
    } catch (err) {
      if (err instanceof ApiError && err.code === 'totp_required') {
        setNeedsTotp(true);
        setError(null);
      } else {
        setError(errorText(err));
      }
    } finally {
      setBusy(false);
    }
  }

  const short = mode === 'register' && password.length > 0 && password.length < 10;

  return (
    <div className="auth-screen">
      <div className="auth-card">
        <span className="brand"><span className="brand-mark"><Icon name="brand" /></span>MyKids</span>

        <form className="card stack" onSubmit={(e) => void submit(e)} noValidate={false}>
          <div>
            <h1>{mode === 'login' ? 'Вход для родителя' : 'Новая семья'}</h1>
            <p className="muted small" style={{ marginTop: 4 }}>
              {mode === 'login'
                ? 'Правила экрана, задания и магазин наград.'
                : 'Аккаунт владельца. Второго родителя добавите потом в настройках.'}
            </p>
          </div>

          <Tabs<Mode>
            label="Режим"
            segmented
            value={mode}
            onChange={(m) => { setMode(m); setError(null); setNeedsTotp(false); }}
            tabs={[{ key: 'login', label: 'Вход' }, { key: 'register', label: 'Новая семья' }]}
          />

          {error && <Alert tone="danger">{error}</Alert>}
          {needsTotp && <Alert tone="info" icon="shield">Введите шестизначный код из приложения-аутентификатора.</Alert>}

          {mode === 'register' && (
            <Field label="Название семьи" htmlFor="familyName">
              <input id="familyName" className="input" value={familyName} required maxLength={80}
                     placeholder="Ивановы" autoComplete="organization"
                     onChange={(e) => setFamilyName(e.target.value)} />
            </Field>
          )}
          <Field label="Электронная почта" htmlFor="email">
            <input id="email" className="input" type="email" autoComplete="username" value={email} required
                   inputMode="email" onChange={(e) => setEmail(e.target.value)} />
          </Field>
          <Field
            label="Пароль"
            htmlFor="password"
            hint={mode === 'register' ? 'Не короче 10 символов.' : undefined}
            error={short ? `Ещё ${10 - password.length} — нужно не короче 10 символов.` : null}
          >
            <div className="input-group">
              <input id="password" className="input" type={showPassword ? 'text' : 'password'} value={password}
                     required minLength={mode === 'register' ? 10 : 1}
                     autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
                     aria-invalid={short || undefined}
                     onChange={(e) => setPassword(e.target.value)} />
              <button type="button" className="btn ghost sm icon-only"
                      aria-label={showPassword ? 'Скрыть пароль' : 'Показать пароль'}
                      onClick={() => setShowPassword((v) => !v)}>
                <Icon name={showPassword ? 'eyeOff' : 'eye'} size={18} />
              </button>
            </div>
          </Field>
          {needsTotp && (
            <Field label="Код подтверждения" htmlFor="totp">
              <input id="totp" className="input num" inputMode="numeric" autoComplete="one-time-code"
                     maxLength={6} value={totp} autoFocus
                     onChange={(e) => setTotp(e.target.value.replace(/\D/g, ''))} />
            </Field>
          )}
          <button type="submit" className="btn lg block" disabled={busy || short}>
            {busy ? 'Минуту…' : mode === 'login' ? 'Войти' : 'Создать семью'}
          </button>
        </form>

        <p className="auth-foot">
          Данные хранятся на вашем сервере и никуда не уходят.
        </p>
      </div>
    </div>
  );
}
