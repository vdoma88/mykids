import type { JSX } from 'react';
import { useState } from 'react';
import { applyTheme, readTheme, type ThemeChoice } from '../lib/theme.js';
import { Icon, type IconName } from '../ui/Icon.js';

const NEXT: Record<ThemeChoice, ThemeChoice> = { system: 'light', light: 'dark', dark: 'system' };
const LABEL: Record<ThemeChoice, string> = { system: 'Тема: как в системе', light: 'Тема: светлая', dark: 'Тема: тёмная' };
const ICON: Record<ThemeChoice, IconName> = { system: 'monitor', light: 'sun', dark: 'moon' };

export function ThemeToggle({ variant }: { variant: 'nav' | 'icon' }): JSX.Element {
  const [theme, setTheme] = useState<ThemeChoice>(readTheme);
  const cycle = (): void => {
    const next = NEXT[theme];
    applyTheme(next);
    setTheme(next);
  };
  if (variant === 'icon') {
    return (
      <button type="button" className="btn ghost icon-only" aria-label={LABEL[theme]} title={LABEL[theme]} onClick={cycle}>
        <Icon name={ICON[theme]} />
      </button>
    );
  }
  return (
    <button type="button" className="nav-btn" onClick={cycle}>
      <Icon name={ICON[theme]} />{LABEL[theme]}
    </button>
  );
}
