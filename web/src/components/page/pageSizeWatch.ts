/**
 * One ResizeObserver for the hooks that measure a page as its size changes,
 * so that all of them measure in one callback before any of them writes
 * (their writes held with holdStyleWrite, made once the callback is done).
 * Each with its own observer, the page's column and its discussions wrote a
 * variable on the page's document before the next one measured, which then
 * worked out every style under it again: some 100ms each time for a page
 * holding a table of 30 rows by 40 properties, twice for each page opened
 * (seen 2026-11-12). Its wide tables measure here too (usePageWideTable): as
 * a row's peek opened beside a page holding three tables, each table's
 * measure laid the page out again for the one before's variables (9, 20 and
 * then 68ms for a 120-row table's 9,700 elements, seen 2026-11-12). A hook
 * watching more than one element that changed together measures once.
 *
 * Unlike an observer of its own, watching an element already watched brings
 * no first call: a hook measures once itself as it starts.
 */
const watchers = new Map<Element, Set<() => void>>();
let observer: ResizeObserver | null = null;

export function watchPageSize(element: Element, measure: () => void): () => void {
  if (typeof ResizeObserver === "undefined") return () => {};
  observer ??= new ResizeObserver((entries) => {
    const measures = new Set<() => void>();
    for (const entry of entries) {
      for (const each of watchers.get(entry.target) ?? []) measures.add(each);
    }
    for (const each of measures) each();
  });
  let measures = watchers.get(element);
  if (!measures) {
    measures = new Set();
    watchers.set(element, measures);
    observer.observe(element);
  }
  measures.add(measure);
  return () => {
    const current = watchers.get(element);
    if (!current?.delete(measure) || current.size > 0) return;
    watchers.delete(element);
    observer?.unobserve(element);
  };
}
