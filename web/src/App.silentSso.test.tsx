import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MantineProvider } from '@mantine/core';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type { ReactNode } from 'react';

const apiMocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  patch: vi.fn(),
  put: vi.fn(),
  del: vi.fn(),
}));

vi.mock('./lib/api', () => apiMocks);

vi.mock('./contexts/AuthContext', () => ({
  AuthProvider: ({ children }: { children: ReactNode }) => children,
  useAuth: () => ({ user: null, loading: false, login: vi.fn(), logout: vi.fn(), refresh: vi.fn() }),
}));

import App from './App';

const data = new Map<string, string>();
const assign = vi.fn();

function renderAt(path: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <MantineProvider>
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={[path]}><App /></MemoryRouter>
      </QueryClientProvider>
    </MantineProvider>,
  );
}

describe('보호된 경로의 조용한 SSO', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    data.clear();
    Object.defineProperty(window, 'sessionStorage', {
      configurable: true,
      value: { getItem: (key: string) => data.get(key) ?? null, setItem: (key: string, value: string) => { data.set(key, value); }, removeItem: (key: string) => { data.delete(key); } },
    });
    Object.defineProperty(window, 'location', { configurable: true, value: { ...window.location, assign, origin: 'https://jikim.example' } });
  });

  it('auto_login 이 켜져 있으면 깊은 링크를 들고 prompt=none 으로 한 번 이동한다', async () => {
    apiMocks.get.mockResolvedValue({ oidc_enabled: true, oidc_auto_login: true });
    renderAt('/secrets/abc?tab=versions');
    await waitFor(() => expect(assign).toHaveBeenCalledTimes(1));
    expect(assign).toHaveBeenCalledWith('/api/v1/oidc/login?prompt=none&return_to=%2Fsecrets%2Fabc%3Ftab%3Dversions');
    expect(screen.getByText('Keycloak 세션을 확인하고 있습니다.')).toBeInTheDocument();
    expect(data.get('jikim.sso.silentAttempted')).toBe('true');
  });

  it('auto_login 이 꺼져 있으면 평소처럼 로그인 화면으로 간다', async () => {
    apiMocks.get.mockResolvedValue({ oidc_enabled: true, oidc_auto_login: false });
    renderAt('/dashboard');
    await screen.findByRole('heading', { name: '안전하게 로그인하세요' });
    expect(assign).not.toHaveBeenCalled();
  });

  it('이미 시도한 탭에서는 다시 이동하지 않는다', async () => {
    data.set('jikim.sso.silentAttempted', 'true');
    apiMocks.get.mockResolvedValue({ oidc_enabled: true, oidc_auto_login: true });
    renderAt('/dashboard');
    await screen.findByRole('heading', { name: '안전하게 로그인하세요' });
    expect(assign).not.toHaveBeenCalled();
  });

  it('공개 설정을 읽지 못하면 로그인 화면으로 간다', async () => {
    apiMocks.get.mockRejectedValue(new Error('offline'));
    renderAt('/dashboard');
    await screen.findByRole('heading', { name: '안전하게 로그인하세요' });
    expect(assign).not.toHaveBeenCalled();
  });
});
