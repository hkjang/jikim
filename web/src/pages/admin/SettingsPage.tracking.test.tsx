import { describe, expect, it, vi, beforeEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
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
  testAIIntegration: vi.fn(),
  testWebhookIntegration: vi.fn(),
}));

vi.mock('../../lib/api', () => apiMocks);

vi.mock('../../contexts/AuthContext', () => ({
  useAuth: () => ({ user: { id: '1', username: 'admin', role: 'admin' }, loading: false }),
}));

vi.mock('@mantine/notifications', () => ({ notifications: { show: vi.fn() } }));

import { AdminSettingsPage } from './SettingsPage';

const settingsResponse = {
  general: { service_name: 'jikim', default_language: 'ko', timezone: 'Asia/Seoul' },
  approval: { enabled: false, reviewer_role: 'manager', targets: ['secret_write'] },
  oidc: { enabled: false },
  ai: { enabled: false },
  security: { allow_local_login: true, session_timeout_minutes: 720, password_min_length: 12, audit_retention_days: 180, allowed_networks: '' },
  notifications: { enabled: false, events: [] },
  tracking: { enabled: false },
};

const violations = [
  { origin: 'https://pixel.corp.example', directive: 'img-src', page: 'https://jikim.example/dashboard', count: 4, first_seen: '2026-09-12T01:00:00Z', last_seen: '2026-09-12T01:05:00Z', allowed: false },
  { origin: 'https://momento.corp.example', directive: 'connect-src', page: 'https://jikim.example/secrets', count: 1, first_seen: '2026-09-12T01:00:00Z', last_seen: '2026-09-12T01:01:00Z', allowed: true },
];

function renderPage(): void {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <MantineProvider>
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={['/admin/settings?tab=tracking']}>{children}</MemoryRouter>
      </QueryClientProvider>
    </MantineProvider>
  );
  render(<AdminSettingsPage />, { wrapper: Wrapper });
}

describe('AdminSettingsPage tracking tab', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    apiMocks.get.mockImplementation((path: string) => {
      if (path === '/tracking/violations') return Promise.resolve(violations);
      return Promise.resolve(settingsResponse);
    });
    apiMocks.patch.mockResolvedValue(settingsResponse);
    apiMocks.post.mockResolvedValue({ allowed_hosts: 'https://pixel.corp.example' });
  });

  it('defaults to off with Momento first and explains the policy', async () => {
    renderPage();
    const toggle = await screen.findByLabelText(/방문 추적 사용/);
    expect(toggle).not.toBeChecked();
    expect(screen.getByDisplayValue('Momento (사내 자체 호스팅 수집기)')).toBeInTheDocument();
    expect(screen.getByLabelText(/같은 오리진 프록시 사용/)).toBeChecked();
    expect(screen.getByText(/콘텐츠 보안 정책\(CSP\)은 그대로 잠겨 있습니다/)).toBeInTheDocument();
  });

  it('lists blocked origins and allows one with a click', async () => {
    renderPage();
    await screen.findByText('https://pixel.corp.example');
    expect(screen.getByText('허용됨')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: '허용' }));
    await waitFor(() => expect(apiMocks.post).toHaveBeenCalledWith('/tracking/violations/allow', { origin: 'https://pixel.corp.example' }));
    await waitFor(() => expect(screen.getByLabelText(/추가 허용 출처/)).toHaveValue('https://pixel.corp.example'));
  });

  it('refuses to save a Momento setup that is switched on without a collector', async () => {
    renderPage();
    fireEvent.click(await screen.findByLabelText(/방문 추적 사용/));
    fireEvent.click(screen.getByRole('button', { name: '설정 저장' }));
    expect(await screen.findByText(/수집기 주소와 사이트 ID를 모두 입력하세요/)).toBeInTheDocument();
    expect(apiMocks.patch).not.toHaveBeenCalled();
  });

  it('sends the tracking group with the rest of the settings', async () => {
    renderPage();
    fireEvent.click(await screen.findByLabelText(/방문 추적 사용/));
    fireEvent.change(screen.getByLabelText(/Momento 수집기 주소/), { target: { value: 'https://momento.corp.example' } });
    fireEvent.change(screen.getByLabelText(/Momento 사이트 ID/), { target: { value: 'jikim-prod' } });
    fireEvent.click(screen.getByRole('button', { name: '설정 저장' }));
    await waitFor(() => expect(apiMocks.patch).toHaveBeenCalled());
    const payload = apiMocks.patch.mock.calls[0][1] as { tracking: Record<string, unknown> };
    expect(payload.tracking).toMatchObject({
      enabled: true,
      provider: 'momento',
      momento_url: 'https://momento.corp.example',
      momento_site_id: 'jikim-prod',
      momento_proxy: true,
      include_admin: false,
      placement: 'head',
    });
  });
});
