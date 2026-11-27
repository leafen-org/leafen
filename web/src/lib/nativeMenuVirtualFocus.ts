import { type FocusEvent as ReactFocusEvent, type HTMLAttributes, type KeyboardEvent as ReactKeyboardEvent, type RefObject,
  useLayoutEffect, useReducer, useRef, useState } from "react";
import type { TreeState } from "react-stately";
import { isInertMenuKey, isMenuMoveKey, isTextField, isTypedLetter, nativeMenuKeyboardDelegate } from "./menuKeyboard";
import { goRoundMenuStops } from "./menuTabStops";
import { keyboardPropsEvent } from "./overlayLayer";
import { fieldFocusOnOpen, raisesKeyboard } from "./screenKind";
import { inlineEndKey, inlineStartKey } from "./readingDirection";
import { keysLeadRows } from "./menuInputLead";
import { useKeyWalkScroll } from "./menuKeyScroll";
import { fieldEnter, imeComposing } from "./fieldKeyBoundary";

// A menu's keys with the DOM focus on its owner — the menu itself or its
// search field — and only the highlight moving (useNativeMenu in
// nativeMenu.tsx, and the pickers that keep a field of their own).

// Keys the highlighted item owns: activation and submenu direction. Space
// chooses nothing, as in Notion's menus (seen 2026-11-14: Space on the lit
// "데이터베이스" of the sidebar's "새로" and on "탭 삭제" of its home tab's menu
// left them open and lit), so letters and spaces typed right after "새로"
// make no page.
const ITEM_KEYS = new Set(["Enter", "ArrowRight", "ArrowLeft"]);

/** Notion keeps DOM focus on an open menu or its search field and moves only
 * the highlight. Aria's virtual focus names that item with
 * `aria-activedescendant`; the owner's activation and submenu keys reach the
 * highlighted item as Aria's Autocomplete forwards them, so item press,
 * close and submenu policy stay Aria's. */
export function useMenuVirtualFocus<T>({ state, menuRef, focusOwnerRef, ownerKeepsTab = false, ownerSearches = true, nameStop = false, searchLights, menuProps, enter, entryKey, quiet = false, tabRings = false }: {
  state: TreeState<T>; menuRef: RefObject<HTMLElement | null>; focusOwnerRef?: RefObject<HTMLElement | null>; ownerSearches?: boolean;
  ownerKeepsTab?: boolean;
  /**
   * Tab, which moves nothing in a menu without a field, hands it to the keys:
   * its lit row, or its first when none is, takes the keyboard ring there and
   * then, as Notion's selection toolbar turn list (seen 2026-11-26 in Chrome
   * 1440, two tabs, no script before the key: pressed open on a text line,
   * Tab ringed its first row "텍스트").
   */
  tabRings?: boolean;
  /**
   * The name heading the menu is a stop of its own, lighting no row, as
   * Notion's column menu (seen 2026-11-14 at 1440, its name focused or the
   * menu itself): ↓ from none stops on the name, the next ↓ lights the first
   * row; ↑ from the first row goes back to the name and on to the last; ↓
   * from the last to the name. A row lit while the name holds the focus
   * shows no ring (NativeMenuItem).
   */
  nameStop?: boolean;
  /**
   * The rows a search may light as it finds them, the others passed over: the
   * views list's search lights a view it finds, never "새 보기" or "새 데이터
   * 소스" under it. Finding none to light, it lights nothing and the field is
   * a stop of its own in the arrows' round: ↓ lights the first row and ↑ the
   * last, ↑ from the first and ↓ from the last go back to the field, lighting
   * none (Notion's views list, seen 2026-11-15 at 1440 under "zzqq" — the
   * first ↓ ringed "새 보기", the next "새 데이터 소스", the next none; ↓
   * then ↑ none, ↑ again "새 데이터 소스"). Unset, a search lights its first row.
   */
  searchLights?(key: string): boolean;
  menuProps: HTMLAttributes<HTMLElement>;
  /** The menu is open for keyboard use; its owner takes DOM focus once. */
  enter: boolean;
  /** The highlight a menu entered without one starts at (its checked item). */
  entryKey?: () => string | undefined;
  /** Entered without a highlight, it starts with none (see quietOnPointer). */
  quiet?: boolean;
}) {
  const owner = () => focusOwnerRef?.current ?? menuRef.current;
  const entered = useRef(false);
  // A layout effect: DOM focus reaches the owner before Aria's passive item
  // effect announces a highlight, so a previous owner (an editor) is never
  // sent a virtual blur.
  useLayoutEffect(() => {
    if (!enter) { entered.current = false; return; }
    if (entered.current) return;
    entered.current = true;
    // On a touch screen a search waits for a tap, as Notion's iPad pickers
    // open with the keyboard down (seen 2026-11-14 at 834): the menu itself
    // takes the focus (fieldFocusOnOpen), its field rising the keyboard once
    // pressed. A pointer's menu opens with its search focused.
    const field = focusOwnerRef?.current;
    const target = field && !fieldFocusOnOpen() && raisesKeyboard(field) ? menuRef.current : owner();
    if (target && target !== field && !target.hasAttribute("tabindex")) target.tabIndex = -1;
    if (target && !target.contains(document.activeElement)) target.focus({ preventScroll: true });
  });
  // What a search finds first is highlighted as it is typed, so Enter takes
  // it, as Notion's menus do (seen 2026-11-06), as NativeActionMenu's.
  const searched = useRef("");
  // Whether the field is a stop in the arrows' round (searchLights).
  const fieldStop = useRef(false);
  const atName = useRef(false);
  useLayoutEffect(() => {
    const field = focusOwnerRef?.current;
    if (!ownerSearches || !(field instanceof HTMLInputElement)) return;
    const value = field.value.trim();
    if (value === searched.current) return;
    // A search wiped out lights its first row again, the list back at its
    // top, where a search may light only some (Notion's views list, seen
    // 2026-11-15 at 1440 in two tabs: "(3" then two backspaces lit "Default
    // view").
    const cleared = Boolean(searchLights) && !value && Boolean(searched.current);
    searched.current = value;
    const delegate = nativeMenuKeyboardDelegate(state.collection, state.disabledKeys, menuRef);
    let first = value || cleared ? delegate.getFirstKey() : null;
    while (first !== null && searchLights && !searchLights(String(first))) first = delegate.getKeyBelow(first);
    if (searchLights) fieldStop.current = atName.current = Boolean(value) && first === null;
    // That highlight is shaded with no keyboard ring while the pointer leads,
    // ringed once the keys do (menuInputLead.ts), as Notion's (seen
    // 2026-11-07; 2026-11-16 at 1440, a block's "⋮⋮" pressed open and "link"
    // typed shaded "블록 링크 복사", ↓ first ringed it; the views list, seen
    // 2026-11-15 in two tabs: ↓ then "(3" ringed "표 (3)", two backspaces
    // then "Default view"; without the ↓ both only shaded).
    if (menuRef.current) {
      if (first === null || keysLeadRows()) delete menuRef.current.dataset.searchLit;
      else menuRef.current.dataset.searchLit = String(first);
      if (cleared) menuRef.current.scrollTop = 0;
    }
    state.selectionManager.setFocused(first !== null);
    state.selectionManager.setFocusedKey(first);
  });
  const focusedKey = state.selectionManager.focusedKey;
  // Its rows scroll as Notion's menus scroll theirs: the menu gives Aria `ariaScrollsNothing`.
  useKeyWalkScroll(menuRef, focusedKey);
  const [activeId, setActiveId] = useState<string>();
  // Read after every render (the row may be drawn later than its key), it is
  // set only when it changes.
  useLayoutEffect(() => {
    const item = focusedKey === null ? null
      : menuRef.current?.querySelector<HTMLElement>(`[data-key="${CSS.escape(String(focusedKey))}"]`);
    const id = item?.id || undefined;
    if (id !== activeId) setActiveId(id);
  });
  // A key that moves the highlight makes the menu's rings the keyboard's again.
  const moved = () => { if (menuRef.current) delete menuRef.current.dataset.searchLit; };
  // Its rows read the keys' lead as they are drawn (NativeMenuItem): drawn again, the lit one rings (tabRings).
  const [, redraw] = useReducer((count: number) => count + 1, 0);
  const ringLitRow = () => {
    const manager = state.selectionManager;
    const key = manager.focusedKey !== null && state.collection.getItem(manager.focusedKey) ? manager.focusedKey
      : nativeMenuKeyboardDelegate(state.collection, state.disabledKeys, menuRef).getFirstKey();
    if (key !== null) { manager.setFocused(true); manager.setFocusedKey(key); }
    moved(); redraw();
  };
  // The menu holding the focus itself, off the field that owns it otherwise (menuTakesFocus), takes its keys as one without a field.
  const itself = (event: ReactKeyboardEvent<HTMLElement>) => !focusOwnerRef || event.target === menuRef.current;
  const forward = (event: ReactKeyboardEvent<HTMLElement>) => event.target === menuRef.current
    ? forwardMenuItemKey(event, menuRef.current, false) : forwardMenuItemKey(event, owner(), Boolean(focusOwnerRef));
  // A text field in the menu keeps its own keys (Aria's type-to-select must
  // not take its letters). Its vertical arrows move the highlight instead:
  // into the results at an end, then by the collection's own navigation.
  function moveHighlight(event: ReactKeyboardEvent<HTMLElement>) {
    if (imeComposing(event) || event.altKey || event.ctrlKey || event.metaKey || !isMenuMoveKey(event.key)) return false;
    event.preventDefault(); event.stopPropagation(); moved();
    const manager = state.selectionManager;
    // A name or a search field that lit nothing is a stop of the round, as the name is.
    if ((nameStop || fieldStop.current) && (event.key === "ArrowDown" || event.key === "ArrowUp")) {
      const delegate = nativeMenuKeyboardDelegate(state.collection, state.disabledKeys, menuRef), down = event.key === "ArrowDown";
      const lit = manager.focusedKey !== null && state.collection.getItem(manager.focusedKey) ? manager.focusedKey : null;
      const next = lit === null ? (atName.current || !down ? (down ? delegate.getFirstKey() : delegate.getLastKey()) : null)
        : lit === (down ? delegate.getLastKey() : delegate.getFirstKey()) ? null : down ? delegate.getKeyBelow(lit) : delegate.getKeyAbove(lit);
      atName.current = next === null;
      manager.setFocused(true); manager.setFocusedKey(next);
      return true;
    }
    if (manager.isFocused && manager.focusedKey !== null && state.collection.getItem(manager.focusedKey) && menuRef.current) {
      menuProps.onKeyDown?.(keyboardPropsEvent(event, menuRef.current, true)); return true;
    }
    const delegate = nativeMenuKeyboardDelegate(state.collection, state.disabledKeys, menuRef);
    const key = event.key === "ArrowUp" ? delegate.getLastKey() : delegate.getFirstKey();
    if (key !== null) { manager.setFocused(true); manager.setFocusedKey(key); }
    return true;
  }
  const onKeyDown = (event: ReactKeyboardEvent<HTMLElement>) => {
    if (forward(event)) return;
    if (isTextField(event.target)) {
      // A field that is not the owner (a name being edited) is left for the
      // menu's highlight, as a person moving down from it expects.
      if (event.target !== owner() && moveHighlight(event) && !focusOwnerRef) menuRef.current?.focus({ preventScroll: true });
      return;
    }
    if (itself(event) && isInertMenuKey(event)) {
      event.preventDefault(); event.stopPropagation();
      if (tabRings && event.key === "Tab") ringLitRow();
      return;
    }
    // Typed letters choose no item: they never reach Aria's type-to-select.
    if (itself(event) && isTypedLetter(event)) return;
    if (isMenuMoveKey(event.key)) moved();
    menuProps.onKeyDown?.(event);
  };
  const onKeyUp = (event: ReactKeyboardEvent<HTMLElement>) => { if (!forward(event)) menuProps.onKeyUp?.(event); };
  // Any path that DOM-focuses an item (a closed submenu returning to its
  // trigger, a restored opener) becomes the same highlight on the owner.
  const onFocus = (event: ReactFocusEvent<HTMLElement>) => {
    const target = event.target;
    const key = target !== menuRef.current && target.closest("[data-key]") === target ? target.dataset.key : undefined;
    if (key !== undefined && document.activeElement === target && state.collection.getItem(key)) {
      state.selectionManager.setFocused(true); state.selectionManager.setFocusedKey(key);
      owner()?.focus({ preventScroll: true }); return;
    }
    // A field taking focus (the search, a name) chooses no highlight itself.
    if (isTextField(target)) return;
    const entry = target === menuRef.current && state.selectionManager.focusedKey === null ? entryKey?.() : undefined;
    if (entry !== undefined && state.collection.getItem(entry) && !state.disabledKeys.has(entry)) {
      state.selectionManager.setFocused(true); state.selectionManager.setFocusedKey(entry); return;
    }
    // A menu placed before it takes focus (open only once placed) misses
    // Aria's autofocus, whose focus entry lights the first row; a quiet one
    // lights none.
    if (quiet && target === menuRef.current && !state.selectionManager.isFocused && state.selectionManager.focusedKey === null) {
      state.selectionManager.setFocused(true); return;
    }
    menuProps.onFocus?.(event);
  };
  // The search field that owns focus: item keys reach the highlight, arrows
  // move it, Tab does nothing (Notion's page and block menus), and its other
  // keys stay its own.
  const ownerKeyDown = (event: ReactKeyboardEvent<HTMLElement>) => {
    if (!ownerKeepsTab && goRoundMenuStops(event)) return true;
    if (!ownerKeepsTab && isInertMenuKey(event, true)) { event.preventDefault(); event.stopPropagation(); return true; }
    return forward(event) || moveHighlight(event);
  };
  return { menuProps: { ...menuProps, onKeyDown, onKeyUp, onFocus, "data-native-menu": "true", "data-name-stop": nameStop ? "true" : undefined,
    ...(focusOwnerRef ? {} : { tabIndex: -1 }), "aria-activedescendant": activeId },
    /** Spread on the search field that keeps focus. `onKeyDown` reports
     * whether it took the key, so the field may give Enter its own default. */
    ownerProps: { "aria-activedescendant": activeId, "aria-controls": menuProps.id,
      onKeyDown: ownerKeyDown, onKeyUp: (event: ReactKeyboardEvent<HTMLElement>) => { forward(event); } } };
}

/** The focus owner's activation and submenu keys reach the item its
 * `aria-activedescendant` names, as Aria's Autocomplete forwards them. A
 * search owner keeps its own caret keys; only a highlighted
 * submenu trigger takes the key into its submenu from it. */
export function forwardMenuItemKey(event: ReactKeyboardEvent<HTMLElement>, owner: HTMLElement | null, searchOwner: boolean) {
  if (!owner || event.target !== owner || imeComposing(event)) return false;
  // A menu without a field takes Space and does nothing with it (ITEM_KEYS).
  if (event.key === " " && !searchOwner && !event.altKey && !event.ctrlKey && !event.metaKey) {
    event.preventDefault(); event.stopPropagation(); return true;
  }
  if (!ITEM_KEYS.has(event.key)) return false;
  const id = owner.getAttribute("aria-activedescendant"), target = id ? owner.ownerDocument.getElementById(id) : null;
  if (!target) return false;
  // Into a submenu is toward the reading direction's end: ArrowLeft where it reads from the right.
  const intoSubmenu = inlineEndKey(target);
  if (searchOwner && (event.key === inlineStartKey(target)
    || event.key === intoSubmenu && !target.hasAttribute("aria-haspopup"))) return false;
  event.stopPropagation();
  // A link item's Enter comes from the menu, not from a focused anchor, so the
  // browser would not follow it: it clicks the anchor, as a pointer does.
  if (fieldEnter(event) && target instanceof HTMLAnchorElement && target.href) {
    event.preventDefault();
    if (event.type === "keydown") target.click();
    return true;
  }
  // Aria moves focus into a submenu only from a DOM-focused trigger. A
  // submenu a pointer already opened takes the keys from its highlight.
  const opened = event.type === "keydown" && event.key === intoSubmenu && target.getAttribute("aria-expanded") === "true";
  if (!target.dispatchEvent(new KeyboardEvent(event.type, event.nativeEvent))) event.preventDefault();
  const submenu = opened ? owner.ownerDocument.getElementById(target.getAttribute("aria-controls") ?? "") : null;
  if (submenu) { event.preventDefault(); submenu.focus({ preventScroll: true }); }
  return true;
}
