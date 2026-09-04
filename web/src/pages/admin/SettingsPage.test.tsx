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

const issuerUrl = 'https://keycloak.intra/realms/jikim';

const settingsResponse = {
  general: { service_name: 'jikim', default_language: 'ko', timezone: 'Asia/Seoul' },
  approval: { enabled: false, reviewer_role: 'manager', targets: ['secret_write'] },
  oidc: {
    enabled: true,
    issuer_url: issuerUrl,
    client_id: 'jikim-web',
    client_secret_configured: true,
    scopes: ['openid', 'profile', 'email', 'groups'],
    group_claim: 'groups',
    role_claim: 'roles',
    username_claim: 'preferred_username',
    allow_insecure_http: false,
  },
  ai: { enabled: false, base_url: '', auth_type: 'bearer', model: '', max_tokens: 4096, timeout_seconds: 600 },
  security: { allow_local_login: true, session_timeout_minutes: 720, password_min_length: 12, audit_retention_days: 180, allowed_networks: '' },
  notifications: { enabled: false, events: [] },
};

function renderPage(): void {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <MantineProvider>
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={['/admin/settings?tab=oidc']}>{children}</MemoryRouter>
      </QueryClientProvider>
    </MantineProvider>
  );
  render(<AdminSettingsPage />, { wrapper: Wrapper });
}

async function runDiscoveryTest(): Promise<void> {
  fireEvent.click(screen.getByRole('button', { name: /Discovery 연결 테스트/ }));
  await screen.findByText('연결 확인 완료');
}

describe('AdminSettingsPage OIDC tab', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    apiMocks.get.mockResolvedValue(settingsResponse);
    apiMocks.post.mockResolvedValue({ connected: true, issuer: issuerUrl });
  });

  it('keeps rendering while typing in Issuer URL after a discovery test result', async () => {
    renderPage();
    const issuer = await screen.findByLabelText(/Issuer URL/);
    // A pending second state update makes React defer the setSettings updater to
    // the render phase, where the pooled event's currentTarget is already null.
    await runDiscoveryTest();

    fireEvent.change(issuer, { target: { value: `${issuerUrl}-2` } });

    // Before the fix this threw "Cannot read properties of null (reading 'value')"
    // during render and unmounted the whole tree (blank screen).
    await waitFor(() => expect(screen.getByLabelText(/Issuer URL/)).toHaveValue(`${issuerUrl}-2`));
    expect(screen.getByText(/Issuer discovery로 Keycloak SSO 연결 정보를/)).toBeInTheDocument();
  });

  it('clears the stale discovery result when the Issuer URL changes', async () => {
    renderPage();
    const issuer = await screen.findByLabelText(/Issuer URL/);
    await runDiscoveryTest();

    fireEvent.change(issuer, { target: { value: `${issuerUrl}-changed` } });
    await waitFor(() => expect(screen.queryByText('연결 확인 완료')).not.toBeInTheDocument());
  });

  it('keeps other OIDC inputs editable', async () => {
    renderPage();
    const clientId = await screen.findByLabelText(/Client ID/);

    fireEvent.change(clientId, { target: { value: 'jikim-admin' } });
    await waitFor(() => expect(screen.getByLabelText(/Client ID/)).toHaveValue('jikim-admin'));
    expect(screen.getByText(/Issuer discovery로 Keycloak SSO 연결 정보를/)).toBeInTheDocument();
  });
});
