/** Контракт интерактивного виджета: он сообщает результат, но не начисляет кредиты. */
export interface WidgetResult {
  completed: boolean;
  score?: number;
  durationSec: number;
}

export interface Widget {
  mount(host: HTMLElement, props: Record<string, unknown>): void;
  result(): WidgetResult;
  unmount(): void;
}

export type WidgetFactory = () => Widget;
