import { Button, Center, Stack, Text, ThemeIcon, Title } from '@mantine/core';
import { MapPinOff } from 'lucide-react';
import { useNavigate } from 'react-router-dom';

export function NotFoundPage() {
  const navigate = useNavigate();
  return <Center mih="70vh"><Stack align="center" ta="center" maw={520}><ThemeIcon size={72} radius="xl" color="teal" variant="light"><MapPinOff size={34} /></ThemeIcon><Text size="sm" fw={800} c="teal.8">404</Text><Title order={1}>페이지를 찾을 수 없습니다.</Title><Text c="dimmed">주소가 바뀌었거나 접근 가능한 메뉴가 아닙니다. 저장된 북마크를 확인해 주세요.</Text><Button onClick={() => navigate('/dashboard')}>대시보드로 이동</Button></Stack></Center>;
}
