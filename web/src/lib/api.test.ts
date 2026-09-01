import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError, get } from './api';

describe('API 클라이언트', () => {
  afterEach(() => { vi.restoreAllMocks(); });

  it('브라우저 세션은 토큰 헤더 노출 없이 same-origin 쿠키를 사용한다', async () => {
    const mocked = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ data: { ok: true } }), { status: 200, headers: { 'content-type': 'application/json' } }));
    await expect(get<{ ok: boolean }>('/me')).resolves.toEqual({ ok: true });
    const headers = new Headers(mocked.mock.calls[0][1]?.headers);
    expect(mocked.mock.calls[0][1]?.credentials).toBe('same-origin');
    expect(headers.get('Authorization')).toBeNull();
    expect(headers.get('X-Vault-Token')).toBeNull();
  });

  it('오류 응답을 ApiError로 정규화한다', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ error: { message: '거부됨' } }), { status: 403, headers: { 'content-type': 'application/json' } }));
    await expect(get('/settings')).rejects.toMatchObject({ status: 403, message: '거부됨' } satisfies Partial<ApiError>);
  });

  it('일회성 토큰이 포함된 응답 envelope를 보존한다', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ data: { id: 'token-id' }, token: 'hvs.once' }), { status: 201, headers: { 'content-type': 'application/json' } }));
    await expect(get<{ data: { id: string }; token: string }>('/tokens')).resolves.toEqual({ data: { id: 'token-id' }, token: 'hvs.once' });
  });
});
