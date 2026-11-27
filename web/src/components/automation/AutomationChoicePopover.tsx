import { Fragment, useContext, useLayoutEffect, useMemo, useRef, useState, type CSSProperties, type KeyboardEvent, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { CheckIcon, ChevronRight, XCircleIcon } from "@/icons/leafen";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { usePopoverAppear } from "@/lib/popoverAppear";
import styles from "./AutomationChoicePopover.module.css";
import { POPOVER_MARGIN } from "@/lib/popoverPlacement";
import { PropertyIcon } from "@/components/database/PropertyIcon";
import type { DbProperty } from "@/lib/types";
import { AutomationPhoneSheets } from "./AutomationActionMenus";
import { AutomationChoiceSheet } from "./AutomationChoiceSheet";

/**
 * A property's mark in an automation's lists, as Notion's: its 16px mark drawn
 * a fifth larger, 19.2px (seen 2026-11-10 in "+ 새 조건", "속성 편집..." and a
 * page line's lists).
 */
export function AutomationPropertyMark({ property }: { property: Pick<DbProperty, "type" | "config"> }) {
  return <span className={styles.propertyMark}><PropertyIcon property={property} size="small" /></span>;
}

export type AutomationChoice = {
  key: string;
  text: string;
  icon?: ReactNode;
  /** A second, grey line under the words ("전체 데이터 소스"). */
  detail?: string;
  chosen?: boolean;
  /** The group it heads into ("이벤트", "속성"), over a line after the first. */
  group?: string;
  /** A line over it with no group name ("∑ 사용자 지정 수식 작성"). */
  rule?: boolean;
  more?: boolean;
  /** Red on a phone's sheet only: a desktop menu keeps a delete in its words, as Notion's (AutomationActionMenus). */
  danger?: boolean;
  /** A property's mark, in a 24px box, its words 40px in (Notion's property lists, seen 2026-11-10). */
  wideIcon?: boolean;
};

type ChoicePopoverProps = Parameters<typeof DesktopChoicePopover>[0];

/** A choice list: on a phone a list rising from the screen's foot (AutomationChoiceSheet), elsewhere a menu under its opener. */
export function AutomationChoicePopover(props: ChoicePopoverProps) {
  return useContext(AutomationPhoneSheets) ? <AutomationChoiceSheet {...props} /> : <DesktopChoicePopover {...props} />;
}

/**
 * A desktop menu under what opened it, as Notion's automation dialog's (seen
 * 2026-10-24 at 1280px): 28px rows of a mark and words, groups under a 12px
 * grey name and a line, two-line 45px rows with a ✓ for where it applies,
 * a search field first where it has one. A mark sits 8px in and its words 8px
 * after it, a property's in a 24px box (2026-11-10). The field keeps focus and
 * arrows move the highlight.
 */
function DesktopChoicePopover({ choices, label, title, searchPlaceholder, width = 266, align = "start", flush = false, stacked = false,
  properties = false, tightRules = false, keepOpen = false, quiet = false, greyMarks = false, picked, clearLabel, onClear, emptyText, trigger, pickDropsFocus = false,
  afterEmptyGroup = false, onPick, onClose }: {
  choices: AutomationChoice[];
  label: string;
  /** Its grey words over the search ("알림을 받을 사람:"). */
  title?: string;
  searchPlaceholder?: string;
  width?: number;
  /** Right under its opener, its search 12px in, as the dialog's "+ 새 조건" (Notion's, seen 2026-11-02). */
  flush?: boolean;
  /** Two-line choices whose second line wraps, the mark and ✓ on the first (the open action's "에서"). */
  stacked?: boolean;
  /** A page line's properties, as Notion's "속성 편집 ⌄" and a line's own "⌄" (seen 2026-11-10): the search 16px under its top. */
  properties?: boolean;
  /** Its lines between rows 4px from each, as Notion's "적용할 페이지 위치" list (seen 2026-11-15). */
  tightRules?: boolean;
  /** Its one group a group's 1px line lower, after an empty one, as Notion's condition "⋯" (seen 2026-11-15: 37 high). */
  afterEmptyGroup?: boolean;
  /** Under its opener's start, centred under it as a new page's "속성 편집 ⌄" list, or at its end as the people's. */
  align?: "start" | "center" | "end";
  /** Stays open as each is picked, as Notion's people (seen 2026-11-08): Escape or a press outside closes it. */
  keepOpen?: boolean;
  /** Opens with no row lit until the pointer or a key moves in, as Notion's person value's "다음으로 교체… ⌄" (seen 2026-11-11). */
  quiet?: boolean;
  /** Its marks grey, as Notion's list of conditions to add (seen 2026-11-11); a menu's are its words' colour. */
  greyMarks?: boolean;
  /** What is picked so far, in the search's box before its words (the people's); null while nothing is. */
  picked?: ReactNode;
  /** "⊗" at the box's end, taking back all that is picked. */
  clearLabel?: string;
  onClear?(): void;
  /** What a search that finds nothing says in grey, as Notion's property lists' "결과 없음" (seen 2026-11-14). */
  emptyText?: string;
  trigger: HTMLElement | null;
  /** A pick that removes its opener leaves the keys nowhere, as Notion's condition "삭제" (seen 2026-11-15 in two tabs). */
  pickDropsFocus?: boolean;
  onPick(key: string): void;
  onClose(): void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const [query, setQuery] = useState("");
  const shown = choices.filter((choice) => choice.text.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()));
  const pickedOne = useRef(false);
  usePopoverAppear(true, ref, () => trigger);
  const layer = useOverlayLayer(true, { kind: "picker", ref, onEscape: onClose, onOutsidePress: onClose, inside: () => [trigger],
    returnFocusRef: { current: trigger }, shouldRestoreFocus: () => !(pickDropsFocus && pickedOne.current) });
  // It opens with its first choice lit, from the pointer too, under its
  // search as without one, as Notion's choice lists (seen 2026-11-08: "+ 새
  // 조건"'s "편집된 모든 속성", the people list's first).
  const menu = useNativeMenu({ items: shown, open: true, menuRef: listRef, label, focusOnOpen: !searchPlaceholder, highlightOnOpen: !quiet,
    quietOnPointer: quiet,
    ...(searchPlaceholder ? { focusOwnerRef: searchRef } : {}),
    keyOf: (choice) => choice.key, textOf: (choice) => choice.text, onAction: (choice) => {
      if (keepOpen) setQuery(""); else { pickedOne.current = true; onClose(); }
      onPick(choice.key);
    } });
  // Under its opener, kept on the screen: over it where it fits there and
  // not under it, as Notion's at the window's foot — its foot then stays on
  // the opener as it grows or shrinks, as Notion's people's (seen 2026-11-08).
  const [above, setAbove] = useState<number | null>(null);
  const place = useMemo((): CSSProperties => {
    const rect = trigger?.getBoundingClientRect();
    if (!rect || typeof window === "undefined") return { top: 0, left: 0, width };
    const start = align === "center" ? rect.left + rect.width / 2 - width / 2 : align === "end" ? rect.right - width : rect.left;
    const left = Math.max(POPOVER_MARGIN, Math.min(start, window.innerWidth - width - POPOVER_MARGIN));
    if (above !== null) return { bottom: window.innerHeight - rect.top + (flush ? 0 : 4), left, width, maxHeight: rect.top - POPOVER_MARGIN - 4 };
    return { top: rect.bottom + (flush ? 0 : 4), left, width, maxHeight: Math.max(160, window.innerHeight - rect.bottom - 16) };
  }, [above, align, flush, trigger, width]);
  // Its whole height, its list's hidden rows too (the list shrinks to the room
  // under the opener), measured again as its rows come (the people's load).
  useLayoutEffect(() => {
    const rect = trigger?.getBoundingClientRect();
    const list = listRef.current;
    const height = (ref.current?.scrollHeight ?? 0) + Math.max(0, (list?.scrollHeight ?? 0) - (list?.clientHeight ?? 0));
    if (!rect || above !== null) return;
    const below = window.innerHeight - rect.bottom - POPOVER_MARGIN - 4;
    if (height > below && rect.top - POPOVER_MARGIN - 4 > below) setAbove(height);
  });
  const highlighted = menu.ownerProps["aria-activedescendant"];
  useLayoutEffect(() => {
    const search = searchRef.current;
    if (!search) return;
    if (highlighted) search.setAttribute("aria-activedescendant", highlighted); else search.removeAttribute("aria-activedescendant");
    search.setAttribute("aria-controls", menu.ownerProps["aria-controls"] ?? "");
  }, [highlighted, menu.ownerProps]);
  useLayoutEffect(() => { searchRef.current?.focus({ preventScroll: true }); }, []);
  const keyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.target === searchRef.current && menu.ownerProps.onKeyDown(event)) return;
    layer.onKeyDown(event);
  };
  if (typeof document === "undefined") return null;
  return createPortal(
    <div ref={ref} role="dialog" aria-label={label} className={styles.popover} style={{ ...place, zIndex: layer.zIndex }} data-automation-popover
      data-flush={flush || undefined} data-stacked={stacked || undefined} data-properties={properties || undefined} data-tight-rules={tightRules || undefined}
      data-after-empty-group={afterEmptyGroup || undefined}
      data-grey-marks={greyMarks || undefined}
      onKeyDown={keyDown} onKeyUp={(event) => { if (event.target === searchRef.current) menu.ownerProps.onKeyUp(event); }}>
      {title ? <div className={styles.group}>{title}</div> : null}
      {searchPlaceholder ? (
        <div className={styles.searchRow}>
          {picked !== undefined ? (
            <div className={styles.searchBox} onPointerDown={(event) => { if (event.target === event.currentTarget) event.preventDefault(); }}
              onClick={() => searchRef.current?.focus()}>
              {picked}
              <input ref={searchRef} className={styles.searchInBox} value={query} aria-label={searchPlaceholder}
                placeholder={picked ? undefined : searchPlaceholder}
                onChange={(event) => setQuery(event.currentTarget.value)} />
              {onClear ? <button type="button" className={styles.clear} aria-label={clearLabel} onClick={onClear}>
                <XCircleIcon size="small" aria-hidden="true" />
              </button> : null}
            </div>
          ) : <input ref={searchRef} className={styles.search} value={query} placeholder={searchPlaceholder} aria-label={searchPlaceholder}
            onChange={(event) => setQuery(event.currentTarget.value)} />}
        </div>
      ) : null}
      <div {...menu.menuProps} ref={listRef} className={styles.list}>
        {emptyText && !shown.length && query.trim() ? <div className={styles.group} role="status">{emptyText}</div> : null}
        {shown.map((choice, index) => (
          <Fragment key={choice.key}>
            {choice.group && choice.group !== shown[index - 1]?.group ? (
              <>
                {index ? <div className={styles.rule} /> : null}
                <div className={styles.group}>{choice.group}</div>
              </>
            ) : choice.rule && index ? <div className={styles.rule} /> : null}
            <NativeMenuItem state={menu.state} itemKey={choice.key} className={choice.detail ? styles.tallItem : styles.item}
              data-danger={choice.danger || undefined} data-automation-choice={choice.key}>
              {({ labelProps }) => (
                <>
                  {choice.icon ? <span className={styles.icon} data-wide={choice.wideIcon || undefined} aria-hidden="true">{choice.icon}</span> : null}
                  <span className={styles.words} {...labelProps}>
                    <span>{choice.text}</span>
                    {choice.detail ? <small>{choice.detail}</small> : null}
                  </span>
                  {choice.chosen ? <CheckIcon size="small" aria-hidden="true" /> : null}
                  {choice.more ? <ChevronRight size="small" className={styles.more} aria-hidden="true" /> : null}
                </>
              )}
            </NativeMenuItem>
          </Fragment>
        ))}
      </div>
    </div>,
    document.body,
  );
}
