import { useMemo, useState } from 'react';
import { ActionIcon, Badge, Button, Card, Group, Select, Table, Text, TextInput, Tooltip } from '@mantine/core';
import { useDebouncedValue } from '@mantine/hooks';
import { useQuery } from '@tanstack/react-query';
import { Download, RefreshCw, Search } from 'lucide-react';
import { get } from '../lib/api';
import { formatDate } from '../lib/format';
import type { AuditRecord } from '../lib/types';
import { EmptyState, PageError, PageLoading } from '../components/AsyncState';
import { PageHeader } from '../components/PageHeader';
import { StatusBadge } from '../components/StatusBadge';

function normalize(body: any): AuditRecord[] {
  const source = Array.isArray(body) ? body : body?.items || body?.events || body?.audit || [];
  return source.map((item: any) => ({
    ...item,
    id: String(item.id),
    actor: item.actor ?? item.username,
    ip: item.ip ?? item.remote_ip,
    result: item.result ?? (item.success === true ? 'success' : item.success === false ? 'failed' : undefined),
  }));
}

export function AuditPage() {
  const [search, setSearch] = useState('');
  const [debounced] = useDebouncedValue(search, 250);
  const [result, setResult] = useState<string | null>(null);
  const query = useQuery({ queryKey: ['audit', debounced, result], queryFn: () => get<any>(`/audit?q=${encodeURIComponent(debounced)}${result ? `&result=${result}` : ''}`) });
  const events = useMemo(() => normalize(query.data), [query.data]);
  if (query.isLoading) return <PageLoading />;
  if (query.isError) return <PageError error={query.error} onRetry={() => void query.refetch()} />;
  const exportCsv = () => {
    const safe = (value: unknown) => `"${String(value ?? '').replaceAll('"', '""')}"`;
    const csv = ['시간,사용자,작업,대상,IP,결과', ...events.map((e) => [e.created_at, e.actor, e.action, e.resource, e.ip, e.result].map(safe).join(','))].join('\n');
    const link = document.createElement('a'); link.href = URL.createObjectURL(new Blob(['\uFEFF' + csv], { type: 'text/csv;charset=utf-8' })); link.download = `jikim-audit-${new Date().toISOString().slice(0, 10)}.csv`; link.click(); URL.revokeObjectURL(link.href);
  };
  return <>
    <PageHeader eyebrow="Tamper-aware audit" title="감사 로그" description="누가, 언제, 어떤 리소스에 어떤 작업을 했는지 추적합니다. 시크릿 평문은 기록하지 않습니다." actions={<Button variant="default" leftSection={<Download size={18} />} onClick={exportCsv}>CSV 내보내기</Button>} />
    <Card className="surface" radius="lg" p={0}><Group p="lg" justify="space-between" align="flex-end"><Group align="flex-end"><TextInput label="검색" value={search} onChange={(e) => setSearch(e.currentTarget.value)} leftSection={<Search size={17} />} placeholder="사용자, 작업, 리소스, IP" w={{ base: 260, sm: 380 }} /><Select label="결과" value={result} onChange={setResult} clearable placeholder="전체" data={[{ value: 'success', label: '성공' }, { value: 'failed', label: '실패' }, { value: 'denied', label: '거부' }]} w={145} /></Group><Group><Badge variant="light">{events.length}건</Badge><Tooltip label="새로 고침"><ActionIcon size="lg" variant="default" onClick={() => void query.refetch()}><RefreshCw size={18} /></ActionIcon></Tooltip></Group></Group>{events.length ? <Table.ScrollContainer minWidth={920}><Table striped highlightOnHover><Table.Thead><Table.Tr><Table.Th>시각</Table.Th><Table.Th>사용자</Table.Th><Table.Th>작업</Table.Th><Table.Th>리소스</Table.Th><Table.Th>출발 IP</Table.Th><Table.Th>결과</Table.Th><Table.Th>이벤트 ID</Table.Th></Table.Tr></Table.Thead><Table.Tbody>{events.map((event) => <Table.Tr key={event.id}><Table.Td><Text size="sm">{formatDate(event.created_at)}</Text></Table.Td><Table.Td fw={650}>{event.actor || 'system'}</Table.Td><Table.Td><Badge variant="light" color="blue">{event.action}</Badge></Table.Td><Table.Td><Text ff="monospace" size="sm" maw={330} truncate>{event.resource || '—'}</Text></Table.Td><Table.Td><Text size="sm" ff="monospace">{event.ip || '—'}</Text></Table.Td><Table.Td><StatusBadge status={event.result} /></Table.Td><Table.Td><Text size="xs" c="dimmed" ff="monospace">{event.id.slice(0, 12)}</Text></Table.Td></Table.Tr>)}</Table.Tbody></Table></Table.ScrollContainer> : <EmptyState title="감사 이벤트가 없습니다." description="로그인, 설정 변경, 시크릿 접근과 키 작업이 이곳에 기록됩니다." />}</Card>
  </>;
}
