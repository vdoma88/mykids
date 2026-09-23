import type { JSX } from 'react';
import { useState } from 'react';
import { api } from '../../api.js';
import { CardSkeleton, ErrorBox, useAsync } from '../../ui/Async.js';
import { Card, Empty, Tabs } from '../../ui/kit.js';
import { LedgerTable } from './LedgerTable.js';

type Filter = 'all' | 'minutes' | 'credits';

/** Журнал операций целиком: только дополняется, и по нему видно всё. */
export function LedgerTab({ childId }: { childId: string }): JSX.Element {
  const [limit, setLimit] = useState(100);
  const [filter, setFilter] = useState<Filter>('all');
  const ledgerQ = useAsync(() => api.ledger(childId, limit), [childId, limit]);

  if (!ledgerQ.data) return ledgerQ.error ? <ErrorBox message={ledgerQ.error} /> : <CardSkeleton lines={8} />;
  const all = ledgerQ.data.entries;
  const shown = filter === 'all' ? all : all.filter((e) => e.currency === filter);

  return (
    <Card
      title="Журнал операций"
      desc="Балансы считаются из этого журнала. Записи не меняются и не удаляются — ошибка исправляется новой записью."
      actions={
        <Tabs<Filter>
          label="Что показывать"
          value={filter}
          onChange={setFilter}
          tabs={[{ key: 'all', label: 'Всё' }, { key: 'minutes', label: 'Минуты' }, { key: 'credits', label: 'Кредиты' }]}
        />
      }
    >
      <ErrorBox message={ledgerQ.error} />
      {shown.length === 0
        ? <Empty icon="list" title="Пока пусто">Здесь появятся выдачи, задания, обмены и покупки.</Empty>
        : <LedgerTable entries={shown} />}
      {all.length >= limit && limit < 500 && (
        <div className="row" style={{ justifyContent: 'center', marginTop: 14 }}>
          <button type="button" className="btn secondary sm" onClick={() => setLimit((l) => Math.min(500, l + 100))}>
            Показать ещё
          </button>
        </div>
      )}
    </Card>
  );
}
