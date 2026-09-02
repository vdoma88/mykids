import type { EconomyConfig } from '@mykids/contracts';
import { listPacks, loadPack } from './load.js';
import { TaskSession, type AttemptHistory } from './session.js';
import { renderItem, type RenderedItem } from './ui/render.js';

/**
 * Оболочка раннера для локального прогона без сервера.
 *
 * История попыток и кредиты живут в localStorage: это временно, до появления
 * API. Раннер считает кредиты только чтобы показать результат ребёнку —
 * записывать их будет сервер, пересчитывая теми же функциями домена.
 */

const PACKS_URL = '/content/packs';
const STORAGE_KEY = 'mykids-runner-state';

const ECONOMY: EconomyConfig = {
  creditsPerMinute: 2,
  maxConvertedMinutesPerDay: 45,
  minCreditsToConvert: 10,
  maxCreditsPerDay: 120,
};

interface StoredState {
  day: string;
  creditsTotal: number;
  creditsEarnedToday: number;
  packCreditsToday: Record<string, number>;
  lastAwardedAt: Record<string, string>;
}

const todayKey = (): string => new Date().toISOString().slice(0, 10);

function loadState(): StoredState {
  const empty: StoredState = {
    day: todayKey(), creditsTotal: 0, creditsEarnedToday: 0,
    packCreditsToday: {}, lastAwardedAt: {},
  };
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return empty;
    const parsed = JSON.parse(raw) as StoredState;
    // Суточные счётчики обнуляются при смене дня, накопленные кредиты — нет.
    if (parsed.day !== todayKey()) {
      return { ...empty, creditsTotal: parsed.creditsTotal ?? 0, lastAwardedAt: parsed.lastAwardedAt ?? {} };
    }
    return { ...empty, ...parsed };
  } catch {
    return empty;
  }
}

function saveState(state: StoredState): void {
  try { localStorage.setItem(STORAGE_KEY, JSON.stringify(state)); } catch { /* приватный режим */ }
}

function historyFor(state: StoredState, packId: string): AttemptHistory {
  const lastAwardedAt: Record<string, Date> = {};
  for (const [id, iso] of Object.entries(state.lastAwardedAt)) lastAwardedAt[id] = new Date(iso);
  return {
    lastAwardedAt,
    packCreditsToday: state.packCreditsToday[packId] ?? 0,
    creditsEarnedToday: state.creditsEarnedToday,
  };
}

function el<T extends HTMLElement>(selector: string): T {
  const node = document.querySelector<T>(selector);
  if (!node) throw new Error(`не найден элемент ${selector}`);
  return node;
}

async function main(): Promise<void> {
  const state = loadState();
  const balance = el('#balance');
  const packList = el('#pack-list');
  const stage = el('#stage');

  const paintBalance = (): void => {
    balance.textContent =
      `${state.creditsTotal} кредитов · сегодня заработано ${state.creditsEarnedToday} из ${ECONOMY.maxCreditsPerDay}`;
  };
  paintBalance();

  const packs = await listPacks(PACKS_URL);
  packList.innerHTML = packs
    .map((p) => `<button type="button" class="pack" data-id="${p.id}">
      <strong>${p.title}</strong><span>${p.subject} · ${p.itemCount} заданий</span></button>`)
    .join('');

  packList.addEventListener('click', (ev) => {
    const button = (ev.target as HTMLElement).closest<HTMLElement>('.pack');
    const id = button?.dataset['id'];
    if (id) void runPack(id);
  });

  async function runPack(packId: string): Promise<void> {
    const pack = await loadPack(PACKS_URL, packId);
    const session = new TaskSession({
      pack, economy: ECONOMY, history: historyFor(state, packId), now: new Date(),
    });

    let rendered: RenderedItem | null = null;

    const step = (): void => {
      rendered?.dispose();
      const item = session.current;

      if (!item) {
        const sum = session.summary();
        state.creditsTotal += sum.creditsEarned;
        state.creditsEarnedToday += sum.creditsEarned;
        state.packCreditsToday[packId] = (state.packCreditsToday[packId] ?? 0) + sum.creditsEarned;
        saveState(state);
        paintBalance();

        stage.innerHTML = `<section class="summary" id="summary">
          <h2>Сессия закончена</h2>
          <p>Верно: ${sum.correct} из ${sum.total} · точность ${Math.round(sum.accuracy * 100)}%</p>
          <p class="credits-earned">Начислено кредитов: <strong>${sum.creditsEarned}</strong>${
            sum.bonusAwarded > 0 ? ` (включая бонус ${sum.bonusAwarded})` : ''}</p>
          ${sum.pendingApproval.length ? `<p>Ждут подтверждения родителя: ${sum.pendingApproval.length}</p>` : ''}
          <button type="button" id="back">К списку пакетов</button>
        </section>`;
        el('#back').addEventListener('click', () => { stage.innerHTML = ''; });
        return;
      }

      const { index, total } = session.progress;
      stage.innerHTML = `<section class="card">
        <p class="progress">Задание ${index + 1} из ${total}</p>
        <div id="item"></div>
        <p class="feedback" id="feedback"></p>
        <button type="button" id="submit">Ответить</button>
      </section>`;

      rendered = renderItem(el('#item'), item);

      // Слушатель не одноразовый: при пустом ответе кнопка обязана сработать снова.
      const submit = el<HTMLButtonElement>('#submit');
      const onSubmit = (): void => {
        const answer = rendered?.collect();
        const feedback = el('#feedback');
        if (!answer) { feedback.textContent = 'Сначала ответь на задание.'; return; }
        submit.removeEventListener('click', onSubmit);

        const outcome = session.submit(answer);
        const parts = [outcome.grade.feedback, outcome.withheldReason].filter(Boolean);
        if (outcome.creditsAwarded > 0) parts.push(`+${outcome.creditsAwarded} кредитов`);
        feedback.textContent = parts.join(' ');
        if (outcome.creditsAwarded > 0) {
          state.lastAwardedAt[item.id] = new Date().toISOString();
        }

        submit.textContent = 'Дальше';
        submit.id = 'next';
        submit.addEventListener('click', step, { once: true });
      };
      submit.addEventListener('click', onSubmit);
    };

    step();
  }
}

void main().catch((err: unknown) => {
  document.body.insertAdjacentHTML('afterbegin',
    `<p class="error">Не удалось запустить раннер: ${String(err)}</p>`);
});
