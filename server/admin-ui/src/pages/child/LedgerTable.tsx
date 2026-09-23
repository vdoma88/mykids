import type { JSX } from 'react';
import type { LedgerRow } from '../../api.js';
import { dateTime, num } from '../../lib/format.js';
import { REASONS } from '../../lib/labels.js';

/** Строки журнала. Сумма со знаком и цветом: «+» — пришло, «−» — ушло. */
export function LedgerTable({ entries }: { entries: LedgerRow[] }): JSX.Element {
  return (
    <div className="table-wrap">
      <table className="table">
        <thead>
          <tr><th>Операция</th><th className="num">Сумма</th></tr>
        </thead>
        <tbody>
          {/* Две колонки, дата — под описанием: в три колонки на телефоне
              сумма уезжала за край, а она здесь главное. */}
          {entries.map((e) => (
            <tr key={e.id}>
              <td>
                {REASONS[e.reason] ?? e.reason}
                {e.note && <span className="muted"> — {e.note}</span>}
                <div className="tiny subtle">{dateTime(e.recordedAt)}</div>
              </td>
              <td className={`num nowrap ${e.amount < 0 ? 'neg' : 'pos'}`} style={{ fontWeight: 650 }}>
                {e.amount > 0 ? '+' : e.amount < 0 ? '−' : ''}{num(Math.abs(e.amount))}{' '}
                <span className="muted small">{e.currency === 'minutes' ? 'мин' : 'кр.'}</span>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
