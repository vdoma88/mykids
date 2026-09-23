import type { JSX } from 'react';
import { useEffect, useState } from 'react';
import { BrowserRouter, Navigate, Route, Routes, useNavigate } from 'react-router-dom';
import { api, parentToken, type Me } from './api.js';
import { Shell } from './layout/Shell.js';
import { LoginPage } from './pages/Login.js';
import { ChildrenPage } from './pages/Children.js';
import { ChildPage } from './pages/child/ChildPage.js';
import { StorePage } from './pages/Store.js';
import { ApprovalsPage } from './pages/Approvals.js';
import { SettingsPage } from './pages/Settings.js';
import { KidApp } from './kid/KidApp.js';
import { ToastProvider } from './ui/Toast.js';
import { Icon } from './ui/Icon.js';

function Splash(): JSX.Element {
  return (
    <div className="auth-screen" aria-busy="true">
      <span className="brand" style={{ color: 'var(--text)' }}>
        <span className="brand-mark"><Icon name="brand" /></span>MyKids
      </span>
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

  if (!checked) return <Splash />;
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
    >
      <Routes>
        <Route path="/children" element={<ChildrenPage me={me} />} />
        <Route path="/children/:childId" element={<ChildPage me={me} />} />
        <Route path="/children/:childId/:tab" element={<ChildPage me={me} />} />
        <Route path="/store" element={<StorePage me={me} />} />
        <Route path="/approvals" element={<ApprovalsPage me={me} />} />
        <Route path="/settings" element={<SettingsPage me={me} onChanged={setMe} />} />
        <Route path="*" element={<Navigate to="/children" replace />} />
      </Routes>
    </Shell>
  );
}

export function App(): JSX.Element {
  return (
    <ToastProvider>
      <BrowserRouter>
        <Routes>
          {/* Интерфейс ребёнка живёт отдельной веткой: у него свой доступ
              по токену устройства и своя навигация. */}
          <Route path="/child/*" element={<KidApp />} />
          <Route path="*" element={<Authenticated />} />
        </Routes>
      </BrowserRouter>
    </ToastProvider>
  );
}
