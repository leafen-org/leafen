import { useEffect, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { useDragReorder } from "@/lib/useDragReorder";
import { useTranslation } from "react-i18next";
import type { GraphProperty } from "@/contentGraph";
import { DatabasePropertyPicker } from "@/components/database/DatabasePropertyPicker";
import { databaseViewLabels } from "@/components/database/databaseViewLabels";
import { ArchiveIcon, ArrowLeft, CheckIcon, ChevronRight, DragHandleIcon, EyeIcon, EyeSlashIcon, FilterIcon, GroupIcon, Search, SortIcon, X } from "@/icons/leafen";
import { isLibraryGroupColumn, type LibraryGroupColumn } from "./libraryGrouping";
import { LibraryColumnIcon } from "./LibraryColumnIcon";
import { libraryColumnsAll, libraryColumnsWith, moveLibraryColumn, type LibraryColumn, type LibraryColumns, type LibrarySettingsItem } from "./libraryModel";
import styles from "./LibraryPanel.module.css";
import { escapeLayerOver } from "@/lib/overlayLayer";

/**
 * The library's view settings, as Notion's "설정" opens them (seen
 * 2026-10-02 and 11-14): a 290px panel at the table's right, "설정 보기"
 * with the rows the view has (librarySettingsItems). "속성 표시 여부" and
 * the count shown lists the columns shown in the table ("표에 표시하기",
 * with "모두 숨기기") and those hidden from it ("표에서 숨기기", with "모두
 * 표시하기") under a search field, each with its eye, the shown ones dragged
 * by their handle into another order; the page name always shows, first.
 * "필터" asks which column to filter by ("필터 추가"), whose pill then opens
 * in the filter bar; "정렬" picks the sorts ("새 정렬") and lists them;
 * "그룹화" names the grouping, to change or stop ("없음"); "보관된 페이지
 * 포함" lists the archived pages too, ✓ while it does.
 */
export function LibrarySettingsPanel({
  items,
  columns,
  hidden,
  label,
  filterProperties,
  filterMenu,
  groupBy,
  sortSettings,
  sortCount,
  includeArchived,
  onChange,
  onGroup,
  onIncludeArchived,
  onClose,
}: {
  /** The rows the view's settings have. */
  items: readonly LibrarySettingsItem[];
  /** The columns shown, in their order. */
  columns: readonly LibraryColumn[];
  /** The columns hidden, in their order (libraryHiddenColumns). */
  hidden: readonly LibraryColumn[];
  label: (column: LibraryColumn) => string;
  filterProperties: readonly GraphProperty[];
  /** "필터 추가": the columns to filter by, or the advanced filter once it has rules. */
  filterMenu: ReactNode;
  groupBy: LibraryGroupColumn | null;
  /** "정렬": the view's sorts, as the toolbar's "정렬" edits them. */
  sortSettings: ReactNode;
  sortCount: number;
  includeArchived: boolean;
  onChange: (columns: LibraryColumns) => void;
  onGroup: (column: LibraryGroupColumn | null) => void;
  onIncludeArchived: (included: boolean) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation("library");
  const [page, setPage] = useState<"settings" | "properties" | "filters" | "sort" | "group">("settings");
  const [query, setQuery] = useState("");
  const search = useRef<HTMLInputElement>(null);
  const state = { columns: [...columns], hidden: [...hidden] };
  const matches = (column: LibraryColumn) => label(column).toLowerCase().includes(query.trim().toLowerCase());

  useEffect(() => {
    if (page === "properties") search.current?.focus({ preventScroll: true });
  }, [page]);

  function onKeyDown(event: KeyboardEvent<HTMLElement>) {
    if (event.key !== "Escape") return;
    // A picker or menu open over it takes the Escape and closes alone.
    if (escapeLayerOver(event.currentTarget, event.nativeEvent)) { event.stopPropagation(); return; }
    event.preventDefault();
    onClose();
  }

  const header = (title: string, back: boolean) => (
    <div className={styles.panelHeader} data-root={back ? undefined : ""}>
      {back ? (
        <button type="button" className={styles.panelIconButton} aria-label={t("back")} data-library-settings-back
          onClick={() => { setPage("settings"); setQuery(""); }}>
          <ArrowLeft size="small" aria-hidden="true" />
        </button>
      ) : null}
      <span className={styles.panelTitle}>{title}</span>
      <button type="button" className={styles.panelClose} aria-label={t("closeSettings")} data-library-settings-close
        onClick={onClose}>
        <X size="small" aria-hidden="true" />
      </button>
    </div>
  );
  // A shown column's row moves among the others, live under the drag; the
  // name stays first, the rows passing it by.
  const reorder = useDragReorder("library-column", (dragged, target, side) => onChange({ ...state, columns: moveLibraryColumn(
    columns, dragged as LibraryColumn, target as LibraryColumn, side === "after") }), { live: true });
  const dragProps = (column: LibraryColumn) => reorder.row(column, () => column !== "name", column !== "name");
  const row = (column: LibraryColumn, shown: boolean): ReactNode => (
    <div key={column} className={styles.propertyRow} data-library-property={column} data-shown={shown ? "true" : "false"}
      {...(shown ? dragProps(column) : {})}>
      {shown ? <span className={styles.propertyHandle} aria-hidden="true"><DragHandleIcon size="small" /></span> : null}
      <span className={styles.propertyIcon}><LibraryColumnIcon column={column} /></span>
      <span className={styles.propertyName}>{label(column)}</span>
      <button type="button" className={styles.panelIconButton} disabled={column === "name"}
        aria-label={t(shown ? "hideProperty" : "showProperty", { name: label(column) })} aria-pressed={shown}
        data-library-property-toggle={column} onClick={() => onChange(libraryColumnsWith(state, column, !shown))}>
        {shown ? <EyeIcon size="regular" aria-hidden="true" /> : <EyeSlashIcon size="regular" aria-hidden="true" />}
      </button>
    </div>
  );

  return (
    // A section, not an aside: the app styles every aside's buttons as the sidebar's.
    <section className={styles.panel} aria-label={t("viewSettings")} data-library-settings={page} onKeyDown={onKeyDown}>
      {page === "settings" ? (
        <>
          {header(t("viewSettings"), false)}
          {items.includes("properties") ? (
            <button type="button" className={styles.panelItem} data-library-settings-item="properties"
              onClick={() => setPage("properties")}>
              <EyeIcon size="regular" weight="regular" aria-hidden="true" />
              <span className={styles.panelItemLabel}>{t("propertyVisibility")}</span>
              <span className={styles.panelItemValue}>{columns.length}</span>
              <ChevronRight size="small" aria-hidden="true" />
            </button>
          ) : null}
          {items.includes("filter") ? (
            <button type="button" className={styles.panelItem} data-library-settings-item="filters"
              onClick={() => setPage("filters")}>
              <FilterIcon size="regular" aria-hidden="true" />
              <span className={styles.panelItemLabel}>{t("filter")}</span>
              <ChevronRight size="small" aria-hidden="true" />
            </button>
          ) : null}
          {items.includes("sort") ? (
            <button type="button" className={styles.panelItem} data-library-settings-item="sort" onClick={() => setPage("sort")}>
              <SortIcon size="regular" aria-hidden="true" />
              <span className={styles.panelItemLabel}>{t("sort")}</span>
              {sortCount ? <span className={styles.panelItemValue}>{databaseViewLabels().sortCountPill(sortCount)}</span> : null}
              <ChevronRight size="small" aria-hidden="true" />
            </button>
          ) : null}
          {items.includes("group") ? (
            <button type="button" className={styles.panelItem} data-library-settings-item="group" onClick={() => setPage("group")}>
              <GroupIcon size="regular" aria-hidden="true" />
              <span className={styles.panelItemLabel}>{t("group")}</span>
              {groupBy ? <span className={styles.panelItemValue}>{label(groupBy)}</span> : null}
              <ChevronRight size="small" aria-hidden="true" />
            </button>
          ) : null}
          {items.includes("archived") ? (
            <button type="button" className={styles.panelItem} role="menuitemcheckbox" aria-checked={includeArchived}
              data-library-settings-item="archived" onClick={() => onIncludeArchived(!includeArchived)}>
              <ArchiveIcon size="regular" aria-hidden="true" />
              <span className={styles.panelItemLabel}>{t("includeArchived")}</span>
              {includeArchived ? <CheckIcon size="small" aria-hidden="true" /> : null}
            </button>
          ) : null}
        </>
      ) : page === "group" ? (
        <>
          {header(databaseViewLabels().groupBy, true)}
          <DatabasePropertyPicker placeholder={t("searchProperties")} properties={filterProperties}
            leading={{ label: t("noGrouping"), onPick: () => { onGroup(null); setPage("settings"); } }}
            onPick={(property) => {
              if (isLibraryGroupColumn(property.id)) onGroup(property.id);
              setPage("settings");
            }} />
        </>
      ) : page === "sort" ? (
        <>
          {header(sortCount ? t("sort") : databaseViewLabels().sheetNewSort, true)}
          {sortSettings}
        </>
      ) : page === "filters" ? (
        <>
          {header(t("addFilter"), true)}
          {filterMenu}
        </>
      ) : (
        <>
          {header(t("propertyVisibility"), true)}
          <label className={styles.panelSearch}>
            <Search size="small" aria-hidden="true" />
            <input ref={search} value={query} placeholder={t("searchProperties")} aria-label={t("searchProperties")}
              onChange={(event) => setQuery(event.target.value)} />
          </label>
          <div className={styles.propertyGroup}>
            <span>{t("shownInTable")}</span>
            <button type="button" className={styles.groupAction} data-library-hide-all disabled={columns.length <= 1}
              onClick={() => onChange(libraryColumnsAll(state, false))}>
              {t("hideAll")}
            </button>
          </div>
          {columns.filter(matches).map((column) => row(column, true))}
          {hidden.length > 0 ? (
            <>
              <div className={styles.propertyGroup}>
                <span>{t("hiddenInTable")}</span>
                <button type="button" className={styles.groupAction} data-library-show-all
                  onClick={() => onChange(libraryColumnsAll(state, true))}>
                  {t("showAll")}
                </button>
              </div>
              {hidden.filter(matches).map((column) => row(column, false))}
            </>
          ) : null}
        </>
      )}
    </section>
  );
}
