import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { App } from './App.js';
import { applyTheme, readTheme } from './lib/theme.js';
import './styles.css';

// Тему — до первого кадра: иначе тёмная тема на секунду вспыхивает светлой.
applyTheme(readTheme());

const root = document.getElementById('root');
if (!root) throw new Error('нет элемента #root');
createRoot(root).render(<StrictMode><App /></StrictMode>);
