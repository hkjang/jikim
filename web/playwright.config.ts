import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  retries: 0,
  // 콘솔 reporter는 PLAYWRIGHT_REPORTER 로 바꿀 수 있다(e2e-docker.sh 는 성공 경로 출력을
  // 줄이려고 dot 을 넘긴다). 실패 상세는 어느 reporter든 그대로 나오고 HTML 보고서는 유지한다.
  reporter: [[process.env.PLAYWRIGHT_REPORTER || 'list'], ['html', { open: 'never' }]],
  use: {
    baseURL: process.env.E2E_BASE_URL || 'http://127.0.0.1:8080',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
    locale: 'ko-KR',
    timezoneId: 'Asia/Seoul',
  },
  projects: [
    { name: 'desktop-chromium', testIgnore: /mobile\.spec\.ts/, use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 1024 } } },
    { name: 'mobile-chromium', use: { ...devices['Pixel 7'], viewport: { width: 390, height: 844 } }, testMatch: /mobile\.spec\.ts/ },
  ],
});
