/**
 * Тема оформления. По умолчанию — как в системе; выбор человека помнится
 * в этом браузере. Выбор хранится локально намеренно: у родителя днём
 * светлый ноутбук, вечером тёмный телефон, и навязывать одно другому незачем.
 */
export type ThemeChoice = 'system' | 'light' | 'dark';
const KEY = 'mykids-theme';

export function readTheme(): ThemeChoice {
  try {
    const v = localStorage.getItem(KEY);
    return v === 'light' || v === 'dark' ? v : 'system';
  } catch {
    return 'system';
  }
}

export function applyTheme(choice: ThemeChoice): void {
  const root = document.documentElement;
  if (choice === 'system') root.removeAttribute('data-theme');
  else root.setAttribute('data-theme', choice);
  try {
    if (choice === 'system') localStorage.removeItem(KEY);
    else localStorage.setItem(KEY, choice);
  } catch { /* приватный режим: тема живёт до закрытия вкладки */ }
}
