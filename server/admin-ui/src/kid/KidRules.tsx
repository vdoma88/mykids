import type { JSX } from 'react';
import { localMoment } from '@mykids/domain';
import { deviceToken, type ChildMe } from '../api.js';
import { credits, minutes } from '../lib/format.js';
import { MODES, describeDays } from '../lib/labels.js';
import { Card, ConfirmButton } from '../ui/kit.js';

/**
 * Правила — открыто и полностью. Скрытая механика воспринимается как
 * несправедливость и провоцирует искать обход вместо игры по правилам.
 */
export function KidRules({ me, onDisconnect }: { me: ChildMe; onDisconnect: () => void }): JSX.Element {
  const { policy } = me;
  // День недели — по часовому поясу семьи, а не устройства: сутки на сервере
  // считаются так же, и «лимит на сегодня» должен совпадать с тем, что выдано.
  const today = localMoment(new Date(), policy.timezone).weekday;

  return (
    <>
      <Card title="Правила">
        <div className="rules">
          <div className="rule"><div className="k">Лимит на сегодня</div><div className="v">{minutes(policy.dailyLimitMinutes[today] ?? 0)}</div></div>
          <div className="rule"><div className="k">Переносится на завтра</div><div className="v">до {minutes(policy.carryOverMaxMinutes)}</div></div>
          <div className="rule"><div className="k">Курс обмена</div><div className="v">{credits(policy.economy.creditsPerMinute)} за минуту</div></div>
          <div className="rule"><div className="k">Обменом за день</div><div className="v">до {minutes(policy.economy.maxConvertedMinutesPerDay)}</div></div>
          <div className="rule"><div className="k">Заработать за день</div><div className="v">до {credits(policy.economy.maxCreditsPerDay)}</div></div>
        </div>
      </Card>

      {policy.windows.length > 0 && (
        <Card title="Расписание">
          <div className="list">
            {policy.windows.map((w, i) => (
              <div className="list-item" key={i}>
                <div className="grow">
                  <div className="title">{w.name}</div>
                  <div className="meta">{describeDays(w.days)}, {w.from}–{w.to}</div>
                </div>
                <span className={`badge ${w.mode === 'blocked' ? 'danger' : w.mode === 'tasks_only' ? 'warning' : 'success'}`}>
                  {MODES[w.mode]}
                </span>
              </div>
            ))}
          </div>
        </Card>
      )}

      <Card title="Это устройство" desc="Отключив его, придётся заново вводить токен от родителя.">
        <ConfirmButton confirm="Точно отключить?" onConfirm={() => { deviceToken.set(null); onDisconnect(); }}>
          Отключить устройство
        </ConfirmButton>
      </Card>
    </>
  );
}
