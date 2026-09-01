import { useMemo, useState, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Alert,
  Badge,
  Box,
  Button,
  Checkbox,
  Divider,
  Group,
  Loader,
  NumberInput,
  Paper,
  PasswordInput,
  Select,
  SimpleGrid,
  Skeleton,
  Stack,
  Switch,
  Tabs,
  Text,
  TextInput,
  ThemeIcon,
  Title,
} from '@mantine/core';
import { notifications } from '@mantine/notifications';
import {
  Bell,
  Bot,
  CheckCircle2,
  CircleAlert,
  KeyRound,
  Languages,
  RefreshCw,
  RotateCcw,
  Save,
  Settings2,
  ShieldCheck,
  UsersRound,
} from 'lucide-react';
import { get, patch, post } from '../../lib/api';
import type { SystemSettings } from '../../lib/types';
import { useAuth } from '../../contexts/AuthContext';
import { useSearchParams } from 'react-router-dom';

type SettingsTab = 'general' | 'approval' | 'oidc' | 'ai' | 'security' | 'notifications';

interface GeneralSettings {
  service_name: string;
  default_language: string;
  timezone: string;
}

interface ApprovalSettings {
  enabled: boolean;
  reviewer_role: string;
  four_eyes: boolean;
  required_approvals: number;
  targets: string[];
}

interface OidcSettings {
  enabled: boolean;
  issuer_url: string;
  client_id: string;
  client_secret: string;
  client_secret_configured: boolean;
  scopes: string;
  group_claim: string;
  role_claim: string;
  username_claim: string;
}

interface AiSettings {
  enabled: boolean;
  base_url: string;
  api_key: string;
  api_key_configured: boolean;
  model: string;
  max_tokens: number;
  timeout_seconds: number;
}

interface SecuritySettings {
  allow_local_login: boolean;
  require_password_change: boolean;
  session_timeout_minutes: number;
  password_min_length: number;
  audit_retention_days: number;
  allowed_networks: string;
}

interface NotificationSettings {
  enabled: boolean;
  webhook_url: string;
  email_recipients: string;
  events: string[];
}

interface AdminSettings {
  general: GeneralSettings;
  approval: ApprovalSettings;
  oidc: OidcSettings;
  ai: AiSettings;
  security: SecuritySettings;
  notifications: NotificationSettings;
}

interface OidcTestResult {
  success?: boolean;
  connected?: boolean;
  message?: string;
  issuer?: string;
  authorization_endpoint?: string;
  token_endpoint?: string;
  userinfo_endpoint?: string;
  discovery?: {
    authorization_endpoint?: string;
    token_endpoint?: string;
    userinfo_endpoint?: string;
  };
}

const bootstrapVariables = [
  { name: 'POSTGRES_DSN', description: 'PostgreSQL 연결 정보' },
  { name: 'BOOTSTRAP_ADMIN', description: '최초 관리자 계정' },
  { name: 'BOOTSTRAP_ADMIN_PASSWORD', description: '최초 관리자 비밀번호' },
  { name: 'ENCRYPTION_KEY', description: '루트 암호화 키(KEK)' },
] as const;

const approvalTargets = [
  { value: 'secret_write', label: 'Secret 생성·변경' },
  { value: 'secret_delete', label: 'Secret 폐기' },
];

const notificationEvents = [
  { value: 'approval_requested', label: '승인 요청' },
  { value: 'rotation_failed', label: '키·Secret 회전 실패' },
  { value: 'security_alert', label: '보안 경보' },
  { value: 'certificate_expiring', label: '인증서 만료 예정' },
];

function record(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value)
    ? value as Record<string, unknown>
    : {};
}

function stringValue(value: unknown, fallback = ''): string {
  return typeof value === 'string' ? value : fallback;
}

function booleanValue(value: unknown, fallback = false): boolean {
  return typeof value === 'boolean' ? value : fallback;
}

function numberValue(value: unknown, fallback: number): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : fallback;
}

function stringList(value: unknown, fallback: string[] = []): string[] {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : fallback;
}

function normalizeSettings(response: SystemSettings | unknown): AdminSettings {
  const envelope = record(response);
  const root = record(envelope.settings ?? envelope);
  const general = record(root.general);
  const approval = record(root.approval);
  const oidc = record(root.oidc);
  const ai = record(root.ai);
  const security = record(root.security);
  const notification = record(root.notifications);

  return {
    general: {
      service_name: stringValue(general.service_name, 'jikim'),
      default_language: stringValue(general.default_language, 'ko'),
      timezone: stringValue(general.timezone, 'Asia/Seoul'),
    },
    approval: {
      enabled: booleanValue(approval.enabled ?? approval.approval_enabled),
      reviewer_role: stringValue(approval.reviewer_role, 'manager'),
      four_eyes: true,
      required_approvals: 1,
      targets: stringList(approval.targets, ['secret_write', 'secret_delete']).filter((target) => target === 'secret_write' || target === 'secret_delete'),
    },
    oidc: {
      enabled: booleanValue(oidc.enabled),
      issuer_url: stringValue(oidc.issuer_url),
      client_id: stringValue(oidc.client_id),
      client_secret: '',
      client_secret_configured: booleanValue(oidc.client_secret_configured ?? oidc.has_client_secret),
      scopes: Array.isArray(oidc.scopes) ? stringList(oidc.scopes).join(' ') : stringValue(oidc.scopes, 'openid profile email groups'),
      group_claim: stringValue(oidc.group_claim, 'groups'),
      role_claim: stringValue(oidc.role_claim, 'roles'),
      username_claim: stringValue(oidc.username_claim, 'preferred_username'),
    },
    ai: {
      enabled: booleanValue(ai.enabled),
      base_url: stringValue(ai.base_url),
      api_key: '',
      api_key_configured: booleanValue(ai.api_key_configured ?? ai.has_api_key),
      model: stringValue(ai.model),
      max_tokens: numberValue(ai.max_tokens, 4096),
      timeout_seconds: numberValue(ai.timeout_seconds, 600),
    },
    security: {
      allow_local_login: booleanValue(security.allow_local_login, true),
      require_password_change: booleanValue(security.require_password_change, false),
      session_timeout_minutes: numberValue(security.session_timeout_minutes, 720),
      password_min_length: numberValue(security.password_min_length, 12),
      audit_retention_days: numberValue(security.audit_retention_days, 180),
      allowed_networks: stringValue(security.allowed_networks),
    },
    notifications: {
      enabled: booleanValue(notification.enabled),
      webhook_url: stringValue(notification.webhook_url),
      email_recipients: stringValue(notification.email_recipients),
      events: stringList(notification.events, ['approval_requested', 'rotation_failed', 'security_alert']),
    },
  };
}

function settingsPayload(settings: AdminSettings): Record<string, unknown> {
  const oidc: Record<string, unknown> = {
    enabled: settings.oidc.enabled,
    issuer_url: settings.oidc.issuer_url.trim(),
    client_id: settings.oidc.client_id.trim(),
    scopes: settings.oidc.scopes.trim().split(/\s+/).filter(Boolean),
    group_claim: settings.oidc.group_claim.trim(),
    role_claim: settings.oidc.role_claim.trim(),
    username_claim: settings.oidc.username_claim.trim(),
  };
  if (settings.oidc.client_secret.trim()) oidc.client_secret = settings.oidc.client_secret;

  const ai: Record<string, unknown> = {
    enabled: settings.ai.enabled,
    base_url: settings.ai.base_url.trim(),
    model: settings.ai.model.trim(),
    streaming: true,
    max_tokens: settings.ai.max_tokens,
    timeout_seconds: settings.ai.timeout_seconds,
  };
  if (settings.ai.api_key.trim()) ai.api_key = settings.ai.api_key;

  return {
    general: settings.general,
    approval: { ...settings.approval, approval_enabled: settings.approval.enabled },
    oidc,
    ai,
    security: settings.security,
    notifications: settings.notifications,
  };
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : '요청을 처리하지 못했습니다.';
}

function SettingsLoading() {
  return (
    <Stack gap="lg" aria-label="설정을 불러오는 중">
      <Skeleton height={42} width="min(520px, 100%)" />
      <Skeleton height={58} />
      <Skeleton height={340} radius="md" />
    </Stack>
  );
}

function SectionHeading({ icon, title, description }: { icon: React.ReactNode; title: string; description: string }) {
  return (
    <Group align="flex-start" wrap="nowrap">
      <ThemeIcon size="lg" variant="light" aria-hidden="true">{icon}</ThemeIcon>
      <Box>
        <Title order={3} size="h4">{title}</Title>
        <Text c="dimmed" mt={3}>{description}</Text>
      </Box>
    </Group>
  );
}

function AdminSettingsForm({ initialSettings, initialTab = 'general' }: { initialSettings: AdminSettings; initialTab?: SettingsTab }) {
  const queryClient = useQueryClient();
  const [activeTab, setActiveTab] = useState<SettingsTab>(initialTab);
  const [settings, setSettings] = useState(initialSettings);
  const [oidcResult, setOidcResult] = useState<OidcTestResult | null>(null);

  const saveMutation = useMutation({
    mutationFn: () => patch<unknown>('/settings', settingsPayload(settings)),
    onSuccess: async () => {
      setSettings((current) => ({
        ...current,
        oidc: {
          ...current.oidc,
          client_secret: '',
          client_secret_configured: current.oidc.client_secret_configured || Boolean(current.oidc.client_secret),
        },
        ai: {
          ...current.ai,
          api_key: '',
          api_key_configured: current.ai.api_key_configured || Boolean(current.ai.api_key),
        },
      }));
      await queryClient.invalidateQueries({ queryKey: ['admin-settings'] });
      notifications.show({ color: 'teal', title: '설정 저장 완료', message: '변경한 관리 설정을 안전하게 저장했습니다.' });
    },
  });

  const oidcTestMutation = useMutation({
    mutationFn: async () => {
      const payload = settingsPayload(settings).oidc as Record<string, unknown>;
      return post<OidcTestResult>('/oidc/test', payload);
    },
    onSuccess: (result) => {
      const normalized = {
        ...result,
        success: result.connected ?? result.success ?? false,
        authorization_endpoint: result.authorization_endpoint ?? result.discovery?.authorization_endpoint,
        token_endpoint: result.token_endpoint ?? result.discovery?.token_endpoint,
        userinfo_endpoint: result.userinfo_endpoint ?? result.discovery?.userinfo_endpoint,
      };
      setOidcResult(normalized);
      notifications.show({
        color: normalized.success ? 'teal' : 'red',
        title: normalized.success ? 'OIDC 연결 성공' : 'OIDC 연결 실패',
        message: normalized.message || (normalized.success ? 'Keycloak discovery 정보를 확인했습니다.' : '설정을 확인해 주세요.'),
      });
    },
    onError: (error) => setOidcResult({ success: false, message: errorMessage(error) }),
  });

  const redirectUrl = useMemo(
    () => `${window.location.origin}/api/v1/oidc/callback`,
    [],
  );

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    saveMutation.mutate();
  };

  return (
    <form onSubmit={submit} noValidate>
      <Stack gap="lg">
        <Group justify="space-between" align="flex-end">
          <Box>
            <Title order={1} className="page-title">서비스 관리 설정</Title>
            <Text c="dimmed" mt={6}>인증, 승인, AI와 보안 정책을 한 곳에서 관리합니다.</Text>
          </Box>
          <Group gap="sm">
            <Button
              type="button"
              variant="default"
              leftSection={<RotateCcw size={17} />}
              onClick={() => {
                setSettings(initialSettings);
                setOidcResult(null);
              }}
              disabled={saveMutation.isPending}
            >
              변경 취소
            </Button>
            <Button type="submit" leftSection={<Save size={17} />} loading={saveMutation.isPending}>
              설정 저장
            </Button>
          </Group>
        </Group>

        {saveMutation.isError && (
          <Alert icon={<CircleAlert size={18} />} color="red" title="설정을 저장하지 못했습니다" role="alert">
            {errorMessage(saveMutation.error)}
          </Alert>
        )}

        <Paper className="surface" radius="lg" p={{ base: 'md', sm: 'xl' }}>
          <Tabs value={activeTab} onChange={(value) => setActiveTab((value || 'general') as SettingsTab)} keepMounted={false}>
            <Tabs.List style={{ overflowX: 'auto', flexWrap: 'nowrap' }} aria-label="관리 설정 분류">
              <Tabs.Tab value="general" leftSection={<Settings2 size={16} />} style={{ flexShrink: 0 }}>일반</Tabs.Tab>
              <Tabs.Tab value="approval" leftSection={<UsersRound size={16} />} style={{ flexShrink: 0 }}>
                승인 워크플로
                <Badge ml={8} size="xs" color={settings.approval.enabled ? 'teal' : 'gray'}>
                  {settings.approval.enabled ? '사용' : '미사용'}
                </Badge>
              </Tabs.Tab>
              <Tabs.Tab value="oidc" leftSection={<KeyRound size={16} />} style={{ flexShrink: 0 }}>Keycloak OIDC</Tabs.Tab>
              <Tabs.Tab value="ai" leftSection={<Bot size={16} />} style={{ flexShrink: 0 }}>AI</Tabs.Tab>
              <Tabs.Tab value="security" leftSection={<ShieldCheck size={16} />} style={{ flexShrink: 0 }}>보안</Tabs.Tab>
              <Tabs.Tab value="notifications" leftSection={<Bell size={16} />} style={{ flexShrink: 0 }}>알림</Tabs.Tab>
            </Tabs.List>

            <Tabs.Panel value="general" pt="xl">
              <Stack gap="xl">
                <SectionHeading icon={<Languages size={20} />} title="일반 설정" description="v0.1.0의 제품명과 한국어 운영 기준을 확인합니다." />
                <SimpleGrid cols={{ base: 1, md: 2 }} spacing="lg">
                  <TextInput
                    label="서비스 표시 이름"
                    description="브라우저와 관리 화면에 표시됩니다."
                    required
                    readOnly
                    value={settings.general.service_name}
                    onChange={(event) => setSettings((current) => ({
                      ...current,
                      general: { ...current.general, service_name: event.currentTarget.value },
                    }))}
                  />
                  <Select
                    label="기본 언어"
                    description="신규 사용자의 기본 UI 언어입니다."
                    data={[{ value: 'ko', label: '한국어' }]}
                    disabled
                    value={settings.general.default_language}
                    onChange={(value) => setSettings((current) => ({
                      ...current,
                      general: { ...current.general, default_language: value || 'ko' },
                    }))}
                  />
                  <TextInput
                    label="표준 시간대"
                    description="IANA 시간대 이름을 입력하세요."
                    placeholder="Asia/Seoul"
                    readOnly
                    value={settings.general.timezone}
                    onChange={(event) => setSettings((current) => ({
                      ...current,
                      general: { ...current.general, timezone: event.currentTarget.value },
                    }))}
                  />
                </SimpleGrid>

                <Divider />
                <Box>
                  <Group gap="xs" mb="xs">
                    <ShieldCheck size={19} aria-hidden="true" />
                    <Title order={3} size="h4">신뢰 기반 환경변수</Title>
                  </Group>
                  <Alert color="blue" variant="light" mb="md">
                    아래 4개 값은 부팅 신뢰 경계를 구성하므로 관리자 화면에서 조회하거나 변경할 수 없습니다. 값 변경은 운영 환경의 안전한 배포 절차로 수행하세요.
                  </Alert>
                  <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="sm">
                    {bootstrapVariables.map((variable) => (
                      <Paper key={variable.name} withBorder p="md" radius="md">
                        <Group justify="space-between" align="flex-start" wrap="nowrap">
                          <Box style={{ minWidth: 0 }}>
                            <Text fw={700} ff="monospace" style={{ overflowWrap: 'anywhere' }}>{variable.name}</Text>
                            <Text size="sm" c="dimmed" mt={4}>{variable.description}</Text>
                          </Box>
                          <Badge color="gray" variant="light" style={{ flexShrink: 0 }}>읽기 전용</Badge>
                        </Group>
                      </Paper>
                    ))}
                  </SimpleGrid>
                </Box>
              </Stack>
            </Tabs.Panel>

            <Tabs.Panel value="approval" pt="xl">
              <Stack gap="xl">
                <SectionHeading icon={<UsersRound size={20} />} title="승인 워크플로" description="필요할 때만 팀장 검토 및 승인 절차를 적용합니다." />
                <Switch
                  size="md"
                  checked={settings.approval.enabled}
                  onChange={(event) => setSettings((current) => ({
                    ...current,
                    approval: { ...current.approval, enabled: event.currentTarget.checked },
                  }))}
                  label="검토·승인 워크플로 사용"
                  description="끄면 신규 작업은 승인 상태를 만들지 않고 즉시 실행되며 관련 프로세스가 사용자 화면에서 제외됩니다."
                />

                {!settings.approval.enabled ? (
                  <Alert icon={<CheckCircle2 size={18} />} color="teal" title="승인 프로세스가 제외됩니다">
                    저장 후 요청·검토·승인·반려 단계와 메뉴를 사용하지 않습니다. 기존 대기 요청은 관리자가 별도로 정리해야 합니다.
                  </Alert>
                ) : (
                  <Stack gap="lg">
                    <Alert color="orange" title="요청자와 승인자를 분리하세요">
                      중요 Secret 작업에는 Four-eyes 원칙을 적용합니다. 모든 결정은 감사 로그에 기록됩니다.
                    </Alert>
                    <SimpleGrid cols={{ base: 1, md: 2 }} spacing="lg">
                      <Select
                        label="기본 검토 역할"
                        description="승인 요청을 검토할 역할입니다."
                        data={[
                          { value: 'manager', label: '팀장' },
                          { value: 'admin', label: '서비스 관리자' },
                        ]}
                        value={settings.approval.reviewer_role}
                        onChange={(value) => setSettings((current) => ({
                          ...current,
                          approval: { ...current.approval, reviewer_role: value || 'manager' },
                        }))}
                      />
                      <NumberInput
                        label="필수 승인 수"
                        description="작업 실행에 필요한 승인 인원입니다."
                        min={1}
                        max={1}
                        disabled
                        clampBehavior="strict"
                        value={settings.approval.required_approvals}
                        onChange={(value) => setSettings((current) => ({
                          ...current,
                          approval: { ...current.approval, required_approvals: typeof value === 'number' ? value : 1 },
                        }))}
                      />
                    </SimpleGrid>
                    <Switch
                      checked
                      disabled
                      label="요청자 본인 승인 금지"
                      description="요청자와 승인자가 반드시 다르도록 항상 강제합니다. v0.1.0은 1인 승인을 지원합니다."
                    />
                    <Checkbox.Group
                      label="승인 적용 작업"
                      description="선택한 작업만 승인 대기 상태로 전환합니다."
                      value={settings.approval.targets}
                      onChange={(targets) => setSettings((current) => ({
                        ...current,
                        approval: { ...current.approval, targets },
                      }))}
                    >
                      <SimpleGrid cols={{ base: 1, sm: 2 }} mt="sm">
                        {approvalTargets.map((target) => <Checkbox key={target.value} value={target.value} label={target.label} />)}
                      </SimpleGrid>
                    </Checkbox.Group>
                  </Stack>
                )}
              </Stack>
            </Tabs.Panel>

            <Tabs.Panel value="oidc" pt="xl">
              <Stack gap="xl">
                <SectionHeading icon={<KeyRound size={20} />} title="Keycloak OIDC" description="Issuer discovery로 Keycloak SSO 연결 정보를 자동 확인합니다." />
                <Switch
                  size="md"
                  checked={settings.oidc.enabled}
                  onChange={(event) => setSettings((current) => ({
                    ...current,
                    oidc: { ...current.oidc, enabled: event.currentTarget.checked },
                  }))}
                  label="Keycloak SSO 사용"
                  description="연결 테스트와 저장을 완료한 뒤 로그인 화면에 SSO가 표시됩니다."
                />
                <SimpleGrid cols={{ base: 1, md: 2 }} spacing="lg">
                  <TextInput
                    type="url"
                    label="Issuer URL"
                    description="realm을 포함한 URL이며 discovery 주소는 자동으로 구성됩니다."
                    placeholder="https://keycloak.intra/realms/jikim"
                    required={settings.oidc.enabled}
                    value={settings.oidc.issuer_url}
                    onChange={(event) => {
                      setOidcResult(null);
                      setSettings((current) => ({ ...current, oidc: { ...current.oidc, issuer_url: event.currentTarget.value } }));
                    }}
                  />
                  <TextInput
                    label="Client ID"
                    required={settings.oidc.enabled}
                    autoComplete="off"
                    value={settings.oidc.client_id}
                    onChange={(event) => setSettings((current) => ({
                      ...current,
                      oidc: { ...current.oidc, client_id: event.currentTarget.value },
                    }))}
                  />
                  <PasswordInput
                    label="Client Secret"
                    description={settings.oidc.client_secret_configured ? '저장된 값이 있습니다. 비워두면 기존 값을 유지합니다.' : '저장 시 암호화되며 다시 표시되지 않습니다.'}
                    placeholder={settings.oidc.client_secret_configured ? '•••••••••••• (설정됨)' : 'Client Secret 입력'}
                    autoComplete="new-password"
                    value={settings.oidc.client_secret}
                    onChange={(event) => setSettings((current) => ({
                      ...current,
                      oidc: { ...current.oidc, client_secret: event.currentTarget.value },
                    }))}
                  />
                  <TextInput label="Redirect URL" description="Keycloak client의 Valid redirect URI에 등록하세요." value={redirectUrl} readOnly />
                  <TextInput
                    label="Scopes"
                    description="공백으로 구분합니다."
                    value={settings.oidc.scopes}
                    onChange={(event) => setSettings((current) => ({
                      ...current,
                      oidc: { ...current.oidc, scopes: event.currentTarget.value },
                    }))}
                  />
                  <TextInput
                    label="사용자명 Claim"
                    value={settings.oidc.username_claim}
                    onChange={(event) => setSettings((current) => ({
                      ...current,
                      oidc: { ...current.oidc, username_claim: event.currentTarget.value },
                    }))}
                  />
                  <TextInput
                    label="그룹 Claim"
                    value={settings.oidc.group_claim}
                    onChange={(event) => setSettings((current) => ({
                      ...current,
                      oidc: { ...current.oidc, group_claim: event.currentTarget.value },
                    }))}
                  />
                  <TextInput
                    label="역할 Claim"
                    value={settings.oidc.role_claim}
                    onChange={(event) => setSettings((current) => ({
                      ...current,
                      oidc: { ...current.oidc, role_claim: event.currentTarget.value },
                    }))}
                  />
                </SimpleGrid>
                <Group>
                  <Button
                    type="button"
                    variant="light"
                    leftSection={oidcTestMutation.isPending ? <Loader size={16} /> : <RefreshCw size={17} />}
                    loading={oidcTestMutation.isPending}
                    disabled={!settings.oidc.issuer_url.trim() || !settings.oidc.client_id.trim()}
                    onClick={() => oidcTestMutation.mutate()}
                  >
                    Discovery 연결 테스트
                  </Button>
                  {settings.oidc.client_secret_configured && <Badge color="teal">Client Secret 설정됨</Badge>}
                </Group>
                {oidcResult && (
                  <Alert
                    role="status"
                    icon={oidcResult.success ? <CheckCircle2 size={18} /> : <CircleAlert size={18} />}
                    color={oidcResult.success ? 'teal' : 'red'}
                    title={oidcResult.success ? '연결 확인 완료' : '연결 확인 실패'}
                  >
                    <Stack gap={4}>
                      <Text>{oidcResult.message || (oidcResult.success ? 'OIDC discovery 응답을 확인했습니다.' : '입력한 설정을 확인하세요.')}</Text>
                      {oidcResult.issuer && <Text size="sm">확인된 Issuer: {oidcResult.issuer}</Text>}
                      {oidcResult.authorization_endpoint && <Text size="sm" style={{ overflowWrap: 'anywhere' }}>Authorization: {oidcResult.authorization_endpoint}</Text>}
                      {oidcResult.token_endpoint && <Text size="sm" style={{ overflowWrap: 'anywhere' }}>Token: {oidcResult.token_endpoint}</Text>}
                    </Stack>
                  </Alert>
                )}
              </Stack>
            </Tabs.Panel>

            <Tabs.Panel value="ai" pt="xl">
              <Stack gap="xl">
                <SectionHeading icon={<Bot size={20} />} title="AI 설정" description="내부망의 OpenAI-compatible API를 연결합니다. Secret 평문은 AI로 보내지 않습니다." />
                <Switch
                  size="md"
                  checked={settings.ai.enabled}
                  onChange={(event) => setSettings((current) => ({
                    ...current,
                    ai: { ...current.ai, enabled: event.currentTarget.checked },
                  }))}
                  label="AI 기능 사용"
                  description="기본값은 비활성이며, 사용자가 설정하기 전에는 외부 요청을 보내지 않습니다."
                />
                <SimpleGrid cols={{ base: 1, md: 2 }} spacing="lg">
                  <TextInput
                    type="url"
                    label="OpenAI-compatible Base URL"
                    description="폐쇄망에서 접근 가능한 API 주소를 사용하세요."
                    placeholder="http://ai-gateway.intra/v1"
                    required={settings.ai.enabled}
                    value={settings.ai.base_url}
                    onChange={(event) => setSettings((current) => ({
                      ...current,
                      ai: { ...current.ai, base_url: event.currentTarget.value },
                    }))}
                  />
                  <TextInput
                    label="모델"
                    placeholder="사내 제공 모델 이름"
                    required={settings.ai.enabled}
                    value={settings.ai.model}
                    onChange={(event) => setSettings((current) => ({
                      ...current,
                      ai: { ...current.ai, model: event.currentTarget.value },
                    }))}
                  />
                  <PasswordInput
                    label="API Key"
                    description={settings.ai.api_key_configured ? '저장된 키가 있습니다. 비워두면 기존 값을 유지합니다.' : '암호화해 저장하며 다시 표시하지 않습니다.'}
                    placeholder={settings.ai.api_key_configured ? '•••••••••••• (설정됨)' : 'API Key 입력'}
                    autoComplete="new-password"
                    value={settings.ai.api_key}
                    onChange={(event) => setSettings((current) => ({
                      ...current,
                      ai: { ...current.ai, api_key: event.currentTarget.value },
                    }))}
                  />
                  <NumberInput
                    label="최대 출력 토큰"
                    description="1~262,144 범위입니다. 실제 허용량은 연결한 모델 정책을 따릅니다."
                    min={1}
                    max={262144}
                    clampBehavior="strict"
                    thousandSeparator=","
                    value={settings.ai.max_tokens}
                    onChange={(value) => setSettings((current) => ({
                      ...current,
                      ai: { ...current.ai, max_tokens: typeof value === 'number' ? value : 4096 },
                    }))}
                  />
                  <NumberInput
                    label="요청 제한 시간(초)"
                    min={10}
                    max={3600}
                    clampBehavior="strict"
                    value={settings.ai.timeout_seconds}
                    onChange={(value) => setSettings((current) => ({
                      ...current,
                      ai: { ...current.ai, timeout_seconds: typeof value === 'number' ? value : 600 },
                    }))}
                  />
                  <Switch
                    checked
                    disabled
                    label="응답 스트리밍"
                    description="AI 응답은 항상 스트리밍으로 처리되며 끌 수 없습니다."
                  />
                </SimpleGrid>
                {settings.ai.api_key_configured && <Badge color="teal" w="fit-content">API Key 설정됨</Badge>}
              </Stack>
            </Tabs.Panel>

            <Tabs.Panel value="security" pt="xl">
              <Stack gap="xl">
                <SectionHeading icon={<ShieldCheck size={20} />} title="보안 정책" description="인증 세션과 비밀번호, 감사 보존 정책을 설정합니다." />
                <Alert color="blue" title="즉시 적용되는 설정">
                  세션 제한 시간, 최소 비밀번호 길이와 로컬 로그인 허용은 저장 후 새 요청부터 적용됩니다. 보존 자동화·네트워크 강제·최초 비밀번호 변경 강제는 v0.1.0 프리뷰입니다.
                </Alert>
                <SimpleGrid cols={{ base: 1, md: 2 }} spacing="lg">
                  <NumberInput
                    label="세션 제한 시간(분)"
                    min={5}
                    max={1440}
                    clampBehavior="strict"
                    value={settings.security.session_timeout_minutes}
                    onChange={(value) => setSettings((current) => ({
                      ...current,
                      security: { ...current.security, session_timeout_minutes: typeof value === 'number' ? value : 720 },
                    }))}
                  />
                  <NumberInput
                    label="최소 비밀번호 길이"
                    min={12}
                    max={128}
                    clampBehavior="strict"
                    value={settings.security.password_min_length}
                    onChange={(value) => setSettings((current) => ({
                      ...current,
                      security: { ...current.security, password_min_length: typeof value === 'number' ? value : 12 },
                    }))}
                  />
                  <NumberInput
                    label="감사 로그 보존 기간(일)"
                    description="보존 정책 메타데이터 프리뷰이며 v0.1.0은 자동 삭제를 수행하지 않습니다."
                    disabled
                    min={30}
                    max={3650}
                    clampBehavior="strict"
                    value={settings.security.audit_retention_days}
                    onChange={(value) => setSettings((current) => ({
                      ...current,
                      security: { ...current.security, audit_retention_days: typeof value === 'number' ? value : 180 },
                    }))}
                  />
                  <TextInput
                    label="허용 네트워크"
                    description="네트워크 Zone 프리뷰입니다. v0.1.0에서는 Reverse Proxy나 방화벽에서 강제하세요."
                    disabled
                    placeholder="10.10.0.0/16, 10.20.0.0/16"
                    value={settings.security.allowed_networks}
                    onChange={(event) => setSettings((current) => ({
                      ...current,
                      security: { ...current.security, allowed_networks: event.currentTarget.value },
                    }))}
                  />
                </SimpleGrid>
                <Switch
                  checked={settings.security.allow_local_login}
                  onChange={(event) => setSettings((current) => ({
                    ...current,
                    security: { ...current.security, allow_local_login: event.currentTarget.checked },
                  }))}
                  label="로컬 로그인 허용"
                  description="Keycloak 장애 시 복구 계정을 위해 최소 한 명의 로컬 관리자를 유지하세요."
                />
                <Switch
                  checked={settings.security.require_password_change}
                  disabled
                  onChange={(event) => setSettings((current) => ({
                    ...current,
                    security: { ...current.security, require_password_change: event.currentTarget.checked },
                  }))}
                  label="Bootstrap 관리자의 최초 비밀번호 변경 요구 (프리뷰)"
                  description="v0.1.0은 상태를 저장하지만 로그인 시 강제하지 않습니다. 배포 직후 프로필에서 직접 변경하세요."
                />
              </Stack>
            </Tabs.Panel>

            <Tabs.Panel value="notifications" pt="xl">
              <Stack gap="xl">
                <SectionHeading icon={<Bell size={20} />} title="알림 (프리뷰)" description="향후 폐쇄망 webhook과 메일 릴레이로 운영 이벤트를 전달할 설정 초안입니다." />
                <Alert color="yellow" title="v0.1.0은 알림을 전송하지 않습니다">
                  입력값은 운영 전송 기능이 구현될 때까지 변경할 수 없습니다. 현재 이벤트는 감사 로그에서 확인하세요.
                </Alert>
                <Switch
                  size="md"
                  disabled
                  checked={settings.notifications.enabled}
                  onChange={(event) => setSettings((current) => ({
                    ...current,
                    notifications: { ...current.notifications, enabled: event.currentTarget.checked },
                  }))}
                  label="운영 알림 사용"
                />
                <SimpleGrid cols={{ base: 1, md: 2 }} spacing="lg">
                  <TextInput
                    type="url"
                    disabled
                    label="Webhook URL"
                    description="Mattermost 등 내부 HTTP endpoint를 입력하세요."
                    placeholder="https://mattermost.intra/hooks/..."
                    value={settings.notifications.webhook_url}
                    onChange={(event) => setSettings((current) => ({
                      ...current,
                      notifications: { ...current.notifications, webhook_url: event.currentTarget.value },
                    }))}
                  />
                  <TextInput
                    label="메일 수신자"
                    disabled
                    description="쉼표로 구분합니다. SMTP 연결은 시스템 관리자가 구성해야 합니다."
                    placeholder="security@example.internal, ops@example.internal"
                    value={settings.notifications.email_recipients}
                    onChange={(event) => setSettings((current) => ({
                      ...current,
                      notifications: { ...current.notifications, email_recipients: event.currentTarget.value },
                    }))}
                  />
                </SimpleGrid>
                <Checkbox.Group
                  label="알림 이벤트"
                  value={settings.notifications.events}
                  onChange={(events) => setSettings((current) => ({
                    ...current,
                    notifications: { ...current.notifications, events },
                  }))}
                >
                  <SimpleGrid cols={{ base: 1, sm: 2 }} mt="sm">
                    {notificationEvents.map((event) => <Checkbox key={event.value} value={event.value} label={event.label} disabled />)}
                  </SimpleGrid>
                </Checkbox.Group>
              </Stack>
            </Tabs.Panel>
          </Tabs>
        </Paper>
      </Stack>
    </form>
  );
}

export function AdminSettingsPage() {
  const { user, loading: authLoading } = useAuth();
  const [searchParams] = useSearchParams();
  const settingsQuery = useQuery({
    queryKey: ['admin-settings'],
    queryFn: () => get<SystemSettings>('/settings'),
    enabled: !authLoading && user?.role === 'admin',
  });

  if (authLoading) return <SettingsLoading />;

  if (!user || user.role !== 'admin') {
    return (
      <Alert icon={<ShieldCheck size={20} />} color="red" title="관리자 권한이 필요합니다" role="alert">
        서비스 관리 설정은 관리자만 조회하고 변경할 수 있습니다.
      </Alert>
    );
  }

  if (settingsQuery.isPending) return <SettingsLoading />;

  if (settingsQuery.isError) {
    return (
      <Paper className="surface" p="xl" radius="lg">
        <Stack align="flex-start">
          <Alert icon={<CircleAlert size={20} />} color="red" title="관리 설정을 불러오지 못했습니다" w="100%" role="alert">
            {errorMessage(settingsQuery.error)}
          </Alert>
          <Button variant="light" leftSection={<RefreshCw size={17} />} onClick={() => settingsQuery.refetch()}>
            다시 시도
          </Button>
        </Stack>
      </Paper>
    );
  }

  const normalized = normalizeSettings(settingsQuery.data);
  const formKey = JSON.stringify(normalized);
  const requestedTab = searchParams.get('tab');
  const initialTab = ['general', 'approval', 'oidc', 'ai', 'security', 'notifications'].includes(requestedTab || '')
    ? requestedTab as SettingsTab
    : 'general';
  return <AdminSettingsForm key={`${formKey}:${initialTab}`} initialSettings={normalized} initialTab={initialTab} />;
}

export default AdminSettingsPage;
