import { useRef, useState } from 'react';
import { ActionIcon, Alert, Avatar, Badge, Box, Button, Card, Group, NumberInput, ScrollArea, Stack, Text, Textarea, ThemeIcon, Title } from '@mantine/core';
import { useQuery } from '@tanstack/react-query';
import { Bot, CircleStop, Send, ShieldCheck, Sparkles, UserRound } from 'lucide-react';
import { get, streamChat } from '../lib/api';
import { PageHeader } from '../components/PageHeader';

interface Message { role: 'user' | 'assistant'; content: string }

export function AiPage() {
  const settings = useQuery({ queryKey: ['public-settings'], queryFn: () => get<any>('/settings/public'), retry: false });
  const [messages, setMessages] = useState<Message[]>([]);
  const [input, setInput] = useState('');
  const [maxTokens, setMaxTokens] = useState<number | string>(4096);
  const [streaming, setStreaming] = useState(false);
  const [error, setError] = useState('');
  const controller = useRef<AbortController | null>(null);

  const submit = async (prompt = input) => {
    if (!prompt.trim() || streaming) return;
    setError(''); setInput(''); setStreaming(true);
    const next: Message[] = [...messages, { role: 'user', content: prompt }, { role: 'assistant', content: '' }];
    setMessages(next);
    controller.current = new AbortController();
    try {
      await streamChat({ messages: next.slice(0, -1), max_tokens: Number(maxTokens) || 4096 }, (chunk) => {
        setMessages((current) => current.map((message, index) => index === current.length - 1 ? { ...message, content: message.content + chunk } : message));
      }, controller.current.signal);
    } catch (caught) {
      if ((caught as Error).name !== 'AbortError') setError(caught instanceof Error ? caught.message : 'AI 응답 중 오류가 발생했습니다.');
    } finally { setStreaming(false); controller.current = null; }
  };

  return <>
    <PageHeader eyebrow="Security copilot" title="AI 보안 도우미" description="시크릿 평문을 보내지 않고 메타데이터, 정책, 위험 지표와 오류만 분석합니다." actions={<Badge color="teal" size="lg" leftSection={<Sparkles size={14} />}>SSE 스트리밍 기본</Badge>} />
    {!settings.data?.ai_enabled && <Alert mb="lg" color="blue" icon={<Bot size={20} />} title="AI 연동이 비활성 상태입니다.">서비스 관리자가 AI 공급자 URL, 모델과 API 키를 등록하면 사용할 수 있습니다. 설정 전에는 어떤 외부 요청도 발생하지 않습니다.</Alert>}
    <Alert mb="lg" color="teal" variant="light" icon={<ShieldCheck size={20} />} title="민감정보 보호 경계">시크릿 값, 복호화된 키, 토큰을 질문에 붙여 넣지 마세요. 서버도 저장된 시크릿 평문을 AI 요청에 포함하지 않습니다.</Alert>
    <Card className="surface" radius="lg" p={0} style={{ overflow: 'hidden' }}>
      <Group px="lg" py="md" justify="space-between" style={{ borderBottom: '1px solid #e2eaee' }}><Group><ThemeIcon variant="light" size={40}><Bot size={21} /></ThemeIcon><Box><Text fw={800}>운영 분석 대화</Text><Text size="xs" c="dimmed">설정된 내부 또는 OpenAI-compatible API</Text></Box></Group><NumberInput label="최대 출력 토큰" hideControls value={maxTokens} onChange={setMaxTokens} min={1} max={262144} clampBehavior="strict" w={160} /></Group>
      <ScrollArea h={{ base: 430, md: 520 }} p="lg">
        {!messages.length ? <Stack align="center" justify="center" mih={380} ta="center"><Avatar size={72} radius="xl" color="teal"><Bot size={34} /></Avatar><div><Title order={2} fz="xl">어떤 보안 운영을 도와드릴까요?</Title><Text c="dimmed" mt="xs">메타데이터와 감사 통계를 바탕으로 분석 초안을 제공합니다.</Text></div><Group justify="center" maw={760}>{['최근 위험도가 높아진 시크릿 알려줘', '회전 실패 원인을 분석해줘', '과도한 정책을 점검하는 방법 알려줘'].map((sample) => <Button key={sample} variant="default" size="sm" onClick={() => void submit(sample)}>{sample}</Button>)}</Group></Stack> : <Stack gap="lg">{messages.map((message, index) => <Group key={index} align="flex-start" wrap="nowrap" justify={message.role === 'user' ? 'flex-end' : 'flex-start'}><Avatar color={message.role === 'user' ? 'navy' : 'teal'} radius="xl">{message.role === 'user' ? <UserRound size={19} /> : <Bot size={19} />}</Avatar><Box bg={message.role === 'user' ? 'navy.9' : 'gray.0'} c={message.role === 'user' ? 'white' : undefined} p="md" style={{ borderRadius: 14, maxWidth: '78%', whiteSpace: 'pre-wrap', lineHeight: 1.75 }}><Text>{message.content || (streaming && index === messages.length - 1 ? '생각하는 중…' : '')}</Text></Box></Group>)}</Stack>}
      </ScrollArea>
      <Box p="lg" bg="gray.0" style={{ borderTop: '1px solid #e2eaee' }}>{error && <Alert color="red" mb="sm">{error}</Alert>}<Group align="flex-end" wrap="nowrap"><Textarea flex={1} autosize minRows={2} maxRows={6} placeholder="민감한 평문을 제외하고 질문을 입력하세요. (Ctrl+Enter로 전송)" value={input} disabled={streaming || !settings.data?.ai_enabled} onChange={(e) => setInput(e.currentTarget.value)} onKeyDown={(e) => { if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) void submit(); }} />{streaming ? <ActionIcon size={48} color="red" variant="light" aria-label="응답 중지" onClick={() => controller.current?.abort()}><CircleStop /></ActionIcon> : <ActionIcon size={48} aria-label="질문 전송" disabled={!input.trim() || !settings.data?.ai_enabled} onClick={() => void submit()}><Send size={21} /></ActionIcon>}</Group><Text size="xs" c="dimmed" mt="xs">설정 상한은 262,144이며 실제 모델의 지원 한도는 공급자에 따라 다릅니다.</Text></Box>
    </Card>
  </>;
}
