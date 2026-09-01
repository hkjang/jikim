import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Alert,
  Badge,
  Box,
  Button,
  Card,
  Checkbox,
  Group,
  Modal,
  Paper,
  SimpleGrid,
  Skeleton,
  Stack,
  Table,
  Text,
  ThemeIcon,
  Title,
  Tooltip,
} from '@mantine/core';
import { useMediaQuery } from '@mantine/hooks';
import { notifications } from '@mantine/notifications';
import {
  CircleAlert,
  KeyRound,
  LockKeyhole,
  RefreshCw,
  RotateCw,
  Settings2,
  ShieldCheck,
} from 'lucide-react';
import { get, patch, post } from '../../lib/api';
import { formatDate, statusLabel } from '../../lib/format';
import type { KeyRecord } from '../../lib/types';
import { useAuth } from '../../contexts/AuthContext';

interface PersonalKey extends Omit<KeyRecord, 'permissions'> {
  permissions: string[];
  last_used_at?: string;
}

const availablePermissions = [
  { value: 'encrypt', label: '암호화', description: '이 키로 데이터를 암호화합니다.' },
  { value: 'decrypt', label: '복호화', description: '기존 Secret 복구에 필요한 필수 권한으로 해제할 수 없습니다.' },
  { value: 'rotate', label: '회전 요청', description: '새 키 버전을 생성하도록 요청합니다.' },
];

const permissionLabels: Record<string, string> = Object.fromEntries(
  availablePermissions.map((permission) => [permission.value, permission.label]),
);

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : '요청을 처리하지 못했습니다.';
}

function record(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value)
    ? value as Record<string, unknown>
    : {};
}

function normalizeKeys(response: unknown): PersonalKey[] {
  const envelope = record(response);
  const source = Array.isArray(response)
    ? response
    : Array.isArray(envelope.items)
      ? envelope.items
      : Array.isArray(envelope.keys)
        ? envelope.keys
        : [];

  return source.map((value, index) => {
    const item = record(value);
    const permissionObject = record(item.permissions);
    const permissions = Array.isArray(item.permissions)
      ? item.permissions.filter((permission): permission is string => typeof permission === 'string')
      : Array.isArray(item.capabilities)
        ? item.capabilities.filter((permission): permission is string => typeof permission === 'string')
        : Object.entries(permissionObject).filter(([, enabled]) => enabled === true).map(([permission]) => permission);
    return {
      id: typeof item.id === 'string' ? item.id : String(index),
      name: typeof item.name === 'string' ? item.name : '이름 없는 키',
      type: typeof item.type === 'string' ? item.type : undefined,
      algorithm: typeof item.algorithm === 'string' ? item.algorithm : undefined,
      version: typeof item.version === 'number' ? item.version : undefined,
      status: typeof item.status === 'string' ? item.status : item.active === false ? 'retired' : 'active',
      owner: typeof item.owner === 'string' ? item.owner : undefined,
      rotated_at: typeof item.rotated_at === 'string' ? item.rotated_at : undefined,
      next_rotation_at: typeof item.next_rotation_at === 'string' ? item.next_rotation_at : undefined,
      created_at: typeof item.created_at === 'string' ? item.created_at : undefined,
      last_used_at: typeof item.last_used_at === 'string' ? item.last_used_at : undefined,
      permissions,
    };
  });
}

function permissionsText(permissions: string[]): string {
  if (!permissions.length) return '부여된 권한 없음';
  return permissions.map((permission) => permissionLabels[permission] || permission).join(', ');
}

function KeyStatusBadge({ status }: { status?: string }) {
  const inactive = ['disabled', 'revoked', 'retired'].includes((status || '').toLowerCase());
  return <Badge color={inactive ? 'gray' : 'teal'} variant="light">{statusLabel(status || 'active')}</Badge>;
}

interface KeyActionsProps {
  keyRecord: PersonalKey;
  onRotate: (key: PersonalKey) => void;
  onPermissions: (key: PersonalKey) => void;
}

function KeyActions({ keyRecord, onRotate, onPermissions }: KeyActionsProps) {
  const unavailable = ['disabled', 'revoked', 'retired'].includes((keyRecord.status || '').toLowerCase());
  return (
    <Group gap="xs" wrap="nowrap">
      <Tooltip label={unavailable ? '비활성 키는 회전할 수 없습니다.' : '새 키 버전 생성'}>
        <span>
          <Button
            size="sm"
            variant="light"
            leftSection={<RotateCw size={15} />}
            disabled={unavailable}
            onClick={() => onRotate(keyRecord)}
          >
            회전
          </Button>
        </span>
      </Tooltip>
      <Button size="sm" variant="default" leftSection={<Settings2 size={15} />} disabled={unavailable} onClick={() => onPermissions(keyRecord)}>
        권한
      </Button>
    </Group>
  );
}

function MobileKeyCard({ keyRecord, onRotate, onPermissions }: KeyActionsProps) {
  return (
    <Card withBorder radius="lg" padding="lg">
      <Stack gap="md">
        <Group justify="space-between" align="flex-start" wrap="nowrap">
          <Group gap="sm" wrap="nowrap" style={{ minWidth: 0 }}>
            <ThemeIcon variant="light" size="lg" aria-hidden="true"><KeyRound size={20} /></ThemeIcon>
            <Box style={{ minWidth: 0 }}>
              <Text fw={750} style={{ overflowWrap: 'anywhere' }}>{keyRecord.name}</Text>
              <Text size="sm" c="dimmed">{keyRecord.algorithm || keyRecord.type || '알고리즘 정보 없음'}</Text>
            </Box>
          </Group>
          <KeyStatusBadge status={keyRecord.status} />
        </Group>
        <SimpleGrid cols={2} spacing="sm">
          <Box>
            <Text size="xs" c="dimmed">버전</Text>
            <Text fw={650}>v{keyRecord.version ?? 1}</Text>
          </Box>
          <Box>
            <Text size="xs" c="dimmed">최근 회전</Text>
            <Text fw={650} size="sm">{formatDate(keyRecord.rotated_at || keyRecord.created_at)}</Text>
          </Box>
        </SimpleGrid>
        <Box>
          <Text size="xs" c="dimmed" mb={6}>내 권한</Text>
          <Group gap={6}>
            {keyRecord.permissions.length
              ? keyRecord.permissions.map((permission) => <Badge key={permission} color="blue" variant="light">{permissionLabels[permission] || permission}</Badge>)
              : <Text size="sm" c="dimmed">부여된 권한이 없습니다.</Text>}
          </Group>
        </Box>
        <KeyActions keyRecord={keyRecord} onRotate={onRotate} onPermissions={onPermissions} />
      </Stack>
    </Card>
  );
}

function KeysLoading() {
  return (
    <Stack gap="lg" aria-label="개인 키를 불러오는 중">
      <Skeleton height={42} width="min(460px, 100%)" />
      <Skeleton height={280} radius="md" />
    </Stack>
  );
}

export function PersonalKeysPage() {
  const { user, loading: authLoading } = useAuth();
  const queryClient = useQueryClient();
  const isMobile = useMediaQuery('(max-width: 48em)');
  const [rotationTarget, setRotationTarget] = useState<PersonalKey | null>(null);
  const [permissionTarget, setPermissionTarget] = useState<PersonalKey | null>(null);
  const [permissionDraft, setPermissionDraft] = useState<string[]>([]);

  const keysQuery = useQuery({
    queryKey: ['personal-keys'],
    queryFn: () => get<unknown>('/keys?mine=true'),
    enabled: !authLoading && Boolean(user),
  });
  const keys = useMemo(() => normalizeKeys(keysQuery.data), [keysQuery.data]);

  const rotationMutation = useMutation({
    mutationFn: (key: PersonalKey) => post<unknown>(`/keys/${encodeURIComponent(key.id)}/rotate`, {}),
    onSuccess: async () => {
      setRotationTarget(null);
      await queryClient.invalidateQueries({ queryKey: ['personal-keys'] });
      notifications.show({
        color: 'teal',
        title: '개인 키 회전 완료',
        message: '새 키 버전을 활성화했습니다. 기존 암호문은 계속 사용할 수 있습니다.',
      });
    },
  });

  const permissionMutation = useMutation({
    mutationFn: ({ key, permissions }: { key: PersonalKey; permissions: string[] }) =>
      patch<unknown>(`/keys/${encodeURIComponent(key.id)}/permissions`, { permissions }),
    onSuccess: async () => {
      setPermissionTarget(null);
      await queryClient.invalidateQueries({ queryKey: ['personal-keys'] });
      notifications.show({ color: 'teal', title: '키 권한 저장 완료', message: '개인 키 권한을 업데이트했습니다.' });
    },
  });

  const openPermissions = (key: PersonalKey) => {
    permissionMutation.reset();
    setPermissionDraft(Array.from(new Set([...key.permissions, 'decrypt'])));
    setPermissionTarget(key);
  };

  const openRotation = (key: PersonalKey) => {
    rotationMutation.reset();
    setRotationTarget(key);
  };

  if (authLoading) return <KeysLoading />;

  if (!user) {
    return (
      <Alert icon={<LockKeyhole size={20} />} color="red" title="로그인이 필요합니다" role="alert">
        개인 키를 확인하려면 다시 로그인하세요.
      </Alert>
    );
  }

  return (
    <Stack gap="lg">
      <Group justify="space-between" align="flex-end">
        <Box>
          <Title order={1} className="page-title">개인 키 관리</Title>
          <Text c="dimmed" mt={6}>내 키 버전과 사용 권한을 확인하고 안전하게 회전합니다.</Text>
        </Box>
        <Button
          variant="default"
          leftSection={<RefreshCw size={17} />}
          onClick={() => keysQuery.refetch()}
          loading={keysQuery.isFetching}
        >
          새로고침
        </Button>
      </Group>

      <Alert icon={<ShieldCheck size={18} />} color="blue" title="키 회전은 기존 데이터를 손상시키지 않습니다">
        Jikim은 버전별 사용자 키로 데이터 키를 감쌉니다. 회전 후에도 이전 버전으로 암호화된 데이터를 복호화할 수 있으며 모든 작업은 감사 로그에 남습니다.
      </Alert>

      {keysQuery.isPending && <KeysLoading />}

      {keysQuery.isError && (
        <Paper className="surface" p="xl" radius="lg">
          <Stack align="flex-start">
            <Alert icon={<CircleAlert size={20} />} color="red" title="개인 키를 불러오지 못했습니다" w="100%" role="alert">
              {errorMessage(keysQuery.error)}
            </Alert>
            <Button variant="light" leftSection={<RefreshCw size={17} />} onClick={() => keysQuery.refetch()}>
              다시 시도
            </Button>
          </Stack>
        </Paper>
      )}

      {keysQuery.isSuccess && keys.length === 0 && (
        <Paper className="surface" p={{ base: 'xl', sm: 48 }} radius="lg">
          <Stack align="center" gap="sm" ta="center">
            <ThemeIcon size={56} radius="xl" variant="light" color="gray" aria-hidden="true"><KeyRound size={28} /></ThemeIcon>
            <Title order={2} size="h3">할당된 개인 키가 없습니다</Title>
            <Text c="dimmed" maw={520}>개인 키가 필요한 경우 서비스 관리자에게 키 생성 또는 할당을 요청하세요.</Text>
          </Stack>
        </Paper>
      )}

      {keysQuery.isSuccess && keys.length > 0 && (isMobile ? (
        <Stack gap="md">
          {keys.map((key) => (
            <MobileKeyCard key={key.id} keyRecord={key} onRotate={openRotation} onPermissions={openPermissions} />
          ))}
        </Stack>
      ) : (
        <Paper className="surface" radius="lg" p={0} style={{ overflow: 'hidden' }}>
          <Table.ScrollContainer minWidth={920}>
            <Table striped highlightOnHover aria-label="개인 키 목록">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>키</Table.Th>
                  <Table.Th>알고리즘</Table.Th>
                  <Table.Th>버전</Table.Th>
                  <Table.Th>상태</Table.Th>
                  <Table.Th>권한</Table.Th>
                  <Table.Th>최근 회전</Table.Th>
                  <Table.Th>작업</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {keys.map((key) => (
                  <Table.Tr key={key.id}>
                    <Table.Td>
                      <Group gap="sm" wrap="nowrap">
                        <ThemeIcon variant="light" size="md" aria-hidden="true"><KeyRound size={17} /></ThemeIcon>
                        <Box>
                          <Text fw={700}>{key.name}</Text>
                          <Text size="xs" c="dimmed" ff="monospace">{key.id}</Text>
                        </Box>
                      </Group>
                    </Table.Td>
                    <Table.Td>{key.algorithm || key.type || '—'}</Table.Td>
                    <Table.Td><Badge variant="outline">v{key.version ?? 1}</Badge></Table.Td>
                    <Table.Td><KeyStatusBadge status={key.status} /></Table.Td>
                    <Table.Td>
                      <Tooltip multiline maw={360} label={permissionsText(key.permissions)}>
                        <Text lineClamp={1} maw={220}>{permissionsText(key.permissions)}</Text>
                      </Tooltip>
                    </Table.Td>
                    <Table.Td>{formatDate(key.rotated_at || key.created_at)}</Table.Td>
                    <Table.Td><KeyActions keyRecord={key} onRotate={openRotation} onPermissions={openPermissions} /></Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        </Paper>
      ))}

      <Modal
        opened={Boolean(rotationTarget)}
        onClose={() => !rotationMutation.isPending && setRotationTarget(null)}
        title="개인 키 회전 확인"
        centered
        closeOnClickOutside={!rotationMutation.isPending}
        closeOnEscape={!rotationMutation.isPending}
      >
        <Stack gap="lg">
          <Alert icon={<RotateCw size={18} />} color="orange" title="새 키 버전을 생성합니다">
            <Text>
              <Text component="span" fw={750}>{rotationTarget?.name}</Text> 키를 회전하면 새 버전이 즉시 기본값이 됩니다.
              기존 버전은 복호화를 위해 보존됩니다.
            </Text>
          </Alert>
          {rotationMutation.isError && (
            <Alert icon={<CircleAlert size={18} />} color="red" title="키를 회전하지 못했습니다" role="alert">
              {errorMessage(rotationMutation.error)}
            </Alert>
          )}
          <Stack gap={5}>
            <Text size="sm" c="dimmed">현재 버전</Text>
            <Text fw={700}>v{rotationTarget?.version ?? 1} → v{(rotationTarget?.version ?? 1) + 1}</Text>
          </Stack>
          <Group justify="flex-end">
            <Button variant="default" onClick={() => setRotationTarget(null)} disabled={rotationMutation.isPending}>취소</Button>
            <Button
              color="orange"
              leftSection={<RotateCw size={17} />}
              loading={rotationMutation.isPending}
              onClick={() => rotationTarget && rotationMutation.mutate(rotationTarget)}
            >
              회전 실행
            </Button>
          </Group>
        </Stack>
      </Modal>

      <Modal
        opened={Boolean(permissionTarget)}
        onClose={() => !permissionMutation.isPending && setPermissionTarget(null)}
        title="개인 키 권한 편집"
        centered
        closeOnClickOutside={!permissionMutation.isPending}
        closeOnEscape={!permissionMutation.isPending}
      >
        <Stack gap="lg">
          <Box>
            <Text fw={750}>{permissionTarget?.name}</Text>
            <Text size="sm" c="dimmed" mt={3}>이 개인 키의 암호화·회전 capability를 선택합니다. 기존 Secret을 복구할 수 있도록 복호화 capability는 항상 유지됩니다.</Text>
          </Box>
          {permissionMutation.isError && (
            <Alert icon={<CircleAlert size={18} />} color="red" title="권한을 저장하지 못했습니다" role="alert">
              {errorMessage(permissionMutation.error)}
            </Alert>
          )}
          <Checkbox.Group label="키 권한" value={permissionDraft} onChange={setPermissionDraft}>
            <Stack gap="sm" mt="sm">
              {availablePermissions.map((permission) => (
                <Checkbox
                  key={permission.value}
                  value={permission.value}
                  label={permission.label}
                  description={permission.description}
                  disabled={permission.value === 'decrypt'}
                />
              ))}
            </Stack>
          </Checkbox.Group>
          <Group justify="flex-end">
            <Button variant="default" onClick={() => setPermissionTarget(null)} disabled={permissionMutation.isPending}>취소</Button>
            <Button
              leftSection={<Settings2 size={17} />}
              loading={permissionMutation.isPending}
              onClick={() => permissionTarget && permissionMutation.mutate({ key: permissionTarget, permissions: permissionDraft })}
            >
              권한 저장
            </Button>
          </Group>
        </Stack>
      </Modal>
    </Stack>
  );
}

export default PersonalKeysPage;
