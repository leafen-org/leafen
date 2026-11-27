import { createContext, type MouseEvent, useContext } from "react";
import { useTranslation } from "react-i18next";
import { PageTitleText } from "@/components/page/TitleWithCustomEmoji";
import { pageHref } from "@/lib/navigation";
import { openPagePeek } from "@/lib/pagePeek";
import { usePageNames } from "@/lib/pageNames";
import type { Page } from "@/lib/types";
import { PageIconGlyph } from "../page/PageIcon";
import { pageIconSource } from "./PropertyRelationCellView";
import styles from "./databaseSheet";

/** How a row's page opens a rollup's page pressed on it: in the view's peek that holds the row, beside it. */
export const RollupPageOpenContext = createContext<((pageId: string) => void) | null>(null);

/**
 * A rollup's pages shown as they are, each a link to its page, its icon and
 * underlined name pressed together (Notion's, seen 2026-11-12: in a table's
 * cell, on a board's, gallery's or list's card and on a row's page, where a
 * relation's pages are names only). A press opens the page as the view opens
 * its own rows — at the side, or in the middle where the view says so — and,
 * from a row's page or where no view opens it, at the side; ⌘, Ctrl or ⇧
 * leaves it to the browser.
 */
export function RollupPageLinks({ pages, readonly, asText, onOpenPage, hrefOf = pageHref }: {
  pages: readonly Page[];
  /** Whether it stands in a value of its own rather than an editable cell's box. */
  readonly?: boolean;
  /** Names only, where a press means something else (a relation picker's line). */
  asText?: boolean;
  onOpenPage?: (pageId: string) => void;
  hrefOf?: (pageId: string) => string;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation("propValue");
  const openFromRowPage = useContext(RollupPageOpenContext);
  function open(pageId: string, event: MouseEvent<HTMLAnchorElement>) {
    // Not the card's, the row's or the cell's press under it.
    event.stopPropagation();
    if (event.metaKey || event.ctrlKey || event.shiftKey) return;
    event.preventDefault();
    (onOpenPage ?? openFromRowPage ?? openPagePeek)(pageId);
  }
  return (
    <span className={styles.relationChips} data-readonly={readonly ? "true" : undefined} data-rollup-pages>
      {pages.map((page, index) => {
        const named = <>
          <span className={styles.relationPageIcon} aria-hidden="true"><PageIconGlyph page={pageIconSource(page)} size={18} /></span>
          <span className={styles.relationChipText}><span className={styles.relationChipName}><PageTitleText page={page} /></span></span>
        </>;
        return asText ? <span key={`${page.id}:${index}`} className={styles.relationChip}>{named}</span> : (
          <a key={`${page.id}:${index}`} className={styles.relationChip} href={hrefOf(page.id)} data-rollup-page-link
            aria-label={t("propValue:openRelation", { title: pageNames.title(page) })}
            onClick={(event) => open(page.id, event)} onAuxClick={(event) => event.stopPropagation()}>{named}</a>
        );
      })}
    </span>
  );
}
