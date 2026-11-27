import { useEffect, useRef, useState } from "react";
import { usePageLayoutTabFilter } from "./pageLayoutTabEnvironment";
import { useFilterThisPage } from "./filterThisPage";
import type { GraphProperty, GraphView } from "@/contentGraph";
import { CountChevronDown, FilterIcon, Plus, SortIcon, SplitChevronDown, StraightArrowDown, StraightArrowUp, X } from "@/icons/leafen";
import type { DbProperty, FilterGroup, ViewFilter } from "@/lib/types";
import { usePageNames } from "@/lib/pageNames";
import { QuickFilterEditor } from "./DatabaseQuickFilterEditor";
import { QuickFilterSheet } from "./DatabaseQuickFilterSheet";
import { PhoneSheet } from "../shell/PhoneSheet";
import { dateFilterSummary, isDateFilterProperty } from "./DateFilterEditor";
import { defaultOperator, defaultValue, effectiveFilterOperator, filterOperatorLabel, filterOperatorSign, filterPropertyForComparison, withRollupFilterTargets } from "./databaseFilterPolicy";
import { PropertyIcon } from "./PropertyIcon";
import { DatabaseSortSettings, viewSorts } from "./DatabaseSortSettings";
import { advancedFilterParts, advancedFilterPickerRow } from "./DatabaseAdvancedFilter";
import { AdvancedFilterWindow } from "./DatabaseAdvancedFilterEditor";
import { advancedPillPress, keepFocusBehindWindow } from "./useFilterWindowKeys";
import type { DatabaseFilterSnapshot } from "./databaseFilterSnapshot";
import type { DatabaseFilterBarRequest } from "./databaseFilterBar";
import { NO_VALUE_FILTERS } from "./databaseFilterLabels";
import { filterValueText as valueText } from "./databaseFilterValueText";
import { databaseViewLabels } from "./databaseViewLabels";
import { i18next } from "@/i18n";
import { propertyPickerOrder, toolbarListFit, viewWithQuickFilterInAdvanced } from "./databaseToolbarModel";
import { filterAwaitsValue } from "@/shared/database/queryFilterPolicy";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { keyCameLast, keysLedToLastPress } from "@/lib/lastPress";
import { giveFocusBack } from "@/lib/focusGiveBack";
import { PopoverLayer } from "@/lib/popoverLayer";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { nativeBoardQueryBounds } from "./boardViewModel";
import { queryConfigOverBounds } from "./queryBudget";
import { sameViewQuery } from "./databasePersonalQuery";
import { FilterBarPill, QuickFilterAddPicker } from "./QuickFilterAddPicker";
import styles from "./DatabaseQuickFilterBar.module.css";

// Quick filters are the view's filter-bar pills: one condition per property,
// kept apart from the advanced filter and applied together with it. Older
// imported entries that hold a whole group still apply and show as one pill
// that can only be removed. While the viewer applies filters or sorts of
// their own, the bar offers to reset them and, to an editor, to save them
// for everyone.

type QuickFilter = ViewFilter | FilterGroup;

const NO_RECT = { top: 0, bottom: 0, left: 0, right: 0 };

function isSingleFilter(filter: QuickFilter): filter is ViewFilter {
  return typeof (filter as ViewFilter).propertyId === "string";
}

function groupSize(group: FilterGroup): number {
  return group.filters.length + (group.groups ?? []).reduce((total, child) => total + groupSize(child), 0);
}

/**
 * What a pill says after its property's name, as Notion's (seen
 * 2026-10-01): a number's comparison as its sign ("Count = 3"); anything
 * else after a colon — the value alone for "값과 동일한 데이터" ("Stage:
 * Early,Late"), "선택 값과 동일하지 않음" and the value for its opposite, else
 * the operator and the value ("Notes: 시작 값 fi") or the operator alone
 * ("Count: 비어 있음").
 */
export function quickFilterSummary(filter: ViewFilter, property: DbProperty, properties: readonly DbProperty[] = [],
  pageName?: (id: string) => string, thisPageName?: string): string {
  property = filterPropertyForComparison(property, properties);
  if (isDateFilterProperty(property)) {
    const summary = dateFilterSummary(filter, property);
    return summary ? `: ${summary}` : "";
  }
  const operator = effectiveFilterOperator(property, filter.operator);
  const label = filterOperatorLabel(operator, property);
  const value = NO_VALUE_FILTERS.has(operator) || operator === "relative_to_today" ? "" : valueText(filter.value, property, pageName, thisPageName);
  if (!value) return `: ${label}`;
  const sign = filterOperatorSign(operator, property);
  if (sign) return ` ${sign} ${value}`;
  if (operator === "equals") return `: ${value}`;
  if (operator === "does_not_equal") return `: ${databaseViewLabels().filterNotEqualTo} ${value}`;
  return `: ${label} ${value}`;
}

export function databaseToolbarProperties(properties: readonly GraphProperty[]): DbProperty[] {
  return properties.map((property, position) => ({
    id: property.id,
    databaseId: property.dataSourceId,
    name: property.name,
    position,
    type: property.type,
    config: property.config,
  }) as DbProperty);
}

/**
 * One quick filter's pill and, while open, its editor under it: the bar's
 * pills and the view settings' filter list draw it alike, as Notion's (seen
 * 2026-09-30). A filter still waiting for its value shows only its property.
 * The editor is a window of its own over the page, 4px under the pill's box
 * (a dashboard's is 26px, DashboardMultiSourceFilter.module.css) and at most
 * half the window tall to the fraction (361.5 in 723, seen 2026-11-22), as Notion's
 * over a widget's or the view settings' filter list (seen 2026-11-11): inside them
 * it scrolled them, their head out of sight; a bar that scrolls sideways cut it off.
 */
export function QuickFilterPill({ filter, properties, snapshot, writable, open, unsaved = false, onOpenChange, onChange, onRemove, onToAdvanced }: {
  filter: ViewFilter;
  /** The viewer's own, not yet the view's: an orange dot at its top right (Notion's, seen 2026-11-05). */
  unsaved?: boolean;
  properties: DbProperty[];
  snapshot: DatabaseFilterSnapshot;
  writable: boolean;
  open: boolean;
  onOpenChange(open: boolean): void;
  onChange(filter: ViewFilter): void;
  onRemove(): void;
  /** Moves it into the view's advanced filter, from its editor's "⋯". */
  onToAdvanced?(): void;
}) {
  const labels = databaseViewLabels(), pageNames = usePageNames();
  const { phone } = useSidebarLayout(true);
  const thisPage = useFilterThisPage();
  const wrapRef = useRef<HTMLSpanElement>(null);
  const pillRef = useRef<HTMLButtonElement>(null);
  const editorRef = useRef<HTMLDivElement>(null);
  // On a phone its sheet's own choices and menus rise over it and stay inside it.
  // On a desktop a press beside it goes on to what it pressed, as the bar's own (below).
  const layer = useOverlayLayer(open, { kind: "popover", ref: editorRef,
    inside: () => [wrapRef.current, ...(phone ? document.querySelectorAll("[data-phone-sheet], [data-phone-sheet-menu], [data-phone-sheet-shade]") : [])],
    onEscape: () => onOpenChange(false), onOutsidePress: () => onOpenChange(false), pressGoesOn: !phone });
  const place = usePopoverPlacement(editorRef, open && !phone ? (size, viewport) => placeAround(
    popoverRectOf(wrapRef.current) ?? NO_RECT, size, viewport, { gap: 4, align: "start", cap: viewport.height / 2 }) : null, [], undefined, false);
  useEffect(() => {
    if (!open) return;
    // As in Notion, the editor opens on its value field, a checkbox's states with the first lit, or else its first control
    // (a date's "시작일 ⌄"), ringed when keys opened it; a phone's sheet on itself. Measured 2026-11-15 after the press only (no script in the page before it), Notion in Chrome 1440:
    // a select filter added from the toolbar, "z" typed went into its option search (Leafen the same); 2026-11-17, a date's added with ↓ Enter or
    // its pill pressed, Enter opened "시작일 / 종료일" (the ring only by keys; the audit's 11-16 tab the same), a number's added by keys took "5".
    // A pill out of sight in a bar scrolled sideways comes into it first, as Notion's.
    pillRef.current?.scrollIntoView({ block: "nearest", inline: "nearest" });
    const editor = editorRef.current;
    (editor?.querySelector<HTMLElement>("input:not([type='hidden']), textarea, [data-quick-filter-states]") ?? (phone ? null : editor?.querySelector<HTMLElement>("button:not(:disabled)")) ?? editor)?.focus();
  }, [open, phone]);
  const property = properties.find(({ id }) => id === filter.propertyId);
  if (!property) return null;
  if (filter.locked) return (
    // The database's own rule, as Notion's Sprint board's current sprint (seen 2026-11-04): dimmed, opening nothing.
    <span className={styles.pill} data-locked-rule={filter.propertyId} aria-disabled="true">
      <PropertyIcon property={property} size="small" />
      <span>{property.name}{quickFilterSummary(filter, property, properties)}</span>
    </span>
  );
  return (
    <span ref={wrapRef} className={styles.pillWrap} data-quick-filter-box>
      <button ref={pillRef} type="button" className={styles.pill} data-quick-filter={filter.propertyId}
        data-awaits-value={filterAwaitsValue(filter) ? "true" : undefined}
        aria-haspopup="dialog" aria-expanded={open} disabled={!writable}
        aria-label={labels.editQuickFilter(property.name)}
        onClick={() => onOpenChange(!open)}>
        {/* Its property's mark first, as Notion's pills (seen 2026-11-04), the filter list's own drawn 1.2 times (2026-11-22). */}
        <PropertyIcon property={property} size="small" />
        {/* Its property's name in 500 and its value in 400, as Notion's (seen 2026-11-05). */}
        <span><span className={styles.pillName}>{property.name}</span>{filterAwaitsValue(filter) ? "" : quickFilterSummary(filter, property, properties, (id) => {
          const page = snapshot.pagesById[id];
          return page ? pageNames.title(page) : "";
        }, thisPage && (property.config?.relationDatabaseId ?? property.databaseId) === thisPage.dataSourceId ? thisPage.name : undefined)}</span>
        <CountChevronDown size="tiny" trim className={styles.pillChevron} aria-hidden="true" />
        {unsaved ? <span className={styles.unsaved} aria-hidden="true" data-unsaved /> : null}
      </button>
      {open && phone ? (
        // On a phone, a sheet over the filters' sheet, headed by its condition (Notion's, seen 2026-10-21).
        <QuickFilterSheet filter={filter} property={property} properties={properties} snapshot={snapshot} sheetRef={editorRef}
          zIndex={layer.zIndex} onKeyDown={layer.onKeyDown} onChange={(patch) => onChange({ ...filter, ...patch })}
          onRemove={onRemove} onToAdvanced={onToAdvanced} onClose={() => onOpenChange(false)} />
      ) : open ? (
        <PopoverLayer><div ref={editorRef} className={`${styles.popover} ${styles.placed}`} role="dialog" aria-label={labels.editQuickFilter(property.name)}
          data-quick-filter-editor data-quick-filter-dates={isDateFilterProperty(property) ? "" : undefined} tabIndex={-1} style={{ ...popoverPlacementStyle(place), zIndex: layer.zIndex }} onKeyDown={layer.onKeyDown}>
          <QuickFilterEditor filter={filter} property={property} properties={properties} snapshot={snapshot}
            onChange={(patch) => onChange({ ...filter, ...patch })}
            onRemove={onRemove} onToAdvanced={onToAdvanced} onClose={() => onOpenChange(false)} />
        </div></PopoverLayer>
      ) : null}
    </span>
  );
}

/**
 * The view's sorts as the bar's first pill, as Notion's (seen 2026-10-01): one
 * sort names its property after an arrow of its direction, more read "정렬
 * 2개"; pressing it opens the sorts' editor under it, placed as the toolbar's lists (toolbarListFit).
 */
function SortPill({ view, properties, writable, open, unsaved = false, zIndex, onOpenChange, onUpdateView }: {
  view: GraphView;
  /** The bar's layer, which the sorts' window is drawn in over the page. */
  zIndex?: number;
  unsaved?: boolean;
  properties: DbProperty[];
  writable: boolean;
  open: boolean;
  onOpenChange(open: boolean): void;
  onUpdateView(view: GraphView): void;
}) {
  const labels = databaseViewLabels();
  const { phone } = useSidebarLayout(true);
  const sorts = viewSorts(view);
  const only = sorts.length === 1 ? sorts[0] : undefined;
  const name = only ? properties.find(({ id }) => id === only.propertyId)?.name ?? "" : labels.sortCountPill(sorts.length);
  const Icon = only ? (only.direction === "desc" ? StraightArrowDown : StraightArrowUp) : SortIcon;
  const pillRef = useRef<HTMLButtonElement>(null), panelRef = useRef<HTMLDivElement>(null);
  const place = usePopoverPlacement(panelRef, open && !phone ? (size, viewport) => placeAround(
    popoverRectOf(pillRef.current) ?? NO_RECT, size, viewport, { gap: 4, align: "start", marginY: 8, ...toolbarListFit(viewport.height) }) : null, [], undefined, false);
  return (
    <span className={styles.pillWrap}>
      <button ref={pillRef} type="button" className={styles.pill} data-quick-sort aria-haspopup="dialog" aria-expanded={open}
        disabled={!writable} aria-label={labels.editSorts} onClick={() => onOpenChange(!open)}>
        <Icon size={only ? "tiny" : "small"} trim aria-hidden="true" />
        <span>{name}</span>
        <CountChevronDown size="tiny" trim className={styles.pillChevron} aria-hidden="true" />
        {unsaved ? <span className={styles.unsaved} aria-hidden="true" data-unsaved /> : null}
      </button>
      {open && phone ? (
        // On a phone, the sheet the toolbar's "정렬" opens (Notion's, seen 2026-10-21).
        <PhoneSheet title={labels.sorts} label={labels.editSorts} onClose={() => onOpenChange(false)} data={{ "data-quick-sort-editor": "" }}>
          <DatabaseSortSettings view={view} properties={properties} onUpdateView={onUpdateView} sheet />
        </PhoneSheet>
      ) : open ? (
        <PopoverLayer><div ref={panelRef} className={`${styles.popover} ${styles.placed} ${styles.sortPopover}`} role="dialog" aria-label={labels.editSorts}
          data-quick-sort-editor style={{ ...popoverPlacementStyle(place), zIndex }}>
          <DatabaseSortSettings view={view} properties={properties} onUpdateView={onUpdateView} />
        </div></PopoverLayer>
      ) : null}
    </span>
  );
}

/**
 * The view's advanced filter as a pill after the sorts, as Notion's (seen
 * 2026-10-04): "규칙 N개", which opens its rules' editor under it.
 */
function AdvancedFilterPill({ view, properties, snapshot, writable, open, unsaved = false, onOpenChange, onUpdateView }: {
  view: GraphView;
  unsaved?: boolean;
  properties: DbProperty[];
  snapshot: DatabaseFilterSnapshot;
  writable: boolean;
  open: boolean;
  onOpenChange(open: boolean): void;
  onUpdateView(view: GraphView): void;
}) {
  const labels = databaseViewLabels();
  const { phone } = useSidebarLayout(true);
  const advanced = advancedFilterParts({ view, properties, snapshot, onUpdateView });
  const pillRef = useRef<HTMLButtonElement>(null);
  if (advanced.count === 0) return null;
  return (
    <span className={styles.pillWrap}>
      <button ref={pillRef} type="button" className={styles.pill} data-quick-advanced-filter aria-haspopup="dialog" aria-expanded={open}
        disabled={!writable} {...advancedPillPress(open, () => onOpenChange(!open))}>
        <FilterIcon size="small" trim aria-hidden="true" />
        <span>{labels.advancedFilterPill(advanced.count)}</span>
        <CountChevronDown size="tiny" trim className={styles.pillChevron} aria-hidden="true" />
        {unsaved ? <span className={styles.unsaved} aria-hidden="true" data-unsaved /> : null}
      </button>
      {open && phone ? (
        // On a phone, the "고급 필터" sheet (Notion's, seen 2026-10-21); "필터 제거" closes it with the pill.
        <PhoneSheet title={labels.sheetAdvancedFilter} label={labels.advancedFilterPill(advanced.count)} onClose={() => onOpenChange(false)}
          data={{ "data-quick-advanced-filter-editor": "" }}>
          {advanced.sheetEditor(() => onOpenChange(false))}
        </PhoneSheet>
      ) : open ? (
        <AdvancedFilterWindow anchor={pillRef} label={labels.advancedFilterPill(advanced.count)} onClose={() => onOpenChange(false)}>
          {advanced.editor}
        </AdvancedFilterWindow>
      ) : null}
    </span>
  );
}

export function DatabaseQuickFilterBar({
  view,
  properties,
  snapshot,
  writable,
  ownQuery,
  hidden = false,
  kept = false,
  onPopulated,
  request,
  onRequestDone,
  onUpdateView,
  advanced = true,
  advancedRequest = 0,
  sortRequest = 0,
  addLabel,
  toolbarSheet = false,
}: {
  view: GraphView;
  properties: readonly GraphProperty[];
  snapshot: DatabaseFilterSnapshot;
  writable: boolean;
  /** The viewer's own filters and sorts, not the view's: they reset them, and an editor saves them for everyone. */
  ownQuery?: { saved?: GraphView; onReset(): void; onSaveForEveryone?(): void; onSaveAsNewView?(): void } | null;
  /** Whether the viewer hid the bar with the toolbar's "필터". */
  hidden?: boolean;
  /** Left open with "+ 필터" after its filters were cleared (databaseFilterBar). */
  kept?: boolean;
  /** It shows filters or sorts: from now it stays open when they are cleared. */
  onPopulated?(): void;
  /** A column menu's "filter": that property's pill, added when the bar has none, opens. */
  request?: DatabaseFilterBarRequest | null;
  onRequestDone?(): void;
  onUpdateView(view: GraphView): void;
  /** Whether a pill can move to the advanced filter; the library has none. */
  advanced?: boolean;
  /** A filter the view's sub-item option sets, shown first and changed only there. */
  /** Opens the advanced filter's rules each time it changes, as an empty view's "필터 편집하기" or the toolbar's "고급 필터 추가" does. */
  advancedRequest?: number | { at: number; from?: HTMLElement | null };
  /** Opens the sorts' editor each time it changes, as a sort picked from the toolbar does. */
  sortRequest?: number | { at: number; from?: HTMLElement | null };
  /** "+ 필터" where the bar's own "필터 추가" is shorter, as in a group's preview. */
  addLabel?: string;
  /** On a phone the view's filters and sorts are its toolbar sheet's alone, as Notion's has no bar (seen 2026-10-24). */
  toolbarSheet?: boolean;
}) {
  const labels = databaseViewLabels();
  const { phone } = useSidebarLayout(true);
  const quick = (Array.isArray(view.config.quickFilters) ? view.config.quickFilters : []) as QuickFilter[];
  const toolbarProperties = databaseToolbarProperties(properties);
  const filterProperties = withRollupFilterTargets(toolbarProperties, snapshot.propertiesByDatabase);
  const [open, setOpen] = useState<number | "add" | "sort" | "advanced" | "save" | null>(null);
  const layoutTabFilter = usePageLayoutTabFilter();
  const tabFilter = layoutTabFilter?.viewId === view.id ? layoutTabFilter : null;
  const barRef = useRef<HTMLDivElement>(null);
  const requested = useRef<number | null>(null);
  const openedFrom = useRef<{ open: number | "sort" | "advanced"; from: HTMLElement | null } | null>(null);
  const commit = (next: QuickFilter[]) => onUpdateView({
    ...view, config: { ...view.config, quickFilters: next.length ? next : undefined },
  });

  useEffect(() => {
    if (!request) return;
    const index = quick.findIndex((filter) => isSingleFilter(filter) && filter.propertyId === request.propertyId);
    if (index >= 0) {
      setOpen(index);
      openedFrom.current = { open: index, from: request.from ?? null };
      onRequestDone?.();
      return;
    }
    if (requested.current === request.at) return;
    requested.current = request.at;
    const property = filterProperties.find(({ id }) => id === request.propertyId);
    if (writable && property) {
      commit([...quick, { propertyId: property.id, operator: defaultOperator(property, filterProperties), value: defaultValue(property, filterProperties) }]);
    } else onRequestDone?.();
  });

  const openRequested = (open: "advanced" | "sort", request: typeof sortRequest) => {
    if (!request) return; setOpen(open); openedFrom.current = { open, from: typeof request === "object" ? request.from ?? null : null };
    if (open === "advanced") keepFocusBehindWindow(openedFrom.current.from);
  };
  useEffect(() => openRequested("advanced", advancedRequest), [advancedRequest]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => openRequested("sort", sortRequest), [sortRequest]); // eslint-disable-line react-hooks/exhaustive-deps
  // What the toolbar's "필터" or "정렬" opened here (a pill's editor, the sorts) gives the focus back to that button as it closes, where the focus has no place
  // left, as Notion's (2026-11-15 and -24, Chrome, no script before the action, judged by the screen and one typed letter: "필터" › a property, then Escape on its
  // values, or "zzz" typed and a press in the view's empty room — "필터" ringed, the letter landing nowhere; nothing typed, no ring). The page or view holding the bar,
  // focused by the press, is no place. Its advanced filter's window gives none (2026-11-24, two tabs: after "zzz" that press left the focus on nothing; one under the view, a new line's caret).
  useEffect(() => {
    const opened = openedFrom.current;
    if (!opened || open === opened.open) return;
    openedFrom.current = null; const from = opened.from;
    if (open !== null || !from || opened.open === "advanced") return;
    return giveFocusBack({ to: from, keepScroll: true, unlessOpened: true, ring: () => keyCameLast() || keysLedToLastPress(), when: () => {
      const active = document.activeElement, bar = barRef.current, holder = !!active && active !== from && !!bar && active.contains(bar);
      return !active || active === document.body || !active.isConnected || holder;
    } });
  }, [open]);

  // An open pill's editor or the sorts is a layer over the page: Escape or a
  // press outside the bar closes it, and on a desktop the press goes on to
  // what it pressed, as Notion's bar windows have no shade (seen 2026-11-12:
  // with a pill's editor, "+ 필터" or the advanced filter open, the view's
  // settings button opened its settings and a cell its editor at one press,
  // the page under them answering hover). A select's choices are a layer of
  // their own over it.
  const barLayer = useOverlayLayer(open !== null, { kind: "popover", ref: barRef, restoreFocus: false, onEscape: () => setOpen(null), onOutsidePress: () => setOpen(null), pressGoesOn: !phone,
    // On a phone a pill's editor is a sheet at the page's end, still the bar's, as are the sheets it opens;
    // the advanced filter's window is over the page, still the bar's.
    inside: () => [...document.querySelectorAll("[data-phone-sheet], [data-phone-sheet-menu], [data-phone-sheet-shade], [data-quick-advanced-filter-editor]")] });

  const usedProperties = new Set(quick.filter(isSingleFilter).map(({ propertyId }) => propertyId));
  const addable = propertyPickerOrder(filterProperties.filter(({ id }) => !usedProperties.has(id)));
  // The add menu holds the keys while open; Escape returns to its button.
  const addButtonRef = useRef<HTMLButtonElement>(null), addMenuRef = useRef<HTMLDivElement>(null);
  // Its "고급 필터 추가" or "고급 필터" hands its place to the advanced filter's
  // window, the focus left on nothing, as Notion's (seen 2026-11-12).
  const toAdvanced = useRef(false);
  const addLayer = useOverlayLayer(open === "add", { kind: "popover", ref: addMenuRef, onEscape: () => setOpen(null), onOutsidePress: () => setOpen(null), pressGoesOn: !phone,
    inside: () => [addButtonRef.current], returnFocusRef: addButtonRef, shouldRestoreFocus: () => !toAdvanced.current });
  const addPlace = usePopoverPlacement(addMenuRef, open === "add" ? (size, viewport) => placeAround(
    popoverRectOf(addButtonRef.current) ?? NO_RECT, size, viewport, { gap: 4, align: "start", margin: 8, ...toolbarListFit(viewport.height) }) : null, [], undefined, false);
  const addQuick = (property: DbProperty) => {
    commit([...quick, { propertyId: property.id, operator: defaultOperator(property, filterProperties), value: defaultValue(property, filterProperties) }]);
    setOpen(quick.length);
  };
  // "⌄" beside "모두에게 저장" holds "새 보기로 저장", as Notion's (seen 2026-10-24).
  const saveButtonRef = useRef<HTMLButtonElement>(null), saveMenuRef = useRef<HTMLDivElement>(null);
  useOverlayLayer(open === "save", { kind: "popover", ref: saveMenuRef, onEscape: () => setOpen(null), onOutsidePress: () => setOpen(null), pressGoesOn: !phone,
    inside: () => [saveButtonRef.current], returnFocusRef: saveButtonRef });
  const saveMenu = useNativeMenu({ items: ownQuery?.onSaveAsNewView ? ["new"] : [], open: open === "save", menuRef: saveMenuRef,
    label: labels.saveOptionsMore, keyOf: (key) => key, textOf: () => labels.saveAsNewView,
    onAction: () => { setOpen(null); ownQuery?.onSaveAsNewView?.(); } });
  const sorts = viewSorts(view);
  const advancedParts = advanced ? advancedFilterParts({ view, properties: filterProperties, snapshot, onUpdateView }) : null;
  const advancedCount = advancedParts?.count ?? 0;
  const populated = quick.length > 0 || sorts.length > 0 || advancedCount > 0 || Boolean(ownQuery) || Boolean(tabFilter);
  const shownPopulated = populated && !hidden;
  useEffect(() => { if (shownPopulated) onPopulated?.(); }, [shownPopulated, onPopulated]);
  if (hidden || (toolbarSheet && phone) || (!populated && !kept)) return null;
  const overBounds = queryConfigOverBounds(view.config, nativeBoardQueryBounds);
  // What the viewer changed from the view's own carries Notion's orange dot (seen 2026-11-05).
  const saved = ownQuery?.saved;
  const savedQuick = (saved && Array.isArray(saved.config.quickFilters) ? saved.config.quickFilters : []) as QuickFilter[];
  const filterUnsaved = (filter: ViewFilter) => Boolean(saved) && !sameViewQuery({ quickFilters: [filter] },
    { quickFilters: savedQuick.filter((kept) => isSingleFilter(kept) && kept.propertyId === filter.propertyId) });
  const sortsUnsaved = Boolean(saved) && !sameViewQuery({ sorts: view.config.sorts }, { sorts: saved?.config.sorts });
  const advancedUnsaved = Boolean(saved) && !sameViewQuery(
    { filterGroup: view.config.filterGroup, filters: view.config.filters, filterConjunction: view.config.filterConjunction },
    { filterGroup: saved?.config.filterGroup, filters: saved?.config.filters, filterConjunction: saved?.config.filterConjunction });

  return (
    <div ref={barRef} className={styles.bar} role="toolbar" aria-label={labels.quickFilters} data-quick-filter-bar data-dashboard-filter-bar={view.type === "dashboard" ? "" : undefined}>
      {/* One line that scrolls sideways past its end, "초기화 · 모두에게 저장" kept after it (Notion's, seen 2026-11-12). */}
      <div className={styles.pills} data-quick-filter-pills>
      {sorts.length > 0 && (
        <>
          <SortPill view={view} properties={toolbarProperties} writable={writable} open={open === "sort"} unsaved={sortsUnsaved} zIndex={barLayer.zIndex}
            onOpenChange={(next) => setOpen(next ? "sort" : null)} onUpdateView={onUpdateView} />
          <span className={styles.divider} aria-hidden="true" />
        </>
      )}
      {advancedCount > 0 && (
        <AdvancedFilterPill view={view} properties={filterProperties} snapshot={snapshot} writable={writable}
          open={open === "advanced"} unsaved={advancedUnsaved} onOpenChange={(next) => setOpen(next ? "advanced" : null)} onUpdateView={onUpdateView} />
      )}
      {tabFilter ? (
        // A one-way relation tab's rows, set by the page's layout (PageLayoutTabFilterContext).
        <span className={styles.pill} data-layout-tab-filter aria-disabled="true"><span>{tabFilter.label}</span></span>
      ) : null}
      {quick.map((filter, index) => {
        if (!isSingleFilter(filter)) {
          const count = groupSize(filter);
          return (
            <span key={`group:${index}`} className={styles.pill} data-quick-filter-group={index}>
              <span>{labels.quickFilterGroup(count)}</span>
              {writable && (
                <button type="button" aria-label={labels.removeQuickFilter(labels.quickFilterGroup(count))}
                  onClick={() => commit(quick.filter((_item, current) => current !== index))}>
                  <X size="tiny" />
                </button>
              )}
            </span>
          );
        }
        return (
          <FilterBarPill Pill={QuickFilterPill} key={filter.propertyId} filter={filter} properties={filterProperties}
            snapshot={snapshot} writable={writable} open={open === index} unsaved={filterUnsaved(filter)} onOpenChange={(next) => setOpen(next ? index : null)}
            onChange={(next) => commit(quick.map((item, current) => current === index ? next : item))}
            onRemove={() => {
              setOpen(null);
              commit(quick.filter((_item, current) => current !== index));
            }}
            onToAdvanced={advanced ? () => {
              // Its rule's pill opens with the rule's editor, as Notion's.
              onUpdateView(viewWithQuickFilterInAdvanced(view, index));
              setOpen("advanced");
            } : undefined} />
        );
      })}
      {/* Kept with every property filtered, its list then "결과 없음" (Notion's, seen 2026-11-11). */}
      {writable && (
        <span className={styles.pillWrap}>
          <button ref={addButtonRef} type="button" className={styles.add} aria-haspopup="menu" aria-expanded={open === "add"}
            data-quick-filter-add onClick={() => { toAdvanced.current = false; setOpen(open === "add" ? null : "add"); }}>
            <Plus size="tiny" trim /><span>{addLabel ?? labels.addQuickFilter}</span>
          </button>
          {/* The toolbar's "필터" picker, as Notion's "+ 필터" (seen 2026-11-05): a focused
              "필터 기준" search, the properties, and "고급 필터 추가" (QuickFilterAddPicker). */}
          {open === "add" && (
            <PopoverLayer><div ref={addMenuRef} className={`${styles.popover} ${styles.placed} ${styles.addPicker}`} role="dialog" aria-label={labels.addQuickFilter} data-property-picker-window=""
              style={{ ...popoverPlacementStyle(addPlace), zIndex: addLayer.zIndex }}>
              <QuickFilterAddPicker properties={addable}
                footer={advancedParts ? advancedFilterPickerRow(advancedCount, {
                  onAdd: () => { toAdvanced.current = true; advancedParts.add(); setOpen("advanced"); },
                  onOpen: () => { toAdvanced.current = true; setOpen("advanced"); },
                }) : undefined}
                onPick={(picked) => { const property = addable.find(({ id }) => id === picked.id); if (property) addQuick(property); }} />
            </div></PopoverLayer>
          )}
        </span>
      )}
      {overBounds.filters && (
        <span className={styles.overBound} role="status" data-filters-over-bound>
          {i18next.t("databaseView:filtersOverBound", { max: nativeBoardQueryBounds.maxFilters })}
        </span>
      )}
      {overBounds.groups && (
        <span className={styles.overBound} role="status" data-filter-groups-over-bound>
          {i18next.t("databaseView:filterGroupsOverBound")}
        </span>
      )}
      {overBounds.sorts && (
        <span className={styles.overBound} role="status" data-sorts-over-bound>
          {i18next.t("databaseView:sortsOverBound", { max: nativeBoardQueryBounds.maxSorts })}
        </span>
      )}
      </div>
      {ownQuery && (
        <span className={styles.ownQuery} data-own-view-query title={labels.ownViewQueryHint}>
          <button type="button" className={styles.reset} data-own-view-query-reset onClick={ownQuery.onReset}>
            {labels.resetOwnViewQuery}
          </button>
          {ownQuery.onSaveForEveryone && (
            <span className={styles.pillWrap}>
              <button type="button" className={styles.saveForEveryone} data-save-view-for-everyone
                onClick={ownQuery.onSaveForEveryone}>
                {labels.saveViewForEveryone}
              </button>
              {ownQuery.onSaveAsNewView && (
                <button ref={saveButtonRef} type="button" className={styles.saveForEveryone} aria-label={labels.saveOptionsMore}
                  aria-haspopup="menu" aria-expanded={open === "save"} data-save-view-options onClick={() => setOpen(open === "save" ? null : "save")}>
                  <SplitChevronDown size="small" aria-hidden="true" />
                </button>
              )}
              {open === "save" && (
                <div {...saveMenu.menuProps} ref={saveMenuRef} className={`${styles.popover} ${styles.saveMenu}`}>
                  <NativeMenuItem state={saveMenu.state} itemKey="new" className={styles.menuItem} data-save-as-new-view>
                    {({ labelProps }) => <span {...labelProps}>{labels.saveAsNewView}</span>}
                  </NativeMenuItem>
                </div>
              )}
            </span>
          )}
        </span>
      )}
    </div>
  );
}
