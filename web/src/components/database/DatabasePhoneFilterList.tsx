import { useEffect, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { GraphView } from "@/contentGraph";
import { DotsHorizontal, Plus, ResetIcon, SaveForEveryoneIcon, Settings, SortIcon } from "@/icons/leafen";
import type { DbProperty, FilterGroup, ViewFilter } from "@/lib/types";
import { activeDateLocale } from "@/lib/i18n";
import { usePageNames } from "@/lib/pageNames";
import { filterAwaitsValue } from "@/shared/database/queryFilterPolicy";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow, phoneSheetStyles as sheetStyles, usePhoneSheetHead } from "../shell/PhoneSheet";
import { DatabasePropertyPicker, type SheetRowMark } from "./DatabasePropertyPicker";
import { DashboardFilterPropertyPicker } from "./DashboardFilterPropertyPicker";
import { groupedSources, useDashboardFilterSources } from "./dashboardFilterSources";
import { MultiSourceSheet } from "./DashboardMultiSourceSheet";
import { MultiSourceEditContext } from "./dashboardMultiSourceEdit";
import { isMultiSourceFilter, multiSourceProperties, multiSourceShownProperty, takeMultiSourcePick, withMultiSourceProperties } from "./dashboardMultiSourceFilter";
import { PropertyIcon } from "./PropertyIcon";
import { quickFilterSummary } from "./DatabaseQuickFilterBar";
import { QuickFilterSheet } from "./DatabaseQuickFilterSheet";
import { viewSorts } from "./DatabaseSortSettings";
import { defaultOperator, defaultValue } from "./databaseFilterPolicy";
import type { DatabaseFilterSnapshot } from "./databaseFilterSnapshot";
import { sameViewQuery, viewQuery, type PersonalQuery } from "./databasePersonalQuery";
import { databaseViewLabels } from "./databaseViewLabels";
import { viewWithQuickFilterInAdvanced } from "./databaseToolbarModel";
import styles from "./DatabasePhoneFilterList.module.css";

type QuickFilter = ViewFilter | FilterGroup;

function isSingleFilter(filter: QuickFilter): filter is ViewFilter {
  return typeof (filter as ViewFilter).propertyId === "string";
}

function quickFiltersOf(view: GraphView): QuickFilter[] {
  return (Array.isArray(view.config.quickFilters) ? view.config.quickFilters : []) as QuickFilter[];
}

/** How many filters a phone's list counts: a property's once it has a value, as Notion's title does. */
export function phoneFilterCount(view: GraphView, advancedCount: number) {
  return quickFiltersOf(view).filter((filter) => !isSingleFilter(filter) || !filterAwaitsValue(filter)).length + advancedCount;
}

/** The viewer's own filters and sorts: the view as saved, and resetting them or, for an editor, saving them for everyone. */
export type PhoneOwnQuery = { saved: GraphView; onReset(): void; onSaveForEveryone?(): void; onSaveAsNewView?(): void };

const shownQuery = (config: GraphView["config"]): PersonalQuery => ({
  ...viewQuery(config),
  quickFilters: (Array.isArray(config.quickFilters) ? config.quickFilters as QuickFilter[] : [])
    .filter((filter) => !isSingleFilter(filter) || !filterAwaitsValue(filter)),
});

/** Whether the viewer's own filters and sorts show other rows than the view's: a filter still waiting for its value changes nothing, as Notion's. */
export function ownQueryChanged(view: GraphView, ownQuery?: PhoneOwnQuery | null) {
  return Boolean(ownQuery) && !sameViewQuery(shownQuery(view.config), shownQuery(ownQuery!.saved.config));
}

const advancedPart = (config: GraphView["config"]): PersonalQuery => ({
  filterGroup: config.filterGroup, filters: config.filters, filterConjunction: config.filterConjunction,
});

/**
 * A phone's filters, as Notion's sheet "필터 N개" (seen 2026-10-24 at
 * 375px): while the viewer's own filters and sorts apply, an orange "모두에게
 * 저장" and a "⋯" ("초기화") over them; "정렬", the advanced filter while it
 * has rules, then the properties, those it filters first, the rest by name — each of those
 * blue with its value grey at its end and, while it is the viewer's own, an
 * orange dot — and "고급 필터" while it has none. "⋯" holds "초기화" and, for
 * an editor, "새 보기로 저장". A property's filter opens
 * over the list, its "완료" back to it. A dashboard's lists only the
 * properties it filters, then a small grey "+ 필터 추가하기" under them,
 * which opens "속성 필터링" over the whole sheet: a bare "필터 기준" search
 * over the other properties, their marks white and no "›", no advanced
 * filter, as Notion's iPhone (seen 2026-11-16 at 402, with and without a
 * filter).
 */
export function PhoneFilterList({ view, properties, snapshot, editable, advanced, ownQuery, onSort, start, dashboard = false, onUpdateView }: {
  view: GraphView;
  properties: DbProperty[];
  snapshot: DatabaseFilterSnapshot;
  editable: boolean;
  advanced: { count: number; add(): void; sheetEditor(onEmpty: () => void): ReactNode };
  ownQuery?: PhoneOwnQuery | null;
  /** "정렬" first, in the toolbar's sheet; the view settings' "필터" has none. */
  onSort?(): void;
  /** A property to open the filter of at once, as a column's "필터" asks. */
  start?: { propertyId: string; request: number } | null;
  /** A dashboard's filters, added one property at a time. */
  dashboard?: boolean;
  onUpdateView(view: GraphView): void;
}) {
  const pageNames = usePageNames();
  const labels = databaseViewLabels();
  const { t } = useTranslation("databaseView");
  const quick = quickFiltersOf(view);
  const [open, setOpen] = useState<string | null>(null);
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [more, setMore] = useState(false);
  const [adding, setAdding] = useState(false);
  // A dashboard filter's "다중 출처 필터링" sheet, by the filter's place among them.
  const [multiEdit, setMultiEdit] = useState<number | null>(null);
  const dashboardSources = groupedSources(useDashboardFilterSources());
  const editorRef = useRef<HTMLDivElement>(null);
  usePhoneSheetHead(advancedOpen ? { title: labels.sheetAdvancedFilter, onBack: () => setAdvancedOpen(false) } : null);
  const commit = (next: QuickFilter[]) => onUpdateView({ ...view, config: { ...view.config, quickFilters: next.length ? next : undefined } });
  const filterOf = (propertyId: string) => quick.find((filter): filter is ViewFilter => isSingleFilter(filter) && filter.propertyId === propertyId);
  // A property without a filter gets one at once, heading the list, as Notion's.
  const openProperty = (propertyId: string) => {
    const property = properties.find(({ id }) => id === propertyId);
    // The database's own rule (the Sprint board's current sprint) opens nothing, as its pill.
    if (!property || filterOf(propertyId)?.locked) return;
    if (!filterOf(propertyId)) {
      if (!editable) return;
      const filter = { propertyId, operator: defaultOperator(property, properties), value: defaultValue(property, properties) };
      // One picked from "다중 출처 필터링"'s list opens on its sheet, as the window on a computer.
      if (takeMultiSourcePick(propertyId)) {
        commit([...quick, withMultiSourceProperties(filter, [propertyId])]);
        setMultiEdit(quick.length);
        return;
      }
      commit([...quick, filter]);
    }
    setOpen(propertyId);
  };
  useEffect(() => {
    if (start) openProperty(start.propertyId);
    // Only when asked again.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [start?.request]);

  if (advancedOpen) return <>{advanced.sheetEditor(() => setAdvancedOpen(false))}</>;

  const saved = ownQuery?.saved;
  const unsaved = (seen: PersonalQuery, kept: PersonalQuery) => Boolean(saved) && !sameViewQuery(seen, kept);
  const savedFilterOf = (propertyId: string) => saved ? quickFiltersOf(saved).filter((filter) => isSingleFilter(filter) && filter.propertyId === propertyId) : [];
  const sorts = viewSorts(view);
  const sortsUnsaved = Boolean(saved) && unsaved({ sorts: view.config.sorts }, { sorts: saved?.config.sorts });
  const advancedUnsaved = Boolean(saved) && unsaved(advancedPart(view.config), advancedPart(saved!.config));
  // One sort names its property, more say how many ("2"), as Notion's.
  const sortValue = sorts.length === 1 ? properties.find(({ id }) => id === sorts[0]!.propertyId)?.name
    : sorts.length > 1 ? String(sorts.length) : undefined;
  const pageName = (id: string) => { const page = snapshot.pagesById[id]; return page ? pageNames.title(page) : ""; };
  // Those it filters first, the rest by name — the name property among them,
  // not first as on a desktop — as Notion's (seen 2026-10-24 and 2026-11-11).
  const filtered = quick.filter(isSingleFilter).map(({ propertyId }) => propertyId);
  const ordered = [...filtered.flatMap((id) => properties.filter((property) => property.id === id)),
    ...properties.filter(({ id }) => !filtered.includes(id)).sort((left, right) => left.name.localeCompare(right.name, activeDateLocale()))];
  const mark = (property: Pick<DbProperty, "id">): SheetRowMark | undefined => {
    const filter = filterOf(property.id), full = properties.find(({ id }) => id === property.id);
    if (!filter || !full || filterAwaitsValue(filter)) return undefined;
    const value = quickFilterSummary(filter, full, properties, pageName).replace(/^:\s*/u, "").trim();
    return { applied: true, unsaved: unsaved({ quickFilters: [filter] }, { quickFilters: savedFilterOf(property.id) }), value: value || undefined };
  };
  const marked = (icon: ReactNode, dot: boolean) => (
    <span className={styles.markIcon}>{icon}{dot ? <span className={styles.unsaved} data-unsaved /> : null}</span>
  );
  const editing = open ? filterOf(open) : undefined;
  const editingProperty = editing ? properties.find(({ id }) => id === editing.propertyId) : undefined;
  const editingIndex = editing ? quick.indexOf(editing) : -1;
  return (
    <div className={`${sheetStyles.stack} ${sheetStyles.propertyRows}`} data-phone-filter-list>
      {ownQuery && ownQueryChanged(view, ownQuery) ? (
        <div className={styles.own} data-own-view-query>
          {ownQuery.onSaveForEveryone ? (
            <button type="button" className={styles.save} data-save-view-for-everyone onClick={ownQuery.onSaveForEveryone}>
              <SaveForEveryoneIcon size="regular" aria-hidden="true" />
              <span>{labels.saveViewForEveryone}</span>
            </button>
          ) : (
            <button type="button" className={`${styles.save} ${styles.reset}`} data-own-view-query-reset onClick={ownQuery.onReset}>
              <ResetIcon size="regular" aria-hidden="true" />
              <span>{labels.resetOwnViewQuery}</span>
            </button>
          )}
          {ownQuery.onSaveForEveryone ? (
            <button type="button" className={styles.more} aria-label={labels.filterMoreActions} aria-haspopup="dialog"
              data-own-view-query-more onClick={() => setMore(true)}>
              <DotsHorizontal size="small" aria-hidden="true" />
            </button>
          ) : null}
        </div>
      ) : null}
      {onSort ? (
        <PhoneSheetGroup>
          <PhoneSheetRow icon={marked(<SortIcon size="regular" />, sortsUnsaved)} label={<span className={styles.markLabel}>{labels.sort}</span>}
            value={sortValue} onPress={onSort} data={{ "data-phone-filter-sorts": "", "data-applied": sorts.length ? "true" : undefined }} />
        </PhoneSheetGroup>
      ) : null}
      {advanced.count > 0 ? (
        <PhoneSheetGroup>
          <PhoneSheetRow icon={marked(<Settings size="regular" />, advancedUnsaved)} label={<span className={styles.markLabel}>{labels.sheetAdvancedFilter}</span>}
            value={labels.advancedFilterPill(advanced.count)} onPress={() => setAdvancedOpen(true)}
            data={{ "data-advanced-filter": "", "data-applied": "true" }} />
        </PhoneSheetGroup>
      ) : null}
      {dashboard ? <>
        {filtered.length ? <PhoneSheetGroup>
          {ordered.filter(({ id }) => filtered.includes(id)).map((property) => {
            const shown = mark(property), filter = filterOf(property.id);
            // A filter over several sources' properties named and marked as its pill is.
            const named = filter && isMultiSourceFilter(filter) ? multiSourceShownProperty(filter, property, properties, t("multiSourceEmptyPill")) : property;
            // With none of its properties left it is "필터" alone, as its pill.
            const empty = filter && isMultiSourceFilter(filter) && !multiSourceProperties(filter, properties).length;
            return <PhoneSheetRow key={property.id} icon={marked(<PropertyIcon property={named} size="wide" />, Boolean(shown?.unsaved))}
              label={<span className={styles.markLabel}>{named.name}</span>} value={empty ? undefined : shown?.value} onPress={() => openProperty(property.id)}
              data={{ "data-phone-filter-property": property.id, "data-applied": shown ? "true" : undefined }} />;
          })}
        </PhoneSheetGroup> : null}
        {editable ? <div className={styles.addLine}>
          <button type="button" className={styles.addFilter} data-phone-filter-add onClick={() => setAdding(true)}>
            <Plus size="small" trim aria-hidden="true" />{t("sheetAddFilterRow")}
          </button>
        </div> : null}
      </> : <DatabasePropertyPicker sheet keepOrder placeholder={labels.sheetSearchProperties} properties={ordered} mark={mark}
        footer={editable && advanced.count === 0 ? {
          icon: <Settings size="regular" />, label: labels.addAdvancedFilter, sheetLabel: labels.sheetAdvancedFilter,
          onClick: () => { advanced.add(); setAdvancedOpen(true); },
        } : undefined}
        onPick={(property) => openProperty(property.id)} />}
      {adding ? (
        <PhoneSheet title={t("sheetFilterByProperty")} onClose={() => setAdding(false)} data={{ "data-phone-filter-by-property": "" }}>
          {/* Each widget source's properties apart under its name, as Notion's iPhone (w1554, 2026-11-17). */}
          {dashboardSources ? <DashboardFilterPropertyPicker sheet multiSource sources={dashboardSources} exclude={filtered}
            onPick={(propertyId) => { setAdding(false); openProperty(propertyId); }} />
            : <DatabasePropertyPicker sheet keepOrder sheetOpensMore={false} emptyApart placeholder={labels.filterBy} properties={ordered.filter(({ id }) => !filtered.includes(id))}
            onPick={(property) => { setAdding(false); openProperty(property.id); }} />}
        </PhoneSheet>
      ) : null}
      {editing && editingProperty ? (
        <MultiSourceEditContext.Provider value={dashboardSources ? () => {
          if (!isMultiSourceFilter(editing)) commit(quick.map((filter) => filter === editing ? withMultiSourceProperties(editing, [editing.propertyId]) : filter));
          setOpen(null); setMultiEdit(editingIndex);
        } : null}>
        <QuickFilterSheet filter={editing} property={isMultiSourceFilter(editing) ? multiSourceShownProperty(editing, editingProperty, properties, t("multiSourceEmptyPill")) : editingProperty}
          properties={properties} snapshot={snapshot} sheetRef={editorRef}
          onChange={(patch) => commit(quick.map((filter) => filter === editing ? { ...editing, ...patch } : filter))}
          onRemove={() => { setOpen(null); commit(quick.filter((filter) => filter !== editing)); }}
          onToAdvanced={() => { setOpen(null); onUpdateView(viewWithQuickFilterInAdvanced(view, editingIndex)); }}
          onClose={() => setOpen(null)} />
        </MultiSourceEditContext.Provider>
      ) : null}
      {multiEdit !== null && dashboardSources && quick[multiEdit] && isSingleFilter(quick[multiEdit]!) ? (
        <MultiSourceSheet filter={quick[multiEdit] as ViewFilter} properties={properties} sources={dashboardSources}
          onChange={(next) => commit(quick.map((filter, index) => index === multiEdit ? next : filter))}
          onRemove={() => { setMultiEdit(null); commit(quick.filter((_filter, index) => index !== multiEdit)); }}
          onDone={() => { setOpen((quick[multiEdit] as ViewFilter).propertyId); setMultiEdit(null); }}
          onClose={() => setMultiEdit(null)} />
      ) : null}
      {more && ownQuery ? (
        // "⋯", as Notion's (seen 2026-10-24): a sheet without a title over the list.
        <PhoneSheet size="half" title="" label={labels.filterMoreActions} onClose={() => setMore(false)} data={{ "data-own-view-query-actions": "" }}>
          <div className={styles.moreActions}>
            <PhoneSheetGroup>
              <PhoneSheetRow label={labels.resetOwnViewQuery} more={false} onPress={() => { setMore(false); ownQuery.onReset(); }}
                data={{ "data-own-view-query-reset": "" }} />
              {ownQuery.onSaveAsNewView ? (
                <PhoneSheetRow label={labels.saveAsNewView} more={false} onPress={() => { setMore(false); ownQuery.onSaveAsNewView?.(); }}
                  data={{ "data-save-as-new-view": "" }} />
              ) : null}
            </PhoneSheetGroup>
          </div>
        </PhoneSheet>
      ) : null}
    </div>
  );
}
