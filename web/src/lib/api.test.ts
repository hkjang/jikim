import { startHttpServer } from '../test/httpServer';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError, api, streamChat, get, simulatePolicy, testAIIntegration, testWebhookIntegration } from './api';

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

// These regressions cross an actual HTTP connection, including native stream reads.
describe('실제 HTTP API 오류와 SSE', () => {
  for (const client of ['api', 'streamChat'] as const) {
    it.each([400, 401, 403, 429, 500, 503])(`${client} JSON HTTP %i 계약`, async (status) => {
      const details = { error: { code: 'request_failed', message: '요청을 처리할 수 없습니다' }, request_id: 'test' };
      const stop = await startHttpServer((_request, response) => {
        response.writeHead(status, { 'Content-Type': 'application/json' });
        response.end(JSON.stringify(details));
      });
      let events = 0;
      const listener = () => { events++; };
      window.addEventListener('jikim:unauthorized', listener);
      try {
        const request = client === 'api' ? api('/ai/chat') : streamChat({ messages: [] }, () => {});
        await expect(request).rejects.toBeInstanceOf(ApiError);
        await expect(request).rejects.toMatchObject({ message: details.error.message, status, details });
        expect(events).toBe(status === 401 ? 1 : 0);
      } finally {
        window.removeEventListener('jikim:unauthorized', listener);
        await stop();
      }
    });

    it.each([
      ['text/plain', '일시적인 장애', '일시적인 장애'],
      ['text/plain', '', '요청을 처리하지 못했습니다.'],
      ['application/json', '{"message":"상위 메시지","error":{"message":"하위"}}', '상위 메시지'],
      ['application/json', '{"error":"문자열 오류"}', '문자열 오류'],
      ['application/json', '{"errors":["첫 오류","다음 오류"]}', '첫 오류, 다음 오류'],
      ['application/json', '{}', '요청을 처리하지 못했습니다.'],
    ])(`${client} 오류 본문 %s %s`, async (contentType, body, message) => {
      const stop = await startHttpServer((_request, response) => {
        response.writeHead(503, { 'Content-Type': contentType });
        response.end(body);
      });
      try {
        const request = client === 'api' ? api('/ai/chat') : streamChat({ messages: [] }, () => {});
        await expect(request).rejects.toMatchObject({ message, status: 503, details: contentType === 'application/json' ? JSON.parse(body) : body });
      } finally { await stop(); }
    });
  }

  it('정상 SSE에서 나뉜 한국어 UTF-8 청크를 수신한다', async () => {
    const bytes = Buffer.from('data: {"content":"안녕하세요"}\n\ndata: {"choices":[{"delta":{"content":" 반갑습니다"}}]}\n\ndata: [DONE]\n\n');
    const stop = await startHttpServer(async (_request, response) => {
      response.writeHead(200, { 'Content-Type': 'text/event-stream' });
      const split = bytes.indexOf(Buffer.from('안')) + 1;
      response.write(bytes.subarray(0, split));
      await new Promise<void>((resolve) => setImmediate(resolve));
      response.end(bytes.subarray(split));
    });
    try {
      const chunks: string[] = [];
      await streamChat({ messages: [{ role: 'user', content: '안녕' }] }, (text) => chunks.push(text));
      expect(chunks).toEqual(['안녕하세요', ' 반갑습니다']);
    } finally { await stop(); }
  });

  it('스트림 수신 중 AbortSignal 취소는 AbortError를 유지한다', async () => {
    const stop = await startHttpServer((_request, response) => {
      response.writeHead(200, { 'Content-Type': 'text/event-stream' });
      response.write('data: {"content":"시작"}\n\n');
    });
    try {
      const controller = new AbortController();
      const chunks: string[] = [];
      const request = streamChat({ messages: [] }, (text) => { chunks.push(text); controller.abort(); }, controller.signal);
      await expect(request).rejects.toMatchObject({ name: 'AbortError' });
      expect(chunks).toEqual(['시작']);
    } finally { await stop(); }
  });
});
