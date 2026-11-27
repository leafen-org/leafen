import type { ProductStatusSink } from "@/lib/productStatus";
import { Suspense, useCallback, useMemo, useState } from "react";
import { lazyScreen } from "@/lib/lazyScreen";
import { createPageAnalyticsPanelService } from "@/lib/pageAnalyticsPanelService";
import { pagePathLinks } from "@/lib/pagePath";
import { usePageNames } from "@/lib/pageNames";
import type { Page, Session } from "@/types";
import { usePathsAboveShown } from "@/lib/usePagePathsAbove";
import { pagesByIdOf } from "@/lib/pageIndex";
import { pageHref } from "@/lib/navigation";
import { useOpenPageGraphStore, usePageGraphSelect } from "@/lib/pageGraphStore";
import type { ContentGraph } from "@/contentGraph";
import { readPageHistoryVersionAt } from "@/lib/pageHistoryGroups";

const PageAnalyticsPanel = lazyScreen(() => (
  import("../PageAnalyticsPanel").then(({ PageAnalyticsPanel: Panel }) => ({ default: Panel }))
));
const DatabaseHistoryDialog = lazyScreen(() => (
  import("../DatabaseHistoryDialog").then(({ DatabaseHistoryDialog: Dialog }) => ({ default: Dialog }))
));
const PageHistoryDialog = lazyScreen(() => (
  import("../PageHistoryDialog").then(({ PageHistoryDialog: Dialog }) => ({ default: Dialog }))
));

/** The open page's "업데이트와 애널리틱스" beside it, and the history its updates open over the page. */
export function PageUpdatesSidePanel({ page, pages, session, canEdit, database, onClose, onPageChange, onStatus }: {
  page: Page;
  pages: Page[];
  session: Session;
  canEdit: boolean;
  /** An inline database opened alone on the page: its own updates, locks and analytics are listed, under the page (Notion's, seen 2026-11-10, 2026-11-11). */
  database?: { blockId: string; title: string; href: string; sourceId?: string; titleLabel?: string };
  onClose(): void;
  onPageChange?: (page: Page) => void | Promise<void>;
  onStatus?: ProductStatusSink;
}) {
  const pageNames = usePageNames();
  const service = useMemo(() => createPageAnalyticsPanelService(session), [session]);
  const [history, setHistory] = useState<{ versionId?: string } | null>(null);
  const pagesById = pagesByIdOf(pages);
  // A row page's, and a page's under one, path the server names (Notion's).
  usePathsAboveShown([page], pagesById);
  const pathLinks = pagePathLinks(page.parentId, pagesById);
  // A wiki's page is its database: its own updates are listed with the page's (PageUpdatesSource), its pages' titles named as its title property is.
  const wikiSourceId = page.isWiki && page.wikiRootId === page.id && !database ? page.wikiDataSourceId ?? "" : "";
  const titleOf = useCallback((graph: ContentGraph) => wikiSourceId
    ? graph.properties.find((property) => property.dataSourceId === wikiSourceId && property.type === "title")?.name : undefined, [wikiSourceId]);
  const wikiTitleLabel = usePageGraphSelect(useOpenPageGraphStore(), titleOf);
  const source = wikiSourceId ? { sourceId: wikiSourceId, workspaceId: page.workspaceId, title: pageNames.title(page), href: pageHref(page.id),
    pathLinks, titleLabel: wikiTitleLabel } : undefined;
  // A wiki's update of one of its pages opens that page's own history (pageHistoryVersionAt).
  const historyPage = pagesById[readPageHistoryVersionAt(history?.versionId)?.pageId ?? ""] ?? page;
  return (
    <Suspense fallback={null}>
      <PageAnalyticsPanel docked pageId={page.id} containerId={database?.blockId} workspaceId={page.workspaceId} service={service}
        updates={{ pageTitle: pageNames.title(page), path: pathLinks.map(({ title }) => title).join(" / "), pathLinks, session, source,
          ...(database ? { lockTarget: { ...database, workspaceId: page.workspaceId,
            pathLinks: [...pathLinks, { id: page.id, title: pageNames.title(page) }] } } : {}) }}
        onClose={onClose} onOpenPageHistory={(versionId) => setHistory({ versionId })} />
      {/* A database's own update opens its own history there. */}
      {history?.versionId?.startsWith("activity:") && database?.sourceId ? (
        <DatabaseHistoryDialog session={session} initialId={history.versionId.slice("activity:".length)} onClose={() => setHistory(null)}
          database={{ pageId: page.id, workspaceId: page.workspaceId, blockId: database.blockId, sourceId: database.sourceId, title: database.title }} />
      ) : history ? (
        <PageHistoryDialog key={historyPage.id} page={historyPage} session={session} canEdit={canEdit} onClose={() => setHistory(null)} onPageChange={onPageChange}
          onStatus={onStatus} initialVersionId={history.versionId} />
      ) : null}
    </Suspense>
  );
}
