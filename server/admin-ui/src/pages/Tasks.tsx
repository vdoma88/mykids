import type { JSX } from 'react';
import { useEffect, useRef, useState } from 'react';
import type { TaskItem, TaskPack } from '@mykids/contracts';
import { listPacks, loadPack, renderItem, type PackSummary, type RenderedItem }
  from '@mykids/task-runner';
import { api, ApiError } from '../api.js';

/**
 * Задания ребёнка.
 *
 * До сих пор их не было нигде: интерфейс показывал баланс, обмен и магазин, а
 * заработать кредиты было негде вовсе — вся экономика держалась на ручных
 * начислениях родителя.
 *
 * Проверяет ответ браузер, а начисляет сервер. Это не небрежность, а принятая
 * цена: правильные ответы всё равно попадают на устройство ребёнка, потому что
 * задания решаются там. Сервер ограничивает не честность, а суммы — дневной
 * потолок пакета, общий дневной потолок и cooldown на повтор задания. Обойти
 * проверку можно, получить больше дневного потолка — нет.
 */

/** Откуда браузер берёт пакеты. Тот же загрузчик, что и у локального прогона. */
const CONTENT = '/content/packs';

interface Props {
  /** Пересчитать баланс на странице после начисления. */
  onEarned: () => void;
}

export function Tasks({ onEarned }: Props): JSX.Element {
  const [packs, setPacks] = useState<PackSummary[] | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [open, setOpen] = useState<TaskPack | null>(null);

  useEffect(() => {
    let alive = true;
    void (async () => {
      try {
        // Назначенные — с сервера, описания — из каталога пакетов: первое
        // про этого ребёнка, второе одинаково для всех.
        const [assigned, all] = await Promise.all([api.childPacks(), listPacks(CONTENT)]);
        const mine = all.filter((p: PackSummary) => assigned.packs.includes(p.id));
        if (alive) setPacks(mine);
      } catch (e) {
        if (alive) setErr(e instanceof ApiError ? e.message : String(e));
      }
    })();
    return () => { alive = false; };
  }, []);

  if (err) return <section><h3>Задания</h3><p className="err">{err}</p></section>;
  if (!packs) return <section><h3>Задания</h3><p>Загрузка…</p></section>;

  if (open) {
    return (
      <Session
        pack={open}
        onDone={() => { setOpen(null); onEarned(); }}
      />
    );
  }

  if (packs.length === 0) {
    return (
      <section>
        <h3>Задания</h3>
        <p>Пакетов пока не назначено. Это к родителям — не к тебе.</p>
      </section>
    );
  }

  return (
    <section>
      <h3>Задания</h3>
      <p className="hint">Решай — получаешь кредиты, кредиты меняются на время.</p>
      <ul className="packs">
        {packs.map((p) => (
          <li key={p.id} data-testid={`pack-${p.id}`}>
            <b>{p.title}</b>
            {p.description && <div className="muted">{p.description}</div>}
            <button onClick={() => void openPack(p.id, setOpen, setErr)}>Решать</button>
          </li>
        ))}
      </ul>
    </section>
  );
}

async function openPack(
  id: string,
  setOpen: (p: TaskPack) => void,
  setErr: (e: string) => void,
): Promise<void> {
  try {
    setOpen(await loadPack(CONTENT, id));
  } catch (e) {
    setErr(String(e));
  }
}

/** Прохождение одного пакета: задание за заданием. */
function Session({ pack, onDone }: { pack: TaskPack; onDone: () => void }): JSX.Element {
  const [index, setIndex] = useState(0);
  const [earned, setEarned] = useState(0);
  const [note, setNote] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const host = useRef<HTMLDivElement>(null);
  const rendered = useRef<RenderedItem | null>(null);
  const item: TaskItem | undefined = pack.items[index];

  useEffect(() => {
    if (!host.current || !item) return;
    rendered.current = renderItem(host.current, item);
    return () => {
      rendered.current?.dispose();
      rendered.current = null;
    };
  }, [item]);

  if (!item) {
    return (
      <section>
        <h3>{pack.manifest.title}</h3>
        <p data-testid="session-done">
          Готово. Заработано кредитов: <b data-testid="session-credits">{earned}</b>
        </p>
        <button onClick={onDone}>Вернуться</button>
      </section>
    );
  }

  async function submit(): Promise<void> {
    const answer = rendered.current?.collect();
    if (!answer) {
      setNote('Сначала ответь.');
      return;
    }
    setBusy(true);
    setNote(null);
    try {
      // Оценку считаем здесь, сумму — на сервере. Поэтому и score, и
      // параметры пакета уходят вместе: сервер применяет к ним потолки.
      const { grade } = await import('@mykids/task-runner');
      const g = grade(item!, answer);
      const res = await api.childAttempt({
        packId: pack.manifest.id,
        itemId: item!.id,
        score: g.score,
        baseCredits: g.score > 0 ? pack.manifest.reward.creditsPerCorrect : 0,
        packDailyCreditCap: pack.manifest.reward.dailyCreditCap,
        ...(pack.manifest.delivery?.cooldownHoursPerItem !== undefined
          ? { cooldownHours: pack.manifest.delivery.cooldownHoursPerItem }
          : {}),
      });
      setEarned((c) => c + res.credits);
      // Почему кредитов нет, говорим прямо: «решил, а ничего не дали» без
      // объяснения читается как обман.
      // Пояснение про повтор идёт вместе с начислением, а не вместо него:
      // «+5 кредитов» без причины, когда вчера было 10, читается как ошибка.
      setNote(res.withheldReason ?? (res.credits > 0
        ? [`Верно. +${res.credits} кредитов`, res.note].filter(Boolean).join(' ')
        : 'Не засчитано'));
      setIndex((i) => i + 1);
    } catch (e) {
      setNote(e instanceof ApiError ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section>
      <h3>{pack.manifest.title}</h3>
      <p className="muted">Задание {index + 1} из {pack.items.length}</p>
      <div ref={host} data-testid="task-host" />
      {note && <p data-testid="task-note">{note}</p>}
      <button disabled={busy} onClick={() => void submit()} data-testid="task-submit">
        Ответить
      </button>
      <button className="ghost" onClick={onDone}>Выйти</button>
    </section>
  );
}
