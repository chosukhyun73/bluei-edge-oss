import { t } from './i18n';

// 빈 문자열 default — 같은 주소의 processing-bluei 서버를 부른다. 개발 때는 Vite proxy 가 /v1/* 을 넘긴다.
const BASE = (import.meta.env['VITE_PROCE_API'] as string | undefined) ?? '';

export class ApiError extends Error {
  constructor(public code: string, message: string, public status: number) {
    super(message);
    this.name = 'ApiError';
  }
}

const API_TIMEOUT_MS = 20000;

// 거부 응답은 { error: { code, message }, issues? } 모양이다. raw 로 본문 전체를 돌려받을 수 있다.
export async function request(path: string, init?: RequestInit): Promise<{ ok: boolean; status: number; body: unknown }> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), API_TIMEOUT_MS);
  try {
    const res = await fetch(BASE + path, {
      headers: { 'content-type': 'application/json' }, ...init, signal: controller.signal,
    });
    let body: unknown = null;
    try { body = await res.json(); } catch { /* ignore parse error */ }
    return { ok: res.ok, status: res.status, body };
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') {
      throw new ApiError('TIMEOUT', t('api.timeoutError', { s: API_TIMEOUT_MS / 1000, path }), 0);
    }
    throw err;
  } finally {
    clearTimeout(timer);
  }
}

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await request(path, init);
  if (!res.ok) {
    const err = (res.body as { error?: { code?: string; message?: string } } | null)?.error ?? {};
    throw new ApiError(err.code ?? 'HTTP_ERROR', err.message ?? `HTTP ${res.status}`, res.status);
  }
  return res.body as T;
}
