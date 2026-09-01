import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  ActionIcon,
  Alert,
  Badge,
  Box,
  Button,
  Checkbox,
  Divider,
  Group,
  Modal,
  MultiSelect,
  Paper,
  Select,
  SimpleGrid,
  Skeleton,
  Stack,
  Text,
  Textarea,
  TextInput,
  ThemeIcon,
  Title,
  Tooltip,
} from '@mantine/core';
import { notifications } from '@mantine/notifications';
import {
  CheckCircle2,
  CircleAlert,
  FileKey2,
  FlaskConical,
  Pencil,
  Plus,
  RefreshCw,
  Save,
  ShieldX,
  Trash2,
  Users,
} from 'lucide-react';
import { del, get, patch, post, put } from '../../lib/api';
import { formatDate } from '../../lib/format';
import { useAuth } from '../../contexts/AuthContext';

type Capability = 'create' | 'read' | 'update' | 'delete' | 'list' | 'rotate' | 'encrypt' | 'decrypt';

interface PathRule {
  path: string;
  capabilities: Capability[];
}

interface PolicyRecord {
  id: string;
  name: string;
  description: string;
  rules: PathRule[];
  version: number;
  updated_at?: string;
  created_at?: string;
}

interface PolicyDraft {
  name: string;
  description: string;
  rules: PathRule[];
}

interface UserOption {
  id: string;
  username: string;
  display_name?: string;
  role?: string;
  active?: boolean;
}

const capabilities: Array<{ value: Capability; label: string }> = [
  { value: 'create', label: '생성' },
  { value: 'read', label: '조회' },
  { value: 'update', label: '변경' },
  { value: 'delete', label: '삭제' },
  { value: 'list', label: '목록' },
  { value: 'rotate', label: '회전' },
  { value: 'encrypt', label: '암호화' },
  { value: 'decrypt', label: '복호화' },
];

const emptyDraft = (): PolicyDraft => ({
  name: '',
  description: '',
  rules: [{ path: '*', capabilities: ['read', 'list'] }],
});

function record(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value)
    ? value as Record<string, unknown>
    : {};
}

function parseRules(value: unknown): PathRule[] {
  let parsed: unknown = value;
  if (typeof value === 'string') {
    try { parsed = JSON.parse(value); } catch { return []; }
  }
  const root = record(parsed);
  const source = Array.isArray(root.paths) ? root.paths : Array.isArray(parsed) ? parsed : [];
  return source.flatMap((entry) => {
    const rule = record(entry);
    if (typeof rule.path !== 'string') return [];
    const allowed = Array.isArray(rule.capabilities)
      ? rule.capabilities.filter((item): item is Capability => capabilities.some((candidate) => candidate.value === item))
      : [];
    return [{ path: rule.path, capabilities: allowed }];
  });
}

function normalizePolicies(response: unknown): PolicyRecord[] {
  const envelope = record(response);
  const source = Array.isArray(response)
    ? response
    : Array.isArray(envelope.items)
      ? envelope.items
      : Array.isArray(envelope.policies)
        ? envelope.policies
        : [];
  return source.map((entry, index) => {
    const policy = record(entry);
    return {
      id: typeof policy.id === 'string' ? policy.id : String(index),
      name: typeof policy.name === 'string' ? policy.name : '이름 없는 정책',
      description: typeof policy.description === 'string' ? policy.description : '',
      rules: parseRules(policy.rules),
      version: typeof policy.version === 'number' ? policy.version : 1,
      updated_at: typeof policy.updated_at === 'string' ? policy.updated_at : undefined,
      created_at: typeof policy.created_at === 'string' ? policy.created_at : undefined,
    };
  });
}

function normalizeUsers(response: unknown): UserOption[] {
  const envelope = record(response);
  const source = Array.isArray(response)
    ? response
    : Array.isArray(envelope.items)
      ? envelope.items
      : Array.isArray(envelope.users)
        ? envelope.users
        : [];
  return source.flatMap((entry) => {
    const user = record(entry);
    if (typeof user.id !== 'string' || typeof user.username !== 'string') return [];
    return [{
      id: user.id,
      username: user.username,
      display_name: typeof user.display_name === 'string' ? user.display_name : undefined,
      role: typeof user.role === 'string' ? user.role : undefined,
      active: typeof user.active === 'boolean' ? user.active : undefined,
    }];
  });
}

function normalizeAssignedUserIDs(response: unknown): string[] {
  const envelope = record(response);
  const source = Array.isArray(response)
    ? response
    : Array.isArray(envelope.user_ids)
      ? envelope.user_ids
      : Array.isArray(envelope.users)
        ? envelope.users
        : [];
  return source.flatMap((entry) => {
    if (typeof entry === 'string') return [entry];
    const user = record(entry);
    return typeof user.id === 'string' ? [user.id] : [];
  });
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : '요청을 처리하지 못했습니다.';
}

function matchesPath(pattern: string, requestedPath: string): boolean {
  const normalizedPattern = pattern.trim().replace(/^\/+|\/+$/g, '');
  const normalizedPath = requestedPath.trim().replace(/^\/+|\/+$/g, '');
  if (normalizedPattern === '*') return true;
  if (normalizedPattern.endsWith('*')) return normalizedPath.startsWith(normalizedPattern.slice(0, -1));
  return normalizedPattern === normalizedPath;
}

function PolicyLoading() {
  return (
    <Stack gap="lg" aria-label="정책을 불러오는 중">
      <Skeleton height={42} width="min(460px, 100%)" />
      <SimpleGrid cols={{ base: 1, lg: 2 }}><Skeleton height={320} /><Skeleton height={320} /></SimpleGrid>
    </Stack>
  );
}

export function PoliciesPage() {
  const { user, loading: authLoading } = useAuth();
  const canManage = user?.role === 'admin' || user?.role === 'manager';
  const queryClient = useQueryClient();
  const [editorOpen, setEditorOpen] = useState(false);
  const [editingPolicy, setEditingPolicy] = useState<PolicyRecord | null>(null);
  const [draft, setDraft] = useState<PolicyDraft>(emptyDraft);
  const [validationError, setValidationError] = useState<string | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<PolicyRecord | null>(null);
  const [simulationPolicyId, setSimulationPolicyId] = useState<string | null>(null);
  const [simulationPath, setSimulationPath] = useState('production/payment/database');
  const [simulationCapability, setSimulationCapability] = useState<Capability>('read');
  const [simulationRun, setSimulationRun] = useState(false);
  const [assignmentTarget, setAssignmentTarget] = useState<PolicyRecord | null>(null);
  const [assignmentDraft, setAssignmentDraft] = useState<string[]>([]);
  const [assignmentLoading, setAssignmentLoading] = useState(false);
  const [assignmentError, setAssignmentError] = useState<string | null>(null);

  const policyQuery = useQuery({
    queryKey: ['policies'],
    queryFn: () => get<unknown>('/policies'),
    enabled: !authLoading && Boolean(user),
  });
  const policies = useMemo(() => normalizePolicies(policyQuery.data), [policyQuery.data]);
  const usersQuery = useQuery({
    queryKey: ['policy-user-options'],
    queryFn: () => get<unknown>('/users?limit=500'),
    enabled: !authLoading && canManage,
  });
  const users = useMemo(() => normalizeUsers(usersQuery.data).filter((item) => item.active !== false), [usersQuery.data]);

  const saveMutation = useMutation({
    mutationFn: () => {
      const payload = { name: draft.name.trim(), description: draft.description.trim(), rules: { paths: draft.rules } };
      return editingPolicy
        ? patch<unknown>(`/policies/${encodeURIComponent(editingPolicy.id)}`, payload)
        : post<unknown>('/policies', payload);
    },
    onSuccess: async () => {
      setEditorOpen(false);
      await queryClient.invalidateQueries({ queryKey: ['policies'] });
      notifications.show({ color: 'teal', title: '정책 저장 완료', message: '경로와 capability 정책을 적용했습니다.' });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (policy: PolicyRecord) => del<unknown>(`/policies/${encodeURIComponent(policy.id)}`),
    onSuccess: async () => {
      setDeleteTarget(null);
      await queryClient.invalidateQueries({ queryKey: ['policies'] });
      notifications.show({ color: 'teal', title: '정책 삭제 완료', message: '선택한 정책을 삭제했습니다.' });
    },
  });

  const openCreate = () => {
    if (!canManage) return;
    saveMutation.reset();
    setEditingPolicy(null);
    setDraft(emptyDraft());
    setValidationError(null);
    setEditorOpen(true);
  };

  const openEdit = (policy: PolicyRecord) => {
    if (!canManage) return;
    saveMutation.reset();
    setEditingPolicy(policy);
    setDraft({
      name: policy.name,
      description: policy.description,
      rules: policy.rules.length ? policy.rules.map((rule) => ({ ...rule, capabilities: [...rule.capabilities] })) : emptyDraft().rules,
    });
    setValidationError(null);
    setEditorOpen(true);
  };

  const submitPolicy = () => {
    setValidationError(null);
    if (!draft.name.trim()) { setValidationError('정책 이름을 입력하세요.'); return; }
    if (!draft.rules.length || draft.rules.some((rule) => !rule.path.trim() || !rule.capabilities.length)) {
      setValidationError('모든 규칙에 경로와 하나 이상의 capability를 지정하세요.');
      return;
    }
    saveMutation.mutate();
  };

  const openAssignment = async (policy: PolicyRecord) => {
    if (!canManage) return;
    setAssignmentTarget(policy);
    setAssignmentDraft([]);
    setAssignmentError(null);
    setAssignmentLoading(true);
    try {
      const assigned = await get<unknown>(`/policies/${encodeURIComponent(policy.id)}/users`);
      setAssignmentDraft(normalizeAssignedUserIDs(assigned));
    } catch (error) {
      setAssignmentError(errorMessage(error));
    } finally {
      setAssignmentLoading(false);
    }
  };

  const saveAssignment = async () => {
    if (!assignmentTarget) return;
    setAssignmentError(null);
    setAssignmentLoading(true);
    try {
      await put(`/policies/${encodeURIComponent(assignmentTarget.id)}/users`, { user_ids: assignmentDraft });
      notifications.show({ color: 'teal', title: '사용자 할당 완료', message: '선택한 사용자에게 정책을 적용했습니다.' });
      setAssignmentTarget(null);
    } catch (error) {
      setAssignmentError(errorMessage(error));
    } finally {
      setAssignmentLoading(false);
    }
  };

  const simulatedPolicy = policies.find((policy) => policy.id === simulationPolicyId);
  const matchedRule = simulationRun && simulatedPolicy
    ? simulatedPolicy.rules.find((rule) => matchesPath(rule.path, simulationPath) && rule.capabilities.includes(simulationCapability))
    : undefined;

  if (authLoading || policyQuery.isPending) return <PolicyLoading />;
  if (!user) return <Alert color="red" title="로그인이 필요합니다" role="alert">접근 정책을 조회하려면 다시 로그인하세요.</Alert>;

  return (
    <Stack gap="lg">
      <Group justify="space-between" align="flex-end">
        <Box>
          <Title order={1} className="page-title">접근 정책</Title>
          <Text c="dimmed" mt={6}>Secret 경로별 최소 권한을 시각적으로 구성하고 결과를 미리 확인합니다.</Text>
        </Box>
        {canManage && <Button leftSection={<Plus size={17} />} onClick={openCreate}>정책 만들기</Button>}
      </Group>

      {policyQuery.isError && (
        <Paper className="surface" p="xl" radius="lg">
          <Stack align="flex-start">
            <Alert icon={<CircleAlert size={18} />} color="red" title="정책을 불러오지 못했습니다" w="100%" role="alert">{errorMessage(policyQuery.error)}</Alert>
            <Button variant="light" leftSection={<RefreshCw size={17} />} onClick={() => policyQuery.refetch()}>다시 시도</Button>
          </Stack>
        </Paper>
      )}

      {policyQuery.isSuccess && policies.length === 0 && (
        <Paper className="surface" p={{ base: 'xl', sm: 48 }} radius="lg">
          <Stack align="center" ta="center">
            <ThemeIcon size={58} radius="xl" variant="light" color="gray"><FileKey2 size={29} /></ThemeIcon>
            <Title order={2} size="h3">등록된 정책이 없습니다</Title>
            <Text c="dimmed">경로와 capability를 지정해 첫 접근 정책을 만드세요.</Text>
            {canManage && <Button leftSection={<Plus size={17} />} onClick={openCreate}>첫 정책 만들기</Button>}
          </Stack>
        </Paper>
      )}

      {policyQuery.isSuccess && policies.length > 0 && (
        <SimpleGrid cols={{ base: 1, xl: 2 }} spacing="lg">
          {policies.map((policy) => (
            <Paper key={policy.id} className="surface" p="lg" radius="lg">
              <Stack gap="md">
                <Group justify="space-between" align="flex-start" wrap="nowrap">
                  <Group gap="sm" wrap="nowrap" style={{ minWidth: 0 }}>
                    <ThemeIcon variant="light"><FileKey2 size={18} /></ThemeIcon>
                    <Box style={{ minWidth: 0 }}>
                      <Text fw={750} size="lg" style={{ overflowWrap: 'anywhere' }}>{policy.name}</Text>
                      <Text size="sm" c="dimmed">버전 {policy.version} · {formatDate(policy.updated_at || policy.created_at)}</Text>
                    </Box>
                  </Group>
                  {canManage && (
                    <Group gap={4} wrap="nowrap">
                      <Tooltip label="사용자 할당"><ActionIcon variant="subtle" aria-label={`${policy.name} 사용자 할당`} onClick={() => void openAssignment(policy)}><Users size={17} /></ActionIcon></Tooltip>
                      <Tooltip label="정책 편집"><ActionIcon variant="subtle" aria-label={`${policy.name} 정책 편집`} onClick={() => openEdit(policy)}><Pencil size={17} /></ActionIcon></Tooltip>
                      <Tooltip label="정책 삭제"><ActionIcon color="red" variant="subtle" aria-label={`${policy.name} 정책 삭제`} onClick={() => setDeleteTarget(policy)}><Trash2 size={17} /></ActionIcon></Tooltip>
                    </Group>
                  )}
                </Group>
                <Text c={policy.description ? undefined : 'dimmed'}>{policy.description || '설명이 없습니다.'}</Text>
                <Divider />
                {policy.rules.length ? policy.rules.map((rule, index) => (
                  <Box key={`${rule.path}-${index}`}>
                    <Text ff="monospace" fw={700} style={{ overflowWrap: 'anywhere' }}>{rule.path}</Text>
                    <Group gap={6} mt={8}>
                      {rule.capabilities.map((capability) => (
                        <Badge key={capability} variant="light">{capabilities.find((item) => item.value === capability)?.label || capability}</Badge>
                      ))}
                    </Group>
                  </Box>
                )) : <Text c="dimmed">경로 규칙이 없습니다.</Text>}
              </Stack>
            </Paper>
          ))}
        </SimpleGrid>
      )}

      <Paper className="surface" p={{ base: 'md', sm: 'xl' }} radius="lg">
        <Stack gap="lg">
          <Group align="flex-start" wrap="nowrap">
            <ThemeIcon color="violet" variant="light" size="lg"><FlaskConical size={20} /></ThemeIcon>
            <Box>
              <Title order={2} size="h3">간단 정책 시뮬레이터</Title>
              <Text c="dimmed" mt={3}>저장된 단일 정책의 path/capability 일치 여부를 브라우저에서 확인합니다. 사용자 최종 권한 판정은 서버가 수행합니다.</Text>
            </Box>
          </Group>
          <SimpleGrid cols={{ base: 1, md: 3 }} spacing="lg">
            <Select
              label="정책"
              placeholder="정책 선택"
              searchable
              data={policies.map((policy) => ({ value: policy.id, label: policy.name }))}
              value={simulationPolicyId}
              onChange={(value) => { setSimulationPolicyId(value); setSimulationRun(false); }}
            />
            <TextInput
              label="요청 경로"
              placeholder="production/payment/database"
              value={simulationPath}
              onChange={(event) => { setSimulationPath(event.currentTarget.value); setSimulationRun(false); }}
            />
            <Select
              label="요청 capability"
              data={capabilities}
              value={simulationCapability}
              onChange={(value) => { setSimulationCapability((value || 'read') as Capability); setSimulationRun(false); }}
            />
          </SimpleGrid>
          <Group>
            <Button variant="light" leftSection={<FlaskConical size={17} />} disabled={!simulationPolicyId || !simulationPath.trim()} onClick={() => setSimulationRun(true)}>
              결과 확인
            </Button>
          </Group>
          {simulationRun && (
            <Alert
              role="status"
              color={matchedRule ? 'teal' : 'red'}
              icon={matchedRule ? <CheckCircle2 size={18} /> : <ShieldX size={18} />}
              title={matchedRule ? '정책상 허용' : '정책상 거부'}
            >
              {matchedRule
                ? `${matchedRule.path} 규칙이 ${simulationCapability} 요청을 허용합니다.`
                : '선택한 정책에서 요청 경로와 capability에 일치하는 허용 규칙을 찾지 못했습니다.'}
            </Alert>
          )}
        </Stack>
      </Paper>

      <Modal opened={Boolean(assignmentTarget)} onClose={() => !assignmentLoading && setAssignmentTarget(null)} title="정책 사용자 할당" size="lg" centered>
        <Stack gap="lg">
          <Box>
            <Text fw={750}>{assignmentTarget?.name}</Text>
            <Text size="sm" c="dimmed" mt={3}>선택한 사용자 목록으로 이 정책의 현재 할당을 교체합니다. 관리자와 매니저는 역할 자체로 전체 Secret 권한을 가지므로 일반 사용자 중심으로 할당하세요.</Text>
          </Box>
          {assignmentError && <Alert icon={<CircleAlert size={18} />} color="red" title="사용자 할당을 처리할 수 없습니다" role="alert">{assignmentError}</Alert>}
          <MultiSelect
            label="정책을 적용할 사용자"
            description="비활성 사용자는 목록에서 제외됩니다."
            searchable
            clearable
            disabled={assignmentLoading || usersQuery.isError}
            data={users.map((item) => ({ value: item.id, label: `${item.display_name || item.username} (${item.username}${item.role ? ` · ${item.role}` : ''})` }))}
            value={assignmentDraft}
            onChange={setAssignmentDraft}
            placeholder={usersQuery.isLoading ? '사용자 목록을 불러오는 중' : '사용자 선택'}
          />
          {usersQuery.isError && <Alert color="red">{errorMessage(usersQuery.error)}</Alert>}
          <Group justify="flex-end">
            <Button variant="default" onClick={() => setAssignmentTarget(null)} disabled={assignmentLoading}>취소</Button>
            <Button leftSection={<Users size={17} />} loading={assignmentLoading} disabled={usersQuery.isError || Boolean(assignmentError && assignmentDraft.length === 0)} onClick={() => void saveAssignment()}>할당 저장</Button>
          </Group>
        </Stack>
      </Modal>

      <Modal opened={editorOpen} onClose={() => !saveMutation.isPending && setEditorOpen(false)} title={editingPolicy ? '정책 편집' : '정책 만들기'} size="lg" centered>
        <Stack gap="lg">
          {(validationError || saveMutation.isError) && (
            <Alert icon={<CircleAlert size={18} />} color="red" title="정책을 저장할 수 없습니다" role="alert">{validationError || errorMessage(saveMutation.error)}</Alert>
          )}
          <TextInput label="정책 이름" required value={draft.name} onChange={(event) => setDraft((current) => ({ ...current, name: event.currentTarget.value }))} />
          <Textarea label="설명" autosize minRows={2} maxRows={5} value={draft.description} onChange={(event) => setDraft((current) => ({ ...current, description: event.currentTarget.value }))} />
          <Divider label="경로 규칙" labelPosition="left" />
          {draft.rules.map((rule, index) => (
            <Paper key={index} withBorder p="md" radius="md">
              <Stack gap="md">
                <Group align="flex-end" wrap="nowrap">
                  <TextInput
                    label={`Secret 경로 ${index + 1}`}
                    description="끝의 *는 하위 경로 prefix와 일치합니다."
                    placeholder="production/payment/*"
                    required
                    style={{ flex: 1 }}
                    value={rule.path}
                    onChange={(event) => setDraft((current) => ({
                      ...current,
                      rules: current.rules.map((item, itemIndex) => itemIndex === index ? { ...item, path: event.currentTarget.value } : item),
                    }))}
                  />
                  <ActionIcon
                    color="red"
                    variant="subtle"
                    size="lg"
                    aria-label={`${index + 1}번 경로 규칙 삭제`}
                    disabled={draft.rules.length === 1}
                    onClick={() => setDraft((current) => ({ ...current, rules: current.rules.filter((_, itemIndex) => itemIndex !== index) }))}
                  >
                    <Trash2 size={18} />
                  </ActionIcon>
                </Group>
                <Checkbox.Group
                  label="허용 capability"
                  value={rule.capabilities}
                  onChange={(values) => setDraft((current) => ({
                    ...current,
                    rules: current.rules.map((item, itemIndex) => itemIndex === index ? { ...item, capabilities: values as Capability[] } : item),
                  }))}
                >
                  <Group mt="xs">
                    {capabilities.map((capability) => <Checkbox key={capability.value} value={capability.value} label={capability.label} />)}
                  </Group>
                </Checkbox.Group>
              </Stack>
            </Paper>
          ))}
          <Button type="button" variant="default" leftSection={<Plus size={16} />} onClick={() => setDraft((current) => ({ ...current, rules: [...current.rules, { path: '', capabilities: ['read'] }] }))}>
            경로 규칙 추가
          </Button>
          <Group justify="flex-end">
            <Button variant="default" onClick={() => setEditorOpen(false)} disabled={saveMutation.isPending}>취소</Button>
            <Button leftSection={<Save size={17} />} loading={saveMutation.isPending} onClick={submitPolicy}>정책 저장</Button>
          </Group>
        </Stack>
      </Modal>

      <Modal opened={Boolean(deleteTarget)} onClose={() => !deleteMutation.isPending && setDeleteTarget(null)} title="정책 삭제 확인" centered>
        <Stack>
          <Alert icon={<CircleAlert size={18} />} color="red" title="연결된 사용자의 권한이 즉시 달라질 수 있습니다">
            <Text><Text component="span" fw={750}>{deleteTarget?.name}</Text> 정책을 삭제하시겠습니까?</Text>
          </Alert>
          {deleteMutation.isError && <Alert color="red" role="alert">{errorMessage(deleteMutation.error)}</Alert>}
          <Group justify="flex-end">
            <Button variant="default" onClick={() => setDeleteTarget(null)} disabled={deleteMutation.isPending}>취소</Button>
            <Button color="red" leftSection={<Trash2 size={17} />} loading={deleteMutation.isPending} onClick={() => deleteTarget && deleteMutation.mutate(deleteTarget)}>삭제</Button>
          </Group>
        </Stack>
      </Modal>
    </Stack>
  );
}

export default PoliciesPage;
