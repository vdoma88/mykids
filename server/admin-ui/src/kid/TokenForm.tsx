import type { JSX } from 'react';
import { useState, type FormEvent } from 'react';
import { deviceToken } from '../api.js';
import { Icon } from '../ui/Icon.js';
import { Alert, Field } from '../ui/kit.js';

export function TokenForm({ onSaved, invalid = false }: { onSaved: () => void; invalid?: boolean }): JSX.Element {
  const [token, setToken] = useState('');
  return (
    <div className="auth-screen">
      <div className="auth-card">
        <span className="brand"><span className="brand-mark"><Icon name="brand" /></span>MyKids</span>
        <form className="card stack" onSubmit={(e: FormEvent) => {
          e.preventDefault();
          deviceToken.set(token.trim());
          onSaved();
        }}>
          <div>
            <h1>Подключить устройство</h1>
            <p className="muted small" style={{ marginTop: 4 }}>
              Токен выдаёт родитель в своей админке, во вкладке «Устройства».
            </p>
          </div>
          {invalid && <Alert tone="danger">Токен не подошёл или его отозвали — попроси новый.</Alert>}
          <Field label="Токен устройства" htmlFor="devtok">
            <input id="devtok" className="input mono" value={token} required autoComplete="off" spellCheck={false}
                   onChange={(e) => setToken(e.target.value)} />
          </Field>
          <button type="submit" className="btn lg block" disabled={!token.trim()}>Подключить</button>
        </form>
      </div>
    </div>
  );
}
