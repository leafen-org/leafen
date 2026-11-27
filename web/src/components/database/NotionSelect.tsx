"use client";

import { type CSSProperties, createContext, Fragment, type KeyboardEvent, type ReactNode, type RefObject, useContext, useEffect, useId, useLayoutEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useButton, useMenu, useMenuItem, useMenuTrigger } from "react-aria";
import { Item, type TreeState, useMenuTriggerState, useTreeState } from "react-stately";
import { POPOVER_MARGIN, placeAround, placeBesideRow, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { isInertMenuKey, isTextField, isTypedLetter, nativeMenuKeyboardDelegate } from "@/lib/menuKeyboard";
import { ariaScrollsNothing } from "@/lib/menuKeyScroll";
import { useEdgeFadeAsSized } from "@/lib/edgeFade";
import { useOpenAtChosen } from "@/lib/openAtChosen";
import { stopOpeningPressRelease } from "@/lib/nativeMenu";
import { useMenuVirtualFocus } from "@/lib/nativeMenuVirtualFocus";
import { type PickerWindowLine, pickerWindowBox, pickerWindowLine, usePickerWindow } from "@/lib/usePickerWindow";
import { CheckIcon, ChevronDown } from "../icons";
import { PhoneSettingChoice, usePhonePropertySettings } from "./PhonePropertySettings";
import { PropertySettingButton } from "./PropertySettingRow";
import { SettingsChevron } from "@/components/settings/SettingsChevron";
import styles from "./NotionSelect.module.css";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import { fieldEnter, fieldEscape } from "@/lib/fieldKeyBoundary";
import { keysFromNothing } from "@/lib/lastPress";

export type NotionSelectOption = {
  value: string; label: string; description?: string; note?: string; triggerLabel?: string; icon?: ReactNode; disabled?: boolean;
  /** The heading it falls under in the list, shown above the first option showing with it. */
  group?: string;
  /** A line above it, setting it and those after apart from those before, with no heading. */
  divided?: boolean;
};
/**
 * The ⌄ the selects in a place draw after their words, where that place
 * draws Notion's own mark in place of the default: a filter rule's and a
 * sort's thin chevron (arrowChevronSingleDownSmall, 8.64 by 14, seen
 * 2026-11-22). A select's own `caret` wins.
 */
export const NotionSelectCaret = createContext<ReactNode>(undefined);
export type NotionSelectSearch = {
  ariaLabel: string; placeholder: string; noResultsLabel?: string; hideDisabledOptions?: boolean;
  /** The heading kept over noResultsLabel when nothing answers (a time zone list's "시간대 선택"). */
  noResultsGroup?: string;
  match?: (option: NotionSelectOption, query: string) => boolean;
  /** How well an option answers the search, higher first and 0 for none, in place of `match`: a group's options then stand best first. */
  rank?: (option: NotionSelectOption, query: string) => number;
  /** Told the search's words as they change ("" as the list opens and closes), for a list whose owner searches the server. */
  onQueryChange?: (query: string) => void;
  /** The list scrolled to its end: its owner reads on and adds what follows to the options. */
  onEnd?: () => void;
};
function searchMatches(search: NotionSelectSearch | undefined, option: NotionSelectOption, query: string) {
  return !query || (search?.match ? search.match(option, query) : option.label.toLocaleLowerCase().includes(query));
}
/** The options that answer a search (`rank` over 0), best first within each group, else in their order. */
function ranked(options: NotionSelectOption[], rank: (option: NotionSelectOption) => number) {
  let run = -1;
  return options.map((option, index) => {
    if (index === 0 || option.group !== options[index - 1].group) run = index;
    return { option, index, run, rank: rank(option) };
  }).filter(entry => entry.rank > 0)
    .sort((a, b) => a.run - b.run || b.rank - a.rank || a.index - b.index).map(entry => entry.option);
}

function SelectMenuItem({ option, index, state, showCheck, className, multiple, place }: {
  option: NotionSelectOption; index: number; state: TreeState<NotionSelectOption>; showCheck: boolean; className?: string; multiple: boolean;
  /** Where it stands among the list's choices, said while the list draws only what it shows. */
  place?: { position: number; of: number };
}) {
  const ref = useRef<HTMLButtonElement>(null), id = useId();
  const { menuItemProps, labelProps, descriptionProps, isFocused, isFocusVisible, isSelected } = useMenuItem({ key: option.value, id, closeOnSelect: !multiple }, state, ref);
  return <button {...menuItemProps} ref={ref} type="button" disabled={option.disabled} onClickCapture={stopOpeningPressRelease}
    className={`${styles.notionSelectOption} ${className ?? ""}`} data-option-index={index} data-option-value={option.value}
    data-described={option.description ? "true" : undefined} aria-posinset={place?.position} aria-setsize={place?.of}
    data-active={isFocused ? "true" : undefined} data-focus-visible={isFocused && isFocusVisible ? "true" : undefined}>
    <span className={styles.notionSelectIcon} aria-hidden="true">{option.icon}</span>
    {/* The item is named by its name alone, its sentence describing it apart (2026-11-08: "켜짐사람들이 앱을 씁니다."). */}
    <span className={styles.notionSelectLabel} data-described={option.description ? "true" : undefined}>
      <span {...labelProps}>{option.label}</span>
      {option.description && <small {...descriptionProps} className={styles.notionSelectDescription}>{option.description}</small>}
      {option.note ? <small className={styles.notionSelectNote}>{option.note}</small> : null}
    </span>
    {showCheck && isSelected ? <CheckIcon size="tiny" aria-hidden="true" /> : null}
  </button>;
}

function SelectMenu({ id, ariaLabel, values, multiple, options, visibleOptions, search, query, setQuery, menuRef, searchInputRef,
  menuStyle, menuClassName, optionClassName, showCheck, lightFirst, unlit = false, choose, close }: {
  id?: string; ariaLabel: string; values: readonly string[]; multiple: boolean; options: NotionSelectOption[]; visibleOptions: NotionSelectOption[];
  search?: NotionSelectSearch; query: string; setQuery: (query: string) => void; menuRef: RefObject<HTMLDivElement | null>;
  searchInputRef: RefObject<HTMLInputElement | null>; menuStyle: CSSProperties; menuClassName?: string; optionClassName?: string;
  showCheck: boolean; lightFirst: boolean; unlit?: boolean; choose: (key: string) => void; close: (restore?: boolean) => void;
}) {
  const state = useTreeState<NotionSelectOption>({ items: visibleOptions,
    children: option => <Item key={option.value} textValue={option.label}>{option.label}</Item>,
    selectionMode: multiple ? "multiple" : "single", selectedKeys: values, disabledKeys: visibleOptions.filter(option => option.disabled).map(option => option.value) });
  const delegate = useMemo(() => nativeMenuKeyboardDelegate(state.collection, state.disabledKeys, menuRef), [state.collection, state.disabledKeys, menuRef]);
  const { menuProps: ariaMenuProps } = useMenu({ ...ariaScrollsNothing, id, "aria-label": ariaLabel, shouldFocusWrap: true, autoFocus: !search && !unlit,
    shouldUseVirtualFocus: true, escapeKeyBehavior: "none", keyboardDelegate: delegate, onAction: key => choose(String(key)),
    onClose: () => close(true) }, state, menuRef);
  const { menuProps, ownerProps } = useMenuVirtualFocus({ state, menuRef, menuProps: ariaMenuProps, enter: true,
    focusOwnerRef: search ? searchInputRef : undefined, quiet: unlit });
  // A long list (a time zone's 419) draws only the choices around what it
  // shows, the highlighted one always (usePickerWindow): drawn whole, its
  // opening press made every choice's menu item before its paint (2026-11-14:
  // 464 ms, CPU ×2). Its lines stand as far apart as its choices do.
  const optionsRef = useRef<HTMLDivElement>(null);
  // Its choices fade out at an edge they run on past, 1.7px a pixel scrolled
  // up to 34px, as Notion's lists (seen 2026-11-20 at 1440, dpr 2: the
  // settings language list, a property's number formats under their search
  // and a code block's languages alike — 8.5px at 5 scrolled, 34 from 20;
  // none on a list that fits). Measured as the list is laid out, never on
  // the opening press's own time (useEdgeFadeAsSized).
  useEdgeFadeAsSized(optionsRef, 1.7);
  const [gap, setGap] = useState(0);
  const focusedKey = state.selectionManager.focusedKey;
  // A list that marks its choice opens with it in view (useOpenAtChosen);
  // one that lights its first choice opens at its top.
  const [openChosen] = useState(() => lightFirst || unlit ? null : visibleOptions.find(option => values.includes(option.value))?.value ?? null);
  useOpenAtChosen(optionsRef, menuRef, openChosen);
  const shownKey = focusedKey ?? openChosen;
  const lines = usePickerWindow({ scrollRef: optionsRef, open: true, count: visibleOptions.length, keyAt: index => visibleOptions[index].value,
    sizeAt: index => expectedLineSize(visibleOptions[index], visibleOptions[index - 1], gap), gap,
    focused: shownKey === null ? -1 : visibleOptions.findIndex(option => option.value === shownKey) });
  useLayoutEffect(() => {
    const list = optionsRef.current;
    if (lines.windowed && list) setGap(Number.parseFloat(getComputedStyle(list).rowGap) || 0);
  }, [lines.windowed]);
  const drawLine = (line: PickerWindowLine) => {
    const option = visibleOptions[line.index], index = line.index;
    const parts = <>
      {option.group && option.group !== visibleOptions[index - 1]?.group
        ? <div role="presentation" className={styles.notionSelectGroup} data-first={index === 0 ? "true" : undefined}>{option.group}</div> : null}
      {option.divided && index > 0 ? <div role="presentation" className={styles.notionSelectDivider} /> : null}
      <SelectMenuItem option={option} index={options.indexOf(option)} state={state} showCheck={showCheck} className={optionClassName}
        multiple={multiple} place={lines.windowed ? { position: index + 1, of: visibleOptions.length } : undefined} />
    </>;
    if (line.top === undefined) return <Fragment key={line.key}>{parts}</Fragment>;
    const placed = pickerWindowLine(line, lines.measure);
    return <div key={line.key} {...placed} className={styles.notionSelectLine} style={{ ...placed.style, gap }}>{parts}</div>;
  };
  // After Aria's own autofocus, which lights the chosen one.
  useEffect(() => {
    if (!lightFirst) return;
    const first = delegate.getFirstKey();
    if (first !== null) { state.selectionManager.setFocused(true); state.selectionManager.setFocusedKey(first); }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- once, as it opens
  }, []);
  function menuKeys(event: KeyboardEvent<HTMLDivElement>) {
    // Escape closes this list alone: the panel it opened from hears the key
    // first otherwise, through the portal, and closed too (2026-10-06).
    if (fieldEscape(event)) {
      event.preventDefault(); event.stopPropagation(); close(true); return;
    }
    // Keys as in Notion's menus: a search field keeps its text editing, Tab
    // does nothing, and without a search Home, End, the page keys and typed
    // letters move no highlight either.
    if ((event.key === "Home" || event.key === "End") && isTextField(event.target)) return;
    if (isInertMenuKey(event, Boolean(search))) { event.preventDefault(); event.stopPropagation(); return; }
    if (!search && isTypedLetter(event)) return;
    if (event.target === searchInputRef.current && fieldEnter(event)) {
      event.preventDefault();
      const key = state.selectionManager.focusedKey ?? delegate.getFirstKey();
      if (key !== null) { choose(String(key)); if (!multiple) close(true); }
      return;
    }
    // The search field's ↑↓ move the highlight through the choices, as
    // Notion's do (seen 2026-11-14 in a code block's languages): the field
    // keeps focus and its caret, the highlight its keyboard ring.
    if (event.target === searchInputRef.current) { ownerProps.onKeyDown(event); return; }
    menuProps.onKeyDown?.(event);
  }
  return <div {...menuProps} ref={menuRef}
    className={`${styles.notionSelectMenu} ${menuClassName ?? ""}`} style={menuStyle}
    data-notion-select-menu onKeyDown={menuKeys}>
    {search && <div className={styles.notionSelectSearch}><input ref={searchInputRef} autoFocus={fieldFocusOnOpen()} type="search"
      aria-activedescendant={ownerProps["aria-activedescendant"]} aria-controls={ownerProps["aria-controls"]}
      aria-label={search.ariaLabel} placeholder={search.placeholder} value={query} onChange={event => setQuery(event.target.value)} /></div>}
    <div ref={optionsRef} className={styles.notionSelectOptions} data-edge-fade="" onScroll={search?.onEnd ? (event) => {
      const list = event.currentTarget;
      if (list.scrollTop + list.clientHeight >= list.scrollHeight - 32) search.onEnd?.();
    } : undefined}>
      {lines.windowed
        ? <div ref={lines.linesRef} role="presentation" style={pickerWindowBox(lines.height)}>{lines.lines.map(drawLine)}</div>
        : lines.lines.map(drawLine)}
      {!visibleOptions.length && search?.noResultsLabel && search.noResultsGroup
        ? <div role="presentation" className={styles.notionSelectGroup} data-first="true">{search.noResultsGroup}</div> : null}
      {!visibleOptions.length && search?.noResultsLabel
        && <div className={styles.notionSelectEmpty} data-under-head={search.noResultsGroup ? "true" : undefined}>{search.noResultsLabel}</div>}
    </div>
  </div>;
}

// A choice's height as it is expected to draw, its heading and line above it
// with it, before it is measured (NotionSelect.module.css).
function expectedLineSize(option: NotionSelectOption, previous: NotionSelectOption | undefined, gap: number) {
  const heading = option.group && option.group !== previous?.group ? (previous ? 36.9 : 33.9 + gap) : 0;
  return (option.description ? 45 : 30) + heading + (option.divided && previous ? 9 + gap : 0);
}

/**
 * A choice, as a setting's row with its value ("숫자 형식 숫자 ›") or a plain
 * select. In a phone's "속성 편집" a setting's row is the sheet's own and its
 * choices open in a sheet over it (PhoneSettingChoice), as Notion's there.
 */
export function NotionSelect(props: Parameters<typeof DesktopNotionSelect>[0]) {
  const phone = usePhonePropertySettings();
  const { settingLabel, multiple, options, value, placeholder } = props;
  if (!phone || !settingLabel || multiple) return <DesktopNotionSelect {...props} />;
  const selected = options.find(option => option.value === value) ?? (placeholder === undefined ? options[0] : undefined);
  return <PhoneSettingChoice label={settingLabel} icon={props.settingIcon} value={value}
    shown={selected?.triggerLabel || selected?.label || placeholder || ""}
    valueIcon={props.settingValueIcon === false ? undefined : selected?.icon} options={options} disabled={props.disabled}
    showCheck={props.showCheck !== false && !props.unmarked} long={Boolean(props.search)} onChange={props.onChange} />;
}

/** Native options, filtering and visual policy; Aria owns trigger, menu/item
 * interaction, collection focus and selection semantics. */
function DesktopNotionSelect({ ariaLabel, value, options, onChange, disabled = false, className, buttonClassName,
  menuClassName, optionClassName, search, showCheck = true, settingLabel, settingIcon, settingValueIcon = true, settingCaret, settingPlace = "below", settingMenuWidth = 250, placeholder, deselectable = false,
  describedBy, multiple, autoFocus = false, menuSize, menuMinWidth = 220, menuAlign = "start", menuUnder, settingsRow = false, settingsList = false, rowChoice = false, propertyChoice = false, unmarked = false, openAtFirst = false, plain = false, firstLit = false, choiceIcons = false, buttonContent, caret }: {
  /** The ⌄ its button draws after its words, Notion's own mark at that place (a pill editor's filled 12px one, a relative date's thin 16px one); else its place's (NotionSelectCaret), else the default. */
  caret?: ReactNode;
  /** A plain list right under it, as Notion's filter conditions and sort directions (seen 2026-11-05): 4px in, 28px rows a pixel apart in 14px, unticked, no marks.
   *  "spaced" is that list 4px under it with no row lit as it opens, as Notion's export window's (seen 2026-11-07);
   *  "ticked" is that with its chosen row ticked at its end, as Notion's new token window's lifetimes (seen 2026-11-13). */
  plain?: boolean | "spaced" | "ticked";
  ariaLabel: string; value: string; options: NotionSelectOption[]; onChange: (value: string) => void;
  /** The element that describes the choice, as a native select's aria-describedby. */
  describedBy?: string;
  /** Takes focus when it first shows, as a native select's autoFocus. */
  autoFocus?: boolean;
  /** Its list shows open when it first shows, for a way in that asks for this choice. */
  openAtFirst?: boolean;
  /** Several choices, each toggled from a list that stays open, as a native multiple select's. */
  multiple?: { values: readonly string[]; onChange: (values: string[]) => void };
  disabled?: boolean; className?: string; buttonClassName?: string; backdropClassName?: string;
  menuClassName?: string; optionClassName?: string; search?: NotionSelectSearch; showCheck?: boolean;
  settingLabel?: string; settingIcon?: ReactNode; settingCaret?: "right" | "down"; settingMenuWidth?: number;
  /** Whether a setting row's value shows its option's icon; the row's own icon or plain words stand without it. */
  settingValueIcon?: boolean;
  /** Where a setting row's list opens: under the row at its end, as a property's settings (Notion's, seen 2026-11-02), or beside its window. */
  settingPlace?: "beside" | "below";
  /** Shown while no option is chosen, rather than the first option as if it were. */
  placeholder?: string;
  /** What its button shows for the chosen option in place of its icon and words, as a checkbox filter's box (Notion's, seen 2026-11-12). */
  buttonContent?: ReactNode;
  /** Choosing the chosen option again leaves none chosen, as Notion's select editor does. */
  deselectable?: boolean;
  /** A plain select's list of its own size and distance under the button, as a code block's languages (Notion's 240 wide, 8px under).
   *  With `whole`, the list keeps its height: over its button where only that side has room, else on its own side moved
   *  into the window over its button, `whole` from the window's top and foot (Notion's code languages, 4.5). */
  menuSize?: { width: number; maxHeight: number; gap: number; whole?: number };
  /** A plain select's list is at least its button's width and this wide (220px unless a list's words need more). */
  menuMinWidth?: number;
  /** Which of the button's edges a plain select's list keeps to: a settings row's ends at its button's end. */
  menuAlign?: "start" | "end";
  /** What a plain select's list opens under in place of its button, its middle on that element's, `gap` below it: a
   *  day field's ⌄ opens its list under the whole field, as Notion's (seen 2026-11-22 at 1440 dark: 6 under it, centred). */
  menuUnder?: { of: (button: HTMLElement) => Element | null; gap: number };
  /** The list of a settings window row (settingsRowMenu). */
  settingsRow?: boolean;
  /** A plain select's list drawn as a property setting's, its button and place its own: its search 12px in,
   *  28px rows a pixel apart 4px in, their 14px words 8px in, as Notion's code block languages (seen 2026-11-20). */
  settingsList?: boolean;
  /** A view setting row's choice ("정렬", "카드 크기"): a 250px settings-row list
   *  right under the whole row at its end, its first choice lit, as Notion's
   *  (seen 2026-11-06). */
  rowChoice?: boolean;
  /** A view setting row's property ("캘린더 표시 기준"): its 300px list searches
   *  the properties, each by its icon, and its chevron stays turned right, as
   *  Notion's (seen 2026-11-06). Without rowChoice, the same list under its
   *  button at its own size (menuSize), kept under it and slid up to fit, as
   *  an advanced filter rule's property (Notion's, seen 2026-11-16). */
  propertyChoice?: boolean;
  /** Its list opens with its first choice lit, not the chosen one (Notion's relative date lists, seen 2026-11-06). */
  firstLit?: boolean;
  /** A setting row's list shows each choice's 20px icon 8px in, its name 8px past it, as Notion's status
   *  "다음과 같이 표시" (☑ 체크박스 · ⊙ 선택, seen 2026-11-16); the row's value stays its words alone. */
  choiceIcons?: boolean;
  /** The list marks no choice: no check, and it opens at its top rather than at the choice, as Notion's language list (seen 2026-10-30). */
  unmarked?: boolean;
}) {
  const placeCaret = useContext(NotionSelectCaret);
  const [open, setOpen] = useState(openAtFirst), [query, setQuery] = useState("");
  const [menuWidth, setMenuWidth] = useState(220);
  const buttonRef = useRef<HTMLButtonElement>(null), menuRef = useRef<HTMLDivElement>(null), searchInputRef = useRef<HTMLInputElement>(null);
  const chosen = multiple ? options.filter(option => multiple.values.includes(option.value)) : [];
  const selected = multiple
    ? (chosen.length ? { value: "", label: chosen.map(option => option.label).join(", ") } : undefined)
    : options.find(option => option.value === value) ?? (placeholder === undefined ? options[0] : undefined);
  // An empty choice (a form's blank first option) shows the placeholder too.
  const shown = selected?.triggerLabel || selected?.label || "";
  const placeheld = !shown && placeholder !== undefined;
  const visibleOptions = useMemo(() => {
    const shown = options.filter(option => !search?.hideDisabledOptions || !option.disabled);
    return search?.rank && query.trim() ? ranked(shown, option => search.rank!(option, query))
      : shown.filter(option => searchMatches(search, option, query.trim().toLocaleLowerCase()));
  }, [options, query, search]);
  const queryChanged = useRef(search?.onQueryChange);
  queryChanged.current = search?.onQueryChange;
  useEffect(() => { queryChanged.current?.(query); }, [query]);
  const triggerState = useMenuTriggerState({ isOpen: open, onOpenChange: setOpen });
  const { menuTriggerProps, menuProps: triggerMenuProps } = useMenuTrigger({ type: "menu", isDisabled: disabled || options.length === 0 }, triggerState, buttonRef);
  const { buttonProps: pressProps } = useButton({ ...menuTriggerProps, "aria-label": ariaLabel, "aria-describedby": describedBy,
    isDisabled: disabled || options.length === 0 }, buttonRef);
  // A setting row that wraps it in a <label> opens it from anywhere on the row,
  // as Notion's (seen 2026-10-24): the click the label hands on comes without
  // a press of its own, which the press handling above leaves alone.
  const pressedHere = useRef(false);
  const buttonProps = {
    ...pressProps,
    onPointerDown: (event: React.PointerEvent<HTMLButtonElement>) => { pressedHere.current = true; pressProps.onPointerDown?.(event); },
    onClick: (event: React.MouseEvent<HTMLButtonElement>) => {
      pressProps.onClick?.(event);
      const fromLabel = !pressedHere.current && event.detail > 0;
      pressedHere.current = false;
      if (fromLabel && !disabled && options.length > 0) setOpen(true);
    },
  };
  useEffect(() => {
    // Leafen's own: only the AI key window asks for it; the Notion note above is about the label's press.
    if (autoFocus) buttonRef.current?.focus({ preventScroll: true });
    // Only when it first shows.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  // Its button takes the focus back as a choice or Escape closes it, unless
  // keys put down with the focus on nothing opened it (lib/lastPress.ts
  // openerOf): the focus stays on nothing then, as Notion's.
  const backToButton = useRef(true), wasOpen = useRef(false);
  if (open && !wasOpen.current) backToButton.current = !keysFromNothing();
  wasOpen.current = open;
  function close(restoreFocus = false) {
    if (restoreFocus && backToButton.current) buttonRef.current?.focus({ preventScroll: true });
    triggerState.close(); setQuery("");
  }
  function choose(key: string) {
    const option = options.find(option => option.value === key);
    if (!option || option.disabled) return;
    if (multiple) {
      multiple.onChange(multiple.values.includes(key) ? multiple.values.filter(item => item !== key) : [...multiple.values, key]);
    } else onChange(deselectable && key === value ? "" : key);
  }
  // A setting row's list opens under the row at its end, as a property's
  // settings in Notion's column menu and view settings (seen 2026-11-02); a
  // plain select under itself.
  const keepUnder = settingsRow || rowChoice || propertyChoice || Boolean(settingLabel);
  // A property setting row whose list opens under it (not beside it).
  const settingRowBelow = Boolean(settingLabel) && !(settingCaret !== "down" && settingPlace === "beside");
  // A settings row's list that, at its height up to its cap, doesn't fit
  // under its button but fits over it turns over, its foot its gap over the
  // button; one that fits on neither side keeps the rule below. Notion's
  // (seen 2026-11-14 in a 723 window): "숫자 형식" 509..603 over 607..635;
  // the language list, 506 high (70% of the window) and scrolling, at
  // 178..684 under a button at 150..178, 100..606 over one at 606..634, and
  // moved up to 205..711 over one at 250 or 400. A view setting row's few
  // choices turn over the whole row alike, their foot on its top and their
  // end on its end (Notion's "페이지 보기 선택", seen 2026-11-12 and
  // 2026-11-15 in a 723 window: 206 high, 298..505 over its row at 505..533).
  // A property setting row's list (settingLabel) turns over its row alike, its
  // foot on the row's top, by its height up to its 70% cap (Notion's
  // dependencies "이동 기준", seen 2026-11-22 at 1440×722 dark, dpr 1: 113
  // high, 490.8..603.8 over its row at 603.8..631.8, its end on the row's).
  const turnAnchor = () => rowChoice ? popoverRectOf(buttonRef.current?.closest("label") ?? buttonRef.current)
    : (settingsRow && !settingLabel) || settingRowBelow ? popoverRectOf(buttonRef.current) : null;
  const turnGap = rowChoice || settingRowBelow ? 0 : menuSize?.gap ?? 4;
  // Turned over, it keeps that side while it stays open and fits there, its
  // foot on its button or row, though its search narrows it to fit under
  // (Notion's "이동 기준", seen 2026-11-22 at 1440×722 dark in two tabs: 84
  // high once "마" is typed, still over its row with room for it under).
  const turned = useRef(false);
  useEffect(() => { if (!open) turned.current = false; }, [open]);
  const turnsOver = (size: { height: number }, viewport: { height: number }) => {
    const anchor = turnAnchor();
    if (!anchor) return false;
    const needs = Math.min(size.height, menuSize?.maxHeight ?? (settingRowBelow ? Math.round(viewport.height * 0.7) : Infinity)) + turnGap;
    turned.current = needs <= anchor.top - POPOVER_MARGIN && (turned.current || needs > viewport.height - anchor.bottom - POPOVER_MARGIN);
    return turned.current;
  };
  const place = usePopoverPlacement(menuRef, open ? (size, viewport) => settingLabel && settingCaret !== "down" && settingPlace === "beside"
    ? placeBesideRow(buttonRef.current, size, viewport)
    : turnsOver(size, viewport) ? placeAround(turnAnchor()!, size, viewport,
      { prefer: "above", gap: turnGap, align: rowChoice || settingRowBelow ? "end" : menuAlign,
        ...(rowChoice || settingRowBelow ? { keepSide: true, minHeight: Math.ceil(size.height) } : {}),
        ...(menuSize ? { cap: menuSize.maxHeight } : settingRowBelow ? { cap: Math.round(viewport.height * 0.7) } : {}) })
    : placeAround(
      popoverRectOf(rowChoice ? buttonRef.current?.closest("label") ?? buttonRef.current
        : menuUnder && buttonRef.current ? menuUnder.of(buttonRef.current) ?? buttonRef.current : buttonRef.current)!, size, viewport,
      { gap: settingLabel || rowChoice ? 0 : menuUnder?.gap ?? menuSize?.gap ?? 4,
        align: settingLabel || rowChoice ? "end" : menuUnder ? "center" : menuAlign,
        // A settings row's long list keeps its whole height under its button,
        // moved up over it 12px in from the window's edge rather than turned
        // to the other side, as Notion's language list (seen 2026-10-30) and
        // a property's number formats (seen 2026-11-02).
        alignFallback: !keepUnder, ...(keepUnder ? { keepSide: true } : {}),
        ...((settingsRow || propertyChoice) && menuSize ? { minHeight: menuSize.maxHeight } : {}),
        // A view setting row's few choices show whole, slid up where they fit on neither side; a
        // property setting's long list, as a number's formats, up to 70% of the window (Notion's,
        // seen 2026-11-06: 611px at 873, its top over the row).
        ...(rowChoice || settingLabel ? { minHeight: Math.ceil(size.height) } : {}),
        ...(menuSize?.whole !== undefined ? { whole: true, marginY: menuSize.whole } : {}),
        ...(menuSize ? { cap: menuSize.maxHeight } : settingLabel ? { cap: Math.round(viewport.height * 0.7) } : {}) }) : null,
    [open, settingLabel, settingMenuWidth, settingPlace, menuSize?.gap, menuSize?.maxHeight, menuSize?.whole, menuAlign, menuUnder?.gap, keepUnder, rowChoice, settingRowBelow], undefined,
    { kind: "menu", restoreFocus: false, onEscape: () => close(true), onOutsidePress: () => close(), inside: () => [buttonRef.current] });
  const menuStyle: CSSProperties = { position: "fixed", width: settingLabel ? settingMenuWidth : rowChoice ? (propertyChoice ? 300 : 250) : menuSize?.width ?? menuWidth, ...popoverPlacementStyle(place) };
  useEffect(() => {
    if (!open) return;
    setQuery("");
    const trigger = buttonRef.current;
    if (!trigger) return;
    const width = () => setMenuWidth(Math.max(trigger.getBoundingClientRect().width, menuMinWidth));
    width();
    const observer = new ResizeObserver(width); observer.observe(trigger);
    return () => observer.disconnect();
  }, [open, menuMinWidth]);

  const menu = open ? <SelectMenu id={triggerMenuProps.id} ariaLabel={ariaLabel} values={unmarked || plain === "spaced" ? [] : multiple ? multiple.values : [value]} multiple={Boolean(multiple)} options={options}
    visibleOptions={visibleOptions} search={search} query={query} setQuery={setQuery} menuRef={menuRef} searchInputRef={searchInputRef}
    menuStyle={menuStyle} menuClassName={`${settingLabel || settingsList ? styles.settingMenu : ""} ${settingsRow || settingsList || rowChoice || propertyChoice || settingLabel ? styles.settingsRowMenu : ""} ${propertyChoice ? styles.propertyChoiceMenu : ""} ${propertyChoice && unmarked ? styles.unmarkedChoices : ""} ${choiceIcons ? styles.iconChoices : ""} ${plain ? styles.plainMenu : ""} ${plain === "spaced" || plain === "ticked" ? styles.plainSpaced : ""} ${menuClassName ?? ""}`}
    optionClassName={`${plain ? styles.plainOption : ""} ${plain === "ticked" ? styles.plainTicked : ""} ${optionClassName ?? ""}`}
    showCheck={showCheck && !unmarked && (!plain || plain === "ticked")} lightFirst={rowChoice || firstLit || Boolean(settingLabel)}
    unlit={plain === "spaced" || plain === "ticked"} choose={choose} close={close} /> : null;
  return <span className={`${styles.notionSelect} ${className ?? ""}`} data-notion-select="">
    {settingLabel ? <PropertySettingButton {...buttonProps} buttonRef={buttonRef} label={settingLabel} icon={settingIcon}
      value={placeheld ? placeholder : shown} valueIcon={settingValueIcon ? selected?.icon : undefined} caret={settingCaret} className={buttonClassName} /> : <button {...buttonProps} ref={buttonRef} type="button" className={`${styles.notionSelectButton} ${buttonClassName ?? styles.fieldLook}`} data-notion-select-button=""
      data-property-choice={propertyChoice ? "true" : undefined}>
      {buttonContent && !placeheld ? buttonContent : <>
        {selected?.icon && !rowChoice && <span className={styles.notionSelectIcon} aria-hidden="true">{selected.icon}</span>}
        <span className={styles.notionSelectLabel} data-placeholder={placeheld ? "true" : undefined}>{placeheld ? placeholder : shown}</span>
      </>}{settingsRow ? <SettingsChevron /> : caret ?? placeCaret ?? <ChevronDown size="tiny" aria-hidden="true" />}
    </button>}
    {menu && (typeof document === "undefined" ? menu : createPortal(menu, document.body))}
  </span>;
}
