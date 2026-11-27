import type { Dispatch, SetStateAction } from "react";
import type { GraphBlock, GraphOperation } from "@/contentGraph";
import { graphTextBlockTypes } from "@/contentGraphWire.generated";
import { movePageIntoDatabase } from "@/databaseRowPageApi";
import { i18next } from "@/i18n";
import { createBlockMoveService } from "@/lib/blockMoveService";
import { sendContentGraphChange } from "@/lib/contentGraphSend";
import { createDatabaseDeletedPropertiesSurface } from "@/lib/databaseDeletedProperties";
import { createDatabaseRowPageService } from "@/lib/databaseRowPageService";
import { newId } from "@/lib/ids";
import type { PageGraphStore } from "@/lib/pageGraphStore";
import { pageNamesNow } from "@/lib/pageNames";
import { fullPageDatabaseSource } from "@/lib/relationDatabaseSearch";
import { graphsDrawn, holdRowsShown, rowsDrawn, withoutRows, withoutRowsWhereDrawn, withRowsAsDrawn } from "@/lib/rowsShownAtPress";
import { withPageAsRow, withPagesUnder } from "@/components/pagesIntoDatabase";
import type { Bootstrap, Page } from "@/types";
import { askMovedOffCaret, landMovedOffCaret } from "./blockMoveCaret";
import { blockPlaces, blocksDrawn, showBlocksBack, showBlocksGone } from "./blockMoveHistory";
import { editorBlockChildren, editorDescendantIds } from "./editorBlockIndex";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import type { CaretLanding } from "./focus";
import { isPageBlock } from "./nativeEditorGraphAdapter";
import {
  dropNativeEditorCommand, joinCommittedNativeEditorCommand, landingNativeEditorCommand, startNativeEditorCommand,
} from "./nativeEditorUndoOwner";

/**
 * Blocks moved into a full-page database become its rows, as Notion's do
 * (seen 2026-11-10, 2026-11-11). A block with words, or a divider, turns
 * into a page where it stands — its words with their formatting the title,
 * the blocks inside it the body, a callout's icon its icon
 * (child_page_blocks.go) — and that page goes in before the database's first
 * row, so blocks moved together stand last one first. A to-do's check goes
 * with it into the database's "체크 표시됨" checkbox, which the first to-do
 * makes (page_row_todo_check.go) and ⌘Z takes away again for good. Any other block,
 * as an image, goes into the body of a new untitled row there: a page made
 * around it where it stands. A page's block takes its page in after the last
 * row, said as a page's move is, with "이동" and "실행 취소". The caret goes
 * where blocks moved off their page leave it (blockMoveCaret.ts); all of it
 * is one ⌘Z, which brings the blocks back where they stood.
 *
 * All of it shows at the press (user decision 2026-11-12: a press answers on
 * the next frame — the window stayed up until the database's source was
 * read, each block made a page and moved, and the page read again): the
 * window closes, the blocks leave the page and a page's block the sidebar,
 * the notice comes, and where the open page or a sidebar section draws the
 * database the blocks stand as its rows, by their words, where the server
 * puts them — held over every read until the server moved them. The server's
 * work goes on behind; refused, the blocks come back where they stood (those
 * made pages stay pages), their rows go, the pages are back in the sidebar,
 * the notice goes and the failure is said.
 */

const BECOME_ROWS = new Set<string>([...graphTextBlockTypes, "divider"]);

/**
 * Whether the blocks moving are or hold a database — one a page's body
 * holds, or a full-page one's block — which no database takes: Notion's
 * "옮기기" dims every database for them (seen 2026-11-11).
 */
export function blocksMoveDatabase(graph: NativeEditorGraphAdapter["graph"], blockIds: readonly string[], pages: readonly Page[]) {
  return blockIds.some((id) => {
    const block = graph.blocks.find((candidate) => candidate.id === id);
    return block?.type === "inline_database" || (block !== undefined && isPageBlock(block)
      && pages.some((page) => page.id === block.id && page.kind === "database"));
  });
}

/**
 * What a block move's list leaves out for the blocks moving: databases where
 * they are or hold one (blocksMoveDatabase), and the pages moving with them —
 * a sub-page among them or in one of them, which can't take itself
 * (BlockMoveInput.movingPageIds).
 */
export function blocksMoving(graph: NativeEditorGraphAdapter["graph"], blockIds: readonly string[], pages: readonly Page[]) {
  const moving = editorDescendantIds(graph, blockIds);
  return {
    databaseMoving: blocksMoveDatabase(graph, blockIds, pages),
    movingPageIds: graph.blocks.filter((block) => moving.has(block.id) && isPageBlock(block)).map((block) => block.id),
  };
}

/**
 * What a page's block moved alone into the database says, with the page to
 * go to and its undo; it answers how to take the notice away.
 */
export type BlockMoveDatabaseNotice = (message: string, pageId: string, undo: () => void) => (() => void) | void;

/**
 * A database's data source as the open page or a sidebar section beside it
 * draws it: its own at its page's root, or the block that names it.
 */
function databaseSourceShown(store: PageGraphStore | null, database: Page) {
  for (const graph of graphsDrawn(store)) {
    const container = graph.databaseContainers.find((item) => item.kind === "source"
      && (item.blockId === database.id || (item.pageId === database.id && !item.parentBlockId)));
    const id = container && graph.blocks.find((block) => block.id === container.blockId)?.content.dataSourceId;
    if (container && typeof id === "string" && id) return { id, pageId: container.pageId };
  }
  return undefined;
}

/**
 * Moves blocks, in their order, into the full-page database `database` is
 * the page of, shown so at the press. What it answers sets the caret, and
 * says where a page went, once what asked has closed.
 */
export async function moveBlocksIntoDatabase(
  adapter: NativeEditorGraphAdapter, blockIds: readonly string[], chosen: boolean, database: Page,
  options: { notice?: BlockMoveDatabaseNotice; store?: PageGraphStore | null; setData?: Dispatch<SetStateAction<Bootstrap>> } = {},
) {
  const context = adapter.moveContext;
  if (!context) throw new Error("The page cannot move blocks.");
  const { session } = context, sourcePageId = adapter.pageId, sync = adapter.graphSync, store = options.store ?? null;
  const graph = adapter.graph;
  const blockOf = (id: string) => graph.blocks.find((block) => block.id === id);
  const turned = blockIds.filter((id) => BECOME_ROWS.has(blockOf(id)?.type ?? "") && !isPageBlock(blockOf(id)));
  const wrapped = blockIds.filter((id) => !turned.includes(id) && !isPageBlock(blockOf(id)));
  // A to-do's check, read before it becomes a page.
  const checks = new Map(blockIds.flatMap((id) => {
    const block = blockOf(id);
    return block?.type === "to_do" ? [[id, block.content.checked === true] as const] : [];
  }));
  const places = blockPlaces(graph, blockIds);
  const scope = `${adapter.workspaceId}/${sourcePageId}/${session.user.id}`;
  const key = `move-blocks-into:${blockIds[0]}:${Date.now()}`;
  const rows = createDatabaseRowPageService(session, () => {});
  const moves = createBlockMoveService(session);
  // The database's source, as drawn here or read behind the press.
  const known = databaseSourceShown(store, database);
  let source = known;
  // The page each block goes in as: its own, one it is made into, or one
  // made around it, by the IDs given those (new each time it goes in).
  const freshWrappers = () => new Map(wrapped.map((id) => [id, newId()]));
  const rowPagesOf = (wrappers: ReadonlyMap<string, string>) => blockIds.map((id) => {
    const block = blockOf(id);
    if (isPageBlock(block)) {
      return { first: false, page: context.pages.find((page) => page.id === id) ?? { id, title: String(block?.content.childPageTitle ?? "") } };
    }
    const title = turned.includes(id) && block ? adapter.text(block) : "";
    return { first: true, page: { id: wrappers.get(id) ?? id, title, createdBy: session.user.id, lastEditedBy: session.user.id } };
  });
  // The blocks as drawn before the move, which its ⌘Z shows back (showBlocksBack),
  // and the lines made pages as they went in, which the page's history takes back.
  const drawnBlocks = blocksDrawn(graph, blockIds);
  const asTurned = new Map<string, GraphBlock>();
  const leaving = withPagesUnder(context.pages, blockIds.filter((id) => isPageBlock(blockOf(id))));
  const listed = context.pages.filter(({ id }) => leaving.has(id));
  const listPages = (shown: boolean) => {
    if (listed.length) options.setData?.((current) => ({ ...current, pages: [...current.pages.filter(({ id }) => !leaving.has(id)), ...(shown ? listed : [])] }));
  };
  // The rows the blocks went in as, in the order they went in, each with the
  // block a page was made around (still a page here until it went in), and
  // the checkbox the to-dos may have made.
  const rowIds: { rowId: string; wrapped?: string; inDatabase: boolean }[] = [];
  let checkbox = "";
  const flush = async () => {
    if (sync && !(await sync.flush())) throw new Error("The page's changes could not be saved.");
  };
  // The title's caret is asked for as what moves them begins (`land`), what
  // is typed till then typed there (caretOnceAnswered).
  const caretAtTitle = (land: CaretLanding) => land({ key: `title:${sourcePageId}`, position: "start" });
  // Once begun it goes on to the end, so no block is left half moved; the
  // first time, pages made around blocks take the IDs their rows were shown by.
  const goIn = async (pageIds?: ReadonlyMap<string, string>) => {
    if (!source) throw new Error("The database was not found.");
    await flush();
    checkbox = newId();
    for (const id of blockIds) {
      const row = wrapped.includes(id) ? { rowId: await wrapInPage(id, pageIds?.get(id)), wrapped: id, inDatabase: false }
        : { rowId: id, inDatabase: false };
      if (row.wrapped) rowIds.push(row);
      const checked = checks.get(id);
      await movePageIntoDatabase(session, row.rowId, {
        requestId: newId(), dataSourceId: source.id, ...(row.wrapped || turned.includes(id) ? { first: true } : {}),
        ...(checked === undefined ? {} : { checked, checkedPropertyId: checkbox }),
      });
      row.inDatabase = true;
      if (!row.wrapped) rowIds.push(row);
    }
  };
  // A block without words goes in a new untitled page made where it stands.
  const wrapInPage = async (blockId: string, pageId = newId()) => {
    const block = blockOf(blockId)!;
    const siblings = editorBlockChildren(graph, block.parentId);
    const before = siblings.slice(0, siblings.findIndex((candidate) => candidate.id === blockId)).reverse()
      .find((candidate) => !blockIds.includes(candidate.id));
    const wrapper: GraphBlock = {
      id: pageId, parentId: block.parentId, afterId: before?.id ?? null, position: block.position,
      type: "child_page", content: { childPageId: pageId, childPageTitle: "" },
    };
    const operations: GraphOperation[] = [
      { kind: "upsertBlock", block: wrapper },
      { kind: "moveBlock", move: { blockId, parentId: pageId, afterId: null } },
    ];
    await sendContentGraphChange(session, sourcePageId, operations);
    return pageId;
  };
  // Each row comes out as a page under this page, last, and goes where its
  // block stood; a page made around a block gives it back and goes.
  const goBack = async () => {
    const made: string[] = [];
    while (rowIds.length > 0) {
      const { rowId, wrapped: blockId, inDatabase } = rowIds[rowIds.length - 1];
      if (inDatabase) await rows.move(rowId, { kind: "page", id: sourcePageId });
      if (blockId) {
        await moves.move({ blockId, destinationPageId: sourcePageId, sourcePageId: rowId });
        made.push(rowId);
      }
      rowIds.pop();
    }
    for (const place of places) await sendContentGraphChange(session, sourcePageId, [{ kind: "moveBlock", move: place }]);
    if (made.length > 0) {
      await sendContentGraphChange(session, sourcePageId, made.map((blockId) => ({ kind: "deleteBlock", deletion: { blockId } })));
    }
    // The checkbox the to-dos made goes with them for good, not into the
    // database's deleted properties (Notion's, seen 2026-11-11); one they found stays.
    if (checks.size > 0 && source) {
      const { pageId, id } = source;
      await sendContentGraphChange(session, pageId, [{ kind: "deleteProperty", propertyDeletion: { id: checkbox } }])
        .then(() => createDatabaseDeletedPropertiesSurface(session, pageId, id, () => undefined).deletePermanently(checkbox))
        .catch(() => undefined);
    }
  };
  const settle = async () => {
    await sync?.reread();
    await context.onPagesChanged?.();
  };
  // Going in, at the press: the blocks leave the page — before those with
  // words become pages, which then draw as gone at once, not as pages for a
  // frame — a page's block the sidebar with the pages under it, and the
  // rows stand in the database once its source is known; let go once the
  // server moved them and the page was read again, or refused.
  // What the last step going in still shows till the page is read again: a step coming back before then lets
  // it go, or its rows and theirs redrew the page against each other with no end (2026-11-22: a line moved
  // into a database, then ⌘Z at once, overflowed the stack; showBlocksGone's blocks yield the same way).
  let goingIn: { moved: () => void } | null = null;
  const showGoingIn = (wrappers: ReadonlyMap<string, string>) => {
    const gone = showBlocksGone(store, adapter, blockIds);
    listPages(false);
    let holds: ((refused?: boolean) => void)[] = [];
    const showRows = (dataSourceId: string) => {
      holds = rowPagesOf(wrappers).map(({ page, first }) => holdRowsShown(store, withPageAsRow(page, dataSourceId, first), withoutRows(new Set([page.id]))));
    };
    if (source) showRows(source.id);
    const shown = {
      showRows: (dataSourceId: string) => { if (!holds.length) showRows(dataSourceId); },
      moved: () => { gone?.moved(); for (const release of holds) release(); },
      refused: () => { gone?.refused(); for (const release of holds) release(true); listPages(true); },
    };
    goingIn = shown;
    return shown;
  };
  // Coming back, at the press of ⌘Z or "실행 취소": the blocks stand where
  // they did, as drawn, the pages made around them on the way not drawn, the
  // rows leave the database, and a page's block is back in the sidebar
  // (user decision 2026-11-12: a press answers on the next frame — they came
  // back once the server had moved each back, 0.7 s on a NAS). Let go once
  // the server has them back, before the page is read again: a line made a
  // page goes back to a line after this, by the page's own history.
  // Lines made pages are let go as the pages they went as (`asTurned`),
  // which the page's own history then turns back into lines in the same
  // task (showBlocksBack): let go before the page was read again, the read
  // drew them as pages a frame before the history's step, a timer later.
  const showComingBack = () => {
    goingIn?.moved();
    goingIn = null;
    const rows = rowsDrawn(store, rowIds.map(({ rowId }) => rowId));
    const release = showBlocksBack(store, adapter, drawnBlocks, places, new Set(rowIds.flatMap(({ rowId, wrapped }) => wrapped ? [rowId] : [])),
      { shown: withoutRowsWhereDrawn(rows), undo: withRowsAsDrawn(rows) });
    listPages(true);
    let back = true;
    return {
      moved: () => { if (!back) return; back = false; release(turned.length ? drawnBlocks.map((block) => asTurned.get(block.id) ?? block) : false); },
      refused: () => { if (back) listPages(false); back = false; release(true); },
    };
  };
  const step = async (forward: boolean) => {
    // The caret it leaves, asked for at its key, by the rule a move to another page keeps (askMovedOffCaret).
    const asked = askMovedOffCaret(forward, !chosen);
    const wrappers = forward ? freshWrappers() : new Map<string, string>();
    const shown = forward ? showGoingIn(wrappers) : showComingBack();
    try {
      if (forward) await goIn(wrappers);
      else {
        await goBack();
        // Lines made pages stay shown as lines till the page's history takes them back (asTurned).
        if (!turned.length) shown.moved();
      }
      await sync?.reread();
      if (forward) await context.onPagesChanged?.();
      else void context.onPagesChanged?.();
      // Lines made pages go back to lines by the page's own history after
      // this, against the page as drawn: drawn as the server holds them
      // (pages), or the history, finding them lines as shown, left them pages
      // (seen 2026-11-13).
      shown.moved();
      landMovedOffCaret(asked, caretAtTitle, chosen ? { pageId: sourcePageId, blockIds } : undefined);
      return true;
    } catch {
      asked.land?.(null);
      shown.refused();
      context.onStatus?.(i18next.t("blockMoveToDialog:toast.moveFailed"), "error");
      await sync?.reread();
      return false;
    }
  };
  // One ⌘Z from the moment it starts: one pressed before the blocks are in
  // waits for them (landingNativeEditorCommand). Blocks made pages first go
  // back with it, or alone when the move didn't happen. All of it shows at
  // the press (showGoingIn). The move, its ⌘Z and its ⌘⇧Z leave the caret
  // by the rule a move to another page keeps (blockMoveCaret.ts): gone in or
  // done again, at the start of the page's title when a line held it, none
  // for chosen blocks; taken back, none, chosen blocks chosen again. A line's
  // ⌘Z took the caret to the title before (2026-11-24).
  const asked = askMovedOffCaret(true, !chosen);
  const wrappers = freshWrappers();
  const shown = showGoingIn(wrappers);
  const command = turned.length > 0 ? landingNativeEditorCommand(() => Promise.resolve(true)) : null;
  const started = command ? null : startNativeEditorCommand(scope, key);
  if (command) {
    adapter.changeType(turned, "child_page");
    for (const block of blocksDrawn(adapter.graph, turned)) asTurned.set(block.id, block);
    joinCommittedNativeEditorCommand(scope, command.step);
  }
  const moving = (async () => {
    // One a page's body holds is no page of the reader's: it is chosen by its database's ID.
    source ??= await fullPageDatabaseSource(session, database, undefined, !context.pages.some(({ id }) => id === database.id));
    if (!source) throw new Error("The database was not found.");
    shown.showRows(source.id);
    await goIn(wrappers);
  })();
  let dismiss: (() => void) | void = undefined;
  void moving.then(async () => {
    command?.land(step);
    started?.land(step);
    // Read again with the blocks gone and the rows in, nothing is held any more.
    await settle().catch(() => undefined);
    shown.moved();
  }, async () => {
    command?.fail();
    started?.fail();
    shown.refused();
    dismiss?.();
    context.onStatus?.(i18next.t("blockMoveToDialog:toast.moveFailed"), "error");
    // Those that went in come back; blocks made pages stay pages here.
    await goBack().catch(() => undefined);
    await settle().catch(() => undefined);
  });
  const page = blockIds.length === 1 && isPageBlock(blockOf(blockIds[0]))
    ? context.pages.find((candidate) => candidate.id === blockIds[0]) : undefined;
  return () => {
    landMovedOffCaret(asked, caretAtTitle);
    if (!page || !options.notice) return;
    const place = pageNamesNow.title(database);
    const message = page.title.trim()
      ? i18next.t("moveToDialog:toast.movedTo", { title: page.title, place })
      : i18next.t("moveToDialog:toast.movedCountTo", { count: 1, place });
    // Pressed before the server moved it, "실행 취소" waits for it.
    dismiss = options.notice(message, page.id, () => void moving.then(() => step(false), () => false).then((done) => {
      if (done) dropNativeEditorCommand(scope, key);
    }));
  };
}
