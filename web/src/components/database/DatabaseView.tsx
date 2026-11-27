"use client";

import { useContext, useState, type ReactNode } from "react";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { holdFeedInPageScroll } from "@/components/shell/pageScrollHost";
import type {
  GraphBlock,
  GraphDataSource,
  GraphProperty,
  GraphRow,
  GraphView,
  GraphViewType,
} from "@/contentGraph";
import { DatabaseSprintRowContext } from "./databaseSprintContext";
import { DatabaseViewTabs } from "./DatabaseViewTabs";
import { viewPanelId } from "./viewPanel";
import { useViewSwitchGivesNoFocusBack } from "./useViewSwitchGivesNoFocusBack";
import { viewToolsInTitleRow } from "./databaseViewModel";
import { useDatabaseOwnPage } from "./databaseFullPageLink";
import { viewDisplayName } from "./databaseViewShared";
import styles from "./databaseSheet";
import { DatabaseArchivedBanner, useDatabaseArchived } from "./databaseArchive";
import { WikiViewsContext } from "./wikiViewsContext";
import { useDatabaseChromeMarks } from "./databaseBlockRowMarks";

export type DatabaseViewPlacement = "inline" | "page" | "loading";
export type DatabaseViewOpenMode = "side" | "center" | "full" | "new_tab";

export interface DatabaseViewSnapshot {
  snapshotIdentity: string;
  contextIdentity: string;
  requestGeneration: number;
  placement: DatabaseViewPlacement;
  block: GraphBlock;
  dataSource: GraphDataSource;
  /**
   * The page the database's pages are laid out from: its own page, or the
   * page an inline database sits on (its "레이아웃 편집", as Notion's).
   */
  layoutPageId?: string;
  /** The page holding the database, as a sidebar view section names it. */
  holderPageId?: string;
  displayTitle: string;
  views: readonly GraphView[];
  activeView: GraphView;
  properties: readonly GraphProperty[];
  rows: readonly GraphRow[];
  // Archived rows are left out of the view but can still be opened.
  archivedRows?: readonly GraphRow[];
  // Rows in trash that the address opens; their page is read on its own.
  trashedRowIds?: readonly string[];
  readOnly: boolean;
}

export interface DatabaseViewOperationContext {
  contextIdentity: string;
  dataSourceId: string;
  requestGeneration: number;
  requestKey: string;
  signal: AbortSignal;
  snapshotIdentity: string;
  viewId: string;
}

export interface DatabaseViewOperations {
  isCurrent(context: DatabaseViewOperationContext): boolean;
  selectView(view: GraphView, context: DatabaseViewOperationContext): void | Promise<void>;
  /** The view shows at once, `shown` hearing of it in the same task; it answers the view once saved. */
  createView(type: GraphViewType, context: DatabaseViewOperationContext, shown?: (view: GraphView) => void): Promise<GraphView | null>;
  /** A person's change to the views (`step`), as tabs reordered, is a step of the page's ⌘Z; a copy's name and settings are part of making it. */
  updateViews(views: readonly GraphView[], context: DatabaseViewOperationContext, step?: boolean): Promise<void>;
  deleteView(view: GraphView, context: DatabaseViewOperationContext): Promise<void>;
  restoreView(view: GraphView, context: DatabaseViewOperationContext): Promise<void>;
  /** `fromMenu`: its menu's "다음에서 열기", which opens it as any page is (useDatabaseViewController). */
  openRow(rowId: string, mode: DatabaseViewOpenMode, context: DatabaseViewOperationContext, fromMenu?: boolean): void;
  onError(error: unknown, context: DatabaseViewOperationContext): void;
}

export function DatabaseView({
  snapshot,
  operations,
  databaseToolbar,
  filterBar,
  renderer,
  rowPreview,
  onNewViewSettings,
  onViewSettings,
  onCopyViewLink,
  title,
  archivedBar,
  sourceTrashedBar,
}: {
  snapshot: DatabaseViewSnapshot;
  operations: DatabaseViewOperations;
  databaseToolbar: ReactNode;
  /** The view's quick filter pills, on their own row under the toolbar. */
  filterBar?: ReactNode;
  renderer: ReactNode;
  rowPreview?: ReactNode;
  onNewViewSettings?(trigger: HTMLElement): void;
  onViewSettings?(trigger: HTMLElement): void;
  onCopyViewLink?(viewId: string): void;
  /** An inline database's title row, over its views. */
  title?: ReactNode;
  /** "보관된 페이지 보는 중", over a view showing its archived rows. */
  archivedBar?: ReactNode;
  /** The note over an inline database whose source is in the trash where it stands. */
  sourceTrashedBar?: ReactNode;
}) {
  const panelId = viewPanelId(snapshot.activeView.id);
  useViewSwitchGivesNoFocusBack(panelId);
  const archived = useDatabaseArchived();
  // On a phone an inline database with more views keeps its title on a row
  // of its own, "⌄" 4px after it, as Notion's (seen 2026-11-03 at 375px);
  // so does a touch screen wider than one, as Notion's iPad (seen 2026-11-14 at 834).
  const { touch } = useSidebarLayout(true);
  const [titleSlot, setTitleSlot] = useState<HTMLSpanElement | null>(null);
  const [sprintRow, setSprintRow] = useState<HTMLDivElement | null>(null);
  // On its own page its title row stands over its tabs, one tab or more (Notion's, seen 2026-11-09).
  const ownPage = useDatabaseOwnPage();
  // A wiki's database: its title is the wiki's, its views one picker.
  const wiki = useContext(WikiViewsContext);
  // Its tool row tells an inline database's editor row what it shows the "+ ⋮⋮" for (databaseBlockRowMarks).
  const chromeMarks = useDatabaseChromeMarks();
  if (snapshot.placement === "loading") {
    return <>{title}<div className={styles.dbLoading} data-database-loading role="status" /></>;
  }
  const titleRow = !wiki && viewToolsInTitleRow({ placement: snapshot.placement, viewCount: snapshot.views.length,
    titleShown: Boolean(title) && !ownPage && snapshot.activeView.config.showDataSourceTitle !== false });
  return (
    // The database's own pages and blocks are no wiki's.
    <WikiViewsContext.Provider value={null}>
    <DatabaseSprintRowContext.Provider value={sprintRow}>
    {titleRow || wiki ? null : title && touch && snapshot.placement === "inline" ? (
      <div className={styles.phoneTitleRow} data-phone-title-row>{title}<span ref={setTitleSlot} className={styles.phoneTitleSlot} /></div>
    ) : title}
    <section
      className={styles.db}
      data-database-view
      data-database-placement={snapshot.placement}
      data-placement={snapshot.placement}
      data-database-archived={archived ? "true" : undefined}
      data-viewing-archived-rows={archivedBar ? "true" : undefined}
    >
      <div className={styles.dbChrome} data-database-chrome data-title-row={titleRow ? "true" : undefined} data-active-view={snapshot.activeView.type} {...chromeMarks}>
        {titleRow ? title : null}
        <DatabaseViewTabs snapshot={snapshot} operations={operations} panelId={panelId} onNewViewSettings={onNewViewSettings}
          onViewSettings={onViewSettings} onCopyViewLink={onCopyViewLink} titleRow={titleRow}
          titleActionsSlot={touch && !titleRow ? titleSlot : null} wiki={wiki ?? undefined} />
        {databaseToolbar}
      </div>
      <div className={styles.filterRow} data-filter-row>
        {filterBar}
        <div ref={setSprintRow} className={styles.sprintRow} data-sprint-row />
      </div>
      {sourceTrashedBar}
      {archived ? <DatabaseArchivedBanner /> : null}
      {archivedBar}
      <div
        className={styles.viewBody}
        data-view-type={snapshot.activeView.type}
        ref={snapshot.activeView.type === "feed" ? holdFeedInPageScroll : undefined}
        id={panelId}
        role={titleRow ? "region" : "tabpanel"}
        aria-labelledby={titleRow ? undefined : `database-view-tab-${snapshot.activeView.id}`}
        aria-label={titleRow ? viewDisplayName(snapshot.activeView) : undefined}
      >
        {renderer}
      </div>
      {rowPreview}
    </section>
    </DatabaseSprintRowContext.Provider>
    </WikiViewsContext.Provider>
  );
}
