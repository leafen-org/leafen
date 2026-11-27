import { Fragment, useId, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { GraphView } from "@/contentGraph";
import { AddCircleFillIcon, CheckIcon, DotsHorizontal, DragHandleIcon, Plus, SquareOnSquarePlusIcon } from "@/icons/leafen";
import { useEdgeFade } from "@/lib/edgeFade";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { placeInScrollBox } from "@/lib/scrollBoxPlace";
import { useDragReorder } from "@/lib/useDragReorder";
import { touchScreen } from "@/lib/screenKind";
import { useHoverTip } from "../IconTipButton";
import { databaseViewLabels } from "./databaseViewLabels";
import { ViewIcon, viewDisplayName } from "./databaseViewShared";
import styles from "./DatabaseViewList.module.css";

const ADD = "__add-view", SOURCE = "__new-source", LEAD = "__lead";
// As the list opens its current view shows: left where it is above the
// list's last 34px (Notion's bottom fade), else the list scrolled to bring
// its row's end 64px over the list's end, two rows and more under it
// (Notion's, seen 2026-11-15 at 1440 in a 314px list of 18 views: the 9th
// left at the top, the 10th to 18th each lifted to 220–230px down; Notion
// reckons it at the start of its 96% growing-in, hence its 1px a row more).
const CURRENT_FADE = 34, CURRENT_ROOM = 64;
// Its rows fade at an edge they scroll on past, 1.7px a pixel scrolled up to 34px (Notion's mask, useEdgeFade).
const FADE_RATE = 1.7;
/**
 * A row among the views that is not one of them: a wiki's Home
 * (wikiViewsContext), at its place, moved and named as a view is.
 */
export type ViewListLead = {
  /** What a move names it by (onReorder). */
  id: string; label: string; icon: ReactNode; current: boolean; onSelect(): void;
  /** How many of the views stand before it. */
  index: number;
  /** Its "⋯", its actions beside it. */
  onActions?(element: HTMLElement): void;
  actionsOpen?: boolean;
};
export function DatabaseViewList({ views, activeId, label, open, readOnly, actionsOpen, lead, onSelect, onActions, onReorder, onAddView, onAddSource, addRefusal }: {
  views: GraphView[]; activeId: string; label: string; open: boolean; readOnly: boolean;
  /** The view whose actions are open from its "⋯": its row stays lit, as Notion's. */
  actionsOpen?: string;
  lead?: ViewListLead;
  onSelect(view: GraphView): void; onActions(view: GraphView, element: HTMLElement): void;
  onReorder(source: string, target: string, side: "before" | "after"): void;
  /** "새 보기", with its row, beside which the view types open. */
  onAddView?(row: HTMLElement | null): void; onAddSource?(): void;
  /** Why "새 보기" and "새 데이터 소스" are dim, told in a tip before them: the database holds as many views as it may. */
  addRefusal?: string;
}) {
  const { t } = useTranslation("databaseView"), labels = databaseViewLabels();
  const [query, setQuery] = useState("");
  const touch = touchScreen();
  const ref = useRef<HTMLDivElement>(null), search = useRef<HTMLInputElement>(null);
  const shown = views.filter(view => viewDisplayName(view).toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()));
  const leadShown = lead && lead.label.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase());
  const listed: Array<GraphView | typeof LEAD> = [...shown];
  if (leadShown) listed.splice(shown.filter(view => views.indexOf(view) < lead.index).length, 0, LEAD);
  const items: Array<GraphView | typeof LEAD | typeof ADD | typeof SOURCE> = [...listed,
    ...(onAddView ? [ADD as typeof ADD] : []), ...(onAddSource ? [SOURCE as typeof SOURCE] : [])];
  const text = (item: typeof items[number]) => item === LEAD ? lead?.label ?? "" : item === ADD ? labels.newView : item === SOURCE ? t("newDataSource") : viewDisplayName(item);
  // Its search heads the list whenever there are views to choose between, as
  // Notion's (seen 2026-11-14: two views in the folded picker at 1440, three
  // under "1개 더 보기" at 1440 and on an iPad at 834) — but for a wiki's,
  // its Home among them, which has none however many views it holds, from
  // "홈 ⌄" or from a view's own picker (seen 2026-11-16 at 1440: Home and
  // two views, Home and eleven).
  const searchable = !lead && views.length > 1;
  const reason = useHoverTip({ delay: 180, placement: "left" });
  // Its search lights the first view it finds, never "새 보기" under them (searchLights).
  const menu = useNativeMenu({ items, open, menuRef: ref, label, quietOnPointer: true, disabledKeys: addRefusal ? [ADD, SOURCE] : [],
    searchLights: key => key !== ADD && key !== SOURCE,
    focusOwnerRef: searchable ? search : undefined, keyOf: item => typeof item === "string" ? item : item.id, textOf: text,
    onAction: item => item === LEAD ? lead?.onSelect() : item === ADD ? onAddView?.(ref.current?.querySelector<HTMLElement>("[data-view-list-add]") ?? null)
      : item === SOURCE ? onAddSource?.() : onSelect(item) });
  // As it opens its current view is lit, shaded with no ring, and the arrows
  // go on from it: the first ↓ rings the view under it, the first ↑ the one
  // over it (Notion's, seen 2026-11-15 at 1440 in two tabs: V5 current, ↓
  // ringed V6, ↑ V4; V9 current, ↓ V10, ↑ V8).
  const currentKey = lead?.current ? LEAD : views.some(view => view.id === activeId) ? activeId : null;
  const manager = menu.state.selectionManager;
  useLayoutEffect(() => {
    const list = ref.current;
    if (!open || !list || currentKey === null) return;
    list.dataset.searchLit = currentKey;
    manager.setFocused(true); manager.setFocusedKey(currentKey);
  // Once as it opens.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  useLayoutEffect(() => {
    const list = ref.current, row = list?.querySelector<HTMLElement>('[aria-current="true"]');
    if (!open || !list || !row || list.scrollHeight <= list.clientHeight) return;
    // In the list's own pixels as its menu grows into place (placeInScrollBox).
    const end = placeInScrollBox(row, list).bottom + list.scrollTop;
    if (end > list.clientHeight - CURRENT_FADE) list.scrollTop = end - list.clientHeight + CURRENT_ROOM;
  // Once as it opens, not as its search narrows it.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  useEdgeFade(ref, `${open}:${items.length}`, FADE_RATE);
  const leadId = (key: string) => key === LEAD && lead ? lead.id : key;
  const id = useId(), reorder = useDragReorder(`database-view-list:${id}`, (source: string, target: string, side: "before" | "after") =>
    onReorder(leadId(source), leadId(target), side), { live: true });
  const keyOf = (item: GraphView | typeof LEAD | undefined) => item === undefined ? undefined : item === LEAD ? LEAD : item.id;
  const handle = (key: string, name: string) => {
    const at = listed.findIndex(item => keyOf(item) === key);
    return <button type="button" {...reorder.handle(key, true, { above: keyOf(listed[at - 1]), below: keyOf(listed[at + 1]) })}
      className={`${styles.tool} ${styles.handle}`} aria-label={t("reorderView", { name })}
      onClick={event => event.stopPropagation()}><DragHandleIcon size="small" aria-hidden="true" /></button>;
  };
  // Notion's (seen 2026-11-02): every view with its handle and "⋯", the
  // current one ticked, then under a line "새 보기" and "새 데이터 소스".
  return <div className={styles.list}>
    {/* A row the arrows ring takes its ring away, the field keeping the keys typed after them (styles.css, menu search fields). */}
    {searchable && <div className={styles.search}><input {...menu.ownerProps} ref={search} value={query} aria-label={t("searchViews")} placeholder={t("searchViews")}
      onChange={event => setQuery(event.currentTarget.value)} /></div>}
    <div {...menu.menuProps} ref={ref} className={styles.items} data-edge-fade>
    {/* A search that names no view says so over "새 보기", nothing lit (Notion's, seen 2026-11-07 and 2026-11-15). */}
    {query.trim() && shown.length === 0 && !leadShown && <div className={styles.noResults}>{t("viewSearchNoResults")}</div>}
    {items.map(item => item === LEAD ? lead && <div key={LEAD} {...reorder.row(LEAD, () => !readOnly)} className={styles.row} data-view-list-lead>
      {/* Home moves and is renamed as a view is (Notion's, seen 2026-11-11): its handle, and "⋯" with "이름 바꾸기". */}
      {readOnly ? <span className={`${styles.tool} ${styles.handle}`} aria-hidden="true" /> : handle(LEAD, lead.label)}
      <NativeMenuItem itemKey={LEAD} state={menu.state} className={styles.item} aria-current={lead.current ? "true" : undefined}>
        {() => <>{lead.icon}<span className={styles.itemName}>{lead.label}</span>
          {lead.current && <CheckIcon size="small" aria-hidden="true" className={styles.current} />}</>}
      </NativeMenuItem>
      {!readOnly && (lead.onActions ? <button type="button" className={`${styles.tool} ${styles.more}`} aria-label={labels.openActions(lead.label)}
        aria-haspopup="dialog" aria-expanded={lead.actionsOpen === true} data-view-list-lead-actions
        onKeyDown={event => event.stopPropagation()} onClick={event => { event.stopPropagation(); lead.onActions?.(event.currentTarget); }}>
        <DotsHorizontal size="small" aria-hidden="true" /></button> : <span className={styles.tool} aria-hidden="true" />)}
    </div> : typeof item === "string" ? <Fragment key={item}>
      {item === items.find(other => other === ADD || other === SOURCE) && <div role="separator" className={styles.divider} />}
      {/* "새 보기" offers its layouts beside it when chosen, not as the pointer passes (Notion's, seen 2026-11-05). */}
      <NativeMenuItem itemKey={item} state={menu.state} className={`${styles.item} ${styles.action}${item === ADD && touch ? ` ${styles.addRow}` : ""}`}
        data-view-list-add={item === ADD ? "" : undefined}
        disabled={Boolean(addRefusal)} onPointerEnter={addRefusal ? (event) => reason.enter(event.currentTarget, addRefusal) : undefined}
        onPointerLeave={addRefusal ? reason.leave : undefined}>
        {() => item === ADD && touch ? <>
          {/* A touch screen's is two lines under a blue round "+", as Notion's iPad (seen 2026-11-14 at 834) and its phone sheet. */}
          <AddCircleFillIcon size="regular" aria-hidden="true" className={styles.addCircle} />
          <span className={styles.addText}><span className={styles.itemName}>{text(item)}</span><span className={styles.addKinds}>{labels.newViewKinds}</span></span>
        </> : <>{item === ADD ? <Plus size="regular" aria-hidden="true" /> : <SquareOnSquarePlusIcon size="regular" aria-hidden="true" />}<span className={styles.itemName}>{text(item)}</span></>}
      </NativeMenuItem>
    </Fragment> : <div key={item.id} {...reorder.row(item.id, () => !readOnly)} className={styles.row} data-view-list-row={item.id}>
      {!readOnly && handle(item.id, viewDisplayName(item))}
      <NativeMenuItem itemKey={item.id} state={menu.state} className={styles.item} aria-current={item.id === activeId && !lead?.current ? "true" : undefined}>
        {() => <><ViewIcon view={item} size="regular" /><span className={styles.itemName}>{viewDisplayName(item)}</span>
          {item.id === activeId && !lead?.current && <CheckIcon size="small" aria-hidden="true" className={styles.current} />}</>}
      </NativeMenuItem>
      {!readOnly && <button type="button" className={`${styles.tool} ${styles.more}`} aria-label={labels.openActions(viewDisplayName(item))}
        aria-haspopup="dialog" aria-expanded={actionsOpen === item.id}
        onKeyDown={event => event.stopPropagation()} onClick={event => { event.stopPropagation(); onActions(item, event.currentTarget); }}>
        <DotsHorizontal size="small" aria-hidden="true" /></button>}
    </div>)}</div>
    {reason.tip}
  </div>;
}
