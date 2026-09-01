import type { Widget, WidgetResult } from './types.js';

/**
 * Заглушка для виджетов, которые ещё не написаны.
 *
 * Она честно говорит об этом и не позволяет отметить выполнение мгновенно:
 * завершение открывается только после того, как истекло требуемое время.
 * Иначе заглушка стала бы способом фармить кредиты в один клик.
 */
export function timedPlaceholder(widgetName: string): Widget {
  let started = 0;
  let confirmed = false;
  let host: HTMLElement | null = null;
  let ticker: ReturnType<typeof setInterval> | null = null;

  return {
    mount(el, props) {
      host = el;
      started = Date.now();
      const required = typeof props['minDurationSec'] === 'number' ? props['minDurationSec'] : 60;

      el.innerHTML = `
        <p class="placeholder-note">
          Виджет «${widgetName}» ещё не реализован. Выполни упражнение по описанию,
          а затем отметь его — кнопка станет активной через отведённое время.
        </p>
        <p class="timed-left"></p>
        <button type="button" class="btn timed-done" disabled>Выполнено</button>`;

      const button = el.querySelector<HTMLButtonElement>('.timed-done');
      const left = el.querySelector<HTMLElement>('.timed-left');

      ticker = setInterval(() => {
        const elapsed = Math.round((Date.now() - started) / 1000);
        const remaining = Math.max(0, required - elapsed);
        if (left) left.textContent = remaining > 0 ? `Осталось ${remaining} с` : 'Можно отмечать';
        if (remaining === 0 && button) {
          button.disabled = false;
          if (ticker !== null) clearInterval(ticker);
          ticker = null;
        }
      }, 250);

      button?.addEventListener('click', () => {
        confirmed = true;
        button.textContent = 'Отмечено';
        button.disabled = true;
      });
    },

    result(): WidgetResult {
      return { completed: confirmed, durationSec: Math.round((Date.now() - started) / 1000) };
    },

    unmount() {
      if (ticker !== null) clearInterval(ticker);
      ticker = null;
      if (host) host.innerHTML = '';
      host = null;
    },
  };
}
