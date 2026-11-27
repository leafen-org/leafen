import { useEffect, useRef, type ReactNode } from "react";
import { foldViewSearchText, viewSearchTextRanges } from "@/lib/viewSearchText";

const NAME = "leafen-view-search";
const found = new Map<symbol, Range[]>();

type HighlightRegistry = { set(name: string, highlight: unknown): void; delete(name: string): void };
type HighlightConstructor = new (...ranges: Range[]) => unknown;

function publish() {
  const registry = (globalThis.CSS as unknown as { highlights?: HighlightRegistry } | undefined)?.highlights;
  const Highlight = (globalThis as unknown as { Highlight?: HighlightConstructor }).Highlight;
  if (!registry || !Highlight) return;
  const ranges = [...found.values()].flat();
  if (ranges.length) registry.set(NAME, new Highlight(...ranges));
  else registry.delete(NAME);
}

/** Where the search `query` falls in `root`'s text (viewSearchTextRanges), as ranges over its text nodes. */
function rangesIn(root: Element, query: string): Range[] {
  const nodes: Text[] = [];
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  for (let node = walker.nextNode(); node; node = walker.nextNode()) nodes.push(node as Text);
  const starts: number[] = [];
  let text = "";
  for (const node of nodes) { starts.push(text.length); text += node.data; }
  const at = (offset: number) => {
    let index = starts.length - 1;
    while (index > 0 && starts[index] > offset) index -= 1;
    return { node: nodes[index], offset: offset - starts[index] };
  };
  return viewSearchTextRanges(text, query).map(({ start: from, end: to }) => {
    const start = at(from), end = at(to - 1);
    const range = document.createRange();
    range.setStart(start.node, start.offset);
    range.setEnd(end.node, end.offset + 1);
    return range;
  });
}

/**
 * A view's search paints what it found in the rows' titles, as Notion's
 * (seen 2026-11-03: rgba(255, 205, 56, 0.4) under the matched letters),
 * without touching the titles themselves; the letters as written, accents
 * aside as the search finds them (viewSearchTextRanges, seen 2026-11-16).
 */
export function ViewSearchHighlight({ query, children }: { query: string; children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const root = ref.current;
    const key = Symbol(NAME);
    const needle = query.trim();
    if (!root || !foldViewSearchText(needle) || typeof MutationObserver === "undefined") return undefined;
    let frame = 0;
    const collect = () => {
      frame = 0;
      found.set(key, [...root.querySelectorAll("[data-search-text]")].flatMap((element) => rangesIn(element, needle)));
      publish();
    };
    const observer = new MutationObserver(() => { if (!frame) frame = window.requestAnimationFrame(collect); });
    observer.observe(root, { childList: true, subtree: true, characterData: true });
    collect();
    return () => {
      observer.disconnect();
      if (frame) window.cancelAnimationFrame(frame);
      found.delete(key);
      publish();
    };
  }, [query]);
  return <div ref={ref} style={{ display: "contents" }}>{children}</div>;
}
