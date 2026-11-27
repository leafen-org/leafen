import { useContext, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { GraphView, GraphViewType } from "@/contentGraph";
import { ChevronLeft, X } from "@/icons/leafen";
import { isDashboardWidget } from "@/lib/dashboardLayout";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { placeInWindow, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import type { ViewType } from "@/lib/types";
import { ViewIcon, ViewTypeIcon } from "./databaseViewShared";
import { databaseViewLabels } from "./databaseViewLabels";
import { dataSourceDisplayTitle } from "../editor/databaseTitles";
import type { DashboardWidgetEditing } from "./DatabaseDashboardWidgets";
import { DatabaseDataSourcesContext } from "./databaseDataSourcesContext";
import styles from "./DatabaseDashboardWidgets.module.css";
import menu from "./DashboardWidgetPanel.module.css";
import { readsRightToLeft } from "@/lib/readingDirection";
import { fieldFocusOnOpen } from "@/lib/screenKind";

/** What a new widget can become, in Notion's order (seen 2026-11-03). */
const WIDGET_TYPES: GraphViewType[] = ["table", "board", "gallery", "list", "chart", "timeline", "feed", "map", "calendar"];
/** The database's views shown before "N개 더 보기". */
const SHOWN_VIEWS = 3;
const PANEL_WIDTH = 290;

type Choice = { key: string; view?: GraphView; type?: GraphViewType; more?: number };

/**
 * "새 보기" beside a widget "+" just made, as Notion's (seen 2026-11-03):
 * the widget starts as a chart; under "〈데이터베이스〉의 조회수" the
 * database's views it can take after (three, then "N개 더 보기", searched
 * once there are more), under "새로운 보기" the layouts it can become.
 * Closing keeps the chart. A widget whose source was asked first has "←"
 * back to that question, to give it another (Notion's, seen 2026-11-12); a
 * source another database owns lists this database's views of it, then that
 * database's own.
 */
export function DashboardWidgetPanel({ editing, widget, onBack, onDone, onRetyped }: {
  editing: DashboardWidgetEditing;
  widget: GraphView;
  /** Back to the widget's source (DashboardSourcePicker). */
  onBack?(): void;
  onDone(): void;
  /** A layout or a view to take after chosen: the widget's settings follow (Notion's, seen 2026-11-07). */
  onRetyped?(widgetId: string, settings: "view" | "newView"): void;
}) {
  const { t } = useTranslation(["databaseView", "common"]);
  const labels = databaseViewLabels();
  const panelRef = useRef<HTMLDivElement>(null), menuRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  // At the window's end edge (its left read from the right), level with the widget.
  const place = usePopoverPlacement(panelRef, (size, viewport) => {
    const frame = popoverRectOf(document.querySelector(`[data-dashboard-widget="${CSS.escape(widget.id)}"]`));
    return placeInWindow({ left: readsRightToLeft() ? 12 : viewport.width - PANEL_WIDTH - 12, top: frame?.top ?? 12 }, size, viewport);
  }, [widget.id], undefined, { kind: "picker", onEscape: onDone, onOutsidePress: onDone });
  const [query, setQuery] = useState("");
  const [expanded, setExpanded] = useState(false);
  const [busy, setBusy] = useState(false);
  const surface = useContext(DatabaseDataSourcesContext);
  const linked = surface?.linked.find(({ dataSourceId }) => dataSourceId === widget.dataSourceId);
  const [sourceViews, setSourceViews] = useState<GraphView[]>([]);
  useEffect(() => {
    setSourceViews([]);
    if (!surface || !linked?.sourceContainerId) return;
    const controller = new AbortController();
    void surface.loadSourceViews({ id: linked.sourceContainerId, pageId: linked.pageId ?? "", pageTitle: "", dataSourceId: linked.dataSourceId,
      viewId: "", viewType: "table", viewName: "", viewConfig: {}, title: linked.title }, undefined, controller.signal)
      .then(({ views }) => { if (!controller.signal.aborted) setSourceViews(views.filter((view) => view.type !== "dashboard" && !isDashboardWidget(view))); })
      .catch(() => undefined);
    return () => controller.abort();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [linked?.sourceContainerId, linked?.dataSourceId]);
  const views = [...editing.views.filter((view) => view.id !== widget.id && view.dataSourceId === widget.dataSourceId), ...sourceViews];
  const searched = query.trim().toLocaleLowerCase();
  const nameOf = (view: GraphView) => view.name || labels.viewTypes[view.type];
  const matching = searched ? views.filter((view) => nameOf(view).toLocaleLowerCase().includes(searched)) : views;
  const shown = expanded || searched ? matching : matching.slice(0, SHOWN_VIEWS);
  const types = WIDGET_TYPES.filter((type) => !searched || labels.viewTypes[type].toLocaleLowerCase().includes(searched));
  const viewChoices: Choice[] = [
    ...shown.map((view) => ({ key: `view:${view.id}`, view })),
    ...(shown.length < matching.length ? [{ key: "more", more: matching.length - shown.length }] : []),
  ];
  const typeChoices: Choice[] = types.map((type) => ({ key: `type:${type}`, type }));
  async function settle(write: Promise<void>) {
    setBusy(true);
    await write;
    onDone();
  }
  // A view taken after gives the widget its layout, name and settings.
  const choose = (choice: Choice) => {
    if (choice.more) { setExpanded(true); return; }
    const dashboardViewId = widget.config.dashboardViewId;
    if (choice.view && sourceViews.includes(choice.view)) {
      // One of the other database's own views, taken after as a new view of its source does (adoptSourceViewOperations).
      void settle(surface!.adoptView(widget.id, choice.view).then(() => undefined)).then(() => onRetyped?.(widget.id, "view"));
    } else if (choice.view) {
      const config = structuredClone(choice.view.config);
      for (const key of ["dashboardRows", "dashboardRowHeights", "dashboardWidgetWidths"]) delete config[key];
      void settle(editing.update({ ...widget, type: choice.view.type, name: choice.view.name, config: { ...config, dashboardViewId } }))
        .then(() => onRetyped?.(widget.id, "view"));
    } else if (choice.type) {
      void settle(editing.retype(widget, choice.type)).then(() => onRetyped?.(widget.id, "newView"));
    }
  };
  const choices = [...viewChoices, ...typeChoices];
  const picker = useNativeMenu({ items: choices, open: true, menuRef, label: t("newView"), keyOf: (choice) => choice.key,
    textOf: (choice) => choice.view ? nameOf(choice.view) : choice.type ? labels.viewTypes[choice.type] : t("dashboardWidgetMoreViews", { count: choice.more }),
    onAction: choose, disabledKeys: busy ? choices.map(({ key }) => key) : [], focusOnOpen: !views.length || views.length <= SHOWN_VIEWS,
    focusOwnerRef: views.length > SHOWN_VIEWS ? searchRef : undefined });
  const sourceName = editing.sources.find(({ id }) => id === widget.dataSourceId)?.name ?? dataSourceDisplayTitle("");
  const item = (choice: Choice) => (
    <NativeMenuItem key={choice.key} state={picker.state} itemKey={choice.key} className={menu.panelChoice}
      data-dashboard-widget-view={choice.view?.id} data-dashboard-widget-type={choice.type}>
      {({ labelProps }) => <>
        {choice.view ? <ViewIcon view={choice.view} size="regular" /> : choice.type ? <ViewTypeIcon type={choice.type as ViewType} size="regular" />
          : <span className={menu.panelMore} aria-hidden="true">···</span>}
        <span {...labelProps}>{choice.view ? nameOf(choice.view) : choice.type ? labels.viewTypes[choice.type] : t("dashboardWidgetMoreViews", { count: choice.more })}</span>
      </>}
    </NativeMenuItem>
  );
  return createPortal((
    <div ref={panelRef} className={styles.panel} style={popoverPlacementStyle(place)} role="dialog" aria-label={t("newView")}
      data-dashboard-widget-panel={widget.id}>
      <div className={styles.panelHead}>
        <span className={styles.panelTitle}>
          {onBack ? <button type="button" className={styles.panelBack} aria-label={t("common:actions.back")} disabled={busy}
            data-dashboard-widget-back onClick={onBack}><ChevronLeft size="small" /></button> : null}
          <span>{t("newView")}</span>
        </span>
        <button type="button" aria-label={t("close")} onClick={onDone}><X size="tiny" /></button>
      </div>
      {views.length > SHOWN_VIEWS && (
        <input ref={searchRef} className={styles.panelSearch} type="search" value={query} placeholder={labels.searchDashboardViews}
          aria-label={labels.searchDashboardViews} autoFocus={fieldFocusOnOpen()} onChange={(event) => setQuery(event.currentTarget.value)} />
      )}
      <div {...picker.menuProps} ref={menuRef} className={menu.panelMenu}>
        {viewChoices.length > 0 && <div className={menu.panelGroup}>
          <div className={menu.panelSection}>{t("sourceViewsOf", { source: sourceName })}</div>
          {viewChoices.map(item)}
        </div>}
        {typeChoices.length > 0 && <div className={menu.panelGroup}>
          <div className={menu.panelSection}>{labels.newDashboardViewSection}</div>
          {typeChoices.map(item)}
        </div>}
      </div>
    </div>
  ), document.body);
}
