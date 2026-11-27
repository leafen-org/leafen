/**
 * Page find reads most of a page from what it has drawn. A part that draws
 * only some of its content (a long table draws the rows near the view) is
 * found from its data instead: it registers here, and the find bar takes its
 * matches in its place in the page, reveals the one it moves to, and marks
 * those that are drawn.
 */
export type PageFindSourceMatch = {
  /** Draws the match's place and brings it into view. */
  reveal(): void;
  /** The match in what is drawn, or null while it is not drawn. */
  range(): Range | null;
};

export type PageFindSource = {
  /** The matches of `query` in its content, in order. */
  find(query: string): readonly PageFindSourceMatch[];
};

export const PAGE_FIND_SOURCE_ATTRIBUTE = "data-page-find-source";
const sources = new WeakMap<Element, PageFindSource>();

export function registerPageFindSource(element: HTMLElement, source: PageFindSource) {
  sources.set(element, source);
  element.setAttribute(PAGE_FIND_SOURCE_ATTRIBUTE, "");
  return () => {
    if (sources.get(element) !== source) return;
    sources.delete(element);
    element.removeAttribute(PAGE_FIND_SOURCE_ATTRIBUTE);
  };
}

export function pageFindSourceOf(element: Element) {
  return sources.get(element);
}

/** The searchable text nodes under `root`, with each one's offset in their
 * joined text. */
function textNodesWithOffsets(root: Element) {
  const nodes: { node: Text; start: number }[] = [];
  let length = 0;
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    const parent = node.parentElement;
    if (!parent || parent.closest('button, input, select, textarea, [aria-hidden="true"]')) continue;
    nodes.push({ node: node as Text, start: length });
    length += node.nodeValue?.length ?? 0;
  }
  return nodes;
}

/** The range of `[start, end)` in the joined text of `root`'s text nodes. */
export function rangeInDrawnText(root: Element, start: number, end: number): Range | null {
  const nodes = textNodesWithOffsets(root);
  const last = (test: (entry: { start: number }) => boolean) => [...nodes].reverse().find(test);
  const from = last((entry) => entry.start <= start);
  const to = last((entry) => entry.start < end);
  if (!from || !to) return null;
  const range = document.createRange();
  range.setStart(from.node, Math.min(start - from.start, from.node.length));
  range.setEnd(to.node, Math.min(end - to.start, to.node.length));
  return range;
}

/** The joined text of `root`'s searchable text nodes. */
export function drawnText(root: Element) {
  return textNodesWithOffsets(root).map(({ node }) => node.nodeValue ?? "").join("");
}
