import type { JSX } from 'react';
import { useCallback, useEffect, useState } from 'react';
import { ApiError } from '../api.js';

export interface AsyncState<T> {
  data: T | null;
  error: string | null;
  /** Код ответа, если ошибка пришла от API. Сравнивать нужно его, а не текст. */
  status: number | null;
  loading: boolean;
  reload: () => void;
}

/**
 * Загрузка данных с явными состояниями: ошибку нельзя проглотить.
 *
 * `enabled: false` не просто прячет результат, а вообще не делает запрос —
 * иначе компонент, которому ещё нечем авторизоваться, успевает получить 401
 * и принять его за отказ в доступе.
 */
export function useAsync<T>(
  load: () => Promise<T>,
  deps: unknown[] = [],
  options: { enabled?: boolean } = {},
): AsyncState<T> {
  const enabled = options.enabled ?? true;
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [status, setStatus] = useState<number | null>(null);
  const [loading, setLoading] = useState(enabled);
  const [tick, setTick] = useState(0);

  const reload = useCallback(() => { setTick((t) => t + 1); }, []);

  useEffect(() => {
    if (!enabled) { setLoading(false); return; }
    let alive = true;
    setLoading(true);
    load()
      .then((d) => { if (alive) { setData(d); setError(null); setStatus(null); } })
      .catch((e: unknown) => {
        if (!alive) return;
        setError(e instanceof ApiError ? e.message : String(e));
        setStatus(e instanceof ApiError ? e.status : null);
      })
      .finally(() => { if (alive) setLoading(false); });
    return () => { alive = false; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, tick, enabled]);

  return { data, error, status, loading, reload };
}

export function ErrorBox({ message }: { message: string | null }): JSX.Element | null {
  if (!message) return null;
  return <div className="err" role="alert">{message}</div>;
}

export function Loading(): JSX.Element {
  return <p className="note">Загрузка…</p>;
}
