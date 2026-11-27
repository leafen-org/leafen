import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { CheckIcon } from "@/icons/leafen";
import { DataSourceMark } from "../database/DataSourceMark";
import type { DatabaseContainerSource } from "@/lib/databaseContainerPicker";
import type { Session } from "@/types";
import { popoverAnchorOf, popoverPlacementStyle, popoverRectOf, usePopoverPlacement, type PopoverRect } from "@/lib/popoverPlacement";
import { styles } from "./sidebarStyles";
import { NativePickerOption, useNativeAutocomplete } from "@/lib/nativeAutocomplete";
import { plainTitleOf } from "@/lib/closedMentions";
import { dataSourceDisplayTitle } from "@/components/editor/databaseTitles";
import { sidebarDatabasePlace } from "@/lib/pagePath";
import { pagesByIdOf } from "@/lib/pageIndex";
import { useProductPages } from "@/components/product/productPagesContext";
import { PlaceLine } from "../PlaceLine";
import { SearchClearButton } from "@/components/PageActionSearch";
import { placeSidebarPickerByRow } from "./SidebarPagePicker";
import type { SidebarBesidePlace } from "./SidebarActionMenu";
import { closeOnOwnEscape, sidebarPickerBesideRow, sidebarPickerSearchFirst, useSidebarPickerOpened } from "./sidebarPickerSearch";

const SEARCH_DELAY_MS = 150;
// The first twenty, searched or not, with no "N개 더 보기" and none read on as
// it scrolls, as Notion's "데이터베이스 보기" lists them (seen 2026-11-18 in
// Chrome 1440 with over a hundred sources and a search finding 71).
const LISTED = 20;

/**
 * A listed source's name: its own, else "새 데이터베이스" as its database's
 * only one, "새 데이터 소스" as one of several (untitledDataSourceTitle), as
 * Notion's "데이터베이스 보기" and "출처" list them (seen 2026-11-18, twice).
 */
function pickerSourceName(source: DatabaseContainerSource) {
  return dataSourceDisplayTitle(plainTitleOf(source), source.sourceTitles?.length ?? 1);
}

/**
 * The menu row the picker opens from ("데이터베이스 보기", a view section's
 * "출처"), live, the row its `contextElement` (placeSidebarPickerByRow).
 */
export function sidebarViewPickerAnchor({ main, trigger }: Pick<SidebarBesidePlace, "main" | "trigger">): PopoverRect {
  return trigger.current ? popoverAnchorOf(trigger.current) : popoverRectOf(main.current) ?? { top: 0, bottom: 0, left: 0, right: 0 };
}

/**
 * The databases "섹션 추가" → "데이터베이스 보기" offers, as Notion's (seen
 * 2026-10-01): a 300px window with "데이터 소스 검색...", each database by
 * its name and where it is, a path under it (sidebarDatabasePlace, seen
 * 2026-11-18), as tall as its rows up to 380 and scrolling its field away
 * with them, the page picker's frame (SIDEBAR_PICKER_HEIGHT, seen
 * 2026-11-18). Choosing one puts its view on the tab.
 */
export function SidebarViewPicker({ anchor, session, workspaceId, checkedSourceId, searchFirst, onChoose, onClose }: {
  /** Beside its menu row ("데이터베이스 보기", "출처"), that row its `contextElement` (sidebarViewPickerAnchor). */
  anchor: PopoverRect;
  /**
   * Opened from the keys, its search takes the focus and its first row is
   * lit; pressed open, the window takes the focus itself, nothing lit, a
   * letter typed going nowhere, the first time it opens since the app
   * loaded (sidebarPickerSearchFirst) — after that the search takes it, nothing lit.
   */
  searchFirst: boolean;
  session: Session;
  /** The data source a section shows, checked, as Notion's "출처" checks it. */
  checkedSourceId?: string;
  workspaceId: string;
  onChoose: (source: DatabaseContainerSource) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation("sidebar");
  const inputRef = useRef<HTMLInputElement>(null);
  const pickerRef = useRef<HTMLDivElement>(null);
  const [query, setQuery] = useState("");
  const [sources, setSources] = useState<DatabaseContainerSource[] | null>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const pagesById = pagesByIdOf(useProductPages()?.pages ?? []);

  useEffect(() => {
    (sidebarPickerSearchFirst("dataSource", searchFirst) ? inputRef.current : pickerRef.current)?.focus({ preventScroll: true });
    // Once, as it opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  useSidebarPickerOpened("dataSource");

  useEffect(() => {
    const controller = new AbortController();
    // Its rows are asked for at once as it opens, as tall as they come; a
    // search waits for the typing to rest.
    const timer = window.setTimeout(() => {
      void api.databaseContainerSources(session, workspaceId, query.trim(), { limit: LISTED, signal: controller.signal })
        .then(response => { if (!controller.signal.aborted) setSources(response.items); })
        .catch(() => {
          if (!controller.signal.aborted) setSources([]);
        });
    }, query.trim() ? SEARCH_DELAY_MS : 0);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [query, session, workspaceId]);

  const listed = sources ?? [];
  const collection = useNativeAutocomplete({ items: listed, query, onQuery: setQuery, inputRef, listRef,
    popoverRef: pickerRef, label: t("searchDataSources"), keyOf: source => `${source.id}:${source.dataSourceId}`,
    textOf: pickerSourceName, onPick: onChoose, onClose,
    // Pressed open, no row is lit, as Notion's (SidebarChoiceMenu).
    highlightFirst: searchFirst });
  // It floats over the page beside its menu row, or under it where the window
  // has no room beside it, as the shortcut's page lists.
  const place = usePopoverPlacement(pickerRef, (size, viewport) => placeSidebarPickerByRow(anchor, size, viewport), [anchor], undefined,
    { kind: anchor.contextElement ? "submenu" : "picker", onEscape: onClose, onOutsidePress: onClose, ...sidebarPickerBesideRow(anchor.contextElement) });
  const picker = (
    <div className={styles.viewPicker} ref={pickerRef} style={popoverPlacementStyle(place)} role="dialog" aria-label={t("databaseView")} tabIndex={-1} data-sidebar-view-picker onKeyDown={closeOnOwnEscape(onClose)}>
      {/* The page picker's field and rows (SidebarViews.module.css), as Notion's
          two side by side (seen 2026-11-18): no icon, "입력 값 지우기" once typed in. */}
      <label className={styles.viewPickerSearch}>
        <input {...collection.inputProps} ref={inputRef} placeholder={t("searchDataSources")} />
        <SearchClearButton value={query} inputRef={inputRef} onClear={() => setQuery("")} />
      </label>
      <div {...collection.listBoxProps} ref={listRef} className={styles.viewPickerList}>
        {listed.map(source => {
          const where = sidebarDatabasePlace(source, pagesById);
          return (
          <NativePickerOption key={`${source.id}:${source.dataSourceId}`} state={collection.state}
            itemKey={`${source.id}:${source.dataSourceId}`} className={styles.viewPickerRow}
            data-sidebar-view-source={source.dataSourceId} data-described={where.length ? "true" : undefined}>
            {({ labelProps, placeProps }) => <>
            <DataSourceMark source={source} />
            <span className={styles.viewPickerText}>
              <span {...labelProps}>{pickerSourceName(source)}</span>
              {where.length ? <PlaceLine {...placeProps} place={where} /> : null}
            </span>
            {checkedSourceId === source.dataSourceId ? <CheckIcon size="small" aria-hidden="true" /> : null}
            </>}
          </NativePickerOption>
          );
        })}
        {sources && listed.length === 0 ? <p className={styles.customizeEmpty}>{t("noDataSources")}</p> : null}
      </div>
    </div>
  );
  return typeof document === "undefined" ? picker : createPortal(picker, document.body);
}
