import { useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { CheckIcon, ChevronRight, DragHandleIcon } from "@/icons/leafen";
import { popoverAnchorOf, placeAround, placeBeside, popoverRectOf, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import { NativeMenuItem, useNativeMenu, useNativeSubmenu } from "@/lib/nativeMenu";
import { LibraryColumnIcon } from "./LibraryColumnIcon";
import { libraryTimeColumn, type LibraryColumn, type LibrarySort } from "./libraryModel";
import menuStyles from "./LibraryColumnMenu.module.css";
import styles from "./LibraryView.module.css";

/**
 * A library column's header and its menu, as Notion's (seen 2026-10-01,
 * 220px): "필터", which opens the column's pill in the filter bar, "정렬"
 * with its two orders beside it — A → Z and back for text, old → new and
 * back for times — "그룹화", which groups the view by the column or, ticked,
 * stops, "숨기기" for every column but the page name, and "콘텐츠
 * 줄바꿈하기". The source column doesn't filter, sort or group. The page
 * name's and the last edit's menus start with "속성 편집", which Notion's
 * library shows disabled (seen 2026-10-02). The menu sits over the window,
 * as the header cells and the table's scroll would clip it.
 */
type LibraryColumnMenuProps = {
  column: LibraryColumn;
  label: string;
  sort: LibrarySort | null;
  wrapped: boolean;
  /** Opens the column's filter pill; the source has none. */
  onFilter?: () => void;
  onSort: (sort: LibrarySort | null) => void;
  /** Whether the view is grouped by the column. */
  grouped?: boolean;
  /** Groups the view by the column, or stops when it is; the source has none. */
  onGroup?: () => void;
  /** Takes the column off the table; the page name has none. */
  onHide?: () => void;
  onWrap: (wrapped: boolean) => void;
};

export function LibraryColumnMenu(props: LibraryColumnMenuProps) {
  const [anchor, setAnchor] = useState<DOMRect | null>(null);
  const root = useRef<HTMLSpanElement>(null);
  return <span ref={root} className={styles.columnHead}>
    <button type="button" className={styles.columnButton} data-library-column={props.column} aria-haspopup="menu"
      aria-expanded={!!anchor} onClick={event => {
        // The opener read in the press: an updater may run again later, the event gone by (check-current-target-reads).
        const opener = event.currentTarget;
        setAnchor(current => current ? null : popoverAnchorOf(opener));
      }}>
      <span className={styles.headGlyph} aria-hidden="true">
        <span className={styles.headGlyphIcon}><LibraryColumnIcon column={props.column} /></span>
        <DragHandleIcon className={styles.headHandle} size="small" aria-hidden="true" />
      </span>
      <span className={styles.headLabel}>{props.label}</span>
    </button>
    {anchor ? <LibraryColumnPopover {...props} anchor={anchor} root={root} close={() => setAnchor(null)} /> : null}
  </span>;
}

/** Public submenu hooks are mounted with their real parent menu DOM. */
function LibraryColumnPopover({ column,
  label,
  sort,
  wrapped,
  onFilter,
  onSort,
  grouped = false,
  onGroup,
  onHide,
  onWrap,
  anchor, root, close,
}: LibraryColumnMenuProps & { anchor: DOMRect; root: React.RefObject<HTMLSpanElement | null>; close(): void }) {
  const { t } = useTranslation("library");
  const sortingRef = useRef<HTMLDivElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const sortingButton = useRef<HTMLButtonElement>(null);
  const place = usePopoverPlacement(menuRef, (size, viewport) => placeAround(
    anchor, size, viewport, { gap: 4 },
  ), [anchor], undefined, { kind: "picker", inside: () => [root.current],
    onEscape: close, onOutsidePress: close });
  const sortable = column !== "source";
  const items = [
    ...(column === "name" || column === "edited" ? ["edit-property"] : []),
    ...(onFilter ? ["filter"] : []), ...(sortable ? ["sort"] : []), ...(onGroup ? ["group"] : []),
    ...(onHide ? ["hide"] : []), "wrap",
  ];
  const actionText = (key: string) => t(key === "edit-property" ? "editProperty" : key === "wrap" ? "wrapContent" : key);
  const picker = useNativeMenu({ items, open: true, menuRef, label: t("columnMenu", { name: label }), onClose: close,
    keyOf: key => key, textOf: actionText, disabledKeys: ["edit-property"], onAction: key => {
      if (key === "sort") return;
      close();
      if (key === "filter") onFilter?.();
      else if (key === "group") onGroup?.();
      else if (key === "hide") onHide?.();
      else if (key === "wrap") onWrap(!wrapped);
    } });

  const submenu = useNativeSubmenu({ triggerKey: "sort", rootTrigger: picker.rootTrigger,
    parentMenuRef: menuRef, submenuRef: sortingRef, triggerRef: sortingButton });

  const current = sort?.column === column ? sort.direction : null;
  return createPortal(<>
        <div {...picker.menuProps} ref={menuRef} className={menuStyles.columnMenu}
          style={{ position: "fixed", ...popoverPlacementStyle(place) }}>
          {items.map(key => {
            const checked = key === "group" ? grouped : key === "wrap" ? wrapped : undefined;
            return <NativeMenuItem key={key} state={picker.state} itemKey={key} className={menuStyles.columnMenuItem}
              buttonRef={key === "sort" ? sortingButton : undefined} disabled={key === "edit-property"}
              role={checked === undefined ? "menuitem" : "menuitemcheckbox"} aria-checked={checked}
              itemOptions={key === "sort" ? submenu.submenuTriggerProps : undefined}
              data-library-column-action={key}>
              {({ labelProps }) => <><span {...labelProps}>{actionText(key)}</span>
                {key === "sort" ? <ChevronRight size="small" aria-hidden="true" />
                  : checked ? <CheckIcon size="small" aria-hidden="true" /> : null}</>}
            </NativeMenuItem>;
          })}
        </div>
        {submenu.state.isOpen && sortable ? <LibrarySortMenu column={column} current={current} parentRef={menuRef} triggerRef={sortingButton}
          menuRef={sortingRef} menuOptions={submenu.submenuProps} focusOnOpen={submenu.state.focusStrategy !== null}
          onBack={submenu.state.close} onClose={close} onSort={onSort} /> : null}
  </>, document.body);
}

function LibrarySortMenu({ column, current, parentRef, triggerRef, menuRef: ref, menuOptions, focusOnOpen, onBack, onClose, onSort }: {
  column: LibraryColumn; current: LibrarySort["direction"] | null;
  parentRef: React.RefObject<HTMLDivElement | null>; triggerRef: React.RefObject<HTMLButtonElement | null>;
  menuRef: React.RefObject<HTMLDivElement | null>;
  menuOptions: NonNullable<Parameters<typeof useNativeMenu>[0]["menuOptions"]>; focusOnOpen: boolean;
  onBack(): void; onClose(): void; onSort(sort: LibrarySort | null): void;
}) {
  const { t } = useTranslation("library");
  const time = libraryTimeColumn(column);
  const text = (direction: "ascending" | "descending") => t(time ? `sortTime.${direction}` : `sortText.${direction}`);
  const items = ["ascending", "descending"] as const;
  const picker = useNativeMenu({ items: [...items], open: true, menuRef: ref, label: t("sort"), menuOptions, focusOnOpen,
    keyOf: key => key, textOf: text, onAction: direction => {
      onSort(current === direction ? null : { column: column as LibrarySort["column"], direction }); onClose();
    } });
  const placement = usePopoverPlacement(ref, (size, viewport) => placeBeside(
    { ...popoverRectOf(parentRef.current)!, top: popoverRectOf(triggerRef.current)!.top,
      bottom: popoverRectOf(triggerRef.current)!.bottom }, size, viewport, { gap: -4 }), [], undefined,
    { kind: "menu", inside: () => [parentRef.current], onEscape: onBack, onOutsidePress: onClose });
  return <div {...picker.menuProps} ref={ref} className={menuStyles.columnSubmenu}
    style={{ position: "fixed", right: "auto", ...popoverPlacementStyle(placement) }}>
    {items.map(direction => <NativeMenuItem key={direction} state={picker.state} itemKey={direction} role="menuitemradio"
      aria-checked={current === direction} className={menuStyles.columnMenuItem} data-library-sort={direction}>
      {({ labelProps }) => <><span {...labelProps}>{text(direction)}</span>
        {current === direction ? <CheckIcon size="small" aria-hidden="true" /> : null}</>}
    </NativeMenuItem>)}
  </div>;
}
