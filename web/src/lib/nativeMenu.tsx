import { type HTMLAttributes, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent, type ReactNode, type RefObject,
  useCallback, useId, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useMenu, useMenuItem, useSubmenuTrigger, type AriaMenuItemProps, type AriaMenuOptions } from "react-aria";
import { Item, type TreeState, useTreeState, useMenuTriggerState, useSubmenuTriggerState } from "react-stately";
import { isTextField, nativeMenuKeyboardDelegate } from "./menuKeyboard";
import { pointerStill, watchPointerMoves } from "./pointerHover";
import { useOpenedFromKeyboard } from "./openedFromKeyboard";
import { keysLeadNow, keysLeadRows, lastLeadingKeyAt, lastPointerMoveAt, pointerKeptLead, useMenuInputLead } from "./menuInputLead";
import { ariaScrollsNothing } from "./menuKeyScroll";
import { arrivalHoldsLight, SUBMENU_SETTLE_MS, useSubmenuHeadedFor, useSubmenuHoverHold, useSubmenuOtherRowTakes, useSubmenuSettledClose } from "./submenuHoverHold";
import { rowNameProps } from "./pickerRow";
import { besideLightHeld, useBesideListLeaving } from "./besideListLeaving";
import { useMenuVirtualFocus } from "./nativeMenuVirtualFocus";

// Menus read how they were opened as they open (openedFromKeyboard.ts).
export { useOpenedFromKeyboard };

// Aria's triggers open on the press and its items choose on any release over
// them, a native press-drag-release. A menu lying over its button (a settings
// row's long list moved up to fit, as Notion's language list) then took the
// opening click's own release for a choice; Notion's lists choose nothing
// there (seen 2026-10-30). A release where its press began, outside the item,
// is no choice; a drag onto an item still is. A key pressed after the release
// ends its moment: the click an Enter gives the lit item is a choice, however
// soon it comes (2026-11-16: on a busy home "새로" pressed and Enter at once
// made no page, the release's moment not yet over, and the title typed next
// went to the menu).
let lastPress: { x: number; y: number; target: EventTarget | null } | null = null;
let releasing: { x: number; y: number } | null = null;
if (typeof document !== "undefined") {
  document.addEventListener("pointerdown", event => { lastPress = { x: event.clientX, y: event.clientY, target: event.target }; }, true);
  document.addEventListener("pointerup", event => {
    releasing = { x: event.clientX, y: event.clientY };
    setTimeout(() => { releasing = null; });
  }, true);
  document.addEventListener("keydown", () => { releasing = null; }, true);
}

/** Every menu item's click capture: drops the opening press's release. */
export function stopOpeningPressRelease(event: ReactMouseEvent<HTMLElement>) {
  if (!releasing || !lastPress) return;
  if (lastPress.target instanceof Node && event.currentTarget.contains(lastPress.target)) return;
  if (Math.hypot(releasing.x - lastPress.x, releasing.y - lastPress.y) >= 4) return;
  event.preventDefault(); event.stopPropagation();
}

/**
 * Aria keeps a menu's focus on its owner as an item is pressed: it stops the
 * mousedown's default and, should the focus move all the same, puts it back
 * at once (preventFocusOnPress, and the menu's own focus handler). Chrome
 * starts a drag only from a mousedown whose default goes on and whose focus
 * stays where it put it, so an item in a row the person can drag (a
 * calendar's "날짜 없음" list, whose rows Notion's drags onto a day, seen
 * 2026-11-15) leaves the mousedown to the browser, its focus unheard by the
 * menu, and gives the focus back to the owner just after it.
 */
function keepingDragStart(ariaMouseDown: ((event: ReactMouseEvent<HTMLButtonElement>) => void) | undefined) {
  return (event: ReactMouseEvent<HTMLButtonElement>) => {
    if (event.button !== 0 || !event.currentTarget.closest("[draggable='true']")) { ariaMouseDown?.(event); return; }
    event.stopPropagation();
    const owner = document.activeElement, item = event.currentTarget;
    const unheard = (focus: FocusEvent) => { if (focus.target === item) focus.stopPropagation(); };
    item.addEventListener("focusin", unheard);
    window.setTimeout(() => {
      item.removeEventListener("focusin", unheard);
      // The item, or nothing once its press took the row away (dated, it leaves the list).
      const active = document.activeElement, lost = !active || active === document.body || item.contains(active);
      if (owner instanceof HTMLElement && owner.isConnected && lost) owner.focus({ preventScroll: true });
    });
  };
}

/** Native action/checked state stays with its owner. Aria supplies the real
 * menu collection, key/typeahead focus and each item's press/hover semantics.
 * As in Notion, DOM focus stays on the menu (or its search field) and only
 * the highlight moves: Aria's virtual focus with `aria-activedescendant`. */
export function useNativeMenu<T>({ items, open, menuRef, label, id, keyOf, textOf, onAction, disabledKeys = [], firstKey,
  onClose, focusOnOpen = true, highlightOnOpen: highlightOnOpenOption, quietOnPointer = false, menuOptions, focusOwnerRef, ownerKeepsTab, ownerSearches = true, nameStop, searchLights, tabRings }: {
  items: T[]; open: boolean; menuRef: RefObject<HTMLElement | null>; label: string;
  id?: string;
  keyOf(item: T): string; textOf(item: T): string; onAction(item: T): void;
  disabledKeys?: string[]; firstKey?: string;
  onClose?: () => void; focusOnOpen?: boolean;
  /**
   * Whether opening highlights an item; a search may take focus first.
   * Unset, its current choice (firstKey) or, opened from the keyboard, its first row.
   */
  highlightOnOpen?: boolean;
  /** Opened from the pointer, it lights no row until moved in (a view's tab or
   *  list menus, Notion's seen 2026-11-05); others light their first (a
   *  group's "⋯", a card's menu, seen 2026-11-06). */
  quietOnPointer?: boolean;
  menuOptions?: Pick<AriaMenuOptions<unknown>, "id" | "aria-labelledby" | "onKeyDown" | "onKeyUp" | "autoFocus" | "onClose">;
  /** A search field that keeps DOM focus while its results are highlighted. */
  focusOwnerRef?: RefObject<HTMLElement | null>;
  /** The focus owner is a field of its own (a name), whose Tab moves on. */
  ownerKeepsTab?: boolean;
  /** The owner is a search, whose words light what they find first; a name field is not. */
  ownerSearches?: boolean;
  /** The menu's name is a stop of its own at its head (useMenuVirtualFocus). */
  nameStop?: boolean;
  /** The rows a search lights as it finds them, when not every row (useMenuVirtualFocus). */
  searchLights?(key: string): boolean;
  /** Tab rings its lit row, or its first (useMenuVirtualFocus). */
  tabRings?: boolean;
}) {
  // How it was opened is read as it opens: a later key (the first ↓) would
  // otherwise light the first row under it and move on to the second, where
  // Notion's ↓ lights the first (seen 2026-11-05).
  const keyboardNow = useOpenedFromKeyboard(), openedBy = useRef<boolean | null>(null);
  if (!open) openedBy.current = null; else if (openedBy.current === null) openedBy.current = keyboardNow;
  const fromKeyboard = openedBy.current ?? keyboardNow;
  const highlightOnOpen = highlightOnOpenOption ?? (focusOnOpen && (firstKey !== undefined || fromKeyboard));
  const rootTrigger = useMenuTriggerState({ isOpen: open, onOpenChange: next => { if (!next) onClose?.(); } });
  useLayoutEffect(() => {
    // Native owners may close a controlled root directly. Retire its
    // engine submenu stack without emitting a second native close action.
    if (!open && rootTrigger.expandedKeysStack.length) rootTrigger.closeSubmenu(rootTrigger.expandedKeysStack[0], 0);
  }, [open, rootTrigger.expandedKeysStack, rootTrigger.closeSubmenu]);
  // Stately caches dynamic collection values by object identity. Native
  // enum/string/number menus therefore need object entries, not primitives.
  const entries = items.map(item => ({ item, id: keyOf(item), text: textOf(item) }));
  const state = useTreeState({ items: entries, selectionMode: "none", disabledKeys,
    children: entry => <Item key={entry.id} textValue={entry.text}>{entry.text}</Item> });
  const delegate = useMemo(() => nativeMenuKeyboardDelegate(state.collection, state.disabledKeys, menuRef),
    [state.collection, state.disabledKeys, menuRef]);
  const { menuProps } = useMenu({ ...ariaScrollsNothing, id, "aria-label": label, keyboardDelegate: delegate, ...menuOptions,
    autoFocus: open && focusOnOpen && firstKey === undefined ? menuOptions?.autoFocus ?? (highlightOnOpen ? "first" : true) : false,
    shouldFocusWrap: true, shouldUseVirtualFocus: true, escapeKeyBehavior: "none", onAction: key => {
      const entry = state.collection.getItem(key)?.value; if (entry) onAction(entry.item);
    } }, state, menuRef);
  const wasOpen = useRef(false);
  const virtual = useMenuVirtualFocus({ state, menuRef, focusOwnerRef, ownerKeepsTab, ownerSearches, nameStop, searchLights, tabRings, menuProps, enter: open && focusOnOpen,
    entryKey: () => firstKey, quiet: quietOnPointer && !highlightOnOpen });
  useLayoutEffect(() => {
    const key = state.selectionManager.focusedKey;
    if (open && highlightOnOpen && (!wasOpen.current || !state.collection.getItem(key!) || state.disabledKeys.has(key!))) {
      state.selectionManager.setFocused(true);
      state.selectionManager.setFocusedKey(firstKey && state.collection.getItem(firstKey) && !state.disabledKeys.has(firstKey)
        ? firstKey : delegate.getFirstKey());
    }
    if (!open && wasOpen.current) {
      // A closed menu keeps no highlight; reopening announces none before
      // its owner holds focus again.
      state.selectionManager.setFocused(false); state.selectionManager.setFocusedKey(null);
    }
    wasOpen.current = open;
  }, [open, highlightOnOpen, firstKey, state.collection, state.disabledKeys, delegate]);
  return { state, rootTrigger, ...virtual };
}

/**
 * A row's list beside its menu, Aria's submenu, by the one rule of every list
 * beside a row (submenuHoverHold.ts): the pointer resting on the row
 * SUBMENU_SETTLE_MS opens it, and resting that long on another row lets it go
 * (useSubmenuSettledClose); a press or the keys open and close it at once.
 */
export function useNativeSubmenu({ triggerKey, rootTrigger, parentMenuRef, submenuRef, triggerRef, focusBack, besideList }: {
  triggerKey: string; rootTrigger: ReturnType<typeof useMenuTriggerState>;
  parentMenuRef: RefObject<HTMLElement | null>; submenuRef: RefObject<HTMLElement | null>;
  triggerRef: RefObject<HTMLButtonElement | null>;
  /** What takes the focus the submenu held as the pointer closes it from another row (useSubmenuOtherRowTakes). */
  focusBack?: () => void;
  /** The element of a picker drawn beside the row in place of a submenu of its own (`submenuRef` unset), for the pointer on its way to it (useSubmenuHeadedFor). */
  besideList?: () => Element | null;
}) {
  const list = () => submenuRef.current ?? besideList?.() ?? null;
  const state = useSubmenuSettledClose(useSubmenuTriggerState({ triggerKey }, rootTrigger), { parentMenuRef, triggerRef, list });
  const aria = useSubmenuTrigger({ parentMenuRef, submenuRef, delay: SUBMENU_SETTLE_MS }, state, triggerRef);
  // A menu that opens under a still pointer (⌘/ with the pointer resting
  // where its "목록 형식" comes) opens no submenu there until the pointer
  // moves: the keys typed go on to its search (Notion's, seen 2026-11-07 —
  // Leafen's had opened the submenu, which took the keys).
  const [openedAt] = useState(() => performance.now());
  // Closed with the pointer on its row, it waits for the pointer to leave the row (submenuHoverHold.ts).
  const held = useSubmenuHoverHold(state.isOpen, triggerRef);
  useSubmenuOtherRowTakes(state.isOpen, { parentMenuRef, submenuRef, triggerRef, focusBack });
  useSubmenuHeadedFor(state.isOpen, { parentMenuRef, triggerRef, list });
  // Closed with its menu staying, it fades out (besideListLeaving.ts).
  useBesideListLeaving(state.isOpen, list, () => triggerRef.current);
  const hover = aria.submenuTriggerProps.onHoverChange;
  const { stay } = state;
  const onHoverChange = useCallback((hovered: boolean) => {
    // Back on its row before the list went, the list stays.
    if (hovered) stay();
    if (hovered && held.current) return;
    if (!hovered || lastPointerMoveAt() > openedAt) {
      hover?.(hovered);
      return;
    }
    const wake = (event: PointerEvent) => {
      if (!event.movementX && !event.movementY) return;
      document.removeEventListener("pointermove", wake, true);
      if (triggerRef.current?.matches(":hover")) hover?.(true);
    };
    document.addEventListener("pointermove", wake, true);
  }, [held, hover, openedAt, stay, triggerRef]);
  return { state, ...aria, submenuTriggerProps: { ...aria.submenuTriggerProps, onHoverChange } };
}

// The keyboard ring follows the input that leads (menuInputLead.ts), as
// Notion's menus' (seen 2026-11-14 at 1440): one holding the focus itself
// rings its lit row however it was lit, as its :focus-visible (a column's
// "유형 변경": ↓↓ ringed "선택", the pointer's first move into "상태" only
// shaded it, moving on into "날짜" took the highlight with the ring, ↓ rang
// "사람"); under a search field only a row the keys lit (the block menu's:
// the pointer's first move left the ring on "댓글", moving on into "블록
// 링크 복사" took the highlight without it). Escape and typed keys leave the
// rows as the lead before them: one that closes a submenu the pointer opened
// and used gives its row back the pointer's shade, one the keys opened the
// ring (seen 2026-11-16, a page's "⋯" "테마와 레이아웃 ›": pressed open and a
// switch in it pressed twice, Escape left the row grey, ↓ then ringed the
// next; reached by ↓ and opened by →, Escape left it ringed; once, a block
// menu's "전환 ›" opened by the pointer resting on it: Escape left it grey).
watchPointerMoves();

export function NativeMenuItem<T>({ state, itemKey, buttonRef, itemOptions, children, ...attributes }: {
  state: TreeState<T>; itemKey: string;
  buttonRef?: RefObject<HTMLButtonElement | null>;
  itemOptions?: Omit<AriaMenuItemProps, "key">;
  children(props: { focused: boolean; labelProps: HTMLAttributes<HTMLElement>; descriptionProps: HTMLAttributes<HTMLElement> }): ReactNode;
} & Omit<HTMLAttributes<HTMLButtonElement>, "children"> & { disabled?: boolean }) {
  const localRef = useRef<HTMLButtonElement>(null), ref = buttonRef ?? localRef;
  // Drawn again as a typed key gives the keys the lead in a list holding them itself, its ring with them (menuInputLead.ts).
  useMenuInputLead();
  // The owner's aria-activedescendant names the highlighted item by DOM id.
  const generatedId = useId();
  const hoverStartAt = useRef<number | null>(null);
  const { menuItemProps, labelProps, descriptionProps, isFocused, isFocusVisible } = useMenuItem({ key: itemKey, shouldCloseOnSelect: false,
    id: generatedId, ...itemOptions,
    onHoverStart: (event) => {
      hoverStartAt.current = performance.now();
      // Back onto its row while its list stays (useSubmenuSettledClose), the row takes the light from the one
      // passed: Aria lights no row whose list is open.
      const manager = state.selectionManager;
      if (String(itemOptions?.["aria-expanded"]) === "true" && manager.focusedKey !== null && manager.focusedKey !== itemKey
        && !keysLeadNow()) manager.setFocusedKey(itemKey);
      itemOptions?.onHoverStart?.(event);
    },
    onHoverEnd: (event) => { hoverStartAt.current = null; itemOptions?.onHoverEnd?.(event); } }, state, ref);
  // A menu that opens, or redraws, under a still pointer leaves the row there
  // unlit, only shaded (its :hover): the pointer lights a row only as it
  // comes into one, as Notion's (seen 2026-11-14 at 1440: a row page's
  // "속성 편집 › 유형" pressed open shaded "선택" under the pointer, moved
  // within it lit nothing, ↓ lit the first row; its property menu shown
  // again by Escape the same).
  const ariaEnter = menuItemProps.onPointerEnter;
  const onMouseDown = keepingDragStart(menuItemProps.onMouseDown);
  const onPointerEnter = (event: ReactPointerEvent<HTMLButtonElement>) => {
    const manager = state.selectionManager, focused = manager.isFocused, key = manager.focusedKey;
    // Nor the row the pointer first comes onto in a list beside a row that came up lit elsewhere (arrivalHoldsLight).
    const still = pointerStill(event) || arrivalHoldsLight(ref.current);
    ariaEnter?.(event);
    if (!still || manager.focusedKey === key && manager.isFocused === focused) return;
    manager.setFocused(focused); manager.setFocusedKey(key);
    hoverStartAt.current = null;
  };
  // Whether the keys lit it rather than the pointer moving onto it, read as it is lit.
  const lit = useRef<{ byKeys: boolean } | null>(null);
  if (!isFocused) lit.current = null;
  else if (!lit.current) lit.current = { byKeys: keysLeadRows() && (hoverStartAt.current === null || lastLeadingKeyAt() > hoverStartAt.current) };
  // An attribute the caller leaves undefined (a role only some rows set)
  // keeps Aria's own.
  const own = Object.fromEntries(Object.entries(attributes).filter(([, value]) => value !== undefined));
  // The row a search highlighted as it was typed shows no keyboard ring.
  const searchLit = ref.current?.closest<HTMLElement>("[data-search-lit]")?.dataset.searchLit === itemKey;
  const menu = ref.current?.closest<HTMLElement>("[data-native-menu]");
  const menuHoldsFocus = typeof document !== "undefined" && menu === document.activeElement;
  // A menu's name holding the focus rings none of its rows (useMenuVirtualFocus's
  // nameStop), nor a menu whose owner says Notion's rings none there
  // (`data-ringless`): one that keeps the focus on the page (a row page
  // property's "유형", a paste's menu), or one whose rows the keys light grey
  // alone, its search holding the focus or the menu itself (the sidebar's
  // "단축키 추가" and "섹션 추가"; seen 2026-11-18 at 1440 in three tabs, while
  // the page's "⋯" ringed the row ↓ lit under its search, and the sidebar's
  // other menus theirs).
  const nameHolds = typeof document !== "undefined" && (menu?.dataset.ringless === "true"
    || menu?.dataset.nameStop === "true" && isTextField(document.activeElement));
  const expanded = menuItemProps["aria-expanded"] === true || menuItemProps["aria-expanded"] === "true";
  const ringed = !pointerKeptLead() && (isFocusVisible || keysLeadRows() && isFocused && state.selectionManager.isFocused && !expanded
    && (menuHoldsFocus || Boolean(lit.current?.byKeys)));
  // Named as every row is (`rowNameProps`): by its label part where it draws one.
  const name = rowNameProps({ label: menuItemProps["aria-labelledby"], description: menuItemProps["aria-describedby"] }, own["aria-label"]);
  return <button {...menuItemProps} onPointerEnter={onPointerEnter} onMouseDown={onMouseDown} {...own} {...name} ref={ref} type="button" onClickCapture={stopOpeningPressRelease}
    data-focused={isFocused || !state.selectionManager.isFocused && besideLightHeld(ref.current) ? "true" : undefined} data-focus-visible={ringed && !searchLit && !nameHolds ? "true" : undefined}
    data-lit-by={isFocused ? (lit.current?.byKeys ? "keys" : "pointer") : undefined}>
    {children({ focused: isFocused, labelProps, descriptionProps })}</button>;
}
