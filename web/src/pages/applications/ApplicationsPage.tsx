import { useMemo, useState } from 'react';
import { Badge, Button, Card, Grid, Group, Modal, Select, Stack, Table, Text, TextInput, Textarea, ThemeIcon } from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';
import { useQuery } from '@tanstack/react-query';
import { notifications } from '@mantine/notifications';
import { AppWindow, GitBranch, Plus, Search, Server } from 'lucide-react';
import { get, post } from '../../lib/api';
import { formatDate } from '../../lib/format';
import type { ApplicationRecord } from '../../lib/types';
import { EmptyState, PageError, PageLoading } from '../../components/AsyncState';
import { PageHeader } from '../../components/PageHeader';
import { StatusBadge } from '../../components/StatusBadge';
import { useAuth } from '../../contexts/AuthContext';

function normalize(body: any): ApplicationRecord[] { return Array.isArray(body) ? body : body?.items || body?.applications || []; }

export function ApplicationsPage() {
  const { user } = useAuth();
  const canManage = user?.role === 'admin' || user?.role === 'manager';
  const [opened, modal] = useDisclosure(false);
  const [search, setSearch] = useState('');
  const [saving, setSaving] = useState(false);
  const [form, setForm] = useState({ name: '', owner: '', criticality: 'Normal', environment: 'DEV', repository: '', description: '' });
  const query = useQuery({ queryKey: ['applications'], queryFn: () => get<any>('/applications') });
  const apps = useMemo(() => normalize(query.data).filter((app) => app.name.toLowerCase().includes(search.toLowerCase()) || (app.owner || '').toLowerCase().includes(search.toLowerCase())), [query.data, search]);
  if (query.isLoading) return <PageLoading />;
  if (query.isError) return <PageError error={query.error} onRetry={() => void query.refetch()} />;
  const save = async () => {
    if (!form.name.trim()) return;
    setSaving(true);
    try { await post('/applications', form); notifications.show({ color: 'teal', message: '애플리케이션을 등록했습니다.' }); modal.close(); setForm({ name: '', owner: '', criticality: 'Normal', environment: 'DEV', repository: '', description: '' }); void query.refetch(); }
    catch (error) { notifications.show({ color: 'red', message: error instanceof Error ? error.message : '등록하지 못했습니다.' }); }
    finally { setSaving(false); }
  };
  return <>
    <PageHeader eyebrow="Application catalog" title="애플리케이션" description="시크릿을 경로만이 아니라 실제 사용하는 서비스와 Owner 기준으로 관리합니다." actions={canManage ? <Button leftSection={<Plus size={18} />} onClick={modal.open}>애플리케이션 등록</Button> : undefined} />
    <Grid gutter="lg" mb="lg"><Grid.Col span={{ base: 12, sm: 4 }}><Card className="surface" radius="lg"><Group justify="space-between"><div><Text c="dimmed" size="sm">전체 서비스</Text><Text fz={28} fw={850}>{apps.length}</Text></div><ThemeIcon size={45} variant="light"><AppWindow /></ThemeIcon></Group></Card></Grid.Col><Grid.Col span={{ base: 12, sm: 4 }}><Card className="surface" radius="lg"><Group justify="space-between"><div><Text c="dimmed" size="sm">운영 중요 서비스</Text><Text fz={28} fw={850}>{apps.filter((a) => a.criticality === 'Critical').length}</Text></div><ThemeIcon size={45} variant="light" color="red"><Server /></ThemeIcon></Group></Card></Grid.Col><Grid.Col span={{ base: 12, sm: 4 }}><Card className="surface" radius="lg"><Group justify="space-between"><div><Text c="dimmed" size="sm">연결 시크릿</Text><Text fz={28} fw={850}>{apps.reduce((sum, a) => sum + (a.secret_count || 0), 0)}</Text></div><ThemeIcon size={45} variant="light" color="violet"><GitBranch /></ThemeIcon></Group></Card></Grid.Col></Grid>
    <Card className="surface" radius="lg" p={0}><Group p="lg" justify="space-between"><TextInput value={search} onChange={(e) => setSearch(e.currentTarget.value)} leftSection={<Search size={17} />} placeholder="서비스 또는 Owner 검색" w={{ base: '100%', sm: 360 }} /><Text size="sm" c="dimmed">{apps.length}개 서비스</Text></Group>{apps.length ? <Table.ScrollContainer minWidth={760}><Table highlightOnHover><Table.Thead><Table.Tr><Table.Th>서비스</Table.Th><Table.Th>환경</Table.Th><Table.Th>중요도</Table.Th><Table.Th>Owner</Table.Th><Table.Th>시크릿</Table.Th><Table.Th>최근 변경</Table.Th><Table.Th>상태</Table.Th></Table.Tr></Table.Thead><Table.Tbody>{apps.map((app) => <Table.Tr key={app.id}><Table.Td><Text fw={750}>{app.name}</Text><Text size="xs" c="dimmed">{app.repository || '저장소 미연결'}</Text></Table.Td><Table.Td><Badge variant="light">{app.environment || '—'}</Badge></Table.Td><Table.Td><Badge color={app.criticality === 'Critical' ? 'red' : app.criticality === 'High' ? 'orange' : 'gray'} variant="light">{app.criticality || 'Normal'}</Badge></Table.Td><Table.Td>{app.owner || '미지정'}</Table.Td><Table.Td>{app.secret_count || 0}</Table.Td><Table.Td>{formatDate(app.updated_at)}</Table.Td><Table.Td><StatusBadge status={app.status || 'active'} /></Table.Td></Table.Tr>)}</Table.Tbody></Table></Table.ScrollContainer> : <EmptyState title="등록된 애플리케이션이 없습니다." description="서비스를 등록하고 시크릿과 Owner를 연결해 영향도를 추적하세요." action={canManage ? <Button onClick={modal.open}>첫 서비스 등록</Button> : undefined} />}</Card>
    <Modal opened={opened} onClose={modal.close} title="애플리케이션 등록" size="lg"><Stack><TextInput required label="서비스명" placeholder="payment-api" value={form.name} onChange={(e) => setForm({ ...form, name: e.currentTarget.value })} /><TextInput label="Owner" placeholder="결제개발팀" value={form.owner} onChange={(e) => setForm({ ...form, owner: e.currentTarget.value })} /><Group grow align="flex-start"><Select label="중요도" value={form.criticality} onChange={(v) => setForm({ ...form, criticality: v || 'Normal' })} data={['Normal', 'High', 'Critical']} /><Select label="기본 환경" value={form.environment} onChange={(v) => setForm({ ...form, environment: v || 'DEV' })} data={['DEV', 'STG', 'PRD']} /></Group><TextInput label="Git 저장소" placeholder="https://git.example.local/team/payment" value={form.repository} onChange={(e) => setForm({ ...form, repository: e.currentTarget.value })} /><Textarea label="설명" minRows={3} value={form.description} onChange={(e) => setForm({ ...form, description: e.currentTarget.value })} /><Button loading={saving} disabled={!form.name.trim()} onClick={() => void save()}>등록</Button></Stack></Modal>
  </>;
}
