import { useCallback, type MouseEvent as ReactMouseEvent } from "react";
import type { GraphDataSource } from "@/contentGraph";
import { ArrowUpRightIcon } from "@/icons/leafen";
import { inlineDatabaseFullPageHref } from "@/lib/inlineDatabaseFullPage";
import { pageHref } from "@/lib/navigation";
import { pushRoute } from "@/lib/router";
import { useProductPages } from "@/components/product/productPagesContext";
import { useProductPagesSelect } from "@/components/product/productPagesStore";
import { hasExplicitPageIcon, PageIcon } from "../page/PageIcon";
import { inlineDatabasePlaceholderTitle } from "../editor/databaseTitles";
import { INLINE_TITLE_ICON } from "./DatabaseTitleIcon";
import styles from "./InlineDatabaseLinkedTitle.module.css";

/**
 * A database's title while its view shows a source another database owns,
 * as Notion's (seen 2026-10-11): "↗" and that source's name, a link to its
 * database — its own page, or the page holding it opened at it — not a
 * field to rename it here. A source with an icon shows it with a small "↗"
 * over its lower right corner, inline and opened alone (seen 2026-11-08);
 * the icon in its 22px slot as a database's own title draws it
 * (INLINE_TITLE_ICON): a built-in icon 0.96 of it, an emoji 0.9, centred
 * (Notion's in Chrome at 1440, dpr 2: People's person in a ring 21.11 seen
 * 2026-11-19, a checkmark 21.12 and an emoji 19.8 on 2026-11-20).
 */
export function InlineDatabaseLinkedTitle({ source, containerId, className, workspaceId }: {
  source: GraphDataSource;
  /** The database block that owns the source. */
  containerId: string;
  className: string;
  workspaceId?: string;
}) {
  const pages = useProductPages();
  const sourcePage = source.pageId ? pages?.pages.find((page) => page.id === source.pageId) : undefined;
  // A database page keeps its icon on the page; an inline database's is its source's.
  const icon = sourcePage?.kind === "database" && hasExplicitPageIcon(sourcePage) ? sourcePage
    : { icon: source.icon ?? "", iconType: source.iconType ?? "none", workspaceId };
  const href = useLinkedSourceHref()(source, containerId);
  const open = (event: ReactMouseEvent<HTMLAnchorElement>) => {
    if (!href || event.metaKey || event.ctrlKey || event.shiftKey) return;
    event.preventDefault();
    pushRoute(href);
  };
  return (
    <a className={`${className} ${styles.linked}`} href={href} onClick={open}
      data-inline-database-title data-linked-source={source.id}>
      {hasExplicitPageIcon(icon) ? (
        <span className={styles.icon} data-linked-source-icon>
          <PageIcon className={INLINE_TITLE_ICON.className} page={icon} size={INLINE_TITLE_ICON.size(icon)} fallback="none" />
          <ArrowUpRightIcon className={styles.iconArrow} size="small" aria-hidden="true" />
        </span>
      ) : <ArrowUpRightIcon className={styles.arrow} size="regular" aria-hidden="true" />}
      <span className={styles.name}>{source.title.trim() || inlineDatabasePlaceholderTitle()}</span>
    </a>
  );
}

/**
 * Where a linked source opens — its database's own page, or the page holding
 * it opened at it: its title, the tab menu's "소스 데이터베이스 열기" and the
 * title menu's "데이터 소스 보기", as Notion's (seen 2026-11-04).
 */
export function useLinkedSourceHref() {
  // The database pages, which a list read again leaves as they were: a
  // database asking draws again only when they change (productPagesStore.tsx).
  const databasePages = useProductPagesSelect((pages) => (pages?.pages ?? []).flatMap((page) => page.kind === "database" ? [page.id] : []).join("\n"));
  return useCallback((source: GraphDataSource, containerId: string) => {
    if (!source.pageId) return undefined;
    return databasePages.split("\n").includes(source.pageId) ? pageHref(source.pageId) : inlineDatabaseFullPageHref(source.pageId, containerId);
  }, [databasePages]);
}
