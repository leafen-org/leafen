import { createContext, useContext, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { FilterIcon, GroupIcon } from "@/icons/leafen";
import { QuestionCircleIcon } from "@/icons/leafen/automationIcons";
import { HoverTip } from "../IconTipButton";
import type { DbProperty, DbView } from "@/lib/types";
import { Plus } from "../icons";
import styles from "./databaseSheet";
import { effectiveFilterGroup, type QueryBounds } from "./query";
import { useUntitledRowName } from "./untitledRowName";
import { DashboardWidgetEditingContext } from "./dashboardWidgetHeight";
import { useViewOffersNewRows } from "./databaseArchivedRowsContext";

/**
 * A view with nothing to show, as Notion's (seen 2026-10-11): a table or
 * list its search or filters leave empty, or a board with no column left to
 * show. In place of its rows, "필터 편집하기" — "검색 초기화" while it is
 * searched, "그룹 편집하기" with its group settings for a board no filter
 * narrows (`onEditGroups`; seen 2026-11-12, its groups hidden or empty and
 * hidden) — and "+ 새 페이지", centered, but for a dashboard widget in Edit
 * mode (Notion's, seen 2026-11-11). A gallery, feed, calendar, timeline, map
 * or chart keeps its own canvas instead. A page's relation tab with no page
 * related yet is such a view, its relation its filter, with "↗ 기존 항목
 * 연결하기" after (ViewNoResultsExtraContext; Notion's, seen 2026-11-12).
 * Their marks are 16px high and as wide as their glyphs, as Notion's
 * (seen 2026-11-12: 12.61, 10.52, 10.88 and 8.68 wide). A view with no row
 * at all that the one reading it may not add to — a web link's, a site's or
 * a view link's visitor, a guest who may only read — says "결과 없음" with
 * an ⓘ whose tip says why, in the box's middle, as Notion's (seen 2026-11-18
 * twice at 1440 as a guest who may only read: a full-page database's 144px
 * box and an inline one's 72, 978 and 708 wide under the head; its tip 300
 * wide over the words, collectionView.emptyState.noResultsTooltip).
 */
export function DatabaseNoResults({ searching, nothingToAdd = false, onEditFilters, onEditGroups, onClearSearch, onAddRow }: {
  searching: boolean;
  /** No row at all and none the reader may add: "결과 없음" and its ⓘ instead of the actions. */
  nothingToAdd?: boolean;
  onEditFilters?: (trigger: HTMLElement) => void;
  /** In place of "필터 편집하기" where no filter narrows it. */
  onEditGroups?: (trigger: HTMLElement) => void;
  onClearSearch?: () => void;
  onAddRow?: () => void;
}) {
  const { t } = useTranslation("boardView");
  const newPageLabel = useUntitledRowName();
  const widgetEditing = useContext(DashboardWidgetEditingContext);
  const extra = useContext(ViewNoResultsExtraContext);
  const offersNewRows = useViewOffersNewRows();
  if (nothingToAdd) {
    return (
      <div className={styles.boardNothingShown} data-view-no-results data-view-nothing-to-add>
        <HoverTip label={t("nothingToAddTip")} placement="top" wide className={styles.boardNothingNote}>
          {t("nothingToAdd")}<QuestionCircleIcon size="regular" aria-hidden="true" />
        </HoverTip>
      </div>
    );
  }
  return (
    <div className={styles.boardNothingShown} data-view-no-results>
      {searching ? onClearSearch && (
        <button type="button" className={styles.boardNothingAction} onClick={onClearSearch}>{t("clearSearch")}</button>
      ) : onEditGroups ? (
        <button type="button" className={styles.boardNothingAction} onClick={(event) => onEditGroups(event.currentTarget)}>
          <GroupIcon size="small" trim aria-hidden="true" />{t("editGroups")}
        </button>
      ) : onEditFilters && (
        <button type="button" className={styles.boardNothingAction} onClick={(event) => onEditFilters(event.currentTarget)}>
          <FilterIcon size="small" trim aria-hidden="true" />{t("editFilters")}
        </button>
      )}
      {onAddRow && offersNewRows && !widgetEditing && (
        <button type="button" className={styles.boardNothingAction} onClick={onAddRow}>
          <Plus size="small" trim aria-hidden="true" />{newPageLabel}
        </button>
      )}
      {widgetEditing ? null : extra}
    </div>
  );
}

/**
 * The "결과 없음" box (nothingToAdd) of a view showing nothing — no row, or
 * no group of a grouped one — that no search or filter narrows, to one who
 * may not add a row: a table's under its head, a list's under its tabs and a
 * gallery's across its grid alike (Notion's, seen 2026-11-18 as a guest who
 * may only read: the same 144px box with "결과 없음" and its ⓘ in a list
 * and a gallery). A board shows its empty groups, a calendar its days, a
 * timeline its empty lanes, a chart its empty axes instead, as Notion's.
 */
export function viewNothingToAdd(shown: number, queried: boolean, readOnly: boolean) {
  return shown === 0 && !queried && readOnly ? <DatabaseNoResults searching={false} nothingToAdd /> : null;
}

/** What follows a view's "+ 새 페이지" when it has nothing to show: a relation tab's "기존 항목 연결하기". */
export const ViewNoResultsExtraContext = createContext<ReactNode>(null);

/** Whether a view is searched, and whether a search or filter — its own or its dashboard's — narrows it. */
export function viewQuery(view: Pick<DbView, "config">, properties: readonly DbProperty[], bounds: QueryBounds, narrowedByDashboard = false) {
  const searching = typeof view.config?.search === "string" && view.config.search.trim() !== "";
  const filtered = narrowedByDashboard || !!effectiveFilterGroup(view.config, new Map(properties.map((property) => [property.id, property])), bounds);
  return { searching, queried: searching || filtered };
}

/** What a view's no-results state offers: its toolbar's filter menu and search. */
export type ViewNoResultsCommands = {
  openFilterSettings?: (trigger: HTMLElement) => void;
  clearSearch?: () => void;
};
