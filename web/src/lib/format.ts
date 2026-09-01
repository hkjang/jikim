export const APP_VERSION = import.meta.env.VITE_APP_VERSION || 'v0.1.0';

export function formatDate(value?: string) {
  if (!value) return '—';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat('ko-KR', { dateStyle: 'medium', timeStyle: 'short', timeZone: 'Asia/Seoul' }).format(date);
}

export function formatNumber(value?: number) {
  return new Intl.NumberFormat('ko-KR').format(value ?? 0);
}

export function riskColor(score = 0) {
  if (score >= 81) return 'red';
  if (score >= 61) return 'orange';
  if (score >= 31) return 'yellow';
  return 'teal';
}

export function statusLabel(value?: string) {
  const labels: Record<string, string> = {
    active: '활성', enabled: '활성', disabled: '비활성', pending: '대기', approved: '승인',
    rejected: '반려', success: '성공', failed: '실패', healthy: '정상', warning: '주의',
    critical: '심각', draft: '초안', revoked: '폐기', quarantined: '격리', retired: '이전 버전',
  };
  return labels[(value || '').toLowerCase()] || value || '—';
}
