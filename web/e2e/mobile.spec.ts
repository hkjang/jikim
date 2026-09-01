import { expect, test, type Page } from '@playwright/test';
import path from 'node:path';

const username = process.env.E2E_ADMIN || 'admin';
const password = process.env.E2E_ADMIN_PASSWORD || 'ChangeMe-Only-For-E2E!';

async function waitForRenderedPage(page: Page) {
  await page.waitForLoadState('networkidle');
  await expect(page.locator('main .mantine-Loader-root')).toHaveCount(0);
}

test('모바일 로그인 화면이 가로 스크롤 없이 표시된다', async ({ page }) => {
  await page.goto('/login');
  await expect(page.getByRole('heading', { name: '안전하게 로그인하세요' })).toBeVisible();
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth);
  expect(overflow).toBe(false);
  await page.screenshot({ path: path.resolve('../docs/screenshots/login-mobile.png'), fullPage: true });
});

test('주요 모바일 관리 화면', async ({ page }) => {
  await page.goto('/login');
  await page.getByLabel('아이디').fill(username);
  await page.getByLabel('비밀번호').fill(password);
  await page.getByRole('button', { name: '로그인', exact: true }).click();
  await expect(page).toHaveURL(/\/dashboard/);
  for (const [name, route] of [
    ['dashboard-mobile', '/dashboard'], ['secrets-mobile', '/secrets'],
    ['personal-keys-mobile', '/personal/keys'], ['admin-settings-mobile', '/admin/settings'],
  ] as const) {
    await page.goto(route);
    await expect(page.locator('main')).toBeVisible();
    await waitForRenderedPage(page);
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth);
    expect(overflow, `${route}에서 가로 스크롤이 없어야 합니다.`).toBe(false);
    await page.screenshot({ path: path.resolve(`../docs/screenshots/${name}.png`), fullPage: true, animations: 'disabled' });
  }
});
