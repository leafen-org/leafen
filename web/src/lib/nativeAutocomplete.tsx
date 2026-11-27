import { type HTMLAttributes, type KeyboardEvent, type ReactNode, type RefObject, useEffect, useMemo, useRef, useState } from "react";
import { mergeProps, useComboBox, useListBox, useOption } from "react-aria";
import { type Key, type ListState, useComboBoxState } from "react-stately";
import { nativeMenuKeyboardDelegate } from "./menuKeyboard";
import { ariaScrollsNothing, useKeyWalkScroll } from "./menuKeyScroll";
import { PickerCollection } from "./pickerCollection";
import { routeOverlayKeyDown } from "./overlayLayer";
import { PickerRow, type PickerRowParts, drawPickerRows, drawsPickerRows, rowNameProps } from "./pickerRow";
import { keysMovedHighlight, movedPointerHover } from "./pointerHover";
import { fieldEnter, imeComposing } from "./fieldKeyBoundary";

const NO_KEYS: Set<Key> = new Set();

/**
 * The popover Aria's combobox is told it opens in. While its list is open it
 * hides from assistive tools all but its field and that popover
 * (ariaHideOutside): told its own list, it hid the rest of the menu, panel or
 * window holding it and the page with it — a docked view settings' "←" and
 * "×" by its "새 정렬" and "필터 추가" lists (2026-11-18). Told the body, it
 * hides nothing: what shuts the page from assistive tools is a modal layer's
 * alone (overlayLayer.ts), as Notion's open "정렬 기준" and "필터 기준" lists
 * leave its page, the docked panel's "뒤로" and "닫기" and the menu's "고급
 * 필터 추가" in the tree (seen 2026-11-18 at 1440, docked and as a popover).
 * Aria reads it besides only for a blur into its popover, which these pickers,
 * closing on no blur and holding no value, do not act on.
 */
const WHOLE_PAGE: RefObject<HTMLElement | null> = { get current() { return typeof document === "undefined" ? null : document.body; } };

/** A mounted native picker owns its query, allowed choices and commit. Aria
 * owns the input/list relationship, virtual focus, collection navigation
 * and option hover; its rows scroll as the keys walk them (`useKeyWalkScroll`); its rows are `PickerRow`s, pressed as Aria's
 * options. Its transient key is never stored as a second native selection
 * authority. */
export function useNativeAutocomplete<T>({ items, query, onQuery, keyOf, textOf, onPick, onClose, inputRef, listRef, popoverRef,
  label, open = true, disabledKeys = [], firstKey, commitTab = true, highlightFirst = true, wrap = "round", onEnterNone,
  dimmedRowsTakeKeys = false }: {
  items: T[]; query: string; onQuery(value: string): void; keyOf(item: T): string; textOf(item: T): string;
  onPick(item: T): void; onClose?: () => void; inputRef: RefObject<HTMLInputElement | null>;
  listRef: RefObject<HTMLElement | null>; label: string; open?: boolean; disabledKeys?: string[];
  popoverRef?: RefObject<HTMLElement | null>;
  firstKey?: string; commitTab?: boolean;
  /** Whether a row is highlighted before the arrows move: a picker whose rows Notion leaves plain says no. */
  highlightFirst?: boolean;
  /** How the arrows go on past an end, as the picker's Notion list does:
   * round to the other end ("round", the people lists), round through no
   * row lit, the field's own step ("throughNone", the link and invite
   * fields), or not at all ("stop", search) — seen 2026-11-14. */
  wrap?: "round" | "throughNone" | "stop";
  /** Enter with no row lit: what the picker does instead of closing (a
   * people list nothing, an invite field its own Enter). */
  onEnterNone?: (event: KeyboardEvent<HTMLInputElement>) => void;
  /** Whether a dimmed row (`disabledKeys`) takes the arrows and a search's
   * first place as any row does, lit by nothing and chosen by no Enter,
   * where the other pickers' arrows pass over it (the move list's,
   * MOVE_LIST_KEYS). */
  dimmedRowsTakeKeys?: boolean;
}) {
  // The rows' collection is made again only when the rows are new (a picker
  // keeps its rows while its highlight moves), each row's node as it is read
  // (`PickerCollection`).
  const collection = useMemo(() => new PickerCollection(items, keyOf, textOf), [items]);
  const state = useComboBoxState<T>({ items, collection, inputValue: query, onInputChange: onQuery, value: null,
    onChange: key => { const item = key == null ? undefined : collection.getItem(key)?.value; if (item != null) onPick(item); },
    disabledKeys, allowsEmptyCollection: true, allowsCustomValue: true, shouldCloseOnBlur: false,
    onOpenChange: next => { if (!next && open) onClose?.(); }, menuTrigger: "manual" });
  const delegate = useMemo(() => nativeMenuKeyboardDelegate(state.collection, dimmedRowsTakeKeys ? NO_KEYS : state.disabledKeys, listRef),
    [state.collection, state.disabledKeys, listRef, dimmedRowsTakeKeys]);
  const shouldFocusWrap = wrap !== "stop";
  const { inputProps, listBoxProps: options } = useComboBox({ inputRef, listBoxRef: listRef, popoverRef: WHOLE_PAGE,
    "aria-label": label, shouldFocusWrap, keyboardDelegate: delegate }, state);
  const { listBoxProps } = useListBox({ ...options, ...ariaScrollsNothing, shouldFocusWrap, shouldFocusOnHover: false,
    autoFocus: open && state.isOpen ? options.autoFocus : false,
    escapeKeyBehavior: "none", keyboardDelegate: delegate }, state, listRef);
  // Its rows scroll as Notion's menus scroll theirs, in the list or the popover around it.
  useKeyWalkScroll(popoverRef ?? listRef, state.selectionManager.focusedKey);
  // A row's press selects it, as Aria's option's, which picks it (`onChange`).
  drawPickerRows(state, listBoxProps, key => state.selectionManager.replaceSelection(key));
  useEffect(() => {
    if (open) state.open(highlightFirst ? "first" : null, "manual"); else state.setOpen(false);
  }, [open]);
  // Results that come in later move the highlight to the new first row
  // until the person moves it (`caretPicker`).
  const previous = useRef<{ query: string; firstKey?: string; chosen: unknown } | undefined>(undefined);
  // The arrows that went past an end to the field's own step leave no row
  // lit until they move again or the search changes ("throughNone").
  const restsAtNone = useRef(false);
  useEffect(() => {
    if (!open || !highlightFirst) return;
    if (restsAtNone.current && previous.current?.query === query) return;
    restsAtNone.current = false;
    const focused = state.selectionManager.focusedKey;
    const key = firstKey && state.collection.getItem(firstKey) && !state.disabledKeys.has(firstKey)
      ? firstKey : delegate.getFirstKey();
    if (previous.current?.query === query && previous.current.firstKey === firstKey
      && state.collection.getItem(focused!) && !(focused === previous.current.chosen && focused !== key)) return;
    state.selectionManager.setFocused(true); state.selectionManager.setFocusedKey(key);
    previous.current = { query, firstKey, chosen: key };
  }, [open, query, firstKey, highlightFirst, state.collection, state.disabledKeys, delegate]);
  // Whether the arrows moved the highlight last, since the list opened or its
  // search changed: the list says so (`data-keyed`) for the lists Notion
  // rings the arrows' row in (select options, search, link field, CSV place,
  // filter properties); the pointer taking the highlight ends it.
  const [keyed, setKeyed] = useState(false);
  useEffect(() => setKeyed(false), [open, query]);
  function inputKeys(event: KeyboardEvent<HTMLInputElement>) {
    if (event.defaultPrevented || imeComposing(event) || event.altKey || event.ctrlKey || event.metaKey) return;
    if (["ArrowDown", "ArrowUp", "PageDown", "PageUp"].includes(event.key)) {
      setKeyed(true); keysMovedHighlight(listRef.current); restsAtNone.current = false;
    }
    const manager = state.selectionManager, lit = manager.isFocused && manager.focusedKey != null
      && state.collection.getItem(manager.focusedKey) ? manager.focusedKey : null;
    // Past an end the field's own step comes, no row lit, then the other end.
    if (wrap === "throughNone" && lit != null
      && (event.key === "ArrowDown" && lit === delegate.getLastKey() || event.key === "ArrowUp" && lit === delegate.getFirstKey())) {
      event.preventDefault();
      restsAtNone.current = true;
      manager.setFocusedKey(null);
      return;
    }
    if (fieldEnter(event) && lit == null && onEnterNone) { onEnterNone(event); return; }
    // A dimmed row the arrows came to is no choice: Enter there does nothing.
    if (fieldEnter(event) && lit != null && state.disabledKeys.has(lit)) { event.preventDefault(); return; }
    // Native search keeps Home/End as text editing. Tab commits in pickers
    // that historically commit it; the engine supplies the focused choice.
    if (event.key === "Home" || event.key === "End") return;
    // An embedded picker leaves Escape to its native containing overlay.
    if (event.key === "Escape" && !onClose) { routeOverlayKeyDown(event); return; }
    if (event.key === "Tab") {
      if (commitTab) { event.preventDefault(); state.commit(); }
      return;
    }
    inputProps.onKeyDown?.(event);
  }
  // Hover highlights only where the pointer really moves, after the arrows
  // only once it moves into another row (`pointerHover`).
  return { state, inputProps: { ...inputProps, onKeyDown: inputKeys }, keyed,
    listBoxProps: { ...mergeProps(listBoxProps, { onPointerMove: movedPointerHover(state, () => setKeyed(false)) }),
      "data-keyed": keyed ? "true" : undefined },
    activeKey: state.selectionManager.focusedKey, active: collection.indexOf(state.selectionManager.focusedKey) };
}

type NativePickerOptionProps<T> = {
  state: ListState<T>; itemKey: string;
  buttonRef?: RefObject<HTMLElement | null>; element?: "button" | "div";
  children(parts: PickerRowParts): ReactNode;
} & Omit<HTMLAttributes<HTMLElement>, "children"> & { disabled?: boolean };

/** A row of a picker: a `PickerRow` where its picker's search keeps the focus
 * (`useNativeAutocomplete`), else Aria's option, which takes the focus as it
 * is highlighted (`useNativeList`). */
export function NativePickerOption<T>({ buttonRef, ...props }: NativePickerOptionProps<T>) {
  if (!drawsPickerRows(props.state)) return <FocusedPickerOption buttonRef={buttonRef} {...props} />;
  return <PickerRow {...props} rowRef={buttonRef} {...props.element === "div" ? {} : { "data-native-picker-option": true }} />;
}

function FocusedPickerOption<T>({ state, itemKey, buttonRef, element = "button", children, ...attributes }: NativePickerOptionProps<T>) {
  const localRef = useRef<HTMLElement>(null), ref = buttonRef ?? localRef;
  const { optionProps, labelProps, descriptionProps, isFocused } = useOption({ key: itemKey }, state, ref);
  // Named as every row is (`rowNameProps`): Aria names it by its label part
  // where it draws one, the place drawn under it joins the name.
  const placeId = `${optionProps.id}-place`;
  let placed = false;
  const content = children({ active: isFocused, labelProps, placeProps: { get id() { placed = true; return placeId; } }, descriptionProps });
  const name = rowNameProps({ label: optionProps["aria-labelledby"], place: placed ? placeId : undefined,
    description: optionProps["aria-describedby"] }, attributes["aria-label"]);
  if (element === "div") return <div {...optionProps} {...attributes} {...name} ref={ref as RefObject<HTMLDivElement | null>}
    data-active={isFocused ? "true" : undefined}>{content}</div>;
  return <button {...optionProps} {...attributes} {...name} ref={ref as RefObject<HTMLButtonElement | null>} type="button"
    data-native-picker-option data-active={isFocused ? "true" : undefined}>{content}</button>;
}
