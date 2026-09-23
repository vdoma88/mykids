import type { JSX } from 'react';
import { useCallback, useEffect, useRef, useState } from 'react';
import { ApiError } from '../api.js';
import { Icon } from './Icon.js';

export interface AsyncState<T> {
  data: T | null;
  error: string | null;
  /** Код ответа, если ошибка пришла от API. Сравнивать нужно его, а не текст. */
  status: number | null;
  /** Первая загрузка: данных ещё нет. Перезагрузка поверх данных — не loading. */
  loading: boolean;
  reload: () => void;
}

/**
 * Загрузка данных с явными состояниями: ошибку нельзя проглотить.
 *
 * `enabled: false` не просто прячет результат, а вообще не делает запрос —
 * иначе компонент, которому ещё нечем авторизоваться, успевает получить 401
 * и принять его за отказ в доступе.
 *
 * Перезагрузка не прячет уже показанные данные: раньше после каждого
 * действия страница мигала «Загрузка…» и прыгала вверх.
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
  const loadRef = useRef(load);
  loadRef.current = load;

  const reload = useCallback(() => { setTick((t) => t + 1); }, []);

  useEffect(() => {
    if (!enabled) { setLoading(false); return; }
    let alive = true;
    if (data === null) setLoading(true);
    loadRef.current()
      .then((d) => { if (alive) { setData(d); setError(null); setStatus(null); } })
      .catch((e: unknown) => {
        if (!alive) return;
        setError(errorText(e));
        setStatus(e instanceof ApiError ? e.status : null);
      })
      .finally(() => { if (alive) setLoading(false); });
    return () => { alive = false; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, tick, enabled]);

  return { data, error, status, loading, reload };
}

/** Текст ошибки для человека: сетевой сбой — не «TypeError: Failed to fetch». */
export function errorText(e: unknown): string {
  if (e instanceof ApiError) return e.message;
  if (e instanceof TypeError) return 'Нет связи с сервером. Проверьте, что он запущен и доступен.';
  return e instanceof Error ? e.message : String(e);
}

export function ErrorBox({ message }: { message: string | null }): JSX.Element | null {
  if (!message) return null;
  return (
    <div className="alert danger" role="alert">
      <Icon name="alert" />
      <div>{message}</div>
    </div>
  );
}

export function Skeleton({ height = 18, width = '100%' }: { height?: number; width?: number | string }): JSX.Element {
  return <div className="skeleton" style={{ height, width }} aria-hidden />;
}

/** Заглушка на время первой загрузки: форма содержимого, а не слово «Загрузка…». */
export function CardSkeleton({ lines = 3 }: { lines?: number }): JSX.Element {
  return (
    <div className="card stack" aria-busy="true" aria-label="Загрузка" style={{ ['--gap' as string]: '12px' }}>
      <Skeleton height={20} width="40%" />
      {Array.from({ length: lines }, (_, i) => <Skeleton key={i} width={`${90 - i * 12}%`} />)}
    </div>
  );
}
