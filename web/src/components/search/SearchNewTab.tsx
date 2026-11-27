import { useTranslation } from "react-i18next";
import type { ListState } from "react-stately";
import { ComposeIcon } from "@/icons/leafen";
import { NativePickerOption } from "@/lib/nativeAutocomplete";
import type { SearchChoice } from "./SearchResults";
import { SearchRowEnter } from "./SearchRowEnter";
import styles from "./SearchDialog.module.css";
import cards from "./SearchHighlightPanel.module.css";

// A search for a new tab (the Mac and Windows apps' ⌘T and their strip's
// "+"), as Notion's app's (seen 2026-11-12, its search source read
// 2026-11-13): "새 탭에서 열기…" in the field and, before anything is typed,
// "새 페이지" over the visited pages, whose Enter makes a page and opens it
// in a new tab in front at the end; its preview is the card Notion's draws
// for it. Notion's "새 채팅" under it is its AI's.

/** The choice of the "새 페이지" row. */
export const NEW_PAGE_CHOICE = "create:page";

/** The "새 페이지" row: the compose mark, its name and, while chosen, "↵". */
export function SearchNewPageRow({ state }: { state: ListState<SearchChoice> }) {
  const { t } = useTranslation("searchDialog");
  return (
    <section className={styles.section} data-search-new-tab-actions>
      <NativePickerOption state={state} itemKey={NEW_PAGE_CHOICE} className={`${styles.result} ${styles.resultCompact}`}
        data-kind="new-page" data-search-new-page>
        {({ active, labelProps }) => <>
          <span className={styles.pageIcon}><ComposeIcon size="regular" aria-hidden="true" /></span>
          <span {...labelProps} className={styles.compactTitle}>{t("newTab.newPage")}</span>
          <SearchRowEnter shown={active} />
        </>}
      </NativePickerOption>
    </section>
  );
}

/** The preview of the "새 페이지" row, Notion's card for it: what it makes, and gray lines where a page would show. */
export function SearchNewPageCard() {
  const { t } = useTranslation("searchDialog");
  return (
    <aside className={cards.highlightPanel} aria-label={t("highlights.label")} data-search-new-page-card>
      <div className={cards.highlightCard}>
        <div className={cards.highlightBand}>
          <span className={cards.highlightIcon}><ComposeIcon size="empty" aria-hidden="true" /></span>
        </div>
        <div className={cards.highlightBody}>
          <div className={cards.highlightKind}><span>{t("newTab.card.badge")}</span></div>
          <div className={cards.highlightPath}>{t("newTab.card.breadcrumb")}</div>
          <div className={cards.highlightTitle}>{t("newTab.card.title")}</div>
          <p className={cards.highlightLine}>{t("newTab.card.description")}</p>
          <div className={cards.highlightSkeleton} aria-hidden="true">
            {[100, 93, 100, 93, 39].map((width, index) => <span key={index} style={{ width: `${width}%` }} />)}
          </div>
        </div>
      </div>
    </aside>
  );
}
