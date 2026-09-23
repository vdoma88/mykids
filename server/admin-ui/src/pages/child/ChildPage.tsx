import type { JSX } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { api, type Me } from '../../api.js';
import { num } from '../../lib/format.js';
import { ErrorBox, CardSkeleton, useAsync } from '../../ui/Async.js';
import { Icon } from '../../ui/Icon.js';
import { Avatar, Badge, PageHead, Tabs, type TabDef } from '../../ui/kit.js';
import { OverviewTab } from './OverviewTab.js';
import { RulesTab } from './RulesTab.js';
import { PacksTab } from './PacksTab.js';
import { DevicesTab } from './DevicesTab.js';
import { LedgerTab } from './LedgerTab.js';

type TabKey = 'overview' | 'rules' | 'packs' | 'devices' | 'ledger';
const KEYS: TabKey[] = ['overview', 'rules', 'packs', 'devices', 'ledger'];

/**
 * Страница ребёнка.
 *
 * Раньше это была одна лента на три с половиной тысячи пикселей: лимиты,
 * расписание, экономика, корректировка, устройства, пакеты, события и журнал
 * подряд, а заголовок гласил просто «Ребёнок». Теперь — вкладки с адресом:
 * ссылку на журнал или на правила можно открыть сразу.
 */
export function ChildPage({ me }: { me: Me }): JSX.Element {
  const { childId = '', tab } = useParams();
  const navigate = useNavigate();
  const active: TabKey = KEYS.includes(tab as TabKey) ? (tab as TabKey) : 'overview';
  const readOnly = me.role === 'viewer';

  const childQ = useAsync(() => api.child(childId), [childId]);
  const tamperQ = useAsync(() => api.tampers(childId), [childId]);
  const child = childQ.data;
  const pendingTampers = tamperQ.data?.pending ?? 0;

  const tabs: TabDef<TabKey>[] = [
    { key: 'overview', label: 'Обзор', icon: 'grid', ...(pendingTampers ? { count: pendingTampers } : {}) },
    { key: 'rules', label: 'Правила', icon: 'calendar' },
    { key: 'packs', label: 'Задания', icon: 'book' },
    { key: 'devices', label: 'Устройства', icon: 'monitor' },
    { key: 'ledger', label: 'Журнал', icon: 'list' },
  ];

  return (
    <>
      <PageHead
        back={<Link to="/children" className="back"><Icon name="chevronLeft" />Все дети</Link>}
        title={
          <span className="row nowrap" style={{ ['--gap' as string]: '14px' }}>
            {child && <Avatar name={child.name} large />}
            <span>{child?.name ?? '…'}</span>
          </span>
        }
        actions={child && (
          <>
            <Badge tone="accent"><Icon name="clock" size={14} /><span data-testid="bal-minutes">{num(child.balances.minutes)}</span> мин</Badge>
            <Badge tone="accent"><Icon name="coins" size={14} /><span data-testid="bal-credits">{num(child.balances.credits)}</span> кр.</Badge>
          </>
        )}
      />
      <ErrorBox message={childQ.error} />

      <Tabs<TabKey>
        label="Разделы"
        tabs={tabs}
        value={active}
        onChange={(k) => navigate(k === 'overview' ? `/children/${childId}` : `/children/${childId}/${k}`)}
      />

      {!child && childQ.loading && <CardSkeleton lines={4} />}

      {child && (
        <div role="tabpanel" className="stack" style={{ ['--gap' as string]: '20px' }}>
          {active === 'overview' && (
            <OverviewTab child={child} readOnly={readOnly} tamperQ={tamperQ} onChanged={childQ.reload} />
          )}
          {active === 'rules' && <RulesTab childId={childId} readOnly={readOnly} />}
          {active === 'packs' && <PacksTab childId={childId} readOnly={readOnly} />}
          {active === 'devices' && <DevicesTab child={child} readOnly={readOnly} onChanged={childQ.reload} />}
          {active === 'ledger' && <LedgerTab childId={childId} />}
        </div>
      )}
    </>
  );
}
