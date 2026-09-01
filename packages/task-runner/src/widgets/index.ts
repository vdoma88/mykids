import { breathing } from './breathing.js';
import { schulte } from './schulte.js';
import { timedPlaceholder } from './timed.js';
import type { Widget, WidgetFactory } from './types.js';

export * from './types.js';

/** Реализованные виджеты. Остальные из перечня схемы отдаются заглушкой с таймером. */
const REGISTRY: Record<string, WidgetFactory> = {
  schulte,
  breathing,
};

export function createWidget(name: string): Widget {
  const factory = REGISTRY[name];
  return factory ? factory() : timedPlaceholder(name);
}

export function isImplemented(name: string): boolean {
  return name in REGISTRY;
}
