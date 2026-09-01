import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Alert,
  Badge,
  Box,
  Button,
  Card,
  Code,
  CopyButton,
  Group,
  Modal,
  NumberInput,
  Paper,
  SimpleGrid,
  Skeleton,
  Stack,
  Table,
  Text,
  TextInput,
  ThemeIcon,
  Title,
} from '@mantine/core';
import { useMediaQuery } from '@mantine/hooks';
import { notifications } from '@mantine/notifications';
import { Check, CircleAlert, Clock3, Copy, KeyRound, Plus, RefreshCw, ShieldX } from 'lucide-react';
import { get, post } from '../../lib/api';
import { formatDate } from '../../lib/format';
import { useAuth } from '../../contexts/AuthContext';

interface TokenRecord {
  id: string;
  name: string;
  kind: string;
  owner: string;
  created_at?: string;
  expires_at?: string;
  last_seen_at?: string;
  revoked_at?: string;
}

interface CreatedToken {
  token: string;
  id?: string;
  name?: string;
  expires_at?: string;
}

function record(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value)
    ? value as Record<string, unknown>
    : {};
}

function normalizeTokens(response: unknown): TokenRecord[] {
  const envelope = record(response);
  const source = Array.isArray(response)
    ? response
    : Array.isArray(envelope.items)
      ? envelope.items
      : Array.isArray(envelope.tokens)
        ? envelope.tokens
        : Array.isArray(envelope.sessions)
          ? envelope.sessions
          : [];
  return source.map((entry, index) => {
    const token = record(entry);
    const owner = record(token.user);
    return {
      id: typeof token.id === 'string' ? token.id : String(index),
      name: typeof token.name === 'string' && token.name ? token.name : '이름 없는 토큰',
      kind: typeof token.kind === 'string' ? token.kind : 'api',
      owner: typeof token.owner === 'string'
        ? token.owner
        : typeof token.username === 'string'
          ? token.username
          : typeof owner.username === 'string'
            ? owner.username
            : '—',
      created_at: typeof token.created_at === 'string' ? token.created_at : undefined,
      expires_at: typeof token.expires_at === 'string' ? token.expires_at : undefined,
      last_seen_at: typeof token.last_seen_at === 'string' ? token.last_seen_at : undefined,
      revoked_at: typeof token.revoked_at === 'string' ? token.revoked_at : undefined,
    };
  });
}

function normalizeCreatedToken(response: unknown): CreatedToken {
  const envelope = record(response);
  const nested = record(envelope.token_data ?? envelope.session ?? envelope.data);
  const token = typeof envelope.token === 'string'
    ? envelope.token
    : typeof envelope.client_token === 'string'
      ? envelope.client_token
      : typeof nested.token === 'string'
        ? nested.token
        : '';
  return {
    token,
    id: typeof envelope.id === 'string' ? envelope.id : typeof nested.id === 'string' ? nested.id : undefined,
    name: typeof envelope.name === 'string' ? envelope.name : typeof nested.name === 'string' ? nested.name : undefined,
    expires_at: typeof envelope.expires_at === 'string' ? envelope.expires_at : typeof nested.expires_at === 'string' ? nested.expires_at : undefined,
  };
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : '요청을 처리하지 못했습니다.';
}

function tokenStatus(token: TokenRecord): { label: string; color: string } {
  if (token.revoked_at) return { label: '폐기', color: 'gray' };
  if (token.expires_at && new Date(token.expires_at).getTime() <= Date.now()) return { label: '만료', color: 'orange' };
  return { label: '활성', color: 'teal' };
}

function TokenBadge({ token }: { token: TokenRecord }) {
  const status = tokenStatus(token);
  return <Badge color={status.color} variant="light">{status.label}</Badge>;
}

function TokensLoading() {
  return (
    <Stack gap="lg" aria-label="API 토큰을 불러오는 중">
      <Skeleton height={42} width="min(460px, 100%)" />
      <Skeleton height={360} radius="md" />
    </Stack>
  );
}

export function TokensPage() {
  const { user, loading: authLoading } = useAuth();
  const isAdmin = user?.role === 'admin';
  const isMobile = useMediaQuery('(max-width: 48em)');
  const queryClient = useQueryClient();
  const [createOpen, setCreateOpen] = useState(false);
  const [tokenName, setTokenName] = useState('');
  const [ttlHours, setTtlHours] = useState(12);
  const [validationError, setValidationError] = useState<string | null>(null);
  const [createdToken, setCreatedToken] = useState<CreatedToken | null>(null);
  const [revokeTarget, setRevokeTarget] = useState<TokenRecord | null>(null);

  const tokensQuery = useQuery({
    queryKey: ['tokens'],
    queryFn: () => get<unknown>('/tokens'),
    enabled: !authLoading && Boolean(user),
  });
  const tokens = useMemo(() => normalizeTokens(tokensQuery.data), [tokensQuery.data]);

  const createMutation = useMutation({
    mutationFn: () => post<unknown>('/tokens', { name: tokenName.trim(), ttl_seconds: ttlHours * 3600 }),
    onSuccess: async (response) => {
      const created = normalizeCreatedToken(response);
      setCreatedToken(created);
      setCreateOpen(false);
      await queryClient.invalidateQueries({ queryKey: ['tokens'] });
      notifications.show({ color: 'teal', title: 'API 토큰 생성 완료', message: '토큰 평문은 이번 한 번만 표시됩니다.' });
    },
  });

  const revokeMutation = useMutation({
    mutationFn: (token: TokenRecord) => post<unknown>(`/tokens/${encodeURIComponent(token.id)}/revoke`, {}),
    onSuccess: async () => {
      setRevokeTarget(null);
      await queryClient.invalidateQueries({ queryKey: ['tokens'] });
      notifications.show({ color: 'teal', title: '토큰 폐기 완료', message: '선택한 토큰을 더 이상 사용할 수 없습니다.' });
    },
  });

  const openCreate = () => {
    if (!isAdmin) return;
    createMutation.reset();
    setTokenName('');
    setTtlHours(12);
    setValidationError(null);
    setCreateOpen(true);
  };

  const submitCreate = () => {
    setValidationError(null);
    if (!tokenName.trim()) { setValidationError('토큰을 구분할 이름을 입력하세요.'); return; }
    if (ttlHours < 1 || ttlHours > 720) { setValidationError('유효기간은 1~720시간 사이여야 합니다.'); return; }
    createMutation.mutate();
  };

  if (authLoading || tokensQuery.isPending) return <TokensLoading />;
  if (!user) return <Alert color="red" title="로그인이 필요합니다" role="alert">API 토큰을 조회하려면 다시 로그인하세요.</Alert>;

  return (
    <Stack gap="lg">
      <Group justify="space-between" align="flex-end">
        <Box>
          <Title order={1} className="page-title">API 토큰</Title>
          <Text c="dimmed" mt={6}>자동화에 사용하는 만료형 토큰을 발급하고 즉시 폐기합니다.</Text>
        </Box>
        {isAdmin && <Button leftSection={<Plus size={17} />} onClick={openCreate}>토큰 발급</Button>}
      </Group>

      <Alert icon={<KeyRound size={18} />} color="blue" title="토큰 평문은 생성 직후 한 번만 표시됩니다">
        장기 토큰 대신 필요한 최소 유효기간을 사용하고, 사용하지 않는 토큰은 즉시 폐기하세요.
      </Alert>

      {tokensQuery.isError && (
        <Paper className="surface" p="xl" radius="lg">
          <Stack align="flex-start">
            <Alert icon={<CircleAlert size={18} />} color="red" title="토큰을 불러오지 못했습니다" w="100%" role="alert">{errorMessage(tokensQuery.error)}</Alert>
            <Button variant="light" leftSection={<RefreshCw size={17} />} onClick={() => tokensQuery.refetch()}>다시 시도</Button>
          </Stack>
        </Paper>
      )}

      {tokensQuery.isSuccess && tokens.length === 0 && (
        <Paper className="surface" p={{ base: 'xl', sm: 48 }} radius="lg">
          <Stack align="center" ta="center">
            <ThemeIcon size={58} radius="xl" variant="light" color="gray"><KeyRound size={29} /></ThemeIcon>
            <Title order={2} size="h3">발급된 API 토큰이 없습니다</Title>
            <Text c="dimmed">자동화가 필요할 때 최소 유효기간으로 토큰을 발급하세요.</Text>
            {isAdmin && <Button leftSection={<Plus size={17} />} onClick={openCreate}>첫 토큰 발급</Button>}
          </Stack>
        </Paper>
      )}

      {tokensQuery.isSuccess && tokens.length > 0 && (isMobile ? (
        <Stack gap="md">
          {tokens.map((token) => (
            <Card key={token.id} withBorder radius="lg" padding="lg">
              <Stack gap="md">
                <Group justify="space-between" align="flex-start"><Box><Text fw={750}>{token.name}</Text><Text size="sm" c="dimmed">{token.kind.toUpperCase()} · {token.owner}</Text></Box><TokenBadge token={token} /></Group>
                <SimpleGrid cols={2}><Box><Text size="xs" c="dimmed">생성</Text><Text size="sm" fw={650}>{formatDate(token.created_at)}</Text></Box><Box><Text size="xs" c="dimmed">만료</Text><Text size="sm" fw={650}>{formatDate(token.expires_at)}</Text></Box></SimpleGrid>
                {isAdmin && !token.revoked_at && <Button color="red" variant="light" leftSection={<ShieldX size={16} />} onClick={() => { revokeMutation.reset(); setRevokeTarget(token); }}>토큰 폐기</Button>}
              </Stack>
            </Card>
          ))}
        </Stack>
      ) : (
        <Paper className="surface" radius="lg" p={0} style={{ overflow: 'hidden' }}>
          <Table.ScrollContainer minWidth={850}>
            <Table striped highlightOnHover aria-label="API 토큰 목록">
              <Table.Thead><Table.Tr><Table.Th>이름</Table.Th><Table.Th>소유자</Table.Th><Table.Th>종류</Table.Th><Table.Th>생성</Table.Th><Table.Th>최근 사용</Table.Th><Table.Th>만료</Table.Th><Table.Th>상태</Table.Th>{isAdmin && <Table.Th>작업</Table.Th>}</Table.Tr></Table.Thead>
              <Table.Tbody>
                {tokens.map((token) => (
                  <Table.Tr key={token.id}>
                    <Table.Td><Text fw={700}>{token.name}</Text><Text size="xs" c="dimmed" ff="monospace">{token.id}</Text></Table.Td>
                    <Table.Td>{token.owner}</Table.Td><Table.Td>{token.kind.toUpperCase()}</Table.Td><Table.Td>{formatDate(token.created_at)}</Table.Td><Table.Td>{formatDate(token.last_seen_at)}</Table.Td><Table.Td>{formatDate(token.expires_at)}</Table.Td><Table.Td><TokenBadge token={token} /></Table.Td>
                    {isAdmin && <Table.Td>{!token.revoked_at && <Button size="sm" color="red" variant="subtle" leftSection={<ShieldX size={15} />} onClick={() => { revokeMutation.reset(); setRevokeTarget(token); }}>폐기</Button>}</Table.Td>}
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        </Paper>
      ))}

      <Modal opened={createOpen} onClose={() => !createMutation.isPending && setCreateOpen(false)} title="API 토큰 발급" centered>
        <Stack gap="lg">
          {(validationError || createMutation.isError) && <Alert icon={<CircleAlert size={18} />} color="red" title="토큰을 발급할 수 없습니다" role="alert">{validationError || errorMessage(createMutation.error)}</Alert>}
          <TextInput label="토큰 이름" description="사용 목적을 알 수 있는 이름을 입력하세요." placeholder="deployment-automation" required value={tokenName} onChange={(event) => setTokenName(event.currentTarget.value)} />
          <NumberInput label="유효기간(시간)" description="1~720시간. 필요한 최소 기간을 권장합니다." min={1} max={720} clampBehavior="strict" required value={ttlHours} onChange={(value) => setTtlHours(typeof value === 'number' ? value : 12)} />
          <Alert icon={<Clock3 size={18} />} color="orange">만료 후에는 갱신되지 않으며 새 토큰을 발급해야 합니다.</Alert>
          <Group justify="flex-end"><Button variant="default" onClick={() => setCreateOpen(false)} disabled={createMutation.isPending}>취소</Button><Button leftSection={<Plus size={17} />} loading={createMutation.isPending} onClick={submitCreate}>발급</Button></Group>
        </Stack>
      </Modal>

      <Modal opened={Boolean(createdToken)} onClose={() => setCreatedToken(null)} title="새 API 토큰" centered closeOnClickOutside={false}>
        <Stack gap="lg">
          {createdToken?.token ? (
            <>
              <Alert color="orange" title="지금 복사하세요">이 창을 닫으면 토큰 평문을 다시 확인할 수 없습니다.</Alert>
              <Code block style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', fontSize: '0.95rem' }}>{createdToken.token}</Code>
              <CopyButton value={createdToken.token} timeout={1800}>
                {({ copied, copy }) => <Button color={copied ? 'teal' : 'blue'} leftSection={copied ? <Check size={17} /> : <Copy size={17} />} onClick={copy}>{copied ? '복사됨' : '토큰 복사'}</Button>}
              </CopyButton>
              {createdToken.expires_at && <Text size="sm" c="dimmed">만료: {formatDate(createdToken.expires_at)}</Text>}
            </>
          ) : (
            <Alert icon={<CircleAlert size={18} />} color="red" title="토큰 값이 응답에 없습니다">목록에는 생성됐지만 평문이 반환되지 않았습니다. 해당 토큰을 폐기하고 다시 발급하세요.</Alert>
          )}
          <Group justify="flex-end"><Button variant="default" onClick={() => setCreatedToken(null)}>확인하고 닫기</Button></Group>
        </Stack>
      </Modal>

      <Modal opened={Boolean(revokeTarget)} onClose={() => !revokeMutation.isPending && setRevokeTarget(null)} title="토큰 폐기 확인" centered>
        <Stack gap="lg">
          <Alert icon={<ShieldX size={18} />} color="red" title="이 작업은 되돌릴 수 없습니다"><Text><Text component="span" fw={750}>{revokeTarget?.name}</Text> 토큰을 즉시 폐기합니다. 연결된 자동화가 중단될 수 있습니다.</Text></Alert>
          {revokeMutation.isError && <Alert color="red" role="alert">{errorMessage(revokeMutation.error)}</Alert>}
          <Group justify="flex-end"><Button variant="default" onClick={() => setRevokeTarget(null)} disabled={revokeMutation.isPending}>취소</Button><Button color="red" leftSection={<ShieldX size={17} />} loading={revokeMutation.isPending} onClick={() => revokeTarget && revokeMutation.mutate(revokeTarget)}>즉시 폐기</Button></Group>
        </Stack>
      </Modal>
    </Stack>
  );
}

export default TokensPage;
