import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { createHash } from 'node:crypto';

// A bounded workaround for prosemirror-view 1.42.6: each DOM change in an
// editor (its observer's flush) and each document update on Chrome (the
// selection workaround in updateStateInner) read the browser's selection,
// and reading it lays out whatever the page has changed so far. An editor
// out of focus does nothing with what it reads there: the flush takes a
// selection only into an editor that has the focus, and the forced selection
// reset only redraws the selection of an editor that owns it. So an editable
// editor out of focus reads nothing there and is told "no selection", as an
// empty document's read would be; it reads the browser's selection when the
// focus comes to it, as before (2026-11-13: a page of three databases
// opening laid itself out inside one such flush, 110 of its 250 ms of forced
// layout; a peek opening on a long page, 36 ms of its click). The build's copy only: the dev server's prebundled one
// stays upstream's, as it costs time only. Fail closed on a dependency
// change; remove when upstream stops reading the selection of an editor out
// of focus.
const upstream = fileURLToPath(new URL('../node_modules/prosemirror-view/package.json', import.meta.url));
const unread = 'view.editable && !view.hasFocus() ? { focusNode: null, focusOffset: 0, anchorNode: null, anchorOffset: 0 } : ';
const edits = [
  {
    // DOMObserver.flush
    from: '        if (mutations.length)\n            this.queue = [];\n        let sel = view.domSelectionRange();\n',
    to: '        if (mutations.length)\n            this.queue = [];\n        let sel = ' + unread + 'view.domSelectionRange();\n',
  },
  {
    // EditorView.updateStateInner, Chrome's selection workaround
    from: '                let chromeKludge = chrome ? (this.trackWrites = this.domSelectionRange().focusNode) : null;\n',
    to: '                let chromeKludge = chrome && !(this.editable && !this.hasFocus()) ? (this.trackWrites = this.domSelectionRange().focusNode) : null;\n',
  },
];
// The two methods the edits sit in, as reviewed.
const methods = [
  ['    flush() {\n', '\n    registerMutation(mut, added) {', '75228fb0a468c335eca62b714020ac288d209a1638d7de401ac5d1f149c28f1e'],
  ['    updateStateInner(state, prevProps) {\n', '\n    scrollToSelection() {', '4fbe336200d170d5985b7e76792c013e4de8efe8672d21036f95c046bcd3095b'],
];
export function prosemirrorUnfocusedSelection() {
  return {
    name: 'leafen-prosemirror-unfocused-selection', enforce: 'pre',
    transform(code, id) {
      if (!id.split('?')[0].replaceAll('\\', '/').endsWith('/prosemirror-view/dist/index.js')) return;
      if (JSON.parse(readFileSync(upstream, 'utf8')).version !== '1.42.6') throw new Error('Review the ProseMirror selection read for this version.');
      for (const [head, tail, signature] of methods) {
        const start = code.indexOf(head);
        const end = code.indexOf(tail, start);
        if (start < 0 || end < 0 || createHash('sha256').update(code.slice(start, end)).digest('hex') !== signature)
          throw new Error('ProseMirror selection read changed; review before building.');
      }
      let out = code;
      for (const { from, to } of edits) {
        if (out.split(from).length !== 2) throw new Error('ProseMirror selection read moved; review before building.');
        out = out.replace(from, to);
      }
      return { code: out, map: null };
    },
  };
}
