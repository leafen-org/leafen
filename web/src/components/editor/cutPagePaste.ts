import { api } from "@/api";
import { moveDatabaseRowPage, movePageIntoDatabase, readDatabaseRowPage } from "@/databaseRowPageApi";
import { newId } from "@/lib/ids";
import { commitNativePageMetadata } from "@/lib/nativePageMetadata";
import type { Session } from "@/types";
import type { ContentGraph, GraphBlock, GraphRow } from "@/contentGraph";
import type { PageGraphStore } from "@/lib/pageGraphStore";
import { holdRowsGone, rowsDrawn } from "@/lib/rowsShownAtPress";
import { blocksDrawn, showBlocksBack, showBlocksGone } from "./blockMoveHistory";
import { editorBlockChildren } from "./editorBlockIndex";
import type { EditorBlockSpot, EditorGraphSync, NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { keepBlocksOutOfNativeEditorHistory } from "./nativeEditorCommandSteps";
import { recordInNativeEditorScope } from "./nativeEditorUndoScopes";

/**
 * A cut page pasted here comes back from the trash and moves here, as a cut
 * database does (Notion pastes a copy on another page). A cut sub-page
 * comes with this page its parent. A cut database item — a table's row, a
 * board's, a list's or a gallery's item cut with ⌘X — leaves its database as a
 * sub-page here, by the same ID, its body and comments with it and its values
 * kept should it go back, out of the trash in the same move on the server
 * (POST /database-rows/{rowId}/page/move with fromTrash), as Notion's does on
 * the page it was cut from (seen 2026-11-27 in Chrome, two tabs at two times:
 * a table's, a board's, a list's and a gallery's items, pasted on a line or
 * with another database's item chosen). One not in the trash — pasted twice,
 * or never cut — is linked to instead. `serverBlock` puts what came where the
 * paste wants it.
 */
export async function moveCutPage(deps: {
  session: Session | null | undefined; sync: EditorGraphSync | null | undefined; pageId: string; workspaceId: string;
  signal?: AbortSignal; canWrite(): boolean; serverBlock(make: () => Promise<string | null>): Promise<string | null>;
}, pageId: string) {
  const { session, sync } = deps;
  if (!session || !sync || !deps.canWrite() || !await sync.flush()) return null;
  return deps.serverBlock(async () => {
    const page = await api.readPage(session, pageId, deps.signal);
    if (!page.inTrash) return null;
    if (!page.databaseRow) {
      await commitNativePageMetadata(session, page, { inTrash: false, parentId: deps.pageId }, deps.signal);
      return pageId;
    }
    const { row } = await readDatabaseRowPage(session, pageId, { limit: 1, signal: deps.signal });
    await moveDatabaseRowPage(session, pageId, {
      requestId: newId(), expectedAuthorityRevision: row.authorityRevision, targetPageId: deps.pageId, leaveDatabase: true, fromTrash: true,
    }, deps.signal);
    // Its step takes it back into its database and out again itself (cutRowsPasted), no block of the paste's own.
    keepBlocksOutOfNativeEditorHistory(`${deps.workspaceId}/${deps.pageId}/${session.user.id}`, [pageId]);
    broughtOut.set(pageId, { rowId: pageId, dataSourceId: row.dataSourceId, position: row.position, spot: null });
    return pageId;
  });
}

type CutRow = { rowId: string; dataSourceId: string; position: string; spot: EditorBlockSpot | null };

// The graph each open page draws, which a cut item's step shows its change on at the press.
const pageGraphs = new Map<string, PageGraphStore>();

/** Keeps the graph the open page draws for its cut items' steps; returns its letting go. */
export function keepPageGraphForCutRows(pageId: string, store: PageGraphStore | null) {
  if (!store) return undefined;
  pageGraphs.set(pageId, store);
  return () => { if (pageGraphs.get(pageId) === store) pageGraphs.delete(pageId); };
}

// The cut items a paste brought out, till it hears they came (cutRowsPasted).
const broughtOut = new Map<string, CutRow>();

/**
 * The cut database items one paste brought here, once each came: one step of
 * the page's history after the paste's, as Notion's one ⌘Z takes the items
 * back into their database where they stood and takes their blocks away
 * (seen 2026-11-27, two tabs: a table's item, a board's two, and one pasted
 * with another database's item chosen); ⌘⇧Z takes them out to where their
 * blocks were again, as Notion's. At the press the blocks go, or come back
 * as drawn, and the rows leave their database as drawn, held over every read
 * till the server moved them (user decision 2026-11-12: a press answers on
 * the next frame); a row going back fills in its place once it is there.
 */
export function cutRowsPasted(adapter: () => NativeEditorGraphAdapter) {
  const rows: CutRow[] = [], drawn: CutDrawn = { blocks: [], rows: [] };
  return {
    arrived(pageId: string | null) {
      const row = pageId ? broughtOut.get(pageId) : undefined;
      if (row && pageId) { broughtOut.delete(pageId); rows.push(row); }
    },
    settle() {
      const session = adapter().session;
      if (!rows.length || !session) return;
      const scope = `${adapter().workspaceId}/${adapter().pageId}/${session.user.id}`;
      recordInNativeEditorScope(scope, `cut-rows-pasted:${rows[0].rowId}:${Date.now()}`, (forward) => stepCutRows(adapter, session, rows, drawn, forward));
    },
  };
}

// The items' blocks as drawn when they went back, which ⌘⇧Z shows again, and their rows when they came out again, which ⌘Z shows.
type CutDrawn = { blocks: GraphBlock[]; rows: GraphRow[] };

async function stepCutRows(adapter: () => NativeEditorGraphAdapter, session: Session, rows: readonly CutRow[], drawn: CutDrawn, forward: boolean) {
  const sync = adapter().graphSync, store = pageGraphs.get(adapter().pageId) ?? null, ids = rows.map(({ rowId }) => rowId);
  let shown: (refused: boolean) => void;
  if (!forward) {
    drawn.blocks = blocksDrawn(adapter().graph, ids);
    const gone = showBlocksGone(store, adapter(), ids), back = drawn.rows.length ? holdRowsGone(store, drawn.rows, true) : null;
    shown = (refused) => { if (refused) gone?.refused(); else gone?.moved(); back?.(refused); };
  } else {
    drawn.rows = rowsDrawn(store, ids);
    const out = holdRowsGone(store, drawn.rows);
    const back = showBlocksBack(store, adapter(), drawn.blocks, rows.flatMap(({ rowId, spot }) => spot ? [{ blockId: rowId, ...spot }] : []));
    shown = (refused) => { back(refused); out(refused); };
  }
  let done = false;
  try {
    if (!forward) {
      for (const row of [...rows].reverse()) {
        row.spot = spotOf(adapter().graph, row.rowId) ?? row.spot;
        await movePageIntoDatabase(session, row.rowId, { requestId: newId(), dataSourceId: row.dataSourceId, position: row.position });
      }
    } else {
      for (const row of rows) {
        const { row: now } = await readDatabaseRowPage(session, row.rowId, { limit: 1 });
        await moveDatabaseRowPage(session, row.rowId, {
          requestId: newId(), expectedAuthorityRevision: now.authorityRevision, targetPageId: adapter().pageId, leaveDatabase: true,
        });
        if (row.spot) await sync?.place?.(row.rowId, row.spot).catch(() => false);
      }
    }
    done = true;
    return true;
  } catch {
    return false;
  } finally {
    await sync?.reread().catch(() => false);
    shown(!done);
    void adapter().moveContext?.onPagesChanged?.();
  }
}

/** Where a block stands: after the block before it among its parent's, or first there. */
function spotOf(graph: ContentGraph, blockId: string): EditorBlockSpot | null {
  const block = graph.blocks.find((candidate) => candidate.id === blockId);
  if (!block) return null;
  const siblings = editorBlockChildren(graph, block.parentId);
  return { parentId: block.parentId, afterId: siblings[siblings.findIndex((candidate) => candidate.id === blockId) - 1]?.id ?? null };
}
