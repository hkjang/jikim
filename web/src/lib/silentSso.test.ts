import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { beginSilentSso, clearSilentSsoState, markSignedOut, safeReturnTo, shouldAttemptSilentSso, silentSsoAttempted } from './silentSso';

const enabled = { oidc_enabled: true, oidc_auto_login: true };
const dashboard = { pathname: '/dashboard', search: '' };

function installSessionStorage(storage: Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>) {
  Object.defineProperty(window, 'sessionStorage', { configurable: true, value: storage });
}

describe('조용한 SSO 규칙', () => {
  const data = new Map<string, string>();
  beforeEach(() => {
    data.clear();
    installSessionStorage({
      getItem: (key) => data.get(key) ?? null,
      setItem: (key, value) => { data.set(key, String(value)); },
      removeItem: (key) => { data.delete(key); },
    });
  });
  afterEach(() => { vi.restoreAllMocks(); });

  it('auto_login 이 꺼져 있으면 시도하지 않는다', () => {
    expect(shouldAttemptSilentSso({ oidc_enabled: true, oidc_auto_login: false }, dashboard)).toBe(false);
    expect(shouldAttemptSilentSso({ oidc_enabled: false, oidc_auto_login: true }, dashboard)).toBe(false);
    expect(shouldAttemptSilentSso(undefined, dashboard)).toBe(false);
    expect(shouldAttemptSilentSso(enabled, dashboard)).toBe(true);
  });

  it('콜백·로그인·오류·API 경로에서는 시도하지 않는다', () => {
    for (const pathname of ['/login', '/oidc/callback', '/forbidden', '/api/v1/session', '/v1/sys/health', '/mcp', '/healthz', '/readyz', '/momento/tracker.js']) {
      expect(shouldAttemptSilentSso(enabled, { pathname, search: '' }), pathname).toBe(false);
    }
    expect(shouldAttemptSilentSso(enabled, { pathname: '/secrets/abc', search: '?tab=versions' })).toBe(true);
  });

  it('거절 표시가 붙은 주소에서는 시도하지 않는다', () => {
    expect(shouldAttemptSilentSso(enabled, { pathname: '/dashboard', search: '?sso=none' })).toBe(false);
    expect(shouldAttemptSilentSso(enabled, { pathname: '/dashboard', search: '?sso=error' })).toBe(false);
  });

  it('한 탭 세션에 한 번만 시도한다', () => {
    const assign = vi.fn();
    Object.defineProperty(window, 'location', { configurable: true, value: { ...window.location, assign } });
    expect(silentSsoAttempted()).toBe(false);
    beginSilentSso('/secrets/abc?tab=versions');
    expect(assign).toHaveBeenCalledWith('/api/v1/oidc/login?prompt=none&return_to=%2Fsecrets%2Fabc%3Ftab%3Dversions');
    expect(silentSsoAttempted()).toBe(true);
    expect(shouldAttemptSilentSso(enabled, dashboard)).toBe(false);
  });

  it('로그아웃한 뒤에는 시도하지 않고 다시 로그인하면 억제가 풀린다', () => {
    markSignedOut();
    expect(shouldAttemptSilentSso(enabled, dashboard)).toBe(false);
    clearSilentSsoState();
    expect(shouldAttemptSilentSso(enabled, dashboard)).toBe(true);
  });

  it('저장소를 읽지 못하면 이미 시도한 것으로 친다', () => {
    installSessionStorage({
      getItem: () => { throw new DOMException('blocked', 'SecurityError'); },
      setItem: () => { throw new DOMException('blocked', 'SecurityError'); },
      removeItem: () => { throw new DOMException('blocked', 'SecurityError'); },
    });
    expect(silentSsoAttempted()).toBe(true);
    expect(shouldAttemptSilentSso(enabled, dashboard)).toBe(false);
    expect(() => markSignedOut()).not.toThrow();
    expect(() => clearSilentSsoState()).not.toThrow();
  });

  it('돌아갈 자리는 같은 오리진 경로만 받는다', () => {
    expect(safeReturnTo('/secrets/abc?tab=versions')).toBe('/secrets/abc?tab=versions');
    for (const value of [null, '', 'dashboard', '//evil.example/x', '/\\evil.example', 'https://evil.example/', '/login', '/login?sso=none', '/oidc/callback?code=x']) {
      expect(safeReturnTo(value), String(value)).toBe('/dashboard');
    }
  });
});
