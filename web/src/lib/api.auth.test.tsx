import { act, cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { AuthProvider, useAuth } from '../contexts/AuthContext';
import { api, streamChat } from './api';
import { startHttpServer } from '../test/httpServer';

function Session() {
  const { user, loading } = useAuth();
  return <div>{loading ? 'loading' : user ? user.username : 'guest'}</div>;
}

describe('HTTP 오류와 실제 AuthProvider', () => {
  afterEach(cleanup);

  for (const client of ['api', 'streamChat'] as const) {
    it.each([400, 401, 403, 429, 500, 503])(`${client} HTTP %i의 세션 처리`, async (status) => {
      const stop = await startHttpServer((request, response) => {
        response.setHeader('Content-Type', 'application/json');
        if (request.url === '/api/v1/session') {
          response.end(JSON.stringify({ data: { authenticated: true, user: { id: 'user-1', username: 'tester' } } }));
        } else {
          response.writeHead(status);
          response.end(JSON.stringify({ error: { message: '요청 실패' } }));
        }
      });
      let events = 0;
      const onUnauthorized = () => { events++; };
      window.addEventListener('jikim:unauthorized', onUnauthorized);
      try {
        render(<AuthProvider><Session /></AuthProvider>);
        await screen.findByText('tester');
        await act(async () => {
          const request = client === 'api' ? api('/ai/chat') : streamChat({ messages: [] }, () => {});
          await expect(request).rejects.toMatchObject({ status });
        });
        expect(screen.getByText(status === 401 ? 'guest' : 'tester')).toBeInTheDocument();
        expect(events).toBe(status === 401 ? 1 : 0);
      } finally {
        cleanup();
        window.removeEventListener('jikim:unauthorized', onUnauthorized);
        await stop();
      }
    });
  }
});
