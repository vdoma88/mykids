import type { Widget, WidgetResult } from './types.js';

/** Таблица Шульте: найти числа по порядку. Перенос упражнения из UKids Home. */
export function schulte(): Widget {
  let started = 0;
  let finishedAt: number | null = null;
  let next = 1;
  let host: HTMLElement | null = null;
  let size = 5;

  const paint = (): void => {
    if (!host) return;
    const grid = host.querySelector<HTMLElement>('.schulte-grid');
    if (!grid) return;
    grid.querySelectorAll<HTMLElement>(".schulte-cell").forEach((cell) => {
      const n = Number(cell.dataset['n']);
      cell.classList.toggle('found', n < next);
      cell.textContent = n < next ? '✓' : String(n);
    });
    const status = host.querySelector<HTMLElement>('.schulte-status');
    if (status) {
      status.textContent = finishedAt === null
        ? `Ищи ${next}`
        : `Готово за ${Math.round((finishedAt - started) / 1000)} с`;
    }
  };

  return {
    mount(el, props) {
      host = el;
      size = typeof props['size'] === 'number' ? props['size'] : 5;
      const total = size * size;
      const nums = Array.from({ length: total }, (_, i) => i + 1);
      for (let i = nums.length - 1; i > 0; i--) {
        const j = Math.floor(Math.random() * (i + 1));
        [nums[i], nums[j]] = [nums[j] as number, nums[i] as number];
      }

      started = Date.now();
      el.innerHTML = `
        <div class="schulte-grid" style="grid-template-columns:repeat(${size},1fr)">
          ${nums.map((n) => `<button type="button" class="schulte-cell" data-n="${n}">${n}</button>`).join('')}
        </div>
        <p class="schulte-status"></p>`;

      el.addEventListener('click', (ev) => {
        const cell = (ev.target as HTMLElement).closest<HTMLElement>('.schulte-cell');
        if (!cell || finishedAt !== null) return;
        if (Number(cell.dataset['n']) === next) {
          next++;
          if (next > total) finishedAt = Date.now();
          paint();
        }
      });
      paint();
    },

    result(): WidgetResult {
      return {
        completed: finishedAt !== null,
        durationSec: Math.round(((finishedAt ?? Date.now()) - started) / 1000),
      };
    },

    unmount() {
      if (host) host.innerHTML = '';
      host = null;
    },
  };
}
