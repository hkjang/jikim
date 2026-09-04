import { useMemo, useState } from 'react';
import { Badge, Button, Card, Checkbox, Group, Modal, Select, Stack, Table, Text, TextInput, ThemeIcon } from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';
import { useQuery } from '@tanstack/react-query';
import { notifications } from '@mantine/notifications';
import { KeyRound, Plus, RotateCw, ShieldCheck } from 'lucide-react';
import { get, patch, post } from '../lib/api';
import { formatDate } from '../lib/format';
import type { KeyRecord } from '../lib/types';
import { EmptyState, PageError, PageLoading } from '../components/AsyncState';
import { PageHeader } from '../components/PageHeader';
import { StatusBadge } from '../components/StatusBadge';
import { useAuth } from '../contexts/AuthContext';

function normalize(body: any): KeyRecord[] {
  const source = Array.isArray(body) ? body : body?.items || body?.keys || [];
  return source.map((item: any) => ({
    ...item,
    name: item.name ?? `개인 키 v${item.version ?? 1}`,
    owner: item.owner ?? item.username ?? item.user_id,
    type: item.type ?? 'personal',
    algorithm: item.algorithm ?? 'AES-256-GCM',
    status: item.status ?? (item.active ? 'active' : 'disabled'),
    rotated_at: item.rotated_at ?? item.created_at,
    permissions: Array.isArray(item.permissions)
      ? Object.fromEntries(item.permissions.map((permission: string) => [permission, true]))
      : item.permissions || {},
  }));
}

export function KeysPage() {
  const { user } = useAuth();
  const [opened, modal] = useDisclosure(false);
  const [permissionsOpened, permissionsModal] = useDisclosure(false);
  const [saving, setSaving] = useState(false);
  const [savingPermissions, setSavingPermissions] = useState(false);
  const [rotating, setRotating] = useState('');
  const [permissionTarget, setPermissionTarget] = useState<KeyRecord | null>(null);
  const [permissionDraft, setPermissionDraft] = useState<string[]>([]);
  const [form, setForm] = useState({ name: '', algorithm: 'AES-256-GCM', permissions: ['encrypt', 'decrypt', 'rotate', 'manage'] as string[] });
  const query = useQuery({ queryKey: ['keys'], queryFn: () => get<any>('/keys') });
  const keys = useMemo(() => normalize(query.data), [query.data]);

  // Read the event in the handler; React nulls currentTarget once the handler
  // returns, so a deferred setState updater can no longer touch it.
  const togglePermission = (permission: string, enabled: boolean) => setPermissionDraft((current) => (
    enabled ? [...current, permission] : current.filter((item) => item !== permission)
  ));
  if (query.isLoading) return <PageLoading />;
  if (query.isError) return <PageError error={query.error} onRetry={() => void query.refetch()} />;
  const create = async () => {
    setSaving(true);
    try { await post('/keys', form); notifications.show({ color: 'teal', message: '새 암호화 키를 생성했습니다.' }); modal.close(); void query.refetch(); }
    catch (error) { notifications.show({ color: 'red', message: error instanceof Error ? error.message : '키를 생성하지 못했습니다.' }); }
    finally { setSaving(false); }
  };
  const rotate = async (key: KeyRecord) => {
    if (!window.confirm(`${key.name} 키를 회전할까요? 기존 암호문은 이전 버전 키로 계속 복호화할 수 있습니다.`)) return;
    setRotating(key.id);
    try { await post(`/keys/${key.id}/rotate`); notifications.show({ color: 'teal', message: '키 버전을 안전하게 회전했습니다.' }); void query.refetch(); }
    catch (error) { notifications.show({ color: 'red', message: error instanceof Error ? error.message : '키 회전에 실패했습니다.' }); }
    finally { setRotating(''); }
  };
  const canManageKey = (key: KeyRecord) => {
    if (key.type === 'personal' && key.status !== 'active') return false;
    if (user?.role === 'admin') return true;
    if (user?.role !== 'manager') return false;
    if (key.type === 'personal') return key.owner_user_id === user?.id;
    return key.permissions?.manage === true;
  };
  const canRotateKey = (key: KeyRecord) => {
    if (key.type === 'personal' && key.status !== 'active') return false;
    if (user?.role === 'admin') return true;
    if (user?.role !== 'manager') return false;
    if (key.type === 'personal') return key.owner_user_id === user?.id && key.permissions?.rotate === true;
    return key.permissions?.rotate === true;
  };
  const renderKeyActions = (key: KeyRecord) => {
    const canRotate = canRotateKey(key);
    const canManage = canManageKey(key);
    if (!canRotate && !canManage) return '—';
    return <Group gap="xs" wrap="nowrap">{canRotate && <Button size="xs" variant="light" leftSection={<RotateCw size={15} />} loading={rotating === key.id} onClick={() => void rotate(key)}>회전</Button>}{canManage && <Button size="xs" variant="default" onClick={() => openPermissions(key)}>권한</Button>}</Group>;
  };
  const openPermissions = (key: KeyRecord) => {
    setPermissionTarget(key);
    const enabled = Object.entries(key.permissions || {}).filter(([, value]) => value).map(([name]) => name);
    setPermissionDraft(key.type === 'personal' ? Array.from(new Set([...enabled, 'decrypt'])) : enabled);
    permissionsModal.open();
  };
  const savePermissions = async () => {
    if (!permissionTarget) return;
    setSavingPermissions(true);
    try {
      await patch(`/keys/${encodeURIComponent(permissionTarget.id)}/permissions`, { permissions: permissionDraft });
      notifications.show({ color: 'teal', message: '키 권한을 변경했습니다.' });
      permissionsModal.close();
      setPermissionTarget(null);
      void query.refetch();
    } catch (error) {
      notifications.show({ color: 'red', message: error instanceof Error ? error.message : '키 권한을 변경하지 못했습니다.' });
    } finally { setSavingPermissions(false); }
  };
  return <>
    <PageHeader eyebrow="Crypto inventory" title="키 관리" description="서비스 키와 개인별 키의 버전, 알고리즘, 권한과 회전 상태를 중앙에서 관리합니다." actions={<Button leftSection={<Plus size={18} />} onClick={modal.open}>키 만들기</Button>} />
    <Group mb="lg"><Card className="surface" radius="lg" p="lg" miw={230}><Group justify="space-between"><div><Text c="dimmed" size="sm">활성 키</Text><Text fz={28} fw={850}>{keys.filter((key) => !key.status || key.status === 'active').length}</Text></div><ThemeIcon variant="light" size={44}><KeyRound /></ThemeIcon></Group></Card><Card className="surface" radius="lg" p="lg" miw={230}><Group justify="space-between"><div><Text c="dimmed" size="sm">회전 필요</Text><Text fz={28} fw={850}>{keys.filter((key) => key.next_rotation_at && new Date(key.next_rotation_at) <= new Date()).length}</Text></div><ThemeIcon variant="light" size={44} color="orange"><RotateCw /></ThemeIcon></Group></Card></Group>
    <Card className="surface" radius="lg" p={0}>{keys.length ? <Table.ScrollContainer minWidth={900}><Table highlightOnHover><Table.Thead><Table.Tr><Table.Th>키</Table.Th><Table.Th>유형·알고리즘</Table.Th><Table.Th>버전</Table.Th><Table.Th>Owner</Table.Th><Table.Th>최근 회전</Table.Th><Table.Th>다음 회전</Table.Th><Table.Th>상태</Table.Th><Table.Th>작업</Table.Th></Table.Tr></Table.Thead><Table.Tbody>{keys.map((key) => <Table.Tr key={key.id}><Table.Td><Group gap="sm"><ThemeIcon variant="light"><KeyRound size={18} /></ThemeIcon><Text fw={750}>{key.name}</Text></Group></Table.Td><Table.Td><Text size="sm">{key.type || 'service'}</Text><Text size="xs" c="dimmed">{key.algorithm || 'AES-256-GCM'}</Text></Table.Td><Table.Td><Badge variant="light">v{key.version || 1}</Badge></Table.Td><Table.Td>{key.owner || 'system'}</Table.Td><Table.Td>{formatDate(key.rotated_at)}</Table.Td><Table.Td>{formatDate(key.next_rotation_at)}</Table.Td><Table.Td><StatusBadge status={key.status || 'active'} /></Table.Td><Table.Td>{renderKeyActions(key)}</Table.Td></Table.Tr>)}</Table.Tbody></Table></Table.ScrollContainer> : <EmptyState title="등록된 키가 없습니다." description="첫 서비스 키를 만들거나 사용자의 개인 키 발급을 기다리세요." action={<Button onClick={modal.open}>키 만들기</Button>} />}</Card>
    <Modal opened={opened} onClose={modal.close} title="새 암호화 키" size="lg"><Stack><TextInput required label="키 이름" placeholder="customer-pii" value={form.name} onChange={(e) => setForm({ ...form, name: e.currentTarget.value })} /><Select label="알고리즘" value={form.algorithm} onChange={(value) => setForm({ ...form, algorithm: value || 'AES-256-GCM' })} data={['AES-256-GCM']} /><Text size="sm" c="dimmed">Owner는 키를 생성한 현재 관리자로 자동 기록됩니다.</Text><Text fw={700} size="sm">초기 권한</Text>{['encrypt', 'decrypt', 'rotate', 'manage'].map((permission) => <Checkbox key={permission} label={{ encrypt: '암호화', decrypt: '복호화', rotate: '키 회전', manage: '권한 관리' }[permission]} checked={form.permissions.includes(permission)} onChange={(e) => setForm({ ...form, permissions: e.currentTarget.checked ? [...form.permissions, permission] : form.permissions.filter((p) => p !== permission) })} />)}<Button leftSection={<ShieldCheck size={18} />} loading={saving} disabled={!form.name.trim()} onClick={() => void create()}>키 생성</Button></Stack></Modal>
    <Modal opened={permissionsOpened} onClose={permissionsModal.close} title="키 권한 변경" size="md"><Stack><Text fw={750}>{permissionTarget?.name}</Text><Text size="sm" c="dimmed">권한을 제거하면 해당 작업을 사용할 수 없습니다. 개인 키의 복호화 권한은 기존 Secret 복구를 위해 해제할 수 없으며, 서비스 관리자는 Transit 키 잠금을 복구할 수 있습니다.</Text>{(permissionTarget?.type === 'personal' ? ['encrypt', 'decrypt', 'rotate'] : ['encrypt', 'decrypt', 'rotate', 'manage']).map((permission) => <Checkbox key={permission} label={{ encrypt: '암호화', decrypt: '복호화', rotate: '키 회전', manage: '권한 관리' }[permission]} checked={permissionDraft.includes(permission)} disabled={permissionTarget?.type === 'personal' && permission === 'decrypt'} onChange={(event) => togglePermission(permission, event.currentTarget.checked)} />)}<Button loading={savingPermissions} onClick={() => void savePermissions()}>권한 저장</Button></Stack></Modal>
  </>;
}
