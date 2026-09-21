/// <reference types="node" />

import { createServer, type Server } from 'node:http';
import { once } from 'node:events';
import { afterEach, describe, expect, it } from 'vitest';
import { streamChat } from './api';

const nativeFetch = globalThis.fetch;
const payload = { messages: [{ role: 'user', content: 'hello' }] };
let server: Server | undefined;

async function serveEvents(events: string[]) {
  server = createServer((request, response) => {
    request.resume();
    if (request.method !== 'POST' || request.url !== '/api/v1/ai/chat') {
      response.writeHead(404).end();
      return;
    }
    response.writeHead(200, { 'Content-Type': 'text/event-stream' });
    // Both events share one HTTP response, including when the first callback fails.
    response.end(events.map((event) => `data: ${event}\n\n`).join(''));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Expected TCP address');
  const origin = `http://127.0.0.1:${address.port}`;
  // Resolve relative URLs only; native fetch supplies the real Response and reader.
  globalThis.fetch = (input, init) => nativeFetch(
    typeof input === 'string' ? new URL(input, origin) : input,
    init,
  );
}

afterEach(async () => {
  globalThis.fetch = nativeFetch;
  if (server) {
    const current = server;
    server = undefined;
    await new Promise<void>((resolve, reject) => {
      current.close((error) => error ? reject(error) : resolve());
      current.closeAllConnections();
    });
  }
});

describe('streamChat 실제 HTTP 스트림 콜백', () => {
  const formats = [
    ['content', { content: 'hello' }],
    ['delta', { delta: 'hello' }],
    ['choices', { choices: [{ delta: { content: 'hello' } }] }],
  ] as const;

  describe.each(formats)('%s 이벤트', (_name, event) => {
    it.each([Error, SyntaxError])('%s를 재호출 없이 원래 객체로 전파하고 뒤 이벤트를 전달하지 않는다', async (ErrorType) => {
      await serveEvents([JSON.stringify(event), JSON.stringify({ content: 'later' })]);
      const sentinel = new ErrorType('consumer failed');
      const chunks: string[] = [];
      const result = streamChat(payload, (chunk) => {
        chunks.push(chunk);
        if (chunks.length === 1) throw sentinel;
      });

      await expect(result).rejects.toBe(sentinel);
      expect(chunks).toEqual(['hello']);
    });
  });

  it('정상 content/delta/choices와 원문 폴백을 전달하고 빈 data와 DONE을 건너뛴다', async () => {
    await serveEvents([
      '', '   ', '[DONE]',
      JSON.stringify({ content: 'content', delta: 'ignored' }),
      JSON.stringify({ delta: 'delta', choices: [{ delta: { content: 'ignored' } }] }),
      JSON.stringify({ choices: [{ delta: { content: 'choices' } }] }),
      JSON.stringify({ content: '' }), '{}', 'plain text', '{invalid json', '[DONE]',
    ]);
    const chunks: string[] = [];
    await streamChat(payload, (chunk) => { chunks.push(chunk); });
    expect(chunks).toEqual(['content', 'delta', 'choices', 'plain text', '{invalid json']);
  });

  it.each([Error, SyntaxError])('원문 폴백 콜백의 %s도 그대로 전파한다', async (ErrorType) => {
    await serveEvents(['plain text', JSON.stringify({ content: 'later' })]);
    const sentinel = new ErrorType('fallback consumer failed');
    const chunks: string[] = [];
    await expect(streamChat(payload, (chunk) => {
      chunks.push(chunk);
      if (chunks.length === 1) throw sentinel;
    })).rejects.toBe(sentinel);
    expect(chunks).toEqual(['plain text']);
  });
});
