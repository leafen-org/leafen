"use client";

import { useRef, useState } from "react";
import { createPortal } from "react-dom";
import type { GraphView, GraphViewType } from "@/contentGraph";
import { ChevronDown, Copy, FileText, LinkIcon, Pencil, SlidersIcon, Trash } from "@/icons/leafen";
import { placeAround, popoverBackdropStyle, popoverPlacementStyle, popoverRectOf, type PopoverRect, usePopoverPlacement } from "@/lib/popoverPlacement";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { DatabaseViewActionChoices, DatabaseViewActionsBeside, DatabaseViewAddBeside, type DatabaseViewAction } from "./DatabaseTabMenus";
import { DatabaseViewList, type ViewListLead } from "./DatabaseViewList";
import { WIKI_HOME_VIEW_ID } from "@/lib/wikiHome";
import { PhoneViewMenus } from "./DatabaseViewPhoneSheets";
import { databaseViewLabels } from "./databaseViewLabels";
import { viewDisplayName } from "./databaseViewShared";
import { WIKI_ADD_VIEW_TYPES, type WikiViewsRequest } from "./wikiViewsContext";
import styles from "./databaseSheet";

type Menu = { kind: "overflow" | "view" | "add"; viewId?: string };

/**
 * A wiki's Home picker: the database's views list as its views show it in
 * the toolbar's row (DatabaseViewTabs), Home ticked at its place. The views
 * and Home are moved and Home renamed here; a view's action or a new view
 * runs once its views show, as the database lives only there
 * (WikiViewsContext's request).
 */
export function WikiHomeViewPicker({ label, homeLabel, homeIndex, views, readOnly, onSelectView, onReorder, onRenameHome, onRequest, onTrigger }: {
  label: string;
  homeLabel: string;
  /** How many views stand before Home. */
  homeIndex: number;
  views: readonly GraphView[];
  readOnly: boolean;
  onSelectView(viewId: string): void;
  /** Moves a row before or after another, Home's (`home`) among them. */
  onReorder(sourceId: string, targetId: string, side: "before" | "after"): void;
  onRenameHome(): void;
  onRequest(request: WikiViewsRequest): void;
  /** Takes the picker's press first; true keeps the views closed. */
  onTrigger?(): boolean;
}) {
  const labels = databaseViewLabels();
  const { phone } = useSidebarLayout(true);
  const [menu, setMenu] = useState<(Menu & { anchor: PopoverRect }) | null>(null);
  const [addRow, setAddRow] = useState<HTMLElement | null>(null);
  const [listActions, setListActions] = useState<{ viewId: string; anchor: HTMLElement } | null>(null);
  const menuRef = useRef<HTMLDivElement>(null), triggerRef = useRef<HTMLButtonElement>(null);
  const close = () => { setMenu(null); setAddRow(null); setListActions(null); };
  const place = usePopoverPlacement(menuRef, menu && !phone ? (size, viewport) => placeAround(menu.anchor, size, viewport, { gap: 4 }) : null, [menu],
    undefined, { kind: "picker", onEscape: close, onOutsidePress: close, returnFocusRef: triggerRef, inside: () => [triggerRef.current] });
  const lead: ViewListLead = { id: WIKI_HOME_VIEW_ID, label: homeLabel, icon: <FileText pixels={20} aria-hidden="true" />, current: true,
    onSelect: close, index: homeIndex, actionsOpen: listActions?.viewId === WIKI_HOME_VIEW_ID,
    onActions: (element) => setListActions((current) => current?.anchor === element ? null : { viewId: WIKI_HOME_VIEW_ID, anchor: element }) };
  // The tab menu's rows a wiki's view keeps (DatabaseViewTabs tabActions).
  const actions = (view: GraphView): DatabaseViewAction[] => {
    const act = (action: string) => () => { close(); onRequest({ viewId: view.id, action }); };
    const others = views.filter(({ dataSourceId }) => dataSourceId === view.dataSourceId).length > 1;
    return [
      { id: "rename", label: labels.renameView, icon: <Pencil size="regular" />, act: act("rename") },
      { id: "edit", label: labels.editView, icon: <SlidersIcon size="regular" />, act: act("edit") },
      { id: "link", label: labels.copyViewLink, icon: <LinkIcon size="regular" />, grouped: true, act: act("link") },
      { id: "duplicate", label: labels.duplicateView, icon: <Copy size="regular" />, grouped: true, act: act("duplicate") },
      ...(others ? [{ id: "delete", label: labels.deleteView, icon: <Trash size="regular" />, danger: true, act: act("delete") }] : []),
    ];
  };
  const select = (view: GraphView) => { close(); onSelectView(view.id); };
  const create = (type: GraphViewType) => { close(); onRequest({ create: type }); };
  const listView = listActions && views.find(({ id }) => id === listActions.viewId);
  const actionView = menu?.viewId ? views.find(({ id }) => id === menu.viewId) : undefined;

  const layer = phone && menu ? (
    <PhoneViewMenus kind={menu.kind} view={actionView} views={views} activeId="" lead={lead} addTypes={WIKI_ADD_VIEW_TYPES}
      titleActions={() => []} viewActions={(view) => readOnly ? [] : actions(view)} onDisplay={() => undefined}
      onSelect={select} onCreate={readOnly ? undefined : create} onClose={close}
      onOpen={(kind, viewId) => setMenu((current) => current && (kind === "overflow" || kind === "view" || kind === "add")
        ? { kind, viewId, anchor: current.anchor } : current)} />
  ) : menu && typeof document !== "undefined" ? createPortal(<>
    <button type="button" className={styles.menuBackdrop} style={popoverBackdropStyle(place)} aria-label={labels.close} tabIndex={-1} />
    <div ref={menuRef} className={`${styles.viewMenu} ${styles.viewTabMenu}`} data-view-overflow-menu="true"
      style={{ position: "fixed", ...popoverPlacementStyle(place) }}>
      <DatabaseViewList views={[...views]} activeId="" lead={lead} label={label} open={Boolean(place)} readOnly={readOnly}
        actionsOpen={listActions?.viewId} onSelect={select} onReorder={onReorder}
        onActions={(view, element) => setListActions((current) => current?.anchor === element ? null : { viewId: view.id, anchor: element })}
        onAddView={readOnly ? undefined : setAddRow} />
      {addRow && <DatabaseViewAddBeside row={addRow} types={WIKI_ADD_VIEW_TYPES} wide onCreate={create} onClose={() => {
        setAddRow(null);
        addRow.focus();
      }} />}
      {listActions && listView && <DatabaseViewActionsBeside key={listView.id} anchor={listActions.anchor} onClose={() => setListActions(null)}>
        {(open) => <DatabaseViewActionChoices open={open} label={labels.openActions(viewDisplayName(listView))} actions={actions(listView)} />}
      </DatabaseViewActionsBeside>}
      {/* Home's "⋯" holds "이름 바꾸기" alone (Notion's, seen 2026-11-11). */}
      {listActions?.viewId === WIKI_HOME_VIEW_ID && <DatabaseViewActionsBeside anchor={listActions.anchor} onClose={() => setListActions(null)}>
        {(open) => <DatabaseViewActionChoices open={open} label={labels.openActions(homeLabel)} actions={[{ id: "rename", label: labels.renameView,
          icon: <Pencil size="regular" />, act: () => { close(); onRenameHome(); } }]} />}
      </DatabaseViewActionsBeside>}
    </div>
  </>, document.body) : null;

  return <>
    <div className={styles.viewTabWrap} data-view-overflow-wrap data-active="true">
      <button ref={triggerRef} type="button" className={`${styles.viewTab} ${styles.viewOverflowButton}`} data-view-picker data-wiki-view-menu-trigger
        data-phone={phone ? "true" : undefined} aria-haspopup="menu" aria-expanded={Boolean(menu)}
        onClick={(event) => {
          if (menu) { close(); return; }
          if (onTrigger?.()) return;
          setMenu({ kind: "overflow", anchor: popoverRectOf(event.currentTarget)! });
        }}>
        <span className={styles.viewGlyph}><FileText pixels={20} aria-hidden="true" /></span>
        <span>{homeLabel}</span><ChevronDown size="small" />
      </button>
    </div>
    {layer}
  </>;
}
