import { Alert, Button, Center, Loader, Stack, Text, ThemeIcon } from '@mantine/core';
import { AlertCircle, Inbox, RefreshCw } from 'lucide-react';

export function PageLoading({ label = '데이터를 불러오고 있습니다.' }: { label?: string }) {
  return <Center mih={280}><Stack align="center" gap="sm"><Loader color="teal" /><Text c="dimmed">{label}</Text></Stack></Center>;
}

export function PageError({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  const message = error instanceof Error ? error.message : '알 수 없는 오류가 발생했습니다.';
  return (
    <Alert color="red" icon={<AlertCircle size={20} />} title="화면을 불러오지 못했습니다." radius="md">
      <Text mb={onRetry ? 'md' : 0}>{message}</Text>
      {onRetry && <Button color="red" variant="light" leftSection={<RefreshCw size={17} />} onClick={onRetry}>다시 시도</Button>}
    </Alert>
  );
}

export function EmptyState({ title = '표시할 항목이 없습니다.', description, action }: { title?: string; description?: string; action?: React.ReactNode }) {
  return (
    <Center mih={260} p="xl">
      <Stack align="center" maw={460} ta="center">
        <ThemeIcon size={52} radius="xl" color="gray" variant="light"><Inbox size={25} /></ThemeIcon>
        <Text fw={700} size="lg">{title}</Text>
        {description && <Text c="dimmed">{description}</Text>}
        {action}
      </Stack>
    </Center>
  );
}
