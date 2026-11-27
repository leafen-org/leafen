import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { createHash } from 'node:crypto';

// A bounded workaround for Tiptap's layout read (3.31.4, unchanged in 3.35.0)
// on every React contentDOM
// attachment, and its putting the selection back at each one: it is put back
// once, after the commit that moved them. No input, DOM move or history is
// forked. Fail closed on a dependency change; remove when upstream batches
// this read and the restoration.
const upstream = fileURLToPath(new URL('../node_modules/@tiptap/react/package.json', import.meta.url));
const helper = fileURLToPath(new URL('../src/lib/tiptapDomSelection.ts', import.meta.url));
const signature = '8c3805824104c17083ded3c1c3d4e1e1a59f37583672d13dc3d9ef1299d8b8de';
export function tiptapSelectionCache() {
  return {
    name: 'leafen-tiptap-dom-selection-cache', enforce: 'pre',
    config() { return { optimizeDeps: { exclude: ['@tiptap/react'], include: ['@tiptap/react > use-sync-external-store/shim/index.js', '@tiptap/react > use-sync-external-store/shim/with-selector.js', '@tiptap/react > fast-equals'] } }; },
    transform(code, id) {
      if (!id.split('?')[0].replaceAll('\\', '/').endsWith('/@tiptap/react/dist/index.js')) return;
      if (JSON.parse(readFileSync(upstream, 'utf8')).version !== '3.35.0') throw new Error('Review the Tiptap selection cache for this version.');
      const start = code.indexOf('function captureDOMSelection(element) {');
      const end = code.indexOf('\n//#endregion', start);
      const original = code.slice(start, end);
      if (start < 0 || end < 0 || createHash('sha256').update(original).digest('hex') !== signature)
        throw new Error('Tiptap selection capture changed; review before building.');
      // Put back once, after the commit that moved it (restoreNativeDomSelection).
      const replacement = 'function captureDOMSelection(element) {\n\tconst saved = nativeDomSelectionSnapshot(element);\n\tif (!saved) return null;\n'
        + '\tif (!element.contains(saved.anchorNode) || !element.contains(saved.focusNode)) return null;\n'
        + '\treturn () => restoreNativeDomSelection(saved);\n}';
      return { code: `import { nativeDomSelectionSnapshot, restoreNativeDomSelection } from ${JSON.stringify(helper)};\n`
        + code.slice(0, start) + replacement + code.slice(end), map: null };
    },
  };
}
