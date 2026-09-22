// @vitest-environment node
/// <reference types="node" />

import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer, type Server } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { createServer as createViteServer, loadConfigFromFile, type ViteDevServer } from 'vite';

const root = fileURLToPath(new URL('../../', import.meta.url));
const received: { method?: string; url?: string; accept?: string }[] = [];
let upstream: Server | undefined;
let vite: ViteDevServer | undefined;
let cacheDir: string | undefined;
let origin: string;

beforeAll(async () => {
  cacheDir = await mkdtemp(join(tmpdir(), 'jikim-vite-proxy-'));
  upstream = createServer((request, response) => {
    request.resume();
    const arrival = { method: request.method, url: request.url, accept: request.headers.accept };
    received.push(arrival);
    response.writeHead(200, { 'Content-Type': 'application/json' });
    // Transport fixture only; the Go tests verify the actual OpenAPI document.
    response.end(JSON.stringify({ upstream: true, ...arrival }));
  });
  upstream.listen(0, '127.0.0.1');
  await once(upstream, 'listening');
  const address = upstream.address();
  if (!address || typeof address === 'string') throw new Error('Expected upstream TCP address');
  const target = `http://127.0.0.1:${address.port}`;
  const loaded = await loadConfigFromFile(
    { command: 'serve', mode: 'development' }, join(root, 'vite.config.ts'), root,
    undefined, undefined, 'runner',
  );
  if (!loaded) throw new Error('Vite config was not loaded');
  // Preserve production matchers and options; only redirect the upstream port.
  const proxy = Object.fromEntries(Object.entries(loaded.config.server?.proxy ?? {}).map(
    ([path, options]) => [path, typeof options === 'string' ? target : { ...options, target }],
  ));
  vite = await createViteServer({
    ...loaded.config, configFile: false, root, cacheDir,
    optimizeDeps: { noDiscovery: true, include: [] },
    server: { ...loaded.config.server, host: '127.0.0.1', port: 0, proxy, open: false },
  });
  await vite.listen();
  const viteAddress = vite.httpServer?.address();
  if (!viteAddress || typeof viteAddress === 'string') throw new Error('Expected Vite TCP address');
  origin = `http://127.0.0.1:${viteAddress.port}`;
}, 30_000);

afterAll(async () => {
  try {
    await vite?.close();
  } finally {
    try {
      if (upstream) {
        const server = upstream;
        await new Promise<void>((resolve, reject) => {
          server.close((error) => error ? reject(error) : resolve());
          server.closeAllConnections();
        });
      }
    } finally {
      if (cacheDir) await rm(cacheDir, { recursive: true, force: true });
    }
  }
});

describe('Vite development proxy over HTTP', () => {
  it.each(['text/html', 'application/json'])('forwards OpenAPI with Accept: %s', async (accept) => {
    const path = '/api/openapi.json?download=1&name=a%2Fb';
    const start = received.length;
    const response = await fetch(origin + path, { headers: { Accept: accept } });
    const body = await response.text();
    expect(response.status).toBe(200);
    expect(response.headers.get('content-type')).toContain('application/json');
    const arrival = { method: 'GET', url: path, accept };
    expect(received.slice(start)).toEqual([arrival]);
    expect(JSON.parse(body)).toEqual({ upstream: true, ...arrival });
  });

  it.each(['/api/v1/version', '/v1/sys/health', '/mcp', '/healthz', '/readyz'])(
    'preserves existing proxy for %s', async (path) => {
      const url = `${path}?probe=a%2Fb&probe=two`;
      const start = received.length;
      const response = await fetch(origin + url, { headers: { Accept: 'application/json' } });
      expect(response.status).toBe(200);
      expect(response.headers.get('content-type')).toContain('application/json');
      const arrival = { method: 'GET', url, accept: 'application/json' };
      expect(await response.json()).toEqual({ upstream: true, ...arrival });
      expect(received.slice(start)).toEqual([arrival]);
    },
  );

  it.each(['/api-explorer', '/api/unrelated'])('keeps %s on the SPA server', async (path) => {
    const start = received.length;
    const response = await fetch(origin + path, { headers: { Accept: 'text/html' } });
    expect(response.status).toBe(200);
    expect(response.headers.get('content-type')).toContain('text/html');
    expect(await response.text()).toContain('<div id="root"></div>');
    expect(received.slice(start)).toEqual([]);
  });
});
