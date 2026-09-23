import type { JSX, ReactNode } from 'react';
import { useEffect, useState } from 'react';
import { Link, NavLink, useLocation } from 'react-router-dom';
import { api, type Me } from '../api.js';
import { ROLES } from '../lib/labels.js';
import { Icon, type IconName } from '../ui/Icon.js';
import { Avatar } from '../ui/kit.js';
import { ThemeToggle } from './ThemeToggle.js';

/** Событие «очередь одобрений изменилась»: счётчик в меню обновляется сразу. */
export const APPROVALS_CHANGED = 'mykids:approvals-changed';

/**
 * Сколько всего ждёт родителя. Раньше очередь одобрений было видно, только
 * зайдя в неё: ребёнок отправлял задание — и ждал, пока родитель случайно
 * туда заглянет.
 */
function usePendingCount(): number {
  const [count, setCount] = useState(0);
  const location = useLocation();
  useEffect(() => {
    let alive = true;
    const load = (): void => {
      api.approvals()
        .then((q) => { if (alive) setCount(q.purchases.length + q.attempts.length); })
        .catch(() => { /* счётчик — подсказка, а не данные: молча */ });
    };
    load();
    const timer = window.setInterval(load, 60_000);
    window.addEventListener(APPROVALS_CHANGED, load);
    return () => { alive = false; window.clearInterval(timer); window.removeEventListener(APPROVALS_CHANGED, load); };
  }, [location.pathname]);
  return count;
}

interface NavDef { to: string; label: string; icon: IconName; count?: number }

export function Shell({ me, onLogout, children }: { me: Me; onLogout: () => void; children: ReactNode }): JSX.Element {
  const pending = usePendingCount();
  const nav: NavDef[] = [
    { to: '/children', label: 'Дети', icon: 'users' },
    { to: '/store', label: 'Магазин', icon: 'bag' },
    { to: '/approvals', label: 'Одобрения', icon: 'inbox', count: pending },
    { to: '/settings', label: 'Настройки', icon: 'settings' },
  ];
  const link = (n: NavDef): JSX.Element => (
    <NavLink key={n.to} to={n.to} className={({ isActive }) => (isActive ? 'active' : '')}>
      <Icon name={n.icon} />
      <span>{n.label}</span>
      {n.count ? <span className="count" aria-label={`ждут: ${n.count}`}>{n.count}</span> : null}
    </NavLink>
  );

  return (
    <div className="shell">
      <aside className="sidebar" aria-label="Главное меню">
        <Link to="/children" className="brand" style={{ textDecoration: 'none' }}>
          <span className="brand-mark"><Icon name="brand" /></span>MyKids
        </Link>
        <nav className="nav">{nav.map(link)}</nav>
        <div className="sidebar-foot">
          {!me.totpEnabled && (
            <div className="sidebar-warning">
              <Icon name="shield" />
              <span>Второй фактор выключен. <Link to="/settings">Включить</Link></span>
            </div>
          )}
          <div className="account">
            <Avatar name={me.email} />
            <div className="who">
              <div className="email truncate" title={me.email}>{me.email}</div>
              <div className="role">{ROLES[me.role] ?? me.role}</div>
            </div>
          </div>
          <ThemeToggle variant="nav" />
          <button type="button" className="nav-btn" onClick={onLogout}>
            <Icon name="logout" />Выйти
          </button>
        </div>
      </aside>

      <header className="topbar">
        <Link to="/children" className="brand" style={{ textDecoration: 'none' }}>
          <span className="brand-mark"><Icon name="brand" /></span>MyKids
        </Link>
        <span className="spacer" />
        <ThemeToggle variant="icon" />
        <button type="button" className="btn ghost icon-only" aria-label="Выйти" onClick={onLogout}>
          <Icon name="logout" />
        </button>
      </header>

      <main className="content" id="main">
        <div className="content-inner">{children}</div>
      </main>

      <nav className="bottom-nav" aria-label="Главное меню">{nav.map(link)}</nav>
    </div>
  );
}
