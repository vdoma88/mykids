import type { JSX } from 'react';
import { useEffect, useRef, useState } from 'react';
import type { TaskItem, TaskPack } from '@mykids/contracts';
import { listPacks, loadPack, renderItem, type PackSummary, type RenderedItem } from '@mykids/task-runner';
import { api } from '../api.js';
import { credits, tasksWord } from '../lib/format.js';
import { SUBJECTS } from '../lib/labels.js';
import { ErrorBox, Skeleton, errorText } from '../ui/Async.js';
import { Icon } from '../ui/Icon.js';
import { Badge, Card, Empty } from '../ui/kit.js';

/** Откуда браузер берёт пакеты. Тот же загрузчик, что и у локального прогона. */
const CONTENT = '/content/packs';

type Result =
  | { kind: 'good'; text: string }
  | { kind: 'wait'; text: string }
  | { kind: 'miss'; text: string };

/**
 * Прохождение одного пакета: задание за заданием.
 *
 * Ответ проверяет сервер — у него лежит пакет, и правильный ответ вместе с
 * ним. Страница показывает его вердикт, а не свой: две оценки на одно
 * задание — это две версии правды.
 *
 * После ответа задание остаётся на экране с результатом, а дальше ребёнок
 * идёт сам. Раньше следующее задание подменяло предыдущее в ту же секунду,
 * и «Не то» относилось уже к вопросу, которого на экране нет.
 */
function Session({ pack, onDone, onEarned }: { pack: TaskPack; onDone: () => void; onEarned: () => void }): JSX.Element {
  const [index, setIndex] = useState(0);
  const [earned, setEarned] = useState(0);
  const [result, setResult] = useState<Result | null>(null);
  const [hint, setHint] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const host = useRef<HTMLDivElement>(null);
  const rendered = useRef<RenderedItem | null>(null);
  const item: TaskItem | undefined = pack.items[index];
  const total = pack.items.length;

  useEffect(() => {
    if (!host.current || !item) return;
    rendered.current = renderItem(host.current, item);
    host.current.querySelector<HTMLElement>('input, textarea, select, button')?.focus({ preventScroll: true });
    return () => {
      rendered.current?.dispose();
      rendered.current = null;
    };
  }, [item]);

  if (!item) {
    return (
      <Card>
        <div className="empty" data-testid="session-done">
          <div className="empty-icon" style={{ background: 'var(--success-soft)', color: 'var(--success)' }}>
            <Icon name="check" />
          </div>
          <h3>{pack.manifest.title}: пройдено</h3>
          <p>Заработано: <b data-testid="session-credits">{credits(earned)}</b>.</p>
          <button type="button" className="btn" onClick={onDone}>К заданиям</button>
        </div>
      </Card>
    );
  }

  async function submit(): Promise<void> {
    const answer = rendered.current?.collect();
    if (!answer) {
      setHint('Сначала ответь — поле пустое.');
      return;
    }
    setBusy(true);
    setHint(null);
    try {
      const res = await api.childAttempt({ packId: pack.manifest.id, itemId: item!.id, answer });
      setEarned((c) => c + res.credits);
      if (res.credits > 0) onEarned();
      // Почему кредитов нет, говорим прямо: «решил, а ничего не дали» без
      // объяснения читается как обман. Пояснение про повтор идёт вместе с
      // начислением: «+5» без причины, когда вчера было 10, читается как ошибка.
      if (res.withheldReason) setResult({ kind: 'miss', text: res.withheldReason });
      else if (res.pendingApproval) setResult({ kind: 'wait', text: 'Отправлено родителю. Кредиты придут, когда он подтвердит.' });
      else if (res.credits > 0) setResult({ kind: 'good', text: [`Верно. +${credits(res.credits)}`, res.note].filter(Boolean).join('. ') });
      else setResult({ kind: 'miss', text: res.feedback ?? 'Не засчитано.' });
    } catch (e) {
      setHint(errorText(e));
    } finally {
      setBusy(false);
    }
  }

  function next(): void {
    setResult(null);
    setHint(null);
    setIndex((i) => i + 1);
  }

  return (
    <Card>
      <div className="session-head">
        <button type="button" className="btn ghost icon-only" aria-label="Выйти из пакета" onClick={onDone}>
          <Icon name="chevronLeft" />
        </button>
        <div className="grow">
          <div className="row between">
            <strong className="truncate">{pack.manifest.title}</strong>
            <span className="small muted num">Задание {index + 1} из {total}</span>
          </div>
          <div className="progress" role="progressbar" aria-valuemin={0} aria-valuemax={total} aria-valuenow={index}>
            <span style={{ width: `${(index / total) * 100}%` }} />
          </div>
        </div>
      </div>

      <div
        ref={host}
        className="task-host"
        data-testid="task-host"
        inert={result ? true : undefined}
        onKeyDown={(e) => {
          // Enter в поле ответа — то же, что «Ответить»: тянуться к мыши незачем.
          if (e.key === 'Enter' && !result && (e.target as HTMLElement).tagName === 'INPUT') {
            e.preventDefault();
            void submit();
          }
        }}
      />

      {hint && <div style={{ marginTop: 14 }}><ErrorBox message={hint} /></div>}

      {result && (
        <div className={`task-result ${result.kind}`} data-testid="task-note" role="status">
          <Icon name={result.kind === 'good' ? 'check' : result.kind === 'wait' ? 'clock' : 'info'} />
          <span className="msg">{result.text}</span>
        </div>
      )}

      <div className="task-actions">
        {result ? (
          <button type="button" className="btn lg" onClick={next} data-testid="task-next" autoFocus>
            {index + 1 < total ? 'Дальше' : 'Завершить'}<Icon name="arrowRight" />
          </button>
        ) : (
          <button type="button" className="btn lg" disabled={busy} onClick={() => void submit()} data-testid="task-submit">
            {busy ? 'Проверяю…' : 'Ответить'}
          </button>
        )}
        {earned > 0 && <span className="row small muted" style={{ marginLeft: 'auto' }}>За пакет: +{credits(earned)}</span>}
      </div>
    </Card>
  );
}

export function KidTasks({ onEarned }: { onEarned: () => void }): JSX.Element {
  const [packs, setPacks] = useState<PackSummary[] | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [open, setOpen] = useState<TaskPack | null>(null);
  const [opening, setOpening] = useState<string | null>(null);

  useEffect(() => {
    let alive = true;
    void (async () => {
      try {
        // Назначенные — с сервера, описания — из каталога пакетов: первое
        // про этого ребёнка, второе одинаково для всех.
        const [assigned, all] = await Promise.all([api.childPacks(), listPacks(CONTENT)]);
        if (alive) setPacks(all.filter((p: PackSummary) => assigned.packs.includes(p.id)));
      } catch (e) {
        if (alive) setErr(errorText(e));
      }
    })();
    return () => { alive = false; };
  }, []);

  async function openPack(id: string): Promise<void> {
    setOpening(id);
    try {
      setOpen(await loadPack(CONTENT, id));
    } catch (e) {
      setErr(errorText(e));
    } finally {
      setOpening(null);
    }
  }

  if (open) return <Session pack={open} onDone={() => setOpen(null)} onEarned={onEarned} />;
  if (err) return <ErrorBox message={err} />;
  if (!packs) {
    return <div className="grid cards"><div className="card"><Skeleton height={90} /></div><div className="card"><Skeleton height={90} /></div></div>;
  }
  if (packs.length === 0) {
    return (
      <Card><Empty icon="book" title="Заданий пока нет">Пакеты назначает родитель — напомни ему, если ждёшь.</Empty></Card>
    );
  }

  return (
    <div className="grid cards">
      {packs.map((p) => (
        <div className="card pack-card" key={p.id} data-testid={`pack-${p.id}`}>
          <div className="row between">
            <Badge tone="accent">{SUBJECTS[p.subject] ?? p.subject}</Badge>
            <span className="small muted">{p.itemCount} {tasksWord(p.itemCount)}</span>
          </div>
          <h2>{p.title}</h2>
          {p.description && <p className="pack-desc">{p.description}</p>}
          <div className="pack-foot">
            <span />
            <button type="button" className="btn" disabled={opening !== null} onClick={() => void openPack(p.id)}>
              <Icon name="play" size={16} />{opening === p.id ? 'Открываю…' : 'Решать'}
            </button>
          </div>
        </div>
      ))}
    </div>
  );
}
