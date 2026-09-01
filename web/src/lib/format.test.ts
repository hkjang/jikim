import { describe, expect, it } from 'vitest';
import { authSourceLabel, formatNumber, riskColor, statusLabel } from './format';

describe('표시 형식', () => {
  it('위험 점수 구간을 일관되게 분류한다', () => {
    expect(riskColor(0)).toBe('teal');
    expect(riskColor(31)).toBe('yellow');
    expect(riskColor(61)).toBe('orange');
    expect(riskColor(81)).toBe('red');
  });

  it('상태를 한국어로 표시한다', () => {
    expect(statusLabel('active')).toBe('활성');
    expect(statusLabel('rejected')).toBe('반려');
    expect(statusLabel('custom')).toBe('custom');
  });

  it('한국어 숫자 형식을 적용한다', () => {
    expect(formatNumber(12345)).toContain('12,345');
  });

  it('인증 원본을 누락 없이 표시한다', () => {
    expect(authSourceLabel('oidc')).toBe('Keycloak OIDC');
    expect(authSourceLabel('local')).toBe('로컬');
    expect(authSourceLabel('ldap')).toBe('ldap');
    expect(authSourceLabel()).toBe('확인 필요');
  });
});
