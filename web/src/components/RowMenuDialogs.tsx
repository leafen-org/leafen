import type { ProductStatusSink } from "@/lib/productStatus";
import { Suspense } from "react";
import { lazyScreen } from "@/lib/lazyScreen";
import type { createPageAnalyticsPanelService } from "@/lib/pageAnalyticsPanelService";
import { pagePathLinks } from "@/lib/pagePath";
import type { Page, Session } from "@/types";
import type { MovePopoverAnchor } from "./movePopoverPlacement";
import type { PageExportChoice } from "./PageExportDialog";
import { usePathsAboveShown } from "@/lib/usePagePathsAbove";
import type { DatabaseHistoryTarget } from "./DatabaseHistoryDialog";
import { pagesByIdOf } from "@/lib/pageIndex";

const MoveToDialog = lazyScreen(() => (
  import("./MoveToDialog").then(({ MoveToDialog: Dialog }) => ({ default: Dialog }))
), { typedOnOpen: true });
const PageExportDialog = lazyScreen(() => (
  import("./PageExportDialog").then(({ PageExportDialog: Dialog }) => ({ default: Dialog }))
));
const PageHistoryDialog = lazyScreen(() => (
  import("./PageHistoryDialog").then(({ PageHistoryDialog: Dialog }) => ({ default: Dialog }))
));
const DatabaseHistoryDialog = lazyScreen(() => (
  import("./DatabaseHistoryDialog").then(({ DatabaseHistoryDialog: Dialog }) => ({ default: Dialog }))
));
const PageAnalyticsPanel = lazyScreen(() => (
  import("./PageAnalyticsPanel").then(({ PageAnalyticsPanel: Panel }) => ({ default: Panel }))
));

/** The windows a page's menu opens: export, history, updates and analytics, and move. */
export function RowMenuDialogs({
  page, pages, session, canEdit, title, analyticsService,
  exportOpen, historyOpen, historyVersionId, analyticsOpen, moveOpen, moveAnchor,
  onExport, onCloseExport, onCloseHistory, onCloseAnalytics, onHistoryFromAnalytics, onCloseMove,
  onPageChange, onStatus, database,
}: {
  page: Page;
  pages: Page[];
  session: Session;
  canEdit: boolean;
  title: string;
  analyticsService: ReturnType<typeof createPageAnalyticsPanelService> | null;
  exportOpen: boolean;
  historyOpen: boolean;
  /** The version the history opens on, from an update's "버전 보기". */
  historyVersionId?: string;
  analyticsOpen: false | "updates" | "analytics";
  moveOpen: boolean;
  moveAnchor: MovePopoverAnchor | null;
  onExport: (choice: PageExportChoice, signal: AbortSignal) => Promise<void>;
  onCloseExport: () => void;
  onCloseHistory: () => void;
  onCloseAnalytics: () => void;
  onHistoryFromAnalytics: (versionId?: string) => void;
  onCloseMove: () => void;
  onPageChange?: (page: Page) => void | Promise<void>;
  onStatus?: ProductStatusSink;
  /** An inline database opened alone, whose own history and updates the windows show. */
  database?: DatabaseHistoryTarget;
}) {
  const pagesById = pagesByIdOf(pages);
  // A row page's, and a page's under one, path the server names (Notion's).
  usePathsAboveShown([page], pagesById);
  const pathLinks = pagePathLinks(page.parentId, pagesById);
  return (
    <Suspense fallback={null}>
      {exportOpen ? <PageExportDialog policyFor={{ session, workspaceId: page.workspaceId, pageId: page.id }}
        onClose={onCloseExport} onExport={onExport} /> : null}
      {historyOpen && database ? <DatabaseHistoryDialog database={database} session={session} onClose={onCloseHistory} />
      : historyOpen ? (
        <PageHistoryDialog page={page} session={session} canEdit={canEdit} onClose={onCloseHistory} onPageChange={onPageChange} onStatus={onStatus}
          initialVersionId={historyVersionId} />
      ) : null}
      {analyticsOpen && analyticsService ? (
        <PageAnalyticsPanel pageId={page.id} workspaceId={page.workspaceId} service={analyticsService} initialTab={analyticsOpen}
          updates={{ pageTitle: title, path: pathLinks.map(({ title: label }) => label).join(" / "), pathLinks, session }}
          onClose={onCloseAnalytics}
          onOpenPageHistory={onHistoryFromAnalytics} />
      ) : null}
      {moveOpen && onPageChange ? (
        <MoveToDialog pageId={page.id} pages={pages} session={session} anchor={moveAnchor}
          onClose={onCloseMove}
          onPageChange={onPageChange} onStatus={onStatus} />
      ) : null}
    </Suspense>
  );
}
