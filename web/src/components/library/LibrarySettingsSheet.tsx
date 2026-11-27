import { useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { GraphProperty } from "@/contentGraph";
import { DatabasePropertyPicker } from "@/components/database/DatabasePropertyPicker";
import { PropertyVisibilityGroup, PropertyVisibilityListBox } from "@/components/database/DatabasePropertyVisibilityList";
import listStyles from "@/components/database/DatabasePropertyVisibilityList.module.css";
import searchStyles from "@/components/database/DatabaseSourceProperties.module.css";
import { databaseViewLabels } from "@/components/database/databaseViewLabels";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow } from "@/components/shell/PhoneSheet";
import { SearchClearButton } from "@/components/PageActionSearch";
import { ArchiveIcon, CheckIcon, DragHandleIcon, EyeIcon, EyeSlashIcon, FilterIcon, GroupIcon, SortIcon } from "@/icons/leafen";
import { useDragReorder } from "@/lib/useDragReorder";
import { isLibraryGroupColumn, type LibraryGroupColumn } from "./libraryGrouping";
import { LibraryColumnIcon } from "./LibraryColumnIcon";
import { libraryColumnsAll, libraryColumnsWith, moveLibraryColumn, type LibraryColumn, type LibraryColumns, type LibrarySettingsItem } from "./libraryModel";
import styles from "./LibrarySettingsSheet.module.css";

type Screen = "settings" | "properties" | "filters" | "sort" | "group";

/**
 * The library's view settings on a phone, as Notion's phone opens them from
 * "설정" and a view's "보기 편집" (seen 2026-11-14 in iPhone Safari at 402):
 * a sheet "설정" with the rows the view has (librarySettingsItems) in one
 * white group — "속성 표시 여부" with the count shown, "필터", "정렬" with
 * what it sorts by, "그룹화" and "보관된 페이지 포함", ticked while on —
 * each opening its screen in the same sheet under its name with "‹" back, as
 * a database's settings sheet: the columns shown and hidden under a search
 * in the database's own groups and rows (PropertyVisibilityGroup), each with
 * its eye and the shown ones with their handle; a database's phone filters
 * and sorts; "그룹화 기준" with "없음" and the column checked.
 */
export function LibrarySettingsSheet({ items, columns, hidden, label, filterProperties, filters, filterCount, sorts, sortCount, sortLabel, groupBy,
  includeArchived, onChange, onGroup, onIncludeArchived, onClose }: {
  items: readonly LibrarySettingsItem[];
  columns: readonly LibraryColumn[];
  /** The columns hidden, in their order (libraryHiddenColumns). */
  hidden: readonly LibraryColumn[];
  label: (column: LibraryColumn) => string;
  filterProperties: readonly GraphProperty[];
  /** "필터": the phone's filter list, and how many filters the view has. */
  filters: ReactNode;
  filterCount: number;
  /** "정렬": the phone's sort list, how many sorts and the one sorted by. */
  sorts: ReactNode;
  sortCount: number;
  sortLabel?: string;
  groupBy: LibraryGroupColumn | null;
  includeArchived: boolean;
  onChange: (columns: LibraryColumns) => void;
  onGroup: (column: LibraryGroupColumn | null) => void;
  onIncludeArchived: (included: boolean) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation("library");
  const labels = databaseViewLabels();
  const [screen, setScreen] = useState<Screen>("settings");
  const [query, setQuery] = useState("");
  const searchRef = useRef<HTMLInputElement>(null);
  const state = { columns: [...columns], hidden: [...hidden] };
  const matches = (column: LibraryColumn) => label(column).toLowerCase().includes(query.trim().toLowerCase());
  const back = () => { setScreen("settings"); setQuery(""); };
  const reorder = useDragReorder("library-column", (dragged, target, side) => onChange({ ...state, columns: moveLibraryColumn(
    columns, dragged as LibraryColumn, target as LibraryColumn, side === "after") }), { live: true });
  const property = (column: LibraryColumn, shown: boolean) => (
    <div key={column} className={listStyles.row} data-library-property={column}
      {...(shown ? reorder.row(column, () => column !== "name", column !== "name") : {})}>
      <span className={listStyles.handle} aria-hidden="true"><DragHandleIcon size="regular" /></span>
      <span className={listStyles.icon} aria-hidden="true"><LibraryColumnIcon column={column} /></span>
      <span className={listStyles.name}>{label(column)}</span>
      <button type="button" className={listStyles.eye} disabled={column === "name"}
        aria-label={t(shown ? "hideProperty" : "showProperty", { name: label(column) })} aria-pressed={shown}
        data-library-property-toggle={column} onClick={() => onChange(libraryColumnsWith(state, column, !shown))}>
        {shown ? <EyeIcon size="regular" aria-hidden="true" /> : <EyeSlashIcon size="regular" aria-hidden="true" />}
      </button>
    </div>
  );
  // A group's name with its "모두" action, which a search leaves out, as Notion's.
  const group = (title: string, kind: "shown" | "hidden", action: { label: string; disabled?: boolean; run(): void; marker: string }, rows: ReactNode[]) => rows.length ? (
    <PropertyVisibilityGroup title={title} kind={kind} action={query.trim() ? null
      : { label: action.label, disabled: action.disabled, data: { [action.marker]: "" }, onPress: action.run }}>{rows}</PropertyVisibilityGroup>
  ) : null;
  const title = screen === "properties" ? t("propertyVisibility") : screen === "filters" ? labels.sheetFilters(filterCount)
    : screen === "sort" ? sortCount ? t("sort") : labels.sheetNewSort : screen === "group" ? labels.groupBy : t("settings");
  return (
    <PhoneSheet title={title} tone="settings" label={t("viewSettings")} onClose={onClose} onBack={screen === "settings" ? undefined : back}
      data={{ "data-library-settings": screen }}>
      {screen === "settings" ? (
        <PhoneSheetGroup>
          {items.includes("properties") ? <PhoneSheetRow icon={<EyeIcon size="regular" weight="regular" />} label={t("propertyVisibility")}
            value={columns.length} onPress={() => setScreen("properties")} data={{ "data-library-settings-item": "properties" }} /> : null}
          {items.includes("filter") ? <PhoneSheetRow icon={<FilterIcon size="regular" />} label={t("filter")}
            onPress={() => setScreen("filters")} data={{ "data-library-settings-item": "filters" }} /> : null}
          {items.includes("sort") ? <PhoneSheetRow icon={<SortIcon size="regular" />} label={t("sort")}
            value={sortCount > 1 ? String(sortCount) : sortLabel} onPress={() => setScreen("sort")} data={{ "data-library-settings-item": "sort" }} /> : null}
          {items.includes("group") ? <PhoneSheetRow icon={<GroupIcon size="regular" />} label={t("group")}
            value={groupBy ? label(groupBy) : undefined} onPress={() => setScreen("group")} data={{ "data-library-settings-item": "group" }} /> : null}
          {items.includes("archived") ? <PhoneSheetRow icon={<ArchiveIcon size="regular" />} label={t("includeArchived")} more={false}
            value={includeArchived ? <CheckIcon size="small" className={styles.check} aria-hidden="true" /> : undefined}
            onPress={() => onIncludeArchived(!includeArchived)} data={{ "data-library-settings-item": "archived" }} /> : null}
        </PhoneSheetGroup>
      ) : screen === "properties" ? (
        <div className={styles.screen} data-phone-settings>
          <label className={searchStyles.sourcePropertySearch}>
            <input ref={searchRef} value={query} placeholder={t("searchProperties")} aria-label={t("searchProperties")} onChange={(event) => setQuery(event.target.value)} />
            <SearchClearButton value={query} inputRef={searchRef} className={searchStyles.sourcePropertySearchClear} onClear={() => setQuery("")} />
          </label>
          <PropertyVisibilityListBox searching={Boolean(query.trim())}>
          {group(t("shownInTable"), "shown", { label: t("hideAll"), disabled: columns.length <= 1, run: () => onChange(libraryColumnsAll(state, false)), marker: "data-library-hide-all" },
            columns.filter(matches).map((column) => property(column, true)))}
          {group(t("hiddenInTable"), "hidden", { label: t("showAll"), run: () => onChange(libraryColumnsAll(state, true)), marker: "data-library-show-all" },
            hidden.filter(matches).map((column) => property(column, false)))}
          </PropertyVisibilityListBox>
        </div>
      ) : screen === "filters" ? filters
        : screen === "sort" ? sorts
        : (
          <DatabasePropertyPicker placeholder={t("searchProperties")} properties={filterProperties} sheet sheetOpensMore={false} leadingApart={false}
            checked={groupBy ?? ""} leading={{ label: t("noGrouping"), onPick: () => { onGroup(null); back(); } }}
            onPick={(picked) => { if (isLibraryGroupColumn(picked.id)) onGroup(picked.id); back(); }} />
        )}
    </PhoneSheet>
  );
}
