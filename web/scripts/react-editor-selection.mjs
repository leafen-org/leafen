import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { createHash } from 'node:crypto';

// A bounded workaround for react-dom 19.3.1 (19.3 walks as 19.2.4 did):
// before each commit React keeps
// the focused element's selection as text offsets, and for a contenteditable
// it counts them by walking every node in it, to put the selection back if
// the commit takes the focus away. With the caret in a page's editor that
// walked the whole page at every commit (2026-11-14: a 600-block page, 11 ms
// of an Enter's commit, CPU ×2; again in each commit after it). For an
// editor's root (ProseMirror's, which keeps its own selection and puts it
// back as it takes the focus) the selection is kept as its two DOM points
// instead, and put back from them when they are still in it; everything else
// is React's own. The build's copy only: the dev server's prebundled one
// stays upstream's, as it costs time only. Fail closed on a dependency
// change; remove when upstream stops walking a contenteditable each commit.
const upstream = fileURLToPath(new URL('../node_modules/react-dom/package.json', import.meta.url));
const helpers = `function leafenEditorSelection(root) {
  var selection = (root.ownerDocument.defaultView || window).getSelection();
  return selection && selection.rangeCount && root.contains(selection.anchorNode) && root.contains(selection.focusNode)
    ? { editor: true, anchorNode: selection.anchorNode, anchorOffset: selection.anchorOffset, focusNode: selection.focusNode, focusOffset: selection.focusOffset }
    : null;
}
function leafenRestoreEditorSelection(root, saved) {
  if (!root.contains(saved.anchorNode) || !root.contains(saved.focusNode)) return;
  try {
    (root.ownerDocument.defaultView || window).getSelection().setBaseAndExtent(saved.anchorNode, saved.anchorOffset, saved.focusNode, saved.focusOffset);
  } catch (error) {}
}
`;
const edits = [
  {
    // commitBeforeMutationEffects: what React keeps of the focused element's selection
    from: 'function commitBeforeMutationEffects(root, firstChild, committedLanes) {\n  root = root.containerInfo;\n  eventsEnabled = _enabled;\n  root = getActiveElementDeep(root);\n  if (hasSelectionCapabilities(root)) {\n',
    to: helpers + 'function commitBeforeMutationEffects(root, firstChild, committedLanes) {\n  root = root.containerInfo;\n  eventsEnabled = _enabled;\n  root = getActiveElementDeep(root);\n  if (root && root.pmViewDesc) JSCompiler_temp = leafenEditorSelection(root);\n  else if (hasSelectionCapabilities(root)) {\n',
  },
  {
    // commitRoot's mutation phase: putting it back once the focus went elsewhere
    from: '          if (\n            null !== priorSelectionRange &&\n            hasSelectionCapabilities(priorFocusedElem)\n          ) {\n',
    to: '          if (null !== priorSelectionRange && priorSelectionRange.editor)\n            leafenRestoreEditorSelection(priorFocusedElem, priorSelectionRange);\n          else if (\n            null !== priorSelectionRange &&\n            hasSelectionCapabilities(priorFocusedElem)\n          ) {\n',
  },
];
// The two places the edits sit in, as reviewed.
const places = [
  ['function commitBeforeMutationEffects(root, firstChild, committedLanes) {\n', '\n  _enabled = !1;\n', '1bbba696824aa71b2498ba9f8f890e3b2fa2b838425316bca432c4200a99153f'],
  ['        lanes = selectionInformation;\n', '\n          doc = [];\n', '1751dfb86949d879a2ab26751098cefa17595e16b28fda1a5dc5fce9eb9e4191'],
];
export function reactEditorSelection() {
  return {
    name: 'leafen-react-editor-selection', enforce: 'pre',
    transform(code, id) {
      if (!id.split('?')[0].replaceAll('\\', '/').endsWith('/react-dom/cjs/react-dom-client.production.js')) return;
      if (JSON.parse(readFileSync(upstream, 'utf8')).version !== '19.3.1') throw new Error('Review the React editor selection for this version.');
      for (const [head, tail, signature] of places) {
        const start = code.indexOf(head);
        const end = code.indexOf(tail, start);
        if (start < 0 || end < 0 || createHash('sha256').update(code.slice(start, end)).digest('hex') !== signature)
          throw new Error('React selection keeping changed; review before building.');
      }
      let out = code;
      for (const { from, to } of edits) {
        if (out.split(from).length !== 2) throw new Error('React selection keeping moved; review before building.');
        out = out.replace(from, to);
      }
      return { code: out, map: null };
    },
  };
}
