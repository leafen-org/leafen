import { adoptNativeEditorCommandBlocks, startNativeEditorCommand } from "./nativeEditorUndoOwner";
import { choosePageBlocks } from "./pageUndo";
import { moveCutPage } from "./cutPagePaste";
import { collectContentGraphStructure } from "@/lib/authenticatedContentGraph";
import { restoreDeletedDatabase } from "@/lib/deletedDatabases";
import { api } from "@/api";
import { copyBlockTexts } from "@/collaboration";
import { removeBlockTexts } from "@/lib/blockTextEditing";
import { graphTextBlockTypes, type ContentGraph, type GraphBlock, type GraphBlockType, type GraphOperation, type GraphViewType } from "@/contentGraph";
import { i18next } from "@/i18n";
import { rememberCalloutChange, unwrapEditorCallouts, wrapEditorBlocksInCallouts } from "./calloutGraphModel";
import {
  changeEditorBlocksToTables, changeEditorBlocksToTabs, changeEditorBlockTypes, duplicateEditorBlocks,
  editorSelectionOwnsDatabase, editorSelectionRoots, insertEditorBlock, insertEditorChildBlock,
  insertEditorTabBlock, insertEditorTableBlock, NEW_TABLE_SIZE, insertEditorTabItem,
  turnEditorBlockIntoNewTabs, moveEditorBlock, removeEditorBlocks, replaceEditorBlocks, updateEditorBlock,
} from "./editorGraphModel";
import { makesViewsOfSourcesElsewhere } from "./blockViewCopies";
import { newSimpleTableWidths } from "./simpleTableColumns";
import { changeToToggleHeading, toggleHeadingType } from "./toggleHeadingModel";
import { createBlockMoveService } from "@/lib/blockMoveService";
import { requestDatabaseStartPanel } from "@/lib/databaseStartPanel";
import { markFreshDatabase } from "@/lib/freshDatabases";
import {
  showFullPageDatabaseHere,
  showInlineDatabaseTurnedHere,
  startFullPageDatabase,
  turnFullPageDatabaseInline,
  turnInlineDatabaseIntoPage,
} from "@/lib/fullPageDatabase";
import { nextPaint } from "@/lib/afterPress";
import type { PageGraphStore } from "@/lib/pageGraphStore";
import { whileDatabasePageTurns } from "@/lib/turningDatabasePages";
import { inlineDatabaseDrawn, showDatabaseTurnedBackHere } from "@/lib/databaseTurnedBackHere";
import { duplicateNativePage } from "@/lib/pageDuplicationService";
import { hrefOfPageMadeIn } from "@/lib/pagePeek";
import { pushRoute } from "@/lib/router";
import { holdTypingWhile, requestPageTitleFocus } from "@/lib/pageTitleFocus";
import { openPageMadeHere, showPagesMadeHere, type PagesMadeHere } from "./editorPagesMadeHere";
import { selectedRootsInOrder } from "./blockSelectionKeyboard";
import { tidyColumns, type EditorGraphChange } from "./columnLayoutModel";
import { blockItemText } from "./blockItemLabels";
import { TOGGLE_BLOCK_TYPES } from "@/lib/blockSemantics";
import { insertEditorSyncedBlock, syncedLoneDuplicate } from "./syncedBlockModel";
import type {
  EditorBlockSpot,
  EditorCommit,
  EditorDatabaseAdapters,
  EditorGraphSync,
  EditorMoveContext,
  NativeEditorGraphAdapter,
} from "./editorRuntimeTypes";
import type * as Y from "yjs";
import type { Session } from "@/types";
import type { BlockPickerSnapshot } from "./blockPickerModel";
import { databaseStandingNow, noteDatabaseTurned } from "./databaseTurnStanding";

const textBlockTypes = new Set<string>(graphTextBlockTypes);

/** A page made from a line that becomes a full-page database, named after the line. */
type DatabaseStart = { title: string; view?: GraphViewType };

export type NativeEditorGraphAdapterInput = {
  pageId: string;
  workspaceId: string;
  session: Session | null;
  generation: number;
  signal: AbortSignal;
  graph: ContentGraph;
  readOnly: boolean;
  onReadOnlyToggleChange?: (block: GraphBlock, collapsed: boolean) => void;
  fixedBody?: boolean;
  collaborationDocument: Y.Doc | null;
  mentionSnapshot?: BlockPickerSnapshot | null;
  database: EditorDatabaseAdapters;
  moveContext?: EditorMoveContext;
  pagePlaceholderBlockId: string | null;
  graphSync?: EditorGraphSync;
  commit: EditorCommit;
  changeText: (block: GraphBlock, text: string) => void;
  isCurrent: () => boolean;
};

/** A sub-page's block: a page block named by its own ID. */
export function isPageBlock(block: GraphBlock | undefined): boolean {
  return block?.type === "child_page" && block.content?.childPageId === block.id;
}

/** Whether blocks are copied on the server, as pages and databases are. */
export function copiesOnServer(graph: ContentGraph, blockIds: readonly string[]) {
  return editorSelectionRoots(graph, blockIds).some((block) => isPageBlock(block)) || editorSelectionOwnsDatabase(graph, blockIds);
}

/** Whether operations make, move, delete or bring back page blocks of a graph. */
function operationsChangePages(graph: ContentGraph, operations: readonly GraphOperation[]) {
  const byId = new Map(graph.blocks.map((block) => [block.id, block]));
  const pageBelow = (id: string): boolean => isPageBlock(byId.get(id))
    || graph.blocks.some((block) => block.parentId === id && pageBelow(block.id));
  return operations.some((operation) => {
    if (operation.kind === "upsertBlock") return isPageBlock(operation.block) || isPageBlock(byId.get(operation.block.id));
    if (operation.kind === "moveBlock") return isPageBlock(byId.get(operation.move.blockId));
    return operation.kind === "deleteBlock" && pageBelow(operation.deletion.blockId);
  });
}

export function createNativeEditorGraphAdapter(input: NativeEditorGraphAdapterInput): NativeEditorGraphAdapter {
  // Each change is computed from the graph after this adapter's earlier
  // changes, so several changes in one event build on one another.
  let current = input.graph;
  let graphListener: ((before: ContentGraph, after: ContentGraph, operations: readonly GraphOperation[]) => void) | null = null;
  let fillListener: ((blockId: string, type: GraphBlockType, content: Record<string, unknown>) => void) | null = null;

  function blockText(block: GraphBlock) {
    return input.collaborationDocument
      ? input.collaborationDocument.getText(`block:${block.id}`).toString()
      : String(block.content.text ?? "");
  }

  /** A new tab block's tabs are named "Tab 1", "Tab 2"… in the reader's language. */
  function nameNewTabs(items: readonly GraphBlock[]) {
    items.forEach((item, index) => input.changeText(item, blockItemText("tabs.newNamed", { number: index + 1 })));
  }

  function canWrite() {
    return !input.readOnly && !input.signal.aborted && input.isCurrent();
  }

  function commit(
    next: ContentGraph, operations: readonly GraphOperation[], openPageId?: string, database?: DatabaseStart,
  ) {
    return send(next, withoutStaleAnchors(operations), openPageId, database);
  }

  // An existing block's upsert changes the block where it is. The block it
  // was made after is no anchor once it is made, and the server moves a
  // block to follow the anchor its upsert names; moves say where to go.
  function withoutStaleAnchors(operations: readonly GraphOperation[]) {
    const existing = new Set(current.blocks.map((block) => block.id));
    return operations.map((operation) => operation.kind === "upsertBlock" && operation.block.afterId
      && existing.has(operation.block.id)
      ? { ...operation, block: { ...operation.block, afterId: null } }
      : operation);
  }

  function send(
    next: ContentGraph, operations: readonly GraphOperation[], openPageId?: string, database?: DatabaseStart,
    recordHistory = true,
  ) {
    if (!canWrite() || operations.length === 0) return false;
    // Besides blocks, a change may make a new database with its block, as a
    // simple table turned into one; undo turns back the blocks alone.
    const known = new Set([...current.dataSources, ...current.views, ...current.properties, ...current.rows].map((item) => item.id));
    const made = (operation: GraphOperation) => { const item = operation.kind === "upsertDataSource" ? operation.dataSource
      : operation.kind === "upsertView" ? operation.view : operation.kind === "upsertProperty" ? operation.property
      : operation.kind === "upsertRow" ? operation.row : undefined; return !!item && !known.has(item.id); };
    if (operations.some((operation) => operation.kind !== "upsertBlock"
      && operation.kind !== "moveBlock" && operation.kind !== "deleteBlock" && !made(operation))) {
      throw new TypeError("Native editor adapter accepts block graph operations and new databases only.");
    }
    const changesPages = operationsChangePages(current, operations);
    const previous = current;
    // A native content edit names what this writer saw. The server merges
    // changed fields against its current authority, including offline peers.
    const before = new Map(previous.blocks.map(block => [block.id, block]));
    operations = operations.map(operation => {
      if (operation.kind !== "upsertBlock" || operation.block.baseContent) return operation;
      const old = before.get(operation.block.id);
      return old && old.type === operation.block.type
        ? { ...operation, block: { ...operation.block, baseContent: old.content } } : operation;
    });
    current = next;
    if (recordHistory) graphListener?.(previous, next, operations);
    const document = input.collaborationDocument;
    if (recordHistory && document && operations.some((operation) => operation.kind === "deleteBlock")) {
      const present = new Set(next.blocks.map((block) => block.id));
      removeBlockTexts(document, previous.blocks.filter((block) => !present.has(block.id)).map((block) => block.id));
    }
    // A block going whose text this page has taken (above, or by the Undo
    // stepping its text first) keeps the server off that text: it went in the
    // collaborative text at once, and a ⌘Z put back before the slower
    // deletion reached the server was emptied by it (2026-11-08).
    if (document) operations = operations.map((operation) => operation.kind === "deleteBlock"
      && !document.getText(`block:${operation.deletion.blockId}`).length
      ? { ...operation, deletion: { ...operation.deletion, keepText: true } } : operation);
    // Blocks made, removed or moved go to the server soon (EditorCommit).
    const reshapes = operations.some((operation) => operation.kind === "deleteBlock" || operation.kind === "moveBlock"
      || (operation.kind === "upsertBlock" && before.get(operation.block.id)?.parentId !== operation.block.parentId));
    input.commit(next, operations, { reshapes });
    // A linked view pasted from a page whose source this one doesn't hold reads
    // the page again once saved, the source's rows with it (blockViewCopies.ts).
    if (makesViewsOfSourcesElsewhere(previous, operations)) void input.graphSync?.flush().then((saved) => saved && input.graphSync?.reread());
    if (changesPages) {
      showPageTrash(previous, next);
      followPages(openPageId, database, database ? null : showPagesMade(previous, next, operations));
    }
    return true;
  }

  // A page whose block goes from the body goes to the trash with it, and
  // comes back with it, in the page list at once (user decision 2026-11-12:
  // a press answers on the next frame); the list read once the server has
  // the change (followPages) brings its trash time, or puts it back.
  function showPageTrash(before: ContentGraph, after: ContentGraph) {
    const context = input.moveContext;
    if (!context) return;
    const had = new Set(before.blocks.filter(isPageBlock).map((block) => block.id));
    const has = new Set(after.blocks.filter(isPageBlock).map((block) => block.id));
    const now = new Date().toISOString();
    for (const page of context.pages) {
      const goes = had.has(page.id) && !has.has(page.id) && !page.inTrash;
      if (goes || (has.has(page.id) && !had.has(page.id) && page.inTrash)) {
        context.onPageChange({ ...page, inTrash: goes, trashedAt: goes ? now : undefined });
      }
    }
  }

  // A synced block duplicated alone is a copy of its original (syncedLoneDuplicate).
  const referenceAlone = (block: GraphBlock) => syncedLoneDuplicate(block, input.pageId, current.blocks);
  // Copied blocks, put where a change places them, take the text of the
  // blocks they copy once they are made.
  function commitCopies(copied: ReturnType<typeof duplicateEditorBlocks>, placed?: EditorGraphChange) {
    const operations = placed ? [...copied.operations, ...placed.operations] : copied.operations;
    if (!commit(placed?.graph ?? copied.graph, operations)) return [];
    const document = input.collaborationDocument;
    if (document) {
      copyBlockTexts(document, copied.copied.filter(({ source }) => textBlockTypes.has(source.type))
        .map(({ source, copyId }) => ({ sourceId: source.id, copyId })));
    }
    return copied.blocks;
  }

  // The server makes, trashes, restores and moves pages as their blocks'
  // changes are saved; the page list follows once they are. Pages a change
  // makes show at once, one made to be opened opening on the next frame
  // (made); a full-page database opens once the server made and started it,
  // and one that couldn't become a database still opens, as an empty page.
  function followPages(openPageId?: string, database?: DatabaseStart, made?: PagesMadeHere | null) {
    const refresh = input.moveContext?.onPagesChanged;
    const sync = input.graphSync;
    if (!refresh || !sync) return;
    // A full-page database shows and opens at once, as made here (fullPageDatabase.ts).
    if (openPageId && database && input.session && input.moveContext) made = showFullPageDatabaseHere({ session: input.session,
      context: input.moveContext, parentId: input.pageId, workspaceId: input.workspaceId, pageId: openPageId, start: database,
      flush: () => sync.flush(), refresh });
    if (made && (!openPageId || made.opening.has(openPageId))) {
      if (openPageId) openPageMadeHere(made, openPageId, input.pageId, () => input.isCurrent() && !input.signal.aborted, Boolean(database));
      return;
    }
    const follow = async () => {
      // One the server didn't take reads the list as it is, which puts back what showed ahead of it.
      if (!(await sync.flush())) { await refresh(); return; }
      if (openPageId && database && input.session) {
        const started = await startFullPageDatabase(input.session, openPageId, database.title, [], database.view, input.signal)
          .catch(() => {
            input.moveContext?.onStatus?.(i18next.t("syncStatusBadge:status.databaseStartFailed"), "error");
            return null;
          });
        // Its page opens with Notion's "새 데이터베이스" panel, this once
        // (seen 2026-11-04: not again on coming back, closed or not), its
        // toolbar only settings and "새로 만들기" till it first changes, as an
        // inline one's (seen 2026-11-06). The panel's first field takes the
        // keys, the page's title not (Notion's, measured 2026-11-15 in Chrome
        // at 1440 with no script in the page: a typed "z" went into the
        // panel's first field, Notion's AI field being an approved difference,
        // and nowhere after Esc).
        if (started?.databaseBlockId) requestDatabaseStartPanel(started.databaseBlockId);
        if (started?.dataSourceId) markFreshDatabase(started.dataSourceId, started.databaseProperties);
      }
      await refresh();
      if (openPageId && input.isCurrent() && !input.signal.aborted) {
        requestPageTitleFocus(openPageId);
        pushRoute(hrefOfPageMadeIn(input.pageId, openPageId, Boolean(database)));
      }
    };
    // What is typed while the page is being made goes into its title, not
    // into this page.
    void (openPageId ? holdTypingWhile(follow) : follow()).catch(() => {});
  }

  // The pages this change makes show at once (editorPagesMadeHere.ts); a
  // full-page database still opens once the server has made and started it.
  function showPagesMade(before: ContentGraph, after: ContentGraph, operations: readonly GraphOperation[]) {
    const context = input.moveContext, session = input.session, sync = input.graphSync, refresh = context?.onPagesChanged;
    if (!context || !session || !sync || !refresh) return null;
    return showPagesMadeHere({ session, context, pageId: input.pageId, workspaceId: input.workspaceId, before, after, operations,
      isPage: isPageBlock, flush: () => sync.flush(), refresh });
  }

  // A database turned into a page or inline moves between pages on the
  // server; the body and the page list are read again once it has.
  function turnDatabase(blockId: string, turn: "page" | "inline", store?: PageGraphStore | null) {
    const context = input.moveContext;
    const status = context?.onStatus;
    const block = current.blocks.find((candidate) => candidate.id === blockId);
    const session = input.session;
    const sync = input.graphSync;
    if (!session || !context || !sync || !block || !canWrite()) {
      status?.(blockItemText("database.turnFailed"), "error");
      return;
    }
    const moves = createBlockMoveService(session);
    // Into a page, it shows so at once where the open page's graph is at hand
    // (fullPageDatabase.ts), and so does the ⌘⇧Z that turns it again.
    const showTurned = (database: GraphBlock) => {
      const source = (store?.get() ?? current).dataSources.find((candidate) => candidate.id === database.content.dataSourceId);
      const shown = store ? showInlineDatabaseTurnedHere({ session, context, store, pageId: input.pageId,
        workspaceId: input.workspaceId, database, title: source?.title ?? "", sourceTitle: source?.title ?? "" }) : null;
      if (shown) void nextPaint().then(() => choosePageBlocks(input.pageId, [shown.made.pageId]));
      return shown;
    };
    // The database as drawn before it turned into a page, which turning it
    // back shows at once where its page's own isn't held (databaseTurnedBackHere.ts).
    const drawnNow = (id: string) => {
      const graph = store?.get() ?? current, database = graph.blocks.find((candidate) => candidate.id === id);
      return database?.type === "inline_database" ? inlineDatabaseDrawn(graph, database) : null;
    };
    let drawn = turn === "page" ? drawnNow(blockId) : null;
    let here = turn === "page" ? showTurned(block) : null;
    // One way or the other; what stands for the database after it is what the way back turns.
    const go = async (way: "page" | "inline", id: string, choose: boolean) => {
      // Inline, the database stands where its page's link was at once.
      let back = way === "inline" && store ? showDatabaseTurnedBackHere({ principalId: session.user.id, store,
        pageId: input.pageId, pageBlockId: id, drawn, here: input.isCurrent }) : null;
      // Chosen as a ⌘Z or ⌘⇧Z leaves it; the menu's turn leaves it unchosen (below).
      if (back && choose) { const shown = back.databaseId; void nextPaint().then(() => choosePageBlocks(input.pageId, [shown])); }
      // Into a page, a ⌘Z or ⌘⇧Z as the menu's turn.
      // The page as drawn now: a ⌘Z's adapter may have read none of the turns since.
      if (way === "page" && !here) {
        const database = (store?.get() ?? current).blocks.find((candidate) => candidate.id === id);
        drawn = drawnNow(id) ?? drawn;
        if (database?.type === "inline_database") here = showTurned(database);
      }
      try {
        return await turnedWay(way, id, choose, () => { back?.release(); back = null; });
      } catch (error) {
        // Refused, the page shows what stood before the press.
        back?.release(true);
        if (way === "page") { here?.release(true); here = null; }
        throw error;
      }
    };
    const turnedWay = async (way: "page" | "inline", id: string, choose: boolean, readBack: () => void) => {
      if (!(await sync.flush())) throw new Error("The page's changes could not be saved.");
      // The way back comes from a later graph than this adapter's.
      let graph: ContentGraph = current;
      if (id !== blockId) graph = (await collectContentGraphStructure(session, input.pageId))?.graph ?? graph;
      const placed = graph.blocks.find((candidate) => candidate.id === id);
      if (!placed) throw new Error("The database is gone.");
      // The page shows none of the steps between: its reads wait for them
      // (2026-11-11: the page's link showed as "새 페이지" before it read as
      // its database). Where the turn leaves a page's link, the page list is
      // read first, so the link reads as what it is now when the page shows
      // it; where it takes the link away, the page first, before the list
      // has its page in the trash.
      const turned = async () => {
        const made = await (sync.holding ?? ((task) => task()))(async () => {
          if (way === "inline") return turnFullPageDatabaseInline({ session, pageId: input.pageId, pageBlock: placed, moves });
          const source = graph.dataSources.find((candidate) => candidate.id === placed.content.dataSourceId);
          const shown = here;
          const page = await turnInlineDatabaseIntoPage({
            session, pageId: input.pageId, workspaceId: input.workspaceId, database: placed,
            title: source?.title ?? "", moves, sourceTitle: source?.title ?? "", made: shown?.made, onPageMade: shown?.pageMade,
          });
          await context.onPagesChanged?.();
          return page;
        });
        await sync.reread();
        readBack();
        here?.release();
        here = null;
        if (way === "inline") await context.onPagesChanged?.();
        return made;
      };
      const made = way === "inline" ? await whileDatabasePageTurns(id, turned) : await turned();
      noteDatabaseTurned(id, made); if (choose) choosePageBlocks(input.pageId, [made]);
      return made;
    };
    // No word while it turns or once it has, as Notion's (seen 2026-11-06).
    // The page it became stays chosen; the database brought inline from the
    // menu does not, though ⌘Z either way leaves it chosen (seen 2026-11-05·06).
    // A step of the page's ⌘Z from the moment it starts, as Notion's (seen 2026-11-05).
    const command = startNativeEditorCommand(`${input.workspaceId}/${input.pageId}/${session.user.id}`, `turn-database:${blockId}:${Date.now()}`);
    void go(turn, blockId, turn === "page").then((made) => {
      let way = turn, standing = made;
      command.land(async () => {
        // Turned again since by another command, its block now stands here, and that block's kind says the way back.
        standing = databaseStandingNow(standing);
        const now = (store?.get() ?? current).blocks.find((candidate) => candidate.id === standing);
        const back = (now ? now.type !== "inline_database" : way === "page") ? "inline" : "page";
        try { standing = await go(back, standing, true); way = back; return true; } catch {
          status?.(blockItemText("database.turnFailed"), "error");
          return false;
        }
      });
    }).catch(async (error: unknown) => {
      command.fail();
      here?.release(true);
      here = null;
      if (error instanceof DOMException && error.name === "AbortError") return;
      status?.(blockItemText("database.turnFailed"), "error");
      await sync.reread();
      await context.onPagesChanged?.();
    });
  }

  // A block the server makes or brings back here goes where it is wanted
  // before the page reads it, the page's reads waiting meanwhile, so it first
  // shows there: a paste's database copies showed at the page's end, then
  // went to their places (2026-11-08). A copy is made there (`at`); what
  // comes back to the end moves after. Where it stays when it can't, the
  // paste moves it.
  async function serverBlock(sync: EditorGraphSync, make: (at: EditorBlockSpot | null) => Promise<string | null>,
    spot?: (blockId: string | null) => EditorBlockSpot | null, madeInPlace = false) {
    const made = await (sync.holding ?? ((task) => task()))(async () => {
      const at = madeInPlace ? spot?.(null) ?? null : null;
      const id = await make(at), where = id ? spot?.(id) : null;
      const placed = !!at && !!where && at.parentId === where.parentId && at.afterId === where.afterId;
      if (id && where && !placed && sync.place && canWrite()) await sync.place(id, where).catch(() => false);
      return id;
    });
    if (made) await sync.reread();
    return made;
  }

  return {
    observeLocalGraphChanges(listener) {
      graphListener = listener;
      return () => { if (graphListener === listener) graphListener = null; };
    },
    localGraphChanged(before, after, operations) { graphListener?.(before, after, operations); },
    pageId: input.pageId,
    workspaceId: input.workspaceId,
    session: input.session,
    generation: input.generation,
    signal: input.signal,
    get graph() {
      return current;
    },
    readOnly: input.readOnly,
    fixedBody: input.fixedBody ?? false,
    collaborationDocument: input.collaborationDocument,
    mentionSnapshot: input.mentionSnapshot ?? null,
    database: input.database,
    moveContext: input.moveContext,
    pagePlaceholderBlockId: input.pagePlaceholderBlockId,
    graphSync: input.graphSync,
    isCurrent: input.isCurrent,
    text(block) {
      // A read-only page with its collaborative document reads the text there.
      if (!input.collaborationDocument) return String(block.content.text ?? "");
      return input.collaborationDocument.getText(`block:${block.id}`).toString();
    },
    changeText(block, text) {
      if (canWrite()) input.changeText(block, text);
    },
    updateBlock(block, content) {
      const result = updateEditorBlock(current, placedAsShown(current, block), content);
      if (commit(result.graph, result.operations)) rememberCalloutChange(block, content);
    },
    fillBlock(block, content) {
      const result = updateEditorBlock(current, placedAsShown(current, block), content);
      if (send(result.graph, withoutStaleAnchors(result.operations), undefined, undefined, false)) fillListener?.(block.id, block.type, content);
    },
    observeFilledBlocks(listener) {
      fillListener = listener;
      return () => { if (fillListener === listener) fillListener = null; };
    },
    setToggleCollapsed(block, collapsed) {
      const live = current.blocks.find((candidate) => candidate.id === block.id);
      if (!live || !TOGGLE_BLOCK_TYPES.has(live.type as never) || input.signal.aborted || !input.isCurrent()) return;
      if (input.readOnly) {
        input.onReadOnlyToggleChange?.(live, collapsed);
        return;
      }
      const result = updateEditorBlock(current, live, { collapsed });
      commit(result.graph, result.operations);
    },
    replaceBlocks(blocks) {
      const result = replaceEditorBlocks(current, blocks.map(block => placedAsShown(current, block)));
      commit(result.graph, result.operations);
    },
    changeType(requested, type: GraphBlockType, options) {
      // What becomes a callout goes inside one and a callout with no text of
      // its own lets its lines go, each staying the block it is (seen
      // 2026-11-05): the ids stand for what the change left.
      if (type === "callout") {
        const wrapped = wrapEditorBlocksInCallouts(current, requested);
        if (wrapped.ids.length && !commit(wrapped.graph, wrapped.operations)) return requested;
        return wrapped.ids;
      }
      const unwrapped = unwrapEditorCallouts(current, requested, blockText);
      if (unwrapped.operations.length) {
        const simple = !["tab", "table", "child_page"].includes(type) && !toggleHeadingType(type);
        if (!simple) {
          if (!commit(unwrapped.graph, unwrapped.operations)) return requested;
        } else {
          const turned = changeEditorBlockTypes(unwrapped.graph, unwrapped.turned, type);
          commit(turned.graph, [...unwrapped.operations, ...turned.operations]);
          return unwrapped.ids;
        }
      }
      const blockIds = unwrapped.operations.length ? unwrapped.ids : requested;
      if (type === "tab") {
        // An empty line starts a tab block as Notion's "/탭" does; a line with
        // text or blocks under it becomes a tab holding them.
        const only = blockIds.length === 1 ? current.blocks.find((block) => block.id === blockIds[0]) : undefined;
        if (only && !blockText(only).trim() && !current.blocks.some((block) => block.parentId === only.id)) {
          const fresh = turnEditorBlockIntoNewTabs(current, only.id);
          if (fresh && commit(fresh.graph, fresh.operations)) nameNewTabs(fresh.items);
          return blockIds;
        }
        const result = changeEditorBlocksToTabs(current, blockIds);
        if (!commit(result.graph, result.operations)) return blockIds;
        for (const transfer of result.transfers) {
          const label = blockText(transfer.source);
          input.changeText(transfer.item, label);
          input.changeText(transfer.source, "");
        }
        return blockIds;
      }
      if (type === "table") {
        const result = changeEditorBlocksToTables(current, blockIds, (id) => newSimpleTableWidths(id, NEW_TABLE_SIZE));
        commit(result.graph, result.operations);
        return blockIds;
      }
      // A block that becomes a toggle heading takes in what follows it, as
      // Notion's does from its Markdown and its "전환" (seen 2026-10-01).
      const toggleHeading = blockIds.length === 1 ? toggleHeadingType(type) : null;
      const turned = current.blocks.find((block) => block.id === blockIds[0]);
      if (toggleHeading && turned && !toggleHeadingType(turned.type)) {
        const result = changeToToggleHeading(current, turned.id, toggleHeading);
        if (result) {
          commit(result.graph, result.operations);
          return blockIds;
        }
      }
      const result = changeEditorBlockTypes(current, blockIds, type);
      // The title belongs to the text this writer chose, even when its
      // collaboration update reaches the server after the structure change.
      const operations = type === "child_page" ? result.operations.map(operation => {
        if (operation.kind !== "upsertBlock") return operation;
        const source = current.blocks.find(block => block.id === operation.block.id);
        return source?.type !== "child_page" ? {
          ...operation, block: { ...operation.block, content: {
            ...operation.block.content, childPageTitle: source ? blockText(source) : "",
          } },
        } : operation;
      }) : result.operations;
      const openPageId = type === "child_page" && options?.openPage ? blockIds[0] : undefined;
      // A full-page database is named after its line, as the page is.
      const database = openPageId && options?.database ? {
        title: input.collaborationDocument?.getText(`block:${openPageId}`).toString() ?? "",
        view: options.database.view,
      } : undefined;
      // A sub-page turned back into a line reads as the page's title.
      const unmade = type === "child_page" ? [] : current.blocks.filter((block) => blockIds.includes(block.id) && block.type === "child_page")
        .map((block) => ({ id: block.id, title: input.moveContext?.pages.find((page) => page.id === block.id)?.title
          ?? (typeof block.content.childPageTitle === "string" ? block.content.childPageTitle : "") }));
      if (!commit(result.graph, operations, openPageId, database)) return blockIds;
      for (const { id, title } of unmade) {
        const turned = current.blocks.find((block) => block.id === id);
        if (turned && title.trim()) input.changeText(turned, title.trim());
      }
      return blockIds;
    },
    insertAfter(afterId, type, content) {
      if (type === "synced_block") {
        const result = insertEditorSyncedBlock(current, afterId, content);
        return commit(result.graph, result.operations) ? result.block : null;
      }
      if (type === "tab") {
        const result = insertEditorTabBlock(current, afterId, content);
        if (!commit(result.graph, result.operations)) return null;
        nameNewTabs(result.items);
        return result.block;
      }
      if (type === "table") {
        const widths = content?.columnWidths ? undefined : newSimpleTableWidths(afterId, Number(content?.tableWidth) || NEW_TABLE_SIZE);
        const result = insertEditorTableBlock(current, afterId, { ...content, ...(widths ? { columnWidths: widths } : {}) });
        return commit(result.graph, result.operations) ? result.block : null;
      }
      const result = insertEditorBlock(current, afterId, type, content);
      return commit(result.graph, result.operations) ? result.block : null;
    },
    insertChild(parentId, type, content, after) {
      const result = insertEditorChildBlock(current, parentId, type, content, after);
      return commit(result.graph, result.operations) ? result.block : null;
    },
    insertTabItem(parentId, label) {
      const result = insertEditorTabItem(current, parentId);
      if (!commit(result.graph, result.operations)) return null;
      input.changeText(result.item, label);
      return { item: result.item };
    },
    duplicate(blockIds) {
      return commitCopies(duplicateEditorBlocks(current, blockIds, referenceAlone));
    },
    copyBlocks(blockIds, place) {
      if (copiesOnServer(current, blockIds)) return [];
      const copied = duplicateEditorBlocks(current, blockIds, referenceAlone);
      const placed = place(copied.graph, copied.blocks.map((block) => block.id));
      return placed ? commitCopies(copied, placed) : [];
    },
    async copyDatabaseBlock(source, spot) {
      const session = input.session, sync = input.graphSync;
      if (!session || !sync || !canWrite() || !await sync.flush()) throw new Error("The page's changes could not be saved.");
      return (await serverBlock(sync, async (at) => (await createBlockMoveService(session).copy({ blockId: source.blockId,
        destinationPageId: input.pageId, sourcePageId: source.pageId, signal: input.signal,
        ...(at?.afterId ? { placeAfter: at.afterId, placeParent: at.parentId ?? "" } : {}) })).targetBlockId, spot, true))!;
    },
    // A cut database pasted is the same database: it comes back from the
    // trash, then moves here. Notion's comes back so on its own page; on
    // another, Notion pastes a copy named "(1)" and leaves the original in the
    // trash, which Leafen moves instead so that links to it and its rows still
    // lead to it (seen 2026-11-07).
    async moveCutDatabaseBlock(source, spot) {
      const session = input.session, sync = input.graphSync;
      if (!session || !sync || !canWrite() || !await sync.flush()) return null;
      return serverBlock(sync, async () => {
        // The trash puts it back at the end of the page it was cut from.
        await restoreDeletedDatabase(session, source.workspaceId, source.blockId);
        if (source.pageId !== input.pageId) {
          await createBlockMoveService(session).move({ blockId: source.blockId, destinationPageId: input.pageId,
            sourcePageId: source.pageId, signal: input.signal });
        }
        return source.blockId;
      }, spot);
    },
    // A cut sub-page or database item moves here as a cut database does (cutPagePaste.ts).
    moveCutPageBlock(pageId, spot) {
      return moveCutPage({ session: input.session, sync: input.graphSync, pageId: input.pageId, workspaceId: input.workspaceId, signal: input.signal,
        canWrite, serverBlock: (make) => serverBlock(input.graphSync!, make, spot) }, pageId);
    },
    // A sub-page copied pastes as a copy of it under this page, as Notion's,
    // on its own page or another: "Plans (1)" (seen 2026-11-11), named so by
    // the duplication in its workspace. One in the trash, or not to be read,
    // is linked to instead.
    async copyPageBlock(pageId, spot) {
      const session = input.session, sync = input.graphSync;
      if (!session || !sync || !canWrite() || !await sync.flush()) return null;
      return serverBlock(sync, async () => {
        const page = await api.readPage(session, pageId, input.signal);
        if (page.inTrash) return null;
        const elsewhere = input.workspaceId && page.workspaceId !== input.workspaceId ? { workspaceId: input.workspaceId } : {};
        return (await duplicateNativePage(session, page, input.signal, { parentId: input.pageId, ...elsewhere })).targetRootPageId;
      }, spot);
    },
    async copyOnServer(blockIds, options) {
      const session = input.session;
      const sync = input.graphSync;
      const context = input.moveContext;
      if (!session || !sync || !canWrite()) return [];
      // The copies are a step of the page's history once they are here: a
      // ⌘Z meanwhile waits for them rather than take back the action before,
      // and takes them away as they come where they are the step itself
      // (`oneStep`, a duplicate's, as Notion's ⌘D).
      const scope = `${input.workspaceId}/${input.pageId}/${session.user.id}`, key = `copy-on-server:${blockIds[0]}:${Date.now()}`;
      const held = startNativeEditorCommand(scope, key);
      let adopted = false;
      try {
        if (!await sync.flush() || !canWrite()) return [];
        const roots = selectedRootsInOrder(current, new Set(blockIds));
        const pages = roots.map((root) => isPageBlock(root) ? context?.pages.find((page) => page.id === root.id) : undefined);
        if (!session || !sync || !canWrite() || roots.length === 0 || roots.some((root, index) => isPageBlock(root) && !pages[index])) return [];
        const service = createBlockMoveService(session);
        const copies: string[] = [];
        // A page is copied as its menu copies it, which shows the copy in the
        // list at once and reads the list once it is made (usePageMenuActions);
        // a full-page database's line without content, its database without
        // its pages, as its menu's "콘텐츠 제외 복제" (Notion's, seen 2026-11-25).
        const copyPage = context?.pageMenu?.onDuplicatePage;
        for (const [index, root] of roots.entries()) {
          const page = pages[index];
          const pageOptions = page?.kind === "database" && options?.withoutContent ? { withoutContent: true } : undefined;
          copies.push(page
            ? copyPage ? (await copyPage(page, input.signal, pageOptions)).id
              : (await duplicateNativePage(session, page, input.signal, pageOptions)).targetRootPageId
            : (await service.copy({ blockId: root.id, destinationPageId: input.pageId, sourcePageId: input.pageId, signal: input.signal,
              withoutContent: options?.withoutContent })).targetBlockId);
        }
        const read = sync.reread();
        if (options?.oneStep && copies.length) {
          // The page takes in what the server made as it hears of it, or once
          // it is read; a ⌘Z waiting takes them then, not after the reading.
          const here = () => copies.every((id) => current.blocks.some((block) => block.id === id));
          let readDone = false;
          void read.then(() => { readDone = true; }, () => { readDone = true; });
          for (let waited = 0; waited < 300 && !here() && !(readDone && waited > 100); waited++) {
            await new Promise((resolve) => setTimeout(resolve, 10));
          }
          adopted = adoptNativeEditorCommandBlocks(scope, key, copies);
          if (adopted) held.land(() => Promise.resolve(true));
        }
        await read;
        if (!copyPage || pages.some((page) => !page)) await context?.onPagesChanged?.();
        return copies;
      } finally { if (!adopted) held.fail(); }
    },
    remove(blockIds) {
      const sources = blockIds.map((id) => current.blocks.find((block) => block.id === id)?.parentId ?? "");
      const result = removeEditorBlocks(current, blockIds);
      const tidied = tidyColumns(result.graph, sources);
      commit(tidied.graph, [...result.operations, ...tidied.operations]);
    },
    move(blockId, parentId, afterId) {
      const source = current.blocks.find((block) => block.id === blockId)?.parentId ?? "";
      const result = moveEditorBlock(current, blockId, parentId, afterId);
      if (!result) return;
      const tidied = tidyColumns(result.graph, [source]);
      commit(tidied.graph, [...result.operations, ...tidied.operations]);
    },
    restoreGraph(graph, operations) {
      send(graph, withoutStaleAnchors(operations), undefined, undefined, false);
    },
    turnDatabaseIntoPage(blockId, store) {
      turnDatabase(blockId, "page", store);
    },
    turnDatabaseInline(pageBlockId, store) {
      turnDatabase(pageBlockId, "inline", store);
    },
    changeLayout(change) {
      const result = change(current);
      return result ? commit(result.graph, result.operations) : false;
    },
  };
}

/**
 * A block a view hands back to be written, in the place the graph has it:
 * a view keeps the block it was drawn with while only its place changes (a
 * line made renumbers the places through the page, useNativeTiptapNode),
 * and its content is written where the block stands now.
 */
function placedAsShown(graph: ContentGraph, block: GraphBlock): GraphBlock {
  const live = graph.blocks.find(candidate => candidate.id === block.id);
  return live && (live.parentId !== block.parentId || live.afterId !== block.afterId || live.position !== block.position)
    ? { ...block, parentId: live.parentId, afterId: live.afterId, position: live.position } : block;
}
