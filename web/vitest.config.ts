import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';

// jsdom 30은 Node.js 22.22.2 이상을 요구한다. 그 아래에서는 undici의 webidl이
// Node 22.10에서 추가된 worker_threads.markAsUncloneable을 찾지 못해 테스트 파일이
// 시작조차 못 한 채 jsdom 내부에서 죽는다. npm은 lifecycle 스크립트의 PATH 앞에
// 상위 디렉터리의 node_modules/.bin을 모두 붙이므로 셸과 다른 Node가 들어올 수 있다.
// 그래서 셸이 아니라 테스트를 실제로 돌리는 이 프로세스에서 하한을 확인한다.
const NODE_FLOOR = [22, 22, 2];
const current = process.versions.node.split('.').map(Number);
const meetsFloor = ((): boolean => {
  for (let index = 0; index < NODE_FLOOR.length; index += 1) {
    const part = current[index] ?? 0;
    if (part !== NODE_FLOOR[index]) return part > NODE_FLOOR[index];
  }
  return true;
})();
if (!meetsFloor) {
  throw new Error(
    `프런트 테스트에는 Node.js ${NODE_FLOOR.join('.')} 이상이 필요합니다.`
    + ` 현재: v${process.versions.node} (${process.execPath}) — .nvmrc 참고`,
  );
}

export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    globals: true,
    pool: 'threads',
    maxWorkers: 1,
    exclude: ['e2e/**', 'node_modules/**', 'dist/**'],
    setupFiles: ['./src/test/setup.ts'],
    coverage: { reporter: ['text', 'html'], include: ['src/lib/**', 'src/components/**'] },
  },
});
