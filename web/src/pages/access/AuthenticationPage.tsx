import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import {
  Alert,
  Badge,
  Box,
  Button,
  Group,
  Paper,
  SimpleGrid,
  Skeleton,
  Stack,
  Text,
  ThemeIcon,
  Title,
} from '@mantine/core';
import { Link } from 'react-router-dom';
import { CircleAlert, ExternalLink, Fingerprint, KeyRound, LockKeyhole, RefreshCw, ShieldCheck, UserRound } from 'lucide-react';
import { get } from '../../lib/api';
import { useAuth } from '../../contexts/AuthContext';

interface AuthMethod {
  id: string;
  type: string;
  name: string;
  path: string;
  description: string;
  enabled: boolean;
  configured: boolean;
  version?: string;
}

const methodMeta: Record<string, { label: string; description: string; icon: typeof KeyRound }> = {
  token: { label: '토큰', description: 'API와 자동화 요청에 만료형 토큰을 사용합니다.', icon: KeyRound },
  userpass: { label: '로컬 계정', description: '사용자명과 비밀번호로 폐쇄망에서 로그인합니다.', icon: UserRound },
  local: { label: '로컬 계정', description: '사용자명과 비밀번호로 폐쇄망에서 로그인합니다.', icon: UserRound },
  oidc: { label: 'Keycloak OIDC', description: 'Keycloak discovery와 표준 OIDC 로그인을 사용합니다.', icon: Fingerprint },
  approle: { label: 'AppRole', description: '애플리케이션이 Role ID와 Secret ID로 인증합니다.', icon: LockKeyhole },
};

function record(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value)
    ? value as Record<string, unknown>
    : {};
}

function normalizeMethods(response: unknown): AuthMethod[] {
  const envelope = record(response);
  const source = Array.isArray(response)
    ? response
    : Array.isArray(envelope.items)
      ? envelope.items
      : Array.isArray(envelope.methods)
        ? envelope.methods
        : Array.isArray(envelope.auth_methods)
          ? envelope.auth_methods
          : [];
  return source.map((entry, index) => {
    const method = record(entry);
    const type = typeof method.type === 'string' ? method.type.toLowerCase() : typeof method.id === 'string' ? method.id.toLowerCase() : 'unknown';
    const meta = methodMeta[type];
    return {
      id: typeof method.id === 'string' ? method.id : typeof method.accessor === 'string' ? method.accessor : String(index),
      type,
      name: typeof method.name === 'string' ? method.name : meta?.label || type,
      path: typeof method.path === 'string' ? method.path : type === 'token' ? 'auth/token' : `auth/${type}`,
      description: typeof method.description === 'string' && method.description ? method.description : meta?.description || '구성된 인증 방식입니다.',
      enabled: typeof method.enabled === 'boolean' ? method.enabled : typeof method.status === 'string' ? method.status !== 'disabled' : true,
      configured: typeof method.configured === 'boolean' ? method.configured : true,
      version: typeof method.version === 'string' ? method.version : undefined,
    };
  });
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : '요청을 처리하지 못했습니다.';
}

function AuthenticationLoading() {
  return (
    <Stack gap="lg" aria-label="인증 방식을 불러오는 중">
      <Skeleton height={42} width="min(500px, 100%)" />
      <SimpleGrid cols={{ base: 1, md: 2 }}><Skeleton height={240} /><Skeleton height={240} /></SimpleGrid>
    </Stack>
  );
}

export function AuthenticationPage() {
  const { user, loading: authLoading } = useAuth();
  const isAdmin = user?.role === 'admin';
  const methodsQuery = useQuery({
    queryKey: ['auth-methods'],
    queryFn: () => get<unknown>('/auth-methods'),
    enabled: !authLoading && Boolean(user),
  });
  const methods = useMemo(() => normalizeMethods(methodsQuery.data), [methodsQuery.data]);

  if (authLoading || methodsQuery.isPending) return <AuthenticationLoading />;
  if (!user) return <Alert color="red" title="로그인이 필요합니다" role="alert">인증 방식을 조회하려면 다시 로그인하세요.</Alert>;

  return (
    <Stack gap="lg">
      <Group justify="space-between" align="flex-end">
        <Box>
          <Title order={1} className="page-title">인증 방식</Title>
          <Text c="dimmed" mt={6}>사용자와 애플리케이션이 Jikim에 신원을 증명하는 방법을 확인합니다.</Text>
        </Box>
        {isAdmin && (
          <Button component={Link} to="/admin/settings?tab=oidc" leftSection={<Fingerprint size={17} />} rightSection={<ExternalLink size={15} />}>
            Keycloak 설정
          </Button>
        )}
      </Group>

      <Alert icon={<ShieldCheck size={18} />} color="blue" title="복구 가능한 인증 경로를 유지하세요">
        Keycloak을 사용하더라도 장애 복구를 위해 보호된 로컬 관리자 계정을 최소 한 개 유지하는 것을 권장합니다.
      </Alert>

      {methodsQuery.isError && (
        <Paper className="surface" p="xl" radius="lg">
          <Stack align="flex-start">
            <Alert icon={<CircleAlert size={18} />} color="red" title="인증 방식을 불러오지 못했습니다" w="100%" role="alert">{errorMessage(methodsQuery.error)}</Alert>
            <Button variant="light" leftSection={<RefreshCw size={17} />} onClick={() => methodsQuery.refetch()}>다시 시도</Button>
          </Stack>
        </Paper>
      )}

      {methodsQuery.isSuccess && methods.length === 0 && (
        <Paper className="surface" p={{ base: 'xl', sm: 48 }} radius="lg">
          <Stack align="center" ta="center">
            <ThemeIcon size={58} radius="xl" color="gray" variant="light"><Fingerprint size={29} /></ThemeIcon>
            <Title order={2} size="h3">표시할 인증 방식이 없습니다</Title>
            <Text c="dimmed" maw={540}>서버의 기본 인증 구성을 확인하세요. 관리자는 Keycloak OIDC 연결을 설정할 수 있습니다.</Text>
            {isAdmin && <Button component={Link} to="/admin/settings?tab=oidc" leftSection={<Fingerprint size={17} />}>Keycloak 설정 열기</Button>}
          </Stack>
        </Paper>
      )}

      {methodsQuery.isSuccess && methods.length > 0 && (
        <SimpleGrid cols={{ base: 1, md: 2, xl: 3 }} spacing="lg">
          {methods.map((method) => {
            const meta = methodMeta[method.type];
            const Icon = meta?.icon || Fingerprint;
            return (
              <Paper key={method.id} className="surface" p="lg" radius="lg">
                <Stack gap="lg">
                  <Group justify="space-between" align="flex-start" wrap="nowrap">
                    <ThemeIcon size={46} radius="md" variant="light" color={method.enabled ? 'teal' : 'gray'}><Icon size={23} /></ThemeIcon>
                    <Group gap={6}>
                      <Badge color={method.enabled ? 'teal' : 'gray'}>{method.enabled ? '활성' : '비활성'}</Badge>
                      {!method.configured && <Badge color="orange">설정 필요</Badge>}
                    </Group>
                  </Group>
                  <Box>
                    <Title order={2} size="h3">{method.name || meta?.label}</Title>
                    <Text c="dimmed" mt={6}>{method.description}</Text>
                  </Box>
                  <Paper withBorder p="sm" radius="md">
                    <Text size="xs" c="dimmed">Mount path</Text>
                    <Text ff="monospace" fw={650} mt={3} style={{ overflowWrap: 'anywhere' }}>{method.path}</Text>
                  </Paper>
                  {method.version && <Text size="sm" c="dimmed">플러그인 버전 {method.version}</Text>}
                  {method.type === 'oidc' && isAdmin && (
                    <Button component={Link} to="/admin/settings?tab=oidc" variant="light" leftSection={<Fingerprint size={16} />}>
                      OIDC 설정 관리
                    </Button>
                  )}
                </Stack>
              </Paper>
            );
          })}
        </SimpleGrid>
      )}

      {!isAdmin && (
        <Text size="sm" c="dimmed">인증 방식 변경이 필요하면 서비스 관리자에게 요청하세요.</Text>
      )}
    </Stack>
  );
}

export default AuthenticationPage;
