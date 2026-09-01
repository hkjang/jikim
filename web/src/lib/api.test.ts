import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError, get, simulatePolicy, testAIIntegration, testWebhookIntegration } from './api';

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

  it('정책 시뮬레이션은 서버 최종 판정 API에 요청한다', async () => {
    const mocked = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ data: { allowed: true, matches: [] } }), { status: 200, headers: { 'content-type': 'application/json' } }));
    await simulatePolicy({ user_id: 'user-1', path: 'production/payment/database', capability: 'read' });
    expect(mocked.mock.calls[0][0]).toBe('/api/v1/policies/simulate');
    expect(mocked.mock.calls[0][1]).toMatchObject({ method: 'POST', body: JSON.stringify({ user_id: 'user-1', path: 'production/payment/database', capability: 'read' }) });
  });

  it('연동 테스트는 저장된 AI와 webhook 설정을 빈 객체로 검사한다', async () => {
    const mocked = vi.spyOn(globalThis, 'fetch').mockImplementation(async () => new Response(JSON.stringify({ data: { ok: true } }), { status: 200, headers: { 'content-type': 'application/json' } }));
    await testAIIntegration();
    await testWebhookIntegration();
    expect(mocked.mock.calls.map(([url, options]) => [url, options?.method, options?.body])).toEqual([
      ['/api/v1/integrations/ai/test', 'POST', '{}'],
      ['/api/v1/integrations/webhook/test', 'POST', '{}'],
    ]);
  });
});
