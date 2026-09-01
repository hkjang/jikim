import { useMemo, useState } from 'react';
import { Alert, Badge, Button, Card, Code, Grid, Group, JsonInput, SegmentedControl, Stack, Tabs, Text, TextInput, Title } from '@mantine/core';
import { Braces, CheckCircle2, Copy, FileJson2, Play, ShieldCheck, Terminal } from 'lucide-react';
import { PageHeader } from '../components/PageHeader';

export function ApiExplorerPage() {
  const [method, setMethod] = useState('GET');
  const [path, setPath] = useState('/v1/sys/health');
  const [body, setBody] = useState('{\n  \\"data\\": {\n    \\"example\\": \\"value\\"\n  }\n}'.replaceAll('\\"', '"'));
  const [response, setResponse] = useState('');
  const [status, setStatus] = useState<number | null>(null);
  const [running, setRunning] = useState(false);
  const execute = async () => {
    if (!path.startsWith('/') || path.startsWith('//')) { setResponse(JSON.stringify({ errors: ['상대 API 경로만 사용할 수 있습니다.'] }, null, 2)); setStatus(400); return; }
    setRunning(true); setStatus(null);
    try {
	      const headers = new Headers({ Accept: path === '/mcp' ? 'application/json, text/event-stream' : 'application/json' });
	      if (path === '/mcp') headers.set('MCP-Protocol-Version', '2025-11-25');
      let payload: string | undefined;
      if (!['GET', 'HEAD', 'DELETE', 'LIST'].includes(method)) { JSON.parse(body); headers.set('Content-Type', 'application/json'); payload = body; }
      const result = await fetch(path, { method, credentials: 'same-origin', headers, body: payload });
      setStatus(result.status);
      const text = await result.text();
      try { setResponse(JSON.stringify(JSON.parse(text), null, 2)); } catch { setResponse(text || '(응답 본문 없음)'); }
    } catch (error) { setResponse(JSON.stringify({ errors: [error instanceof Error ? error.message : '요청 실패'] }, null, 2)); setStatus(0); }
    finally { setRunning(false); }
  };
  const curl = useMemo(() => `curl --request ${method} \\\n  --header "X-Vault-Token: $JIKIM_TOKEN"${path === '/mcp' ? ` \\\n  --header "Accept: application/json, text/event-stream" \\\n  --header "MCP-Protocol-Version: 2025-11-25"` : ''}${!['GET', 'HEAD', 'DELETE', 'LIST'].includes(method) ? ` \\\n  --header "Content-Type: application/json" \\\n  --data '${body.replaceAll("'", "'\\''")}'` : ''} \\\n  "http://jikim.local:8080${path}"`, [method, path, body]);
  return <>
    <PageHeader eyebrow="Developer tools" title="API 탐색기" description="현재 세션 권한으로 jikim REST API와 구현된 OpenBao 호환 API를 직접 확인합니다." actions={<Button component="a" href="/api/openapi.json" target="_blank" rel="noreferrer" variant="light" leftSection={<FileJson2 size={17} />}>OpenAPI 3.1</Button>} />
    <Alert mb="lg" color="yellow" icon={<ShieldCheck size={20} />} title="운영 환경 사용 주의">실행한 쓰기 요청은 실제 데이터에 반영되고 감사 로그에 기록됩니다. 응답을 공유하기 전에 토큰과 시크릿 값을 제거하세요.</Alert>
    <Grid gutter="lg"><Grid.Col span={{ base: 12, lg: 7 }}><Card className="surface" radius="lg" p="xl"><Stack><Title order={2} fz="lg">요청</Title><Group align="flex-end" wrap="nowrap"><SegmentedControl value={method} onChange={setMethod} data={['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'LIST']} /><TextInput label="경로" flex={1} value={path} onChange={(e) => setPath(e.currentTarget.value)} placeholder="/v1/sys/health" /></Group>{!['GET', 'HEAD', 'DELETE', 'LIST'].includes(method) && <JsonInput label="요청 JSON" value={body} onChange={setBody} minRows={11} autosize formatOnBlur validationError="유효한 JSON이 아닙니다." styles={{ input: { fontFamily: 'ui-monospace, monospace' } }} />}<Button leftSection={<Play size={18} />} loading={running} onClick={() => void execute()}>실행</Button></Stack></Card><Card className="surface" radius="lg" p="xl" mt="lg"><Group justify="space-between" mb="md"><Title order={2} fz="lg">응답</Title>{status !== null && <Badge color={status >= 200 && status < 300 ? 'teal' : 'red'} size="lg">HTTP {status}</Badge>}</Group><pre className="code-block" style={{ minHeight: 220 }}>{response || '// 실행 결과가 여기에 표시됩니다.'}</pre></Card></Grid.Col><Grid.Col span={{ base: 12, lg: 5 }}><Card className="surface" radius="lg" p="xl"><Tabs defaultValue="curl"><Tabs.List><Tabs.Tab value="curl" leftSection={<Terminal size={16} />}>cURL</Tabs.Tab><Tabs.Tab value="mcp" leftSection={<Braces size={16} />}>MCP</Tabs.Tab></Tabs.List><Tabs.Panel value="curl" pt="md"><pre className="code-block">{curl}</pre><Button mt="md" variant="light" leftSection={<Copy size={17} />} onClick={() => void navigator.clipboard.writeText(curl)}>코드 복사</Button></Tabs.Panel><Tabs.Panel value="mcp" pt="md"><Text mb="sm" c="dimmed">MCP 클라이언트는 <Code>POST /mcp</Code>에서 JSON-RPC 2.0으로 도구를 조회·호출합니다.</Text><pre className="code-block">{`{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/list"
}`}</pre><Text size="xs" c="dimmed" mt="sm">모든 MCP POST는 <Code>Content-Type: application/json</Code>, dual Accept와 협상된 protocol header를 사용합니다.</Text></Tabs.Panel></Tabs></Card><Card className="surface" radius="lg" p="xl" mt="lg"><Group mb="md"><CheckCircle2 color="#11b5ae" /><Title order={2} fz="lg">호환 프로파일</Title></Group><Stack gap="xs"><Text>• OpenBao 2.6.1 기준 제한 프로파일</Text><Text>• KV v2 CAS·soft delete·undelete·destroy·metadata</Text><Text>• Token lookup/create/revoke 일부</Text><Text>• Transit encrypt/decrypt 일부</Text><Text>• 전체 OpenBao API 호환을 의미하지 않음</Text></Stack></Card></Grid.Col></Grid>
  </>;
}
