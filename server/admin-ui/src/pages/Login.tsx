import type { JSX } from 'react';
import { useState, type FormEvent } from 'react';
import { api, parentToken, ApiError, type Me } from '../api.js';

export function LoginPage({ onSignedIn }: { onSignedIn: (me: Me) => void }): JSX.Element {
  const [mode, setMode] = useState<'login' | 'register'>('login');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
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
        setError('Введите код из приложения-аутентификатора.');
      } else {
        setError(err instanceof ApiError ? err.message : String(err));
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="auth">
      <h2>MyKids</h2>
      <p className="sub">
        {mode === 'login' ? 'Вход для родителя' : 'Создание семьи'}
      </p>
      {error && <div className="err" role="alert">{error}</div>}
      <form className="card" onSubmit={(e) => void submit(e)}>
        {mode === 'register' && (
          <div className="field" style={{ marginBottom: 12 }}>
            <label htmlFor="familyName">Название семьи</label>
            <input id="familyName" value={familyName} required
                   onChange={(e) => setFamilyName(e.target.value)} />
          </div>
        )}
        <div className="field" style={{ marginBottom: 12 }}>
          <label htmlFor="email">Электронная почта</label>
          <input id="email" type="email" autoComplete="username" value={email} required
                 onChange={(e) => setEmail(e.target.value)} />
        </div>
        <div className="field" style={{ marginBottom: 12 }}>
          <label htmlFor="password">Пароль</label>
          <input id="password" type="password" value={password} required
                 autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
                 onChange={(e) => setPassword(e.target.value)} />
          {mode === 'register' && <span className="note">Не короче 10 символов.</span>}
        </div>
        {needsTotp && (
          <div className="field" style={{ marginBottom: 12 }}>
            <label htmlFor="totp">Код подтверждения</label>
            <input id="totp" inputMode="numeric" value={totp} onChange={(e) => setTotp(e.target.value)} />
          </div>
        )}
        <button type="submit" disabled={busy}>
          {busy ? 'Минуту…' : mode === 'login' ? 'Войти' : 'Создать семью'}
        </button>
      </form>
      <p className="note">
        {mode === 'login' ? 'Ещё нет аккаунта? ' : 'Уже есть аккаунт? '}
        <button className="ghost" style={{ padding: '2px 6px' }}
                onClick={() => { setMode(mode === 'login' ? 'register' : 'login'); setError(null); }}>
          {mode === 'login' ? 'Создать семью' : 'Войти'}
        </button>
      </p>
    </div>
  );
}
