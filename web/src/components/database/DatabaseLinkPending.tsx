import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { GraphDataSource, GraphView, GraphViewType } from "@/contentGraph";
import { useInlineDatabaseRequests } from "@/lib/inlineDatabaseRequests";
import { placeAround, POPOVER_MARGIN, popoverPlacementStyle, popoverRectOf, type PopoverRect, usePopoverPlacement } from "@/lib/popoverPlacement";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { ChevronDown, FolderIcon, PaintBrushIcon, Plus, Trash } from "@/icons/leafen";
import { QuestionCircleIcon } from "@/icons/leafen/automationIcons";
import { databaseViewLabels } from "./databaseViewLabels";
import { NOTION_2023_VIEW_TYPES, ViewIcon, viewDisplayName } from "./databaseViewShared";
import { useViewTabDisplay } from "./databaseViewTabDisplay";
import { DatabaseAddViewChoices, type DatabaseViewAction } from "./DatabaseTabMenus";
import { PhoneViewListSheet, PhoneViewMenus } from "./DatabaseViewPhoneSheets";
import styles from "./DatabaseLinkPending.module.css";
import sheet from "./databaseSheet";
import { ViewBlockNameEditor, viewBlockNameAt } from "./ViewBlockNameEditor";

// The layouts a waiting view can take, as Notion's "새 보기 추가" beside its tabs and on a phone (no form).
const PENDING_VIEW_TYPES: GraphViewType[] = NOTION_2023_VIEW_TYPES.map(({ type }) => type).filter((type) => type !== "form");

/** A waiting link's own name and icon, kept on the source it stands on until it has one. */
export type DatabaseLinkPendingName = {
  blockId: string; workspaceId: string; pageId: string; source: GraphDataSource;
  /** Drawn alone (in a peek, on its own page): the copy that answers while a peek draws it (shownBlockCopy). */
  alone?: boolean;
  onTitleChange(title: string): void;
  onIconChange(icon: string, iconType: NonNullable<GraphDataSource["iconType"]>): void;
};

/**
 * A linked view still waiting for its data source, as Notion's "데이터 소스
 * 보기 연결" leaves it (seen 2026-11-02 at 1280px): its one tab, then a
 * folder, "새 표" and "데이터 소스 연결", which offers the sources again.
 * Its block menu's "이름 바꾸기" and "아이콘 편집" open over its tab, as a
 * view block's (seen 2026-11-09). It may hold several waiting views, a tab
 * each ("표|보드"), the chosen one's a chip, its "새 〈layout〉" under them;
 * the "+" after them, drawn while the pointer is on their row (CSS), opens
 * "새 보기 추가", its ten layouts five to a row (484px, seen 2026-11-11),
 * whose layout adds another and then offers the sources, as "새 데이터
 * 소스" does at once. On a phone its tab is a pill "표 ⌄" that opens the
 * sheet "N개 보기", the chosen view ticked, each with
 * its "⋯" ("보기 옵션": "다음과 같이 표시 ›", "보기 삭제" in red), then "새
 * 보기", whose layouts add another waiting view and then offer the sources,
 * and "새 데이터 소스", which offers them at once (Notion's, seen 2026-11-11
 * at 402pt in iOS Safari).
 */
export function DatabaseLinkPending({ view, views = [view], readOnly, onConnect, onDrawn, name, onSelect, onAddView, onDeleteView }: {
  view: GraphView; readOnly: boolean; onConnect(): void; name?: DatabaseLinkPendingName;
  /** The block's waiting views in tab order, `view` the chosen one. */
  views?: readonly GraphView[];
  onSelect?(view: GraphView): void;
  /** Adds another waiting view in the layout, then offers the sources. */
  onAddView?(type: GraphViewType): void;
  onDeleteView?(view: GraphView): void;
  /** Told once its tab row is drawn, which a panel can hang under. */
  onDrawn?(): void;
}) {
  const { t } = useTranslation(["databaseView", "propertyTypeConfig"]);
  const frame = useRef<HTMLDivElement>(null), tabs = useRef<HTMLDivElement>(null);
  const [editing, setEditing] = useState<{ kind: "rename" | "icon"; at: { top: number; left: number } } | null>(null);
  const { phone } = useSidebarLayout(true);
  const [menu, setMenu] = useState<{ kind: "overflow" | "view" | "add"; viewId?: string } | null>(null);
  const [display, setDisplay] = useViewTabDisplay(view.dataSourceId);
  const labels = databaseViewLabels();
  const actionsFor = (candidate: GraphView): DatabaseViewAction[] => readOnly ? [] : [
    { id: "display", label: labels.showTabsAs, icon: <PaintBrushIcon size="regular" />, next: true, act: () => undefined },
    ...(onDeleteView && views.length > 1 ? [{ id: "delete", label: labels.deleteView, icon: <Trash size="regular" />, danger: true, grouped: true,
      act: () => { setMenu(null); onDeleteView(candidate); } }] : []),
  ];
  // "+" beside the tabs, under it or over it where only that side has room.
  const [adding, setAdding] = useState<PopoverRect | null>(null);
  const addRef = useRef<HTMLDivElement>(null), plusRef = useRef<HTMLButtonElement>(null);
  const addPlace = usePopoverPlacement(addRef, adding ? (size, viewport) => placeAround(adding, size, viewport, { gap: 4 }) : null, [adding],
    undefined, { kind: "picker", onEscape: () => setAdding(null), onOutsidePress: () => setAdding(null), returnFocusRef: plusRef,
      inside: () => [plusRef.current] });
  const tabFace = (candidate: GraphView) => <>
    {display === "text" ? null : <ViewIcon view={candidate} size="regular" />}
    {display === "icon" ? null : viewDisplayName(candidate)}
  </>;
  // Once, as it is first drawn.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => { onDrawn?.(); }, []);
  useInlineDatabaseRequests(name?.blockId ?? "", (request) => {
    const block = frame.current?.getBoundingClientRect(), row = tabs.current?.getBoundingClientRect();
    if ((request === "rename" || request === "icon") && !readOnly && block && row) {
      setEditing({ kind: request, at: viewBlockNameAt(block.left, row.bottom, POPOVER_MARGIN) });
    }
  }, name ? { blockId: name.blockId, alone: name.alone === true } : undefined);
  return (
    <div ref={frame} className={`${styles.frame} inline-database`} data-inline-database-wrapper data-database-link-pending contentEditable={false}>
      {editing && name ? <ViewBlockNameEditor editing={editing.kind} at={editing.at} blockId={name.blockId} workspaceId={name.workspaceId}
        uploadPageId={name.pageId} title={name.source.title} icon={name.source.icon ?? ""} iconType={name.source.iconType ?? "none"}
        onTitleChange={name.onTitleChange} onIconChange={name.onIconChange} onClose={() => setEditing(null)} /> : null}
      <div ref={tabs} className={styles.tabs} data-database-link-pending-tabs>
        {phone ? (
          <button type="button" className={styles.tab} aria-haspopup="dialog" aria-expanded={menu !== null} data-database-link-pending-views
            onClick={() => setMenu({ kind: "overflow" })}>
            {tabFace(view)}<ChevronDown size="small" />
          </button>
        ) : views.map((candidate) => candidate.id === view.id
          ? <span key={candidate.id} className={styles.tab} data-database-link-pending-view={candidate.id}>{tabFace(candidate)}</span>
          : <button key={candidate.id} type="button" className={styles.tab} data-chosen="false" data-database-link-pending-view={candidate.id}
            onClick={() => onSelect?.(candidate)}>{tabFace(candidate)}</button>)}
        {!phone && !readOnly && onAddView ? (
          <button ref={plusRef} type="button" className={styles.plus} aria-label={labels.addNewView} aria-haspopup="dialog" aria-expanded={adding !== null}
            data-database-link-pending-add onClick={(event) => setAdding(adding ? null : popoverRectOf(event.currentTarget))}>
            <Plus size="small" aria-hidden="true" />
          </button>
        ) : null}
      </div>
      {adding && onAddView ? createPortal(
        <div ref={addRef} className={`${sheet.viewMenu} ${sheet.titleAddViewMenuPanel}`} data-wide="true" style={{ position: "fixed", ...popoverPlacementStyle(addPlace) }}>
          <DatabaseAddViewChoices types={PENDING_VIEW_TYPES} heading={labels.addNewView} className={sheet.titleAddViewGrid}
            itemClassName={sheet.inlineAddViewType} open={Boolean(addPlace)} onCreate={(type) => { setAdding(null); onAddView(type); }}
            onAddSource={() => { setAdding(null); onConnect(); }} />
        </div>, document.body) : null}
      {menu?.kind === "overflow" ? <PhoneViewListSheet views={views} activeId={view.id}
        onSelect={(candidate) => { setMenu(null); onSelect?.(candidate); }}
        onActions={readOnly ? undefined : (candidate) => setMenu({ kind: "view", viewId: candidate.id })}
        onNewView={readOnly || !onAddView ? undefined : () => setMenu({ kind: "add" })}
        onAddSource={readOnly ? undefined : () => { setMenu(null); onConnect(); }} onClose={() => setMenu(null)} /> : null}
      {/* "새 보기 추가" lists the layouts alone, without "새 데이터 소스" (Notion's, seen 2026-11-11). */}
      {menu && menu.kind !== "overflow" ? <PhoneViewMenus kind={menu.kind} view={views.find(({ id }) => id === menu.viewId)} views={views}
        activeId={view.id} addTypes={PENDING_VIEW_TYPES} titleActions={() => []} viewActions={actionsFor} onDisplay={setDisplay}
        onSelect={(candidate) => { setMenu(null); onSelect?.(candidate); }}
        onCreate={onAddView ? (type) => { setMenu(null); onAddView(type); } : undefined}
        onOpen={(kind, viewId) => setMenu(kind === "view" || kind === "add" || kind === "overflow" ? { kind, viewId } : null)}
        onClose={() => setMenu(null)} /> : null}
      <div className={styles.empty}>
        <span className={styles.mark} aria-hidden="true"><FolderIcon size="large" /><QuestionCircleIcon size="small" weight="fill" /></span>
        <span className={styles.title}>{view.type === "table" ? t("databaseView:linkPendingTitle")
          : t("databaseView:linkPendingTitleOf", { layout: labels.viewTypes[view.type] })}</span>
        {readOnly ? null : (
          <button type="button" className={styles.connect} onClick={onConnect} data-database-link-connect>
            {t("propertyTypeConfig:connectDataSource")}
          </button>
        )}
      </div>
    </div>
  );
}
