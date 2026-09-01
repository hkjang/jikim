import { useState } from 'react';
import { Alert, Anchor, Box, Button, Divider, Group, Image, Paper, PasswordInput, Stack, Text, TextInput, ThemeIcon, Title } from '@mantine/core';
import { useForm } from 'react-hook-form';
import { AlertCircle, ArrowRight, KeyRound, LockKeyhole, Radio, ShieldCheck } from 'lucide-react';
import { Navigate, useLocation, useNavigate } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { get } from '../lib/api';
import { APP_VERSION } from '../lib/format';
import { useAuth } from '../contexts/AuthContext';

interface FormValues { username: string; password: string }
interface PublicSettings { oidc_enabled?: boolean; oidc_login_url?: string; version?: string }

export function LoginPage() {
  const { user, login } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const [error, setError] = useState('');
  const { register, handleSubmit, formState: { errors, isSubmitting } } = useForm<FormValues>({ defaultValues: { username: '', password: '' } });
  const settings = useQuery({ queryKey: ['public-settings-login'], queryFn: () => get<PublicSettings>('/settings/public'), retry: false });
  if (user) return <Navigate to="/dashboard" replace />;

  const submit = handleSubmit(async (values) => {
    setError('');
    try {
      await login(values.username, values.password);
      const target = (location.state as { from?: { pathname?: string } } | null)?.from?.pathname || '/dashboard';
      navigate(target, { replace: true });
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : '아이디 또는 비밀번호를 확인해 주세요.');
    }
  });

  const oidcLogin = () => {
    const returnTo = `${window.location.origin}/oidc/callback`;
    window.location.href = settings.data?.oidc_login_url || `/api/v1/oidc/login?redirect_uri=${encodeURIComponent(returnTo)}`;
  };

  return (
    <div className="login-page">
      <div className="login-grid">
        <section className="login-story" aria-label="제품 소개">
          <Stack gap="xl" maw={650}>
            <Group gap="md"><Image src="/logo.png" alt="" w={76} h={76} fit="contain" /><Box><Text fz={38} fw={850} className="brand-word">jikim</Text><Text c="#9edbd7" fw={650}>OpenBao 2.6.1 API 제한 호환 프리뷰</Text></Box></Group>
            <Title order={1} fz={{ base: 38, xl: 51 }} lh={1.18} lts="-.045em">보이지 않는 자격 증명까지<br />안전하게 지킵니다.</Title>
            <Text fz="lg" c="#c6dce5" lh={1.8}>시크릿, 개인 키, 정책, 회전과 접근 승인을 하나의 한국어 관리 화면에서 운영하세요. 모든 정적 자산은 이미지에 포함되어 폐쇄망에서도 그대로 동작합니다.</Text>
            <Group gap="lg">
              {[
                [ShieldCheck, '저장 시 암호화'], [KeyRound, '개인별 키 회전'], [Radio, 'AI 응답 스트리밍'],
              ].map(([Icon, label]) => <Group key={String(label)} gap="xs"><ThemeIcon color="teal" variant="light" radius="xl"><Icon size={18} /></ThemeIcon><Text fw={650}>{String(label)}</Text></Group>)}
            </Group>
          </Stack>
        </section>
        <section className="login-panel">
          <Box className="login-card">
            <Stack gap={8} mb="xl">
              <Group hiddenFrom="md" mb="md"><Image src="/logo.png" alt="jikim 로고" w={54} h={54} /><Text fz={30} fw={850}>jikim</Text></Group>
              <Text size="sm" fw={800} c="teal.8">보안 관리 콘솔</Text>
              <Title order={2} fz={30} className="page-title">안전하게 로그인하세요</Title>
              <Text c="dimmed">관리자가 등록한 로컬 계정 또는 Keycloak SSO를 사용할 수 있습니다.</Text>
            </Stack>
            <Paper className="surface" radius="lg" p={{ base: 'lg', sm: 'xl' }} bg="white">
              <form onSubmit={submit} noValidate>
                <Stack gap="md">
                  {error && <Alert color="red" icon={<AlertCircle size={19} />} title="로그인 실패">{error}</Alert>}
                  <TextInput label="아이디" autoComplete="username" placeholder="아이디를 입력하세요" leftSection={<LockKeyhole size={18} />} error={errors.username?.message} {...register('username', { required: '아이디를 입력해 주세요.' })} />
                  <PasswordInput label="비밀번호" autoComplete="current-password" placeholder="비밀번호를 입력하세요" error={errors.password?.message} {...register('password', { required: '비밀번호를 입력해 주세요.' })} />
                  <Group justify="space-between"><Text size="sm" c="dimmed">접근 문제가 있으면 서비스 관리자에게 계정 상태를 요청하세요.</Text><Text size="xs" c="dimmed">TLS 연결 권장</Text></Group>
                  <Button type="submit" fullWidth loading={isSubmitting} rightSection={<ArrowRight size={18} />}>로그인</Button>
                  {settings.data?.oidc_enabled && <><Divider label="또는" labelPosition="center" /><Button type="button" variant="default" fullWidth onClick={oidcLogin} leftSection={<ShieldCheck size={18} />}>Keycloak SSO로 로그인</Button></>}
                </Stack>
              </form>
            </Paper>
            <Group justify="space-between" mt="lg" align="flex-start">
              <Text size="sm" c="dimmed">jikim {settings.data?.version || APP_VERSION}</Text>
              <Text size="xs" c="dimmed" ta="right">
                OpenBao 2.6.1 API 제한 호환 프리뷰<br />
                <Anchor href="/guide" size="xs">제품 가이드</Anchor>
              </Text>
            </Group>
          </Box>
        </section>
      </div>
    </div>
  );
}
