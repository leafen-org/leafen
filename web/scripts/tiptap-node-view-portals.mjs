import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { createHash } from 'node:crypto';

// A bounded workaround for Tiptap's node view portals (3.31.4, unchanged in
// 3.35.0): each node view added
// copied every portal into a new object, so pasting a 100-column, 100-row
// table (10,000 cell views) took 13 seconds and 600 rows nine minutes
// (2026-10-14). The portals are kept in a map and their object is built once
// a batch, when React reads it; the order and the batched notice are as
// upstream's. Fail closed on a dependency change; remove when upstream does.
const upstream = fileURLToPath(new URL('../node_modules/@tiptap/react/package.json', import.meta.url));
const signature = '4f0ef399ad6b240f767b64c87e9681f2e936f8d65784b3c8135642845102d622';
const replacement = `function createContentComponent() {
\tconst subscribers = /* @__PURE__ */ new Set();
\tconst portals = /* @__PURE__ */ new Map();
\tlet renderers = {};
\tlet changed = false;
\tlet isNotificationQueued = false;
\tconst notifySubscribers = () => {
\t\tchanged = true;
\t\tif (isNotificationQueued || !subscribers.size) return;
\t\tisNotificationQueued = true;
\t\tqueueMicrotask(() => {
\t\t\tisNotificationQueued = false;
\t\t\tsubscribers.forEach((subscriber) => subscriber());
\t\t});
\t};
\tconst snapshot = () => {
\t\tif (changed) {
\t\t\trenderers = Object.fromEntries(portals);
\t\t\tchanged = false;
\t\t}
\t\treturn renderers;
\t};
\treturn {
\t\tsubscribe(callback) {
\t\t\tsubscribers.add(callback);
\t\t\treturn () => {
\t\t\t\tsubscribers.delete(callback);
\t\t\t};
\t\t},
\t\tgetSnapshot: snapshot,
\t\tgetServerSnapshot: snapshot,
\t\tsetRenderer(id, renderer) {
\t\t\tportals.set(id, ReactDOM.createPortal(renderer.reactElement, renderer.element, id));
\t\t\tnotifySubscribers();
\t\t},
\t\tremoveRenderer(id) {
\t\t\tportals.delete(id);
\t\t\tnotifySubscribers();
\t\t}
\t};
}
`;
export function tiptapNodeViewPortals() {
  return {
    name: 'leafen-tiptap-node-view-portals', enforce: 'pre',
    transform(code, id) {
      if (!id.split('?')[0].replaceAll('\\', '/').endsWith('/@tiptap/react/dist/index.js')) return;
      if (JSON.parse(readFileSync(upstream, 'utf8')).version !== '3.35.0') throw new Error('Review the Tiptap node view portals for this version.');
      const start = code.indexOf('function createContentComponent() {');
      const end = code.indexOf('var PureEditorContent = class', start);
      if (start < 0 || end < 0 || createHash('sha256').update(code.slice(start, end)).digest('hex') !== signature)
        throw new Error('Tiptap node view portals changed; review before building.');
      return { code: code.slice(0, start) + replacement + code.slice(end), map: null };
    },
  };
}
