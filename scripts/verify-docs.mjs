import { existsSync, readFileSync, statSync } from 'node:fs';
import { dirname, extname, join, normalize, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const scriptDir = dirname(fileURLToPath(import.meta.url));
const projectDir = resolve(scriptDir, '..');
const docsDir = join(projectDir, 'docs');
const failures = [];

const requiredFiles = [
  'index.html',
  '404.html',
  'robots.txt',
  'sitemap.xml',
  'llms.txt',
  'site.webmanifest',
  'assets/styles.css',
  'assets/app.js',
  'assets/logo.svg',
  'assets/favicon.svg',
  'assets/og-image.png',
  'guides/index.html',
  'guides/admin-guide.md',
  'guides/user-guide.md',
  'guides/api-guide.md',
  'guides/offline-install.md',
  'guides/security.md',
  'guides/compatibility.md',
  'screenshots/manifest.json',
];

for (const relative of requiredFiles) {
  const target = join(docsDir, relative);
  if (!existsSync(target) || !statSync(target).isFile() || statSync(target).size === 0) {
    failures.push(`필수 문서가 없거나 비어 있습니다: docs/${relative}`);
  }
}

const index = readFileSync(join(docsDir, 'index.html'), 'utf8');
for (const marker of [
  '<meta name="description"',
  '<link rel="canonical"',
  'property="og:title"',
  'property="og:description"',
  'property="og:image"',
  'name="twitter:card"',
  'type="application/ld+json"',
]) {
  if (!index.includes(marker)) failures.push(`홍보 페이지 SEO 표식이 없습니다: ${marker}`);
}

const jsonLdMatches = [...index.matchAll(/<script\s+type="application\/ld\+json">([\s\S]*?)<\/script>/g)];
if (jsonLdMatches.length === 0) {
  failures.push('JSON-LD 블록이 없습니다.');
} else {
  for (const match of jsonLdMatches) {
    try {
      const value = JSON.parse(match[1]);
      const serialized = JSON.stringify(value);
      if (!serialized.includes('SoftwareApplication')) failures.push('JSON-LD에 SoftwareApplication이 없습니다.');
      if (!serialized.includes('FAQPage')) failures.push('JSON-LD에 FAQPage가 없습니다.');
    } catch (error) {
      failures.push(`JSON-LD 구문 오류: ${error.message}`);
    }
  }
}

for (const htmlRelative of ['index.html', '404.html', 'guides/index.html']) {
  const htmlPath = join(docsDir, htmlRelative);
  const html = readFileSync(htmlPath, 'utf8');
  const attributes = [...html.matchAll(/\b(?:href|src)="([^"]+)"/g)].map((match) => match[1]);
  for (const reference of attributes) {
    if (/^(?:https?:|mailto:|tel:|data:|#)/.test(reference)) continue;
    const withoutFragment = reference.split('#')[0].split('?')[0];
    if (!withoutFragment) continue;
    let target = normalize(join(dirname(htmlPath), withoutFragment));
    if (existsSync(target) && statSync(target).isDirectory()) target = join(target, 'index.html');
    if (!existsSync(target)) failures.push(`깨진 HTML 참조: docs/${htmlRelative} -> ${reference}`);
  }
}

try {
  JSON.parse(readFileSync(join(docsDir, 'site.webmanifest'), 'utf8'));
} catch (error) {
  failures.push(`site.webmanifest 구문 오류: ${error.message}`);
}

try {
  const manifest = JSON.parse(readFileSync(join(docsDir, 'screenshots/manifest.json'), 'utf8'));
  const ids = new Set();
  const files = new Set();
  for (const shot of manifest.shots ?? []) {
    if (!shot.id || !shot.route || !shot.file) failures.push('스크린샷 항목에는 id, route, file이 필요합니다.');
    if (ids.has(shot.id)) failures.push(`중복 스크린샷 id: ${shot.id}`);
    if (files.has(shot.file)) failures.push(`중복 스크린샷 파일: ${shot.file}`);
    ids.add(shot.id);
    files.add(shot.file);
    if (manifest.status === 'complete' && shot.required && !existsSync(join(docsDir, 'screenshots', shot.file))) {
      failures.push(`완료 매니페스트의 필수 캡처가 없습니다: ${shot.file}`);
    }
    if (manifest.status === 'complete' && shot.mobile_required) {
      const mobileFile = shot.file.replace(/(\.[^.]+)$/, '-mobile$1');
      if (!existsSync(join(docsDir, 'screenshots', mobileFile))) failures.push(`완료 매니페스트의 모바일 캡처가 없습니다: ${mobileFile}`);
    }
  }
  if (!['capture-required', 'complete'].includes(manifest.status)) failures.push(`알 수 없는 스크린샷 상태: ${manifest.status}`);
} catch (error) {
  failures.push(`screenshots/manifest.json 구문 오류: ${error.message}`);
}

if (extname(join(docsDir, 'index.html')) !== '.html') failures.push('홍보 페이지 확장자가 올바르지 않습니다.');

if (failures.length > 0) {
  for (const failure of failures) process.stderr.write(`- ${failure}\n`);
  process.exit(1);
}

process.stdout.write('문서·SEO·갤러리 계약 검증 완료\n');
