import type { JSX } from 'react';
import { useEffect, useState } from 'react';
import { BrowserRouter, NavLink, Navigate, Route, Routes, useNavigate } from 'react-router-dom';
import { api, parentToken, type Me } from './api.js';
import { LoginPage } from './pages/Login.js';
import { ChildrenPage } from './pages/Children.js';
import { ChildPage } from './pages/Child.js';
import { StorePage } from './pages/Store.js';
import { ApprovalsPage } from './pages/Approvals.js';
import { ChildApp } from './pages/ChildApp.js';

function Shell({ me, onLogout }: { me: Me; onLogout: () => void }): JSX.Element {
  return (
    <div className="shell">
      <aside className="side">
        <h1>MyKids</h1>
        <nav>
          <NavLink to="/children" className={({ isActive }) => (isActive ? 'active' : '')}>Дети</NavLink>
          <NavLink to="/store" className={({ isActive }) => (isActive ? 'active' : '')}>Магазин</NavLink>
          <NavLink to="/approvals" className={({ isActive }) => (isActive ? 'active' : '')}>Одобрения</NavLink>
        </nav>
        <div className="foot">
          {me.email}<br />
          {me.role === 'owner' ? 'владелец' : me.role === 'parent' ? 'родитель' : 'только чтение'}
          {!me.totpEnabled && <><br /><span style={{ color: '#F2C879' }}>второй фактор выключен</span></>}
          <br /><br />
          <button className="ghost" onClick={onLogout} style={{ color: '#D9D3F0', borderColor: '#7F72B0' }}>
            Выйти
          </button>
        </div>
      </aside>
      <main className="main">
        <Routes>
          <Route path="/children" element={<ChildrenPage me={me} />} />
          <Route path="/children/:childId" element={<ChildPage me={me} />} />
          <Route path="/store" element={<StorePage me={me} />} />
          <Route path="/approvals" element={<ApprovalsPage />} />
          <Route path="*" element={<Navigate to="/children" replace />} />
        </Routes>
      </main>
    </div>
  );
}

function Authenticated(): JSX.Element {
  const [me, setMe] = useState<Me | null>(null);
  const [checked, setChecked] = useState(false);
  const navigate = useNavigate();

  useEffect(() => {
    if (!parentToken.get()) { setChecked(true); return; }
    api.me()
      .then(setMe)
      // Токен мог протухнуть, пока вкладка была закрыта: тихо возвращаем на вход.
      .catch(() => { parentToken.set(null); })
      .finally(() => setChecked(true));
  }, []);

  if (!checked) return <p className="note" style={{ padding: 24 }}>Загрузка…</p>;
  if (!me) return <LoginPage onSignedIn={setMe} />;

  return (
    <Shell
      me={me}
      onLogout={() => {
        void api.logout().catch(() => undefined);
        parentToken.set(null);
        setMe(null);
        navigate('/');
      }}
    />
  );
}

export function App(): JSX.Element {
  return (
    <BrowserRouter>
      <Routes>
        {/* Интерфейс ребёнка живёт отдельной веткой: у него свой доступ
            по токену устройства и своя навигация. */}
        <Route path="/child/*" element={<ChildApp />} />
        <Route path="*" element={<Authenticated />} />
      </Routes>
    </BrowserRouter>
  );
}
