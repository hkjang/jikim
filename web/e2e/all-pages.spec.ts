import { expect, test, type Page } from '@playwright/test';
import path from 'node:path';

const username = process.env.E2E_ADMIN || 'admin';
const password = process.env.E2E_ADMIN_PASSWORD || 'ChangeMe-Only-For-E2E!';
const configuredVersion = process.env.E2E_VERSION || process.env.VITE_APP_VERSION;
const expectedVersion = configuredVersion
  ? configuredVersion.startsWith('v') ? configuredVersion : `v${configuredVersion}`
  : undefined;

const pages = [
  ['dashboard', '/dashboard'], ['secrets', '/secrets'], ['secret-new', '/secrets/new'],
  ['dynamic-secrets', '/secrets/dynamic'], ['leases', '/secrets/leases'], ['rotation', '/secrets/rotation'], ['risk', '/secrets/risk'],
  ['transit', '/encryption/transit'], ['keys', '/encryption/keys'], ['crypto-operations', '/encryption/operations'],
  ['certificates', '/certificates'], ['pki', '/certificates/pki'], ['certificate-expiration', '/certificates/expiration'],
  ['authentication', '/access/authentication'], ['policies', '/access/policies'], ['identities', '/access/identities'], ['tokens', '/access/tokens'],
  ['approvals', '/approvals'],
  ['applications', '/applications'], ['environments', '/applications/environments'], ['dependencies', '/applications/dependencies'],
  ['audit', '/audit'], ['events', '/audit/events'], ['security-events', '/audit/security'],
  ['ai', '/ai'], ['api-explorer', '/api-explorer'], ['cluster', '/infrastructure/cluster'], ['storage', '/infrastructure/storage'],
  ['infrastructure-security', '/infrastructure/security'], ['admin-settings', '/admin/settings'], ['admin-users', '/admin/users'], ['namespaces', '/admin/namespaces'],
  ['profile', '/personal/profile'], ['personal-keys', '/personal/keys'], ['preferences', '/personal/preferences'], ['guide', '/guide'],
  ['forbidden', '/forbidden'], ['not-found', '/not-found-demo'],
] as const;

async function login(page: Page) {
  await page.goto('/login');
  await page.getByLabel('아이디').fill(username);
  await page.getByLabel('비밀번호').fill(password);
  await page.getByRole('button', { name: '로그인', exact: true }).click();
  await expect(page).toHaveURL(/\/dashboard/);
}

async function expectServiceVersion(page: Page) {
  const versionText = expectedVersion ? `jikim ${expectedVersion}` : /^jikim v\d+\.\d+\.\d+(?:[-+][\w.-]+)?$/;
  await expect(page.getByText(versionText)).toBeVisible();
}

async function waitForRenderedPage(page: Page) {
  await page.waitForLoadState('networkidle');
  await expect(page.locator('main .mantine-Loader-root')).toHaveCount(0);
}

// 감사 로그는 조회한 이벤트를 모두 한 화면에 그리므로 fullPage 캡처가 계속 길어집니다.
// 가이드와 갤러리에서 읽을 수 있는 그림이 되도록 이 화면만 viewport 높이로 찍습니다.
const viewportOnlyShots = new Set(['audit']);

// 갤러리 계약은 "빈 화면을 찍지 않는다"이므로 목록 화면에 보일 비운영 fixture를 먼저 만듭니다.
// 값은 모두 가짜이며 example.internal 주소와 데모 계정만 사용합니다.
const fixtureApplications = [
  { name: 'payment-api', owner: '결제개발팀', criticality: 'Critical', environment: 'PRD', repository: 'https://git.example.internal/demo/payment-api', description: '데모 결제 API' },
  { name: 'portal-web', owner: '포털개발팀', criticality: 'High', environment: 'STG', repository: 'https://git.example.internal/demo/portal-web', description: '데모 고객 포털' },
  { name: 'batch-worker', owner: '플랫폼개발팀', criticality: 'Normal', environment: 'DEV', repository: '', description: '데모 야간 배치' },
] as const;

const fixtureSecrets = [
  { path: 'payment/production/database', description: '데모 결제 DB 자격 증명', tags: ['database', 'demo'], metadata: { application: 'payment-api', environment: 'PRD', owner: '결제개발팀' }, data: { username: 'demo-payment', password: 'masked-in-captures' } },
  { path: 'portal/staging/oauth-client', description: '데모 포털 OAuth Client', tags: ['oauth', 'demo'], metadata: { application: 'portal-web', environment: 'STG', owner: '포털개발팀' }, data: { client_id: 'demo-portal', client_secret: 'masked-in-captures' } },
  { path: 'batch/dev/object-storage', description: '데모 배치 오브젝트 스토리지 키', tags: ['storage', 'demo'], metadata: { application: 'batch-worker', environment: 'DEV', owner: '플랫폼개발팀' }, data: { access_key: 'demo-batch', secret_key: 'masked-in-captures' } },
] as const;

const fixturePolicies = [
  { name: '결제 운영 조회', description: '데모 결제 운영 경로 조회 전용', rules: { paths: [{ path: 'payment/production/*', capabilities: ['read', 'list'] }] } },
  { name: '포털 스테이징 편집', description: '데모 포털 스테이징 경로 편집', rules: { paths: [{ path: 'portal/staging/*', capabilities: ['create', 'read', 'update', 'list'] }] } },
] as const;

const fixtureUsers = [
  { username: 'demo-manager', display_name: '검토 담당자(데모)', email: 'demo-manager@example.internal', role: 'manager', password: 'Demo-Only-Password-2026!' },
  { username: 'demo-auditor', display_name: '감사 담당자(데모)', email: 'demo-auditor@example.internal', role: 'auditor', password: 'Demo-Only-Password-2026!' },
] as const;

const fixtureKeys = [
  { name: 'payment-card-token', algorithm: 'AES-256-GCM' },
  { name: 'portal-session', algorithm: 'AES-256-GCM' },
] as const;

// 캡처 대상은 매 실행마다 새로 만드는 E2E 전용 컨테이너와 전용 PostgreSQL이며
// 전역 설정은 건드리지 않습니다. 되돌릴 상태를 만들지 않는 것이 복원보다 안전합니다.
async function seedCaptureFixtures(page: Page): Promise<string | undefined> {
  const created = async (url: string, data: unknown) => {
    const response = await page.request.post(url, { data });
    expect(response.ok(), `${url} fixture 생성 실패: ${response.status()}`).toBe(true);
    const body = await response.json();
    return body?.data ?? body;
  };
  for (const application of fixtureApplications) await created('/api/v1/applications', application);
  for (const secret of fixtureSecrets) await created('/api/v1/secrets', secret);
  for (const policy of fixturePolicies) await created('/api/v1/policies', policy);
  for (const user of fixtureUsers) await created('/api/v1/users', user);
  for (const key of fixtureKeys) await created('/api/v1/keys', key);
  await created('/api/v1/tokens', { name: '데모 배치 연동 토큰', ttl_seconds: 3600 });
  const detail = await created('/api/v1/secrets', {
    path: 'e2e/demo/database', description: 'E2E 화면 캡처용', tags: ['fixture'],
    metadata: { application: 'e2e-app', environment: 'DEV', owner: 'E2E' },
    data: { username: 'demo-user', password: 'masked-in-captures' },
  });
  return detail?.id;
}

test('로그인 화면', async ({ page }) => {
  await page.goto('/login');
  await expect(page.getByRole('heading', { name: '안전하게 로그인하세요' })).toBeVisible();
  await expectServiceVersion(page);
  await page.screenshot({ path: path.resolve('../docs/screenshots/login.png'), fullPage: true });
});

test('OIDC 콜백의 안전한 실패 안내', async ({ page }) => {
  await page.goto('/oidc/callback');
  await expect(page.getByRole('heading', { name: 'SSO 로그인을 완료하지 못했습니다.' })).toBeVisible();
  await page.screenshot({ path: path.resolve('../docs/screenshots/oidc-callback.png'), fullPage: true });
});

test('모든 관리 화면이 직접 URL과 새로 고침에서 복원된다', async ({ page }) => {
  test.setTimeout(90_000);
  const pageErrors: string[] = [];
  const consoleErrors: string[] = [];
  const failedRequests: string[] = [];
  const httpErrors: string[] = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));
  page.on('console', (message) => {
    if (message.type() === 'error') consoleErrors.push(`${message.text()} (${message.location().url || 'unknown'})`);
  });
  page.on('requestfailed', (request) => failedRequests.push(`${request.method()} ${request.url()}: ${request.failure()?.errorText || 'unknown'}`));
  page.on('response', (response) => {
    if (response.status() >= 400) httpErrors.push(`${response.status()} ${response.request().method()} ${response.url()}`);
  });
  await login(page);
  await expect(page.locator('nav').getByText('검토·승인', { exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: '프로필 메뉴' }).click();
  await expectServiceVersion(page);
  await page.screenshot({ path: path.resolve('../docs/screenshots/profile-menu.png'), fullPage: true, animations: 'disabled' });
  const secretID = await seedCaptureFixtures(page);
  const routes = secretID ? [...pages, ['secret-detail', `/secrets/${secretID}`] as const] : pages;
  for (const [name, route] of routes) {
    await page.goto(route);
    await page.reload();
    await expect.poll(() => page.evaluate(() => window.location.pathname)).toBe(route);
    await expect(page.locator('main')).toBeVisible();
    await waitForRenderedPage(page);
    await expect(page.locator('body')).not.toContainText('Unexpected Application Error');
    if (name !== 'forbidden' && name !== 'not-found') {
      await expect(page.locator('main')).not.toContainText(/불러오지 못했습니다|요청을 처리하지 못했습니다|페이지를 표시하지 못했습니다/);
    }
    await page.evaluate(() => document.fonts.ready);
    await page.screenshot({ path: path.resolve(`../docs/screenshots/${name}.png`), fullPage: !viewportOnlyShots.has(name), animations: 'disabled' });
  }
  expect(pageErrors, `브라우저 pageerror: ${pageErrors.join(' | ')}`).toEqual([]);
  expect(consoleErrors, `브라우저 console.error: ${consoleErrors.join(' | ')}`).toEqual([]);
  expect(failedRequests, `실패한 네트워크 요청: ${failedRequests.join(' | ')}`).toEqual([]);
  expect(httpErrors, `HTTP 오류 응답: ${httpErrors.join(' | ')}`).toEqual([]);
});

test('관리자 프로필 메뉴의 스크롤이 작은 화면 안에서 유지된다', async ({ page }) => {
  await page.setViewportSize({ width: 900, height: 360 });
  await login(page);
  await page.getByRole('button', { name: '프로필 메뉴' }).click();
  const menu = page.locator('.profile-menu-scroll');
  await expect(menu).toBeVisible();
  const layout = await menu.evaluate((element) => {
    const style = getComputedStyle(element);
    const rect = element.getBoundingClientRect();
    return {
      overflowY: style.overflowY,
      scrollbarWidth: style.scrollbarWidth,
      scrollable: element.scrollHeight > element.clientHeight,
      top: rect.top,
      bottom: rect.bottom,
      viewportHeight: window.innerHeight,
    };
  });
  expect(layout.overflowY).toBe('auto');
  expect(layout.scrollbarWidth).toBe('thin');
  expect(layout.scrollable).toBe(true);
  expect(layout.top).toBeGreaterThanOrEqual(0);
  expect(layout.bottom).toBeLessThanOrEqual(layout.viewportHeight);
});
