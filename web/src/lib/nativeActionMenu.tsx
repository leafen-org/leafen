import { Children, createContext, isValidElement, type AriaAttributes, type AriaRole, type KeyboardEventHandler, type PointerEventHandler, type ReactNode,
  type RefObject, useCallback, useContext, useId, useLayoutEffect, useMemo, useRef } from "react";
import { CollectionRendererContext, DefaultCollectionRenderer, Header, Menu, MenuItem, MenuSection, MenuStateContext, SelectableCollectionContext,
  Separator, type CollectionRenderer, type Key, type MenuProps, type MenuItemProps, type MenuSectionProps } from "react-aria-components";
import type { TreeState } from "react-stately";
import { createFocusManager } from "@react-aria/focus";
import { matchesKeyboardShortcut, shortcutCompactLabel } from "./keyboardShortcuts";
import { routeOverlayKeyDown } from "./overlayLayer";
import { isInertMenuKey, isMenuMoveKey, isTextField, nativeMenuKeyboardDelegate } from "./menuKeyboard";
import { stopOpeningPressRelease } from "./nativeMenu";
import { forwardMenuItemKey } from "./nativeMenuVirtualFocus";
import { escapeLeftPressedList, pointerKeptLead, useMenuInputLead } from "./menuInputLead";
import { useFocusWhenLayerJoins } from "./popoverLayer";
import { ariaScrollsNothing, useKeyWalkScroll } from "./menuKeyScroll";
import { pointerStill } from "./pointerHover";
import { imeComposing } from "./fieldKeyBoundary";
import { besideLightHeld } from "./besideListLeaving";
import { arrivalHoldsLight } from "./submenuHoverHold";
import { goRoundMenuStops } from "./menuTabStops";

const EMPTY_REF = { current: null };

type Access = { state: RefObject<TreeState<unknown> | null>; settle?: () => void };
const AccessContext = createContext<Access | null>(null);
const renderer: CollectionRenderer = { ...DefaultCollectionRenderer, CollectionRoot: function NativeCollectionRoot(props) {
  const access = useContext(AccessContext), state = useContext(MenuStateContext);
  // Its rows scroll as Notion's menus scroll theirs: the menu gives Aria `ariaScrollsNothing`.
  useKeyWalkScroll(props.scrollRef ?? EMPTY_REF, state?.selectionManager.focusedKey);
  useLayoutEffect(() => {
    if (!access) return;
    access.state.current = state;
    access.settle?.();
    return () => { if (access.state.current === state) access.state.current = null; };
  }, [access, state]);
  return <DefaultCollectionRenderer.CollectionRoot {...props} />;
} };

// Typed letters choose no item, as in Notion's menus (a search field keeps
// its own letters).
const virtualFocus = { shouldUseVirtualFocus: true, disallowTypeAhead: true };

// Where a menu starts as it opens when its caller says nothing: a list beside
// a row the keys opened starts on its first row (RowMenuBesideSubmenu). It
// reaches only the menu drawn right inside, none drawn within that one.
const OpeningContext = createContext<MenuProps<unknown>["autoFocus"]>(undefined);
export const NativeActionMenuOpening = OpeningContext.Provider;
// Whether the keys lead the look of a list beside a row (RowMenuBesideSubmenu): its lit row is ringed as the
// keys' own would be, wherever the pointer lights it then, until a press. Reaches the menu drawn right inside,
// whose rows read it (`ListKeysLead`), as the opening does.
const KeysLeadContext = createContext(false);
export const NativeActionMenuKeysLead = KeysLeadContext.Provider;
const ListKeysLead = createContext(false);

/** Declarative native actions, including those returned by child components,
 * are registered by Aria's public collection renderer. No DOM item discovery
 * or second selection collection is maintained here. As in Notion, the open
 * menu (or its search field) keeps DOM focus and only the highlight moves:
 * Aria virtual focus, named by `aria-activedescendant` from the focusin Aria
 * announces on the highlighted item, as Aria's Autocomplete tracks it. */
export function NativeActionMenu({ menuRef, onKeyDownCapture, ...props }: Omit<MenuProps<unknown>, "render"> & {
  menuRef?: RefObject<HTMLDivElement | null>; onKeyDownCapture?: KeyboardEventHandler<HTMLDivElement> }) {
  const local = useRef<HTMLDivElement>(null), state = useRef<TreeState<unknown> | null>(null), menu = menuRef ?? local;
  const opening = useContext(OpeningContext), keysLead = useContext(KeysLeadContext);
  const access = useMemo<Access>(() => ({ state }), []);
  const search = () => menu.current?.querySelector<HTMLElement>("[data-native-menu-search]") ?? null;
  const owner = () => search() ?? menu.current;
  // A layout effect: DOM focus reaches the owner before Aria's passive item
  // effect announces a highlight, so the opener never receives a virtual blur.
  // Drawn in a popover layer, once the layer's focus scope has joined, later
  // in the same pass (useFocusWhenLayerJoins).
  const whenLayerJoins = useFocusWhenLayerJoins();
  useLayoutEffect(() => {
    let live = true, frame = 0;
    whenLayerJoins(() => {
      if (!live) return;
      const target = owner();
      if (target && !target.contains(document.activeElement)) target.focus({ preventScroll: true });
      if (search()) return;
      // A search in a section's header is drawn with the collection's items, a
      // frame after the menu; the focus the menu holds moves on to it then.
      let frames = 0;
      frame = window.requestAnimationFrame(function check() {
        const field = search();
        if (field) {
          if (document.activeElement === menu.current) field.focus({ preventScroll: true });
          return;
        }
        if (++frames < 10) frame = window.requestAnimationFrame(check);
      });
    });
    return () => { live = false; window.cancelAnimationFrame(frame); };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  // What a search finds first is highlighted as it is typed, so Enter takes
  // it, as Notion's menus do (seen 2026-11-06: "가져오기" under "더 보기";
  // 2026-11-12: "복" in a page's and an open database page's "…" marks
  // "복제", the first of "복제, 링크 복사, 페이지 내용 복사하기"). Aria makes
  // the rows' collection again a draw after the menu, in the order the search
  // ranks them, so the highlight it placed follows the rows as they land
  // (`settle`, from the collection's root) until the keys or the pointer move it.
  const searched = useRef<{ value: string; key: Key | null }>({ value: "", key: null });
  access.settle = () => {
    const field = search(), engine = state.current;
    if (!engine) return;
    const value = field instanceof HTMLInputElement ? field.value.trim() : "";
    const last = searched.current, manager = engine.selectionManager;
    // Its own highlight, or one on a row the search has since taken away (Aria lets it go then).
    const placed = manager.focusedKey === last.key || (last.key !== null && !engine.collection.getItem(last.key));
    if (value === last.value && (!value || !placed)) return;
    const first = value ? nativeMenuKeyboardDelegate(engine.collection, engine.disabledKeys, menu).getFirstKey() : null;
    searched.current = { value, key: first };
    if (first === manager.focusedKey && manager.isFocused === (first !== null)) return;
    manager.setFocused(first !== null);
    manager.setFocusedKey(first);
  };
  useLayoutEffect(() => access.settle?.());
  return <OpeningContext.Provider value={undefined}><KeysLeadContext.Provider value={false}><ListKeysLead.Provider value={keysLead}>
    <AccessContext.Provider value={access}><CollectionRendererContext.Provider value={renderer}>
    <SelectableCollectionContext.Provider value={virtualFocus}>
    <Menu {...ariaScrollsNothing} {...props} autoFocus={props.autoFocus ?? opening} ref={menu} shouldFocusWrap shouldCloseOnSelect={false} escapeKeyBehavior="none"
      render={domProps => <div {...domProps} tabIndex={-1} data-native-menu="true" onFocus={event => {
        const target = event.target;
        if (target !== event.currentTarget && target.hasAttribute("data-native-menu-item")) {
          const key = target.dataset.key, engine = state.current;
          if (document.activeElement !== target) { owner()?.setAttribute("aria-activedescendant", target.id); return; }
          // An item given DOM focus (a restored or mounted target) becomes
          // the same highlight on the menu's focus owner — but for a row its
          // list beside it gives the focus back to as an Escape closes a list
          // the pointer pressed in: the row keeps it, shaded, and a letter
          // typed then goes nowhere (escapeLeftPressedList). The keys moving
          // on hand it back (onKeyDownCapture).
          if (key !== undefined && engine?.collection.getItem(key)) {
            engine.selectionManager.setFocused(true); engine.selectionManager.setFocusedKey(key);
            if (!escapeLeftPressedList(event.currentTarget)) owner()?.focus({ preventScroll: true });
            return;
          }
        }
        // The search keeps focus without choosing a highlight itself.
        if (target === search()) return;
        // Other native fields keep a text caret; that is not a menu choice.
        if (target !== event.currentTarget && !target.hasAttribute("data-native-menu-item")) {
          state.current?.selectionManager.setFocused(false); return;
        }
        domProps.onFocus?.(event);
      }} onKeyDown={event => {
        if (forwardMenuItemKey(event, owner(), search() !== null)) return;
        if (event.key === "Escape") routeOverlayKeyDown(event);
        if (!event.defaultPrevented && !isTextField(event.target)) domProps.onKeyDown?.(event);
      }} onKeyUp={event => {
        if (!forwardMenuItemKey(event, owner(), search() !== null)) domProps.onKeyUp?.(event);
      }} onKeyDownCapture={event => {
        onKeyDownCapture?.(event);
        // With its menu open ⌘⇧↵ does what the row that shows those keys does, as Notion's.
        if (!event.defaultPrevented && matchesKeyboardShortcut("openInNewTab", event.nativeEvent)) {
          const label = shortcutCompactLabel("openInNewTab");
          const row = [...event.currentTarget.querySelectorAll<HTMLElement>("[data-native-menu-item]")].find((item) => item.textContent?.includes(label));
          if (row) { event.preventDefault(); event.stopPropagation(); row.click(); return; }
        }
        if (event.defaultPrevented || imeComposing(event) || event.altKey || event.ctrlKey || event.metaKey) return;
        const engine = state.current;
        if (!engine) return;
        // A search field keeps its text editing while its arrows move the
        // action highlight; another field in the menu keeps all its keys.
        const fromInput = isTextField(event.target) && event.target instanceof HTMLElement && event.target.hasAttribute("data-native-menu-search");
        if (isTextField(event.target) && !fromInput) return;
        // Tab goes round the menu's own stops: its search, what clears it, the foot's person (menuTabStops.ts).
        if (search() !== null && goRoundMenuStops(event)) return;
        if (isInertMenuKey(event, search() !== null)) { event.preventDefault(); event.stopPropagation(); return; }
        // A row that kept the focus (onFocus) moves the highlight as the search does and hands it the focus.
        const fromRow = event.target instanceof HTMLElement && event.target !== event.currentTarget
          && event.target.hasAttribute("data-native-menu-item") && search() !== null;
        if (!(fromInput || fromRow) || !isMenuMoveKey(event.key)) return;
        const delegate = nativeMenuKeyboardDelegate(engine.collection, engine.disabledKeys, menu);
        const focused = engine.selectionManager.isFocused ? engine.selectionManager.focusedKey : null;
        const key = event.key === "ArrowDown" && focused !== null ? delegate.getKeyBelow(focused) ?? delegate.getFirstKey()
          : event.key === "ArrowUp" && focused !== null ? delegate.getKeyAbove(focused) ?? delegate.getLastKey()
            : event.key === "ArrowUp" ? delegate.getLastKey() : delegate.getFirstKey();
        if (key === null) return;
        event.preventDefault(); event.stopPropagation();
        if (fromRow) search()?.focus({ preventScroll: true });
        engine.selectionManager.setFocused(true); engine.selectionManager.setFocusedKey(key);
      }} />} />
    </SelectableCollectionContext.Provider>
  </CollectionRendererContext.Provider></AccessContext.Provider></ListKeysLead.Provider></KeysLeadContext.Provider></OpeningContext.Provider>;
}

function actionText(children: ReactNode): string {
  return Children.toArray(children).map(child => {
    if (typeof child === "string" || typeof child === "number") return String(child);
    if (!isValidElement<{ children?: ReactNode; "aria-hidden"?: boolean }>(child) || child.props["aria-hidden"]) return "";
    return actionText(child.props.children);
  }).join(" ").trim();
}

/** Native checked values and callbacks remain native policy. Aria supplies
 * the item press, hover, focus, disabled skipping and collection identity. */
export function NativeActionMenuItem({ children, onClick, disabled, type: _type, textValue, role, focusOnMount = false, actionRef, ...props }:
  Omit<MenuItemProps<unknown>, "onAction" | "children" | "render" | "isDisabled"> & { children: ReactNode; onClick?(): void;
  disabled?: boolean; type?: "button"; textValue?: string; role?: AriaRole; focusOnMount?: boolean;
  /** The row's element, as it is drawn: held, or handed to a function each time it is drawn or taken away. */
  actionRef?: RefObject<HTMLDivElement | HTMLAnchorElement | null> | ((element: HTMLDivElement | HTMLAnchorElement | null) => void) } & AriaAttributes) {
  const access = useContext(AccessContext), keysLead = useContext(ListKeysLead), generatedKey = useId(), focusKey = props.id ?? generatedKey, domId = useId();
  const mounted = useRef<Element | null>(null);
  // Drawn again as a typed key gives the keys the lead in a list holding them itself, its ring with them (menuInputLead.ts).
  useMenuInputLead();
  const itemRef = useCallback((element: HTMLDivElement | HTMLAnchorElement | null) => {
    if (typeof actionRef === "function") actionRef(element); else if (actionRef) actionRef.current = element;
    if (mounted.current === element) return;
    mounted.current = element;
    if (!element || !focusOnMount) return;
    // A native async confirmation replaces its focused target. Focus the
    // new engine key after the actual collection commits, without a DOM list.
    queueMicrotask(() => {
      const owner = access?.state.current;
      if (mounted.current !== element || !element.isConnected || !owner?.collection.getItem(focusKey) || owner.disabledKeys.has(focusKey)) return;
      owner.selectionManager.setFocused(true); owner.selectionManager.setFocusedKey(focusKey);
      if (element.parentElement) createFocusManager({ current: element.parentElement }, { accept: candidate => candidate === element }).focusFirst();
    });
  }, [access, focusKey, focusOnMount, actionRef]);
  // A row a redraw brings under a still pointer takes no highlight, as the
  // block menu's rows (NativeMenuItem): the pointer lights a row only as it
  // comes into one. The highlight stays where it was put — a confirmation's
  // "취소" giving it back to its target (focusOnMount), a search's first find —
  // where the browser's enter for the row that came under the pointer took it
  // (2026-11-18: a row move's 취소 lit the row after the list, 보관 or
  // 휴지통으로 이동, 5 ms after its target).
  const stillEnter = (enter: PointerEventHandler<HTMLElement> | undefined): PointerEventHandler<HTMLElement> => (event) => {
    const manager = access?.state.current?.selectionManager, focused = manager?.isFocused, key = manager?.focusedKey;
    // Nor the row the pointer first comes onto in a list beside a row that came up lit elsewhere (arrivalHoldsLight).
    const held = pointerStill(event) || arrivalHoldsLight(mounted.current);
    enter?.(event);
    if (!manager || !held || manager.focusedKey === key && manager.isFocused === focused) return;
    manager.setFocused(Boolean(focused)); manager.setFocusedKey(key ?? null);
  };
  // Lit, or held lit in a list beside a row its light has gone out of (besideLightHeld).
  const lit = (focused: boolean) => focused || !access?.state.current?.selectionManager.isFocused && besideLightHeld(mounted.current) ? "true" : undefined;
  return <MenuItem {...props} id={focusKey} ref={itemRef} textValue={textValue ?? props["aria-label"] ?? actionText(children)} isDisabled={disabled}
    onAction={onClick} shouldCloseOnSelect={false}
    // Keys that lead nothing (typing in the search, Escape) after the pointer led ring no row (menuInputLead.ts).
    render={(domProps, engine) => "href" in domProps ? <a {...domProps} id={domId} onPointerEnter={stillEnter(domProps.onPointerEnter)} onClickCapture={stopOpeningPressRelease}
      data-native-menu-item data-focused={lit(engine.isFocused)}
      data-focus-visible={(engine.isFocusVisible || keysLead && engine.isFocused) && !pointerKeptLead() ? "true" : undefined} />
      : <div {...domProps} id={domId} role={role ?? domProps.role} onPointerEnter={stillEnter(domProps.onPointerEnter)} onClickCapture={stopOpeningPressRelease}
      aria-checked={props["aria-checked"]} data-native-menu-item data-focused={lit(engine.isFocused)}
      data-focus-visible={(engine.isFocusVisible || keysLead && engine.isFocused) && !pointerKeptLead() ? "true" : undefined} />}>{children}</MenuItem>;
}

export function NativeMenuSection({ role, ...props }: Omit<MenuSectionProps<unknown>, "render"> & { role?: AriaRole }) {
  return <MenuSection {...props} render={domProps => <section {...domProps} role={role ?? domProps.role}
    aria-labelledby={role === "alertdialog" && props["aria-label"] ? undefined : domProps["aria-labelledby"]} />} />;
}
export { Header as NativeMenuHeader, Separator as NativeMenuSeparator };
