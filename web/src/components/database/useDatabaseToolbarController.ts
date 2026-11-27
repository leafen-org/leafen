import { type RefObject, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useStateUnlessSame } from "@/lib/useStateUnlessSame";
import type {
  DatabaseToolbarTaskFeatureWire,
  DatabaseToolbarTemplateMetadataWire,
  DatabaseToolbarTemplateRepeatWire,
} from "@/databaseToolbarConfigWire.generated";
import type {
  GraphDataSource,
  GraphProperty,
  GraphRow,
  GraphView,
} from "@/contentGraph";
import { createListOperationCoordinator } from "./listViewModel";
import { placeAround, popoverBackdropStyle, type PopoverRect, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { scrollOpenerUpForPanel } from "@/lib/openerScroll";
import type { OverlayLayerOptions } from "@/lib/overlayLayer";
import { placeAtLinkedViewWindow, type LinkedViewWindowPlace } from "@/lib/linkedViewWindowPlace";
import { placeUnderTabPill, usePageLayoutTabPill } from "./pageLayoutTabPill";
import {
  toolbarListFit,
  toolbarMenuColumn,
  type DatabaseToolbarMenu,
} from "./databaseToolbarModel";
import type { DatabaseFilterSnapshot } from "./databaseFilterSnapshot";
import type { DatabaseTemplatePropertyCells } from "./DatabaseTemplateProperties";
import { focusOpener } from "@/lib/overlayLayer";
import { pressedByFinger } from "@/lib/lastPress";
import { inlineEndX, readsRightToLeft } from "@/lib/readingDirection";

export interface DatabaseToolbarSnapshot {
  snapshotIdentity: string;
  contextIdentity: string;
  /** Who and which view, without its source: the view settings stay open while the view turns to another source. */
  menuIdentity?: string;
  /** Who and which database, without its view: the view settings stay open while another view is chosen. */
  settingsIdentity?: string;
  requestGeneration: number;
  workspaceId: string;
  pageId: string;
  dataSource: GraphDataSource;
  activeView: GraphView;
  views: readonly GraphView[];
  properties: readonly GraphProperty[];
  rows: readonly GraphRow[];
  filterSnapshot: DatabaseFilterSnapshot;
  templates: readonly DatabaseToolbarTemplateMetadataWire[];
  /** False while its templates aren't read yet: `templates` then holds none of the database's. */
  templatesRead?: boolean;
  taskConfiguration: DatabaseToolbarTaskFeatureWire;
  writable: boolean;
  /** The cells a template's property values edit with. */
  templatePropertyCells: DatabaseTemplatePropertyCells;
}

export interface DatabaseToolbarOperationContext {
  requestKey: string;
  signal: AbortSignal;
  snapshotIdentity: string;
  contextIdentity: string;
  requestGeneration: number;
  dataSourceId: string;
  viewId: string;
}

export interface DatabaseToolbarOperations {
  isCurrent(context: DatabaseToolbarOperationContext): boolean;
  updateView(view: GraphView, context: DatabaseToolbarOperationContext): Promise<void>;
  /** Turns the view into a dashboard whose first widget is the view as it was. */
  turnIntoDashboard?(view: GraphView, context: DatabaseToolbarOperationContext): Promise<void>;
  /** Turns a dashboard into the given layout, its widgets gone. */
  leaveDashboard?(dashboard: GraphView, next: GraphView, context: DatabaseToolbarOperationContext): Promise<void>;
  commitProperties(properties: readonly GraphProperty[], context: DatabaseToolbarOperationContext): Promise<void>;
  /** Moves properties to the database's deleted properties, from where they can be restored. */
  deleteProperties?(propertyIds: readonly string[], context: DatabaseToolbarOperationContext): Promise<void>;
  /** Whether the server took the change, saying why it didn't itself. */
  configureTaskFeatures(taskConfiguration: DatabaseToolbarTaskFeatureWire, context: DatabaseToolbarOperationContext,
    madeProperties?: readonly GraphProperty[]): Promise<boolean>;
  createRow(templateId: string | undefined, context: DatabaseToolbarOperationContext): Promise<GraphRow | null>;
  createTemplate(input: { id: string; name: string; position: string }, context: DatabaseToolbarOperationContext): Promise<void>;
  duplicateTemplate(templateId: string, targetTemplateId: string, context: DatabaseToolbarOperationContext): Promise<void>;
  updateTemplate(templateId: string, patch: Partial<DatabaseToolbarTemplateMetadataWire>, context: DatabaseToolbarOperationContext): Promise<void>;
  deleteTemplate(templateId: string, context: DatabaseToolbarOperationContext): Promise<void>;
  /** A deleted template's trash banner, back to the menu or gone for good. */
  restoreTemplate(templateId: string, context: DatabaseToolbarOperationContext): Promise<void>;
  deleteTemplateForever(templateId: string, context: DatabaseToolbarOperationContext): Promise<void>;
  setDefaultTemplate(templateId: string, isDefault: boolean, context: DatabaseToolbarOperationContext): Promise<void>;
  setTemplateRepeat(
    templateId: string,
    repeat: DatabaseToolbarTemplateRepeatWire | null,
    context: DatabaseToolbarOperationContext,
  ): Promise<void>;
  onError(error: unknown, context: DatabaseToolbarOperationContext): void;
}

function contextFor(
  snapshot: DatabaseToolbarSnapshot,
  requestKey: string,
  controller: AbortController,
): DatabaseToolbarOperationContext {
  return {
    requestKey,
    signal: controller.signal,
    snapshotIdentity: snapshot.snapshotIdentity,
    contextIdentity: snapshot.contextIdentity,
    requestGeneration: snapshot.requestGeneration,
    dataSourceId: snapshot.dataSource.id,
    viewId: snapshot.activeView.id,
  };
}

/** An element's own toolbar, or the toolbar of the database it is in (a new
 * view's "+" beside the title or among the tabs, a table's column). */
export function databaseToolbarOf(element: Element) {
  return element.closest<HTMLElement>("[data-database-toolbar]")
    ?? element.closest("[data-database-view-embedded], [data-database-view]")?.querySelector<HTMLElement>("[data-database-toolbar]") ?? null;
}

// Where the toolbar's buttons end, the right edge of its last shown one.
export function toolbarButtonsEnd(trigger: HTMLElement) {
  const toolbar = databaseToolbarOf(trigger);
  const rtl = readsRightToLeft(trigger);
  const ends = toolbar
    ? [...toolbar.querySelectorAll("button")].map((button) => button.getBoundingClientRect()).filter((rect) => rect.width > 0).map((rect) => inlineEndX(rect, 0, trigger))
    : [];
  return ends.length ? (rtl ? Math.min(...ends) : Math.max(...ends)) : undefined;
}

export function useDatabaseToolbarController(
  snapshot: DatabaseToolbarSnapshot,
  operations: DatabaseToolbarOperations,
  /** Whether a menu opens as a panel docked at the page's end (DatabaseToolbarMenus settingsDocked), read as it opens. */
  docks?: RefObject<((menu: DatabaseToolbarMenu) => boolean) | null>,
) {
  const [openMenu, setOpenMenu] = useState<DatabaseToolbarMenu | null>(null);
  // The menu the open one was led to from, as the view settings lead to automations, whose "←" goes back.
  const [menuFrom, setMenuFrom] = useState<DatabaseToolbarMenu | null>(null);
  const [inputModality, setInputModality] = useState<"pointer" | "keyboard">("pointer");
  const [menuAnchor, setMenuAnchor] = useState<{
    rect: PopoverRect;
    column: ReturnType<typeof toolbarMenuColumn>;
    /** Where a window before it stood, which it takes (a linked view's settings after its "새 보기"). */
    at?: LinkedViewWindowPlace;
    /** It opens as a panel docked at the page's end. */
    docked?: boolean;
    /** Its button's top as it was pressed, measured then. */
    pressedTop: number;
  } | null>(null);
  const [search, setSearch] = useState("");
  const [busyKey, setBusyKey] = useStateUnlessSame<string | null>(null);
  const activeButtonRef = useRef<HTMLElement | null>(null);
  const restoreFocusOnClose = useRef(false);
  // Whether its button takes the focus back as it closes: not where it was
  // opened for something else, at its button's place (a property's settings
  // from its column or a new relation), which leaves it on the page with no
  // ring on the button, as Notion's (seen 2026-11-14: BODY after Escape).
  const openerTakesFocus = useRef(true);
  const menuRef = useRef<HTMLDivElement>(null);
  // Under its button where it fits, else over it, else cut on the roomier
  // side; across, where its column puts it. The automations' rail and the
  // templates' menu are layers of their own, not drawn here.
  const popoverMenu = openMenu !== null && openMenu !== "automations" && openMenu !== "templates";
  // A view's settings docked at a database's own page's right lie beside the
  // page, without the shade a popover has: a press on the page closes them
  // and goes on to what it pressed, as Notion's (seen 2026-11-11: the page's
  // "⋯" opened its menu, a date cell its date, the panel gone at once).
  // Known as they open, they are not placed as a popover is: placed, the
  // panel was measured in the press that opened it, and its place, its
  // settling and then its being docked drew the toolbar and the panel again
  // three times (2026-11-13, NAS condition, CPU ×2). Their layer is their
  // box's (DatabaseViewSettingsPanel's): held here, its taking a place in the
  // layers drew the toolbar and the panel's 157 parts once more as they
  // opened (2026-11-14).
  const dockedPanel = (openMenu === "view" || openMenu === "newView") && menuAnchor?.docked === true;
  // A layout builder's tab has its view's settings under its pill (pageLayoutTabPill).
  const tabPill = usePageLayoutTabPill();
  const pillOf = (menu: DatabaseToolbarMenu | null) => menu === "view" ? tabPill?.() ?? null : null;
  const layer = {
    // Opened from a builder tab's pill (its button unseen), they take the
    // focus at their first control, so Escape closes them at once, as
    // Notion's (seen 2026-11-12, as the tab is added and its pill pressed again).
    initialFocus: pillOf(openMenu) ? "first" as const : undefined,
    onEscape: () => closeMenu(),
    // A press on the shade gives the focus back to the button, ringless
    // until a key comes, as Notion's "정렬" (seen 2026-11-15 twice in fresh
    // Chrome tabs, judged after the press: no ring, a typed letter landing
    // nowhere and ringing "정렬"). A docked panel's press goes on to what it
    // pressed, which takes the focus; a finger's leaves it on the page.
    onOutsidePress: (openMenu === "view" || openMenu === "newView") && !dockedPanel ? undefined
      : () => closeMenu(!dockedPanel && !pressedByFinger()),
    pressGoesOn: dockedPanel,
    // A phone's sheet opens its choices in sheets of their own over it (a
    // sort's direction, a filter's condition); pressing in them, or on their
    // shade, stays in the menu. A docked panel's database's other view tabs
    // choose their view with the panel kept open (Notion's, seen 2026-11-18, settingsScreenAcrossViews.ts).
    inside: () => [activeButtonRef.current, ...document.querySelectorAll("[data-phone-sheet], [data-phone-sheet-menu], [data-phone-sheet-shade]"),
      ...(dockedPanel && openMenu === "view" ? activeButtonRef.current?.closest("[data-database-chrome]")?.querySelectorAll('[role="tab"]:not([aria-selected="true"])') ?? [] : [])],
    // Its shade, drawn just before it, closes it from over a modal too (a layout builder's tab).
    modalOwned: () => [menuRef.current?.previousElementSibling?.closest("[data-toolbar-menu-backdrop]")],
  };
  const dockedLayer: Omit<OverlayLayerOptions, "ref"> | null = popoverMenu && dockedPanel ? { kind: "side-peek", floats: true, closesAfterClick: false, ...layer } : null;
  const menuPlace = usePopoverPlacement(menuRef, popoverMenu && menuAnchor && !dockedPanel ? (size, viewport) => {
    if (menuAnchor.at) return placeAtLinkedViewWindow(menuAnchor.at, size, viewport, toolbarListFit(viewport.height, true).cap);
    const pill = pillOf(openMenu);
    if (pill) return placeUnderTabPill(pill, viewport);
    const rect = popoverRectOf(activeButtonRef.current) ?? menuAnchor.rect;
    const column = toolbarMenuColumn(rect, openMenu!, viewport.width,
      (openMenu === "view" || openMenu === "newView") && activeButtonRef.current ? toolbarButtonsEnd(activeButtonRef.current) : undefined,
      snapshot.activeView.type, readsRightToLeft(activeButtonRef.current));
    // A view's settings and the filter's and sort's lists show whole up to four fifths of the
    // window (toolbarListFit), 12px and 8px over the window's foot (column.margin).
    const settings = openMenu === "view" || openMenu === "newView";
    const fit = settings || openMenu === "filter" || openMenu === "sort" ? toolbarListFit(viewport.height, settings) : undefined;
    // Risen, a view's settings stand at the whole pixel nearest 13 over the window's foot: Notion's box goes 12
    // over it and its panel is drawn a pixel above its box, as under its button (seen 2026-11-22 at 1440, dpr 1,
    // six times in 643–779 windows: 554.8 high at 154..708.8 in a 722 one, 155 in 723, 211 in 779; capped at
    // 577.6 at 131 in 722, 514.4 at 116 in 643 — round(height of window − 12 − its height) − 1).
    const drawn = Math.min(size.height, fit?.cap ?? Infinity), risen = Math.round(viewport.height - 13 - drawn);
    const at = settings && rect.bottom + column.gap + drawn > viewport.height - 13 ? { top: risen, bottom: risen } : {};
    return placeAround({ ...rect, ...at, left: column.left, right: column.left }, size, viewport,
      { gap: "top" in at ? 0 : column.gap, margin: column.margin, ...fit });
  } : null, [openMenu, menuAnchor], undefined, { kind: "menu", restoreFocus: false, ...layer });
  // A view's settings opened low on the page bring their button up first, as Notion's (openerScroll.ts):
  // from where it was pressed, one high on the page measures nothing. Its
  // place is the number taken at the press: the live rect read it again
  // here, with the panel just drawn, and worked out the panel's styles and
  // laid it out before its first frame (2026-11-14, 180 elements, 6 ms,
  // NAS condition, CPU ×2).
  const rises = openMenu === "view" || openMenu === "newView";
  useLayoutEffect(() => {
    if (rises && menuAnchor && !menuAnchor.at && !pillOf(openMenu)) {
      scrollOpenerUpForPanel(activeButtonRef.current, () => menuRef.current?.scrollHeight ?? 0, 12, { top: menuAnchor.pressedTop });
    }
  }, [menuAnchor, rises]);
  const menuStyle = useMemo<React.CSSProperties | undefined>(() => menuAnchor ? {
    position: "fixed",
    right: "auto",
    width: menuAnchor.column.width,
    ...(dockedPanel ? {} : popoverPlacementStyle(menuPlace)),
  } : undefined, [menuAnchor, menuPlace, dockedPanel]);
  const menuBackdropStyle = useMemo(() => popoverBackdropStyle(menuPlace), [menuPlace]);
  const liveSnapshot = useRef(snapshot);
  const coordinator = useRef(createListOperationCoordinator()).current;
  const controllers = useRef(new Set<AbortController>());
  liveSnapshot.current = snapshot;

  const retirePendingActions = useCallback(() => {
    for (const controller of controllers.current) controller.abort();
    controllers.current.clear();
  }, []);

  // A view turned to another source keeps its settings open, as Notion's
  // (seen 2026-10-11), and so does another view chosen in the same database,
  // the settings then the chosen view's (Notion's, seen 2026-11-18,
  // settingsScreenAcrossViews.ts) — a view of a source linked from another
  // database or of the database's second source too, the database being the
  // block on its page whatever page its source keeps (Notion's, seen
  // 2026-11-18 in two tabs: "데이터베이스" and "데이터 소스 관리" left open);
  // anything else moving the toolbar closes its menus.
  const menuIdentity = snapshot.menuIdentity ?? snapshot.contextIdentity;
  const settingsIdentity = snapshot.settingsIdentity ?? menuIdentity;
  const lastIdentity = useRef({ menu: menuIdentity, settings: settingsIdentity });
  useEffect(() => {
    const sameView = lastIdentity.current.menu === menuIdentity, sameDatabase = lastIdentity.current.settings === settingsIdentity;
    lastIdentity.current = { menu: menuIdentity, settings: settingsIdentity };
    setOpenMenu((current) => (sameView && current === "newView") || (sameDatabase && current === "view") ? current : null);
    setBusyKey(null);
    retirePendingActions();
  }, [menuIdentity, settingsIdentity, retirePendingActions, snapshot.contextIdentity]);

  useEffect(() => {
    setBusyKey(null);
    retirePendingActions();
  }, [retirePendingActions, snapshot.snapshotIdentity, snapshot.requestGeneration]);

  useEffect(() => () => retirePendingActions(), [retirePendingActions]);

  useEffect(() => {
    if (openMenu !== null || !restoreFocusOnClose.current) return;
    restoreFocusOnClose.current = false;
    if (!openerTakesFocus.current) return;
    const returnTo = activeButtonRef.current;
    // An unseen settings button (a builder tab's) gives it to the tab's pill.
    const shown = returnTo && !returnTo.offsetParent && tabPill ? tabPill() : returnTo;
    const frame = window.requestAnimationFrame(() => {
      if (activeButtonRef.current === returnTo) focusOpener(shown);
    });
    return () => window.cancelAnimationFrame(frame);
  }, [openMenu, tabPill]);

  const closeMenu = useCallback((restoreFocus = true) => {
    restoreFocusOnClose.current = restoreFocus;
    setOpenMenu(null);
  }, []);

  const toggleMenu = useCallback((
    menu: DatabaseToolbarMenu,
    trigger: HTMLElement,
    modality: "pointer" | "keyboard",
    at?: LinkedViewWindowPlace,
    /** Opened at its button for something else, which keeps the focus back from it. */
    forOther = false,
  ) => {
    if (openMenu === menu) {
      closeMenu();
      return;
    }
    restoreFocusOnClose.current = false;
    openerTakesFocus.current = !forOther;
    activeButtonRef.current = trigger;
    setInputModality(modality);
    const toolbarEnd = menu === "view" ? toolbarButtonsEnd(trigger) : undefined;
    const rect = popoverRectOf(trigger)!;
    setMenuAnchor({ rect, column: toolbarMenuColumn(rect, menu, window.innerWidth, toolbarEnd, undefined, readsRightToLeft(trigger)), at,
      docked: docks?.current?.(menu) === true, pressedTop: rect.top });
    setOpenMenu(menu);
    setMenuFrom(null);
  }, [closeMenu, openMenu]);

  // A menu the open one leads to, as the view settings lead to automations,
  // opens in its place from the same button.
  const switchMenu = useCallback((menu: DatabaseToolbarMenu) => {
    const trigger = activeButtonRef.current;
    if (!trigger?.isConnected || openMenu === menu) return;
    toggleMenu(menu, trigger, inputModality, undefined, !openerTakesFocus.current);
    setMenuFrom(openMenu);
  }, [inputModality, openMenu, toggleMenu]);

  const isCurrent = useCallback((context: DatabaseToolbarOperationContext) => {
    const current = liveSnapshot.current;
    return !context.signal.aborted
      && context.contextIdentity === current.contextIdentity
      && context.snapshotIdentity === current.snapshotIdentity
      && context.requestGeneration === current.requestGeneration
      && context.dataSourceId === current.dataSource.id
      && context.viewId === current.activeView.id
      && operations.isCurrent(context);
  }, [operations]);

  // A write (`write`) is never refused for another of its key still saving; a press that makes something runs once (listViewModel.ts).
  const run = useCallback(async <Result,>(
    requestKey: string,
    perform: (context: DatabaseToolbarOperationContext) => Promise<Result>,
    write = false,
  ) => {
    if (!liveSnapshot.current.writable) return false;
    const controller = new AbortController();
    controllers.current.add(controller);
    const context = contextFor(liveSnapshot.current, requestKey, controller);
    setBusyKey(requestKey);
    const handlers = { isCurrent, onError: (error: unknown, context: DatabaseToolbarOperationContext) => operations.onError(error, context), perform, publish: () => {} };
    const started = await (write ? coordinator.write(context, handlers) : coordinator.run(context, handlers));
    controllers.current.delete(controller);
    if (isCurrent(context)) setBusyKey((current) => current === requestKey ? null : current);
    return started;
  }, [coordinator, isCurrent, operations]);

  const actions = useMemo(() => ({
    updateView: (view: GraphView) => run(
      `view:${view.id}`,
      (context) => operations.updateView(view, context),
      true,
    ),
    turnIntoDashboard: (view: GraphView) => run(
      `view:${view.id}`,
      (context) => operations.turnIntoDashboard?.(view, context) ?? Promise.resolve(),
      true,
    ),
    leaveDashboard: (dashboard: GraphView, next: GraphView) => run(
      `view:${dashboard.id}`,
      (context) => operations.leaveDashboard?.(dashboard, next, context) ?? Promise.resolve(),
      true,
    ),
    commitProperties: (properties: readonly GraphProperty[]) => run(
      `properties:${snapshot.dataSource.id}`,
      (context) => operations.commitProperties(properties, context),
      true,
    ),
    deleteProperty: (propertyId: string) => run(
      `property-delete:${propertyId}`,
      (context) => operations.deleteProperties?.([propertyId], context) ?? Promise.resolve(),
      true,
    ),
    // Says whether the change took, which a screen moving on after it goes by.
    // With the properties a task database's conversion makes or retypes, in one request (useTaskDatabaseConversion).
    configureTaskFeatures: async (taskConfiguration: DatabaseToolbarTaskFeatureWire, madeProperties?: readonly GraphProperty[]) => {
      let saved = false;
      await run(`tasks:${snapshot.dataSource.id}`, async (context) => {
        saved = await operations.configureTaskFeatures(taskConfiguration, context, madeProperties);
      }, true);
      return saved;
    },
    createRow: (templateId?: string) => run(
      `row:create:${snapshot.dataSource.id}`,
      (context) => operations.createRow(templateId, context),
    ),
    createTemplate: (input: { id: string; name: string; position: string }) => run(
      `template:create:${snapshot.dataSource.id}`,
      (context) => operations.createTemplate(input, context),
    ),
    duplicateTemplate: (templateId: string, targetTemplateId: string) => run(
      `template:duplicate:${templateId}`,
      (context) => operations.duplicateTemplate(templateId, targetTemplateId, context),
    ),
    updateTemplate: (templateId: string, patch: Partial<DatabaseToolbarTemplateMetadataWire>) => run(
      `template:update:${templateId}`,
      (context) => operations.updateTemplate(templateId, patch, context),
      true,
    ),
    deleteTemplate: (templateId: string) => run(
      `template:delete:${templateId}`,
      (context) => operations.deleteTemplate(templateId, context),
      true,
    ),
    restoreTemplate: (templateId: string) => run(
      `template:restore:${templateId}`,
      (context) => operations.restoreTemplate(templateId, context),
      true,
    ),
    deleteTemplateForever: (templateId: string) => run(
      `template:delete-forever:${templateId}`,
      (context) => operations.deleteTemplateForever(templateId, context),
    ),
    setDefaultTemplate: (templateId: string, isDefault: boolean) => run(
      `template:default:${templateId}`,
      (context) => operations.setDefaultTemplate(templateId, isDefault, context),
      true,
    ),
    setTemplateRepeat: (templateId: string, repeat: DatabaseToolbarTemplateRepeatWire | null) => run(
      `template:repeat:${templateId}`,
      (context) => operations.setTemplateRepeat(templateId, repeat, context),
      true,
    ),
  }), [operations, run, snapshot.dataSource.id]);

  return {
    actions,
    busyKey,
    closeMenu,
    /** The layer of a panel docked at the page's end, which its box takes (DatabaseViewSettingsPanel). */
    dockedLayer,
    /** The button the open menu came from. */
    menuTrigger: activeButtonRef,
    /** Where the open menu stands in place of a linked view's window before it (linkedViewWindowPlace). */
    menuAt: menuAnchor?.at,
    inputModality,
    menuRef,
    menuFrom,
    menuStyle,
    menuBackdropStyle,
    openMenu,
    search,
    setSearch,
    switchMenu,
    toggleMenu,
  };
}
