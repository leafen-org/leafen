import { useRef, useState, type ReactNode } from "react";
import type { GraphView } from "@/contentGraph";
import { ChevronDown, DragHandleIcon, FilterIcon, Plus, ResetIcon } from "@/icons/leafen";
import type { DbProperty, FilterGroup, ViewFilter } from "@/lib/types";
import { DatabasePropertyPicker } from "./DatabasePropertyPicker";
import { DashboardFilterPropertyPicker } from "./DashboardFilterPropertyPicker";
import { groupedSources, useDashboardFilterSources } from "./dashboardFilterSources";
import { QuickFilterPill } from "./DatabaseQuickFilterBar";
import { defaultOperator, defaultValue } from "./databaseFilterPolicy";
import type { DatabaseFilterSnapshot } from "./databaseFilterSnapshot";
import { databaseViewLabels } from "./databaseViewLabels";
import { viewWithQuickFilterInAdvanced, viewWithQuickFiltersInAdvanced } from "./databaseToolbarModel";
import { DatabaseOwnQueryRows } from "./DatabaseOwnQueryRows";
import { AdvancedFilterWindow } from "./DatabaseAdvancedFilterEditor";
import { advancedFilterPickerRow } from "./DatabaseAdvancedFilter";
import type { PhoneOwnQuery } from "./DatabasePhoneFilterList";
import { useDragReorder } from "@/lib/useDragReorder";
import { sameViewQuery } from "./databasePersonalQuery";
import { useSettingsScreenHead } from "./settingsScreenHead";
import styles from "./DatabaseFilterSettings.module.css";
import pillStyles from "./DatabaseQuickFilterBar.module.css";

type QuickFilter = ViewFilter | FilterGroup;

const FILTER_DRAG = "application/x-leafen-view-filter";

function isSingleFilter(filter: QuickFilter): filter is ViewFilter {
  return typeof (filter as ViewFilter).propertyId === "string";
}

function viewQuickFilters(view: GraphView): QuickFilter[] {
  return (Array.isArray(view.config.quickFilters) ? view.config.quickFilters : []) as QuickFilter[];
}

function quickFilterKey(filter: QuickFilter, index: number) {
  return isSingleFilter(filter) ? filter.propertyId : `group:${index}`;
}

type FilterSettingsProps = {
  view: GraphView;
  properties: DbProperty[];
  snapshot: DatabaseFilterSnapshot;
  writable: boolean;
  /** The rules of the view's advanced filter. */
  advancedCount: number;
  /** The advanced filter's editor. */
  advancedEditor: ReactNode;
  /** Starts the advanced filter with one rule when it has none. */
  onAddAdvanced(): void;
  onUpdateView(view: GraphView): void;
  /** A locked database's "필터 초기화", while the viewer's own filters and sorts apply. */
  onReset?(): void;
  /** The viewer's own filters and sorts, whose filters the screen saves or resets while they differ from the view's. */
  ownQuery?: PhoneOwnQuery | null;
};

const ADVANCED_KEYS = ["filterGroup", "filters", "filterConjunction"] as const;

/** How many of the view's filters (each pill, the advanced filter as one) differ from those it saves. */
function changedFilterCount(view: GraphView, saved: GraphView): number {
  const pills = (config: GraphView["config"]) => new Map<string, string>([
    ...viewQuickFilters({ config } as GraphView).map((filter, index) => [quickFilterKey(filter, index), JSON.stringify(filter)] as [string, string]),
    ["advanced", JSON.stringify(ADVANCED_KEYS.map((key) => config[key] ?? null))],
  ]);
  const seen = pills(view.config), kept = pills(saved.config);
  return [...new Set([...seen.keys(), ...kept.keys()])].filter((key) => seen.get(key) !== kept.get(key)).length;
}

/**
 * The view settings' filter screen, as Notion's (seen 2026-09-30): with no
 * filters it is the "필터 기준" picker; with filters, each is a row of drag
 * handle and pill (whose editor opens under it), then "필터 추가". Picking a
 * property adds its pill with the editor open; "고급 필터 추가" adds the
 * advanced filter's row with its window open. A locked database's own filters end in "필터 초기화"
 * (seen 2026-10-01), which takes them away; elsewhere the viewer's own end
 * in DatabaseOwnQueryRows.
 */
export function DatabaseFilterSettings({
  view, properties, snapshot, writable, advancedCount, advancedEditor, onAddAdvanced, onUpdateView, onReset, ownQuery,
}: FilterSettingsProps) {
  const labels = databaseViewLabels();
  const quick = viewQuickFilters(view);
  // Merged into the advanced filter, its window opens at once, as Notion's (seen 2026-11-04).
  const [merged, setMerged] = useState(false);
  const [mode, setMode] = useState<"list" | "pick">("list");
  // After "필터 초기화" the screen stays a list with "필터 추가", as Notion's.
  const [reset, setReset] = useState(false);
  const [open, setOpen] = useState<string | null>(null);
  const commit = (next: QuickFilter[]) => onUpdateView({ ...view, config: { ...view.config, quickFilters: next.length ? next : undefined } });
  const saved = ownQuery?.saved;
  const changed = saved ? changedFilterCount(view, saved) : 0;
  // A filter the viewer changed carries the orange dot here too, as on the bar (Notion's, seen 2026-11-06).
  const savedQuick = (saved && Array.isArray(saved.config.quickFilters) ? saved.config.quickFilters : []) as QuickFilter[];
  const filterUnsaved = (filter: ViewFilter) => Boolean(saved) && !sameViewQuery({ quickFilters: [filter] },
    { quickFilters: savedQuick.filter((kept) => isSingleFilter(kept) && kept.propertyId === filter.propertyId) });
  const used = new Set(quick.filter(isSingleFilter).map(({ propertyId }) => propertyId));
  const keys = quick.map(quickFilterKey);
  const reorder = useDragReorder(FILTER_DRAG, (sourceId, targetId, side) => {
    if (!keys.includes(sourceId) || !keys.includes(targetId)) return;
    const order = keys.filter((key) => key !== sourceId);
    order.splice(order.indexOf(targetId) + (side === "after" ? 1 : 0), 0, sourceId);
    commit(order.map((key) => quick[keys.indexOf(key)]));
  }, { live: true });

  const picking = mode === "pick" || (quick.length === 0 && advancedCount === 0 && !reset);
  // Picking, the screen is "필터 추가", its "←" back to the filters when "필터 추가" there opened it; listing them,
  // "필터" — after "필터 초기화" too — as Notion's (seen 2026-11-18 in Chrome 1440).
  useSettingsScreenHead(picking ? { title: labels.addFilter, onBack: mode === "pick" ? () => setMode("list") : undefined } : null);
  if (picking) {
    return (
      <DatabasePropertyPicker placeholder={labels.filterBy} properties={properties.filter(({ id }) => !used.has(id))}
        footer={writable ? advancedFilterPickerRow(advancedCount, {
          // Its window opens at once over the list, the focus on nothing, as Notion's (seen 2026-11-12).
          onAdd: () => { onAddAdvanced(); setMerged(true); setMode("list"); },
          // Its window opens over the list, as one just merged into it.
          onOpen: () => { setMerged(true); setMode("list"); },
        }) : undefined}
        onPick={(property) => {
          const full = properties.find(({ id }) => id === property.id);
          if (!full || !writable) return;
          commit([...quick, { propertyId: full.id, operator: defaultOperator(full, properties), value: defaultValue(full, properties) }]);
          setOpen(full.id);
          setMode("list");
        }} />
    );
  }
  return (
    <div className={styles.list} data-filter-settings>
      {advancedCount > 0 && <AdvancedFilterRow key={merged ? "merged" : "row"} count={advancedCount} editor={advancedEditor} startOpen={merged} />}
      {quick.map((filter, index) => {
        const key = keys[index];
        return (
          <div key={key} className={styles.row} data-filter-setting={key} {...reorder.row(key)}>
            <button type="button" className={styles.handle} disabled={!writable || (isSingleFilter(filter) && filter.locked)}
              aria-label={labels.dragToMoveFilter(isSingleFilter(filter)
                ? properties.find(({ id }) => id === filter.propertyId)?.name ?? "" : labels.quickFilterGroup(groupSize(filter)))}
              {...reorder.handle(key, writable, { above: keys[index - 1], below: keys[index + 1] })}>
              <DragHandleIcon size="small" aria-hidden="true" />
            </button>
            {isSingleFilter(filter) ? (
              <QuickFilterPill filter={filter} properties={properties} snapshot={snapshot} unsaved={filterUnsaved(filter)}
                writable={writable} open={open === key} onOpenChange={(next) => setOpen(next ? key : null)}
                onChange={(next) => commit(quick.map((item, current) => current === index ? next : item))}
                onRemove={() => {
                  setOpen(null);
                  commit(quick.filter((_item, current) => current !== index));
                }}
                onToAdvanced={() => {
                  setOpen(null);
                  onUpdateView(viewWithQuickFilterInAdvanced(view, index));
                }} />
            ) : (
              <span className={styles.groupPill}>{labels.quickFilterGroup(groupSize(filter))}</span>
            )}
          </div>
        );
      })}
      {writable && (
        <button type="button" className={styles.action} data-add-filter onClick={() => setMode("pick")}>
          <span className={styles.actionIcon} aria-hidden="true"><Plus size="regular" /></span>
          <span>{labels.addFilter}</span>
        </button>
      )}
      {saved && changed > 0 ? (
        <DatabaseOwnQueryRows note={labels.filtersChangedFromView(changed)} resetLabel={labels.resetFilters}
          rowClassName={styles.action} iconClassName={styles.actionIcon}
          onSaveForEveryone={ownQuery?.onSaveForEveryone} onSaveAsNewView={ownQuery?.onSaveAsNewView}
          onMergeFilters={quick.length ? () => { setMerged(true); onUpdateView(viewWithQuickFiltersInAdvanced(view)); } : undefined}
          onReset={() => {
            setReset(true);
            onUpdateView({ ...view, config: { ...view.config, quickFilters: saved.config.quickFilters,
              ...Object.fromEntries(ADVANCED_KEYS.map((key) => [key, saved.config[key]])) } });
          }} />
      ) : null}
      {onReset && (
        <button type="button" className={styles.action} data-reset-filters onClick={() => { setReset(true); onReset(); }}>
          <span className={styles.actionIcon} aria-hidden="true"><ResetIcon size="regular" /></span>
          <span>{labels.resetFilters}</span>
        </button>
      )}
    </div>
  );
}

/**
 * The advanced filter in the settings' filter list, as Notion's (seen
 * 2026-11-03): its "규칙 N개 ⌄" pill after a handle, whose window opens under
 * it over the settings (AdvancedFilterWindow).
 */
function AdvancedFilterRow({ count, editor, startOpen = false }: { count: number; editor: ReactNode; startOpen?: boolean }) {
  const labels = databaseViewLabels();
  const [open, setOpen] = useState(startOpen);
  const pillRef = useRef<HTMLButtonElement>(null);
  return (
    <div className={styles.row} data-filter-setting="advanced">
      <span className={styles.handle} aria-hidden="true"><DragHandleIcon size="small" /></span>
      <button ref={pillRef} type="button" className={pillStyles.pill} aria-haspopup="dialog" aria-expanded={open}
        data-advanced-filter onClick={() => setOpen(!open)}>
        <FilterIcon size="tiny" aria-hidden="true" />
        <span>{labels.advancedFilterPill(count)}</span>
        <ChevronDown size="micro" aria-hidden="true" />
      </button>
      {open ? <AdvancedFilterWindow anchor={pillRef} label={labels.advancedFilterPill(count)} onClose={() => setOpen(false)}>
        {editor}
      </AdvancedFilterWindow> : null}
    </div>
  );
}

function groupSize(group: FilterGroup): number {
  return group.filters.length + (group.groups ?? []).reduce((total, child) => total + groupSize(child), 0);
}

/**
 * The toolbar's "필터" when the view has no quick filters, as Notion's (seen
 * 2026-09-30): the "필터 기준" picker, whose property becomes a pill in the
 * filter bar with its editor open, and "고급 필터 추가", which makes the
 * advanced filter's pill alone (seen 2026-11-02) where there is a bar. A view
 * with an advanced filter opens on it. A phone's is `PhoneFilterList`.
 */
export function DatabaseFilterMenu({ properties, writable, advancedCount, advancedEditor, onAddAdvanced, onAdvancedAdded, onPick }: {
  properties: DbProperty[];
  writable: boolean;
  advancedCount: number;
  advancedEditor: ReactNode;
  onAddAdvanced(): void;
  onAdvancedAdded?(): void;
  onPick(propertyId: string): void;
}) {
  const labels = databaseViewLabels();
  const [advanced, setAdvanced] = useState(advancedCount > 0);
  // A dashboard's lists its widgets' sources' properties, more than one source's each apart, with no advanced filter
  // and ending in "⚙ 다중 출처 필터링" (Notion's, seen 2026-11-17).
  const dashboardSources = useDashboardFilterSources(), grouped = groupedSources(dashboardSources);
  if (grouped) return <DashboardFilterPropertyPicker sources={grouped} multiSource onPick={onPick} />;
  if (dashboardSources) return <DatabasePropertyPicker placeholder={labels.filterBy} properties={properties} onPick={(property) => onPick(property.id)} />;
  if (advanced) return <div className={styles.advanced}>{advancedEditor}</div>;
  return (
    <DatabasePropertyPicker placeholder={labels.filterBy} properties={properties}
      footer={writable ? { icon: <Plus size="regular" />, label: labels.addAdvancedFilter, onClick: () => {
        onAddAdvanced();
        if (onAdvancedAdded) onAdvancedAdded();
        else setAdvanced(true);
      } } : undefined}
      onPick={(property) => onPick(property.id)} />
  );
}
