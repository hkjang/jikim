// 조용한 SSO(prompt=none)를 언제 시도할지 정하는 규칙.
//
// prompt=none 은 화면을 그리지 않고 제공자의 기존 세션으로만 답한다. 세션이 없으면
// error=login_required 로 돌아오는데 그때 다시 시도하면 브라우저가 제공자와 앱 사이를
// 끝없이 오간다. 그래서 세 겹으로 막는다: (1) sessionStorage 에 한 탭 세션에 한 번만
// 시도했다는 표시, (2) 스스로 로그아웃했으면 억제, (3) 콜백이 거절을 받으면
// /login?sso=none 으로 보내 주소에도 표시를 남긴다.

// localStorage 가 아니라 sessionStorage 다: 새 탭에서는 다시 시도하고, 거절당한 뒤
// 새로고침하면 다시 시도하지 않는 것이 맞다.
const ATTEMPTED_KEY = 'jikim.sso.silentAttempted';
const SIGNED_OUT_KEY = 'jikim.sso.signedOut';

// 콜백·오류·로그인 화면은 가장 흔한 루프의 출처라 시도하지 않는다. API·MCP·probe·
// 프록시 경로는 SPA 라우트가 아니지만 브라우저 이동에만 해당한다는 규칙을 코드에도 남긴다.
const EXCLUDED_PATHS = ['/login', '/oidc/callback', '/forbidden'];
const EXCLUDED_PREFIXES = ['/api/', '/v1/', '/mcp', '/healthz', '/readyz', '/momento/'];

export interface SilentSsoSettings { oidc_enabled?: boolean; oidc_auto_login?: boolean }

function readFlag(key: string): boolean {
  try {
    return window.sessionStorage.getItem(key) === 'true';
  } catch {
    // 사생활 보호 모드나 사이트 데이터가 막힌 브라우저는 예외를 던진다. 그것을 "아직 안
    // 했다"로 읽으면 바로 루프가 되므로 "이미 시도했다"로 친다 — 막히는 쪽으로 실패한다.
    return true;
  }
}

function writeFlag(key: string, value: boolean) {
  try {
    if (value) window.sessionStorage.setItem(key, 'true');
    else window.sessionStorage.removeItem(key);
  } catch {
    /* 저장하지 못해도 readFlag 가 이미 막히는 쪽으로 답한다 */
  }
}

/** 사용자가 스스로 로그아웃했음을 기록한다. 로그아웃 직후 조용히 다시 로그인시키면 로그아웃이 고장 난 것처럼 보인다. */
export function markSignedOut() {
  writeFlag(SIGNED_OUT_KEY, true);
  writeFlag(ATTEMPTED_KEY, true);
}

/** 세션이 다시 생기면 억제를 푼다. */
export function clearSilentSsoState() {
  writeFlag(SIGNED_OUT_KEY, false);
  writeFlag(ATTEMPTED_KEY, false);
}

export function silentSsoAttempted(): boolean {
  return readFlag(ATTEMPTED_KEY);
}

/** '/' 로 시작하고 '//' 로 시작하지 않는 같은 오리진 경로만 돌아갈 자리로 받는다. */
export function safeReturnTo(value: string | null | undefined): string {
  if (!value || !value.startsWith('/') || value.startsWith('//') || value.startsWith('/\\')) return '/dashboard';
  const path = value.split(/[?#]/)[0];
  if (EXCLUDED_PATHS.includes(path)) return '/dashboard';
  return value;
}

export function silentSsoExcludedPath(pathname: string): boolean {
  return EXCLUDED_PATHS.includes(pathname) || EXCLUDED_PREFIXES.some((prefix) => pathname.startsWith(prefix));
}

/**
 * 로그인 화면을 보여 주기 전에 조용한 로그인을 시도할지 정한다.
 *
 * 한 브라우징 세션에 한 번을 넘겨서는 안 된다. prompt=none 은 곧바로 답하거나
 * login_required 로 돌아오는데, 페이지를 열 때마다 다시 시도하면 루프가 된다.
 */
export function shouldAttemptSilentSso(settings: SilentSsoSettings | undefined, location: { pathname: string; search: string }): boolean {
  if (!settings?.oidc_enabled || !settings?.oidc_auto_login) return false;
  if (silentSsoExcludedPath(location.pathname)) return false;
  if (readFlag(SIGNED_OUT_KEY)) return false;
  if (readFlag(ATTEMPTED_KEY)) return false;
  // 콜백은 제공자에 세션이 없을 때 이 표시를 붙인다. sessionStorage 가 그 사이 지워졌더라도
  // 거절을 기억한다.
  const sso = new URLSearchParams(location.search).get('sso');
  if (sso === 'none' || sso === 'error') return false;
  return true;
}

/** 조용한 시도를 위해 브라우저를 제공자로 보낸다. 숨은 iframe 이 아니라 최상위 이동이다. */
export function beginSilentSso(returnTo: string) {
  writeFlag(ATTEMPTED_KEY, true);
  window.location.assign(`/api/v1/oidc/login?prompt=none&return_to=${encodeURIComponent(safeReturnTo(returnTo))}`);
}
