// web/ 의 npm 의존성 중 서로의 peerDependencies 로 버전이 묶인 묶음은 Dependabot 이
// 하나씩 올리면 npm ERESOLVE 로 업데이트 잡이 실패한다. 예를 들어 @mantine/core 만
// 9.7.2 로 올리면 8.3.18 로 고정된 @mantine/hooks 와 어긋나 의존성 해석이 끝나지
// 않는다. 그런 묶음이 .github/dependabot.yml 의 npm /web 항목에서 한 group 으로 함께
// 올라가는지 확인한다.
import { readFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const scriptDir = dirname(fileURLToPath(import.meta.url));
const projectDir = resolve(scriptDir, '..');
const failures = [];

// 이 저장소의 dependabot.yml 만 읽는 최소 YAML 해석기다. 매핑·목록·한 줄 스칼라만
// 다루고, 그 밖의 문법을 만나면 조용히 넘기지 않고 멈춘다.
function readLines(text) {
  const lines = [];
  for (const raw of text.split('\n')) {
    const withoutComment = raw.replace(/^\s*#.*$/, '').replace(/\s+#.*$/, '');
    if (withoutComment.trim() === '') continue;
    lines.push({ indent: withoutComment.length - withoutComment.trimStart().length, text: withoutComment.trim() });
  }
  return lines;
}

function parseScalar(text) {
  if (/^[[{&*|>]/.test(text)) throw new Error(`지원하지 않는 YAML 문법: ${text}`);
  const unquoted = text.replace(/^(['"])(.*)\1$/, '$2');
  if (unquoted === 'true' || unquoted === 'false') return unquoted === 'true';
  return unquoted;
}

function parseBlock(lines, start, indent) {
  if (lines[start].text.startsWith('- ')) return parseSequence(lines, start, indent);
  return parseMapping(lines, start, indent);
}

function parseSequence(lines, start, indent) {
  const items = [];
  let index = start;
  while (index < lines.length && lines[index].indent === indent && lines[index].text.startsWith('- ')) {
    const head = { indent: indent + 2, text: lines[index].text.slice(2).trim() };
    const body = [head];
    index += 1;
    while (index < lines.length && lines[index].indent >= indent + 2) {
      body.push(lines[index]);
      index += 1;
    }
    if (body.length === 1 && !/:(\s|$)/.test(head.text)) {
      items.push(parseScalar(head.text));
      continue;
    }
    const [value, consumed] = parseBlock(body, 0, indent + 2);
    if (consumed !== body.length) throw new Error(`해석할 수 없는 목록 항목: ${body[consumed].text}`);
    items.push(value);
  }
  return [items, index];
}

function parseMapping(lines, start, indent) {
  const mapping = {};
  let index = start;
  while (index < lines.length && lines[index].indent === indent) {
    const matched = /^([^:]+):\s*(.*)$/.exec(lines[index].text);
    if (!matched) throw new Error(`해석할 수 없는 줄: ${lines[index].text}`);
    const key = matched[1].trim();
    const inline = matched[2].trim();
    index += 1;
    if (inline !== '') {
      mapping[key] = parseScalar(inline);
      continue;
    }
    const nested = [];
    const childIndent = index < lines.length ? lines[index].indent : -1;
    const isChild = childIndent > indent || (childIndent === indent && lines[index].text.startsWith('- '));
    if (!isChild) {
      mapping[key] = null;
      continue;
    }
    while (index < lines.length && (lines[index].indent > indent || (lines[index].indent === indent && lines[index].text.startsWith('- ')))) {
      nested.push(lines[index]);
      index += 1;
    }
    const [value, consumed] = parseBlock(nested, 0, childIndent);
    if (consumed !== nested.length) throw new Error(`해석할 수 없는 중첩 블록: ${nested[consumed].text}`);
    mapping[key] = value;
  }
  return [mapping, index];
}

function parseYaml(text) {
  const lines = readLines(text);
  if (lines.length === 0) return {};
  const [value, consumed] = parseBlock(lines, 0, lines[0].indent);
  if (consumed !== lines.length) throw new Error(`해석할 수 없는 줄: ${lines[consumed].text}`);
  return value;
}

function parseVersion(text) {
  const matched = /^(\d+)(?:\.(\d+))?(?:\.(\d+))?/.exec(String(text).trim());
  if (!matched) return null;
  return { major: Number(matched[1]), minor: Number(matched[2] ?? 0), patch: Number(matched[3] ?? 0) };
}

function compareVersions(left, right) {
  return left.major - right.major || left.minor - right.minor || left.patch - right.patch;
}

// `^8.0.0 <9` 처럼 한 묶음으로 쓰인 비교자들에서 하한만 뽑는다. 상한(<, <=)은 버전을
// 올릴 때 따라 움직이지 않으므로 무시한다.
function lowerBound(alternative) {
  let bound = null;
  for (const token of alternative.trim().split(/\s+/)) {
    if (token === '' || token === '*' || token.startsWith('<')) continue;
    const candidate = parseVersion(token.replace(/^[\^~>=v]+/, ''));
    if (candidate === null) continue;
    if (bound === null || compareVersions(candidate, bound) > 0) bound = candidate;
  }
  return bound;
}

// peer 범위의 하한이 상대 패키지의 minor 까지 따라붙으면(정확한 고정, 또는 ^19.2.x
// 처럼) 상대가 새 minor·patch 를 내는 순간 범위도 같이 올라간다. 즉 두 패키지는 함께
// 올려야 해석된다. 하한이 major.0.0 이거나 더 아래 major 면 혼자 올려도 된다.
function tracksMinorOf(range, lockedVersion) {
  const locked = parseVersion(lockedVersion);
  if (locked === null) return false;
  let best = null;
  for (const alternative of String(range).split('||')) {
    const bound = lowerBound(alternative);
    if (bound === null) return false;
    if (compareVersions(bound, locked) <= 0 && (best === null || compareVersions(bound, best) > 0)) best = bound;
  }
  if (best === null) return false;
  return best.major === locked.major && best.minor === locked.minor;
}

function matchesPattern(pattern, name) {
  const escaped = String(pattern).replace(/[.+?^${}()|[\]\\]/g, '\\$&').replace(/\*/g, '.*');
  return new RegExp(`^${escaped}$`, 'i').test(name);
}

function groupCovers(group, name) {
  const appliesTo = group['applies-to'] ?? 'version-updates';
  if (appliesTo !== 'version-updates') return false;
  const patterns = group.patterns ?? [];
  const excluded = group['exclude-patterns'] ?? [];
  if (!Array.isArray(patterns) || !Array.isArray(excluded)) throw new Error('group 의 patterns 는 목록이어야 합니다.');
  if (!patterns.some((pattern) => matchesPattern(pattern, name))) return false;
  return !excluded.some((pattern) => matchesPattern(pattern, name));
}

const lock = JSON.parse(readFileSync(join(projectDir, 'web/package-lock.json'), 'utf8'));
const lockPackages = lock.packages ?? {};
const rootEntry = lockPackages[''] ?? {};
const directNames = [...new Set([...Object.keys(rootEntry.dependencies ?? {}), ...Object.keys(rootEntry.devDependencies ?? {})])];

const installed = new Map();
for (const name of directNames) {
  const meta = lockPackages[`node_modules/${name}`];
  if (meta === undefined) {
    failures.push(`락파일에 최상위 설치 항목이 없습니다: ${name}`);
    continue;
  }
  installed.set(name, meta);
}

// peer 범위가 서로의 minor 를 따라붙는 패키지를 한 묶음으로 모은다.
const parents = new Map(directNames.map((name) => [name, name]));
function find(name) {
  while (parents.get(name) !== name) name = parents.get(name);
  return name;
}
function union(left, right) {
  const [a, b] = [find(left), find(right)];
  if (a !== b) parents.set(a, b);
}
for (const [name, meta] of installed) {
  for (const [peer, range] of Object.entries(meta.peerDependencies ?? {})) {
    const peerMeta = installed.get(peer);
    if (peerMeta === undefined) continue;
    if (tracksMinorOf(range, peerMeta.version)) union(name, peer);
  }
}

const families = new Map();
for (const name of installed.keys()) {
  const key = find(name);
  if (!families.has(key)) families.set(key, []);
  families.get(key).push(name);
}

let groups = {};
try {
  const config = parseYaml(readFileSync(join(projectDir, '.github/dependabot.yml'), 'utf8'));
  const updates = config.updates ?? [];
  const entry = updates.find((item) => item['package-ecosystem'] === 'npm' && item.directory === '/web');
  if (entry === undefined) {
    failures.push('.github/dependabot.yml 에 npm /web 항목이 없습니다.');
  } else {
    groups = entry.groups ?? {};
  }
} catch (error) {
  failures.push(`.github/dependabot.yml 구문 오류: ${error.message}`);
}

const groupList = Object.entries(groups).map(([name, group]) => [name, group ?? {}]);
for (const members of [...families.values()].sort()) {
  if (members.length < 2) continue;
  const sorted = [...members].sort();
  let covering;
  try {
    covering = groupList.find(([, group]) => sorted.every((name) => groupCovers(group, name)));
  } catch (error) {
    failures.push(`.github/dependabot.yml 의 group 정의를 읽을 수 없습니다: ${error.message}`);
    break;
  }
  if (covering === undefined) {
    failures.push(
      `peerDependencies 로 버전이 묶인 묶음을 Dependabot 이 따로 올립니다: ${sorted.join(', ')}` +
        ' — .github/dependabot.yml 의 npm /web 항목에 이 묶음을 함께 올리는 group 이 필요합니다.',
    );
  }
}

if (failures.length > 0) {
  for (const failure of failures) process.stderr.write(`- ${failure}\n`);
  process.exit(1);
}

process.stdout.write('Dependabot npm 묶음 업데이트 계약 검증 완료\n');
