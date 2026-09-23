import type { JSX, ReactNode } from 'react';
import { createContext, useCallback, useContext, useMemo, useRef, useState } from 'react';
import { Icon } from './Icon.js';

type Tone = 'success' | 'danger' | 'info';
interface ToastItem { id: number; tone: Tone; text: string }

interface ToastApi {
  success: (text: string) => void;
  error: (text: string) => void;
  info: (text: string) => void;
}

const Ctx = createContext<ToastApi | null>(null);

/**
 * Короткие сообщения о результате действия.
 *
 * Для итогов, которые не привязаны к одному месту на странице: «товар
 * выключен», «токен скопирован». Ошибки заполнения форм показываются у самих
 * форм — сообщение, исчезающее через пять секунд, для них не годится.
 */
export function ToastProvider({ children }: { children: ReactNode }): JSX.Element {
  const [items, setItems] = useState<ToastItem[]>([]);
  const seq = useRef(0);

  const dismiss = useCallback((id: number) => {
    setItems((all) => all.filter((t) => t.id !== id));
  }, []);

  const push = useCallback((tone: Tone, text: string) => {
    const id = ++seq.current;
    setItems((all) => [...all.slice(-3), { id, tone, text }]);
    window.setTimeout(() => dismiss(id), tone === 'danger' ? 8000 : 4500);
  }, [dismiss]);

  const api = useMemo<ToastApi>(() => ({
    success: (t) => push('success', t),
    error: (t) => push('danger', t),
    info: (t) => push('info', t),
  }), [push]);

  return (
    <Ctx.Provider value={api}>
      {children}
      <div className="toasts" role="status" aria-live="polite">
        {items.map((t) => (
          <div key={t.id} className={`toast ${t.tone}`}>
            <Icon name={t.tone === 'success' ? 'check' : t.tone === 'danger' ? 'alert' : 'info'} />
            <div className="grow">{t.text}</div>
            <button type="button" aria-label="Закрыть" onClick={() => dismiss(t.id)}>
              <Icon name="x" size={16} />
            </button>
          </div>
        ))}
      </div>
    </Ctx.Provider>
  );
}

export function useToast(): ToastApi {
  const api = useContext(Ctx);
  if (!api) throw new Error('useToast вне ToastProvider');
  return api;
}
