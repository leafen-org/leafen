import { Suspense, useContext, useMemo, useRef } from "react";
import type { GraphRow } from "@/contentGraph";
import { createPortal } from "react-dom";
import { movePopoverAnchorOf, type MovePopoverAnchor } from "@/components/movePopoverPlacement";
import { useProductPages } from "@/components/product/productPagesContext";
import type { DatabaseRowPageSnapshot } from "@/databaseRowPageWire.generated";
import { createDatabaseRowPageService } from "@/lib/databaseRowPageService";
import { movePageIntoDatabase } from "@/databaseRowPageApi";
import { newId } from "@/lib/ids";
import { holdPageThroughMove } from "@/lib/pageBecomingRow";
import { lazyScreen } from "@/lib/lazyScreen";
import { useOpenPageGraphStore } from "@/lib/pageGraphStore";
import { holdRowsGone, rowsDrawn } from "@/lib/rowsShownAtPress";
import type { Page } from "@/types";
import { DatabaseDataSourcesContext } from "./databaseDataSourcesContext";

const MoveToDialog = lazyScreen(() => (
  import("@/components/MoveToDialog").then(({ MoveToDialog: Dialog }) => ({ default: Dialog }))
), { typedOnOpen: true });

/** A database's page as the move popover takes it, and the data source it is in. */
export type DatabaseRowMoveSubject = { page: Page; dataSourceId: string };

/** Where a view's rows' pages are: their workspace and the page holding their data source. */
export type DatabaseRowPlace = { workspaceId: string; containerPageId: string };

type MovingRow = Pick<GraphRow, "id" | "dataSourceId" | "icon" | "iconType" | "createdAt" | "updatedAt" | "createdBy" | "lastEditedBy">;

function rowMoveSubject(row: MovingRow, place: DatabaseRowPlace, title: string): DatabaseRowMoveSubject {
  return {
    dataSourceId: row.dataSourceId,
    page: {
      id: row.id, workspaceId: place.workspaceId, teamspaceId: "", title, content: "", revision: 0, cursor: 0,
      parentId: place.containerPageId, position: 0, isLocked: false, inTrash: false,
      icon: row.icon ?? "", iconType: row.iconType || "none", cover: "", coverPosition: 50,
      kind: "page", createdAt: row.createdAt ?? "", updatedAt: row.updatedAt ?? "", createdBy: row.createdBy ?? "", lastEditedBy: row.lastEditedBy ?? "",
    } as Page,
  };
}

/** The row's page and its data source, read from its snapshot. */
export function databaseRowMoveSubject(snapshot: DatabaseRowPageSnapshot, title: string): DatabaseRowMoveSubject {
  return rowMoveSubject(snapshot.row, { workspaceId: snapshot.workspaceId, containerPageId: snapshot.row.containerPageId }, title);
}

/**
 * The row's page as its view holds it, before the page is read: its move
 * reads the row again itself (databaseRowPageService), so a row's "옮기기"
 * opens the pages' move at once, read or not — on a phone the whole "옮기기"
 * sheet with "완료", as Notion's database page's "작업 › 옮기기" (seen
 * 2026-11-17 in Android Chrome on a Galaxy A90 at 411).
 */
export function databaseViewRowMoveSubject(row: GraphRow, place: DatabaseRowPlace | undefined): DatabaseRowMoveSubject | undefined {
  if (!place) return undefined;
  const title = Object.values(row.values).find((cell) => cell.type === "title")?.value;
  return rowMoveSubject(row, place, typeof title === "string" ? title : "");
}

/** The popover's top where the menu's was, as Notion's (seen 2026-10-31). */
export function databaseRowMoveAnchor(menu: Element | null): MovePopoverAnchor | null {
  const at = movePopoverAnchorOf(menu);
  return at ? { ...at, top: at.top - 4, bottom: at.top - 4 } : null;
}

/**
 * A database page's "옮기기" from its menus, as Notion's (seen 2026-10-31:
 * the row page's "…", a table row's menu, a phone's sheet): the pages' move
 * popover where the menu was, its workspace picker included. It moves the
 * row itself and says so as Notion does, with "실행 취소" back to its data
 * source. It is drawn on the page's body, as the menu is, so the block
 * holding a database rises over nothing for it.
 * The row and its sub-items leave the open page's databases at the press
 * (user decision 2026-11-12: a press answers on the next frame; they stayed
 * until the server had moved them and the page was read again, 0.85–1.2 s
 * on a NAS), held so over every graph the page reads until the server moved
 * it; refused or not agreed to, they are back as drawn (MoveToDialog's rowMoved).
 * Opened beside the page — the sidebar, a peek — it holds the open page's
 * graph too (lib/pageGraphStore.ts). Its notice's "실행 취소" shows them back
 * as drawn at the press, and the page it became out of the sidebar, until
 * the server took it back.
 */
export function DatabaseRowMovePopover({ subject, anchor, anchorGap, onClose, onMoved, addTo, onListChanged }: {
  subject: DatabaseRowMoveSubject;
  anchor: MovePopoverAnchor | null;
  /** Hung right under what it opened from, as ⌘⇧P's under the "⋯" (MoveToDialog's). */
  anchorGap?: number;
  onClose: () => void;
  onMoved?: () => void;
  /** Opened from a just-made row's "추가 대상", as MoveToDialog's. */
  addTo?: boolean;
  /** A list of the row beside the page (a sidebar view's section) reads its rows again once the row moved or came back. */
  onListChanged?: () => void;
}) {
  const productPages = useProductPages();
  const session = productPages?.session;
  const service = useMemo(() => session ? createDatabaseRowPageService(session, () => {}) : null, [session]);
  // The database the menu is in reads its rows again once one leaves or comes back.
  const dataSources = useContext(DatabaseDataSourcesContext);
  const store = useOpenPageGraphStore();
  // The press's hold on the page's graph, let go once the server moved the
  // row: the page read next draws the server's rows, the row gone or in its
  // new database there.
  const held = useRef<(refused?: boolean) => void>(() => undefined);
  // The rows as drawn when it left, which "실행 취소" brings back.
  const drawn = useRef<readonly GraphRow[]>([]);
  if (!session || !service || !productPages) return null;
  const { page: rowPage, dataSourceId } = subject;
  // The row is where its database is: a teamspace's row is asked about
  // before it goes to the top of private pages, as a page there is.
  const page = rowPage.teamspaceId ? rowPage
    : { ...rowPage, teamspaceId: productPages.pages.find(({ id }) => id === rowPage.parentId)?.teamspaceId ?? "" };
  // Moved already, a read that fails leaves the row as it is shown.
  const reread = async () => {
    onListChanged?.();
    await dataSources?.reread().catch(() => undefined);
  };
  return createPortal(
    <Suspense fallback={null}>
      <MoveToDialog pageId={page.id} pages={productPages.pages} session={session} anchor={anchor} anchorGap={anchorGap} addTo={addTo}
        onClose={onClose} onMoved={onMoved ? () => { onMoved(); onClose(); } : undefined} onPageChange={() => {}} onStatus={productPages.onStatus}
        row={{
          page,
          leave: () => {
            drawn.current = rowsDrawn(store, [page.id]);
            held.current = holdRowsGone(store, drawn.current);
            return (refused) => held.current(refused);
          },
          preview: (target) => service.previewMove(page.id, target),
          move: async (target, accepted) => {
            await service.move(page.id, target, accepted);
            held.current();
            await reread();
          },
          // A row gone to another database goes back as a row, reading its new
          // revision first; one gone to a page goes back as that page, taking
          // its kept values, the open one drawn till then (pageBecomingRow.ts).
          undo: dataSourceId ? async (target) => {
            const back = holdRowsGone(store, drawn.current, true);
            const turned = target.kind === "page" ? holdPageThroughMove(session, [page.id]) : () => undefined;
            let listed: Page | undefined;
            productPages.setData((current) => {
              listed = current.pages.find(({ id }) => id === page.id);
              return listed ? { ...current, pages: current.pages.filter(({ id }) => id !== page.id) } : current;
            });
            try {
              if (target.kind === "page") {
                await movePageIntoDatabase(session, page.id, { requestId: newId(), dataSourceId }, new AbortController().signal);
              } else {
                await service.refresh(page.id);
                await service.move(page.id, { kind: "database", id: dataSourceId }, []);
              }
              back();
              turned();
            } catch (error) {
              back(true);
              const was = listed;
              if (was) productPages.setData((current) => ({ ...current, pages: [...current.pages.filter(({ id }) => id !== page.id), was] }));
              turned(true);
              throw error;
            }
            await reread();
          } : undefined,
        }} />
    </Suspense>,
    document.body,
  );
}
