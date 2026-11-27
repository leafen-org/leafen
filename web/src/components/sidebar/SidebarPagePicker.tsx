import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { usePageVisits } from "@/components/library/useLibraryData";
import { PageIconGlyph } from "@/components/page/PageIcon";
import { LockIcon } from "@/icons/leafen";
import { pagePlace, type Place } from "@/lib/pagePath";
import { PlaceLine } from "@/components/PlaceLine";
import { SearchClearButton } from "@/components/PageActionSearch";
import { usePageNames } from "@/lib/pageNames";
import { PageTitleText } from "@/components/page/TitleWithCustomEmoji";
import { searchWorkspacePages } from "@/lib/workspacePageSearch";
import type { Page, Session } from "@/types";
import { placeAround, placeBeside, POPOVER_MARGIN, popoverPlacementStyle, usePopoverPlacement,
  type PopoverRect, type PopoverRequest, type PopoverSize, type PopoverViewport } from "@/lib/popoverPlacement";
import { readsRightToLeft } from "@/lib/readingDirection";
import { styles } from "./sidebarStyles";
import { NativePickerOption, useNativeAutocomplete } from "@/lib/nativeAutocomplete";
import { pageTrashRoot } from "@/lib/pageTrash";
import { usePathsAboveShown } from "@/lib/usePagePathsAbove";
import { pageMapOf, pagesByIdOf } from "@/lib/pageIndex";
import { closeOnOwnEscape, sidebarPickerBesideRow, sidebarPickerSearchFirst, useSidebarPickerOpened, type SidebarPickerSearch } from "./sidebarPickerSearch";

const LISTED = 20;
const SEARCH_DELAY_MS = 150;
/**
 * The sidebar pickers' full height, this one's and SidebarViewPicker's: no
 * taller, as tall as its rows below it (SidebarViews.module.css .viewPicker),
 * cut to it where placed (the placing's `cap`, which stands over the class's
 * own). This one chooses its side as though this tall, so a short list stays
 * where the long one stood — beside its row, its top on the row's (Notion's
 * "단축키 추가 › 페이지", seen 2026-11-17 at 1440×723: 380 high beside the
 * row and slid up into the window, then 315 with seven found and 80 with
 * none, both back on the row's top).
 */
export const SIDEBAR_PICKER_HEIGHT = 380;
/**
 * Where a picker opening from a sidebar menu's row opens — the shortcut's
 * "새 페이지 ›" and "페이지 ›", "섹션 추가 › 데이터베이스 보기", a view
 * section's "출처" — as Notion's (seen 2026-11-18 in Chrome, 723 high, in two
 * tabs half an hour apart): beside the row, over the menu's edge by its
 * padding, its top on the row's, slid up into the window, where the window
 * has room for it there (at 1440, "새 페이지" at 498,213.9 by its row at
 * 213.9); else under the row from its start (at 800, by a menu ending at 502:
 * "새 페이지" at 266,241.9 under its row at 213.9..241.9), slid up over the
 * row where the room under it is too short — Notion's "페이지" (row at 366.8)
 * and "데이터베이스 보기" (row at 451.9) both stood at 266,358.8, 380 high,
 * their foot 16 past the window's; Leafen's is whole in the window, its foot
 * at the window's margin, so its last rows can be reached.
 */
export function placeSidebarPickerByRow(row: PopoverRect, size: PopoverSize, viewport: PopoverViewport): PopoverRequest {
  const room = readsRightToLeft(row.contextElement) ? row.left - POPOVER_MARGIN : viewport.width - POPOVER_MARGIN - row.right;
  return room >= size.width ? placeBeside(row, size, viewport, { cap: SIDEBAR_PICKER_HEIGHT })
    : placeAround(row, size, viewport, { gap: 0, cap: SIDEBAR_PICKER_HEIGHT, rise: true });
}

/**
 * The page a shortcut names, as Notion's "단축키 추가" asks for it (seen
 * 2026-10-01, 2026-10-31): a 300px window with a search field — "다음에 새
 * 페이지 추가" for a new page, which offers the reader's private pages
 * themselves first (their picture and "비공개"), "검색…" for a page — over
 * the pages visited last, or a search's pages, each with where it sits.
 */
export function SidebarPagePicker({ anchor, underRow = false, highlightFirst = true, opening, placeholder, rootLabel, rootIcon, pages, session, workspaceId, foundDatabase = "table", onChoose, onClose }: {
  /** Where it opens; a menu row's (`underRow`) is that row's, the row its `contextElement` (popoverAnchorOf). */
  anchor: PopoverRect;
  /**
   * A sidebar shortcut's list, which remembers its openings apart: pressed
   * open the first time since the app loaded, it holds the focus itself
   * (sidebarPickerSearchFirst). Without it, the search takes the focus as
   * the list opens.
   */
  opening?: { byKeys: boolean; search: SidebarPickerSearch };
  /** A menu row's page list: beside the row, or under it where the window has no room beside (placeSidebarPickerByRow). */
  underRow?: boolean;
  /** Whether the first row is highlighted before the arrows move; the shortcut lists' rows wait (Notion, seen 2026-10-31). */
  highlightFirst?: boolean;
  placeholder: string;
  /** The private pages' own row, for a new page put at their top. */
  rootLabel?: string;
  /** That row's picture; a lock where none is given. */
  rootIcon?: ReactNode;
  /**
   * How a search draws a database with no icon of its own: its table, as
   * Notion's "단축키 추가 › 페이지" (seen 2026-11-11), or the 20px cylinder
   * of Notion's "다음에 보기로 저장" (seen 2026-11-11) and a move window's
   * search (MoveDestinationRow).
   */
  foundDatabase?: "table" | "cylinder";
  pages: readonly Page[];
  session: Session;
  workspaceId: string;
  /** The page chosen, or "" for the private pages themselves. */
  onChoose: (pageId: string) => void;
  onClose: () => void;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation("sidebar");
  const inputRef = useRef<HTMLInputElement>(null);
  const pickerRef = useRef<HTMLDivElement>(null);
  const [query, setQuery] = useState("");
  const [found, setFound] = useState<readonly Page[]>([]);
  const listRef = useRef<HTMLDivElement>(null);
  const visits = usePageVisits(session, workspaceId);

  useEffect(() => {
    (!opening || sidebarPickerSearchFirst(opening.search, opening.byKeys) ? inputRef.current : pickerRef.current)?.focus({ preventScroll: true });
    // Once, as it opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  useSidebarPickerOpened(opening?.search);

  useEffect(() => {
    const trimmed = query.trim();
    if (!trimmed) return undefined;
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      void searchWorkspacePages(session, trimmed, LISTED, workspaceId, { signal: controller.signal })
        .then(result => { if (!controller.signal.aborted) setFound(result.pages.filter(page => !page.inTrash)); })
        .catch(() => {
          // The pages already known still answer the search.
        });
    }, SEARCH_DELAY_MS);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [query, session, workspaceId]);

  const choices = useMemo(() => {
    const byId = pageMapOf(pages);
    if (query.trim()) return found.slice(0, LISTED);
    const visited = [...new Set(visits.map(({ pageId }) => pageId))].flatMap((id) => {
      const page = byId.get(id);
      return page && !pageTrashRoot(page, (parentId) => byId.get(parentId)) ? [page] : [];
    });
    return (visited.length > 0 ? visited : pages.filter((page) => !page.parentId && !page.inTrash)).slice(0, LISTED);
  }, [found, pages, query, visits]);
  const pagesById = pagesByIdOf(pages);
  usePathsAboveShown(choices, pagesById);
  const rows: Array<{ id: string; page?: Page; place?: Place }> = [
    ...(rootLabel && !query.trim() ? [{ id: "" }] : []),
    ...choices.map((page) => ({ id: page.id, page, place: pagePlace(page, pagesById) })),
  ];
  const keyOf = (row: { id: string; page?: Page }) => row.page ? row.id : "__private_root";
  const collection = useNativeAutocomplete({ items: rows, query, onQuery: setQuery, inputRef, listRef,
    popoverRef: pickerRef, label: placeholder, keyOf, textOf: row => row.page ? pageNames.title(row.page) : rootLabel || "",
    highlightFirst, onPick: row => onChoose(row.id), onClose });
  // It floats over the page beside the sidebar's edge, inside the window.
  const place = usePopoverPlacement(pickerRef, (size, viewport) => underRow ? placeSidebarPickerByRow(anchor, size, viewport)
    : placeAround(anchor, size, viewport, { gap: 2, cap: SIDEBAR_PICKER_HEIGHT, sideBy: SIDEBAR_PICKER_HEIGHT }), [anchor, underRow], undefined,
    { kind: underRow ? "submenu" : "picker", onEscape: onClose, onOutsidePress: onClose, ...(underRow ? sidebarPickerBesideRow(anchor.contextElement) : {}) });

  const picker = (
    <div className={styles.viewPicker} ref={pickerRef} style={popoverPlacementStyle(place)} role="dialog" aria-label={placeholder} tabIndex={-1} data-sidebar-page-picker onKeyDown={closeOnOwnEscape(onClose)}>
      <label className={styles.viewPickerSearch}>
        <input {...collection.inputProps} ref={inputRef} placeholder={placeholder} />
        {/* Notion's "입력 값 지우기" here is the shared one: the field keeps the focus
            (seen 2026-11-17 at 1440, a letter typed after the press went into the search). */}
        <SearchClearButton value={query} inputRef={inputRef} onClear={() => setQuery("")} />
      </label>
      <div {...collection.listBoxProps} ref={listRef} className={styles.viewPickerList}>
        {rows.map(row => (
          <NativePickerOption key={keyOf(row)} state={collection.state} itemKey={keyOf(row)}
            className={styles.viewPickerRow} data-sidebar-page-choice={row.id || "root"} data-described={row.place?.length ? "true" : undefined}>
            {({ labelProps, placeProps }) => <>
            <span className={styles.pagePickerIcon}>
              {row.page ? <PageIconGlyph page={row.page} size={18} database={query.trim() ? foundDatabase : "table"} />
                : rootIcon ?? <LockIcon size="small" aria-hidden="true" />}
            </span>
            <span className={styles.viewPickerText}>
              <span {...labelProps}>{row.page ? <PageTitleText page={row.page} /> : rootLabel}</span>
              {row.place?.length ? <span {...placeProps}><PlaceLine place={row.place} /></span> : null}
            </span>
            </>}
          </NativePickerOption>
        ))}
        {rows.length === 0 && query.trim() ? <p className={styles.customizeEmpty}>{t("noDataSources")}</p> : null}
      </div>
    </div>
  );
  return typeof document === "undefined" ? picker : createPortal(picker, document.body);
}
