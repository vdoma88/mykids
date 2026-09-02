import type { TaskItem } from '@mykids/contracts';
import type { Answer } from '../answer.js';
import { createWidget, type Widget } from '../widgets/index.js';

const esc = (s: string): string =>
  s.replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c] as string));

/** Отрисованное задание: знает, как собрать ответ из своего DOM. */
export interface RenderedItem {
  /** null, пока ответ не готов к отправке. */
  collect(): Answer | null;
  dispose(): void;
}

function shuffled<T>(items: readonly T[]): T[] {
  const out = [...items];
  for (let i = out.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1));
    [out[i], out[j]] = [out[j] as T, out[i] as T];
  }
  return out;
}

export function renderItem(host: HTMLElement, item: TaskItem): RenderedItem {
  const hints = item.hints?.length
    ? `<details class="hints"><summary>Подсказка</summary>${item.hints.map((h) => `<p>${esc(h)}</p>`).join('')}</details>`
    : '';
  const head = `<h2 class="stem">${esc(item.stem)}</h2>`;
  let widget: Widget | null = null;

  const body = (inner: string): void => {
    host.innerHTML = `${head}<div class="body">${inner}</div>${hints}`;
  };

  const noop = { dispose: (): void => { widget?.unmount(); } };

  switch (item.type) {
    case 'single_choice': {
      body(item.options
        .map((o, i) => `<label class="opt"><input type="radio" name="opt" value="${i}"/> ${esc(o.text)}</label>`)
        .join(''));
      return {
        ...noop,
        collect: () => {
          const picked = host.querySelector<HTMLInputElement>('input[name=opt]:checked');
          return picked ? { type: 'single_choice', optionIndex: Number(picked.value) } : null;
        },
      };
    }

    case 'multi_choice': {
      body(item.options
        .map((o, i) => `<label class="opt"><input type="checkbox" value="${i}"/> ${esc(o.text)}</label>`)
        .join(''));
      return {
        ...noop,
        collect: () => {
          const picked = [...host.querySelectorAll<HTMLInputElement>('input[type=checkbox]:checked')];
          return picked.length ? { type: 'multi_choice', optionIndexes: picked.map((p) => Number(p.value)) } : null;
        },
      };
    }

    case 'numeric':
    case 'short_text': {
      const unit = item.type === 'numeric' && item.answer.unit ? ` <span class="unit">${esc(item.answer.unit)}</span>` : '';
      body(`<input class="answer-input" type="text" autocomplete="off" placeholder="Ответ"/>${unit}`);
      const type = item.type;
      return {
        ...noop,
        collect: () => {
          const raw = host.querySelector<HTMLInputElement>('.answer-input')?.value ?? '';
          return raw.trim() ? { type, raw } : null;
        },
      };
    }

    case 'ordering': {
      // Кнопки вверх/вниз вместо перетаскивания: работают и мышью, и с клавиатуры.
      const order = shuffled(item.sequence);
      body(`<ol class="order-list">${order
        .map((v) => `<li data-v="${esc(v)}"><span>${esc(v)}</span><button type="button" data-dir="up">↑</button><button type="button" data-dir="down">↓</button></li>`)
        .join('')}</ol>`);
      host.querySelector('.order-list')?.addEventListener('click', (ev) => {
        const btn = (ev.target as HTMLElement).closest<HTMLElement>('button[data-dir]');
        const li = btn?.closest('li');
        if (!btn || !li?.parentElement) return;
        if (btn.dataset['dir'] === 'up' && li.previousElementSibling) {
          li.parentElement.insertBefore(li, li.previousElementSibling);
        } else if (btn.dataset['dir'] === 'down' && li.nextElementSibling) {
          li.parentElement.insertBefore(li.nextElementSibling, li);
        }
      });
      return {
        ...noop,
        collect: () => ({
          type: 'ordering',
          order: [...host.querySelectorAll<HTMLElement>('.order-list li')].map((li) => li.dataset['v'] ?? ''),
        }),
      };
    }

    case 'matching': {
      const rights = shuffled(item.pairs.map((p) => p.right));
      body(`<div class="match">${item.pairs
        .map((p) => `<div class="match-row"><span>${esc(p.left)}</span><select data-left="${esc(p.left)}">
          <option value="">—</option>${rights.map((r) => `<option value="${esc(r)}">${esc(r)}</option>`).join('')}
        </select></div>`)
        .join('')}</div>`);
      return {
        ...noop,
        collect: () => {
          const assignments: Record<string, string> = {};
          let any = false;
          for (const sel of host.querySelectorAll<HTMLSelectElement>('select[data-left]')) {
            if (sel.value) { assignments[sel.dataset['left'] ?? ''] = sel.value; any = true; }
          }
          return any ? { type: 'matching', assignments } : null;
        },
      };
    }

    case 'cloze': {
      let html = esc(item.stem);
      for (const blank of item.blanks) {
        html = html.replace(`{{${blank.id}}}`, `<input class="blank" data-blank="${esc(blank.id)}" type="text"/>`);
      }
      host.innerHTML = `<div class="body"><p class="stem">${html}</p></div>${hints}`;
      return {
        ...noop,
        collect: () => {
          const blanks: Record<string, string> = {};
          let any = false;
          for (const input of host.querySelectorAll<HTMLInputElement>('.blank')) {
            blanks[input.dataset['blank'] ?? ''] = input.value;
            if (input.value.trim()) any = true;
          }
          return any ? { type: 'cloze', blanks } : null;
        },
      };
    }

    case 'likert': {
      const { min, max } = item.scale;
      const points = Array.from({ length: max - min + 1 }, (_, i) => min + i);
      body(`<div class="likert">
        <span class="scale-label">${esc(item.scale.minLabel ?? String(min))}</span>
        ${points.map((v) => `<label><input type="radio" name="likert" value="${v}"/><span>${v}</span></label>`).join('')}
        <span class="scale-label">${esc(item.scale.maxLabel ?? String(max))}</span>
      </div>`);
      return {
        ...noop,
        collect: () => {
          const picked = host.querySelector<HTMLInputElement>('input[name=likert]:checked');
          return picked ? { type: 'likert', value: Number(picked.value) } : null;
        },
      };
    }

    case 'reflection': {
      const required = item.minChars ?? 40;
      body(`${(item.prompts ?? []).map((p) => `<p class="prompt">${esc(p)}</p>`).join('')}
        <textarea class="answer-text" rows="5" placeholder="Напиши хотя бы ${required} символов"></textarea>
        <p class="counter"></p>`);
      const area = host.querySelector<HTMLTextAreaElement>('.answer-text');
      const counter = host.querySelector<HTMLElement>('.counter');
      area?.addEventListener('input', () => {
        const n = area.value.trim().length;
        if (counter) counter.textContent = `${n} из ${required}`;
      });
      return { ...noop, collect: () => (area && area.value.trim() ? { type: 'reflection', text: area.value } : null) };
    }

    case 'parent_verified': {
      body(`<p class="verify">Родитель подтвердит выполнение: «${esc(item.verificationPrompt)}»</p>
        <label class="opt"><input type="checkbox" class="ask-parent"/> Я сделал(а), отправить на подтверждение</label>`);
      return {
        ...noop,
        collect: () =>
          host.querySelector<HTMLInputElement>('.ask-parent')?.checked
            ? { type: 'parent_verified', requested: true }
            : null,
      };
    }

    case 'interactive': {
      body('<div class="widget-host"></div>');
      const widgetHost = host.querySelector<HTMLElement>('.widget-host');
      widget = createWidget(item.widget);
      if (widgetHost) {
        widget.mount(widgetHost, {
          ...(item.props ?? {}),
          minDurationSec: item.completionRule?.minDurationSec ?? 60,
        });
      }
      return {
        dispose: () => widget?.unmount(),
        collect: () => {
          const r = widget?.result();
          if (!r) return null;
          return { type: 'interactive', completed: r.completed, durationSec: r.durationSec,
                   ...(r.score !== undefined ? { score: r.score } : {}) };
        },
      };
    }

    case 'code': {
      body(`<p class="verify">Задания на код появятся в фазе 5.</p>`);
      return { ...noop, collect: () => null };
    }
  }
}
