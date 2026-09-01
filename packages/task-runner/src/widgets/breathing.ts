import type { Widget, WidgetResult } from './types.js';

const PHASES = [
  { label: 'Вдох', sec: 4, scale: 1.35 },
  { label: 'Задержка', sec: 7, scale: 1.35 },
  { label: 'Выдох', sec: 8, scale: 0.8 },
] as const;

/** Дыхание 4-7-8. Перенос упражнения из UKids Home. */
export function breathing(): Widget {
  let started = 0;
  let cycles = 0;
  let timer: ReturnType<typeof setTimeout> | null = null;
  let host: HTMLElement | null = null;

  const runPhase = (index: number, targetCycles: number): void => {
    if (!host) return;
    const circle = host.querySelector<HTMLElement>('.breath-circle');
    if (!circle) return;

    if (index >= PHASES.length) {
      cycles++;
      if (cycles >= targetCycles) {
        circle.textContent = 'Готово';
        circle.style.transform = 'scale(1)';
        return;
      }
      runPhase(0, targetCycles);
      return;
    }

    const phase = PHASES[index] as (typeof PHASES)[number];
    circle.textContent = `${phase.label} · ${phase.sec}`;
    circle.style.transition = `transform ${phase.sec}s linear`;
    circle.style.transform = `scale(${phase.scale})`;
    timer = setTimeout(() => runPhase(index + 1, targetCycles), phase.sec * 1000);
  };

  return {
    mount(el, props) {
      host = el;
      started = Date.now();
      const targetCycles = typeof props['cycles'] === 'number' ? props['cycles'] : 3;
      el.innerHTML = `
        <div class="breath-circle">Готов?</div>
        <button type="button" class="btn breath-start">Начать</button>
        <p class="hint">Вдох на 4 счёта, задержка на 7, выдох на 8. Повторить ${targetCycles} раза.</p>`;
      el.querySelector('.breath-start')?.addEventListener('click', () => {
        if (timer !== null) return;
        cycles = 0;
        started = Date.now();
        runPhase(0, targetCycles);
      });
    },

    result(): WidgetResult {
      return { completed: cycles >= 1, durationSec: Math.round((Date.now() - started) / 1000) };
    },

    unmount() {
      if (timer !== null) clearTimeout(timer);
      timer = null;
      if (host) host.innerHTML = '';
      host = null;
    },
  };
}
