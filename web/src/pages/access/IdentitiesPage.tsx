import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  ActionIcon,
  Alert,
  Badge,
  Box,
  Button,
  Card,
  Group,
  Modal,
  Paper,
  PasswordInput,
  Select,
  SimpleGrid,
  Skeleton,
  Stack,
  Switch,
  Table,
  Text,
  TextInput,
  ThemeIcon,
  Title,
  Tooltip,
} from '@mantine/core';
import { useMediaQuery } from '@mantine/hooks';
import { notifications } from '@mantine/notifications';
import { CircleAlert, Pencil, Plus, RefreshCw, Save, Search, ShieldCheck, UserRound, UsersRound } from 'lucide-react';
import { get, patch, post } from '../../lib/api';
import { authSourceLabel, formatDate } from '../../lib/format';
import type { User } from '../../lib/types';
import { useAuth } from '../../contexts/AuthContext';

interface IdentityRecord extends User {
  active: boolean;
  personal_key_version?: number;
}

interface UserDraft {
  username: string;
  display_name: string;
  email: string;
  role: string;
  active: boolean;
  password: string;
  confirm_password: string;
}

const roleOptions = [
  { value: 'admin', label: '서비스 관리자' },
  { value: 'manager', label: '팀장' },
  { value: 'user', label: '일반 사용자' },
  { value: 'auditor', label: '감사자' },
];

function roleLabel(value?: string): string {
  return roleOptions.find((role) => role.value === value)?.label || value || '일반 사용자';
}

function record(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value)
    ? value as Record<string, unknown>
    : {};
}

function normalizeUsers(response: unknown): IdentityRecord[] {
  const envelope = record(response);
  const source = Array.isArray(response)
    ? response
    : Array.isArray(envelope.items)
      ? envelope.items
      : Array.isArray(envelope.users)
        ? envelope.users
        : [];
  return source.map((entry, index) => {
    const user = record(entry);
    const active = typeof user.active === 'boolean'
      ? user.active
      : typeof user.status === 'string'
        ? user.status.toLowerCase() !== 'disabled'
        : true;
    return {
      id: typeof user.id === 'string' ? user.id : String(index),
      username: typeof user.username === 'string' ? user.username : '알 수 없는 사용자',
      display_name: typeof user.display_name === 'string' ? user.display_name : '',
      email: typeof user.email === 'string' ? user.email : '',
      role: typeof user.role === 'string' ? user.role : 'user',
      status: active ? 'active' : 'disabled',
      active,
      auth_source: typeof user.auth_source === 'string' ? user.auth_source : undefined,
      personal_key_version: typeof user.personal_key_version === 'number' ? user.personal_key_version : undefined,
      last_login_at: typeof user.last_login_at === 'string' ? user.last_login_at : undefined,
      created_at: typeof user.created_at === 'string' ? user.created_at : undefined,
    };
  });
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : '요청을 처리하지 못했습니다.';
}

function emptyDraft(): UserDraft {
  return { username: '', display_name: '', email: '', role: 'user', active: true, password: '', confirm_password: '' };
}

function UserStatus({ active }: { active: boolean }) {
  return <Badge color={active ? 'teal' : 'gray'} variant="light">{active ? '활성' : '비활성'}</Badge>;
}

function IdentityLoading() {
  return (
    <Stack gap="lg" aria-label="사용자 목록을 불러오는 중">
      <Skeleton height={42} width="min(460px, 100%)" />
      <Skeleton height={390} radius="md" />
    </Stack>
  );
}

export function IdentitiesPage({ adminMode = false }: { adminMode?: boolean }) {
  const { user: currentUser, loading: authLoading } = useAuth();
  const isAdmin = currentUser?.role === 'admin';
  const isMobile = useMediaQuery('(max-width: 48em)');
  const queryClient = useQueryClient();
  const [search, setSearch] = useState('');
  const [editorOpen, setEditorOpen] = useState(false);
  const [editingUser, setEditingUser] = useState<IdentityRecord | null>(null);
  const [draft, setDraft] = useState<UserDraft>(emptyDraft);
  const [validationError, setValidationError] = useState<string | null>(null);

  const usersQuery = useQuery({
    queryKey: ['users'],
    queryFn: () => get<unknown>('/users'),
    enabled: !authLoading && Boolean(currentUser),
  });
  const users = useMemo(() => normalizeUsers(usersQuery.data), [usersQuery.data]);
  const filteredUsers = useMemo(() => {
    const needle = search.trim().toLocaleLowerCase('ko');
    if (!needle) return users;
    return users.filter((user) => [user.username, user.display_name, user.email, roleLabel(user.role)]
      .some((field) => (field || '').toLocaleLowerCase('ko').includes(needle)));
  }, [search, users]);

  const saveMutation = useMutation({
    mutationFn: () => editingUser
      ? patch<unknown>(`/users/${encodeURIComponent(editingUser.id)}`, {
          display_name: draft.display_name.trim(),
          email: draft.email.trim(),
          role: draft.role,
          active: draft.active,
        })
      : post<unknown>('/users', {
          username: draft.username.trim(),
          display_name: draft.display_name.trim(),
          email: draft.email.trim(),
          role: draft.role,
          password: draft.password,
          active: true,
        }),
    onSuccess: async () => {
      setEditorOpen(false);
      await queryClient.invalidateQueries({ queryKey: ['users'] });
      notifications.show({ color: 'teal', title: editingUser ? '사용자 수정 완료' : '사용자 생성 완료', message: 'Identity 정보를 안전하게 저장했습니다.' });
    },
  });

  const openCreate = () => {
    if (!isAdmin) return;
    saveMutation.reset();
    setEditingUser(null);
    setDraft(emptyDraft());
    setValidationError(null);
    setEditorOpen(true);
  };

  const openEdit = (user: IdentityRecord) => {
    if (!isAdmin) return;
    saveMutation.reset();
    setEditingUser(user);
    setDraft({
      username: user.username,
      display_name: user.display_name || '',
      email: user.email || '',
      role: user.role,
      active: user.active,
      password: '',
      confirm_password: '',
    });
    setValidationError(null);
    setEditorOpen(true);
  };

  const submit = () => {
    setValidationError(null);
    if (!editingUser && draft.username.trim().length < 3) { setValidationError('사용자명은 3자 이상이어야 합니다.'); return; }
    if (!draft.display_name.trim()) { setValidationError('표시 이름을 입력하세요.'); return; }
    if (!editingUser && draft.password.length < 12) { setValidationError('초기 비밀번호는 12자 이상이어야 합니다.'); return; }
    if (!editingUser && draft.password !== draft.confirm_password) { setValidationError('초기 비밀번호와 확인 값이 일치하지 않습니다.'); return; }
    saveMutation.mutate();
  };

  if (authLoading || usersQuery.isPending) return <IdentityLoading />;
  if (!currentUser) return <Alert color="red" title="로그인이 필요합니다" role="alert">사용자 Identity를 조회하려면 다시 로그인하세요.</Alert>;

  return (
    <Stack gap="lg">
      <Group justify="space-between" align="flex-end">
        <Box>
          <Title order={1} className="page-title">{adminMode ? '사용자 관리' : '사용자 Identity'}</Title>
          <Text c="dimmed" mt={6}>로컬 및 SSO 사용자의 역할, 상태와 개인 키 버전을 확인합니다.</Text>
        </Box>
        {isAdmin && <Button leftSection={<Plus size={17} />} onClick={openCreate}>사용자 만들기</Button>}
      </Group>

      {usersQuery.isError && (
        <Paper className="surface" p="xl" radius="lg">
          <Stack align="flex-start">
            <Alert icon={<CircleAlert size={18} />} color="red" title="사용자를 불러오지 못했습니다" w="100%" role="alert">{errorMessage(usersQuery.error)}</Alert>
            <Button variant="light" leftSection={<RefreshCw size={17} />} onClick={() => usersQuery.refetch()}>다시 시도</Button>
          </Stack>
        </Paper>
      )}

      {usersQuery.isSuccess && users.length === 0 && (
        <Paper className="surface" p={{ base: 'xl', sm: 48 }} radius="lg">
          <Stack align="center" ta="center">
            <ThemeIcon size={58} radius="xl" variant="light" color="gray"><UsersRound size={29} /></ThemeIcon>
            <Title order={2} size="h3">표시할 사용자가 없습니다</Title>
            <Text c="dimmed">로컬 사용자를 만들거나 Keycloak SSO 로그인을 연결하세요.</Text>
            {isAdmin && <Button leftSection={<Plus size={17} />} onClick={openCreate}>첫 사용자 만들기</Button>}
          </Stack>
        </Paper>
      )}

      {usersQuery.isSuccess && users.length > 0 && (
        <Stack gap="md">
          <TextInput
            aria-label="사용자 검색"
            placeholder="사용자명, 표시 이름, 이메일 또는 역할 검색"
            leftSection={<Search size={17} />}
            value={search}
            onChange={(event) => setSearch(event.currentTarget.value)}
          />

          {filteredUsers.length === 0 ? (
            <Alert color="blue" title="검색 결과가 없습니다">다른 검색어를 입력해 보세요.</Alert>
          ) : isMobile ? (
            <Stack gap="md">
              {filteredUsers.map((user) => (
                <Card key={user.id} withBorder radius="lg" padding="lg">
                  <Stack gap="md">
                    <Group justify="space-between" align="flex-start" wrap="nowrap">
                      <Group gap="sm" wrap="nowrap" style={{ minWidth: 0 }}>
                        <ThemeIcon variant="light"><UserRound size={18} /></ThemeIcon>
                        <Box style={{ minWidth: 0 }}>
                          <Text fw={750}>{user.display_name || user.username}</Text>
                          <Text size="sm" c="dimmed" style={{ overflowWrap: 'anywhere' }}>@{user.username} · {user.email || '이메일 없음'}</Text>
                        </Box>
                      </Group>
                      <UserStatus active={user.active} />
                    </Group>
                    <Group gap="xs"><Badge color={user.role === 'admin' ? 'violet' : 'blue'}>{roleLabel(user.role)}</Badge><Badge variant="outline">키 v{user.personal_key_version ?? 1}</Badge></Group>
                    <Text size="sm" c="dimmed">인증: {authSourceLabel(user.auth_source)} · 최근 로그인 {formatDate(user.last_login_at)}</Text>
                    {isAdmin && <Button variant="default" leftSection={<Pencil size={16} />} onClick={() => openEdit(user)}>사용자 편집</Button>}
                  </Stack>
                </Card>
              ))}
            </Stack>
          ) : (
            <Paper className="surface" radius="lg" p={0} style={{ overflow: 'hidden' }}>
              <Table.ScrollContainer minWidth={900}>
                <Table striped highlightOnHover aria-label="사용자 Identity 목록">
                  <Table.Thead><Table.Tr><Table.Th>사용자</Table.Th><Table.Th>역할</Table.Th><Table.Th>인증</Table.Th><Table.Th>개인 키</Table.Th><Table.Th>최근 로그인</Table.Th><Table.Th>상태</Table.Th>{isAdmin && <Table.Th>작업</Table.Th>}</Table.Tr></Table.Thead>
                  <Table.Tbody>
                    {filteredUsers.map((user) => (
                      <Table.Tr key={user.id}>
                        <Table.Td><Text fw={700}>{user.display_name || user.username}</Text><Text size="sm" c="dimmed">@{user.username} · {user.email || '—'}</Text></Table.Td>
                        <Table.Td><Badge color={user.role === 'admin' ? 'violet' : 'blue'}>{roleLabel(user.role)}</Badge></Table.Td>
                        <Table.Td>{authSourceLabel(user.auth_source)}</Table.Td>
                        <Table.Td>v{user.personal_key_version ?? 1}</Table.Td>
                        <Table.Td>{formatDate(user.last_login_at)}</Table.Td>
                        <Table.Td><UserStatus active={user.active} /></Table.Td>
                        {isAdmin && <Table.Td><Tooltip label="사용자 편집"><ActionIcon variant="subtle" aria-label={`${user.username} 편집`} onClick={() => openEdit(user)}><Pencil size={17} /></ActionIcon></Tooltip></Table.Td>}
                      </Table.Tr>
                    ))}
                  </Table.Tbody>
                </Table>
              </Table.ScrollContainer>
            </Paper>
          )}
        </Stack>
      )}

      <Modal opened={editorOpen} onClose={() => !saveMutation.isPending && setEditorOpen(false)} title={editingUser ? '사용자 편집' : '사용자 만들기'} centered size="lg">
        <Stack gap="lg">
          {(validationError || saveMutation.isError) && <Alert icon={<CircleAlert size={18} />} color="red" title="사용자를 저장할 수 없습니다" role="alert">{validationError || errorMessage(saveMutation.error)}</Alert>}
          <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="lg">
            <TextInput
              label="사용자명"
              description={editingUser ? '사용자명은 변경할 수 없습니다.' : '3자 이상 입력하세요.'}
              required
              readOnly={Boolean(editingUser)}
              autoComplete="username"
              value={draft.username}
              onChange={(event) => setDraft((current) => ({ ...current, username: event.currentTarget.value }))}
            />
            <TextInput label="표시 이름" required autoComplete="name" value={draft.display_name} onChange={(event) => setDraft((current) => ({ ...current, display_name: event.currentTarget.value }))} />
            <TextInput type="email" label="이메일" autoComplete="email" value={draft.email} onChange={(event) => setDraft((current) => ({ ...current, email: event.currentTarget.value }))} />
            <Select
              label="역할"
              required
              data={roleOptions}
              value={draft.role}
              disabled={editingUser?.id === currentUser.id}
              description={editingUser?.id === currentUser.id ? '현재 로그인한 계정의 역할은 여기서 낮출 수 없습니다.' : undefined}
              onChange={(value) => setDraft((current) => ({ ...current, role: value || 'user' }))}
            />
          </SimpleGrid>
          {!editingUser && (
            <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="lg">
              <PasswordInput label="초기 비밀번호" description="12자 이상 입력하세요. 더 강한 관리자 정책이 있으면 해당 길이를 따라야 합니다." required autoComplete="new-password" value={draft.password} onChange={(event) => setDraft((current) => ({ ...current, password: event.currentTarget.value }))} />
              <PasswordInput label="초기 비밀번호 확인" required autoComplete="new-password" value={draft.confirm_password} onChange={(event) => setDraft((current) => ({ ...current, confirm_password: event.currentTarget.value }))} />
            </SimpleGrid>
          )}
          {editingUser && (
            <Switch
              checked={draft.active}
              disabled={editingUser.id === currentUser.id}
              label="계정 활성"
              description={editingUser.id === currentUser.id ? '현재 로그인한 계정은 스스로 비활성화할 수 없습니다.' : '끄면 해당 사용자의 활성 세션도 서버 정책에 따라 폐기됩니다.'}
              onChange={(event) => setDraft((current) => ({ ...current, active: event.currentTarget.checked }))}
            />
          )}
          <Alert icon={<ShieldCheck size={18} />} color="blue">역할과 계정 상태 변경은 즉시 적용되고 감사 로그에 기록됩니다.</Alert>
          <Group justify="flex-end">
            <Button variant="default" onClick={() => setEditorOpen(false)} disabled={saveMutation.isPending}>취소</Button>
            <Button leftSection={<Save size={17} />} loading={saveMutation.isPending} onClick={submit}>{editingUser ? '변경 저장' : '사용자 생성'}</Button>
          </Group>
        </Stack>
      </Modal>
    </Stack>
  );
}

export default IdentitiesPage;
