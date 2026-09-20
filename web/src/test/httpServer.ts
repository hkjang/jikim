/// <reference types="node" />
import { createServer, type RequestListener } from 'node:http';

// Only resolve relative URLs; responses and streams come from native fetch.
export async function startHttpServer(handler: RequestListener) {
  const server = createServer(handler);
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Missing HTTP address');
  const origin = `http://127.0.0.1:${address.port}`;
  const nativeFetch = globalThis.fetch;
  globalThis.fetch = (input, init) => nativeFetch(typeof input === 'string' ? new URL(input, origin) : input, init);
  return async () => {
    globalThis.fetch = nativeFetch;
    await new Promise<void>((resolve, reject) => {
      server.close((error) => error ? reject(error) : resolve());
      server.closeAllConnections();
    });
  };
}
