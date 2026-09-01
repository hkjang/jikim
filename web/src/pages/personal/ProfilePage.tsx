import { useState, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Alert,
  Badge,
  Box,
  Button,
  Divider,
  Group,
  Paper,
  PasswordInput,
  SimpleGrid,
  Skeleton,
  Stack,
  Text,
  TextInput,
  ThemeIcon,
  Title,
} from '@mantine/core';
import { notifications } from '@mantine/notifications';
import {
  CircleAlert,
  KeyRound,
  LockKeyhole,
  RefreshCw,
  Save,
  ShieldCheck,
  UserRound,
} from 'lucide-react';
import { get, patch, post } from '../../lib/api';
import { APP_VERSION, formatDate, statusLabel } from '../../lib/format';
import type { User } from '../../lib/types';
import { useAuth } from '../../contexts/AuthContext';

interface ProfileDraft {
  display_name: string;
  email: string;
}

interface PasswordDraft {
  current_password: string;
  new_password: string;
  confirm_password: string;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : '요청을 처리하지 못했습니다.';
}

function roleLabel(role?: string): string {
  const roles: Record<string, string> = {
    admin: '서비스 관리자',
    manager: '팀장',
    auditor: '감사자',
    user: '일반 사용자',
  };
  return roles[role || ''] || role || '일반 사용자';
}

function ProfileLoading() {
  return (
    <Stack gap="lg" aria-label="프로필을 불러오는 중">
      <Skeleton height={42} width="min(420px, 100%)" />
      <SimpleGrid cols={{ base: 1, md: 2 }}>
        <Skeleton height={360} radius="md" />
        <Skeleton height={360} radius="md" />
      </SimpleGrid>
    </Stack>
  );
}

function ProfileForm({ initialUser }: { initialUser: User }) {
  const { refresh, logout } = useAuth();
  const queryClient = useQueryClient();
  const [profile, setProfile] = useState<ProfileDraft>({
    display_name: initialUser.display_name || '',
    email: initialUser.email || '',
  });
  const [password, setPassword] = useState<PasswordDraft>({
    current_password: '',
    new_password: '',
    confirm_password: '',
  });
  const [passwordValidation, setPasswordValidation] = useState<string | null>(null);

  const profileMutation = useMutation({
    mutationFn: () => patch<User>('/me', {
      display_name: profile.display_name.trim(),
      email: profile.email.trim(),
    }),
    onSuccess: async (updated) => {
      queryClient.setQueryData(['profile'], updated);
      await refresh();
      notifications.show({ color: 'teal', title: '프로필 저장 완료', message: '개인 정보를 업데이트했습니다.' });
    },
  });

  const passwordMutation = useMutation({
    mutationFn: () => post<unknown>('/me/password', {
      current_password: password.current_password,
      new_password: password.new_password,
    }),
    onSuccess: async () => {
      setPassword({ current_password: '', new_password: '', confirm_password: '' });
      setPasswordValidation(null);
      notifications.show({
        color: 'teal',
        title: '비밀번호 변경 완료',
        message: '모든 기존 세션을 종료했습니다. 새 비밀번호로 다시 로그인해 주세요.',
      });
      await logout();
    },
  });

  const submitProfile = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    profileMutation.mutate();
  };

  const submitPassword = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setPasswordValidation(null);
    if (password.new_password.length < 12) {
      setPasswordValidation('새 비밀번호는 12자 이상이어야 합니다. 관리 정책에 따라 더 긴 비밀번호가 필요할 수 있습니다.');
      return;
    }
    if (password.new_password !== password.confirm_password) {
      setPasswordValidation('새 비밀번호와 확인 값이 일치하지 않습니다.');
      return;
    }
    if (password.current_password === password.new_password) {
      setPasswordValidation('현재 비밀번호와 다른 값을 입력하세요.');
      return;
    }
    passwordMutation.mutate();
  };

  return (
    <Stack gap="lg">
      <Box>
        <Title order={1} className="page-title">내 프로필</Title>
        <Text c="dimmed" mt={6}>계정 정보와 로컬 로그인 비밀번호를 안전하게 관리합니다.</Text>
      </Box>

      <SimpleGrid cols={{ base: 1, lg: 2 }} spacing="lg" style={{ alignItems: 'start' }}>
        <Paper component="form" onSubmit={submitProfile} className="surface" p={{ base: 'md', sm: 'xl' }} radius="lg">
          <Stack gap="lg">
            <Group align="flex-start" wrap="nowrap">
              <ThemeIcon size="lg" variant="light" aria-hidden="true"><UserRound size={20} /></ThemeIcon>
              <Box>
                <Title order={2} size="h3">기본 정보</Title>
                <Text c="dimmed" mt={3}>표시 이름과 알림용 이메일을 수정할 수 있습니다.</Text>
              </Box>
            </Group>

            {profileMutation.isError && (
              <Alert icon={<CircleAlert size={18} />} color="red" title="프로필을 저장하지 못했습니다" role="alert">
                {errorMessage(profileMutation.error)}
              </Alert>
            )}

            <TextInput label="사용자명" value={initialUser.username} readOnly description="사용자명은 관리자가 변경할 수 있습니다." />
            <TextInput
              label="표시 이름"
              autoComplete="name"
              value={profile.display_name}
              onChange={(event) => setProfile((current) => ({ ...current, display_name: event.currentTarget.value }))}
            />
            <TextInput
              type="email"
              label="이메일"
              autoComplete="email"
              placeholder="name@example.internal"
              value={profile.email}
              onChange={(event) => setProfile((current) => ({ ...current, email: event.currentTarget.value }))}
            />

            <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="sm">
              <Paper withBorder p="md" radius="md">
                <Text size="sm" c="dimmed">계정 역할</Text>
                <Badge color={initialUser.role === 'admin' ? 'violet' : 'teal'} mt={7}>{roleLabel(initialUser.role)}</Badge>
              </Paper>
              <Paper withBorder p="md" radius="md">
                <Text size="sm" c="dimmed">최근 로그인</Text>
                <Text fw={650} mt={7}>{formatDate(initialUser.last_login_at)}</Text>
              </Paper>
            </SimpleGrid>

            <Group justify="flex-end">
              <Button type="submit" leftSection={<Save size={17} />} loading={profileMutation.isPending}>
                프로필 저장
              </Button>
            </Group>
          </Stack>
        </Paper>

        <Stack gap="lg">
          <Paper component="form" onSubmit={submitPassword} className="surface" p={{ base: 'md', sm: 'xl' }} radius="lg">
            <Stack gap="lg">
              <Group align="flex-start" wrap="nowrap">
                <ThemeIcon size="lg" color="orange" variant="light" aria-hidden="true"><LockKeyhole size={20} /></ThemeIcon>
                <Box>
                  <Title order={2} size="h3">비밀번호 변경</Title>
                  <Text c="dimmed" mt={3}>로컬 로그인 계정에만 적용됩니다.</Text>
                </Box>
              </Group>

              {(passwordValidation || passwordMutation.isError) && (
                <Alert icon={<CircleAlert size={18} />} color="red" title="비밀번호를 변경할 수 없습니다" role="alert">
                  {passwordValidation || errorMessage(passwordMutation.error)}
                </Alert>
              )}

              <PasswordInput
                label="현재 비밀번호"
                autoComplete="current-password"
                required
                value={password.current_password}
                onChange={(event) => setPassword((current) => ({ ...current, current_password: event.currentTarget.value }))}
              />
              <PasswordInput
                label="새 비밀번호"
                description="12자 이상이며 현재 비밀번호와 달라야 합니다."
                autoComplete="new-password"
                required
                value={password.new_password}
                onChange={(event) => setPassword((current) => ({ ...current, new_password: event.currentTarget.value }))}
              />
              <PasswordInput
                label="새 비밀번호 확인"
                autoComplete="new-password"
                required
                value={password.confirm_password}
                onChange={(event) => setPassword((current) => ({ ...current, confirm_password: event.currentTarget.value }))}
              />
              <Group justify="flex-end">
                <Button type="submit" color="orange" leftSection={<KeyRound size={17} />} loading={passwordMutation.isPending}>
                  비밀번호 변경
                </Button>
              </Group>
            </Stack>
          </Paper>

          <Paper className="surface" p={{ base: 'md', sm: 'xl' }} radius="lg">
            <Stack gap="md">
              <Group justify="space-between" align="flex-start">
                <Group gap="sm">
                  <ThemeIcon size="lg" color="teal" variant="light" aria-hidden="true"><ShieldCheck size={20} /></ThemeIcon>
                  <Box>
                    <Title order={2} size="h3">계정 및 버전</Title>
                    <Text c="dimmed" mt={3}>지원 요청 시 아래 정보를 함께 알려주세요.</Text>
                  </Box>
                </Group>
                <Badge color={initialUser.status === 'disabled' ? 'gray' : 'teal'}>{statusLabel(initialUser.status || 'active')}</Badge>
              </Group>
              <Divider />
              <Group justify="space-between">
                <Text c="dimmed">Jikim 버전</Text>
                <Text fw={750} ff="monospace">{APP_VERSION}</Text>
              </Group>
              <Group justify="space-between" align="flex-start">
                <Text c="dimmed">사용자 ID</Text>
                <Text fw={650} ff="monospace" style={{ overflowWrap: 'anywhere', textAlign: 'right' }}>{initialUser.id}</Text>
              </Group>
              <Group justify="space-between">
                <Text c="dimmed">계정 생성</Text>
                <Text fw={650}>{formatDate(initialUser.created_at)}</Text>
              </Group>
            </Stack>
          </Paper>
        </Stack>
      </SimpleGrid>
    </Stack>
  );
}

export function ProfilePage() {
  const { user, loading: authLoading } = useAuth();
  const profileQuery = useQuery({
    queryKey: ['profile'],
    queryFn: () => get<User>('/me'),
    enabled: !authLoading && Boolean(user),
    initialData: user || undefined,
  });

  if (authLoading) return <ProfileLoading />;

  if (!user) {
    return (
      <Alert icon={<LockKeyhole size={20} />} color="red" title="로그인이 필요합니다" role="alert">
        프로필을 확인하려면 다시 로그인하세요.
      </Alert>
    );
  }

  if (profileQuery.isPending) return <ProfileLoading />;

  if (profileQuery.isError) {
    return (
      <Paper className="surface" p="xl" radius="lg">
        <Stack align="flex-start">
          <Alert icon={<CircleAlert size={20} />} color="red" title="프로필을 불러오지 못했습니다" w="100%" role="alert">
            {errorMessage(profileQuery.error)}
          </Alert>
          <Button variant="light" leftSection={<RefreshCw size={17} />} onClick={() => profileQuery.refetch()}>
            다시 시도
          </Button>
        </Stack>
      </Paper>
    );
  }

  return <ProfileForm key={`${profileQuery.data.id}-${profileQuery.data.display_name}-${profileQuery.data.email}`} initialUser={profileQuery.data} />;
}

export default ProfilePage;
