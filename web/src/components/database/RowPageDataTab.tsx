import { useTranslation } from "react-i18next";
import type { PageLayoutTab } from "@/lib/databasePageLayout";
import { pageHref } from "@/lib/navigation";
import { usePublicPageHref } from "@/lib/publicPageLinks";
import { useRouter } from "@/lib/router";
import type { DbProperty, Page } from "@/lib/types";
import { PropertyIcon } from "./PropertyIcon";
import { PropValue, type PropertyValueReadContext } from "./PropValue";
import { valueAsIds } from "./rollup";
import styles from "./RowPageLayout.module.css";
import { untitledPageDisplayTitle } from "../../lib/pageTitle";
import { plainTitleOf } from "@/lib/closedMentions";

/** The pages a tab shows on the open page: those it relates to through the tab's relation, or all its data source's. */
function pageLayoutTabRows(tab: PageLayoutTab, row: Page | undefined, context: PropertyValueReadContext): Page[] {
  const pages = context.pagesById;
  if (tab.relationPropertyId) {
    return valueAsIds(row?.properties?.[tab.relationPropertyId], context.bounds.rollup)
      .flatMap((id) => (pages[id] && !pages[id].inTrash ? [pages[id]] : []));
  }
  return Object.values(pages).filter((page) => page.parentType === "database" && page.parentId === tab.dataSourceId && !page.inTrash);
}

/**
 * A data source's tab on a database page, as Notion's (seen 2026-10-12): a
 * table of the source's pages — those the page relates to through the tab's
 * relation, or all of them — with the source's properties as its columns.
 * A page's name opens it: on a published page, inside the site or link it
 * was opened from.
 */
export function RowPageDataTab({ tab, row, context, unloaded }: {
  tab: PageLayoutTab; row: Page | undefined; context: PropertyValueReadContext;
  /** What shows while the source's pages aren't read (a tab the layout builder just added). */
  unloaded?: string;
}) {
  const { t } = useTranslation("rowProperties");
  const router = useRouter();
  const publicHref = usePublicPageHref();
  const href = (pageId: string) => publicHref ? publicHref(pageId) : pageHref(pageId);
  const columns: readonly DbProperty[] = [...(context.propsByDatabaseId[tab.dataSourceId] ?? [])]
    .sort((left, right) => Number(right.type === "title") - Number(left.type === "title"));
  const rows = pageLayoutTabRows(tab, row, context);
  if (columns.length === 0) return <p className={styles.tabEmpty} data-row-layout-tab-unreadable>{unloaded ?? t("layoutTabUnreadable")}</p>;
  return (
    // Wider than the page, as on a phone, the table scrolls on its own, the page staying put.
    <div className={styles.tabTableScroll}>
    <div className={styles.tabTable} role="table" aria-label={tab.name} data-row-layout-tab-table={tab.id}>
      <div className={styles.tabTableRow} role="row">
        {columns.map((column) => (
          <span key={column.id} className={styles.tabTableHead} role="columnheader">
            <PropertyIcon property={column} size="small" /><span>{column.name}</span>
          </span>
        ))}
      </div>
      {rows.map((page) => (
        <div key={page.id} className={styles.tabTableRow} role="row" data-row-layout-tab-row={page.id}>
          {columns.map((column) => (
            <span key={column.id} className={styles.tabTableCell} role="cell">
              {column.type === "title" ? (
                <a href={href(page.id)} onClick={(event) => {
                  if (event.metaKey || event.ctrlKey || event.shiftKey || event.button !== 0) return;
                  event.preventDefault();
                  router.push(href(page.id));
                }}>{plainTitleOf(page) || untitledPageDisplayTitle(page)}</a>
              ) : <PropValue context={context} row={page} prop={column} interactive={false} />}
            </span>
          ))}
        </div>
      ))}
    </div>
    </div>
  );
}
