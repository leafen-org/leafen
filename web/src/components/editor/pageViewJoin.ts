import type { ContentGraph } from "@/contentGraph";
import { chordLetter, primaryModifier } from "@/lib/keyboardShortcuts";
import { VIEW_CHOICE_JOIN, type ViewChoiceJoin } from "../database/viewChoiceJoin";
import { VIEW_CHOICE_ROOT, type ViewChoiceOut } from "../database/viewChoiceSteps";
import { arrowBlockSelection, type BlockSelectionWalk } from "./blockSelectionKeyboard";
import { chosenPlace, occurrenceBlockId, placeChoice } from "./blockOccurrences";
import { showChosen } from "./chosenBlockShown";
import { editorSelectionRoots } from "./editorBlockCopies";
import { joinComingNativeEditorCommand } from "./nativeEditorCommandSteps";
import type { EditorSelection, NativeEditorGraphAdapter } from "./editorRuntimeTypes";

/**
 * The page's half of a choice joined across an inline database's edge
 * (viewChoiceJoin.ts, Notion's): the view whose items are chosen with the
 * page's blocks under it, and which end ↑ ↓ with Shift move — the view's,
 * entered with Shift+↑ from the block under the view, the blocks keeping
 * where the choice began, or the page's, Shift+↓ having gone on from the
 * view's last item. The page takes the keys while they are joined.
 */
export type PageViewJoin = { view: Element; moving: "view" | "page"; selection: EditorSelection; again?: boolean };
type JoinRef = { current: PageViewJoin | null };

function tell(view: Element, detail: ViewChoiceJoin) {
  return !view.dispatchEvent(new CustomEvent<ViewChoiceJoin>(VIEW_CHOICE_JOIN, { cancelable: true, detail })) && detail.out !== true;
}

/**
 * Shift+↑ onto an inline database from the block under it: its view
 * chooses its last item, joined to the blocks chosen, which stay as they
 * are. A view with no item takes nothing, the database's block joining the
 * choice as any block's.
 */
export function joinViewFromBelow(graph: ContentGraph, root: Element | null, current: EditorSelection,
  moved: { walk: BlockSelectionWalk | null }, up: boolean): PageViewJoin | null {
  const id = moved.walk ? occurrenceBlockId(moved.walk.focus) : "";
  if (!up || !id || current.has(id) || graph.blocks.find((block) => block.id === id)?.type !== "inline_database") return null;
  const view = root?.querySelector(`[data-block-id="${CSS.escape(id)}"] [${VIEW_CHOICE_ROOT}]`);
  return view && tell(view, { act: "in" }) ? { view, moving: "view", selection: current } : null;
}

/** A view's Shift+↓ past its last item (VIEW_CHOICE_OUT): the page's next block joined to its items, the page's end moving. */
export function joinFromView(event: Event, moved: EditorSelection): PageViewJoin | null {
  const detail = (event as CustomEvent<ViewChoiceOut>).detail;
  return detail?.shift && detail.down && event.target instanceof Element ? { view: event.target, moving: "page", selection: moved } : null;
}

/** The page's blocks of a joined choice moved by its own walk: the join goes on with them. */
export function keepJoin(join: JoinRef, selection: EditorSelection) {
  if (join.current) join.current.selection = selection;
}

/** A joined choice whose page half went another way (a press, an undo): the view lets its items go too. */
export function dropStaleJoin(join: JoinRef, current: EditorSelection) {
  const joined = join.current;
  if (!joined || joined.selection === current) return;
  // Joined again by a ⌘Z, the same blocks chosen once more as the step settles keep it, until a key.
  if (joined.again && current.size === joined.selection.size && [...current].every((id) => joined.selection.has(id))) {
    joined.selection = current;
    return;
  }
  join.current = null;
  if (joined.view.isConnected) tell(joined.view, { act: "end" });
}

/**
 * A key on a joined choice, as Notion's (seen 2026-11-27, viewChoiceJoin.ts):
 * Shift+↑ ↓ moving the view's end stretch its items, and past them the
 * database's block joins the page's going up, or the blocks stay alone
 * going down; moving the page's end, the blocks walk as theirs do, and
 * Shift+↑ from the block under the view leaves its items chosen alone. ↑
 * chooses the item before the first chosen alone — the database's block
 * where there is none —; any other key goes on as on the blocks alone,
 * Delete and Backspace moving the items to the trash with them. Whether it
 * took the key whole.
 */
export function joinedViewKey(event: KeyboardEvent, join: JoinRef, current: EditorSelection, page: {
  adapter: NativeEditorGraphAdapter; root: Element | null; rootId: string | null; selected(): EditorSelection;
  walk: { current: { ends: BlockSelectionWalk | null; selection: EditorSelection } | null };
  setSelection(next: EditorSelection): void; selectable(selection: Set<string>): Set<string>;
}) {
  const joined = join.current;
  if (!joined) return false;
  if (joined.selection !== current) dropStaleJoin(join, current);
  if (join.current !== joined || !joined.view.isConnected) return false;
  joined.again = false;
  const up = event.key === "ArrowUp", plain = !(event.metaKey || event.ctrlKey || event.altKey);
  const arrow = plain && (up || event.key === "ArrowDown");
  const choose = (next: EditorSelection, ends: BlockSelectionWalk | null) => {
    page.walk.current = ends ? { ends, selection: next } : null;
    page.setSelection(next);
    if (ends) showChosen(page.root, ends.focus);
  };
  if (arrow && event.shiftKey && joined.moving === "page") {
    if (!up || editorSelectionRoots(page.adapter.graph, [...current]).length > 1) return false;
    event.preventDefault();
    join.current = null;
    tell(joined.view, { act: "end", keep: true });
    choose(new Set(), null);
    return true;
  }
  if (arrow && (event.shiftKey || up)) {
    event.preventDefault();
    const kept = tell(joined.view, event.shiftKey ? { act: "stretch", down: !up } : { act: "step" });
    if (!event.shiftKey || !kept) join.current = null;
    if (kept && !event.shiftKey) choose(new Set(), null);
    if (kept || !up) return true;
    // Past the view's first item: its database's block, with the page's blocks with Shift.
    const ends = page.walk.current?.selection === current ? page.walk.current.ends : null;
    const moved = arrowBlockSelection(page.adapter.graph, current, "up", event.shiftKey, page.rootId, ends,
      { pageId: page.adapter.pageId, at: (id) => chosenPlace(current, id) });
    choose(placeChoice(page.selectable(moved.selection), moved.places), moved.walk);
    return true;
  }
  // Delete, Backspace and ⌘X take the items with the blocks, one step of the page's ⌘Z (trashedWithBlocks), ⌘X's
  // copy being the blocks' (Notion's, seen 2026-11-27 in two tabs at two times, a table, a board and a gallery:
  // the item and the line cut, ⌘Z both back joined); another key that changes the blocks' choice lets them go as
  // it is drawn (dropStaleJoin), one that changes nothing, as a letter, keeps them.
  const cut = primaryModifier(event) && !event.shiftKey && !event.altKey && chordLetter(event) === "x";
  if (cut || (plain && !event.shiftKey && (event.key === "Delete" || event.key === "Backspace"))) {
    join.current = null;
    const trashed: ViewChoiceJoin = { act: "trash" };
    trashedWithBlocks(joined, trashed, join, page);
    tell(joined.view, trashed);
  }
  return false;
}

/**
 * A joined choice's items moved to the trash and its blocks removed by the
 * same key are one step of the page's ⌘Z, as Notion's (seen 2026-11-27 in
 * Chrome at 1440 in two tabs at two times, a table, a board and a list,
 * judged after each key: Backspace, Delete or ⌘X, then ⌘Z, brought the items
 * and the blocks back together, chosen and joined; ⌘⇧Z took both again,
 * nothing chosen): the items' step, which comes with the server's answer
 * (rows/trashed/, nativeTableRowSteps' rowsTrashed), joins the blocks'.
 * The action that takes the blocks away takes it, a question about them
 * answered first. Taken back, the items are chosen again joined to the
 * blocks the step chooses, the page's end moving.
 */
function trashedWithBlocks(joined: PageViewJoin, trashed: ViewChoiceJoin, join: JoinRef, page: { adapter: NativeEditorGraphAdapter; selected(): EditorSelection }) {
  const { adapter } = page, blocks = [...joined.selection];
  const scope = `${adapter.workspaceId}/${adapter.pageId}/${adapter.session?.user.id ?? ""}`;
  joinComingNativeEditorCommand(scope, (key) => key.startsWith("rows/trashed/"), (forward) => {
    if (forward || !trashed.ids?.length) return;
    // Once the step's own choice of the blocks is drawn.
    requestAnimationFrame(() => requestAnimationFrame(() => {
      const now = page.selected(), view = joined.view;
      if (!view.isConnected || !blocks.every((id) => now.has(id))) return;
      join.current = { view, moving: "page", selection: now, again: true };
      tell(view, { act: "rejoin", ids: trashed.ids });
    }));
  }, (graph) => !blocks.some((id) => graph.blocks.some((block) => block.id === occurrenceBlockId(id))));
}
