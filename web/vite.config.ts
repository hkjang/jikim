import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api/openapi.json': 'http://127.0.0.1:8080',
      '/api/v1': 'http://127.0.0.1:8080',
      '/v1': 'http://127.0.0.1:8080',
      '/mcp': 'http://127.0.0.1:8080',
      // MCP SSO(OAuth) 메타데이터. /.well-known 전체가 아니라 이 접두사만 넘긴다.
      '/.well-known/oauth-protected-resource': 'http://127.0.0.1:8080',
      '/healthz': 'http://127.0.0.1:8080',
      '/readyz': 'http://127.0.0.1:8080',
    },
  },
  build: {
    sourcemap: false,
    target: 'es2022',
  },
});
