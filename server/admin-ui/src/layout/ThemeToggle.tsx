import type { JSX } from 'react';
import { useEffect, useState } from 'react';
import { THEME_CHANGED, applyTheme, readTheme, type ThemeChoice } from '../lib/theme.js';
import { Icon, type IconName } from '../ui/Icon.js';

const NEXT: Record<ThemeChoice, ThemeChoice> = { system: 'light', light: 'dark', dark: 'system' };
const LABEL: Record<ThemeChoice, string> = { system: 'Тема: как в системе', light: 'Тема: светлая', dark: 'Тема: тёмная' };
const ICON: Record<ThemeChoice, IconName> = { system: 'monitor', light: 'sun', dark: 'moon' };

/**
 * Текущая тема и способ её сменить. Переключателей несколько — в меню, в
 * шапке телефона, в настройках, — и раньше каждый помнил своё: сменив тему в
 * настройках, в меню можно было увидеть «Тема: как в системе».
 */
export function useTheme(): [ThemeChoice, (t: ThemeChoice) => void] {
  const [theme, setTheme] = useState<ThemeChoice>(readTheme);
  useEffect(() => {
    const on = (e: Event): void => setTheme((e as CustomEvent<ThemeChoice>).detail);
    window.addEventListener(THEME_CHANGED, on);
    return () => window.removeEventListener(THEME_CHANGED, on);
  }, []);
  return [theme, applyTheme];
}

export function ThemeToggle({ variant }: { variant: 'nav' | 'icon' }): JSX.Element {
  const [theme, setTheme] = useTheme();
  const cycle = (): void => setTheme(NEXT[theme]);
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
