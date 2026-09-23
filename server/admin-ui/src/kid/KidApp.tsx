import type { JSX } from 'react';
import { useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import '@mykids/task-runner/runner.css';
import { api, deviceToken } from '../api.js';
import { ThemeToggle } from '../layout/ThemeToggle.js';
import { credits, duration, num } from '../lib/format.js';
import { CardSkeleton, ErrorBox, useAsync } from '../ui/Async.js';
import { Icon } from '../ui/Icon.js';
import { Tabs } from '../ui/kit.js';
import { KidRules } from './KidRules.js';
import { KidShop } from './KidShop.js';
import { KidTasks } from './KidTasks.js';
import { TokenForm } from './TokenForm.js';

type Section = 'tasks' | 'shop' | 'rules';
const SECTIONS: Section[] = ['tasks', 'shop', 'rules'];

/**
 * Интерфейс ребёнка.
 *
 * Для подростка: спокойно и по делу. Никаких восклицательных знаков и
 * уменьшительных слов — они читаются как разговор свысока. Главное видно
 * сразу: сколько осталось, сколько заработано и где заработать ещё.
 */
export function KidApp(): JSX.Element {
  const [hasToken, setHasToken] = useState(() => deviceToken.get() !== null);
  const [reloadKey, setReloadKey] = useState(0);
  // Раздел — параметром адреса, а не путём: всё, что глубже «/child/», сервер
  // считает запросами к API и требует токен устройства. Обновив страницу на
  // «/child/shop», ребёнок получал 401 вместо магазина.
  const [params, setParams] = useSearchParams();
  const wanted = params.get('s') as Section | null;
  const section: Section = wanted && SECTIONS.includes(wanted) ? wanted : 'tasks';

  // Без токена запрос не делаем вовсе: иначе первый 401 прилетает раньше, чем
  // ребёнок успел ввести токен.
  const me = useAsync(() => api.childMe(), [reloadKey, hasToken], { enabled: hasToken });
  const store = useAsync(() => api.childStore(), [reloadKey, hasToken], { enabled: hasToken });
  const refresh = (): void => setReloadKey((k) => k + 1);

  if (!hasToken) return <TokenForm onSaved={() => setHasToken(true)} />;

  // Неверный или отозванный токен возвращает к вводу, а не показывает пустой
  // экран. Сравниваем код ответа, а не текст сообщения: текст меняется.
  if (me.status === 401) {
    return <TokenForm invalid onSaved={() => { setHasToken(true); refresh(); }} />;
  }

  const data = me.data;
  const screen = data?.screen;
  const cpm = data?.policy.economy.creditsPerMinute ?? 1;
  const screenText = !screen ? '' : screen.allowed ? 'Экран открыт'
    : screen.reason === 'window_blocked' ? `Экран закрыт: ${screen.window}`
    : screen.reason === 'tasks_only' ? `Сейчас только задания: ${screen.window}`
    : 'Время на сегодня кончилось';

  return (
    <div className="kid">
      <header className="kid-top">
        <div className="kid-top-inner">
          <span className="brand"><span className="brand-mark"><Icon name="brand" /></span>MyKids</span>
          <span className="spacer" />
          <ThemeToggle variant="icon" />
        </div>
      </header>

      <main className="kid-main">
        <ErrorBox message={me.error} />
        {!data && me.loading && <CardSkeleton lines={3} />}

        {data && (
          <>
            <section className="hero" aria-label="Баланс">
              <div className="row between">
                <h1>Привет, {data.name}</h1>
                <span className={`screen-pill ${screen?.allowed ? '' : 'closed'}`}>
                  <span className="dot" />{screenText}
                </span>
              </div>
              <div className="hero-grid">
                <div className="hero-stat">
                  <div className="l">Минут экрана</div>
                  <div className="v" data-testid="child-minutes">{num(data.balances.minutes)}</div>
                  <div className="n">{data.balances.minutes >= 60 ? duration(data.balances.minutes) : 'на счету'}</div>
                </div>
                <div className="hero-stat">
                  <div className="l">Кредитов</div>
                  <div className="v" data-testid="child-credits">{num(data.balances.credits)}</div>
                  <div className="n">≈ {Math.floor(data.balances.credits / cpm)} мин по курсу</div>
                </div>
              </div>
            </section>

            <Tabs<Section>
              label="Разделы"
              segmented
              value={section}
              onChange={(s) => setParams(s === 'tasks' ? {} : { s })}
              tabs={[
                { key: 'tasks', label: 'Задания', icon: 'book' },
                { key: 'shop', label: 'Магазин', icon: 'bag' },
                { key: 'rules', label: 'Правила', icon: 'list' },
              ]}
            />

            <div role="tabpanel" className="stack" style={{ ['--gap' as string]: '20px' }}>
              {section === 'tasks' && <KidTasks onEarned={refresh} />}
              {section === 'shop' && <KidShop me={data} items={store.data} onChanged={refresh} />}
              {section === 'rules' && <KidRules me={data} onDisconnect={() => setHasToken(false)} />}
            </div>

            <p className="small subtle" style={{ textAlign: 'center' }}>
              Заработать больше, чем {credits(data.policy.economy.maxCreditsPerDay)} в день, нельзя — так договорились.
            </p>
          </>
        )}
      </main>
    </div>
  );
}
