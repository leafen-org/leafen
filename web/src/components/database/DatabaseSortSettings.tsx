import { useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { useTranslation } from "react-i18next";
import type { GraphView } from "@/contentGraph";
import { CountChevronDown, DragHandleIcon, Plus, Trash, X } from "@/icons/leafen";
import type { DbProperty, ViewSort } from "@/lib/types";
import { databasePropertyOptions } from "./DatabaseFilterEditors";
import { DatabasePropertyPicker } from "./DatabasePropertyPicker";
import { sortIsDescending, sortableProperty as sortable } from "./query";
import { NotionSelect } from "./NotionSelect";
import { databaseViewLabels } from "./databaseViewLabels";
import { propertySortLabels } from "./tablePropertyMenuModel";
import { useDragReorder } from "@/lib/useDragReorder";
import { usePhoneSheetHead } from "../shell/PhoneSheet";
import { useSettingsScreenHead } from "./settingsScreenHead";
import { DatabasePhoneSortList } from "./DatabasePhoneSortList";
import { DatabaseOwnQueryRows } from "./DatabaseOwnQueryRows";
import styles from "./DatabaseSortSettings.module.css";

// A sort's choices draw Notion's thin ⌄, 8.64 by 14 as wide as its ink, as an advanced filter rule's (DatabaseSortSettings.module.css).
const SORT_CARET = <CountChevronDown size="tiny" trim aria-hidden="true" />;
import windowStyles from "../shell/PhoneSheetWindow.module.css";
import type { PhoneOwnQuery } from "./DatabasePhoneFilterList";


const SORT_DRAG = "application/x-leafen-view-sort";
// "정렬 추가"'s list stops here and scrolls inside (Notion's, below).
const ADD_LIST_HEIGHT = 287;

export function viewSorts(view: GraphView): ViewSort[] {
  if (!Array.isArray(view.config.sorts)) return [];
  return (view.config.sorts as ViewSort[]).map((sort) => ({ ...sort, direction: sortIsDescending(sort.direction) ? "desc" : "asc" }));
}

/**
 * A view's sorts, as Notion's sort screen (seen 2026-09-30): with none, the
 * "정렬 기준" list of properties; then a row per sort of drag handle,
 * property, direction and ×, "정렬 추가", which lists the properties not yet
 * sorted by, and "정렬 제거", which removes every sort. A viewer who can't
 * edit the view sorts it for themselves alone.
 */
export function DatabaseSortSettings({ view, properties, onUpdateView, sheet, touchWindow, ownQuery, onPicked }: {
  view: GraphView;
  properties: DbProperty[];
  onUpdateView(view: GraphView): void;
  /** The first sort picked hands over to its pill's editor (the toolbar's list, as Notion's). */
  onPicked?(): void;
  /** In a phone's sheet, its property list and its sorts as the sheet's rows (Notion's "새 정렬" and "정렬", seen 2026-10-21). */
  sheet?: boolean;
  /**
   * In a wider touch screen's middle window (DatabaseTouchMenuWindow), its
   * sorts are the phone's rows at the window's size — "Aa 이름 ›", "A → Z로
   * 정렬 ›", a red "삭제", "+ 정렬 추가" and "정렬 제거" — as Notion's iPad
   * (seen 2026-11-15 at 834), their property and "정렬 추가" windows of their
   * own there and the direction rising from the bottom (sheetWindow.ts).
   */
  touchWindow?: boolean;
  /** The viewer's own filters and sorts, whose sorts the screen saves or resets while they differ from the view's. */
  ownQuery?: PhoneOwnQuery | null;
}) {
  const labels = databaseViewLabels();
  const { t } = useTranslation("tableView");
  const sorts = viewSorts(view);
  const [picking, setPicking] = useState(sorts.length === 0);
  // "정렬 추가" opens its "속성 검색" list under itself over the sorts, which
  // stay, as Notion's (seen 2026-11-05): a window of its own over the page,
  // not cut by the sorts' window — its top on the row's foot, its start on
  // the row's start, 280 wide and as tall as its properties up to 287px or
  // 70% of the window, its search and rows scrolling inside past that
  // (Notion's, seen 2026-11-15 in a 723 window: the row 378..723 × 257..285,
  // the list 378..658 × 285..572; 2026-11-17 with 13 properties, 287 high at
  // 1440×723 and at 1440×523, kept under the row there but slid up over it to
  // end 12px over the window's foot, not turned over nor cut).
  const [adding, setAdding] = useState(false);
  const addRef = useRef<HTMLButtonElement>(null), addListRef = useRef<HTMLDivElement>(null);
  const addOpen = adding && !sheet;
  const addLayer = useOverlayLayer(addOpen, { kind: "menu", ref: addListRef, onEscape: () => setAdding(false), onOutsidePress: () => setAdding(false),
    inside: () => [addRef.current], returnFocusRef: addRef });
  const addPlace = usePopoverPlacement(addListRef, addOpen ? (size, viewport) => {
    const cap = Math.min(ADD_LIST_HEIGHT, Math.round(viewport.height * 0.7));
    return placeAround(popoverRectOf(addRef.current)!, size, viewport,
      { gap: 0, cap, keepSide: true, minHeight: Math.min(Math.ceil(size.height), cap) });
  } : null, [addOpen], undefined, false);
  const commit = (next: ViewSort[]) => onUpdateView({ ...view, config: { ...view.config, sorts: next } });
  const used = new Set(sorts.map(({ propertyId }) => propertyId));
  const keys = sorts.map(({ propertyId }) => propertyId);
  const reorder = useDragReorder(SORT_DRAG, (sourceId, targetId, side) => {
    const order = keys.filter((key) => key !== sourceId);
    order.splice(order.indexOf(targetId) + (side === "after" ? 1 : 0), 0, sourceId);
    commit(order.map((key) => sorts[keys.indexOf(key)]));
  }, { live: true });

  // The screen names it "새 정렬" while picking the first, else "정렬" — a sheet's, as Notion's phone (seen 2026-10-21), and
  // the view settings panel's, as Notion's (seen 2026-11-18 in Chrome 1440: "새 정렬" over the properties, "정렬" once one
  // was picked, and still once "정렬 제거" left none).
  usePhoneSheetHead(sheet ? { title: picking ? labels.sheetNewSort : labels.sorts } : null);
  useSettingsScreenHead(!sheet && picking ? { title: labels.sheetNewSort } : null);

  if (picking) {
    return (
      <DatabasePropertyPicker placeholder={labels.sortBy} properties={properties.filter((property) => !used.has(property.id) && sortable(property))}
        sheet={sheet} sheetOpensMore={false}
        onPick={(property) => {
          commit([...sorts, { propertyId: property.id, direction: "asc" }]);
          if (onPicked) onPicked();
          else setPicking(false);
        }} />
    );
  }
  const savedSorts = ownQuery ? viewSorts(ownQuery.saved) : sorts;
  const save = ownQuery && JSON.stringify(savedSorts) !== JSON.stringify(sorts) ? {
    onReset: () => commit(savedSorts), onSaveForEveryone: ownQuery.onSaveForEveryone, onSaveAsNewView: ownQuery.onSaveAsNewView,
  } : null;
  if (sheet || touchWindow) {
    const list = <DatabasePhoneSortList sorts={sorts} properties={properties} save={save} onCommit={commit} />;
    return touchWindow ? <div className={windowStyles.rows} data-sort-settings>{list}</div> : list;
  }
  return (
    <div className={styles.list} data-sort-settings>
      {sorts.map((sort, index) => {
        const property = properties.find(({ id }) => id === sort.propertyId);
        const [ascending, descending] = propertySortLabels(property, properties);
        const change = (patch: Partial<ViewSort>) => commit(sorts.map((item, current) => current === index ? { ...item, ...patch } : item));
        return (
          <div key={sort.propertyId} className={styles.row} data-sort-setting={sort.propertyId} {...reorder.row(sort.propertyId)}>
            <button type="button" className={styles.handle} aria-label={labels.dragToMoveSort(property?.name ?? "")}
              {...reorder.handle(sort.propertyId, true, { above: keys[index - 1], below: keys[index + 1] })}>
              <DragHandleIcon size="small" aria-hidden="true" />
            </button>
            <NotionSelect className={styles.property} buttonClassName={styles.choice} caret={SORT_CARET} ariaLabel={labels.sortProperty}
              value={sort.propertyId}
              options={databasePropertyOptions(properties.filter((candidate) => candidate.id === sort.propertyId || (!used.has(candidate.id) && sortable(candidate))))}
              onChange={(propertyId) => change({ propertyId })} />
            <NotionSelect className={styles.direction} buttonClassName={styles.choice} caret={SORT_CARET} ariaLabel={labels.sortDirection}
              value={sort.direction} plain menuMinWidth={0}
              options={[{ value: "asc", label: t(ascending) }, { value: "desc", label: t(descending) }]}
              onChange={(direction) => change({ direction: direction as ViewSort["direction"] })} />
            <button type="button" className={styles.remove} aria-label={labels.removeSort}
              onClick={() => commit(sorts.filter((_item, current) => current !== index))}>
              <X size="small" aria-hidden="true" />
            </button>
          </div>
        );
      })}
      <span className={styles.addWrap}>
        <button ref={addRef} type="button" className={styles.action} data-add-sort aria-expanded={adding}
          onClick={() => setAdding((current) => !current)}>
          <span className={styles.actionIcon} aria-hidden="true"><Plus size="regular" /></span>
          <span>{labels.addSort}</span>
        </button>
        {addOpen ? createPortal(
          <div ref={addListRef} className={styles.addList} role="dialog" aria-label={labels.addSort}
            style={{ position: "fixed", ...popoverPlacementStyle(addPlace), zIndex: addLayer.zIndex }}
            onKeyDown={addLayer.onKeyDown} data-add-sort-list data-property-picker-window="">
            <DatabasePropertyPicker placeholder={labels.sheetSearchProperty}
              properties={properties.filter((property) => !used.has(property.id) && sortable(property))}
              onPick={(property) => { setAdding(false); commit([...sorts, { propertyId: property.id, direction: "asc" }]); }} />
          </div>, document.body) : null}
      </span>
      {sorts.length > 0 && (
        <button type="button" className={styles.action} data-remove-sorts onClick={() => commit([])}>
          <span className={styles.actionIcon} aria-hidden="true"><Trash size="regular" /></span>
          <span>{labels.removeSorts}</span>
        </button>
      )}
      {save ? <DatabaseOwnQueryRows note={labels.sortsChangedFromView} resetLabel={labels.resetOwnSorts}
        rowClassName={styles.action} iconClassName={styles.actionIcon} {...save} /> : null}
    </div>
  );
}
