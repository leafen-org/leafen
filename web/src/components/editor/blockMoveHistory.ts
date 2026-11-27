import { flushSync } from "react-dom";
import type { ContentGraph, GraphBlock, GraphBlockMove } from "@/contentGraph";
import type { TextSpan } from "@/lib/types";
import { i18next } from "@/i18n";
import { createBlockMoveService, moveBlocksInOrder } from "@/lib/blockMoveService";
import { sendContentGraphChange } from "@/lib/contentGraphSend";
import { blockTextsNow, showBlockTextAhead } from "@/lib/blockTextAhead";
import type { PageGraphStore } from "@/lib/pageGraphStore";
import { afterPageMade } from "@/lib/pagesNotYetMade";
import { tellPageEditedHere } from "@/lib/pageEditedHere";
import { holdRowsShown } from "@/lib/rowsShownAtPress";
import { selectedRootsInOrder } from "./blockSelectionKeyboard";
import { editorBlockChildren } from "./editorBlockIndex";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { askMovedOffCaret, landMovedOffCaret, type MovedOffCaret } from "./blockMoveCaret";
import { type CaretLanding, caretMayGoOnceAnswered, caretOnceAnswered, focusEditableNextFrame } from "./focus";
import { joinCommittedNativeEditorCommand, landingNativeEditorCommand, startNativeEditorCommand } from "./nativeEditorUndoOwner";
import { choosePageBlocks } from "./pageUndo";

/** Where each block stands: after the one before it, which goes back first. */
export function blockPlaces(graph: ContentGraph, blockIds: readonly string[]) {
  return blockIds.flatMap((blockId): GraphBlockMove[] => {
    const block = graph.blocks.find((candidate) => candidate.id === blockId);
    if (!block) return [];
    const siblings = editorBlockChildren(graph, block.parentId);
    const before = siblings[siblings.findIndex((candidate) => candidate.id === blockId) - 1];
    return [{ blockId, parentId: block.parentId, afterId: before?.id ?? null }];
  });
}

/** The blocks and every block inside them. */
function blocksWithin(graph: ContentGraph, rootIds: readonly string[]) {
  const ids = new Set(rootIds);
  for (let grew = true; grew;) {
    grew = false;
    for (const block of graph.blocks) {
      if (block.parentId && ids.has(block.parentId) && !ids.has(block.id)) {
        ids.add(block.id);
        grew = true;
      }
    }
  }
  return ids;
}

// Blocks a step taking them back shows back now (showBlocksBack), with how many holds show them. A hold
// showing them gone till the page is read again without them (showBlocksGone) leaves them be from then on:
// a ⌘Z pressed before that read shows them back, and each hold drew the page again against the other with
// no end (2026-11-22: ⌘Z right after ⌘⇧P or "/페이지 위치:" to another page overflowed the stack, the blocks
// staying gone; lines made pages, kept gone after the step, left their pages and links in the page).
const blocksShownBack = new Map<string, number>();

/**
 * Blocks going to another page shown gone from this one at once, as
 * Notion's leave at the press (user decision 2026-11-12: a press answers on
 * the next frame — they stayed until the server had moved them, a few round
 * trips): the open page (its graph store) and every graph it takes meanwhile
 * show them gone, until the page is read again once they are moved; refused,
 * they come back where they stood. Null where the open page doesn't hold
 * them (a row's page, an original's editor), which waits for the server.
 *
 * The store holds whichever page is open: the change is this page's alone,
 * while its editor shows it (`adapter.isCurrent`). Another page opened
 * before the move settled — the one they went to, opened from its notice —
 * draws its own graph, the moved blocks in it once there, and the hold lets
 * go: held on, it took them out of that page too, for as long as it stayed
 * open (the page they left, no longer open, is no longer read again here).
 */
export function showBlocksGone(store: PageGraphStore | null | undefined, adapter: NativeEditorGraphAdapter, blockIds: readonly string[]) {
  const graph = adapter.graph;
  const ids = blocksWithin(graph, blockIds);
  const drawn = graph.blocks.filter((block) => ids.has(block.id));
  if (!store || !drawn.length || !store.get().blocks.some((block) => ids.has(block.id))) return null;
  const here = () => adapter.isCurrent();
  // Shown back by a step taking them back (showBlocksBack), they are no longer kept gone.
  let takenBack = false;
  const keeps = () => !(takenBack ||= [...ids].some((id) => blocksShownBack.has(id)));
  const release = holdRowsShown(store,
    (now) => here() && keeps() && now.blocks.some((block) => ids.has(block.id)) ? { ...now, blocks: now.blocks.filter((block) => !ids.has(block.id)) } : now,
    (now) => {
      if (!here()) return now;
      const held = new Set(now.blocks.map(({ id }) => id));
      const back = drawn.filter((block) => !held.has(block.id));
      return back.length ? { ...now, blocks: [...now.blocks, ...back] } : now;
    });
  // Another page open, the hold has nothing of its own left to keep.
  const stop = store.subscribe(() => {
    if (here()) return;
    stop();
    release();
  });
  return {
    refused: () => { stop(); release(true); },
    moved: () => { stop(); release(); },
  };
}

/** The blocks and every block inside them as the page draws them now, which a move takes away. */
export function blocksDrawn(graph: ContentGraph, blockIds: readonly string[]) {
  const ids = blocksWithin(graph, blockIds);
  return graph.blocks.filter((block) => ids.has(block.id));
}

/** Blocks as drawn now with their text, which a ⌘Z of their move shows back (showMovedBack). */
type ShownBack = { drawn: GraphBlock[]; texts: Map<string, TextSpan[]> };

function shownBack(adapter: NativeEditorGraphAdapter, blockIds: readonly string[]): ShownBack {
  const drawn = blocksDrawn(adapter.graph, blockIds), document = adapter.collaborationDocument;
  return { drawn, texts: document ? blockTextsNow(document, drawn.map(({ id }) => id)) : new Map() };
}

/**
 * Blocks a move took from this page shown back where they stood at once, for
 * the move's ⌘Z and its notice's "실행 취소" (user decision 2026-11-12: a
 * press answers on the next frame — they came back once the server had moved
 * each back, 0.7 s on a NAS): as `drawn` before the move, each after the
 * block it stood after (`places`, placedBack), over every graph the page
 * takes meanwhile, without the `hidden` blocks (pages made around them, on
 * their way back) — while this page's editor shows it (showBlocksGone).
 * `rows` changes the databases drawn here and beside the page with them, and
 * takes the change back (a move into a database: its rows go). Answers how
 * to let go: once the server has them back, or refused, as before the press;
 * or, for a step of the page's own history that follows (a line made a page
 * going back to a line, turnBlocksIntoPageIn), with the blocks drawn at once
 * as that step finds them (`asMoved`: the page and its link as they went).
 * What it shows here — the line as it was, its page's link gone — taken for
 * the page's own, the step found nothing left to do and sent nothing for it;
 * and the page as read meanwhile is no help, the room's changes heard on it
 * made on what was shown. A hold showing them gone leaves them be from then
 * on (blocksShownBack).
 */
export function showBlocksBack(store: PageGraphStore | null | undefined, adapter: NativeEditorGraphAdapter, drawn: readonly GraphBlock[],
  places: readonly GraphBlockMove[], hidden: ReadonlySet<string> = new Set(),
  rows?: { shown: (graph: ContentGraph) => ContentGraph; undo: (graph: ContentGraph) => ContentGraph }) {
  if (!store) return () => undefined;
  const here = () => adapter.isCurrent();
  const placed = placedBack(store.get(), drawn, places);
  const ids = new Set(drawn.map(({ id }) => id));
  const taken = (block: GraphBlock) => ids.has(block.id) || hidden.has(block.id);
  const before = store.get().blocks.filter(taken);
  const withBlocks = (now: ContentGraph, back: readonly GraphBlock[]) => {
    const there = now.blocks.filter(taken);
    if (there.length === back.length && there.every((block) => back.includes(block))) return now;
    return { ...now, blocks: [...now.blocks.filter((block) => !taken(block)), ...back] };
  };
  const same = (graph: ContentGraph) => graph;
  let counted = true;
  for (const id of ids) blocksShownBack.set(id, (blocksShownBack.get(id) ?? 0) + 1);
  const uncount = () => {
    if (!counted) return;
    counted = false;
    for (const id of ids) {
      const left = (blocksShownBack.get(id) ?? 1) - 1;
      if (left > 0) blocksShownBack.set(id, left); else blocksShownBack.delete(id);
    }
  };
  // In the press's own frame: behind it, a page's block came back 150 ms
  // after the key (NAS, CPU ×2), drawn behind the editor's own.
  const release = holdRowsShown(store,
    (now) => (rows?.shown ?? same)(here() ? withBlocks(now, placed) : now),
    (now) => (rows?.undo ?? same)(here() ? withBlocks(now, before) : now), true);
  return (end: boolean | readonly GraphBlock[] = false) => {
    uncount();
    release(end === true);
    if (typeof end === "boolean") return;
    // Where they were shown, a line off once the server numbered the list again (placedBack).
    const at = new Map(placed.map((block) => [block.id, block.position]));
    const asMoved = end.map((block) => ({ ...block, position: at.get(block.id) ?? block.position }));
    flushSync(() => store.set((now) => here() ? withBlocks(now, asMoved) : now));
  };
}

/**
 * Blocks taken back, each block a move took out of its list placed right
 * after the one it stood after (`places`) as the page holds it now, or first
 * in the list: the server numbers a list's blocks again as some leave it, so
 * at the place it was drawn at a block tied with the one after it and came
 * one line off by its ID, until the page was read again (seen 2026-11-13).
 */
function placedBack(now: ContentGraph, drawn: readonly GraphBlock[], places: readonly GraphBlockMove[]): GraphBlock[] {
  const shownAt = new Map<string, string>();
  const positionOf = (id: string | null) => id === null ? "" : shownAt.get(id) ?? now.blocks.find((block) => block.id === id)?.position;
  for (const place of places) {
    const after = positionOf(place.afterId);
    // Right after it and before the block that follows it: a position it starts.
    if (after !== undefined) shownAt.set(place.blockId, `${after}\u0000`);
  }
  return drawn.map((block) => {
    const position = shownAt.get(block.id);
    return position === undefined ? block : { ...block, position };
  });
}

/**
 * Blocks moved to another page going there and back within this page's ⌘Z, as
 * Notion's (seen 2026-11-05). Moved, the caret is at the start of the page's
 * title. Moved back, chosen blocks are chosen again; a line that held the caret
 * does not get it back.
 *
 * Moved to their own page they go last on it (the server's block move), as
 * Notion's (seen 2026-11-10): a line that held the caret has it again where it
 * was in the line, and chosen blocks are no longer chosen; moved back, the
 * caret is at the start of the page's title.
 */
function pageMove(adapter: NativeEditorGraphAdapter, blockIds: readonly string[], chosen: boolean, destinationPageId: string, caret?: number,
  store?: PageGraphStore | null, { turned }: {
    /**
     * Lines made pages to move (turnBlocksIntoPageIn): shown back as these
     * lines, without their pages' links, till the page's history takes them
     * back from the pages and links `asMoved`.
     */
    turned?: ShownBack & { links: ReadonlySet<string>; asMoved: GraphBlock[] };
  } = {}) {
  const context = adapter.moveContext;
  if (!context) throw new Error("The page cannot move blocks.");
  const { session } = context, sourcePageId = adapter.pageId, sync = adapter.graphSync;
  const moves = createBlockMoveService(session), places = blockPlaces(adapter.graph, blockIds);
  const ownPage = destinationPageId === sourcePageId;
  // Going to another page they leave this one at the press (showBlocksGone),
  // and come back at the press of their ⌘Z (showMovedBack).
  const leaveAtOnce = !ownPage && Boolean(store) && blockIds.some((id) => store!.get().blocks.some((block) => block.id === id));
  // The blocks as they were drawn and their text as they went, which their ⌘Z shows back (showMovedBack).
  let went: ShownBack | null = null;
  // The blocks move once the page they go to is made, one made here first included (pagesNotYetMade).
  const moveThere = async (signal?: AbortSignal) => {
    if (leaveAtOnce) went = turned ?? shownBack(adapter, blockIds);
    const shown = leaveAtOnce ? showBlocksGone(store, adapter, blockIds) : null;
    try {
      if (sync && !(await sync.flush())) throw new Error("The page's changes could not be saved.");
      await afterPageMade(destinationPageId, signal);
      await moveBlocksInOrder(moves, { blockIds, destinationPageId, sourcePageId, signal });
      // Both pages are edited by it, as the server notes it (block_move_page_edits.go).
      const edit = { updatedAt: new Date().toISOString(), lastEditedBy: session.user.id };
      for (const id of new Set([destinationPageId, sourcePageId])) tellPageEditedHere({ id, ...edit });
    } catch (error) {
      shown?.refused();
      throw error;
    }
    return shown;
  };
  // Taken back from the page they went to, they stand where they were drawn
  // at the press, their lines with the text they went with (user decision
  // 2026-11-12: a press answers on the next frame — they came back once the
  // server had moved each back, 0.3–0.75 s on a NAS-class server, and drawn
  // before then a line stood empty, its text coming with the server's move).
  // Let go once the page is read with them back — the server takes each back
  // and then puts it after the block it stood after, and a change heard
  // between those would draw it where the first left it; refused, they go again.
  const showMovedBack = () => {
    const document = adapter.collaborationDocument;
    if (!went || !store || !document) return null;
    const texts = showBlockTextAhead(document, went.texts);
    const release = showBlocksBack(store, adapter, went.drawn, places, turned?.links);
    // Lines made pages go back to lines by the page's own history once this
    // step is done, from their pages and links as they went (showBlocksBack):
    // shown till then, their pages coming back and then their links going
    // were drawn on the way, 0.3–0.7 s after the key (NAS, CPU ×2).
    return { moved: () => release(turned ? turned.asMoved : false), refused: () => { release(true); texts(); } };
  };
  // The caret's place once the blocks are there is asked for as what moves
  // them begins (`land`), what is typed till then typed there (caretOnceAnswered).
  const caretAtTitle = (land: CaretLanding) => land({ key: `title:${sourcePageId}`, position: "start" });
  // The page draws them last on their own page: the server's move read back by the room or by the page read again,
  // whichever comes first (2026-11-22: waiting for a read of its own after the move's, a second and more on this
  // Mac, at times past the five seconds what is typed is held, the caret never came back to the line moved).
  const drawnLast = () => new Promise<void>((resolve) => {
    const last = () => {
      const shown = editorBlockChildren(store!.drawn(), null).slice(-blockIds.length);
      return shown.length === blockIds.length && shown.every((block, index) => block.id === blockIds[index]);
    };
    const stop = store && !last() ? store.subscribeDrawn(() => { if (last()) done(); }) : null;
    const done = () => { stop?.(); resolve(); };
    if (store && !stop) done();
    else void (sync?.reread() ?? Promise.resolve(false)).finally(done);
  });
  // Where the caret goes on their own page once it shows them last (landed): a line's back in it, chosen blocks let go of.
  const landed = (land: CaretLanding | null) => void (async () => {
    await drawnLast();
    if (chosen) choosePageBlocks(sourcePageId, []);
    else if (caret !== undefined && blockIds[0]) {
      if (land) land({ key: blockIds[0], position: caret }, true); else focusEditableNextFrame(blockIds[0], caret);
    }
  })();
  // Gone to another page, the move, its ⌘Z and its ⌘⇧Z leave the caret by
  // the rule a move into a database keeps too (blockMoveCaret.ts): at the
  // start of the title when a line the caret was in went, or lines made pages
  // ("페이지 위치:"), and none for chosen blocks — a line chosen by Escape then
  // ⌘⇧P, a link duplicated by ⋮⋮ "복제" then ⌘⇧P, a line chosen by its ⋮⋮
  // then Escape then ⌘⇧P, and ⋮⋮ "옮기기" (seen 2026-11-16, five times in two
  // tabs; once, in a tab whose menus had taken keys oddly before, Escape then
  // ⌘⇧P put the letter before the title's words — seen once, no condition
  // found), and two lines chosen by Escape and ⇧↓ (2026-11-24).
  const arrivesWithCaret = ownPage ? !chosen && caret !== undefined : caret !== undefined || Boolean(turned);
  // As the move lands: off the page by that rule; on their own page a line's caret once it shows them last, else none.
  const arrived = (asked: MovedOffCaret) => ownPage && arrivesWithCaret ? landed(asked.land) : landMovedOffCaret(asked, caretAtTitle);
  return {
    blockIds,
    arrivesWithCaret,
    arrived,
    caretAtTitle,
    leaveAtOnce,
    scope: `${adapter.workspaceId}/${sourcePageId}/${session.user.id}`,
    go: async (signal?: AbortSignal) => {
      const shown = await moveThere(signal);
      // Read again without them, they are no longer held gone; the read failing, they stay gone.
      if (shown) void (sync?.reread() ?? Promise.resolve(false)).then((read) => { if (read) shown.moved(); });
    },
    // `there`, once the server has them on the page the step takes them to, before they are placed and read again.
    step: async (forward: boolean, there?: () => void) => {
      // The caret it leaves, asked for at its key (askMovedOffCaret); taken back on their own page, a line's at the title.
      const asked: MovedOffCaret = ownPage && !forward
        ? { forward, land: chosen ? null : caretOnceAnswered(), mayGo: caretMayGoOnceAnswered() }
        : askMovedOffCaret(forward, arrivesWithCaret);
      const back = forward || ownPage ? null : showMovedBack();
      try {
        const shown = forward ? await moveThere() : null;
        if (!forward) for (const [index, place] of places.entries()) {
          if (!ownPage) await moves.move({ blockId: place.blockId, destinationPageId: sourcePageId, sourcePageId: destinationPageId });
          // The page they went to no longer reads them.
          if (index === places.length - 1) there?.();
          await sendContentGraphChange(session, sourcePageId, [{ kind: "moveBlock", move: place }]);
        }
        const read = await sync?.reread();
        if (!turned) back?.moved();
        if (read) shown?.moved();
        await context.onPagesChanged?.();
        // Done again, as the move landed (arrived); on their own page chosen blocks are let go of too; taken back,
        // chosen blocks are chosen again.
        if (forward) void (ownPage ? landed(asked.land) : arrived(asked));
        else landMovedOffCaret(asked, caretAtTitle, chosen ? { pageId: sourcePageId, blockIds } : undefined);
        if (turned) back?.moved();
        return true;
      } catch {
        asked.land?.(null);
        back?.refused();
        context.onStatus?.(i18next.t("blockMoveToDialog:toast.moveFailed"), "error");
        await sync?.reread();
        return false;
      }
    },
  };
}

/**
 * Moves blocks to another page, or last on their own, in their order; `caret`
 * is where the caret was in the line moved, if it was in one. What it answers
 * sets the caret once what asked has closed.
 */
export async function moveBlocksToPage(
  adapter: NativeEditorGraphAdapter, blockIds: readonly string[], chosen: boolean, destinationPageId: string, signal?: AbortSignal,
  caret?: number, store?: PageGraphStore | null,
) {
  const move = pageMove(adapter, blockIds, chosen, destinationPageId, caret, store);
  // The caret the move leaves, asked for at the press (askMovedOffCaret).
  const asked = askMovedOffCaret(true, move.arrivesWithCaret);
  const moving = recordedFromStart(move, signal);
  if (!move.leaveAtOnce) {
    await moving.catch((error: unknown) => { asked.land?.(null); throw error; });
    return () => move.arrived(asked);
  }
  // Gone from the page at the press, the caret where the move leaves it as
  // what asked closes; refused, they are back where they stood and it says so.
  void moving.catch(() => {
    adapter.moveContext?.onStatus?.(i18next.t("blockMoveToDialog:toast.moveFailed"), "error");
    void adapter.graphSync?.reread();
  });
  return () => move.arrived(asked);
}

/**
 * moveBlocksToPage for an inline database opened alone, answering how its
 * notice's "실행 취소" takes it back to its place, as Notion's (seen 2026-11-11):
 * `back` once the server has it there, before the page is read again; whether it went.
 */
export async function moveBlocksToPageUndoable(
  adapter: NativeEditorGraphAdapter, blockIds: readonly string[], destinationPageId: string, signal?: AbortSignal,
) {
  const move = pageMove(adapter, blockIds, false, destinationPageId);
  await recordedFromStart(move, signal);
  return (back?: () => void) => move.step(false, back);
}

/** The move goes as one step of the page's ⌘Z from its start, a ⌘Z meanwhile waiting for it (startNativeEditorCommand). */
async function recordedFromStart(move: ReturnType<typeof pageMove>, signal?: AbortSignal) {
  const command = startNativeEditorCommand(move.scope, `move-blocks:${move.blockIds[0]}:${Date.now()}`);
  try {
    await move.go(signal);
  } catch (error) {
    command.fail();
    throw error;
  }
  command.land(move.step);
}

/**
 * "페이지 위치:": each block becomes a page inside the page picked, and a
 * link to it stays where it was, as Notion's does (seen 2026-10-01). The
 * blocks turn into pages here first, taking their blocks inside, and are
 * saved; then the pages move there. When the move fails the links go and
 * the pages stay here. All of it is one ⌘Z (seen 2026-11-05).
 */
export async function turnBlocksIntoPageIn(
  adapter: NativeEditorGraphAdapter, blockIds: readonly string[], chosen: boolean, destinationPageId: string,
  store?: PageGraphStore | null,
) {
  const roots = selectedRootsInOrder(adapter.graph, new Set(blockIds));
  const ids = roots.map(({ id }) => id);
  if (ids.length === 0) return undefined;
  // The title's caret it leaves, asked for now (caretOnceAnswered).
  const land = caretOnceAnswered();
  // Their ⌘Z shows the lines back as they are now, with their text, their
  // links gone, at the press (user decision 2026-11-12: a press answers on
  // the next frame — the pages came back from the server, then went back to
  // lines, 0.7 s after the key on a NAS-class server, the page drawn three
  // ways meanwhile); they leave this page at the press too (showBlocksGone).
  const turned = { ...shownBack(adapter, ids), links: new Set<string>(), asMoved: [] as GraphBlock[] };
  const move = pageMove(adapter, ids, chosen, destinationPageId, undefined, store, { turned });
  const links = roots
    .map((root) => adapter.insertAfter(root.id, "link_to_page", {
      childPageId: root.id, childPageTitle: adapter.text(root), childPageKind: "page",
    }))
    .filter((link): link is GraphBlock => link !== null);
  for (const link of links) turned.links.add(link.id);
  adapter.changeType(ids, "child_page");
  turned.asMoved = blocksDrawn(adapter.graph, [...ids, ...turned.links]);
  // A ⌘Z before the pages are there waits for them (landingNativeEditorCommand);
  // pages that didn't move have only their blocks' change to take back.
  const command = landingNativeEditorCommand(() => Promise.resolve(true));
  joinCommittedNativeEditorCommand(move.scope, command.step);
  try {
    await move.go();
  } catch (error) {
    land(null);
    command.fail();
    if (links.length > 0) adapter.remove(links.map(({ id }) => id));
    throw error;
  }
  command.land(move.step);
  return () => move.caretAtTitle(land);
}

/** Moves blocks of the page an editor shows to another page, from outside that editor. */
type DroppedBlockMove = (blockIds: readonly string[], destinationPageId: string) => Promise<unknown>;
const droppedBlockMoves = new Map<string, DroppedBlockMove>();

/**
 * Blocks dragged from a page onto another page in the sidebar move as its
 * move dialog moves them (moveBlocksToPage): gone from the page at the drop,
 * one step of its ⌘Z, and taken back they are chosen where they stood, with
 * no notice, as Notion's (seen 2026-11-13). A whole page's editor offers
 * this while it shows the page; answers how to stop.
 */
export function offerDroppedBlockMove(adapter: NativeEditorGraphAdapter, store: PageGraphStore | null) {
  const pageId = adapter.pageId;
  // The page is read again once they are moved, as after the dialog's move.
  const move: DroppedBlockMove = (blockIds, destinationPageId) =>
    moveBlocksToPage(adapter, blockIds, true, destinationPageId, undefined, undefined, store).then(() => adapter.graphSync?.reread());
  droppedBlockMoves.set(pageId, move);
  return () => {
    if (droppedBlockMoves.get(pageId) === move) droppedBlockMoves.delete(pageId);
  };
}

/** The move offered for blocks dragged out of a page's editor, while it shows the page. */
export function droppedBlockMove(pageId: string): DroppedBlockMove | undefined {
  return droppedBlockMoves.get(pageId);
}
