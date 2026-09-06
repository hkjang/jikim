import { useState, type FormEvent } from 'react';
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
  Trash2,
  UsersRound,
  Webhook,
} from 'lucide-react';
import { get, patch, post, testAIIntegration, testWebhookIntegration } from '../../lib/api';
import type { IntegrationTestResult, SystemSettings } from '../../lib/types';
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
  clear_client_secret: boolean;
  scopes: string;
  group_claim: string;
  role_claim: string;
  username_claim: string;
  allow_insecure_http: boolean;
}

interface AiSettings {
  enabled: boolean;
  base_url: string;
  auth_type: 'bearer' | 'api-key' | 'none';
  api_key: string;
  api_key_configured: boolean;
  clear_api_key: boolean;
  model: string;
  max_tokens: number;
  timeout_seconds: number;
  allow_insecure_http: boolean;
}

interface SecuritySettings {
  allow_local_login: boolean;
  require_password_change: boolean;
  session_timeout_minutes: number;
  password_min_length: number;
  audit_retention_days: number;
  allowed_networks: string;
  trusted_proxies: string;
}

interface NotificationSettings {
  enabled: boolean;
  webhook_url: string;
  webhook_configured: boolean;
  clear_webhook: boolean;
  signing_secret: string;
  signing_secret_configured: boolean;
  rotate_signing_secret: boolean;
  allow_insecure_http: boolean;
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
  { value: 'approval.requested', label: '승인 요청' },
  { value: 'approval.approved', label: '요청 승인' },
  { value: 'approval.rejected', label: '요청 반려' },
  { value: 'secret.created', label: '시크릿 생성' },
  { value: 'secret.updated', label: '시크릿 변경' },
  { value: 'secret.rotated', label: '시크릿 회전' },
  { value: 'secret.deleted', label: '시크릿 격리' },
  { value: 'rotation.failed', label: '회전 실패' },
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
      clear_client_secret: false,
      scopes: Array.isArray(oidc.scopes) ? stringList(oidc.scopes).join(' ') : stringValue(oidc.scopes, 'openid profile email groups'),
      group_claim: stringValue(oidc.group_claim, 'groups'),
      role_claim: stringValue(oidc.role_claim, 'roles'),
      username_claim: stringValue(oidc.username_claim, 'preferred_username'),
      allow_insecure_http: booleanValue(oidc.allow_insecure_http),
    },
    ai: {
      enabled: booleanValue(ai.enabled),
      base_url: stringValue(ai.base_url),
      auth_type: ai.auth_type === 'api-key' || ai.auth_type === 'none' ? ai.auth_type : 'bearer',
      api_key: '',
      api_key_configured: booleanValue(ai.api_key_configured ?? ai.has_api_key),
      clear_api_key: false,
      model: stringValue(ai.model),
      max_tokens: numberValue(ai.max_tokens, 4096),
      timeout_seconds: numberValue(ai.timeout_seconds, 600),
      allow_insecure_http: booleanValue(ai.allow_insecure_http),
    },
    security: {
      allow_local_login: booleanValue(security.allow_local_login, true),
      require_password_change: booleanValue(security.require_password_change, false),
      session_timeout_minutes: numberValue(security.session_timeout_minutes, 720),
      password_min_length: numberValue(security.password_min_length, 12),
      audit_retention_days: numberValue(security.audit_retention_days, 180),
      allowed_networks: stringValue(security.allowed_networks),
      trusted_proxies: stringValue(security.trusted_proxies),
    },
    notifications: {
      enabled: booleanValue(notification.enabled),
      webhook_url: stringValue(notification.webhook_url),
      webhook_configured: booleanValue(notification.webhook_configured),
      clear_webhook: false,
      signing_secret: '',
      signing_secret_configured: booleanValue(notification.signing_secret_configured),
      rotate_signing_secret: false,
      allow_insecure_http: booleanValue(notification.allow_insecure_http),
      events: stringList(notification.events, ['approval.requested', 'secret.rotated', 'rotation.failed']),
    },
  };
}

function settingsPayload(settings: AdminSettings, redirectUrl: string): Record<string, unknown> {
  const oidc: Record<string, unknown> = {
    enabled: settings.oidc.enabled,
    issuer_url: settings.oidc.issuer_url.trim(),
    client_id: settings.oidc.client_id.trim(),
    scopes: settings.oidc.scopes.trim().split(/\s+/).filter(Boolean),
    group_claim: settings.oidc.group_claim.trim(),
    role_claim: settings.oidc.role_claim.trim(),
    username_claim: settings.oidc.username_claim.trim(),
    allow_insecure_http: settings.oidc.allow_insecure_http,
    redirect_url: redirectUrl,
    clear_client_secret: settings.oidc.clear_client_secret,
  };
  if (settings.oidc.client_secret.trim()) oidc.client_secret = settings.oidc.client_secret;

  const ai: Record<string, unknown> = {
    enabled: settings.ai.enabled,
    base_url: settings.ai.base_url.trim(),
    auth_type: settings.ai.auth_type,
    model: settings.ai.model.trim(),
    streaming: true,
    max_tokens: settings.ai.max_tokens,
    timeout_seconds: settings.ai.timeout_seconds,
    allow_insecure_http: settings.ai.allow_insecure_http,
    clear_api_key: settings.ai.clear_api_key,
  };
  if (settings.ai.api_key.trim()) ai.api_key = settings.ai.api_key;

  const notifications: Record<string, unknown> = {
    enabled: settings.notifications.enabled,
    events: settings.notifications.events,
    allow_insecure_http: settings.notifications.allow_insecure_http,
    clear_webhook: settings.notifications.clear_webhook,
    rotate_signing_secret: settings.notifications.rotate_signing_secret,
  };
  if (settings.notifications.webhook_url.trim()) notifications.webhook_url = settings.notifications.webhook_url.trim();
  if (settings.notifications.signing_secret.trim()) notifications.signing_secret = settings.notifications.signing_secret;

  return {
    general: settings.general,
    approval: { ...settings.approval, approval_enabled: settings.approval.enabled },
    oidc,
    ai,
    security: settings.security,
    notifications,
  };
}

function aiSettingsSignature(settings: AiSettings): string {
  return JSON.stringify({
    enabled: settings.enabled,
    base_url: settings.base_url.trim(),
    auth_type: settings.auth_type,
    model: settings.model.trim(),
    max_tokens: settings.max_tokens,
    timeout_seconds: settings.timeout_seconds,
    allow_insecure_http: settings.allow_insecure_http,
    api_key_configured: settings.api_key_configured,
    new_api_key: Boolean(settings.api_key.trim()),
    clear_api_key: settings.clear_api_key,
  });
}

function webhookSettingsSignature(settings: NotificationSettings): string {
  return JSON.stringify({
    enabled: settings.enabled,
    events: [...settings.events].sort(),
    webhook_configured: settings.webhook_configured,
    signing_secret_configured: settings.signing_secret_configured,
    allow_insecure_http: settings.allow_insecure_http,
    new_webhook_url: Boolean(settings.webhook_url.trim()),
    new_signing_secret: Boolean(settings.signing_secret.trim()),
    clear_webhook: settings.clear_webhook,
    rotate_signing_secret: settings.rotate_signing_secret,
  });
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

function IntegrationResultAlert({ result, successTitle, failureTitle }: { result: IntegrationTestResult; successTitle: string; failureTitle: string }) {
  return (
    <Alert
      role="status"
      icon={result.ok ? <CheckCircle2 size={18} /> : <CircleAlert size={18} />}
      color={result.ok ? 'teal' : 'red'}
      title={result.ok ? successTitle : failureTitle}
    >
      <Stack gap={4}>
        {result.message && <Text>{result.message}</Text>}
        {result.endpoint && <Text size="sm" style={{ overflowWrap: 'anywhere' }}>Endpoint: {result.endpoint}</Text>}
        {result.profile && <Text size="sm">Profile: {result.profile}</Text>}
        {(result.status_code ?? result.status) !== undefined && <Text size="sm">응답 상태: {String(result.status_code ?? result.status)}</Text>}
        {result.latency_ms !== undefined && <Text size="sm">응답 시간: {result.latency_ms.toLocaleString('ko-KR')} ms</Text>}
      </Stack>
    </Alert>
  );
}

function AdminSettingsForm({ initialSettings, initialTab = 'general' }: { initialSettings: AdminSettings; initialTab?: SettingsTab }) {
  const queryClient = useQueryClient();
  const [activeTab, setActiveTab] = useState<SettingsTab>(initialTab);
  const [settings, setSettings] = useState(initialSettings);
  const [savedSettings, setSavedSettings] = useState(initialSettings);
  const [oidcResult, setOidcResult] = useState<OidcTestResult | null>(null);
  const [aiResult, setAiResult] = useState<IntegrationTestResult | null>(null);
  const [webhookResult, setWebhookResult] = useState<IntegrationTestResult | null>(null);
  const [saveValidation, setSaveValidation] = useState<string | null>(null);
  const redirectUrl = `${window.location.origin}/api/v1/oidc/callback`;

  // Build the patch in the event handler itself. React clears the synthetic
  // event's currentTarget as soon as the handler returns, so reading it inside
  // the setState updater throws once React defers that updater to the render
  // phase (which it does as soon as another update is already pending).
  const updateGeneral = (patch: Partial<GeneralSettings>) => setSettings((current) => ({ ...current, general: { ...current.general, ...patch } }));
  const updateApproval = (patch: Partial<ApprovalSettings>) => setSettings((current) => ({ ...current, approval: { ...current.approval, ...patch } }));
  const updateOidc = (patch: Partial<OidcSettings>) => setSettings((current) => ({ ...current, oidc: { ...current.oidc, ...patch } }));
  const updateAi = (patch: Partial<AiSettings>) => setSettings((current) => ({ ...current, ai: { ...current.ai, ...patch } }));
  const updateSecurity = (patch: Partial<SecuritySettings>) => setSettings((current) => ({ ...current, security: { ...current.security, ...patch } }));
  const updateNotifications = (patch: Partial<NotificationSettings>) => setSettings((current) => ({ ...current, notifications: { ...current.notifications, ...patch } }));

  const saveMutation = useMutation({
    mutationFn: () => patch<unknown>('/settings', settingsPayload(settings, redirectUrl)),
    onSuccess: async () => {
      const persisted: AdminSettings = {
        ...settings,
        oidc: {
          ...settings.oidc,
          client_secret: '',
          client_secret_configured: Boolean(settings.oidc.client_secret) || (settings.oidc.client_secret_configured && !settings.oidc.clear_client_secret),
          clear_client_secret: false,
        },
        ai: {
          ...settings.ai,
          api_key: '',
          api_key_configured: Boolean(settings.ai.api_key) || (settings.ai.api_key_configured && !settings.ai.clear_api_key),
          clear_api_key: false,
        },
        notifications: {
          ...settings.notifications,
          webhook_url: '',
          webhook_configured: Boolean(settings.notifications.webhook_url) || (settings.notifications.webhook_configured && !settings.notifications.clear_webhook),
          clear_webhook: false,
          signing_secret: '',
          signing_secret_configured: settings.notifications.signing_secret_configured || settings.notifications.webhook_configured || Boolean(settings.notifications.signing_secret) || Boolean(settings.notifications.webhook_url),
          rotate_signing_secret: false,
        },
      };
      setSettings(persisted);
      setSavedSettings(persisted);
      setSaveValidation(null);
      setAiResult(null);
      setWebhookResult(null);
      await queryClient.invalidateQueries({ queryKey: ['admin-settings'] });
      notifications.show({ color: 'teal', title: '설정 저장 완료', message: '변경한 관리 설정을 안전하게 저장했습니다.' });
    },
  });

  const oidcTestMutation = useMutation({
    mutationFn: async () => {
      const payload = settingsPayload(settings, redirectUrl).oidc as Record<string, unknown>;
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

  const aiTestMutation = useMutation({
    mutationFn: () => testAIIntegration<IntegrationTestResult>(),
    onSuccess: (result) => {
      setAiResult(result);
      notifications.show({ color: result.ok ? 'teal' : 'red', title: result.ok ? 'AI 연결 성공' : 'AI 연결 실패', message: result.message || (result.ok ? '저장된 AI 설정으로 응답을 확인했습니다.' : 'AI endpoint 응답을 확인하세요.') });
    },
    onError: (error) => setAiResult({ ok: false, message: errorMessage(error) }),
  });

  const webhookTestMutation = useMutation({
    mutationFn: () => testWebhookIntegration<IntegrationTestResult>(),
    onSuccess: (result) => {
      setWebhookResult(result);
      notifications.show({ color: result.ok ? 'teal' : 'red', title: result.ok ? 'Webhook 전달 성공' : 'Webhook 전달 실패', message: result.message || (result.ok ? '서명된 테스트 이벤트를 전달했습니다.' : 'Webhook endpoint 응답을 확인하세요.') });
    },
    onError: (error) => setWebhookResult({ ok: false, message: errorMessage(error) }),
  });

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setSaveValidation(null);
    if (settings.oidc.enabled && (!settings.oidc.issuer_url.trim() || !settings.oidc.client_id.trim())) {
      setActiveTab('oidc');
      setSaveValidation('OIDC를 사용하려면 Issuer URL과 Client ID를 모두 설정하세요. 비밀 클라이언트인 경우 Client Secret도 입력하세요.');
      return;
    }
    if (settings.ai.enabled && (!settings.ai.base_url.trim() || !settings.ai.model.trim())) {
      setActiveTab('ai');
      setSaveValidation('AI를 사용하려면 Base URL과 모델을 모두 설정하세요. 인증이 필요한 endpoint라면 API Key도 입력하세요.');
      return;
    }
    if (settings.ai.enabled && settings.ai.auth_type !== 'none' && !settings.ai.api_key.trim() && (!settings.ai.api_key_configured || settings.ai.clear_api_key)) {
      setActiveTab('ai');
      setSaveValidation('선택한 AI 인증 방식에는 API Key가 필요합니다. 무인증 내부 endpoint라면 인증 방식을 “인증 없음”으로 바꾸세요.');
      return;
    }
    if (settings.notifications.enabled && ((!settings.notifications.webhook_configured || settings.notifications.clear_webhook) && !settings.notifications.webhook_url.trim())) {
      setActiveTab('notifications');
      setSaveValidation('운영 알림을 사용하려면 Webhook URL을 설정하세요.');
      return;
    }
    if (settings.notifications.signing_secret.trim() && settings.notifications.signing_secret.length < 32) {
      setActiveTab('notifications');
      setSaveValidation('Webhook 서명 Secret은 32자 이상이어야 합니다. 비워두면 안전한 값이 자동 생성됩니다.');
      return;
    }
    if (settings.notifications.enabled && settings.notifications.events.length === 0) {
      setActiveTab('notifications');
      setSaveValidation('Webhook으로 전달할 이벤트를 하나 이상 선택하세요.');
      return;
    }
    saveMutation.mutate();
  };

  const aiHasUnsavedChanges = aiSettingsSignature(settings.ai) !== aiSettingsSignature(savedSettings.ai);
  const webhookHasUnsavedChanges = webhookSettingsSignature(settings.notifications) !== webhookSettingsSignature(savedSettings.notifications);
  const aiTestReady = Boolean(savedSettings.ai.base_url.trim()) && Boolean(savedSettings.ai.model.trim()) && !aiHasUnsavedChanges;
  const webhookTestReady = savedSettings.notifications.webhook_configured && savedSettings.notifications.signing_secret_configured && !webhookHasUnsavedChanges;

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
                setSettings(savedSettings);
                setOidcResult(null);
                setAiResult(null);
                setWebhookResult(null);
                setSaveValidation(null);
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

        {(saveValidation || saveMutation.isError) && (
          <Alert icon={<CircleAlert size={18} />} color="red" title="설정을 저장하지 못했습니다" role="alert">
            {saveValidation || errorMessage(saveMutation.error)}
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
                <SectionHeading icon={<Languages size={20} />} title="일반 설정" description="현재 버전의 제품명과 한국어 운영 기준을 확인합니다." />
                <SimpleGrid cols={{ base: 1, md: 2 }} spacing="lg">
                  <TextInput
                    label="서비스 표시 이름"
                    description="브라우저와 관리 화면에 표시됩니다."
                    required
                    readOnly
                    value={settings.general.service_name}
                    onChange={(event) => updateGeneral({ service_name: event.currentTarget.value })}
                  />
                  <Select
                    label="기본 언어"
                    description="신규 사용자의 기본 UI 언어입니다."
                    data={[{ value: 'ko', label: '한국어' }]}
                    disabled
                    value={settings.general.default_language}
                    onChange={(value) => updateGeneral({ default_language: value || 'ko' })}
                  />
                  <TextInput
                    label="표준 시간대"
                    description="IANA 시간대 이름을 입력하세요."
                    placeholder="Asia/Seoul"
                    readOnly
                    value={settings.general.timezone}
                    onChange={(event) => updateGeneral({ timezone: event.currentTarget.value })}
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
                  onChange={(event) => updateApproval({ enabled: event.currentTarget.checked })}
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
                        onChange={(value) => updateApproval({ reviewer_role: value || 'manager' })}
                      />
                      <NumberInput
                        label="필수 승인 수"
                        description="작업 실행에 필요한 승인 인원입니다."
                        min={1}
                        max={1}
                        disabled
                        clampBehavior="strict"
                        value={settings.approval.required_approvals}
                        onChange={(value) => updateApproval({ required_approvals: typeof value === 'number' ? value : 1 })}
                      />
                    </SimpleGrid>
                    <Switch
                      checked
                      disabled
                      label="요청자 본인 승인 금지"
                      description="요청자와 승인자가 반드시 다르도록 항상 강제합니다. 현재 버전은 1인 승인을 지원합니다."
                    />
                    <Checkbox.Group
                      label="승인 적용 작업"
                      description="선택한 작업만 승인 대기 상태로 전환합니다."
                      value={settings.approval.targets}
                      onChange={(targets) => updateApproval({ targets })}
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
                  onChange={(event) => updateOidc({ enabled: event.currentTarget.checked })}
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
                      updateOidc({ issuer_url: event.currentTarget.value });
                    }}
                  />
                  <TextInput
                    label="Client ID"
                    required={settings.oidc.enabled}
                    autoComplete="off"
                    value={settings.oidc.client_id}
                    onChange={(event) => updateOidc({ client_id: event.currentTarget.value })}
                  />
                  <PasswordInput
                    label="Client Secret"
                    description={settings.oidc.client_secret_configured ? '저장된 값이 있습니다. 비워두면 기존 값을 유지합니다.' : '저장 시 암호화되며 다시 표시되지 않습니다.'}
                    placeholder={settings.oidc.client_secret_configured ? '•••••••••••• (설정됨)' : 'Client Secret 입력'}
                    autoComplete="new-password"
                    value={settings.oidc.client_secret}
                    onChange={(event) => updateOidc({ client_secret: event.currentTarget.value, clear_client_secret: false })}
                  />
                  <TextInput label="Callback URL" description="Keycloak client의 Valid redirect URI에 이 절대 URL을 등록하세요." value={redirectUrl} readOnly />
                  <TextInput
                    label="Scopes"
                    description="공백으로 구분합니다."
                    value={settings.oidc.scopes}
                    onChange={(event) => updateOidc({ scopes: event.currentTarget.value })}
                  />
                  <TextInput
                    label="사용자명 Claim"
                    value={settings.oidc.username_claim}
                    onChange={(event) => updateOidc({ username_claim: event.currentTarget.value })}
                  />
                  <TextInput
                    label="그룹 Claim"
                    value={settings.oidc.group_claim}
                    onChange={(event) => updateOidc({ group_claim: event.currentTarget.value })}
                  />
                  <TextInput
                    label="역할 Claim"
                    value={settings.oidc.role_claim}
                    onChange={(event) => updateOidc({ role_claim: event.currentTarget.value })}
                  />
                </SimpleGrid>
                <Switch
                  checked={settings.oidc.allow_insecure_http}
                  onChange={(event) => updateOidc({ allow_insecure_http: event.currentTarget.checked })}
                  label="OIDC 내부 HTTP 허용"
                  description="기본값은 OFF입니다. TLS를 적용할 수 없는 신뢰된 내부 개발망에서만 사용하세요."
                />
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
                  {settings.oidc.clear_client_secret
                    ? <Badge color="orange">Client Secret 저장 시 삭제</Badge>
                    : settings.oidc.client_secret_configured && <Badge color="teal">Client Secret 설정됨</Badge>}
                  {settings.oidc.client_secret_configured && !settings.oidc.clear_client_secret && (
                    <Button
                      type="button"
                      size="xs"
                      color="red"
                      variant="subtle"
                      leftSection={<Trash2 size={15} />}
                      onClick={() => {
                        if (!window.confirm('저장된 OIDC Client Secret을 삭제할까요? 비밀 클라이언트는 삭제 후 로그인할 수 없습니다.')) return;
                        setOidcResult(null);
                        updateOidc({ client_secret: '', clear_client_secret: true });
                      }}
                    >
                      Client Secret 삭제
                    </Button>
                  )}
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
                  onChange={(event) => updateAi({ enabled: event.currentTarget.checked })}
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
                    onChange={(event) => updateAi({ base_url: event.currentTarget.value })}
                  />
                  <TextInput
                    label="모델"
                    placeholder="사내 제공 모델 이름"
                    required={settings.ai.enabled}
                    value={settings.ai.model}
                    onChange={(event) => updateAi({ model: event.currentTarget.value })}
                  />
                  <Select
                    label="API 인증 방식"
                    description="연결한 OpenAI-compatible endpoint의 인증 헤더 방식입니다."
                    data={[
                      { value: 'bearer', label: 'Bearer Token (Authorization)' },
                      { value: 'api-key', label: 'API Key Header (api-key)' },
                      { value: 'none', label: '인증 없음 (내부망)' },
                    ]}
                    value={settings.ai.auth_type}
                    onChange={(value) => updateAi({ auth_type: (value || 'bearer') as AiSettings['auth_type'] })}
                  />
                  <PasswordInput
                    label="API Key"
                    description={settings.ai.auth_type === 'none'
                      ? '인증 없음에서는 API Key를 전송하지 않습니다.'
                      : settings.ai.api_key_configured ? '저장된 키가 있습니다. 비워두면 기존 값을 유지합니다.' : '암호화해 저장하며 다시 표시하지 않습니다.'}
                    placeholder={settings.ai.api_key_configured ? '•••••••••••• (설정됨)' : 'API Key 입력'}
                    autoComplete="new-password"
                    disabled={settings.ai.auth_type === 'none'}
                    value={settings.ai.api_key}
                    onChange={(event) => updateAi({ api_key: event.currentTarget.value, clear_api_key: false })}
                  />
                  <NumberInput
                    label="최대 출력 토큰"
                    description="1~262,144 범위입니다. 실제 허용량은 연결한 모델 정책을 따릅니다."
                    min={1}
                    max={262144}
                    clampBehavior="strict"
                    thousandSeparator=","
                    value={settings.ai.max_tokens}
                    onChange={(value) => updateAi({ max_tokens: typeof value === 'number' ? value : 4096 })}
                  />
                  <NumberInput
                    label="요청 제한 시간(초)"
                    min={10}
                    max={3600}
                    clampBehavior="strict"
                    value={settings.ai.timeout_seconds}
                    onChange={(value) => updateAi({ timeout_seconds: typeof value === 'number' ? value : 600 })}
                  />
                  <Switch
                    checked
                    disabled
                    label="응답 스트리밍"
                    description="AI 응답은 항상 스트리밍으로 처리되며 끌 수 없습니다."
                  />
                </SimpleGrid>
                <Switch
                  checked={settings.ai.allow_insecure_http}
                  onChange={(event) => updateAi({ allow_insecure_http: event.currentTarget.checked })}
                  label="AI endpoint 내부 HTTP 허용"
                  description="기본값은 OFF입니다. TLS를 적용할 수 없는 신뢰된 내부 개발망에서만 사용하세요."
                />
                <Group>
                  {settings.ai.clear_api_key
                    ? <Badge color="orange">API Key 저장 시 삭제</Badge>
                    : settings.ai.api_key_configured && <Badge color="teal">API Key 설정됨</Badge>}
                  {settings.ai.api_key_configured && !settings.ai.clear_api_key && (
                    <Button
                      type="button"
                      size="xs"
                      color="red"
                      variant="subtle"
                      leftSection={<Trash2 size={15} />}
                      onClick={() => {
                        if (!window.confirm('저장된 AI API Key를 삭제할까요? 인증이 필요한 AI endpoint는 삭제 후 사용할 수 없습니다.')) return;
                        setAiResult(null);
                        updateAi({ api_key: '', clear_api_key: true });
                      }}
                    >
                      API Key 삭제
                    </Button>
                  )}
                  <Button
                    type="button"
                    variant="light"
                    leftSection={aiTestMutation.isPending ? <Loader size={16} /> : <RefreshCw size={17} />}
                    loading={aiTestMutation.isPending}
                    disabled={!aiTestReady}
                    onClick={() => aiTestMutation.mutate()}
                  >
                    저장된 AI 연결 테스트
                  </Button>
                </Group>
                {aiHasUnsavedChanges && <Text size="sm" c="dimmed">변경 사항을 저장하면 새 설정으로 연결을 테스트할 수 있습니다.</Text>}
                {!aiHasUnsavedChanges && aiResult && <IntegrationResultAlert result={aiResult} successTitle="AI 연결 확인 완료" failureTitle="AI 연결 확인 실패" />}
              </Stack>
            </Tabs.Panel>

            <Tabs.Panel value="security" pt="xl">
              <Stack gap="xl">
                <SectionHeading icon={<ShieldCheck size={20} />} title="보안 정책" description="인증 세션과 비밀번호, 감사 보존 정책을 설정합니다." />
                <Alert color="blue" title="즉시 적용되는 설정">
                  세션 제한 시간, 최소 비밀번호 길이, 로컬 로그인 허용과 신뢰 Reverse Proxy는 저장 후 새 요청부터 적용됩니다. 보존 자동화·네트워크 강제·최초 비밀번호 변경 강제는 아직 프리뷰입니다.
                </Alert>
                <SimpleGrid cols={{ base: 1, md: 2 }} spacing="lg">
                  <NumberInput
                    label="세션 제한 시간(분)"
                    min={5}
                    max={1440}
                    clampBehavior="strict"
                    value={settings.security.session_timeout_minutes}
                    onChange={(value) => updateSecurity({ session_timeout_minutes: typeof value === 'number' ? value : 720 })}
                  />
                  <NumberInput
                    label="최소 비밀번호 길이"
                    min={12}
                    max={128}
                    clampBehavior="strict"
                    value={settings.security.password_min_length}
                    onChange={(value) => updateSecurity({ password_min_length: typeof value === 'number' ? value : 12 })}
                  />
                  <NumberInput
                    label="감사 로그 보존 기간(일)"
                    description="보존 정책 메타데이터 프리뷰이며 현재 버전은 자동 삭제를 수행하지 않습니다."
                    disabled
                    min={30}
                    max={3650}
                    clampBehavior="strict"
                    value={settings.security.audit_retention_days}
                    onChange={(value) => updateSecurity({ audit_retention_days: typeof value === 'number' ? value : 180 })}
                  />
                  <TextInput
                    label="허용 네트워크"
                    description="네트워크 Zone 프리뷰입니다. 현재 버전에서는 Reverse Proxy나 방화벽에서 강제하세요."
                    disabled
                    placeholder="10.10.0.0/16, 10.20.0.0/16"
                    value={settings.security.allowed_networks}
                    onChange={(event) => updateSecurity({ allowed_networks: event.currentTarget.value })}
                  />
                  <TextInput
                    label="신뢰 Reverse Proxy"
                    description="여기 등록한 대역에서 온 요청만 X-Forwarded-For를 신뢰해 감사 로그 IP와 로그인 제한에 사용합니다. 비우면 접속 주소를 그대로 기록합니다."
                    placeholder="10.10.0.0/16, 192.0.2.10"
                    value={settings.security.trusted_proxies}
                    onChange={(event) => updateSecurity({ trusted_proxies: event.currentTarget.value })}
                  />
                </SimpleGrid>
                <Switch
                  checked={settings.security.allow_local_login}
                  onChange={(event) => updateSecurity({ allow_local_login: event.currentTarget.checked })}
                  label="로컬 로그인 허용"
                  description="Keycloak 장애 시 복구 계정을 위해 최소 한 명의 로컬 관리자를 유지하세요."
                />
                <Switch
                  checked={settings.security.require_password_change}
                  disabled
                  onChange={(event) => updateSecurity({ require_password_change: event.currentTarget.checked })}
                  label="Bootstrap 관리자의 최초 비밀번호 변경 요구 (프리뷰)"
                  description="현재 버전은 상태를 저장하지만 로그인 시 강제하지 않습니다. 배포 직후 프로필에서 직접 변경하세요."
                />
              </Stack>
            </Tabs.Panel>

            <Tabs.Panel value="notifications" pt="xl">
              <Stack gap="xl">
                <SectionHeading icon={<Webhook size={20} />} title="서명 Webhook 알림" description="운영 이벤트를 폐쇄망 HTTP endpoint로 전달하고 HMAC 서명으로 발신자를 검증합니다." />
                <Alert color="blue" title="수신 측에서 서명을 검증하세요">
                  서명 Secret은 암호화해 저장하고 다시 표시하지 않습니다. 새 연결에서 비워두면 저장 시 안전한 값이 자동 생성됩니다.
                </Alert>
                <Switch
                  size="md"
                  checked={settings.notifications.enabled}
                  onChange={(event) => updateNotifications({ enabled: event.currentTarget.checked })}
                  label="서명 Webhook 알림 사용"
                  description="저장 후 선택한 운영 이벤트를 Webhook으로 전달합니다."
                />
                <SimpleGrid cols={{ base: 1, md: 2 }} spacing="lg">
                  <TextInput
                    type="url"
                    label="Webhook URL"
                    description={settings.notifications.webhook_configured ? '저장된 URL이 있습니다. 비워두면 기존 값을 유지합니다.' : '폐쇄망에서 접근 가능한 HTTP endpoint를 입력하세요.'}
                    placeholder={settings.notifications.webhook_configured ? '•••••••••••• (설정됨)' : 'https://hooks.example.internal/jikim'}
                    required={settings.notifications.enabled && !settings.notifications.webhook_configured}
                    value={settings.notifications.webhook_url}
                    onChange={(event) => updateNotifications({ webhook_url: event.currentTarget.value, clear_webhook: false })}
                  />
                  <PasswordInput
                    label="Webhook 서명 Secret"
                    description={settings.notifications.signing_secret_configured ? '저장된 Secret이 있습니다. 비워두면 기존 값을 유지합니다.' : '비워두면 저장 시 안전한 Secret을 자동 생성합니다.'}
                    placeholder={settings.notifications.signing_secret_configured ? '•••••••••••• (설정됨)' : '직접 지정하거나 비워두기'}
                    autoComplete="new-password"
                    value={settings.notifications.signing_secret}
                    onChange={(event) => updateNotifications({ signing_secret: event.currentTarget.value, rotate_signing_secret: false })}
                  />
                </SimpleGrid>
                <Group gap="xs">
                  {settings.notifications.clear_webhook
                    ? <Badge color="orange">Webhook URL 저장 시 삭제</Badge>
                    : settings.notifications.webhook_configured && <Badge color="teal">Webhook URL 설정됨</Badge>}
                  {settings.notifications.rotate_signing_secret
                    ? <Badge color="orange">서명 Secret 저장 시 회전</Badge>
                    : settings.notifications.signing_secret_configured && <Badge color="teal">서명 Secret 설정됨</Badge>}
                  {settings.notifications.webhook_configured && !settings.notifications.clear_webhook && (
                    <Button
                      type="button"
                      size="xs"
                      color="red"
                      variant="subtle"
                      leftSection={<Trash2 size={15} />}
                      onClick={() => {
                        if (!window.confirm('저장된 Webhook URL을 삭제하고 알림을 끌까요?')) return;
                        setWebhookResult(null);
                        updateNotifications({ enabled: false, webhook_url: '', clear_webhook: true });
                      }}
                    >
                      Webhook 삭제
                    </Button>
                  )}
                  {settings.notifications.signing_secret_configured && !settings.notifications.rotate_signing_secret && (
                    <Button
                      type="button"
                      size="xs"
                      variant="subtle"
                      leftSection={<RotateCcw size={15} />}
                      onClick={() => {
                        if (!window.confirm('Webhook 서명 Secret을 회전할까요? 저장 즉시 기존 Secret으로 만든 서명은 더 이상 유효하지 않습니다.')) return;
                        setWebhookResult(null);
                        updateNotifications({ signing_secret: '', rotate_signing_secret: true });
                      }}
                    >
                      서명 키 회전
                    </Button>
                  )}
                </Group>
                <Switch
                  checked={settings.notifications.allow_insecure_http}
                  onChange={(event) => updateNotifications({ allow_insecure_http: event.currentTarget.checked })}
                  label="Webhook 내부 HTTP 허용"
                  description="기본값은 OFF입니다. TLS를 적용할 수 없는 신뢰된 내부 개발망에서만 사용하세요."
                />
                <Checkbox.Group
                  label="알림 이벤트"
                  description="Webhook으로 전달할 이벤트를 선택합니다."
                  value={settings.notifications.events}
                  onChange={(events) => updateNotifications({ events })}
                >
                  <SimpleGrid cols={{ base: 1, sm: 2 }} mt="sm">
                    {notificationEvents.map((event) => <Checkbox key={event.value} value={event.value} label={event.label} disabled={!settings.notifications.enabled} />)}
                  </SimpleGrid>
                </Checkbox.Group>
                <Group>
                  <Button
                    type="button"
                    variant="light"
                    leftSection={webhookTestMutation.isPending ? <Loader size={16} /> : <RefreshCw size={17} />}
                    loading={webhookTestMutation.isPending}
                    disabled={!webhookTestReady}
                    onClick={() => webhookTestMutation.mutate()}
                  >
                    저장된 Webhook 연결 테스트
                  </Button>
                </Group>
                {webhookHasUnsavedChanges && <Text size="sm" c="dimmed">변경 사항을 저장하면 새 URL과 서명 Secret으로 테스트 이벤트를 보낼 수 있습니다.</Text>}
                {!webhookHasUnsavedChanges && webhookResult && <IntegrationResultAlert result={webhookResult} successTitle="Webhook 전달 확인 완료" failureTitle="Webhook 전달 확인 실패" />}
                <Text size="sm" c="dimmed">이 릴리스의 알림 채널은 서명 Webhook만 지원합니다. 이메일·SMTP 알림은 제공하지 않습니다.</Text>
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
