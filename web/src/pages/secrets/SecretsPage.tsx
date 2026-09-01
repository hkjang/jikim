import { useMemo, useState } from 'react';
import { ActionIcon, Badge, Box, Button, Card, Group, Menu, Select, Table, Text, TextInput, Tooltip } from '@mantine/core';
import { useDebouncedValue } from '@mantine/hooks';
import { useQuery } from '@tanstack/react-query';
import { Ellipsis, Eye, Filter, KeyRound, Plus, RefreshCw, Search, Trash2 } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { notifications } from '@mantine/notifications';
import { del, get } from '../../lib/api';
import { formatDate, riskColor } from '../../lib/format';
import type { SecretRecord } from '../../lib/types';
import { EmptyState, PageError, PageLoading } from '../../components/AsyncState';
import { PageHeader } from '../../components/PageHeader';
import { StatusBadge } from '../../components/StatusBadge';
import { useAuth } from '../../contexts/AuthContext';

function normalizeList(body: any): SecretRecord[] {
  const source = Array.isArray(body) ? body : body?.items || body?.secrets || body?.data || [];
  return source.map((item: any) => ({
    ...item,
    version: item.version ?? item.current_version,
    application: item.application ?? item.application_name ?? item.application_id,
    owner: item.owner ?? item.owner_name ?? item.owner_user_id,
    status: item.status ?? 'active',
  }));
}

export function SecretsPage() {
  const { user } = useAuth();
  const isAuditor = user?.role === 'auditor';
  const navigate = useNavigate();
  const [search, setSearch] = useState('');
  const [debounced] = useDebouncedValue(search, 250);
  const [environment, setEnvironment] = useState<string | null>(null);
  const query = useQuery({ queryKey: ['secrets', debounced, environment], queryFn: () => get<any>(`/secrets?q=${encodeURIComponent(debounced)}${environment ? `&environment=${environment}` : ''}`) });
  const rows = useMemo(() => normalizeList(query.data), [query.data]);
  if (query.isLoading) return <PageLoading label="시크릿 목록을 안전하게 불러오고 있습니다." />;
  if (query.isError) return <PageError error={query.error} onRetry={() => void query.refetch()} />;
  const remove = async (id: string) => {
    if (!window.confirm('이 시크릿을 비활성화하고 격리하시겠습니까? 즉시 영구 삭제되지 않습니다.')) return;
    try {
      const result = await del<{ status?: string }>(`/secrets/${id}`);
      const pending = result?.status === 'pending';
      notifications.show({ color: pending ? 'yellow' : 'teal', message: pending ? '격리 승인 요청을 생성했습니다.' : '시크릿을 격리했습니다.' });
      void query.refetch();
      if (pending) navigate('/approvals');
    }
    catch (error) { notifications.show({ color: 'red', message: error instanceof Error ? error.message : '삭제 요청에 실패했습니다.' }); }
  };
  return (
    <>
      <PageHeader eyebrow="Secret lifecycle" title="시크릿 탐색기" description="애플리케이션과 환경 기준으로 시크릿을 찾고 버전, 책임자, 위험도를 함께 관리합니다." actions={!isAuditor ? <Button leftSection={<Plus size={18} />} onClick={() => navigate('/secrets/new')}>시크릿 만들기</Button> : undefined} />
      <Card className="surface" radius="lg" p={0}>
        <Group p="lg" justify="space-between" align="flex-end">
          <Group align="flex-end" gap="sm" flex={1}>
            <TextInput value={search} onChange={(event) => setSearch(event.currentTarget.value)} leftSection={<Search size={18} />} label="검색" placeholder="경로, 앱, Owner, 태그" w={{ base: '100%', sm: 360 }} />
            <Select value={environment} onChange={setEnvironment} clearable leftSection={<Filter size={17} />} label="환경" placeholder="전체 환경" data={[{ value: 'DEV', label: '개발 (DEV)' }, { value: 'STG', label: '스테이징 (STG)' }, { value: 'PRD', label: '운영 (PRD)' }]} w={180} />
          </Group>
          <Group><Text size="sm" c="dimmed">{rows.length}개</Text><Tooltip label="새로 고침"><ActionIcon size="lg" variant="default" onClick={() => void query.refetch()}><RefreshCw size={18} /></ActionIcon></Tooltip></Group>
        </Group>
        {rows.length ? <Table.ScrollContainer minWidth={940}><Table highlightOnHover><Table.Thead><Table.Tr><Table.Th>시크릿</Table.Th><Table.Th>앱·환경</Table.Th><Table.Th>Owner</Table.Th><Table.Th>버전</Table.Th><Table.Th>위험도</Table.Th><Table.Th>최근 변경</Table.Th><Table.Th>상태</Table.Th><Table.Th w={56}></Table.Th></Table.Tr></Table.Thead><Table.Tbody>
          {rows.map((secret) => {
            const canRead = secret.capabilities?.includes('read') === true;
            const canDelete = secret.capabilities?.includes('delete') === true;
            return <Table.Tr key={secret.id} onDoubleClick={canRead ? () => navigate(`/secrets/${secret.id}`) : undefined}><Table.Td><Group gap="sm" wrap="nowrap"><Box><ActionIcon variant="light" color="teal" size="lg"><KeyRound size={18} /></ActionIcon></Box><Box><Text fw={700} ff="monospace" size="sm">{secret.path}</Text><Group gap={5} mt={4}>{secret.tags?.slice(0, 3).map((tag) => <Badge key={tag} size="xs" variant="light" color="gray">{tag}</Badge>)}</Group></Box></Group></Table.Td><Table.Td><Text size="sm" fw={650}>{secret.application || '미연결'}</Text><Badge mt={3} size="xs" color={secret.environment === 'PRD' ? 'red' : secret.environment === 'STG' ? 'yellow' : 'blue'} variant="light">{secret.environment || '—'}</Badge></Table.Td><Table.Td>{secret.owner || '미지정'}</Table.Td><Table.Td>v{secret.version || 1}</Table.Td><Table.Td><Badge color={riskColor(secret.risk_score)} variant="light">{secret.risk_score ?? 0}</Badge></Table.Td><Table.Td><Text size="sm">{formatDate(secret.updated_at)}</Text></Table.Td><Table.Td><StatusBadge status={secret.status || 'active'} /></Table.Td><Table.Td>{canRead || canDelete ? <Menu position="bottom-end"><Menu.Target><ActionIcon variant="subtle" color="gray" aria-label="작업 메뉴"><Ellipsis size={19} /></ActionIcon></Menu.Target><Menu.Dropdown>{canRead && <Menu.Item leftSection={<Eye size={17} />} onClick={() => navigate(`/secrets/${secret.id}`)}>상세 보기</Menu.Item>}{canRead && canDelete && <Menu.Divider />}{canDelete && <Menu.Item color="red" leftSection={<Trash2 size={17} />} onClick={() => void remove(secret.id)}>격리</Menu.Item>}</Menu.Dropdown></Menu> : '—'}</Table.Td></Table.Tr>;
          })}
        </Table.Tbody></Table></Table.ScrollContainer> : <EmptyState title="조회 가능한 시크릿이 없습니다." description={isAuditor ? '감사자는 Secret 메타데이터 목록만 확인할 수 있습니다.' : '첫 시크릿을 만들면 값은 암호화된 상태로 PostgreSQL에 저장됩니다.'} action={!isAuditor ? <Button leftSection={<Plus size={18} />} onClick={() => navigate('/secrets/new')}>첫 시크릿 만들기</Button> : undefined} />}
      </Card>
    </>
  );
}
