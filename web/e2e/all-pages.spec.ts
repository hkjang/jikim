import { expect, test, type Page } from '@playwright/test';
import path from 'node:path';

const username = process.env.E2E_ADMIN || 'admin';
const password = process.env.E2E_ADMIN_PASSWORD || 'ChangeMe-Only-For-E2E!';

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

test('로그인 화면', async ({ page }) => {
  await page.goto('/login');
  await expect(page.getByRole('heading', { name: '안전하게 로그인하세요' })).toBeVisible();
  await expect(page.getByText('jikim v0.1.0')).toBeVisible();
  await page.screenshot({ path: path.resolve('../docs/screenshots/login.png'), fullPage: true });
});

test('OIDC 콜백의 안전한 실패 안내', async ({ page }) => {
  await page.goto('/oidc/callback');
  await expect(page.getByRole('heading', { name: 'SSO 로그인을 완료하지 못했습니다.' })).toBeVisible();
  await page.screenshot({ path: path.resolve('../docs/screenshots/oidc-callback.png'), fullPage: true });
});

test('모든 관리 화면이 직접 URL과 새로 고침에서 복원된다', async ({ page }) => {
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
  await expect(page.getByText('jikim v0.1.0')).toBeVisible();
  await page.screenshot({ path: path.resolve('../docs/screenshots/profile-menu.png'), fullPage: true, animations: 'disabled' });
  const secretResponse = await page.request.post('/api/v1/secrets', {
    data: { path: 'e2e/demo/database', description: 'E2E 화면 캡처용', tags: ['fixture'], metadata: { application: 'e2e-app', environment: 'DEV', owner: 'E2E' }, data: { username: 'demo-user', password: 'masked-in-captures' } },
  });
  const secretBody = secretResponse.ok() ? await secretResponse.json() : {};
  const secretID = secretBody?.data?.id || secretBody?.id;
  const routes = secretID ? [...pages, ['secret-detail', `/secrets/${secretID}`] as const] : pages;
  for (const [name, route] of routes) {
    await page.goto(route);
    await page.reload();
    await expect(page.locator('main')).toBeVisible();
    await expect(page.locator('body')).not.toContainText('Unexpected Application Error');
    if (name !== 'forbidden' && name !== 'not-found') {
      await expect(page.locator('main')).not.toContainText(/불러오지 못했습니다|요청을 처리하지 못했습니다|페이지를 표시하지 못했습니다/);
    }
    await page.evaluate(() => document.fonts.ready);
    await page.screenshot({ path: path.resolve(`../docs/screenshots/${name}.png`), fullPage: true, animations: 'disabled' });
  }
  expect(pageErrors, `브라우저 pageerror: ${pageErrors.join(' | ')}`).toEqual([]);
  expect(consoleErrors, `브라우저 console.error: ${consoleErrors.join(' | ')}`).toEqual([]);
  expect(failedRequests, `실패한 네트워크 요청: ${failedRequests.join(' | ')}`).toEqual([]);
  expect(httpErrors, `HTTP 오류 응답: ${httpErrors.join(' | ')}`).toEqual([]);
});
