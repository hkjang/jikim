import { useMemo, useState } from 'react';
import { Alert, Badge, Box, Button, Card, Group, Modal, SegmentedControl, Stack, Table, Text, Textarea, ThemeIcon, Timeline } from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';
import { useQuery } from '@tanstack/react-query';
import { notifications } from '@mantine/notifications';
import { Check, Clock3, Info, ShieldCheck, X } from 'lucide-react';
import { get, post } from '../lib/api';
import { formatDate } from '../lib/format';
import type { ApprovalRecord } from '../lib/types';
import { useAuth } from '../contexts/AuthContext';
import { EmptyState, PageError, PageLoading } from '../components/AsyncState';
import { PageHeader } from '../components/PageHeader';
import { StatusBadge } from '../components/StatusBadge';

interface PublicApprovalSettings {
  approval_enabled?: boolean;
  reviewer_role?: string;
  workflow?: {
    approval_enabled?: boolean;
    reviewer_role?: string;
  };
}

function normalize(body: any): ApprovalRecord[] {
  const source = Array.isArray(body) ? body : body?.items || body?.approvals || [];
  return source.map((item: any) => ({ ...item, type: item.type ?? item.action, requester: item.requester ?? item.requester_name ?? item.requester_id, reason: item.reason ?? item.comment }));
}

export function ApprovalsPage() {
  const { user } = useAuth();
  const [filter, setFilter] = useState('pending');
  const [selected, setSelected] = useState<ApprovalRecord | null>(null);
  const [comment, setComment] = useState('');
  const [working, setWorking] = useState(false);
  const [opened, modal] = useDisclosure(false);
  const publicSettings = useQuery({ queryKey: ['public-settings'], queryFn: () => get<PublicApprovalSettings>('/settings/public') });
  const approvalEnabled = publicSettings.data?.workflow?.approval_enabled ?? publicSettings.data?.approval_enabled ?? false;
  const reviewerRole = publicSettings.data?.workflow?.reviewer_role ?? publicSettings.data?.reviewer_role ?? 'admin';
  const canReview = user?.role === 'admin' || (reviewerRole === 'manager' && user?.role === 'manager');
  const listScope = canReview ? 'all' : 'mine';
  const query = useQuery({
    queryKey: ['approvals', listScope],
    queryFn: () => get<any>(canReview ? '/approvals?scope=all' : '/approvals'),
    enabled: approvalEnabled,
  });
  const items = useMemo(() => normalize(query.data).filter((item) => filter === 'all' || item.status === filter), [query.data, filter]);
  if (publicSettings.isLoading || (query.isLoading && approvalEnabled)) return <PageLoading />;
  if (publicSettings.isError) return <PageError error={publicSettings.error} />;
  if (!approvalEnabled) return <><PageHeader eyebrow="Approval workflow" title="검토·승인" description="관리자가 승인 절차를 켠 경우에만 요청·승인·반려 흐름이 적용됩니다." /><Alert color="blue" icon={<Info size={20} />} title="승인 프로세스가 사용 안 함으로 설정되어 있습니다.">현재 변경 작업은 권한 확인 후 즉시 반영되며 별도의 검토·승인·반려 단계가 생성되지 않습니다. 서비스 관리자는 설정 화면에서 이 기능을 켤 수 있습니다.</Alert></>;
  if (query.isError) return <PageError error={query.error} onRetry={() => void query.refetch()} />;
  const decide = async (decision: 'approve' | 'reject') => {
    if (!selected) return;
    setWorking(true);
    try { await post(`/approvals/${selected.id}/${decision}`, { comment }); notifications.show({ color: decision === 'approve' ? 'teal' : 'orange', message: decision === 'approve' ? '요청을 승인했습니다.' : '요청을 반려했습니다.' }); modal.close(); setSelected(null); setComment(''); void query.refetch(); }
    catch (error) { notifications.show({ color: 'red', message: error instanceof Error ? error.message : '결정을 반영하지 못했습니다.' }); }
    finally { setWorking(false); }
  };
  return <>
    <PageHeader eyebrow="Four-eyes workflow" title="검토·승인" description="중요 변경의 요청자와 승인자를 분리하고 모든 결정을 감사 기록으로 남깁니다." />
    <Group justify="space-between" mb="lg"><SegmentedControl value={filter} onChange={setFilter} data={[{ value: 'pending', label: '승인 대기' }, { value: 'approved', label: '승인됨' }, { value: 'rejected', label: '반려됨' }, { value: 'all', label: '전체' }]} /><Badge size="lg" variant="light" color="yellow">대기 {normalize(query.data).filter((item) => item.status === 'pending').length}</Badge></Group>
    <Card className="surface" radius="lg" p={0}>{items.length ? <Table.ScrollContainer minWidth={820}><Table highlightOnHover><Table.Thead><Table.Tr><Table.Th>요청</Table.Th><Table.Th>대상</Table.Th><Table.Th>요청자</Table.Th><Table.Th>사유</Table.Th><Table.Th>요청 시각</Table.Th><Table.Th>상태</Table.Th><Table.Th>검토</Table.Th></Table.Tr></Table.Thead><Table.Tbody>{items.map((item) => <Table.Tr key={item.id}><Table.Td><Text fw={700}>{item.type || '변경 요청'}</Text><Text size="xs" c="dimmed">#{item.id.slice(0, 8)}</Text></Table.Td><Table.Td ff="monospace" fz="sm">{item.resource || '—'}</Table.Td><Table.Td>{item.requester || '—'}</Table.Td><Table.Td><Text maw={260} lineClamp={2}>{item.reason || '사유 없음'}</Text></Table.Td><Table.Td>{formatDate(item.created_at)}</Table.Td><Table.Td><StatusBadge status={item.status} /></Table.Td><Table.Td>{item.status === 'pending' && canReview ? <Button size="xs" variant="light" onClick={() => { setSelected(item); modal.open(); }}>검토</Button> : '—'}</Table.Td></Table.Tr>)}</Table.Tbody></Table></Table.ScrollContainer> : <EmptyState title="조건에 맞는 요청이 없습니다." description="새 요청이 발생하면 검토 권한을 가진 사용자에게 표시됩니다." />}</Card>
    <Modal opened={opened} onClose={modal.close} title="요청 검토" size="lg"><Stack><Group><ThemeIcon size={44} variant="light" color="yellow"><ShieldCheck /></ThemeIcon><Box><Text fw={800}>{selected?.type || '변경 요청'}</Text><Text size="sm" ff="monospace" c="dimmed">{selected?.resource}</Text></Box></Group><Alert color="blue">요청자와 승인자가 같으면 승인할 수 없습니다. 승인 결과는 즉시 실행되고 되돌릴 수 없는 작업일 수 있습니다.</Alert><Timeline active={1} bulletSize={26}><Timeline.Item title="요청 접수" bullet={<Check size={14} />}><Text size="sm" c="dimmed">{selected?.requester} · {formatDate(selected?.created_at)}</Text></Timeline.Item><Timeline.Item title="검토 대기" bullet={<Clock3 size={14} />}><Text size="sm" c="dimmed">현재 단계</Text></Timeline.Item></Timeline><Textarea label="검토 의견" placeholder="승인 또는 반려 근거를 남겨 주세요." minRows={3} value={comment} onChange={(e) => setComment(e.currentTarget.value)} /><Group grow><Button color="red" variant="light" leftSection={<X size={18} />} loading={working} onClick={() => void decide('reject')}>반려</Button><Button leftSection={<Check size={18} />} loading={working} disabled={selected?.requester_id === user?.id} onClick={() => void decide('approve')}>승인</Button></Group></Stack></Modal>
  </>;
}
