import type { ContentGraph, GraphBlock } from "@/contentGraph";
import { i18next } from "@/i18n";
import { blockTextsNow, showBlockTextAhead } from "@/lib/blockTextAhead";
import { createBlockMoveService } from "@/lib/blockMoveService";
import type { PageGraphStore } from "@/lib/pageGraphStore";
import { holdRowsShown } from "@/lib/rowsShownAtPress";
import { blockPlaces, blocksDrawn } from "./blockMoveHistory";
import { moveSelectedBlocks } from "./blockSelectionKeyboard";
import { editorBlockChildren } from "./editorBlockIndex";
import { notePointedEditor } from "./editorScope";
import type { EditorBlockSpot, NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { focusEditableNextFrame, getEditable } from "./focus";
import {
  dropNativeEditorCommand, recordCommittedNativeEditorCommand, recordHandedNativeEditorCommand, startNativeEditorCommand,
} from "./nativeEditorUndoOwner";
import { choosePageBlocks } from "./pageUndo";
import { syncedBlockSource } from "./syncedBlockModel";
import {
  setSyncedCopyCrossing, syncedCopyCrossingNow, syncedOriginalShown, type SyncedCopyCrossing, type SyncedCopySide, type SyncedOriginalSide,
} from "./syncedCopyPlaces";

/**
 * Cmd/Ctrl+Shift+Arrow on blocks (moveSelectedBlocks), with what it leaves
 * past this page's own change: blocks crossing a synced copy's edge to
 * another page, and a line out of a copy shown on its own page keeping its
 * caret where it was in it.
 */
export function moveBlocksByKeys(
  adapter: NativeEditorGraphAdapter, selection: ReadonlySet<string>, direction: "up" | "down", rootId: string | null = null,
  at: { shownIn?: string | null; caret?: number } = {},
) {
  const { crossing, outOfCopy } = moveSelectedBlocks(adapter, selection, direction, rootId, at.shownIn ?? null);
  const caret = at.caret;
  if (crossing) crossSyncedCopy(adapter, crossing.blocks.map(({ id }) => id), direction, crossing.crossing, caret);
  if (outOfCopy && caret !== undefined) focusEditableNextFrame(outOfCopy, caret);
}

/**
 * Moves blocks across a synced copy's edge through the server, as "Move to"
 * moves them between pages, both pages showing them where they went: out of
 * the copy's content they go right before or after the copy; into a copy,
 * last or first in its original. Their page's ⌘Z takes them back as one step,
 * and ⌘⇧Z moves them again. The caret goes with a line, at the same place in
 * it; chosen blocks stay chosen. Each way they show where they go at the
 * press, with their text (showCrossing).
 */
function crossSyncedCopy(
  adapter: NativeEditorGraphAdapter, blockIds: readonly string[], direction: "up" | "down", crossing: SyncedCopyCrossing, caret?: number,
) {
  const plan = crossingPlan(adapter, direction, crossing);
  const session = adapter.session;
  if (!plan || !session || syncedCopyCrossingNow() || blockIds.length === 0) return;
  const { to, spot, stores } = plan, from: SyncedCopySide = adapter;
  const back = blockPlaces(from.graph, blockIds);
  const moves = createBlockMoveService(session);
  const carry = async (source: SyncedCopySide, destination: SyncedCopySide, spots: readonly EditorBlockSpot[]) => {
    if (source.graphSync && !(await source.graphSync.flush())) throw new Error("The page's changes could not be saved.");
    await (destination.graphSync?.holding ?? ((task) => task()))(async () => {
      for (const [index, blockId] of blockIds.entries()) {
        await moves.move({ blockId, sourcePageId: source.pageId, destinationPageId: destination.pageId });
        if (destination.graphSync?.place && !(await destination.graphSync.place(blockId, spots[index]!))) {
          throw new Error("The block could not be placed.");
        }
      }
    });
    await Promise.all([source.graphSync?.reread(), destination.graphSync?.reread()]);
  };
  const forward = blockIds.map((_, index) => ({ parentId: spot.parentId, afterId: index ? blockIds[index - 1]! : spot.afterId }));
  const backward = back.map(({ parentId, afterId }) => ({ parentId, afterId }));
  // Each page keeps the step in its own ⌘Z, and the caret goes from page to
  // page with the blocks: a step done from one page's history goes on in the
  // history of the page it brings them to, done there for ⌘Z to take back or
  // undone for ⌘⇧Z to do again, and leaves the other page's history. A step
  // whose blocks are already where it would take them changes nothing.
  const scope = (page: SyncedCopySide) => `${page.workspaceId}/${page.pageId}/${session.user.id}`;
  const key = `synced-copy-move:${blockIds[0]}:${Date.now()}`;
  let there = false, handed = 0, held: { scope: string; key: string }[] = [];
  const step = (own: { scope: string; key: string } | null) => async (ahead: boolean) => {
    if (ahead === there) return true;
    const [source, destination] = ahead ? [from, to] : [to, from];
    const spots = ahead ? forward : backward;
    const shown = showCrossing(ahead ? stores : { from: stores.to, to: stores.from }, source, destination, blockIds, spots);
    // The caret or the choice goes with them at once, where they are shown.
    landNextFrame(destination, blockIds, caret);
    setSyncedCopyCrossing(true);
    try {
      await carry(source, destination, spots);
    } catch {
      shown.refused();
      adapter.moveContext?.onStatus?.(i18next.t("blockMoveToDialog:toast.moveFailed"), "error");
      await Promise.all([source.graphSync?.reread(), destination.graphSync?.reread()]);
      return false;
    } finally { setSyncedCopyCrossing(false); }
    shown.moved();
    there = ahead;
    for (const other of held) if (other !== own) dropNativeEditorCommand(other.scope, other.key);
    held = own ? [own] : [];
    const landing = scope(destination);
    if (landing !== own?.scope) {
      const next = { scope: landing, key: `${key}:${++handed}` };
      const recorded = own ? recordHandedNativeEditorCommand(landing, next.key, step(next), ahead ? "done" : "undone")
        : recordCommittedNativeEditorCommand(landing, next.key, step(next));
      if (recorded) held.push(next);
    }
    // Drawn again from the pages as read, the line keeps the caret it had.
    landNextFrame(destination, blockIds, caret, () => Boolean(getEditable(blockIds[0]!)?.contains(document.activeElement)));
    return true;
  };
  // A step of the page's ⌘Z from the start (startNativeEditorCommand): one
  // pressed there before the blocks have gone waits for them and takes them
  // back from there; otherwise the page they went to holds the step.
  const own = { scope: scope(from), key: `${key}:0` };
  const started = startNativeEditorCommand(own.scope, own.key);
  void step(null)(true).then((moved) => {
    if (moved && started.asked) {
      held.push(own);
      started.land(step(own));
    } else started.fail();
  });
}

/**
 * The page the blocks go to and where, first of them; and the graphs each
 * side draws, the original's as the copy shows it and the copy's page's when
 * it is the open page.
 */
function crossingPlan(adapter: NativeEditorGraphAdapter, direction: "up" | "down", crossing: SyncedCopyCrossing) {
  const copyStore = (original: SyncedOriginalSide | undefined, copyId: string) =>
    original?.copyStore?.get().blocks.some((block) => block.id === copyId) ? original.copyStore : null;
  if (crossing.kind === "leave") {
    const copyId = adapter.moveContext?.syncedCopy?.blockId, to = adapter.moveContext?.syncedCopy?.page();
    const copy = to?.graph.blocks.find((block) => block.id === copyId);
    if (!to || !copy) return null;
    const siblings = editorBlockChildren(to.graph, copy.parentId);
    const before = siblings[siblings.findIndex((block) => block.id === copy.id) - 1];
    const original = syncedOriginalShown(to.pageId, copy.id);
    return {
      to, spot: { parentId: copy.parentId, afterId: direction === "up" ? before?.id ?? null : copy.id },
      stores: { from: original?.store ?? null, to: copyStore(original, copy.id) },
    };
  }
  const to = syncedOriginalShown(adapter.pageId, crossing.copy.id);
  const original = syncedBlockSource(crossing.copy, adapter.pageId).blockId;
  if (!to?.graph.blocks.some((block) => block.id === original)) return null;
  return {
    to, spot: { parentId: original, afterId: direction === "up" ? editorBlockChildren(to.graph, original).at(-1)?.id ?? null : null },
    stores: { from: copyStore(to, crossing.copy.id), to: to.store },
  };
}

/**
 * Blocks crossing the edge shown at the press where they go (user decision
 * 2026-11-12: a press answers on the next frame — they went and came only
 * once the server had carried them and both pages were read again): gone
 * from the graph the source draws, and in the destination's at their spots,
 * each right after the block it follows, with the text they had; let go once
 * both pages are read with them there, or, refused, as before the press.
 * A side whose graph isn't to hand waits for the server.
 */
function showCrossing(stores: { from: PageGraphStore | null; to: PageGraphStore | null }, source: SyncedCopySide, destination: SyncedCopySide,
  blockIds: readonly string[], spots: readonly EditorBlockSpot[]) {
  const graph = stores.from?.get() ?? source.graph;
  const drawn = blocksDrawn(graph, blockIds);
  const ids = new Set(drawn.map(({ id }) => id));
  const without = (now: ContentGraph) => now.blocks.some((block) => ids.has(block.id)) ? { ...now, blocks: now.blocks.filter((block) => !ids.has(block.id)) } : now;
  const was = stores.to?.get().blocks.filter((block) => ids.has(block.id)) ?? [];
  const placed = stores.to ? placedAt(stores.to.get(), drawn, blockIds, spots) : [];
  const gone = holdRowsShown(stores.from, without,
    (now) => ({ ...now, blocks: [...without(now).blocks, ...drawn] }), true);
  const there = holdRowsShown(stores.to, (now) => ({ ...without(now), blocks: [...without(now).blocks, ...placed] }),
    (now) => ({ ...now, blocks: [...without(now).blocks, ...was] }), true);
  const document = destination.collaborationDocument, from = source.collaborationDocument;
  const texts = document && from && stores.to ? showBlockTextAhead(document, blockTextsNow(from, ids)) : () => undefined;
  return {
    moved: () => { gone(); there(); },
    refused: () => { gone(true); there(true); texts(); },
  };
}

/** The blocks with each moved one at its spot: its parent's, right after the block it follows there, or first. */
function placedAt(now: ContentGraph, drawn: readonly GraphBlock[], blockIds: readonly string[], spots: readonly EditorBlockSpot[]) {
  const shownAt = new Map<string, string>();
  const spotOf = new Map(blockIds.map((id, index) => [id, spots[index]!]));
  const positionOf = (id: string | null) => id === null ? "" : shownAt.get(id) ?? now.blocks.find((block) => block.id === id)?.position;
  for (const [id, spot] of spotOf) {
    const after = positionOf(spot.afterId);
    if (after !== undefined) shownAt.set(id, `${after}\u0000`);
  }
  return drawn.map((block) => {
    const spot = spotOf.get(block.id);
    return spot ? { ...block, parentId: spot.parentId, afterId: spot.afterId, position: shownAt.get(block.id) ?? block.position } : block;
  });
}

/**
 * Once the next frame has drawn them, unless `unless` says then it needn't:
 * the caret back in the line it was in, what is typed meanwhile typed there
 * (focusEditableNextFrame); or the blocks chosen in the editor that now
 * shows them, whose keys act on them next.
 */
function landNextFrame(page: SyncedCopySide, blockIds: readonly string[], caret?: number, unless?: () => boolean) {
  if (caret !== undefined) focusEditableNextFrame(blockIds[0]!, caret, unless);
  else requestAnimationFrame(() => { if (!unless?.()) choose(page, blockIds); });
}

function choose(page: SyncedCopySide, blockIds: readonly string[]) {
  if (document.activeElement instanceof HTMLElement) document.activeElement.blur();
  notePointedEditor(getEditable(blockIds[0]!) ?? document.querySelector(`[data-block-id="${CSS.escape(blockIds[0]!)}"]`));
  choosePageBlocks(page.pageId, blockIds);
}
