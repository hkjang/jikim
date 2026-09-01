import { Alert, Button, Center, Stack, Title } from '@mantine/core';
import { ShieldX } from 'lucide-react';
import { useNavigate } from 'react-router-dom';

export function ForbiddenPage() {
  const navigate = useNavigate();
  return <Center mih="70vh"><Stack maw={540} w="100%"><Title order={1}>접근 권한이 없습니다.</Title><Alert icon={<ShieldX size={22} />} color="orange" title="관리자 전용 영역">현재 계정의 역할로 이 페이지를 열 수 없습니다. 필요한 경우 서비스 관리자에게 권한 변경을 요청하세요.</Alert><Button variant="light" onClick={() => navigate('/dashboard')}>대시보드로 돌아가기</Button></Stack></Center>;
}
