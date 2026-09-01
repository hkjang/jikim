import { ActionIcon, Alert, Badge, Box, Button, Card, Group, Progress, RingProgress, SimpleGrid, Stack, Table, Text, ThemeIcon, Title } from '@mantine/core';
import { useQuery } from '@tanstack/react-query';
import { Activity, AlertTriangle, AppWindow, ArrowRight, CheckCircle2, Clock3, KeyRound, RefreshCw, ShieldCheck, Users } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { get } from '../lib/api';
import { formatDate, formatNumber } from '../lib/format';
import type { AuditRecord, DashboardData } from '../lib/types';
import { PageError, PageLoading } from '../components/AsyncState';
import { PageHeader } from '../components/PageHeader';
import { StatusBadge } from '../components/StatusBadge';
import { useAuth } from '../contexts/AuthContext';

export function DashboardPage() {
  const { user } = useAuth();
  const canManage = user?.role === 'admin' || user?.role === 'manager';
  const canAudit = canManage || user?.role === 'auditor';
  const navigate = useNavigate();
  const query = useQuery({ queryKey: ['dashboard'], queryFn: () => get<DashboardData>('/dashboard') });
  if (query.isLoading) return <PageLoading label="보안 현황을 계산하고 있습니다." />;
  if (query.isError) return <PageError error={query.error} onRetry={() => void query.refetch()} />;
  const raw = (query.data || {}) as DashboardData & { high_risk_secrets?: number; policies?: number };
  const data: DashboardData = {
    ...raw,
    high_risk: raw.high_risk ?? raw.high_risk_secrets ?? 0,
    keys: raw.keys ?? 0,
    security_score: raw.security_score ?? Math.max(0, 100 - (raw.high_risk_secrets || 0) * 3),
    recent_audit: (raw.recent_audit || []).map((event) => ({
      ...event,
      id: String(event.id),
      actor: event.actor ?? (event as AuditRecord & { username?: string }).username,
      result: event.result ?? ((event as AuditRecord & { success?: boolean }).success === true
        ? 'success'
        : (event as AuditRecord & { success?: boolean }).success === false ? 'failed' : undefined),
    })),
  };
  const score = Math.min(100, Math.max(0, data.security_score ?? 0));
  const metrics = [
    { label: '관리 시크릿', value: data.secrets, detail: '암호화 저장된 전체 항목', icon: KeyRound, color: '#11b5ae', path: '/secrets' },
    { label: '애플리케이션', value: data.applications, detail: '시크릿 연결 서비스', icon: AppWindow, color: '#4d7cfe', path: '/applications' },
    ...(canAudit ? [{ label: '사용자·Identity', value: data.users, detail: '사람과 워크로드 계정', icon: Users, color: '#815ac0', path: '/access/identities' }] : []),
    ...(canManage ? [{ label: '암호화 키', value: data.keys, detail: '활성 키와 개인 키', icon: ShieldCheck, color: '#e39a27', path: '/encryption/keys' }] : []),
  ];
  return (
    <>
      <PageHeader eyebrow="Security overview" title="보안 현황" description="시크릿 수명주기, 키 상태와 접근 이벤트를 한눈에 확인하세요." actions={<><ActionIcon variant="default" size="lg" aria-label="새로 고침" onClick={() => void query.refetch()}><RefreshCw size={18} /></ActionIcon>{user?.role !== 'auditor' && <Button rightSection={<ArrowRight size={18} />} onClick={() => navigate('/secrets/new')}>시크릿 만들기</Button>}</>} />
      {(data.high_risk || data.rotation_failed) ? <Alert mb="lg" color="orange" icon={<AlertTriangle size={20} />} title="확인이 필요한 보안 항목이 있습니다."><Group gap="xl"><Text>고위험 시크릿 <b>{formatNumber(data.high_risk)}</b>개</Text><Text>회전 실패 <b>{formatNumber(data.rotation_failed)}</b>건</Text><Button size="xs" variant="light" color="orange" onClick={() => navigate('/secrets/risk')}>위험 검토</Button></Group></Alert> : null}
      <SimpleGrid cols={{ base: 1, xs: 2, xl: 4 }} spacing="lg" mb="lg">
        {metrics.map((item) => { const Icon = item.icon; return (
          <Card key={item.label} className="surface metric-card" radius="lg" p="lg" style={{ '--accent': item.color } as React.CSSProperties} onClick={() => navigate(item.path)} role="button" tabIndex={0}>
            <Group justify="space-between" align="flex-start"><ThemeIcon size={44} radius="md" variant="light" color="teal"><Icon size={22} /></ThemeIcon><Text fz={29} fw={800} c="navy.9">{formatNumber(item.value)}</Text></Group>
            <Text mt="lg" fw={750}>{item.label}</Text><Text size="sm" c="dimmed">{item.detail}</Text>
          </Card>
        ); })}
      </SimpleGrid>
      <SimpleGrid cols={{ base: 1, lg: 3 }} spacing="lg" mb="lg">
        <Card className="surface" radius="lg" p="xl">
          <Group justify="space-between" mb="lg"><Box><Text fw={800} size="lg">보안 점수</Text><Text size="sm" c="dimmed">현재 구성과 위험 지표</Text></Box><Badge color={score >= 80 ? 'teal' : score >= 60 ? 'yellow' : 'red'}>{score >= 80 ? '양호' : score >= 60 ? '개선 필요' : '위험'}</Badge></Group>
          <Group justify="center"><RingProgress size={190} thickness={17} roundCaps sections={[{ value: score, color: score >= 80 ? 'teal' : score >= 60 ? 'yellow' : 'red' }]} label={<Stack align="center" gap={0}><Text fz={37} fw={850}>{score}</Text><Text size="xs" c="dimmed">/ 100</Text></Stack>} /></Group>
          <Button variant="light" fullWidth mt="md" onClick={() => navigate('/secrets/risk')}>개선 항목 보기</Button>
        </Card>
        <Card className="surface span-two" radius="lg" p="xl">
          <Group justify="space-between" mb="xl"><Box><Text fw={800} size="lg">시크릿 건강도</Text><Text size="sm" c="dimmed">회전·만료·위험 상태를 종합한 분포</Text></Box><Activity size={22} color="#11b5ae" /></Group>
          <Stack gap="lg">
            {[
              { label: '정상', value: Math.max(0, 100 - ((data.high_risk || 0) + (data.expiring || 0) + (data.rotation_failed || 0))), color: 'teal', detail: '정책에 맞게 관리 중' },
              { label: '회전 필요', value: data.high_risk || 0, color: 'orange', detail: '오래되었거나 위험 점수 높음' },
              { label: '만료 예정', value: data.expiring || 0, color: 'yellow', detail: '30일 이내 만료' },
              { label: '회전 실패', value: data.rotation_failed || 0, color: 'red', detail: '자동 회전 복구 필요' },
            ].map((row) => {
              const total = Math.max(1, (data.secrets || 0)); const percent = Math.min(100, Math.round(row.value / total * 100));
              return <Box key={row.label}><Group justify="space-between" mb={7}><Box><Text fw={700}>{row.label}</Text><Text size="xs" c="dimmed">{row.detail}</Text></Box><Group gap="xs"><Text fw={800}>{formatNumber(row.value)}</Text><Text size="sm" c="dimmed">({percent}%)</Text></Group></Group><Progress value={percent} color={row.color} size="lg" radius="xl" /></Box>;
            })}
          </Stack>
        </Card>
      </SimpleGrid>
      <SimpleGrid cols={{ base: 1, lg: 3 }} spacing="lg">
        <Card className="surface span-two" radius="lg" p={0}>
          <Group justify="space-between" p="lg" pb="sm"><Box><Title order={2} fz="lg">최근 감사 활동</Title><Text size="sm" c="dimmed">{canAudit ? '민감 값은 감사 로그에 기록하지 않습니다.' : '감사 활동은 권한이 있는 운영자에게만 표시됩니다.'}</Text></Box>{canAudit && <Button variant="subtle" rightSection={<ArrowRight size={16} />} onClick={() => navigate('/audit')}>전체 보기</Button>}</Group>
          <Table.ScrollContainer minWidth={680}><Table highlightOnHover><Table.Thead><Table.Tr><Table.Th>시간</Table.Th><Table.Th>주체</Table.Th><Table.Th>작업</Table.Th><Table.Th>대상</Table.Th><Table.Th>결과</Table.Th></Table.Tr></Table.Thead><Table.Tbody>
            {(data.recent_audit || []).slice(0, 6).map((event) => <Table.Tr key={event.id}><Table.Td><Text size="sm">{formatDate(event.created_at)}</Text></Table.Td><Table.Td>{event.actor || 'system'}</Table.Td><Table.Td><Text fw={650}>{event.action}</Text></Table.Td><Table.Td><Text size="sm" ff="monospace">{event.resource || '—'}</Text></Table.Td><Table.Td><StatusBadge status={event.result} /></Table.Td></Table.Tr>)}
            {!data.recent_audit?.length && <Table.Tr><Table.Td colSpan={5}><Text c="dimmed" ta="center" py="xl">아직 감사 이벤트가 없습니다.</Text></Table.Td></Table.Tr>}
          </Table.Tbody></Table></Table.ScrollContainer>
        </Card>
        <Stack gap="lg">
          <Card className="surface" radius="lg" p="lg"><Group justify="space-between"><ThemeIcon color="yellow" variant="light" size={42}><Clock3 size={21} /></ThemeIcon><Text fz={28} fw={850}>{formatNumber(data.pending_approvals)}</Text></Group><Text fw={750} mt="md">승인 대기</Text><Text size="sm" c="dimmed">설정된 검토가 필요한 요청</Text>{(data.pending_approvals || 0) > 0 && <Button fullWidth variant="light" mt="md" onClick={() => navigate('/approvals')}>검토하기</Button>}</Card>
          <Card className="surface" radius="lg" p="lg"><Group gap="sm"><ThemeIcon color="teal" variant="light"><CheckCircle2 size={19} /></ThemeIcon><Text fw={750}>서비스 준비 상태</Text></Group><Text fz={23} fw={850} mt="lg" c="teal.8">정상 운영 중</Text><Text size="sm" c="dimmed" mt={4}>PostgreSQL·암호화 엔진 연결됨</Text></Card>
        </Stack>
      </SimpleGrid>
    </>
  );
}
