import { useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { ChevronDown } from "@/icons/leafen";
import { SEARCH_SORTS, type SearchFilters } from "./searchFilterModel";
import dialogStyles from "./SearchFilterMenu.module.css";
import styles from "./SearchControls.module.css";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";

/**
 * The results' sort beside "검색 결과(N개)", as Notion's (seen 2026-09-30):
 * "결과 상위 일치" and the edited and created orders. Its menu, as Notion's
 * (seen 2026-11-14): 4px under the button from its start, 180px wide, the
 * order chosen marked only as the row the keys rest on, no "✓"; it stays open
 * as an order is chosen, the results sorting under it. It stands over the
 * window, past the results' edge, over the preview beside them.
 */
export function SearchSortMenu({ sort, onChange }: {
  sort: SearchFilters["sort"];
  onChange: (sort: SearchFilters["sort"]) => void;
}) {
  const { t } = useTranslation(["searchDialog"]);
  const [open, setOpen] = useState(false);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  // Under the button from its start, again as a new order relabels it.
  const place = usePopoverPlacement(menuRef, open ? (size, viewport) => placeAround(
    popoverRectOf(buttonRef.current) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport, { gap: 4 },
  ) : null, [open, sort], undefined, { kind: "menu", onEscape: () => setOpen(false), onOutsidePress: () => setOpen(false),
    inside: () => [buttonRef.current] });
  const menu = useNativeMenu({ items: [...SEARCH_SORTS], open, menuRef, label: t("searchDialog:filters.sort"),
    keyOf: option => option, textOf: option => t(`searchDialog:sort.${option}`), firstKey: sort,
    onAction: option => onChange(option) });

  return (
    <span className={styles.sort}>
      <button ref={buttonRef} type="button" className={styles.sortButton} data-search-filter="sort"
        aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen((current) => !current)}>
        {t(`searchDialog:sort.${sort}`)}
        <ChevronDown size="tiny" aria-hidden="true" />
      </button>
      {open ? createPortal(
        <div {...menu.menuProps} ref={menuRef} className={`${dialogStyles.filterMenu} ${styles.sortMenu}`} style={popoverPlacementStyle(place)}>
          {SEARCH_SORTS.map((option) => (
            <NativeMenuItem key={option} itemKey={option} state={menu.state} role="menuitemradio" aria-checked={option === sort}
              className={`${dialogStyles.filterMenuItem} ${styles.sortItem}`}>{({ labelProps }) => (
              <span {...labelProps} className={dialogStyles.filterMenuLabel}>{t(`searchDialog:sort.${option}`)}</span>
            )}</NativeMenuItem>
          ))}
        </div>,
        document.body,
      ) : null}
    </span>
  );
}
