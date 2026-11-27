import { useState, type ReactNode } from "react";
import { databaseViewLabels } from "@/components/database/databaseViewLabels";
import { PhoneSheet } from "@/components/shell/PhoneSheet";
import panelStyles from "./LibraryPanel.module.css";

/**
 * What the library row's "필터" opens: under the button, while no filter is
 * set, the columns to filter by (a column picked opens its pill); on a phone,
 * always, the filter sheet from the screen's foot as a database's phone
 * "필터" is and as Notion's phone library's is (seen 2026-11-15 in iPhone
 * Safari 402, its "최근" and "개인 페이지" tabs): "필터 없음" or "필터 N개"
 * over "정렬" with the order's column, then the filter list, its "정렬"
 * going on to the tab's sorts in the same sheet with "‹" (2026-11-15: a
 * phone's "필터" opened the desktop popover over the table). "최근" has its
 * "정렬 · 마지막 방문 시간" too (seen 2026-11-16).
 */
export function LibraryFilterMenu({ phone, label, closeLabel, filterCount, sortCount, menu, phoneList, phoneSorts, end, onClose }: {
  phone: boolean;
  label: string;
  closeLabel: string;
  filterCount: number;
  sortCount: number;
  /** The desktop's list of the columns to filter by. */
  menu: ReactNode;
  /** The phone's filter list (PhoneFilterList), given the "정렬" row's press. */
  phoneList(onSort?: () => void): ReactNode;
  /** The phone's sorts (DatabaseSortSettings as a sheet). */
  phoneSorts?: ReactNode;
  /** The desktop list's end against its button's (libraryMenuEnd). */
  end: number;
  onClose(): void;
}) {
  const [sorting, setSorting] = useState(false);
  if (phone) {
    const labels = databaseViewLabels();
    const sortsShown = sorting && Boolean(phoneSorts);
    return (
      <PhoneSheet title={sortsShown ? sortCount ? labels.sorts : labels.sheetNewSort : labels.sheetFilters(filterCount)} label={label}
        tone="settings" onClose={onClose} onBack={sortsShown ? () => setSorting(false) : undefined}
        data={{ "data-library-filter-menu": sortsShown ? "sorts" : "" }}>
        {sortsShown ? phoneSorts : phoneList(phoneSorts ? () => setSorting(true) : undefined)}
      </PhoneSheet>
    );
  }
  return <LibraryToolMenu label={label} closeLabel={closeLabel} end={end} marker="data-library-filter-menu" onClose={onClose}>{menu}</LibraryToolMenu>;
}

/**
 * Where the tab row's "필터" and "정렬" lists end, as an offset from their
 * button's end: both at one place, 12 from the window's edge and inside the
 * scrolling page, which they would otherwise widen — Notion's (seen
 * 2026-11-05; 2026-11-14 at 1440, "필터" and "정렬" both at 1138 to 1428) —
 * so a page narrowed by the side peek ends them at its own edge.
 */
export function libraryMenuEnd(button: HTMLElement) {
  const page = button.closest<HTMLElement>(".nscroll")?.getBoundingClientRect();
  const wrap = (button.parentElement ?? button).getBoundingClientRect();
  if (getComputedStyle(button).direction === "rtl") return Math.max(12, page?.left ?? 0) - wrap.left;
  const scroller = button.closest<HTMLElement>(".nscroll");
  return wrap.right - Math.min(window.innerWidth - 12, page && scroller ? page.left + scroller.clientWidth : Infinity);
}

/** The tab row's "필터" or "정렬" list under its button (libraryMenuEnd), closed by Escape or a press outside. */
export function LibraryToolMenu({ label, closeLabel, end, marker, onClose, children }: {
  label: string;
  closeLabel: string;
  end: number;
  marker: "data-library-filter-menu" | "data-library-sort-menu";
  onClose(): void;
  children: ReactNode;
}) {
  return (
    <>
      <button type="button" className={panelStyles.menuBackdrop} tabIndex={-1} aria-label={closeLabel} onClick={onClose} />
      <div className={panelStyles.filterMenu} style={{ insetInlineEnd: end }} role="dialog" aria-label={label} {...{ [marker]: "" }}
        onKeyDown={(event) => {
          if (event.key !== "Escape") return;
          event.preventDefault();
          onClose();
        }}>
        {children}
      </div>
    </>
  );
}
