import type { ProductStatusSink } from "@/lib/productStatus";
import { Suspense, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { lazyScreen } from "@/lib/lazyScreen";
import { api } from "@/api";
import { i18next } from "@/i18n";
import {
  deleteDatabaseRowPagePermanently,
  readDatabaseRowTrash,
  restoreDatabaseRowPage,
} from "@/databaseRowPageApi";
import type { DatabaseRowTrashItem } from "@/databaseRowPageWire.generated";
import { newId } from "@/lib/ids";
import { commitNativePageMetadata } from "@/lib/nativePageMetadata";
import { canManageNativeWorkspace } from "@/lib/nativeWorkspaceRolePolicy";
import { drainPagePermanentDelete } from "@/lib/nativeTrashLifecycle";
import { databasePagePlace, placeUnder } from "@/lib/pagePath";
import { pageTrashRoot } from "@/lib/pageTrash";
import { dataSourceDisplayTitle } from "@/components/editor/databaseTitles";
import { pathLeavesOwnPages } from "@/lib/pagePathPieces";
import { usePagePathsAbove } from "@/lib/usePagePathsAbove";
import type { Page, Session, Workspace } from "@/types";
import { readEveryWindow } from "@/lib/listWindows";
import { pageHref } from "@/lib/navigation";
import { inlineDatabaseFullPageHref } from "@/lib/inlineDatabaseFullPage";
import { pushRoute } from "@/lib/router";
import { pageMapOf } from "@/lib/pageIndex";

const TrashView = lazyScreen(() =>
  import("@/components/TrashView").then(({ TrashView: View }) => ({ default: View })), { typedOnOpen: true }
);

type ActiveOperation = {
  key: string;
  promise: Promise<void>;
};

function aborted(signal: AbortSignal) {
  if (signal.aborted) throw new DOMException("Trash view operation retired.", "AbortError");
}

// A database row in trash is listed like a trashed page inside the page that
// holds its database; its actions go to the row's own trash. A page of a data
// source in the trash is placed at its own last edit, as Notion's trash
// places each line that went there with what holds it (seen 2026-11-12: "N
// row 1 edited" above "N row 2", both below "W695 M2", sent there before
// their database).
function trashedRowPage(item: DatabaseRowTrashItem, workspaceId: string): Page {
  return {
    id: item.rowId, workspaceId, teamspaceId: "", title: item.title, untitledName: item.untitledName, content: "", revision: 0, cursor: 0,
    parentId: item.containerPageId, position: 0, isLocked: false, inTrash: true, trashedAt: item.editedAt || item.trashedAt,
    icon: item.icon, iconType: item.iconType, cover: "", coverPosition: 50, font: "default", smallText: false,
    fullWidth: false, tableOfContentsDisplay: "default", inlineCommentsDisplay: "default", backlinksDisplay: "default", pageCommentsDisplay: "default",
    createdAt: item.trashedAt, updatedAt: item.trashedAt, createdBy: "", lastEditedBy: item.trashedBy,
  };
}

export function AuthenticatedTrashView({
  initialQuery,
  onClose,
  onOpenPage,
  onOpenRow,
  onPageChange,
  onRefresh,
  onStatus,
  pages,
  session,
  workspace,
}: {
  /** What the trash opens searching for. */
  initialQuery?: string;
  onClose(): void;
  onOpenPage(page: Page): void;
  /** Opens a trashed database row's page inside its database, or at its own address while its data source is in the trash. */
  onOpenRow?(item: DatabaseRowTrashItem): void;
  onPageChange(page: Page): void;
  onRefresh(): Promise<void>;
  onStatus: ProductStatusSink;
  pages: Page[];
  session: Session;
  workspace: Workspace;
}) {
  const operationRef = useRef<AbortController | null>(null);
  const activeRef = useRef<ActiveOperation | null>(null);
  const [rowItems, setRowItems] = useState<DatabaseRowTrashItem[]>([]);
  const loadRows = useCallback(async (signal?: AbortSignal) => {
    try {
      // Every trashed row, a window at a time, as the trashed pages are all here too.
      const items = await readEveryWindow((cursor) => readDatabaseRowTrash(session, workspace.id, signal, cursor), signal);
      if (!signal?.aborted) setRowItems(items);
    } catch {
      if (!signal?.aborted) setRowItems([]);
    }
  }, [session, workspace.id]);
  const rowsById = useMemo(() => new Map(rowItems.map((item) => [item.rowId, item])), [rowItems]);
  // A page of a data source in the trash, or a page from under a page in the
  // trash, restored, is a private page to open.
  const restoredAsPagesRef = useRef(new Map<string, Page>());
  // The pages under a page in the trash are each a line of the trash, sent
  // there with it, as Notion's trash lists them (seen 2026-11-12: "P sub ·
  // … / W674 P page", "P subsub · … / P sub" and the full-page database "P
  // full" beside "W674 P page"); the page list keeps them as they are. Each
  // is placed at its own last edit, among the trash's other lines, as
  // Notion's ("L sub one edited", made before "L sub two" and edited after
  // it, above it; "W695 M1", sent there between their making, below both).
  const nestedPages = useMemo(() => {
    const byId = pageMapOf(pages);
    const nested = new Map<string, Page>();
    for (const page of pages) {
      if (page.inTrash || page.workspaceId !== workspace.id || page.movedDatabase) continue;
      const root = pageTrashRoot(page, (id) => byId.get(id));
      if (root) nested.set(page.id, { ...page, inTrash: true, trashedAt: page.updatedAt || root.trashedAt });
    }
    return nested;
  }, [pages, workspace.id]);
  const pagesById = pageMapOf(pages);
  // A wiki's page trashed from the sidebar is listed once, as its database's row.
  const listedPages = useMemo(
    () => [...pages.filter((page) => !rowsById.has(page.id)).map((page) => nestedPages.get(page.id) ?? page),
      ...rowItems.map((item) => trashedRowPage(item, workspace.id))],
    [nestedPages, pages, rowItems, rowsById, workspace.id],
  );
  // A trashed database page, and a trashed page under one, is placed by the
  // path the server names, its source too, as Notion's trash (seen
  // 2026-11-10: "… / ... / 둘째 소스").
  usePagePathsAbove(useMemo(() => {
    const byId = pageMapOf(listedPages);
    return listedPages.filter((page) => page.inTrash && !rowsById.get(page.id)?.sourceInTrash
      && (rowsById.has(page.id) || pathLeavesOwnPages(page, (id) => byId.get(id))));
  }, [listedPages, rowsById]));

  useEffect(() => () => operationRef.current?.abort(), []);
  useEffect(() => {
    const controller = new AbortController();
    void loadRows(controller.signal);
    return () => controller.abort();
  }, [loadRows]);

  function runOperation(key: string, operation: (signal: AbortSignal) => Promise<void>) {
    if (activeRef.current?.key === key) return activeRef.current.promise;
    if (activeRef.current) {
      return Promise.reject(Object.assign(new Error("Another trash operation is active."), {
        code: "LEAFEN_TRASH_OPERATION_ACTIVE",
      }));
    }
    const controller = new AbortController();
    operationRef.current = controller;
    const promise = operation(controller.signal).finally(() => {
      if (operationRef.current === controller) operationRef.current = null;
      if (activeRef.current?.promise === promise) activeRef.current = null;
    });
    activeRef.current = { key, promise };
    return promise;
  }

  return (
    <Suspense fallback={null}>
      <TrashView
        initialQuery={initialQuery}
        canPermanentlyDelete={canManageNativeWorkspace(workspace)}
        pages={listedPages}
        locationOf={(page, pagesById) => {
          const row = rowsById.get(page.id);
          // A page of a data source in the trash sits through its source, as
          // Notion's trash places it (seen 2026-11-12: "… / H one"; an
          // untitled source's "… / 새 데이터 소스" while its database holds
          // more, its only one's "… / 새 데이터베이스").
          if (row?.sourceInTrash) {
            return placeUnder(row.containerPageId, pagesById,
              ...(row.inlineDatabase && row.databaseTitle?.trim() ? [row.databaseTitle.trim()] : []),
              dataSourceDisplayTitle(row.sourceTitle, row.sourceCount || 1));
          }
          return row ? databasePagePlace(row.containerPageId, pagesById, row, page.id) : undefined;
        }}
        restoresToPrivatePages={(page) => rowsById.get(page.id)?.sourceInTrash === true || nestedPages.has(page.id)}
        onDatabaseRestored={async ({ pageId, blockId, viewId }, item) => {
          // One from a page in the trash is a private page now, which the
          // sidebar reads the workspace again for, opened at its address.
          if (item.inTrashedPage) {
            await onRefresh();
            pushRoute(pageHref(pageId));
            return;
          }
          // Any other opens alone as a page, as Notion's restore does (seen
          // 2026-11-13): an inline database at its own address, a full-page
          // one as its page, a source kept in its database at that source's
          // view.
          pushRoute(pagesById.get(pageId)?.kind === "database"
            ? pageHref(pageId, viewId ? new URLSearchParams({ v: viewId }) : null)
            : inlineDatabaseFullPageHref(pageId, blockId, viewId));
        }}
        session={session}
        workspace={workspace}
        onClose={onClose}
        onDeletePage={(page) => runOperation(`delete:${page.id}`, async (signal) => {
          const row = rowsById.get(page.id);
          if (row) {
            await deleteDatabaseRowPagePermanently(session, row.rowId, {
              requestId: newId(), expectedAuthorityRevision: row.authorityRevision,
            }, signal);
            aborted(signal);
            await loadRows(signal);
            return;
          }
          await drainPagePermanentDelete({
            pageId: page.id,
            requestId: newId(),
            request: (input, requestSignal) => (
              api.deletePagePermanently(session, page.id, input, requestSignal)
            ),
            signal,
          });
          aborted(signal);
          await onRefresh();
          aborted(signal);
        })}
        onOpenPage={(page) => {
          const row = rowsById.get(page.id);
          const restored = restoredAsPagesRef.current.get(page.id);
          if (restored) onOpenPage(restored);
          // A page under a page in the trash opens as the page list keeps it.
          else if (!row) onOpenPage(nestedPages.has(page.id) ? pagesById.get(page.id) ?? page : page);
          // A page of a data source in the trash opens on its own, as a page
          // in the trash, as Notion's does (seen 2026-11-12).
          else onOpenRow?.(row);
        }}
        onRestorePage={(page) => runOperation(`restore:${page.id}`, async (signal) => {
          const row = rowsById.get(page.id);
          if (row) {
            await restoreDatabaseRowPage(session, row.rowId, {
              requestId: newId(), expectedAuthorityRevision: row.authorityRevision,
            }, signal);
            aborted(signal);
            await loadRows(signal);
            if (row.sourceInTrash) {
              // It is a private page now, which the sidebar reads the workspace again for.
              restoredAsPagesRef.current.set(row.rowId, { ...page, parentId: "", inTrash: false, trashedAt: undefined });
              await onRefresh();
              return;
            }
            // A wiki's page comes back to the sidebar, which reads the workspace again for it.
            if (pages.some((candidate) => candidate.wikiDataSourceId === row.dataSourceId)) await onRefresh();
            return;
          }
          // A page from under a page in the trash comes out of it to the top
          // of the reader's private pages, as Notion's restore does after it
          // asks (seen 2026-11-12: "P subsub" and "P inline", then opened
          // there), with the pages under it, which the workspace is read
          // again for.
          const nested = nestedPages.has(page.id) ? pagesById.get(page.id) : undefined;
          if (nested) {
            const { page: restored } = await commitNativePageMetadata(session, nested, { parentId: "", teamspaceId: "" }, signal);
            restoredAsPagesRef.current.set(page.id, restored);
            onPageChange(restored);
            await onRefresh();
            return;
          }
          // A page comes back at once, to the list and opened as the trash
          // closes (user decision 2026-11-12: a press answers on the next
          // frame); the server's answer follows past the trash, and a refusal
          // puts the page back in the trash, saying so.
          onPageChange({ ...page, inTrash: false, trashedAt: undefined });
          void commitNativePageMetadata(session, page, { inTrash: false }).then(({ page: restored }) => onPageChange(restored), () => {
            onPageChange(page);
            onStatus(i18next.t("trashView:toast.couldntRestore"), "error");
          });
        })}
        onStatus={onStatus}
      />
    </Suspense>
  );
}
