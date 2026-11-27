#!/usr/bin/env node

import { createHash } from 'node:crypto';
import {
  existsSync,
  mkdirSync,
  readFileSync,
  readdirSync,
  unlinkSync,
  writeFileSync,
} from 'node:fs';
import { basename, dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { relativeSlashPath } from '../../scripts/source-paths.mjs';

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const fontRoot = join(webRoot, 'public', 'fonts', 'leafen');
const licenseRoot = join(webRoot, 'public', 'fonts', 'licenses');
const manifestPath = join(webRoot, 'src', 'app', 'typography-fonts.json');
const generatedCssPath = join(webRoot, 'src', 'app', 'typography.generated.css');
// Japanese's and Chinese's faces are their own sheets, declared once the page
// is in that language (src/lib/localeFontFaces.ts): typography.css draws CJK
// text in the language's own Noto face, Korean's for every other language.
const localeSheets = {
  ja: ['ja', 'serif-ja'],
  'zh-Hans': ['zh-Hans', 'serif-zh-Hans'],
  'zh-Hant': ['zh-Hant', 'serif-zh-Hant'],
};
const localeSheetPath = (locale) => join(webRoot, 'src', 'app', `typography.${locale}.generated.css`);
const cssUserAgent =
  'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) ' +
  'AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36';
const mode = process.argv[2];

const families = [
  {
    id: 'inter',
    face: 'Leafen Inter',
    upstreamFace: 'Inter',
    requestClass: 'shared-on-demand',
    // Its italic too, which Notion's system face draws (seen 2026-10-30).
    cssUrl: 'https://fonts.googleapis.com/css2?family=Inter:ital,opsz,wght@0,14..32,100..900;1,14..32,100..900&display=swap',
  },
  {
    id: 'ko',
    face: 'Leafen Noto Sans KR',
    upstreamFace: 'Noto Sans KR',
    requestClass: 'locale-on-demand',
    cssUrl: 'https://fonts.googleapis.com/css2?family=Noto+Sans+KR:wght@100..900&display=swap',
  },
  {
    id: 'ja',
    face: 'Leafen Noto Sans JP',
    upstreamFace: 'Noto Sans JP',
    requestClass: 'locale-on-demand',
    cssUrl: 'https://fonts.googleapis.com/css2?family=Noto+Sans+JP:wght@100..900&display=swap',
  },
  {
    id: 'zh-Hans',
    face: 'Leafen Noto Sans SC',
    upstreamFace: 'Noto Sans SC',
    requestClass: 'locale-on-demand',
    cssUrl: 'https://fonts.googleapis.com/css2?family=Noto+Sans+SC:wght@100..900&display=swap',
  },
  {
    id: 'zh-Hant',
    face: 'Leafen Noto Sans TC',
    upstreamFace: 'Noto Sans TC',
    requestClass: 'locale-on-demand',
    cssUrl: 'https://fonts.googleapis.com/css2?family=Noto+Sans+TC:wght@100..900&display=swap',
  },
  // The serif page's and the code's faces (font decision B, 2026-10-30):
  // Source Serif 4 for Notion's Lyon Text, which is not open, with Noto Serif
  // for each CJK script, and iA Writer Mono, Notion's own code face.
  {
    id: 'serif',
    face: 'Leafen Source Serif 4',
    upstreamFace: 'Source Serif 4',
    requestClass: 'shared-on-demand',
    cssUrl: 'https://fonts.googleapis.com/css2?family=Source+Serif+4:ital,opsz,wght@0,8..60,200..900;1,8..60,200..900&display=swap',
  },
  {
    id: 'serif-ko',
    face: 'Leafen Noto Serif KR',
    upstreamFace: 'Noto Serif KR',
    requestClass: 'locale-on-demand',
    cssUrl: 'https://fonts.googleapis.com/css2?family=Noto+Serif+KR:wght@200..900&display=swap',
  },
  {
    id: 'serif-ja',
    face: 'Leafen Noto Serif JP',
    upstreamFace: 'Noto Serif JP',
    requestClass: 'locale-on-demand',
    cssUrl: 'https://fonts.googleapis.com/css2?family=Noto+Serif+JP:wght@200..900&display=swap',
  },
  {
    id: 'serif-zh-Hans',
    face: 'Leafen Noto Serif SC',
    upstreamFace: 'Noto Serif SC',
    requestClass: 'locale-on-demand',
    cssUrl: 'https://fonts.googleapis.com/css2?family=Noto+Serif+SC:wght@200..900&display=swap',
  },
  {
    id: 'serif-zh-Hant',
    face: 'Leafen Noto Serif TC',
    upstreamFace: 'Noto Serif TC',
    requestClass: 'locale-on-demand',
    cssUrl: 'https://fonts.googleapis.com/css2?family=Noto+Serif+TC:wght@200..900&display=swap',
  },
  {
    id: 'mono',
    face: 'Leafen iA Writer Mono',
    upstreamFace: 'iA Writer Mono S',
    requestClass: 'shared-on-demand',
    // The release's web fonts, pinned to a commit; one file per style.
    files: [
      ['normal', '400', 'iAWriterMonoS-Regular.woff2'],
      ['normal', '700', 'iAWriterMonoS-Bold.woff2'],
      ['italic', '400', 'iAWriterMonoS-Italic.woff2'],
      ['italic', '700', 'iAWriterMonoS-BoldItalic.woff2'],
    ].map(([style, weight, name]) => ({
      style, weight,
      upstreamUrl: `https://raw.githubusercontent.com/iaolo/iA-Fonts/c6588670c71e9ac628acc27b72cde4bf12726b7f/iA%20Writer%20Mono/Webfonts/${name}`,
    })),
  },
];

const licenses = [
  {
    id: 'inter',
    upstreamUrl: 'https://github.com/rsms/inter/blob/master/LICENSE.txt',
    upstreamRawUrl: 'https://raw.githubusercontent.com/rsms/inter/master/LICENSE.txt',
    localPath: 'public/fonts/licenses/Inter-OFL-1.1.txt',
  },
  {
    id: 'noto-cjk',
    upstreamUrl: 'https://github.com/notofonts/noto-cjk/blob/main/Sans/README-third_party.md',
    upstreamRawUrl: 'https://raw.githubusercontent.com/google/fonts/main/ofl/notosanskr/OFL.txt',
    localPath: 'public/fonts/licenses/Noto-Sans-CJK-OFL-1.1.txt',
  },
  {
    id: 'source-serif',
    upstreamUrl: 'https://github.com/adobe-fonts/source-serif/blob/release/LICENSE.md',
    upstreamRawUrl: 'https://raw.githubusercontent.com/google/fonts/main/ofl/sourceserif4/OFL.txt',
    localPath: 'public/fonts/licenses/Source-Serif-4-OFL-1.1.txt',
  },
  {
    id: 'noto-serif-cjk',
    upstreamUrl: 'https://github.com/notofonts/noto-cjk/blob/main/Serif/LICENSE',
    upstreamRawUrl: 'https://raw.githubusercontent.com/google/fonts/main/ofl/notoserifkr/OFL.txt',
    localPath: 'public/fonts/licenses/Noto-Serif-CJK-OFL-1.1.txt',
  },
  {
    id: 'ia-writer',
    upstreamUrl: 'https://github.com/iaolo/iA-Fonts/blob/c6588670c71e9ac628acc27b72cde4bf12726b7f/iA%20Writer%20Mono/LICENSE.md',
    upstreamRawUrl: 'https://raw.githubusercontent.com/iaolo/iA-Fonts/c6588670c71e9ac628acc27b72cde4bf12726b7f/iA%20Writer%20Mono/LICENSE.md',
    localPath: 'public/fonts/licenses/iA-Writer-OFL-1.1.md',
  },
];

function sha256(bytes) {
  return createHash('sha256').update(bytes).digest('hex');
}

function relativePath(path) {
  return relativeSlashPath(webRoot, path);
}

function parseFaces(css, family) {
  const blocks = [...css.matchAll(/@font-face\s*\{([\s\S]*?)\}/gu)];
  return blocks.map((match) => {
    const body = match[1];
    const upstreamFace = /font-family:\s*'([^']+)'/u.exec(body)?.[1];
    const style = /font-style:\s*([^;]+);/u.exec(body)?.[1]?.trim();
    const weight = /font-weight:\s*([^;]+);/u.exec(body)?.[1]?.trim();
    const upstreamUrl = /src:\s*url\((https:\/\/[^)]+)\)\s*format\('woff2'\)/u.exec(body)?.[1];
    const unicodeRange = /unicode-range:\s*([^;]+);/u.exec(body)?.[1]?.trim();
    if (
      upstreamFace !== family.upstreamFace ||
      !style ||
      !weight ||
      !upstreamUrl ||
      !unicodeRange
    ) {
      throw new Error(`Unexpected ${family.id} Google Fonts CSS block.`);
    }
    return { style, weight, unicodeRange, upstreamUrl };
  });
}

// Arrows and mathematical operators (≥ ≤ ≠ → ⇒ ∞ …) that Inter lacks are drawn by the device's own face, as Notion's
// system face draws them (seen 2026-11-18 in Chrome at 1440, dpr 1: ≥ ≤ ≠ 8.67 wide at 14px, → 12.53), not full-width
// by the CJK sans faces (14): those faces do not claim them, so they fall through to --font-ui's system tail
// (typography.css). The serif CJK faces keep theirs, behind Source Serif 4.
const systemSymbolRanges = [[0x2190, 0x22ff]];
const systemSymbolFaces = new Set(['ko', 'ja', 'zh-Hans', 'zh-Hant']);

// A unicode-range without the system symbols, or '' when nothing is left.
function withoutSystemSymbols(unicodeRange) {
  const kept = [];
  for (const part of unicodeRange.split(',').map((piece) => piece.trim()).filter(Boolean)) {
    const match = /^U\+([0-9a-f]+)(?:-([0-9a-f]+))?$/iu.exec(part);
    if (!match) throw new Error(`Unexpected unicode-range part: ${part}`);
    let pieces = [[parseInt(match[1], 16), parseInt(match[2] ?? match[1], 16)]];
    for (const [low, high] of systemSymbolRanges) {
      pieces = pieces.flatMap(([start, end]) => [[start, Math.min(end, low - 1)], [Math.max(start, high + 1), end]].filter(([a, b]) => a <= b));
    }
    for (const [start, end] of pieces) kept.push(start === end ? `U+${start.toString(16)}` : `U+${start.toString(16)}-${end.toString(16)}`);
  }
  return kept.join(', ');
}

function renderCss(manifest, ids) {
  const blocks = manifest.families.filter((family) => ids.includes(family.id)).flatMap((family) => family.assets.flatMap((asset) => {
    const unicodeRange = asset.unicodeRange && systemSymbolFaces.has(family.id) ? withoutSystemSymbols(asset.unicodeRange) : asset.unicodeRange;
    if (asset.unicodeRange && !unicodeRange) return [];
    return [[
      '@font-face {',
      `  font-family: "${family.face}";`,
      `  font-style: ${asset.style};`,
      `  font-weight: ${asset.weight};`,
      '  font-display: swap;',
      `  src: url("/${asset.localPath.replace(/^public\//u, '')}") format("woff2");`,
      ...(unicodeRange ? [`  unicode-range: ${unicodeRange};`] : []),
      '}',
    ].join('\n')];
  }));
  return `${[
    '/* Generated by scripts/vendor-typography-fonts.mjs. Do not edit by hand. */',
    ...blocks,
  ].join('\n\n')}\n`;
}

// Each generated sheet with its CSS: the one every page has, then each language's.
function renderedSheets(manifest) {
  const inLocale = new Set(Object.values(localeSheets).flat());
  return [
    [generatedCssPath, renderCss(manifest, manifest.families.map(({ id }) => id).filter((id) => !inLocale.has(id)))],
    ...Object.entries(localeSheets).map(([locale, ids]) => [localeSheetPath(locale), renderCss(manifest, ids)]),
  ];
}

async function fetchBytes(url, headers = {}) {
  const response = await fetch(url, { headers });
  if (!response.ok) throw new Error(`Failed ${response.status} for ${url}`);
  return Buffer.from(await response.arrayBuffer());
}

async function mapConcurrent(values, limit, mapper) {
  const results = new Array(values.length);
  let next = 0;
  await Promise.all(Array.from({ length: Math.min(limit, values.length) }, async () => {
    while (next < values.length) {
      const index = next;
      next += 1;
      results[index] = await mapper(values[index], index);
    }
  }));
  return results;
}

// Fetches the families named after --update, or those not vendored yet, and
// keeps the others as pinned, so adding a family doesn't redownload the rest.
async function update(named) {
  const previous = existsSync(manifestPath) ? JSON.parse(readFileSync(manifestPath, 'utf8')) : { families: [], licenses: [] };
  const resolvedFamilies = [];
  for (const family of families) {
    const kept = previous.families.find((candidate) => candidate.id === family.id);
    if (kept && !named.includes(family.id)) {
      resolvedFamilies.push({ ...kept, assets: kept.assets.map((asset) => ({ ...asset, bytes: readFileSync(join(webRoot, asset.localPath)) })) });
      continue;
    }
    const cssBytes = family.files ? Buffer.from(JSON.stringify(family.files)) : await fetchBytes(family.cssUrl, { 'user-agent': cssUserAgent });
    const parsedAssets = family.files ?? parseFaces(cssBytes.toString('utf8'), family);
    const assets = await mapConcurrent(parsedAssets, 20, async (asset) => {
      const bytes = await fetchBytes(asset.upstreamUrl, { 'user-agent': cssUserAgent });
      const hash = sha256(bytes);
      const localPath = `public/fonts/leafen/${family.id.toLowerCase()}.${hash.slice(0, 12)}.woff2`;
      return { ...asset, bytes, sha256: hash, byteLength: bytes.byteLength, localPath };
    });
    resolvedFamilies.push({
      ...family,
      upstreamCssSha256: sha256(cssBytes),
      fileCount: assets.length,
      totalBytes: assets.reduce((sum, asset) => sum + asset.byteLength, 0),
      assets,
    });
  }

  const resolvedLicenses = await mapConcurrent(licenses, 2, async (license) => {
    const kept = previous.licenses.find((candidate) => candidate.id === license.id);
    const bytes = kept && !named.includes(license.id)
      ? readFileSync(join(webRoot, kept.localPath)) : await fetchBytes(license.upstreamRawUrl);
    return { ...license, bytes, sha256: sha256(bytes), byteLength: bytes.byteLength };
  });
  const manifest = {
    schemaVersion: 1,
    generated: true,
    cssUserAgent,
    licenses: resolvedLicenses.map(({ bytes, ...license }) => license),
    families: resolvedFamilies.map((family) => ({
      ...family,
      assets: family.assets.map(({ bytes, ...asset }) => asset),
    })),
  };

  mkdirSync(fontRoot, { recursive: true });
  mkdirSync(licenseRoot, { recursive: true });
  const expectedFontNames = new Set();
  for (const family of resolvedFamilies) {
    for (const asset of family.assets) {
      expectedFontNames.add(asset.localPath.split('/').at(-1));
      writeFileSync(join(webRoot, asset.localPath), asset.bytes);
    }
  }
  for (const name of readdirSync(fontRoot)) {
    if (name.endsWith('.woff2') && !expectedFontNames.has(name)) unlinkSync(join(fontRoot, name));
  }
  for (const license of resolvedLicenses) {
    writeFileSync(join(webRoot, license.localPath), license.bytes);
  }
  writeFileSync(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`);
  for (const [path, css] of renderedSheets(manifest)) writeFileSync(path, css);
  console.log(
    `Vendored ${manifest.families.reduce((sum, family) => sum + family.fileCount, 0)} ` +
      `font files / ${manifest.families.reduce((sum, family) => sum + family.totalBytes, 0)} bytes.`,
  );
}

function check() {
  if (!existsSync(manifestPath)) throw new Error(`Missing ${relativePath(manifestPath)}.`);
  const manifest = JSON.parse(readFileSync(manifestPath, 'utf8'));
  if (JSON.stringify(manifest.families.map(({ id }) => id)) !== JSON.stringify(families.map(({ id }) => id))) {
    throw new Error('Typography manifest family order is not canonical.');
  }
  for (const [path, css] of renderedSheets(manifest)) {
    if (!existsSync(path) || readFileSync(path, 'utf8') !== css) throw new Error(`${relativePath(path)} is stale.`);
  }
  const expectedFontNames = new Set();
  for (const family of manifest.families) {
    if (family.fileCount !== family.assets.length) throw new Error(`${family.id} file count is stale.`);
    let totalBytes = 0;
    for (const asset of family.assets) {
      const path = join(webRoot, asset.localPath);
      const bytes = readFileSync(path);
      expectedFontNames.add(basename(path));
      totalBytes += bytes.byteLength;
      if (bytes.byteLength !== asset.byteLength || sha256(bytes) !== asset.sha256) {
        throw new Error(`${asset.localPath} does not match its pinned bytes.`);
      }
    }
    if (totalBytes !== family.totalBytes) throw new Error(`${family.id} total bytes are stale.`);
  }
  const actualFontNames = readdirSync(fontRoot).filter((name) => name.endsWith('.woff2'));
  if (
    actualFontNames.length !== expectedFontNames.size ||
    actualFontNames.some((name) => !expectedFontNames.has(name))
  ) {
    throw new Error('public/fonts/leafen contains untracked or missing WOFF2 assets.');
  }
  for (const license of manifest.licenses) {
    const bytes = readFileSync(join(webRoot, license.localPath));
    if (bytes.byteLength !== license.byteLength || sha256(bytes) !== license.sha256) {
      throw new Error(`${license.localPath} does not match its pinned license bytes.`);
    }
  }
  console.log(
    `Typography assets verified: ${expectedFontNames.size} files / ` +
      `${manifest.families.reduce((sum, family) => sum + family.totalBytes, 0)} bytes.`,
  );
}

// Writes the sheets again from the pinned manifest, downloading nothing.
function render() {
  const manifest = JSON.parse(readFileSync(manifestPath, 'utf8'));
  for (const [path, css] of renderedSheets(manifest)) writeFileSync(path, css);
  console.log(`Rendered ${renderedSheets(manifest).length} typography sheets.`);
}

if (mode === '--update') await update(process.argv.slice(3));
else if (mode === '--check') check();
else if (mode === '--render') render();
else throw new Error('Usage: node scripts/vendor-typography-fonts.mjs --update|--check|--render');
