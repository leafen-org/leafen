import { Fragment, useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { createFocusManager } from "@react-aria/focus";
import { useDialog } from "react-aria";
import { useTranslation } from "react-i18next";
import type { GraphViewType } from "@/contentGraph";
import { NativeChoiceGrid } from "@/lib/nativeChoiceGrid";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { ArrowUpRightIcon, CheckIcon, ChevronRight, Plus, SquareOnSquarePlusIcon } from "@/icons/leafen";
import { databaseViewLabels } from "./databaseViewLabels";
import { ViewTypeIcon } from "./databaseViewShared";
import { VIEW_TAB_DISPLAYS, type ViewTabDisplay } from "./databaseViewTabDisplay";
import styles from "./databaseSheet";
import { placeAround, popoverPlacementStyle, popoverRectOf, POPOVER_MARGIN, usePopoverPlacement } from "@/lib/popoverPlacement";
import { inlineEndKey, inlineStartKey } from "@/lib/readingDirection";
import { touchScreen } from "@/lib/screenKind";
import { useHoverTip } from "../IconTipButton";
import { imeComposing } from "@/lib/fieldKeyBoundary";

export function DatabaseAddViewChoices({ types, open, onCreate, selectedType, className, itemClassName, onAddSource, heading, sheet }: {
  types: GraphViewType[]; open: boolean; onCreate(type: GraphViewType): void;
  onAddSource?(): void;
  selectedType?: GraphViewType; className?: string; itemClassName?: string;
  /** The small heading of a menu that adds a view beside a database's title. */
  heading?: string;
  /** Drawn in a touch screen's sheet: "새 데이터 소스" with a 22px mark, as Notion's iPad (DatabaseViewTabs .titleAddViewSheetPanel). */
  sheet?: boolean;
}) {
  const { t } = useTranslation("databaseView");
  const labels = databaseViewLabels(), ref = useRef<HTMLDivElement>(null), dialogRef = useRef<HTMLDivElement>(null);
  const { dialogProps } = useDialog({ "aria-label": labels.addNewView }, dialogRef);
  // On a touch screen the first tile is lit without taking the focus, so it
  // has no key's ring, as Notion's iPad (seen 2026-11-14 at 834: right after
  // "+" or "새 보기" document.activeElement is the body and "표" alone has
  // the 5% fill); a mouse's menu gives the keys the first tile.
  const [keys] = useState(() => !touchScreen());
  return <div {...dialogProps} ref={dialogRef}>
    {heading && <div className={styles.titleAddViewHeading}>{heading}</div>}
    <NativeChoiceGrid items={types} keyOf={type => type} textOf={type => labels.viewTypes[type]} autoFocus={keys}
      marker={keys ? undefined : (type): Record<string, string> => (type === types[0] ? { "data-lit": "true" } : {})}
      listRef={ref} open={open} checkedKey={selectedType} label={labels.addNewView} className={className ?? styles.titleAddViewGrid}
      itemClassName={itemClassName ?? styles.inlineAddViewType} onAction={onCreate}>
      {/* Beside a database's title Notion's tiles carry 22px marks (seen 2026-10-25). */}
      {type => <><ViewTypeIcon type={type} size={heading ? "wide" : className ? "small" : "regular"} /><span>{labels.viewTypes[type]}</span></>}
    </NativeChoiceGrid>
    {onAddSource && <>
      {heading && <div className={`${styles.viewMenuSeparator} ${styles.titleAddViewSeparator}`} role="separator" />}
      <button type="button" className={heading ? `${styles.viewMenuItem} ${styles.titleAddViewSource}` : styles.viewMenuItem} onClick={onAddSource}>
        {heading ? <SquareOnSquarePlusIcon size={sheet ? "wide" : "regular"} aria-hidden="true" /> : <Plus size="small" aria-hidden="true" />}{t("newDataSource")}
      </button>
    </>}
  </div>;
}

/**
 * The view types beside the views list's "새 보기", the list staying open,
 * as Notion's (seen 2026-11-03): 390 wide on the list's right, or on its left
 * when the right lacks room, 4px over its edge and centered on the row; the
 * types only, the list holding "새 데이터 소스". Escape closes it alone. A
 * touch screen opens them right under the row, centered on it, as Notion's
 * iPad (seen 2026-11-14 at 834: under a row on 108–390 ending at 390.8,
 * 484 wide from 7, at 390.8).
 */
export function DatabaseViewAddBeside({ row, types, wide, onCreate, onClose }: {
  row: HTMLElement; types: GraphViewType[]; onCreate(type: GraphViewType): void; onClose(): void;
  /** Five to a row, as a wiki's ten layouts are (Notion's, seen 2026-11-11: 484 wide). */
  wide?: boolean;
}) {
  const labels = databaseViewLabels(), ref = useRef<HTMLDivElement>(null);
  const [place, setPlace] = useState<{ left: number; top: number } | null>(null);
  useLayoutEffect(() => {
    const menu = (row.closest<HTMLElement>("[data-view-overflow-menu]") ?? row).getBoundingClientRect(), at = row.getBoundingClientRect();
    const own = ref.current?.getBoundingClientRect(), width = own?.width ?? 0, height = own?.height ?? 0;
    if (touchScreen()) {
      setPlace({
        left: Math.max(0, Math.min(at.left + at.width / 2 - width / 2, window.innerWidth - width)),
        top: Math.max(POPOVER_MARGIN, Math.min(at.bottom, window.innerHeight - height - POPOVER_MARGIN)),
      });
      return;
    }
    setPlace({
      left: menu.right - 4 + width <= window.innerWidth - POPOVER_MARGIN ? menu.right - 4 : Math.max(POPOVER_MARGIN, menu.left + 4 - width),
      top: Math.max(POPOVER_MARGIN, Math.min(at.top + at.height / 2 - height / 2, window.innerHeight - height - POPOVER_MARGIN)),
    });
  }, [row]);
  return <div ref={ref} className={`${styles.viewMenu} ${styles.titleAddViewMenuPanel}`} data-view-add-beside data-wide={wide ? "true" : undefined}
    style={{ position: "fixed", left: place?.left ?? -9999, top: place?.top ?? 0 }}
    onKeyDown={(event: KeyboardEvent<HTMLDivElement>) => {
      if (event.key !== "Escape") return;
      event.preventDefault(); event.stopPropagation(); onClose();
    }}>
    <DatabaseAddViewChoices types={types} heading={labels.addNewView} className={styles.titleAddViewGrid}
      itemClassName={styles.inlineAddViewType} open onCreate={onCreate} />
  </div>;
}

/** A listed view's actions from its "⋯", over the list that stays open, as
 * Notion's (seen 2026-11-05): placed where the tab's own menu would be at
 * the "⋯" (`gap` under it, 4 unless said), and closing alone on Escape or a
 * press elsewhere in the list. */
export function DatabaseViewActionsBeside({ anchor, onClose, children, gap = 4 }: {
  anchor: HTMLElement; onClose(): void; children(open: boolean): ReactNode; gap?: number;
}) {
  const ref = useRef<HTMLDivElement>(null), opener = useRef<HTMLElement | null>(anchor);
  const place = usePopoverPlacement(ref, (size, viewport) => placeAround(popoverRectOf(anchor)!, size, viewport, { gap }), [anchor], undefined,
    { kind: "menu", onEscape: onClose, onOutsidePress: onClose, returnFocusRef: opener, inside: () => [anchor] });
  return <div ref={ref} className={`${styles.viewMenu} ${styles.viewTabMenu}`} data-view-tab-menu="true"
    style={{ position: "fixed", ...popoverPlacementStyle(place) }}>{children(Boolean(place))}</div>;
}

export type DatabaseViewAction = {
  id: string; label: string; icon?: ReactNode; disabled?: boolean; danger?: boolean;
  /** Why a dim row can't be chosen, in a tip before it, as Notion's "보기 복제" at its views' limit (seen 2026-11-12). */
  reason?: string;
  /** A line goes before it, starting a group of the menu. */
  grouped?: boolean;
  /** What it names now, in gray after its label; another database's source after "↗", as Notion's (seen 2026-11-04). */
  meta?: string; metaLinked?: boolean;
  /** Its choices open beside the menu (›); `open` while they show. */
  next?: boolean; open?: boolean;
  act(): void;
};
/** A view's actions, under its name to change when the menu names it. */
export function DatabaseViewActionChoices({ actions, name, onName, onRename, open, label }: {
  actions: DatabaseViewAction[]; name?: string; onName?(value: string): void; onRename?(): void;
  open: boolean; label: string;
}) {
  const ref = useRef<HTMLDivElement>(null), dialogRef = useRef<HTMLDivElement>(null), entered = useRef(false), labels = databaseViewLabels();
  const { dialogProps } = useDialog({ "aria-label": label }, dialogRef);
  const reason = useHoverTip({ delay: 180, placement: "left" });
  // Opened by the pointer, Notion's view menu lights no row until one is
  // hovered (seen 2026-11-05); from the keyboard its first row is lit. The
  // menu's own opening does both; a name field instead takes focus first.
  const menu = useNativeMenu({ items: actions, open, menuRef: ref, label, focusOnOpen: !onName, quietOnPointer: true,
    keyOf: action => action.id, textOf: action => action.label, disabledKeys: actions.filter(a => a.disabled).map(a => a.id),
    onAction: action => action.act() });
  useEffect(() => {
    if (!open || !onName || entered.current) return;
    entered.current = true; createFocusManager(dialogRef).focusFirst({ tabbable: true });
  }, [open, onName, dialogRef]);
  // → on a row whose choices open beside the menu opens them.
  const openBeside = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key !== inlineEndKey(event.currentTarget)) return;
    const id = dialogRef.current?.querySelector('[data-view-action][data-focused="true"]')?.getAttribute("data-view-action");
    const action = actions.find(candidate => candidate.id === id && candidate.next && !candidate.disabled);
    if (!action) return;
    event.preventDefault(); event.stopPropagation(); action.act();
  };
  return <div {...dialogProps} ref={dialogRef} onKeyDownCapture={openBeside}>
    {onName && <form onSubmit={event => { event.preventDefault(); onRename?.(); }}>
      <input value={name ?? ""} aria-label={labels.viewName} placeholder={labels.viewName} onChange={event => onName(event.currentTarget.value)}
        onFocus={() => menu.state.selectionManager.setFocused(false)} onKeyDown={event => {
          if (event.key === "ArrowDown" && !imeComposing(event)) {
            event.preventDefault(); createFocusManager(ref).focusFirst();
          }
        }} />
    </form>}
    <div {...menu.menuProps} ref={ref}>{actions.map(action => <Fragment key={action.id}>
      {action.grouped && <div className={styles.viewMenuSeparator} role="separator" />}
      <NativeMenuItem state={menu.state} itemKey={action.id} disabled={action.disabled}
        onPointerEnter={action.disabled && action.reason ? (event) => reason.enter(event.currentTarget, action.reason!) : undefined}
        onPointerLeave={action.disabled && action.reason ? reason.leave : undefined}
        className={`${styles.viewMenuItem} ${action.danger ? styles.viewMenuDanger : ""}`} data-view-action={action.id}
        aria-haspopup={action.next ? "menu" : undefined} aria-expanded={action.next ? Boolean(action.open) : undefined}>
        {() => <>{action.icon}<span>{action.label}</span>
          {action.meta ? <span className={styles.viewMenuMeta}>
            {action.metaLinked ? <ArrowUpRightIcon size="page" className={styles.viewMenuMetaArrow} aria-hidden="true" /> : null}{action.meta}
          </span> : null}
          {action.next ? <ChevronRight size="small" className={styles.viewMenuNext} aria-hidden="true" /> : null}</>}
      </NativeMenuItem>
    </Fragment>)}</div>
    {reason.tip}
  </div>;
}

/**
 * "다음과 같이 표시 ›" beside a view tab's menu, as Notion's (seen 2026-10-11):
 * the tabs as text and icon, text only or icon only, the shown one checked,
 * and that it applies to the person alone. Opened by the pointer it lights no
 * row, the first ↓ lighting the first and ↑ the last; from the keys its first
 * row takes them. ← or Escape goes back to the row, lit with no ring, as the
 * column menu's side (TablePropertyMenu closeSide). Notion's, measured in
 * Chrome with no script before the action, the screen after it and one key
 * (2026-11-15: worker w1135 and w1134, and w1249 in another tab at another
 * time): pressed open, no row lit and ↓ ringing "텍스트 및 아이콘"; Escape
 * leaving "다음과 같이 표시" lit with no ring, a letter landing nowhere.
 */
export function DatabaseViewTabDisplayChoices({ row, value, onChoose, onClose }: {
  row: HTMLElement; value: ViewTabDisplay; onChoose(value: ViewTabDisplay): void; onClose(): void;
}) {
  const labels = databaseViewLabels(), ref = useRef<HTMLDivElement>(null);
  const [place, setPlace] = useState<{ left: number; top: number } | null>(null);
  useLayoutEffect(() => {
    const menu = (row.closest<HTMLElement>("[data-view-tab-menu]") ?? row).getBoundingClientRect(), at = row.getBoundingClientRect();
    const own = ref.current?.getBoundingClientRect(), width = own?.width ?? 0, height = own?.height ?? 0;
    // 4px into the menu and centred on its row (Notion's, seen 2026-11-06).
    setPlace({
      left: menu.right - 4 + width <= window.innerWidth - POPOVER_MARGIN ? menu.right - 4 : Math.max(POPOVER_MARGIN, menu.left + 4 - width),
      top: Math.max(POPOVER_MARGIN, Math.min(at.top + at.height / 2 - height / 2, window.innerHeight - height - POPOVER_MARGIN)),
    });
  }, [row]);
  useEffect(() => {
    if (row.dataset.litBy === "keys") ref.current?.querySelector<HTMLElement>("[role=menuitemradio]")?.focus();
    else ref.current?.focus({ preventScroll: true });
  }, [row]);
  const move = (event: KeyboardEvent<HTMLDivElement>) => {
    const items = [...(ref.current?.querySelectorAll<HTMLElement>("[role=menuitemradio]") ?? [])];
    const at = items.indexOf(document.activeElement as HTMLElement), down = event.key === "ArrowDown";
    if (down || event.key === "ArrowUp") {
      event.preventDefault();
      items[at < 0 ? (down ? 0 : items.length - 1) : (at + (down ? 1 : items.length - 1)) % items.length]?.focus();
    } else if (event.key === inlineStartKey(event.currentTarget) || event.key === "Escape") {
      event.preventDefault(); event.stopPropagation(); onClose();
      const menu = row.closest<HTMLElement>("[data-native-menu]");
      if (menu && row.dataset.key) menu.dataset.searchLit = row.dataset.key;
      row.focus();
    }
  };
  return <div ref={ref} role="menu" tabIndex={-1} aria-label={labels.showTabsAs} className={styles.viewTabDisplayMenu} data-view-tab-display-menu
    style={{ position: "fixed", left: place?.left ?? -9999, top: place?.top ?? 0 }} onKeyDown={move}>
    {VIEW_TAB_DISPLAYS.map(display => <button key={display} type="button" role="menuitemradio" aria-checked={value === display}
      className={styles.viewMenuItem} data-view-tab-display={display} onClick={() => onChoose(display)}>
      <span>{labels.tabDisplay(display)}</span>{value === display ? <CheckIcon size="small" aria-hidden="true" /> : null}
    </button>)}
    <div className={styles.viewMenuSeparator} role="separator" />
    <div className={styles.viewTabDisplayNote}>{labels.tabDisplayOnlyYou}</div>
  </div>;
}
