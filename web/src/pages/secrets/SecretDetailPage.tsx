import { useState } from 'react';
import { Alert, Badge, Box, Button, Card, CopyButton, Grid, Group, JsonInput, Modal, Paper, Stack, Table, Tabs, Text, TextInput, Timeline, Title, Tooltip } from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';
import { useQuery } from '@tanstack/react-query';
import { notifications } from '@mantine/notifications';
import { ArrowLeft, Check, Clock3, Copy, Eye, EyeOff, GitBranch, KeyRound, Pencil, RotateCcw, ShieldAlert, Trash2 } from 'lucide-react';
import { useNavigate, useParams } from 'react-router-dom';
import { del, get, post, put } from '../../lib/api';
import { formatDate, riskColor } from '../../lib/format';
import type { SecretRecord } from '../../lib/types';
import { PageError, PageLoading } from '../../components/AsyncState';
import { PageHeader } from '../../components/PageHeader';
import { StatusBadge } from '../../components/StatusBadge';

export function SecretDetailPage() {
  const { id = '' } = useParams();
  const navigate = useNavigate();
  const [revealed, setRevealed] = useState<Record<string, unknown> | null>(null);
  const [reason, setReason] = useState('운영 확인');
  const [revealOpened, reveal] = useDisclosure(false);
  const [editOpened, edit] = useDisclosure(false);
  const [editData, setEditData] = useState('');
  const [editReason, setEditReason] = useState('운영 값 변경');
  const [working, setWorking] = useState(false);
  const query = useQuery({ queryKey: ['secret', id], queryFn: async () => {
    const [secret, versions] = await Promise.all([get<any>(`/secrets/${id}`), get<any>(`/secrets/${id}/versions`)]);
    return { secret, versions: Array.isArray(versions) ? versions : versions?.items || [], dependencies: [] };
  }, enabled: Boolean(id) });
  if (query.isLoading) return <PageLoading />;
  if (query.isError) return <PageError error={query.error} onRetry={() => void query.refetch()} />;
  const secret: SecretRecord = query.data?.secret || query.data;
  const versions = query.data?.versions || [];
  const dependencies = query.data?.dependencies || [];
  const capabilities = new Set(secret.capabilities || []);
  const canUpdate = capabilities.has('update');
  const canDelete = capabilities.has('delete');
  const canRotate = capabilities.has('rotate');
  const revealValue = async () => {
    setWorking(true);
    try { const response = await post<any>(`/secrets/${id}/reveal`, { reason }); setRevealed(response.data || response.value || response); reveal.close(); notifications.show({ color: 'teal', message: '조회 사실이 감사 로그에 기록되었습니다.' }); }
    catch (error) { notifications.show({ color: 'red', message: error instanceof Error ? error.message : '값을 조회하지 못했습니다.' }); }
    finally { setWorking(false); }
  };
  const rotate = async () => {
    if (!window.confirm('시크릿 데이터의 새 버전을 만들까요? 자동 회전 가능한 필드만 난수로 교체하며 외부 시스템 연결 검증은 수행하지 않습니다.')) return;
    const rotationReason = window.prompt('감사 로그에 남길 회전 사유를 입력하세요.', '정기 수동 회전');
    if (!rotationReason?.trim()) return;
    setWorking(true);
    try { const result = await post<any>(`/secrets/${id}/rotate`, { reason: rotationReason.trim() }); notifications.show({ color: result?.status === 'pending' ? 'yellow' : 'teal', message: result?.status === 'pending' ? '회전 승인 요청을 만들었습니다.' : '새 시크릿 버전을 저장했습니다.' }); void query.refetch(); }
    catch (error) { notifications.show({ color: 'red', message: error instanceof Error ? error.message : '회전을 시작하지 못했습니다.' }); }
    finally { setWorking(false); }
  };
  const openEdit = () => {
    if (!revealed) {
      notifications.show({ color: 'blue', message: '현재 값을 감사 기록과 함께 조회한 뒤 변경할 수 있습니다.' });
      reveal.open();
      return;
    }
    setEditData(JSON.stringify(revealed, null, 2));
    setEditReason('운영 값 변경');
    edit.open();
  };
  const update = async () => {
    let data: Record<string, unknown>;
    try {
      const parsed = JSON.parse(editData);
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed) || Object.keys(parsed).length === 0) throw new Error();
      data = parsed as Record<string, unknown>;
    } catch {
      notifications.show({ color: 'red', message: '하나 이상의 필드를 가진 JSON 객체를 입력하세요.' });
      return;
    }
    if (editReason.trim().length < 3) {
      notifications.show({ color: 'red', message: '변경 사유를 3자 이상 입력하세요.' });
      return;
    }
    setWorking(true);
    try {
      const result = await put<{ status?: string }>(`/secrets/${id}`, {
        path: secret.path,
        description: secret.description || '',
        application_id: secret.application_id,
        owner_user_id: secret.owner_user_id,
        tags: secret.tags || [],
        data,
        metadata: { ...(secret.metadata || {}), update_reason: editReason.trim() },
      });
      const pending = result?.status === 'pending';
      notifications.show({ color: pending ? 'yellow' : 'teal', message: pending ? '변경 승인 요청을 만들었습니다.' : '새 시크릿 버전을 저장했습니다.' });
      edit.close();
      setRevealed(null);
      if (pending) navigate('/approvals'); else void query.refetch();
    } catch (error) {
      notifications.show({ color: 'red', message: error instanceof Error ? error.message : '시크릿을 변경하지 못했습니다.' });
    } finally {
      setWorking(false);
    }
  };
  const remove = async () => {
    if (!window.confirm('이 시크릿을 비활성화하고 격리하시겠습니까? 즉시 영구 삭제되지 않습니다.')) return;
    setWorking(true);
    try {
      const result = await del<{ status?: string }>(`/secrets/${id}`);
      const pending = result?.status === 'pending';
      notifications.show({ color: pending ? 'yellow' : 'teal', message: pending ? '격리 승인 요청을 만들었습니다.' : '시크릿을 격리했습니다.' });
      navigate(pending ? '/approvals' : '/secrets');
    } catch (error) {
      notifications.show({ color: 'red', message: error instanceof Error ? error.message : '시크릿을 격리하지 못했습니다.' });
    } finally {
      setWorking(false);
    }
  };
  return (
    <>
      <PageHeader
        eyebrow="Secret detail"
        title={secret.path}
        description="마스킹된 값과 버전, 소유자와 위험도를 한 화면에서 검토합니다."
        actions={(
          <>
            <Button variant="default" leftSection={<ArrowLeft size={18} />} onClick={() => navigate('/secrets')}>목록</Button>
            {canUpdate && (
              <Tooltip label={revealed ? '현재 값을 새 버전으로 변경' : '값 조회 후 변경할 수 있습니다.'}>
                <Button variant="default" leftSection={<Pencil size={18} />} disabled={working} onClick={openEdit}>값 변경</Button>
              </Tooltip>
            )}
            {canRotate && <Button variant="light" leftSection={<RotateCcw size={18} />} loading={working} onClick={() => void rotate()}>수동 회전</Button>}
            {canDelete && <Button color="red" variant="light" leftSection={<Trash2 size={18} />} loading={working} onClick={() => void remove()}>격리</Button>}
          </>
        )}
      />
      <SimpleSummary secret={secret} />
      <Tabs defaultValue="value" mt="lg">
        <Tabs.List><Tabs.Tab value="value" leftSection={<KeyRound size={16} />}>값</Tabs.Tab><Tabs.Tab value="versions" leftSection={<Clock3 size={16} />}>버전 {versions.length ? `(${versions.length})` : ''}</Tabs.Tab><Tabs.Tab value="dependencies" leftSection={<GitBranch size={16} />}>의존성 (후속)</Tabs.Tab></Tabs.List>
        <Tabs.Panel value="value" pt="lg"><Card className="surface" radius="lg" p="xl"><Group justify="space-between" mb="lg"><Box><Title order={2} fz="lg">시크릿 데이터</Title><Text size="sm" c="dimmed">기본 상태에서는 모든 값을 마스킹합니다.</Text></Box><Button variant={revealed ? 'default' : 'light'} leftSection={revealed ? <EyeOff size={18} /> : <Eye size={18} />} onClick={() => revealed ? setRevealed(null) : reveal.open()}>{revealed ? '다시 가리기' : '값 조회'}</Button></Group><Stack gap="sm">{Object.entries(revealed || secret.data || { username: '••••••••', password: '••••••••' }).map(([key, value]) => <Paper key={key} withBorder p="md" radius="md"><Group justify="space-between" wrap="nowrap"><Box><Text size="xs" c="dimmed" fw={700}>{key}</Text><Text className={revealed ? undefined : 'masked-secret'} ff="monospace" mt={5} style={{ wordBreak: 'break-all' }}>{revealed ? String(value) : '••••••••••••'}</Text></Box>{revealed && <CopyButton value={String(value)}>{({ copied, copy }) => <Tooltip label={copied ? '복사됨' : '복사'}><Button variant="subtle" size="xs" onClick={copy} leftSection={copied ? <Check size={15} /> : <Copy size={15} />}>{copied ? '복사됨' : '복사'}</Button></Tooltip>}</CopyButton>}</Group></Paper>)}</Stack>{revealed && <Alert mt="lg" color="orange" icon={<ShieldAlert size={20} />}>화면을 떠나기 전에 값을 다시 가리세요. 값은 AI 기능이나 감사 로그로 전달되지 않습니다.</Alert>}</Card></Tabs.Panel>
        <Tabs.Panel value="versions" pt="lg"><Card className="surface" radius="lg" p="xl"><Timeline active={0} bulletSize={30}>{versions.length ? versions.map((version: any) => <Timeline.Item key={version.version || version.id} title={<Group><Text fw={700}>버전 {version.version}</Text><StatusBadge status={version.status || 'active'} /></Group>} bullet={<Clock3 size={15} />}><Text c="dimmed" size="sm">{version.created_by || 'system'} · {formatDate(version.created_at)}</Text>{version.description && <Text mt="xs">{version.description}</Text>}</Timeline.Item>) : <Text c="dimmed">버전 이력이 아직 없습니다.</Text>}</Timeline></Card></Tabs.Panel>
        <Tabs.Panel value="dependencies" pt="lg"><Card className="surface" radius="lg" p={0}><Table><Table.Thead><Table.Tr><Table.Th>애플리케이션</Table.Th><Table.Th>환경</Table.Th><Table.Th>참조 방식</Table.Th><Table.Th>최근 사용</Table.Th></Table.Tr></Table.Thead><Table.Tbody>{dependencies.map((item: any, index: number) => <Table.Tr key={item.id || index}><Table.Td fw={700}>{item.application || item.name}</Table.Td><Table.Td><Badge>{item.environment || '—'}</Badge></Table.Td><Table.Td>{item.type || 'API'}</Table.Td><Table.Td>{formatDate(item.last_used_at)}</Table.Td></Table.Tr>)}{!dependencies.length && <Table.Tr><Table.Td colSpan={4}><Text ta="center" c="dimmed" py="xl">v0.2.0은 Application 직접 연결만 저장하며 의존성 그래프·사용량 탐지는 후속 기능입니다.</Text></Table.Td></Table.Tr>}</Table.Tbody></Table></Card></Tabs.Panel>
      </Tabs>
      <Modal opened={revealOpened} onClose={reveal.close} title="시크릿 값 조회" centered><Stack><Alert color="blue" icon={<Eye size={20} />}>값 조회는 권한을 확인하고 감사 이벤트를 남깁니다.</Alert><TextInput label="조회 사유" required value={reason} onChange={(e) => setReason(e.currentTarget.value)} /><Button loading={working} disabled={!reason.trim()} onClick={() => void revealValue()}>확인하고 조회</Button></Stack></Modal>
      <Modal opened={editOpened} onClose={() => !working && edit.close()} title="시크릿 값 변경" centered size="lg">
        <Stack>
          <Alert color="orange" icon={<ShieldAlert size={20} />}>전체 JSON 객체를 새 버전으로 저장합니다. 외부 시스템에는 자동으로 반영하지 않습니다.</Alert>
          <JsonInput
            label="새 시크릿 값"
            required
            formatOnBlur
            autosize
            minRows={10}
            validationError="유효한 JSON 객체가 아닙니다."
            value={editData}
            onChange={setEditData}
            styles={{ input: { fontFamily: 'ui-monospace, monospace', fontSize: 15 } }}
          />
          <TextInput label="변경 사유" required value={editReason} onChange={(event) => setEditReason(event.currentTarget.value)} />
          <Group justify="flex-end">
            <Button variant="default" disabled={working} onClick={edit.close}>취소</Button>
            <Button leftSection={<Pencil size={17} />} loading={working} disabled={!editReason.trim()} onClick={() => void update()}>새 버전 저장</Button>
          </Group>
        </Stack>
      </Modal>
    </>
  );
}

function SimpleSummary({ secret }: { secret: SecretRecord }) {
  return <Grid gutter="lg"><Grid.Col span={{ base: 12, sm: 6, lg: 3 }}><Card className="surface" radius="lg"><Text size="sm" c="dimmed">상태</Text><Box mt="sm"><StatusBadge status={secret.status || 'active'} /></Box></Card></Grid.Col><Grid.Col span={{ base: 12, sm: 6, lg: 3 }}><Card className="surface" radius="lg"><Text size="sm" c="dimmed">현재 버전</Text><Text fz={24} fw={800} mt={3}>v{secret.version || 1}</Text></Card></Grid.Col><Grid.Col span={{ base: 12, sm: 6, lg: 3 }}><Card className="surface" radius="lg"><Text size="sm" c="dimmed">위험 점수</Text><Group mt="xs"><Badge color={riskColor(secret.risk_score)} size="lg">{secret.risk_score || 0} / 100</Badge></Group></Card></Grid.Col><Grid.Col span={{ base: 12, sm: 6, lg: 3 }}><Card className="surface" radius="lg"><Text size="sm" c="dimmed">Owner</Text><Text fw={750} mt="xs">{secret.owner || '미지정'}</Text></Card></Grid.Col></Grid>;
}
