import { Fragment, useLayoutEffect, useRef, useState, type ReactNode, type RefObject } from "react";
import { createPortal } from "react-dom";
import { CheckIcon, ChevronRight, Search } from "@/icons/leafen";
import { focusWhenFree } from "@/lib/focusThroughWaits";
import { besideRowOffset } from "@/lib/besideRowOffset";
import { NativeMenuItem, useNativeMenu, useNativeSubmenu } from "@/lib/nativeMenu";
import { menuTakesFocus } from "@/lib/menuTakesFocus";
import { placeAround, placeBeside, popoverRectOf, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import { useHoverTip } from "@/components/IconTipButton";
import { SwitchMark } from "@/components/Switch";
import { useMenuEscape } from "./sidebarMenuEscape";
import { sidebarPickerSearchFirst, useSidebarPickerOpened, type SidebarPickerSearch } from "./sidebarPickerSearch";
import { styles } from "./sidebarStyles";

export type SidebarMenuChoice = {
  key: string; label: string; icon?: ReactNode; checked?: boolean; disabled?: boolean;
  data?: Record<string, string>; run(): void;
};
export type SidebarMenuSubmenu = {
  label: string; width?: number; gap?: number; data?: Record<string, string>; radio?: boolean;
  choices?: SidebarMenuChoice[];
  /** A search over the choices; `bare` draws it without the magnifier, as Notion's "팀스페이스 검색…" (seen 2026-11-11);
   *  `remembers`, the picker whose openings it counts (sidebarPickerSearchFirst). */
  search?: { label: string; query: string; onQuery(query: string): void; bare?: boolean; remembers?: SidebarPickerSearch };
  /** Content that is not a choice list (a name field, another picker). */
  custom?: (place: SidebarBesidePlace) => ReactNode;
};
export type SidebarMenuRow = {
  key: string; label: string; icon?: ReactNode; value?: string; disabled?: boolean; separatorBefore?: boolean;
  /** A line under the label saying what it does; title, why it is dim, in
   *  the dark tip before the row (a lit row's stays the browser's). */
  description?: string; title?: string;
  data?: Record<string, string>; checked?: boolean; run?: () => void; submenu?: SidebarMenuSubmenu;
  /** Draws "checked" as a switch at the row's end, as Notion's on/off rows. */
  asSwitch?: boolean;
  /** A row that removes or revokes, in red, as Notion's settings table menus
   *  draw "팀스페이스 보관" (seen 2026-11-05). */
  danger?: boolean;
  /** A row that opens something further, its "›" at its end (Notion's "내 작업" "작업 출처", seen 2026-11-08). */
  next?: boolean;
  /** A quiet word right after the label, set off by a dash (Notion's person's
   *  teamspaces "Leafen Test Team — 2명의 멤버", seen 2026-11-09). */
  note?: string;
};
/** Where a submenu opens beside its row; `byKeys`, opened from the keyboard rather than by the pointer. */
export type SidebarBesidePlace = { main: RefObject<HTMLDivElement | null>; trigger: RefObject<HTMLButtonElement | null>; back(): void; byKeys: boolean };

/**
 * A sidebar "…" menu, as Notion's are: rows, some opening their choices
 * beside the row. Aria owns the menu's and submenus' focus, keys and hover;
 * the open menu keeps DOM focus and moves its highlight. Escape closes what
 * opened last wherever the keys are (before a peek or selection hears it).
 */
/**
 * A menu's width as Notion's, grown by up to FONT_SLACK for a row whose words
 * the product's own typefaces draw wider (the typeface decision of 2026-10-30
 * B approves their few pixels but not a cut value — "다음에서 열기 · 전체 페이지
 * 보기" fills Notion's 240px exactly). A menu with a search keeps its width.
 */
const FONT_SLACK = 24;
function menuWidth(width: number | undefined, search: boolean, fit?: number, content?: boolean) {
  if (content && !search) return { width: "max-content", maxWidth: "calc(100vw - 24px)" };
  if (fit !== undefined && !search) return { width: "max-content", maxWidth: fit };
  return width === undefined || search ? { width } : { minWidth: width, width: "max-content", maxWidth: width + FONT_SLACK };
}

export function SidebarActionMenu({ button, label, width, fit, contentWidth = false, rows, backdropLabel, close, search, shift = 0, gap = 4, heading, highlightFirst = false, current, pastWindow = false, data }: {
  button: RefObject<HTMLButtonElement | null>; label: string; width?: number; rows: SidebarMenuRow[];
  /** As wide as its rows, up to this (Notion's person's teamspaces, 350px — its code, 2026-11-09). */
  fit?: number;
  /** As wide as its rows, laid out as Notion's plain menu rows: each at least
   *  200px, its icon 8px from the words, a row's note at least 230px wide
   *  (Notion's teamspace "…", seen 2026-11-11). */
  contentWidth?: boolean;
  backdropLabel: string; close(restoreFocus?: boolean): void;
  /** A question over the rows, not a row (Notion's asks where to make the task, seen 2026-11-08). */
  heading?: string;
  /** Its first row lit as it opens, however it was opened (Notion's "내 작업" "새 작업", seen 2026-11-08). */
  highlightFirst?: boolean;
  data?: Record<string, string>;
  /** A list of any length gets a search above it and scrolls, as Notion's pickers do. */
  search?: { label: string; query: string; onQuery(query: string): void };
  /** How far left of the button the menu starts, as Notion's "더 보기" lines its rows' icons up with the button's. */
  shift?: number;
  /** How far from the button it opens: 4px, as Notion's menus; its "더 보기" stands 6px over it. */
  gap?: number;
  /** The row standing for what is chosen now, lit as it opens (Notion's settings teamspace "공개 ⌄", seen 2026-11-11). */
  current?: string;
  /** Opens from the button's start even past its window's edge, to the screen's, as Notion's settings teamspace "…" (seen 2026-11-11). */
  pastWindow?: boolean;
}) {
  const menu = useRef<HTMLDivElement>(null), shade = useRef<HTMLButtonElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  // Why a dim row is dim, in the dark tip before it, level with it and
  // wrapped at its width, 6px off, once the pointer has stayed on it a
  // moment; where that side has no room it goes after the row (Notion's
  // teamspace "…", seen 2026-11-11: about 0.18s).
  const reason = useHoverTip({ delay: 180, maxWidth: "anchor", placement: "left" });
  // Its shade is its own: a press outside a picker beside it closes it too (sidebarPickerBesideRow).
  const position = usePopoverPlacement(menu, (size, viewport) => {
    const at = popoverRectOf(button.current)!;
    // In a window (the settings window's tables) the menu stays inside it:
    // where it would run past the window's right edge it ends at the
    // button's right instead, as that window's other menus do (2026-11-07).
    const holder = button.current?.closest<HTMLElement>('[role="dialog"], [role="alertdialog"]')?.getBoundingClientRect();
    const align = !pastWindow && holder && at.left - shift + size.width > holder.right - 8 ? "end" : "start";
    return placeAround({ ...at, left: at.left - shift, right: at.right - (align === "end" ? 0 : shift) }, size, viewport, { gap, align });
  }, [gap, pastWindow], undefined, { kind: "menu", restoreFocus: false, modalOwned: () => [shade.current] });
  // The open menu takes focus, as Notion's do, so what is typed goes to it
  // and not to the page behind (2026-10-16: typing with the new-page menu open
  // went into the editor).
  const picker = useNativeMenu({ items: rows, open: position !== null, menuRef: menu, label,
    // Opened with the pointer it lights no row, from the keyboard its first,
    // as Notion's teamspace "…" (seen 2026-11-06).
    ...(search ? { focusOnOpen: true, highlightOnOpen: false, focusOwnerRef: searchRef }
      : highlightFirst || current !== undefined ? { focusOnOpen: true, highlightOnOpen: true, firstKey: current }
        : { focusOnOpen: true, quietOnPointer: true }),
    disabledKeys: rows.filter((row) => row.disabled).map((row) => row.key),
    keyOf: (row) => row.key, textOf: (row) => row.label, onAction: (row) => row.run?.() });
  // In a window, a menu that closes gives the focus back to its button —
  // once its action frees it — as Notion's do, unless what it did took the
  // focus on (a field, a window); else the window's keeper gave it to the
  // first control near the menu (2026-11-06: a server user's "기기 등록 코드"
  // left it on the users' search). On the page a closed menu leaves it to
  // what its caller says (close(restoreFocus)).
  useLayoutEffect(() => {
    const opener = button.current;
    if (!opener?.closest("[role=dialog], dialog")) return undefined;
    const root = menu.current;
    return () => {
      queueMicrotask(() => {
        const active = document.activeElement;
        const lost = !active || active === document.body || active === document.documentElement || Boolean(root?.contains(active));
        if (lost && opener.isConnected) focusWhenFree(opener);
      });
    };
  }, [button]);
  useMenuEscape(true, () => {
    const expanded = picker.rootTrigger.expandedKeysStack[0];
    if (expanded === undefined) { close(); return; }
    picker.rootTrigger.closeSubmenu(expanded, 0);
    menuTakesFocus(menu.current);
  });
  return (
    <>
      <button ref={shade} type="button" className={styles.moreMenuBackdrop} tabIndex={-1} aria-label={backdropLabel} onClick={() => close()} />
      <div {...picker.menuProps} ref={menu} className={styles.sectionMenu} {...data} data-content-width={contentWidth ? "" : undefined}
        style={{ ...popoverPlacementStyle(position), ...menuWidth(width, Boolean(search), fit, contentWidth), ...(search ? { maxHeight: "min(420px, 70dvh)", overflowY: "auto" } : {}) }}>
        {search ? (
          <label className={styles.viewChoiceSearch} style={{ position: "sticky", top: 0, zIndex: 1, background: "var(--bg-elevated)" }}>
            <Search size="small" aria-hidden="true" />
            <input ref={searchRef} value={search.query} placeholder={search.label} aria-label={search.label}
              {...picker.ownerProps} onKeyDown={picker.ownerProps.onKeyDown} onChange={(event) => search.onQuery(event.target.value)} />
          </label>
        ) : null}
        {heading ? <div className={styles.sectionMenuHeading} role="presentation">{heading}</div> : null}
        {rows.map((row) => (
          <Fragment key={row.key}>
            {row.separatorBefore ? <div className={styles.sectionMenuSeparator} role="separator" /> : null}
            {row.submenu ? <SidebarSubmenuRow row={row} picker={picker} main={menu} /> : (
              <NativeMenuItem state={picker.state} itemKey={row.key} className={styles.sectionMenuItem} {...row.data}
                role={row.checked === undefined ? undefined : "menuitemcheckbox"} aria-checked={row.checked}
                title={row.disabled ? undefined : row.title}
                onMouseEnter={row.disabled && row.title ? (event) => reason.enter(event.currentTarget, row.title!) : undefined}
                onMouseLeave={row.disabled && row.title ? reason.leave : undefined}
                data-described={row.description ? "true" : undefined} data-danger={row.danger ? "true" : undefined}>
                {({ labelProps, descriptionProps }) => <>
                  {row.icon}
                  {row.description ? <span className={styles.sectionMenuLabelStack}>
                    <span {...labelProps} className={styles.sectionMenuLabel}>{row.label}</span>
                    <span {...descriptionProps} className={styles.sectionMenuDescription}>{row.description}</span>
                  </span> : <span {...labelProps} className={styles.sectionMenuLabel}>{row.label}</span>}
                  {row.note ? <span className={styles.sectionMenuNote}>{row.note}</span> : null}
                  {row.value ? <span className={styles.sectionMenuValue}>{row.value}</span> : null}
                  {row.asSwitch ? <SwitchMark on={row.checked} /> : row.checked ? <CheckIcon size="small" aria-hidden="true" /> : null}
                  {row.next ? <ChevronRight size="small" className={styles.sectionMenuNext} aria-hidden="true" /> : null}
                </>}
              </NativeMenuItem>
            )}
          </Fragment>
        ))}
      </div>
      {reason.tip}
    </>
  );
}

/** A row and the choices it opens beside the menu. The row owns its Aria
 * submenu trigger, so a menu's rows may vary without reordering hooks. */
export function SidebarSubmenuRow<T extends SidebarMenuRow>({ row, picker, main }: {
  row: T; picker: Pick<ReturnType<typeof useNativeMenu<T>>, "state" | "rootTrigger">; main: RefObject<HTMLDivElement | null>;
}) {
  const trigger = useRef<HTMLButtonElement>(null), side = useRef<HTMLDivElement>(null);
  // The menu itself takes the focus a list beside the row held, as the list closes (menuTakesFocus).
  const focusBack = () => menuTakesFocus(main.current);
  const submenu = useNativeSubmenu({ triggerKey: row.key, rootTrigger: picker.rootTrigger, parentMenuRef: main,
    submenuRef: side, triggerRef: trigger, focusBack, besideList: row.submenu?.custom ? customPicker : undefined });
  const spec = row.submenu!;
  // Before the picker goes: its layer, closing with the focus in it, gave the focus back to the menu's search.
  const back = () => { focusBack(); submenu.state.close(); };
  return (
    <>
      <NativeMenuItem state={picker.state} itemKey={row.key} buttonRef={trigger} className={styles.sectionMenuItem} {...row.data}
        itemOptions={{ ...submenu.submenuTriggerProps, onPress: (event) => {
          submenu.submenuTriggerProps.onPress?.(event);
          // A list with its own search, pressed open, takes the focus on
          // itself with nothing lit (SidebarChoiceMenu); others light their first.
          if (event.pointerType !== "mouse" && event.pointerType !== "touch") return;
          submenu.state.open(spec.search || spec.custom ? null : "first");
          // The row pressed is the lit one, whichever the keys had lit: Escape closing its list leaves it lit, as
          // Notion's ("단축키 추가", ↓ onto its second row, "새 페이지 ›" pressed, Escape: "새 페이지" grey; seen 2026-11-18).
          picker.state.selectionManager.setFocused(true); picker.state.selectionManager.setFocusedKey(row.key);
        } }}>
        {({ labelProps }) => <>
          {row.icon}
          <span {...labelProps} className={styles.sectionMenuLabel}>{row.label}</span>
          {row.value ? <span className={styles.sectionMenuValue}>{row.value}</span> : null}
          <ChevronRight size="small" aria-hidden="true" />
        </>}
      </NativeMenuItem>
      {submenu.state.isOpen ? createPortal(spec.custom
        ? spec.custom({ main, trigger, back, byKeys: submenu.state.focusStrategy !== null })
        : <SidebarChoiceMenu spec={spec} main={main} trigger={trigger} menuRef={side}
          menuOptions={submenu.submenuProps} focusOnOpen={submenu.state.focusStrategy !== null} />, document.body) : null}
    </>
  );
}

/** A custom picker beside a sidebar menu's row (SidebarPagePicker, SidebarViewPicker), drawn on its own. */
function customPicker() {
  return document.querySelector("[data-sidebar-page-picker], [data-sidebar-view-picker]");
}

/** Where a beside part sits: level with its row, beside the menu; a list of
 * choices `aroundRow`, centred on its row as a menu's choices are (besideRowOffset). */
export function useSidebarBesidePlacement(element: RefObject<HTMLElement | null>, { main, trigger }: Pick<SidebarBesidePlace, "main" | "trigger">, gap = 4, aroundRow = false) {
  return usePopoverPlacement(element, (size, viewport) => {
    const box = popoverRectOf(main.current)!, row = trigger.current?.getBoundingClientRect();
    if (aroundRow && row) return placeBeside({ ...box, top: row.top, bottom: row.bottom }, size, viewport, { gap, offsetTop: besideRowOffset(row, size, viewport) });
    const top = (row?.top ?? box.top) - 4;
    return placeBeside({ ...box, top, bottom: top }, size, viewport, { gap });
  }, [], undefined, { kind: "submenu", restoreFocus: false, closesHost: false });
}

function SidebarChoiceMenu({ spec, main, trigger, menuRef, menuOptions, focusOnOpen }: {
  spec: SidebarMenuSubmenu; main: RefObject<HTMLDivElement | null>; trigger: RefObject<HTMLButtonElement | null>;
  menuRef: RefObject<HTMLDivElement | null>; menuOptions: NonNullable<Parameters<typeof useNativeMenu>[0]["menuOptions"]>;
  focusOnOpen: boolean;
}) {
  const choices = spec.choices ?? [];
  const searchRef = useRef<HTMLInputElement>(null);
  const remembers = spec.search?.remembers;
  // Read once as it opens: opened before, pressed open it takes its search too.
  const [searchFirst] = useState(() => focusOnOpen || Boolean(remembers && sidebarPickerSearchFirst(remembers, false)));
  useSidebarPickerOpened(remembers);
  // A list with a search grows and shrinks as it is typed in: it stays level with its row.
  const place = useSidebarBesidePlacement(menuRef, { main, trigger }, spec.gap, !spec.search);
  const picker = useNativeMenu({ items: choices, open: place !== null, menuRef, label: spec.label, menuOptions,
    // A list with a search starts with no highlight. Opened from the keys it
    // takes the focus in its search, arrows entering the results; opened by
    // the pointer it takes the focus on itself, a letter typed going nowhere
    // until the search is pressed — Notion's sidebar "섹션 추가" › "팀스페이스"
    // and "데이터베이스 보기" (SidebarViewPicker), measured 2026-11-16 in Chrome
    // at 1440 with no script before the press: the row pressed, no row lit,
    // "z" in neither search, the focus on the list (the views' twice, in two
    // tabs; the teamspaces' once). One that `remembers` takes its search
    // pressed open too once it has opened before (sidebarPickerSearchFirst).
    // Holding the focus itself it lights no row (quietOnPointer): Aria's focus
    // entry had lit the first, and a letter typed ringed it.
    focusOnOpen: focusOnOpen || Boolean(spec.search), highlightOnOpen: focusOnOpen && !spec.search, quietOnPointer: Boolean(spec.search),
    focusOwnerRef: spec.search && searchFirst ? searchRef : undefined,
    firstKey: choices.find((choice) => choice.checked)?.key, disabledKeys: choices.filter((choice) => choice.disabled).map((choice) => choice.key),
    keyOf: (choice) => choice.key, textOf: (choice) => choice.label, onAction: (choice) => choice.run() });
  return (
    <div {...picker.menuProps} ref={menuRef} className={styles.sectionMenu} {...spec.data}
      style={place ? { ...popoverPlacementStyle(place), ...menuWidth(spec.width ?? 220, Boolean(spec.search)) } : { visibility: "hidden" }}>
      {spec.search ? (
        <label className={styles.viewChoiceSearch}>
          {spec.search.bare ? null : <Search size="small" aria-hidden="true" />}
          <input ref={searchRef} value={spec.search.query} placeholder={spec.search.label} aria-label={spec.search.label}
            {...picker.ownerProps} onKeyDown={picker.ownerProps.onKeyDown} onChange={(event) => spec.search!.onQuery(event.target.value)} />
        </label>
      ) : null}
      {choices.map((choice) => (
        <NativeMenuItem key={choice.key} state={picker.state} itemKey={choice.key} className={styles.sectionMenuItem} {...choice.data}
          role={spec.radio ? "menuitemradio" : "menuitem"} aria-checked={spec.radio ? Boolean(choice.checked) : undefined}>
          {({ labelProps }) => <>
            {choice.icon}
            <span {...labelProps} className={styles.sectionMenuLabel}>{choice.label}</span>
            {choice.checked ? <CheckIcon size="small" aria-hidden="true" /> : null}
          </>}
        </NativeMenuItem>
      ))}
    </div>
  );
}

