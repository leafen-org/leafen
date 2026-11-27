import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { createHash } from 'node:crypto';

// A bounded workaround for Tiptap 3.35.0's deleteSelection, which since 3.32
// keeps the marks every deleted letter had for the next typed words: a bold
// word chosen and deleted with Backspace left the letters typed after it
// bold, in a page's lines and in a field (a comment, a cell, a title) alike,
// where Notion's are plain (typed 2026-11-26 in Chrome, a page's line at its
// start and in its middle and a comment being written; Leafen on Tiptap
// 3.31.4 typed them plain too). The deletion is upstream's; only the marks
// it carried over are left out, so typing takes the marks of where it is,
// as before. The build's copy only: the dev server's prebundled one stays
// upstream's. Fail closed on a dependency change; remove when upstream
// stops carrying the marks over.
const upstream = fileURLToPath(new URL('../node_modules/@tiptap/core/package.json', import.meta.url));
const head = 'const deleteSelection = () => ({ state, dispatch }) => {\n';
const tail = '\n//#endregion';
const signature = '57d96de615bf041b9857969e61cd7385d78e603b39d8975b70439324ebc099ae';
const edits = [
  '\t\tconst marks = state.selection instanceof TextSelection ? state.selection.$from.marksAcross(state.selection.$to) : null;\n',
  '\t\tif (marks) tr.ensureMarks(marks);\n',
];
export function tiptapDeleteSelectionMarks() {
  return {
    name: 'leafen-tiptap-delete-selection-marks', enforce: 'pre',
    transform(code, id) {
      if (!id.split('?')[0].replaceAll('\\', '/').endsWith('/@tiptap/core/dist/index.js')) return;
      if (JSON.parse(readFileSync(upstream, 'utf8')).version !== '3.35.0') throw new Error('Review the Tiptap deleteSelection marks for this version.');
      const start = code.indexOf(head);
      const end = code.indexOf(tail, start);
      if (start < 0 || end < 0 || createHash('sha256').update(code.slice(start, end)).digest('hex') !== signature)
        throw new Error('Tiptap deleteSelection changed; review before building.');
      let region = code.slice(start, end);
      for (const line of edits) {
        if (region.split(line).length !== 2) throw new Error('Tiptap deleteSelection moved; review before building.');
        region = region.replace(line, '');
      }
      return { code: code.slice(0, start) + region + code.slice(end), map: null };
    },
  };
}
