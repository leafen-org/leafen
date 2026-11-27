import { compareGraphOrder, type ContentGraph, type GraphBlock } from "@/contentGraph";

/**
 * A page as Notion's presentation mode shows it (help "Presentation mode",
 * seen 2026-10-12): its title and icon first, then a slide for each part of
 * the page between its top-level dividers, with the empty blocks at a part's
 * ends left out and a part with nothing else skipped. The open slide rides in
 * the page's address as `?present=N` (0 is the title), so its link opens the
 * page there.
 */

export const PRESENT_PARAM = "present";
export const PRESENT_EVENT = "leafen:present";

export type PageSlide = { rootIds: readonly string[] };

export type PresentRequest = { pageId: string; blockId?: string };

/** Whether a block holds nothing a slide would show: an empty paragraph. */
function emptyBlock(block: GraphBlock, text: (block: GraphBlock) => string) {
  return block.type === "paragraph" && text(block).trim() === "";
}

/** The page's parts between its top-level dividers, as slides after the title's. */
export function pageSlides(graph: ContentGraph, text: (block: GraphBlock) => string): PageSlide[] {
  const roots = graph.blocks.filter((block) => block.parentId === null).sort(compareGraphOrder);
  const parts: GraphBlock[][] = [[]];
  for (const block of roots) {
    if (block.type === "divider") parts.push([]);
    else parts[parts.length - 1].push(block);
  }
  const slides: PageSlide[] = [];
  for (const part of parts) {
    let start = 0;
    let end = part.length;
    while (start < end && emptyBlock(part[start], text)) start += 1;
    while (end > start && emptyBlock(part[end - 1], text)) end -= 1;
    if (end > start) slides.push({ rootIds: part.slice(start, end).map((block) => block.id) });
  }
  return slides;
}

/**
 * The graph a slide draws: its top-level blocks and every block below any
 * top-level block, so a synced copy still finds its source elsewhere on the
 * page, while only the slide's own blocks are reached from the top.
 */
export function slideGraph(graph: ContentGraph, slide: PageSlide): ContentGraph {
  const roots = new Set(slide.rootIds);
  return { ...graph, blocks: graph.blocks.filter((block) => block.parentId !== null || roots.has(block.id)) };
}

/** The deck's place (title 0) of the slide holding a block, or 0 for one outside every slide. */
export function slideIndexOfBlock(graph: ContentGraph, slides: readonly PageSlide[], blockId: string | null | undefined) {
  if (!blockId) return 0;
  const byId = new Map(graph.blocks.map((block) => [block.id, block]));
  let root = byId.get(blockId);
  // Up to the top-level block, however deep; each block once.
  const seen = new Set<string>();
  while (root?.parentId && !seen.has(root.id)) {
    seen.add(root.id);
    root = byId.get(root.parentId);
  }
  if (!root) return 0;
  const index = slides.findIndex((slide) => slide.rootIds.includes(root!.id));
  return index < 0 ? 0 : index + 1;
}

/** The slide the address names, or null when the page isn't being presented. */
export function presentedSlide(search: URLSearchParams) {
  const value = search.get(PRESENT_PARAM);
  if (value === null || !/^\d{1,4}$/u.test(value)) return null;
  return Number(value);
}

// The pages open in the window — the page and a database page opened over
// it — which a menu's "프레젠테이션 모드" or "여기에서 발표" presents.
const presentable = new Map<string, number>();

/** Marks a page presentable while its body shows; the returned function unmarks it. */
export function registerPresentablePage(pageId: string) {
  presentable.set(pageId, (presentable.get(pageId) ?? 0) + 1);
  return () => {
    const left = (presentable.get(pageId) ?? 1) - 1;
    if (left > 0) presentable.set(pageId, left); else presentable.delete(pageId);
  };
}

export function canPresentPage(pageId: string) {
  return presentable.has(pageId);
}

/** The window's address with the slide set, or the presentation ended; the rest of the address stays. */
export function presentationRoute(index: number | null) {
  const params = new URLSearchParams(window.location.search);
  if (index === null) params.delete(PRESENT_PARAM); else params.set(PRESENT_PARAM, String(index));
  const query = params.toString();
  return `${window.location.pathname}${query ? `?${query}` : ""}`;
}

/** Starts presenting the open page, from the slide holding the block when one is named. */
export function requestPresentation(detail: PresentRequest) {
  window.dispatchEvent(new CustomEvent<PresentRequest>(PRESENT_EVENT, { detail }));
}
