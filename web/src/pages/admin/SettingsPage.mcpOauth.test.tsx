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

const oidcOn = {
  enabled: true,
  issuer_url: 'https://keycloak.corp.example/realms/corp',
  client_id: 'jikim-web',
  redirect_url: 'https://vault.corp.example/api/v1/oidc/callback',
};

function settingsResponse(oidc: Record<string, unknown>, mcp?: Record<string, unknown>) {
  return {
    general: { service_name: 'jikim', default_language: 'ko', timezone: 'Asia/Seoul' },
    approval: { enabled: false, reviewer_role: 'manager', targets: ['secret_write'] },
    oidc,
    ai: { enabled: false },
    security: { allow_local_login: true, session_timeout_minutes: 720, password_min_length: 12, audit_retention_days: 180, allowed_networks: '' },
    notifications: { enabled: false, events: [] },
    tracking: { enabled: false },
    mcp: mcp ?? { oauth: { enabled: false, scopes: 'mcp:read' } },
  };
}

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

describe('AdminSettingsPage MCP SSO card', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('defaults to off, is locked while OIDC is off, and shows where the metadata lives', async () => {
    apiMocks.get.mockResolvedValue(settingsResponse({ enabled: false }));
    renderPage();
    const toggle = await screen.findByLabelText(/MCP에서 SSO 액세스 토큰 받기/);
    expect(toggle).not.toBeChecked();
    expect(toggle).toBeDisabled();
    expect(screen.getByLabelText(/메타데이터 주소/)).toHaveValue(`${window.location.origin}/.well-known/oauth-protected-resource/mcp`);
    expect(screen.getByLabelText(/범위 \(mcp\.oauth\.scopes\)/)).toHaveValue('mcp:read');
  });

  it('derives the metadata address from the configured resource identifier', async () => {
    apiMocks.get.mockResolvedValue(settingsResponse(oidcOn, { oauth: { enabled: true, resource: 'https://vault.corp.example/mcp', audience: 'claude-mcp', scopes: 'mcp:read mcp:transit' } }));
    renderPage();
    expect(await screen.findByLabelText(/MCP에서 SSO 액세스 토큰 받기/)).toBeChecked();
    expect(screen.getByLabelText(/메타데이터 주소/)).toHaveValue('https://vault.corp.example/.well-known/oauth-protected-resource/mcp');
    expect(screen.getByLabelText(/허용 대상/)).toHaveValue('claude-mcp');
  });

  it('sends the mcp.oauth group under the mcp key with normalized lists', async () => {
    apiMocks.get.mockResolvedValue(settingsResponse(oidcOn));
    apiMocks.patch.mockResolvedValue(settingsResponse(oidcOn));
    renderPage();
    fireEvent.click(await screen.findByLabelText(/MCP에서 SSO 액세스 토큰 받기/));
    fireEvent.change(screen.getByLabelText(/리소스 식별자/), { target: { value: ' https://vault.corp.example/mcp ' } });
    fireEvent.change(screen.getByLabelText(/허용 대상/), { target: { value: '  claude-mcp   cursor-mcp ' } });
    fireEvent.change(screen.getByLabelText(/범위 \(mcp\.oauth\.scopes\)/), { target: { value: 'mcp:read  mcp:transit' } });
    fireEvent.click(screen.getByRole('button', { name: '설정 저장' }));
    await waitFor(() => expect(apiMocks.patch).toHaveBeenCalled());
    const payload = apiMocks.patch.mock.calls[0][1] as { mcp: { oauth: Record<string, unknown> } };
    expect(payload.mcp.oauth).toEqual({
      enabled: true,
      resource: 'https://vault.corp.example/mcp',
      audience: 'claude-mcp cursor-mcp',
      scopes: 'mcp:read mcp:transit',
    });
  });

  it('refuses to save the switch on when OIDC is switched off in the same form', async () => {
    apiMocks.get.mockResolvedValue(settingsResponse(oidcOn));
    renderPage();
    fireEvent.click(await screen.findByLabelText(/MCP에서 SSO 액세스 토큰 받기/));
    fireEvent.click(screen.getByLabelText(/Keycloak SSO 사용/));
    fireEvent.click(screen.getByRole('button', { name: '설정 저장' }));
    expect(await screen.findByText(/MCP SSO\(OAuth\)를 켜려면 Keycloak SSO가 켜져 있고/)).toBeInTheDocument();
    expect(apiMocks.patch).not.toHaveBeenCalled();
  });
});
