// Writes sw-precache.json into a finished Vite build with two explicit graphs:
// bootAssets for fast service-worker installation, and assets for the complete
// product graph warmed after activation. The worker publishes a new offline
// shell only after the latter has been staged atomically. The build's own
// plugin (vite.config.ts) calls it for the folder the build wrote, so a build
// given another --outDir carries its list too.
import { readFileSync, writeFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { resolve } from 'node:path';

import { planOfflineFontFormatSelection } from './sw-precache-font-formats.mjs';

export function writeServiceWorkerPrecache(dist) {
  const html = readFileSync(resolve(dist, 'index.html'), 'utf8');
  const viteManifest = JSON.parse(readFileSync(resolve(dist, '.vite/manifest.json'), 'utf8'));
  const brandAssets = [
    '/favicon.ico', '/favicon-16.png', '/favicon-32.png',
    '/icon-48.png', '/icon-96.png', '/mark-128.png', '/icon-192.png', '/icon-512.png',
    '/icon-maskable-192.png', '/icon-maskable-512.png',
    '/apple-touch-icon.png', '/manifest.webmanifest',
  ];
  const bootAssets = new Set(
    [
      // The loading screen's mark is the page's own (page-own-parts.mjs).
      '/favicon.ico', '/manifest.webmanifest',
      ...[...html.matchAll(/(?:src|href)="(\/assets\/[^"]+)"/g)].map((match) => match[1]),
      // Synchronous root scripts (for example the CSP-safe pre-paint theme
      // initializer) are part of the app shell just as much as hashed modules.
      // Without precaching them, a cached navigation fails its first offline
      // reload even though the module graph itself is present.
      ...[...html.matchAll(/<script[^>]+src="(\/(?!\/)[^"]+)"/g)].map((match) => match[1]),
    ]
  );

  function includeManifestEntry(
    key,
    target,
    includeDynamic,
    seen = new Set(),
    excludedEntries = new Set(),
  ) {
    if (!key || seen.has(key) || excludedEntries.has(key)) return;
    seen.add(key);
    const entry = viteManifest[key];
    if (!entry) return;
    if (entry.file) target.add('/' + entry.file);
    for (const css of entry.css ?? []) target.add('/' + css);
    for (const asset of entry.assets ?? []) target.add('/' + asset);
    for (const imported of entry.imports ?? []) {
      includeManifestEntry(imported, target, includeDynamic, seen, excludedEntries);
    }
    if (includeDynamic) {
      for (const imported of entry.dynamicImports ?? []) {
        includeManifestEntry(imported, target, true, seen, excludedEntries);
      }
    }
  }

  const languageEntries = Object.keys(viteManifest).filter((key) =>
    /^src\/locales\/[^/]+\.ts$/.test(key)
  );
  if (languageEntries.length === 0) {
    throw new Error('No language catalog chunks were emitted; refusing an incomplete offline precache.');
  }
  const entryKey = Object.keys(viteManifest).find((key) => viteManifest[key]?.isEntry);
  if (!entryKey) throw new Error('No Vite application entry was emitted.');
  includeManifestEntry(entryKey, bootAssets, false);
  // Only precache the fallback (en) and primary product language (ko) as boot
  // assets. Other language chunks are lazy-loaded and runtime-cached on demand;
  // if one is not cached during an offline boot, i18next falls back to the
  // already-precached English catalog.
  const PRECACHE_LANGUAGES = new Set(['en', 'ko']);
  const bootLanguageEntries = languageEntries.filter((key) => {
    const code = key.replace(/^src\/locales\/([^/]+)\.ts$/, '$1');
    return PRECACHE_LANGUAGES.has(code);
  });
  for (const key of bootLanguageEntries) includeManifestEntry(key, bootAssets, false);

  // A worker starts controlling only after the first page load. Browser HTTP
  // cache is not an offline contract, so precache every reachable PRODUCT lazy
  // module (route chunks, Editor/DatabaseView, and nested feature chunks) instead
  // of assuming the first visit's uncontrolled requests remain available. Do not
  // let the entry module's import.meta.glob pull every optional language chunk
  // into that graph; those remain on-demand runtime-cache entries.
  const assets = new Set([...bootAssets, ...brandAssets]);
  // public typography fonts stay runtime-cached on demand. They are deliberately
  // outside Vite's module manifest so an offline warm never downloads every CJK
  // locale family; covered glyphs become durable after their same-origin use.
  const deferredLanguageEntries = new Set(
    languageEntries.filter((key) => !bootLanguageEntries.includes(key)),
  );
  includeManifestEntry(entryKey, assets, true, new Set(), deferredLanguageEntries);
  const fontFormatSelection = planOfflineFontFormatSelection(viteManifest, assets, bootAssets);
  for (const asset of fontFormatSelection.removedAssets) assets.delete(asset);

  const sortedAssets = ['/', ...Array.from(assets).sort()];
  const versionHash = createHash('sha256');
  for (const url of sortedAssets) {
    const file = url === '/' ? resolve(dist, 'index.html') : resolve(dist, url.slice(1));
    versionHash.update(url);
    versionHash.update('\0');
    versionHash.update(readFileSync(file));
    versionHash.update('\0');
  }

  const manifest = {
    version: versionHash.digest('hex').slice(0, 24),
    assets: sortedAssets,
    bootAssets: ['/', ...Array.from(bootAssets).sort()],
  };
  const workerPath = resolve(dist, 'sw.js');
  const workerSource = readFileSync(workerPath, 'utf8');
  const workerGenerationDeclaration =
    /const BUILD_GENERATION = \/\*__LEAFEN_SW_BUILD_GENERATION__\*\/ "[A-Za-z0-9_-]{1,64}";/g;
  const workerGenerationMatches = [...workerSource.matchAll(workerGenerationDeclaration)];
  if (workerGenerationMatches.length !== 1) {
    throw new Error(
      'Expected exactly one service-worker build-generation declaration; found ' +
        workerGenerationMatches.length + '.',
    );
  }
  writeFileSync(
    workerPath,
    workerSource.replace(
      workerGenerationDeclaration,
      'const BUILD_GENERATION = /*__LEAFEN_SW_BUILD_GENERATION__*/ ' +
        JSON.stringify(manifest.version) + ';',
    ),
  );
  writeFileSync(
    resolve(dist, 'sw-precache.json'),
    JSON.stringify(manifest, null, 2) + '\n'
  );
  const rawBytes = (urls) => urls.reduce((total, url) => {
    const file = url === '/' ? resolve(dist, 'index.html') : resolve(dist, url.slice(1));
    return total + readFileSync(file).byteLength;
  }, 0);
  const bootBytes = rawBytes(manifest.bootAssets);
  const totalBytes = rawBytes(manifest.assets);
  const removedFontBytes = rawBytes(fontFormatSelection.removedAssets);
  console.log(
    'sw-precache.json: install ' + manifest.bootAssets.length + ' entries / ' + bootBytes +
      ' raw bytes; background ' + (manifest.assets.length - manifest.bootAssets.length) +
      ' entries / ' + (totalBytes - bootBytes) + ' raw bytes; full ' + manifest.assets.length +
      ' entries / ' + totalBytes + ' raw bytes (' + bootLanguageEntries.length +
      ' boot language chunks; ' + fontFormatSelection.removedAssets.length +
      ' redundant font formats / ' + removedFontBytes + ' raw bytes omitted)'
  );
}
