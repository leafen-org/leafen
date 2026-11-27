import { useEffect, type Dispatch, type RefObject, type SetStateAction } from "react";
import type { ContentGraph } from "@/contentGraph";
import { chooseAllKey, chooseInView, VIEW_TAB_CHOOSE, VIEW_TAB_PRESSED, type ViewChoiceOut, type ViewTabChoose, type ViewTabPressed } from "../database/viewChoiceSteps";
import { allBlockSelection, arrowBlockSelection, edgeBlockSelection, lastTopLevelBlock, selectedRootsInOrder, type BlockSelectionWalk } from "./blockSelectionKeyboard";
import { chosenPlace, occurrenceBlockId, placeChoice } from "./blockOccurrences";
import { showChosen } from "./chosenBlockShown";
import type { EditorSelection, NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { imeComposing } from "@/lib/fieldKeyBoundary";
import { primaryModifier } from "@/lib/keyboardShortcuts";

const INLINE_DATABASE = "inline_database";

/*
 * A page's blocks and the rows or cards of an inline database on it are one
 * walk for ↑ and ↓, as Notion's (seen 2026-11-12, a table between two lines
 * above and two below): from the line above, ↓ chooses the database's block,
 * then its first row, each row in turn, and from the last row the line
 * below; ↑ from that line chooses the last row, and from the first row the
 * database's block. With nothing chosen, from the page itself, ↓ chooses its
 * first block and ↑ its last, and after Escape let blocks go either chooses
 * again the block it ended on (Notion's, seen 2026-11-12). A database whose
 * view has no item is passed as one block.
 */

function blockElement(root: Element | null, blockId: string) {
  return root?.querySelector(`[data-block-id="${CSS.escape(blockId)}"]`) ?? null;
}

function only(graph: ContentGraph, selection: ReadonlySet<string>) {
  if (selection.size !== 1) return undefined;
  const [id] = selection;
  return graph.blocks.find((block) => block.id === id);
}

/**
 * What ↑ or ↓ chooses with nothing chosen, from the page itself, and ⌘↑ or
 * ⌘↓ (Ctrl off a Mac): the page's first or last block, as from chosen
 * blocks, whatever Escape let go (Notion's, seen twice 2026-11-24 in Chrome
 * on a Mac after a press above the title, fresh tabs, no script before the
 * key: the block chosen and scrolled to, a letter after it written nowhere).
 * ⌘A chooses all the page's blocks, as from chosen blocks, the title not
 * among them, and on a page with none it chooses nothing — never the
 * browser's select-all over the title and the words (Notion's, seen
 * 2026-11-24 in Chrome on a Mac in two tabs at different times, no script
 * before the key, after a press above the title, beside an inline database,
 * left of it or in the room under it: every block washed, a second ⌘A the
 * same, a letter after it written nowhere). A press beside a line puts the
 * caret in it, where ⌘A takes the line's words first (seen once that day).
 * `escaped` is the place Escape let go, a synced copy's occurrence kept.
 * Null when the key is not theirs; `edge` says which end a ⌘ key went to.
 */
export function startPageChoice(event: KeyboardEvent, graph: ContentGraph, rootId: string | null, escaped: string | null) {
  const active = document.activeElement;
  if ((active && active !== document.body && active !== document.documentElement) || imeComposing(event)) return null;
  if (chooseAllKey(event)) return { blocks: allBlockSelection(graph, rootId), edge: null };
  const down = event.key === "ArrowDown";
  const command = primaryModifier(event);
  if ((!down && event.key !== "ArrowUp") || event.shiftKey || event.altKey || (!command && (event.metaKey || event.ctrlKey))) return null;
  const edge = command ? (down ? "last" as const : "first" as const) : null;
  const again = escaped ? occurrenceBlockId(escaped) : "";
  if (!edge && escaped && graph.blocks.some((block) => block.id === again)) return { blocks: placeChoice(new Set([again]), [[again, escaped]]), edge };
  const blocks = edge ? edgeBlockSelection(graph, new Set(), edge, false, rootId)
    : down ? edgeBlockSelection(graph, new Set(), "first", false, rootId) : new Set([lastTopLevelBlock(graph, rootId)?.id ?? ""]);
  return [...blocks].every(Boolean) && blocks.size ? { blocks, edge } : null;
}

/**
 * Hands ↑ or ↓ on chosen blocks to an inline database's view where the walk
 * goes into it: ↓ on its block alone, ↑ onto its block from below. Whether
 * the view took it, choosing its first or last item.
 */
export function pageViewHandoff(graph: ContentGraph, root: Element | null, current: ReadonlySet<string>, moved: ReadonlySet<string>, down: boolean) {
  const from = only(graph, current);
  if (down) return from?.type === INLINE_DATABASE && chooseInView(blockElement(root, from.id), "first");
  const to = only(graph, moved);
  return to?.type === INLINE_DATABASE && to.id !== from?.id && chooseInView(blockElement(root, to.id), "last");
}

/**
 * Whether Enter on a chosen inline database keeps it chosen: its view a
 * table, a board, a list or a gallery, whose rows or cards take the key, as
 * Notion's (seen 2026-11-14, twice each, a second between the keys); a
 * calendar, a timeline, a feed, a map, a form, a chart or a dashboard lets
 * it go.
 */
export function enterKeepsView(root: Element | null, blockId: string) {
  const type = blockElement(root, blockId)?.querySelector("[data-view-type]")?.getAttribute("data-view-type");
  return type === "table" || type === "board" || type === "list" || type === "gallery";
}

/** Hands ⌘↓ to an inline database last on the page: its view chooses its last item. Whether it took it. */
export function pageViewEdgeHandoff(graph: ContentGraph, root: Element | null, chosen: ReadonlySet<string>) {
  const to = only(graph, chosen);
  return to?.type === INLINE_DATABASE && chooseInView(blockElement(root, to.id), "last");
}

/** The blocks an inline database's view hands ↑ or ↓ to past its first or last item: its block, or the block after it. */
export function viewOutSelection(event: Event, graph: ContentGraph, rootId: string | null) {
  const holder = (event.target as Element | null)?.closest?.("[data-block-id]");
  const block = graph.blocks.find((candidate) => candidate.id === holder?.getAttribute("data-block-id"));
  if (block?.type !== INLINE_DATABASE) return null;
  if (!(event as CustomEvent<ViewChoiceOut>).detail?.down) return new Set([block.id]);
  const moved = arrowBlockSelection(graph, new Set([block.id]), "down", false, rootId).selection;
  return moved.has(block.id) ? null : moved;
}

// The page's scroller around `element` brought to its top or its foot.
function scrollPageEdge(element: Element | null, top: boolean) {
  let box = element?.parentElement ?? null;
  while (box && !(box.scrollHeight > box.clientHeight && /(auto|scroll)/u.test(getComputedStyle(box).overflowY))) box = box.parentElement;
  if (box) box.scrollTop = top ? 0 : box.scrollHeight;
}

/**
 * A page's blocks and the view tabs of an inline database on it
 * (viewTabChoice.ts): a press on a tab lets the chosen blocks go, saying where
 * they ended — the end the arrows last moved, as Escape's —, and ↑ or ↓ on a
 * tab chooses that again, shown, or the page's first or last block, the page
 * scrolled to its top or its foot, as Notion's (seen 2026-11-26 in Chrome in
 * two tabs at different times, after the key only).
 */
export function useViewTabBlockChoice({ adapter, root, nested, rootBlockId, selectionRef, setSelection, walk }: {
  adapter: NativeEditorGraphAdapter;
  root: RefObject<HTMLDivElement | null>;
  nested: boolean;
  rootBlockId: string | null;
  selectionRef: RefObject<EditorSelection>;
  setSelection: Dispatch<SetStateAction<EditorSelection>>;
  walk: RefObject<{ ends: BlockSelectionWalk | null; selection: EditorSelection } | null>;
}) {
  useEffect(() => {
    const page = root.current;
    if (!page || nested || adapter.fixedBody) return;
    const pressed = (event: Event) => {
      const current = selectionRef.current;
      if (!current?.size || !adapter.isCurrent()) return;
      const ended = walk.current?.selection === current && walk.current.ends ? walk.current.ends.focus
        : chosenPlace(current, selectedRootsInOrder(adapter.graph, current, rootBlockId)[0]?.id ?? "");
      (event as CustomEvent<ViewTabPressed>).detail.at = ended;
      setSelection(new Set());
    };
    const choose = (event: Event) => {
      const { at, edge } = (event as CustomEvent<ViewTabChoose>).detail ?? {};
      if (adapter.readOnly || !adapter.isCurrent() || selectionRef.current?.size) return;
      const again = at ? occurrenceBlockId(at) : "";
      const blocks = at ? (adapter.graph.blocks.some((block) => block.id === again) ? placeChoice(new Set([again]), [[again, at]]) : null)
        : edge === "first" ? edgeBlockSelection(adapter.graph, new Set(), "first", false, rootBlockId)
          : new Set([lastTopLevelBlock(adapter.graph, rootBlockId)?.id ?? ""]);
      if (!blocks?.size || ![...blocks].every(Boolean)) return;
      event.preventDefault();
      setSelection(blocks);
      if (at) showChosen(page, at);
      else scrollPageEdge(page, edge === "first");
    };
    page.addEventListener(VIEW_TAB_PRESSED, pressed);
    page.addEventListener(VIEW_TAB_CHOOSE, choose);
    return () => {
      page.removeEventListener(VIEW_TAB_PRESSED, pressed);
      page.removeEventListener(VIEW_TAB_CHOOSE, choose);
    };
  }, [adapter, nested, root, rootBlockId, selectionRef, setSelection, walk]);
}
