import { FilterIcon, LightningIcon, Search, SlidersIcon, SortIcon } from "@/icons/leafen";
import type { PageHistoryRowMark } from "@/lib/pageHistoryChanges";
import type { ViewType } from "@/lib/types";
import type { PageHistoryDatabase } from "@/pageHistoryWire.generated";
import { ViewTypeIcon, viewDisplayName } from "./database/databaseViewShared";
import { PageHistoryDatabaseTable } from "./PageHistoryDatabaseTable";
import styles from "./PageHistoryDatabasePage.module.css";

/**
 * A full-page database's own body in a version's preview, as Notion's (seen
 * 2026-11-05): the page's title already names it, so its name isn't said
 * again; its view tabs, the first one open, and its tools stand over the
 * table across the preview's width. Nothing in it can be pressed.
 */
export function PageHistoryDatabasePage({ database, marks, renamed, changed, changeColor, workspaceId }: {
  database: PageHistoryDatabase;
  marks?: readonly PageHistoryRowMark[];
  renamed?: ReadonlySet<string>;
  /** The version changed the database itself, whose block is its bar of views: outlined and counted, as Notion's (seen 2026-11-15). */
  changed?: boolean;
  changeColor?: string;
  workspaceId?: string;
}) {
  return (
    <section className={styles.frame} data-history-database-page={database.blockId} aria-label={database.title}>
      <div className={styles.bar} data-history-changed={changed ? "true" : undefined}>
        <div className={styles.tabs}>
          {(database.views ?? []).map((view, index) => (
            <span key={index} className={styles.tab} data-open={index === 0 ? "true" : undefined}>
              <ViewTypeIcon type={view.type as ViewType} size="small" />
              <span className={styles.tabName}>{viewDisplayName(view)}</span>
            </span>
          ))}
        </div>
        <div className={styles.tools} aria-hidden="true">
          <FilterIcon size="small" />
          <SortIcon size="small" />
          <LightningIcon size="small" />
          <Search size="small" />
          <SlidersIcon size="small" />
        </div>
      </div>
      <PageHistoryDatabaseTable database={database} marks={marks} renamed={renamed} changeColor={changeColor} workspaceId={workspaceId} />
    </section>
  );
}
