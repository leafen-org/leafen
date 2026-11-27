import { useTranslation } from "react-i18next";
import { IconSvg } from "@/icons/leafen/iconPrimitives";
import styles from "./TrashView.module.css";

/**
 * What the trash's list says when it finds nothing, the window's and the
 * phone sheet's (TrashPhoneSheet) both: a search that finds nothing, filters
 * or not beside it, "일치하는 결과 없음"; filters alone that leave nothing
 * "표시할 결과 없음", as Notion's (seen 2026-11-13, 2026-11-15 and twice
 * 2026-11-16 in Chrome, 1440 dark — a search with filters beside it twice).
 */
export function trashNoResultsText(t: (key: string) => string, searched: boolean) {
  return t(searched ? "trashView:noSearchMatches" : "trashView:phone.nothingToShow");
}

/**
 * The trash window's list that finds nothing, as Notion's: in the middle of
 * the list's room a 22px trash can, crossed out for a search, 16 below it the
 * words (trashNoResultsText) in 17px/22 bold and, for a search, 8 below that
 * "오타가 있는지 확인해 보세요." in 14px/20 grey (seen 2026-11-13, 2026-11-15;
 * filters alone, an uncrossed can and the words alone, twice 2026-11-16).
 */
export function TrashNoMatches({ searched }: { searched: boolean }) {
  const { t } = useTranslation("trashView");
  return (
    <div className={styles.noResults} role="status" data-trash-no-matches={searched ? "search" : "filters"}>
      <IconSvg glyph={searched ? "trash-slash" : "trash"} pixels={22} aria-hidden="true">
        <path d="M5 6.5h10M8.2 6.5V5.2h3.6v1.3M6.4 6.5l.7 8.4c.05.6.5 1.1 1.1 1.1h3.6c.6 0 1.05-.5 1.1-1.1l.7-8.4" />
        {searched ? <path d="M4 4l12 12" /> : null}
      </IconSvg>
      <strong>{trashNoResultsText(t, searched)}</strong>
      {searched ? <span>{t("noSearchMatchesHint")}</span> : null}
    </div>
  );
}
