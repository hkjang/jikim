const API_BASE = '/api/v1';

export class ApiError extends Error {
  status: number;
  details?: unknown;

  constructor(message: string, status: number, details?: unknown) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.details = details;
  }
}

function unwrap<T>(body: T | { data: T }): T {
  if (body && typeof body === 'object' && 'data' in body) {
    const keys = Object.keys(body);
    const envelopeKeys = new Set(['data', 'meta', 'pagination', 'request_id']);
    if (keys.every((key) => envelopeKeys.has(key))) return (body as { data: T }).data;
  }
  return body as T;
}

export async function api<T>(path: string, options: RequestInit = {}): Promise<T> {
  const headers = new Headers(options.headers);
  headers.set('Accept', 'application/json');
  if (options.body && !(options.body instanceof FormData)) headers.set('Content-Type', 'application/json');

  const response = await fetch(path.startsWith('/api/') ? path : `${API_BASE}${path}`, { ...options, credentials: 'same-origin', headers });
  const contentType = response.headers.get('content-type') || '';
  const body = contentType.includes('application/json') ? await response.json() : await response.text();

  if (!response.ok) {
    const message = typeof body === 'object'
      ? body?.message || body?.error?.message || (typeof body?.error === 'string' ? body.error : '') || body?.errors?.join?.(', ') || '요청을 처리하지 못했습니다.'
      : body || '요청을 처리하지 못했습니다.';
    if (response.status === 401) window.dispatchEvent(new CustomEvent('jikim:unauthorized'));
    throw new ApiError(message, response.status, body);
  }
  return unwrap<T>(body);
}

export const get = <T>(path: string) => api<T>(path);
export const post = <T>(path: string, data?: unknown) => api<T>(path, { method: 'POST', body: data === undefined ? undefined : JSON.stringify(data) });
export const patch = <T>(path: string, data?: unknown) => api<T>(path, { method: 'PATCH', body: JSON.stringify(data) });
export const put = <T>(path: string, data?: unknown) => api<T>(path, { method: 'PUT', body: JSON.stringify(data) });
export const del = <T>(path: string) => api<T>(path, { method: 'DELETE' });

export const simulatePolicy = <T>(input: { user_id?: string; path: string; capability: string }) =>
  post<T>('/policies/simulate', input);

export const testAIIntegration = <T>() => post<T>('/integrations/ai/test', {});

export const testWebhookIntegration = <T>() => post<T>('/integrations/webhook/test', {});

export async function streamChat(
  payload: { messages: Array<{ role: string; content: string }>; max_tokens?: number },
  onChunk: (text: string) => void,
  signal?: AbortSignal,
) {
  const headers = new Headers({ Accept: 'text/event-stream', 'Content-Type': 'application/json' });
  const response = await fetch(`${API_BASE}/ai/chat`, { method: 'POST', credentials: 'same-origin', headers, body: JSON.stringify(payload), signal });
  if (!response.ok || !response.body) {
    const detail = await response.text();
    throw new ApiError(detail || 'AI 스트리밍 연결을 시작할 수 없습니다.', response.status);
  }
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    const lines = buffer.split('\n');
    buffer = lines.pop() || '';
    for (const line of lines) {
      if (!line.startsWith('data:')) continue;
      const raw = line.slice(5).trim();
      if (!raw || raw === '[DONE]') continue;
      try {
        const event = JSON.parse(raw);
        const content = event.content ?? event.delta ?? event.choices?.[0]?.delta?.content ?? '';
        if (content) onChunk(content);
      } catch {
        onChunk(raw);
      }
    }
  }
}
