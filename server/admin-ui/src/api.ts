import type { Policy, StoreItem } from '@mykids/contracts';

/**
 * Клиент API.
 *
 * Токен родителя и токен устройства хранятся раздельно: админка и интерфейс
 * ребёнка могут быть открыты на одной машине, и путать их доступы нельзя.
 */
const PARENT_KEY = 'mykids-parent-token';
const DEVICE_KEY = 'mykids-device-token';

export class ApiError extends Error {
  constructor(readonly status: number, readonly code: string, message: string) {
    super(message);
  }
}

function read(key: string): string | null {
  try { return localStorage.getItem(key); } catch { return null; }
}
function write(key: string, value: string | null): void {
  try {
    if (value === null) localStorage.removeItem(key);
    else localStorage.setItem(key, value);
  } catch { /* приватный режим */ }
}

export const parentToken = {
  get: () => read(PARENT_KEY),
  set: (t: string | null) => write(PARENT_KEY, t),
};
export const deviceToken = {
  get: () => read(DEVICE_KEY),
  set: (t: string | null) => write(DEVICE_KEY, t),
};

async function request<T>(path: string, init: RequestInit & { token?: string | null } = {}): Promise<T> {
  const { token, ...rest } = init;
  const headers = new Headers(rest.headers);
  if (rest.body !== undefined) headers.set('content-type', 'application/json');
  if (token) headers.set('authorization', `Bearer ${token}`);

  const res = await fetch(path, { ...rest, headers });
  if (res.status === 204) return undefined as T;

  const text = await res.text();
  let data: unknown = null;
  try {
    data = text ? JSON.parse(text) : null;
  } catch {
    // Вместо JSON пришла страница — обычно это прокси или сервер, который
    // ещё поднимается. Сырое «Unexpected token <» человеку ничего не скажет.
    throw new ApiError(res.status, 'bad_response', `Сервер ответил не так, как ожидалось (код ${res.status}).`);
  }
  if (!res.ok) {
    const err = data as { error?: string; message?: string } | null;
    throw new ApiError(res.status, err?.error ?? 'unknown', err?.message ?? 'Ошибка запроса.');
  }
  return data as T;
}

const asParent = <T>(path: string, init: RequestInit = {}): Promise<T> =>
  request<T>(path, { ...init, token: parentToken.get() });
const asDevice = <T>(path: string, init: RequestInit = {}): Promise<T> =>
  request<T>(path, { ...init, token: deviceToken.get() });

// ------------------------------------------------------------------- типы

export interface Me {
  id: string; email: string; role: 'owner' | 'parent' | 'viewer';
  familyId: string; totpEnabled: boolean;
}

export interface Balances { minutes: number; credits: number }

export interface ChildSummary {
  id: string; name: string; birthYear: number | null;
  balances: Balances;
  devices: { id: string; platform: string; name: string; lastSeenAt: string | null; agentVersion: string | null }[];
  hasPolicy: boolean;
}

export interface LedgerRow {
  id: string; currency: 'minutes' | 'credits'; amount: number;
  reason: string; recordedAt: string; note: string | null;
  refType: string | null; deviceId: string | null;
}

export interface TamperEvent {
  id: string; kind: string; detail: string | null;
  /// Часы устройства: когда событие случилось. Может отсутствовать.
  occurredAt: string | null;
  /// Часы сервера: когда сообщение дошло.
  recordedAt: string; reviewedAt: string | null;
  device: { name: string } | null;
}

export interface ScreenState {
  allowed: boolean; reason: string; window?: string; minutesLeft?: number;
}

export interface ChildMe {
  name: string; balances: Balances; policy: Policy; screen: ScreenState;
}

export interface Device {
  id: string; platform: string; name: string; lastSeenAt: string | null; agentVersion: string | null;
}

export interface ChildDetail {
  id: string; name: string; birthYear: number | null; balances: Balances; devices: Device[];
}

export type ShopItem = StoreItem & { costCurrency: 'credits' | 'minutes'; costAmount: number; enabled: boolean };

export interface PendingPurchase {
  id: string; cost: number; currency: 'credits' | 'minutes'; createdAt: string;
  storeItem: { title: string; effect: unknown };
  child: { id: string; name: string };
}

export interface PendingAttempt {
  id: string; itemId: string; packId: string; createdAt: string;
  /** Текст задания и название пакета — сервер подставляет их из каталога. */
  stem: string | null; packTitle: string | null;
  child: { id: string; name: string };
}

// ------------------------------------------------------------------- вызовы

/** Пакет заданий в каталоге сервера. */
export interface PackSummary {
  id: string;
  title: string;
  subject: string;
  version: string;
  itemCount: number;
  description?: string;
}

export const api = {
  login: (body: { email: string; password: string; totp?: string }) =>
    request<{ token: string; expiresAt: string }>('/auth/login', {
      method: 'POST', body: JSON.stringify(body),
    }),

  register: (body: { familyName: string; email: string; password: string }) =>
    request<{ token: string; familyId: string }>('/auth/register', {
      method: 'POST', body: JSON.stringify(body),
    }),

  me: () => asParent<Me>('/auth/me'),
  logout: () => asParent<void>('/auth/logout', { method: 'POST' }),

  children: () => asParent<ChildSummary[]>('/admin/children'),
  child: (childId: string) => asParent<ChildDetail>(`/admin/children/${childId}`),
  addChild: (body: { name: string; birthYear?: number }) =>
    asParent<{ id: string }>('/admin/children', { method: 'POST', body: JSON.stringify(body) }),

  packs: (childId: string) =>
    asParent<{ assigned: string[]; catalog: PackSummary[] }>(`/admin/children/${childId}/packs`),
  savePacks: (childId: string, packs: string[]) =>
    asParent<{ assigned: string[] }>(`/admin/children/${childId}/packs`, {
      method: 'PUT', body: JSON.stringify({ packs }),
    }),

  policy: (childId: string) => asParent<Policy>(`/admin/children/${childId}/policy`),
  savePolicy: (childId: string, body: unknown) =>
    asParent<Policy>(`/admin/children/${childId}/policy`, { method: 'PUT', body: JSON.stringify(body) }),

  ledger: (childId: string, limit = 100) =>
    asParent<{ balances: Balances; entries: LedgerRow[] }>(`/admin/children/${childId}/ledger?limit=${limit}`),

  tampers: (childId: string) =>
    asParent<{ pending: number; events: TamperEvent[] }>(`/admin/children/${childId}/tampers`),

  reviewTampers: (childId: string, ids: string[]) =>
    asParent<{ reviewed: number; pending: number }>(`/admin/children/${childId}/tampers/review`, {
      method: 'POST', body: JSON.stringify({ ids }),
    }),

  adjust: (childId: string, body: { currency: 'minutes' | 'credits'; amount: number; note: string }) =>
    asParent<{ balances: Balances }>(`/admin/children/${childId}/adjust`, {
      method: 'POST', body: JSON.stringify(body),
    }),

  enrollDevice: (childId: string, body: { platform: 'windows' | 'android' | 'web'; name: string }) =>
    asParent<{ deviceId: string; token: string }>(`/admin/children/${childId}/devices`, {
      method: 'POST', body: JSON.stringify(body),
    }),

  revokeDevice: (deviceId: string) =>
    asParent<void>(`/admin/devices/${deviceId}`, { method: 'DELETE' }),

  store: () => asParent<ShopItem[]>('/admin/store'),
  addStoreItem: (body: unknown) =>
    asParent<{ id: string }>('/admin/store', { method: 'POST', body: JSON.stringify(body) }),
  setStoreItemEnabled: (id: string, enabled: boolean) =>
    asParent<{ id: string; enabled: boolean }>(`/admin/store/${id}`, {
      method: 'PATCH', body: JSON.stringify({ enabled }),
    }),

  approvals: () => asParent<{ purchases: PendingPurchase[]; attempts: PendingAttempt[] }>('/admin/approvals'),
  approvePurchase: (id: string) => asParent<void>(`/admin/purchases/${id}/approve`, { method: 'POST' }),
  rejectPurchase: (id: string) => asParent<void>(`/admin/purchases/${id}/reject`, { method: 'POST' }),

  // ---- аккаунт родителя
  totpBegin: () => asParent<{ secret: string; uri: string }>('/auth/totp/begin', { method: 'POST' }),
  totpConfirm: (secret: string, code: string) =>
    asParent<void>('/auth/totp/confirm', { method: 'POST', body: JSON.stringify({ secret, code }) }),
  totpDisable: (password: string) =>
    asParent<void>('/auth/totp/disable', { method: 'POST', body: JSON.stringify({ password }) }),
  addGuardian: (body: { email: string; password: string; role: 'parent' | 'viewer' }) =>
    asParent<{ guardianId: string }>('/auth/guardians', { method: 'POST', body: JSON.stringify(body) }),

  // ---- ребёнок, по токену устройства
  childMe: () => asDevice<ChildMe>('/child/me'),
  childStore: () => asDevice<ShopItem[]>('/child/store'),
  childConvert: (minutes: number) =>
    asDevice<{ minutes: number; creditsSpent: number; balances: Balances }>('/child/convert', {
      method: 'POST', body: JSON.stringify({ minutes }),
    }),
  childBuy: (storeItemId: string) =>
    asDevice<{ purchaseId: string; balances: Balances }>('/child/purchases', {
      method: 'POST', body: JSON.stringify({ storeItemId }),
    }),

  /** Какие пакеты назначены этому ребёнку. Содержимое лежит в /content/packs. */
  childPacks: () => asDevice<{ packs: string[] }>('/child/packs'),

  /**
   * Ответ на одно задание.
   *
   * Уходит только сам ответ. Проверяет его и назначает цену сервер: и
   * правильный ответ, и цена, и потолки лежат в пакете у него на диске.
   * Страница крутится на устройстве ребёнка, и её арифметика здесь не
   * значит ничего.
   */
  childAttempt: (body: { packId: string; itemId: string; answer: unknown }) =>
    asDevice<{
      credits: number; score: number; feedback?: string;
      withheldReason?: string; note?: string; pendingApproval?: true; balances: Balances;
    }>('/child/attempts', { method: 'POST', body: JSON.stringify(body) }),

  approveAttempt: (id: string) =>
    asParent<{ credits: number; withheldReason?: string; note?: string }>(
      `/admin/attempts/${id}/approve`, { method: 'POST' },
    ),
  rejectAttempt: (id: string) =>
    asParent<void>(`/admin/attempts/${id}/reject`, { method: 'POST' }),
};
