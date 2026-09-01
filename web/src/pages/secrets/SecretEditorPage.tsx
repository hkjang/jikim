import { useState } from 'react';
import { Alert, Box, Button, Card, Code, Grid, Group, JsonInput, Select, Stack, Switch, TagsInput, Text, TextInput, Title } from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { useQuery } from '@tanstack/react-query';
import { ArrowLeft, Info, Save, ShieldCheck } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { get, post } from '../../lib/api';
import { PageHeader } from '../../components/PageHeader';
import { useAuth } from '../../contexts/AuthContext';

interface ApplicationOption {
  id: string;
  name: string;
  environment?: string;
}

function normalizeApplications(value: unknown): ApplicationOption[] {
  const source = Array.isArray(value)
    ? value
    : value && typeof value === 'object' && Array.isArray((value as { items?: unknown[] }).items)
      ? (value as { items: unknown[] }).items
      : [];
  return source.flatMap((entry) => {
    if (!entry || typeof entry !== 'object') return [];
    const item = entry as Record<string, unknown>;
    return typeof item.id === 'string' && typeof item.name === 'string'
      ? [{ id: item.id, name: item.name, environment: typeof item.environment === 'string' ? item.environment : undefined }]
      : [];
  });
}

export function SecretEditorPage() {
  const { user } = useAuth();
  const navigate = useNavigate();
  const [saving, setSaving] = useState(false);
  const [form, setForm] = useState({ path: '', application_id: '', environment: 'DEV', owner: user?.display_name || user?.username || '', tags: [] as string[], data: '{\n  "username": "",\n  "password": ""\n}', rotation_enabled: false, rotation_days: '30' });
  const applicationsQuery = useQuery({ queryKey: ['applications', 'secret-editor'], queryFn: () => get<unknown>('/applications') });
  const applications = normalizeApplications(applicationsQuery.data);
  const save = async () => {
    let data: Record<string, unknown>;
    try { data = JSON.parse(form.data); } catch { notifications.show({ color: 'red', message: '시크릿 값이 올바른 JSON인지 확인해 주세요.' }); return; }
    if (!form.path.trim()) { notifications.show({ color: 'red', message: '시크릿 경로를 입력해 주세요.' }); return; }
    setSaving(true);
    try {
      const application = applications.find((item) => item.id === form.application_id);
      const created = await post<{ id?: string; status?: string }>('/secrets', {
        path: form.path,
        description: application ? `${application.name} 애플리케이션 시크릿` : '',
        application_id: form.application_id || undefined,
        owner_user_id: user?.id || undefined,
        tags: form.tags,
        data,
        metadata: {
          application: application?.name || '',
          environment: form.environment,
          owner: form.owner,
          rotation: { enabled: form.rotation_enabled, days: Number(form.rotation_days) },
        },
      });
      notifications.show({ color: created.status === 'pending' ? 'yellow' : 'teal', message: created.status === 'pending' ? '승인 요청이 생성되었습니다.' : '시크릿을 안전하게 저장했습니다.' });
      navigate(created.status === 'pending' ? '/approvals' : created.id ? `/secrets/${created.id}` : '/secrets');
    } catch (error) { notifications.show({ color: 'red', message: error instanceof Error ? error.message : '시크릿을 저장하지 못했습니다.' }); }
    finally { setSaving(false); }
  };
  return (
    <>
      <PageHeader eyebrow="Create secret" title="새 시크릿 만들기" description="평문 값은 전송 직후 암호화하며 응답·감사 로그에 남기지 않습니다." actions={<Button variant="default" leftSection={<ArrowLeft size={18} />} onClick={() => navigate(-1)}>취소</Button>} />
      <Grid gutter="lg">
        <Grid.Col span={{ base: 12, lg: 8 }}><Card className="surface" radius="lg" p="xl"><Stack gap="lg"><Title order={2} fz="lg">기본 정보</Title><TextInput required label="시크릿 경로" description="예: payment/production/database" placeholder="application/environment/resource" value={form.path} onChange={(e) => setForm({ ...form, path: e.currentTarget.value.replace(/^\/+/, '') })} /><Grid><Grid.Col span={{ base: 12, sm: 6 }}><Select label="애플리케이션" description="카탈로그에 등록된 서비스와 직접 연결합니다." placeholder={applicationsQuery.isLoading ? '목록을 불러오는 중' : '연결하지 않음'} searchable clearable data={applications.map((item) => ({ value: item.id, label: item.name }))} value={form.application_id || null} onChange={(value) => { const selected = applications.find((item) => item.id === value); setForm({ ...form, application_id: value || '', environment: selected?.environment || form.environment }); }} /></Grid.Col><Grid.Col span={{ base: 12, sm: 6 }}><Select label="환경" value={form.environment} onChange={(value) => setForm({ ...form, environment: value || 'DEV' })} data={[{ value: 'DEV', label: '개발 (DEV)' }, { value: 'STG', label: '스테이징 (STG)' }, { value: 'PRD', label: '운영 (PRD)' }]} /></Grid.Col></Grid><TextInput label="Owner 표시" description="키 소유자는 현재 로그인 사용자로 안전하게 연결됩니다." placeholder="플랫폼개발팀" value={form.owner} onChange={(e) => setForm({ ...form, owner: e.currentTarget.value })} /><TagsInput label="태그" value={form.tags} onChange={(tags) => setForm({ ...form, tags })} placeholder="태그를 입력하고 Enter" splitChars={[',']} /></Stack></Card>
        <Card className="surface" radius="lg" p="xl" mt="lg"><Stack gap="md"><Group justify="space-between"><Box><Title order={2} fz="lg">시크릿 값</Title><Text size="sm" c="dimmed">JSON 객체만 저장할 수 있습니다. 민감 키는 자동 마스킹됩니다.</Text></Box><Code color="teal">AES-256-GCM</Code></Group><JsonInput value={form.data} onChange={(data) => setForm({ ...form, data })} minRows={10} autosize formatOnBlur validationError="유효한 JSON 객체가 아닙니다." styles={{ input: { fontFamily: 'ui-monospace, monospace', fontSize: 15 } }} /></Stack></Card></Grid.Col>
        <Grid.Col span={{ base: 12, lg: 4 }}><Stack><Card className="surface" radius="lg" p="xl"><Group gap="sm" mb="lg"><ShieldCheck color="#11b5ae" /><Title order={2} fz="lg">회전 정책</Title></Group><Stack><Switch label="자동 회전 예약 (후속 기능)" checked={form.rotation_enabled} disabled onChange={(e) => setForm({ ...form, rotation_enabled: e.currentTarget.checked })} /><TextInput disabled label="회전 주기(일)" type="number" min={1} max={3650} value={form.rotation_days} onChange={(e) => setForm({ ...form, rotation_days: e.currentTarget.value })} /><Text size="sm" c="dimmed">v0.1.0은 상세 화면의 수동 KV 새 버전 생성만 지원하며 스케줄러와 외부 Connector는 제공하지 않습니다.</Text></Stack></Card><Alert color="blue" icon={<Info size={20} />} title="검토·승인 설정">관리자가 승인 프로세스를 켠 경우 저장 대신 검토 요청이 생성됩니다. 꺼져 있으면 즉시 반영됩니다.</Alert><Button size="lg" leftSection={<Save size={19} />} loading={saving} onClick={() => void save()}>암호화해 저장</Button></Stack></Grid.Col>
      </Grid>
    </>
  );
}
