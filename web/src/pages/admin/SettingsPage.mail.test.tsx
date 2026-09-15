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
  testMailIntegration: vi.fn(),
}));

vi.mock('../../lib/api', () => apiMocks);

vi.mock('../../contexts/AuthContext', () => ({
  useAuth: () => ({ user: { id: '1', username: 'admin', role: 'admin' }, loading: false }),
}));

vi.mock('@mantine/notifications', () => ({ notifications: { show: vi.fn() } }));

import { AdminSettingsPage } from './SettingsPage';

const baseSettings = {
  general: { service_name: 'jikim', default_language: 'ko', timezone: 'Asia/Seoul' },
  approval: { enabled: false, reviewer_role: 'manager', targets: ['secret_write'] },
  oidc: { enabled: false },
  ai: { enabled: false },
  security: { allow_local_login: true, session_timeout_minutes: 720, password_min_length: 12, audit_retention_days: 180, allowed_networks: '' },
  notifications: { enabled: false, events: [] },
  tracking: { enabled: false },
};

const deliveries = [
  { id: 'd-1', event: 'approval.requested', recipient: 'alice@corp.example', subject: '[jikim] 승인 요청: Secret 생성·변경 — apps/db', status: 'sent', attempts: 1, created_at: '2026-09-16T01:00:00Z', updated_at: '2026-09-16T01:00:01Z' },
  { id: 'd-2', event: 'rotation.failed', recipient: 'bob@corp.example', subject: '[jikim] 회전 실패: apps/db', status: 'failed', attempts: 2, error_message: 'SMTP 연결 실패: connection refused', created_at: '2026-09-16T01:05:00Z', updated_at: '2026-09-16T01:05:04Z' },
];

function renderPage(mail: Record<string, unknown>): void {
  apiMocks.get.mockImplementation((path: string) => {
    if (path.startsWith('/integrations/mail/deliveries')) return Promise.resolve(deliveries);
    return Promise.resolve({ ...baseSettings, mail });
  });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <MantineProvider>
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={['/admin/settings?tab=mail']}>{children}</MemoryRouter>
      </QueryClientProvider>
    </MantineProvider>
  );
  render(<AdminSettingsPage />, { wrapper: Wrapper });
}

describe('AdminSettingsPage mail tab', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    apiMocks.patch.mockResolvedValue(baseSettings);
  });

  it('defaults to off with an internal relay on port 25 and no credentials', async () => {
    renderPage({ enabled: false, password_configured: false });
    expect(await screen.findByLabelText(/메일 알림 사용/)).not.toBeChecked();
    expect(screen.getByLabelText('포트')).toHaveValue('25');
    expect(screen.getByDisplayValue('자동 (서버가 알리면 STARTTLS)')).toBeInTheDocument();
    expect(screen.getByLabelText(/사용자 이름/)).toHaveValue('');
    expect(screen.getByLabelText(/TLS 인증서 검증 건너뛰기/)).not.toBeChecked();
    expect(screen.getByRole('button', { name: '테스트 메일 보내기' })).toBeDisabled();
  });

  it('shows the password only as configured and never echoes it', async () => {
    renderPage({ enabled: true, smtp_host: 'relay.corp.example', username: 'svc', password_configured: true });
    expect(await screen.findByText('비밀번호 설정됨')).toBeInTheDocument();
    expect(screen.getByLabelText(/비밀번호 \(선택\)/)).toHaveValue('');
    expect(screen.getByPlaceholderText(/설정됨/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: '설정 저장' }));
    await waitFor(() => expect(apiMocks.patch).toHaveBeenCalled());
    const payload = apiMocks.patch.mock.calls[0][1] as { mail: Record<string, unknown> };
    expect(payload.mail).toMatchObject({ enabled: true, smtp_host: 'relay.corp.example', smtp_port: 25, security: 'auto', username: 'svc', clear_password: false });
    expect(payload.mail).not.toHaveProperty('password');
    expect(payload.mail).not.toHaveProperty('password_configured');
  });

  it('refuses to turn mail on without a relay host', async () => {
    renderPage({ enabled: false });
    fireEvent.click(await screen.findByLabelText(/메일 알림 사용/));
    fireEvent.click(screen.getByRole('button', { name: '설정 저장' }));
    expect(await screen.findByText(/SMTP 릴레이 주소를 입력하세요/)).toBeInTheDocument();
    expect(apiMocks.patch).not.toHaveBeenCalled();
  });

  it('sends a test mail with the saved settings and shows the relay outcome in place', async () => {
    apiMocks.testMailIntegration.mockResolvedValue({ ok: false, message: '릴레이가 메일을 받지 않았습니다: SMTP 연결 실패: connection refused', latency_ms: 12 });
    renderPage({ enabled: true, smtp_host: 'relay.corp.example' });
    const button = await screen.findByRole('button', { name: '테스트 메일 보내기' });
    expect(button).toBeEnabled();
    fireEvent.change(screen.getByLabelText(/받는 사람/), { target: { value: 'ops@corp.example' } });
    fireEvent.click(button);
    await waitFor(() => expect(apiMocks.testMailIntegration).toHaveBeenCalledWith('ops@corp.example'));
    const result = await screen.findByRole('status');
    expect(result).toHaveTextContent('테스트 메일을 보내지 못했습니다');
    expect(result).toHaveTextContent('connection refused');
  });

  it('lists sent and failed deliveries without a body', async () => {
    renderPage({ enabled: true, smtp_host: 'relay.corp.example' });
    const table = await screen.findByRole('table', { name: '메일 발송 기록' });
    expect(table).toHaveTextContent('alice@corp.example');
    expect(table).toHaveTextContent('발송');
    expect(table).toHaveTextContent('실패');
    expect(table).toHaveTextContent('2회 시도');
    expect(table).toHaveTextContent('connection refused');
  });
});
