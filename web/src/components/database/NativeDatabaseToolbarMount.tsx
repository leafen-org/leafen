import { createDatabaseDeletedPropertiesSurface } from "@/lib/databaseDeletedProperties";
import { useContext, useEffect, useMemo, useRef, useState, type MutableRefObject } from "react";
import {
  databaseToolbarConfigErrorCodes,
  type DatabaseToolbarConfigMetadataResponse,
  type DatabaseToolbarTaskFeatureWire,
} from "@/databaseToolbarConfigWire.generated";
import type { GraphDataSource, GraphProperty, GraphRow, GraphView } from "@/contentGraph";
import { createDatabaseToolbarConfigService } from "@/lib/databaseToolbarConfigService";
import type { NativeBoardProductAdapter } from "@/lib/nativeBoardProductAdapter";
import type { NativeTableProductAdapter } from "@/lib/nativeTableProductAdapter";
import type { NativeTimelineProductAdapter } from "@/lib/nativeTimelineProductAdapter";
import { newId } from "@/lib/ids";
import { holdTemplateUntilMade, withTemplateCopy } from "@/lib/templatesMadeHere";
import { useTemplateDraftsShown, withTemplateDrafts } from "@/lib/templateDraftSend";
import { afterMadePropertiesSaved } from "@/lib/nativeFormProductAdapter";
import { deleteDatabaseRowPagePermanently } from "@/databaseRowPageApi";
import { createRelationDatabaseSearch } from "@/lib/relationDatabaseSearch";
import type { Session } from "@/types";
import { nativeBoardQueryBounds } from "./boardViewModel";
import { DatabaseDataSourcesContext } from "./databaseDataSourcesContext";
import { ToolbarMountContexts } from "./filterPageMaker";
import { DatabaseToolbar, type DatabaseToolbarOperations } from "./DatabaseToolbar";
import type { PhoneOwnQuery } from "./DatabasePhoneFilterList";
import { graphViewFilterSeeds } from "./graphViewFilterSeeds";
import { rowOrderWithNew, rowsInViewOrder } from "./viewRowOrder";
import { isNonEmptyStoredValue } from "./PropertyTypeChangeConfirm";
import { fillRowFromTemplate, makeRowFrom, rowStartFrom } from "./useDefaultTemplateRows";
import { newPageTemplate } from "./viewDefaultTemplate";
import { madeFromTemplate, readDatabaseTemplatesAhead, rowTemplateToMakeFrom, rowTemplateValues, rowWithTemplateIcon } from "@/lib/rowTemplateHere";
import { makeRowsFromTemplate } from "@/lib/rowMadeHere";
import type { DatabaseToolbarCommands } from "./databaseToolbarModel";
import type { AutomationSurface } from "./DatabaseToolbarMenus";
import {
  databaseFilterGraphIdentity,
  nativeDatabaseFilterSnapshot,
  type DatabaseFilterGraph,
} from "./databaseFilterSnapshot";
import { requestPageLayoutBuilder } from "@/lib/databasePageLayout";
import type { DatabaseToolbarOperationContext, DatabaseToolbarSnapshot } from "./useDatabaseToolbarController";
import { useDatabaseSprintSurface } from "./useDatabaseSprintSurface";
import { useNativePropertyCells } from "./useNativePropertyCells";
import { aborted } from "./useSettingsWriteUnanswered";
import { useToolbarSettingsRead } from "./useToolbarSettingsRead";
import { readAfterSettingsSaved, settingsToWriteOn } from "./toolbarSettingsWrite";

const emptyTaskConfiguration: DatabaseToolbarTaskFeatureWire = {
  revision: 0,
  subitems: null,
  dependencies: null,
};

function graphSnapshotIdentity(
  source: GraphDataSource,
  activeView: GraphView,
  views: readonly GraphView[],
  filterProperties: readonly GraphProperty[],
  filterRows: readonly GraphRow[],
  configuration: DatabaseToolbarConfigMetadataResponse | null,
) {
  return JSON.stringify([
    source,
    activeView,
    views,
    configuration?.snapshotIdentity ?? "loading",
    configuration?.templates.map(({ id, templateAuthorityRevision }) => [id, templateAuthorityRevision]),
  ]) + databaseFilterGraphIdentity({ properties: filterProperties, rows: filterRows });
}

function templateById(configuration: DatabaseToolbarConfigMetadataResponse | null, templateId: string) {
  return configuration?.templates.find((template) => template.id === templateId);
}

type TemplateWire = DatabaseToolbarConfigMetadataResponse["templates"][number];
/** A template change shown ahead of the server's answer. */
type TemplateChange = (templates: TemplateWire[]) => TemplateWire[];

/** The templates with one changed, in their order by place (the server's). */
function withTemplate(templates: TemplateWire[], templateId: string, change: Partial<TemplateWire>): TemplateWire[] {
  const next = templates.map((template) => template.id === templateId ? { ...template, ...change }
    : change.isDefault ? { ...template, isDefault: false } : template);
  return change.position === undefined ? next : next.sort((left, right) => left.position < right.position ? -1 : left.position > right.position ? 1 : 0);
}

export function NativeDatabaseToolbarMount({
  activeView,
  contextPageId,
  widget,
  automation,
  boardAdapter,
  configurationReady,
  dataSources,
  filterGraph,
  pageId,
  properties,
  readOnly,
  rows,
  search,
  session,
  source,
  tableAdapter,
  timelineAdapter,
  views,
  workspaceId,
  onCreateProperty,
  onError,
  onOwnQueryChange,
  ownQuery,
  onTaskFeatures,
  onResetOwnQuery,
  onRowCreated,
  onSearchChange,
  commandsRef,
  blockId,
  onCopyViewLink,
  onTurnIntoDashboard,
  onLeaveDashboard,
  formBarSlotRef,
  viewControlsSlotRef,
}: {
  activeView: GraphView;
  /** A widget's tools in its head: a dashboard's "view" or "edit" mode, Home's "내 작업" ("home"). */
  widget?: "view" | "edit" | "home";
  automation?: AutomationSurface | null;
  /** The page the view is on, a linked view's own: a filter's "this page". */
  contextPageId?: string;
  boardAdapter?: NativeBoardProductAdapter;
  configurationReady: boolean;
  dataSources: readonly GraphDataSource[];
  filterGraph: DatabaseFilterGraph;
  pageId: string;
  properties: readonly GraphProperty[];
  readOnly: boolean;
  rows: readonly GraphRow[];
  search: string;
  session: Session | null;
  source: GraphDataSource;
  tableAdapter?: NativeTableProductAdapter;
  timelineAdapter?: NativeTimelineProductAdapter;
  views: readonly GraphView[];
  workspaceId: string;
  onCreateProperty?(type: GraphProperty["type"], name: string, config?: Record<string, unknown>): Promise<GraphProperty | null>;
  onError(error: unknown): void;
  /** Applies, for this viewer alone, the filters and sorts of a view they can't edit. */
  onOwnQueryChange?(view: GraphView): void;
  /** The viewer's own filters and sorts while they apply, for a phone's filter sheet. */
  ownQuery?: PhoneOwnQuery | null;
  /** Hears the source's task features as read, for the views that show them. */
  onTaskFeatures?(features: DatabaseToolbarTaskFeatureWire | null): void;
  /** Hears the task "스프린트" relation while the sprints are on, for the view's own menus. */
  /** A locked database's "필터 초기화", while the viewer's own filters and sorts apply. */
  onResetOwnQuery?(): void;
  /** Opens a page New made, as the view opens its pages; given the row when it was made here. */
  onRowCreated?(rowId: string, row?: GraphRow): void;
  onSearchChange(value: string): void;
  commandsRef?: MutableRefObject<DatabaseToolbarCommands | null>;
  /** The database block the toolbar is of: its folded tools a person keeps inline; its start panel a linked view's "←" opens. */
  blockId?: string;
  /** Copies a link that opens the page on a view. */
  onCopyViewLink?(viewId: string): void;
  /** Turns a view into a dashboard, the view as it was its first widget. */
  onTurnIntoDashboard?(view: GraphView): Promise<boolean>;
  /** Turns a dashboard into another layout, its widgets gone. */
  onLeaveDashboard?(dashboard: GraphView, next: GraphView): Promise<boolean>;
  /** Takes the place in a form view's row where its builder puts "미리보기" and "양식 공유". */
  formBarSlotRef?(element: HTMLElement | null): void;
  viewControlsSlotRef?(element: HTMLElement | null): void;
}) {
  const [configuration, setConfiguration] = useState<DatabaseToolbarConfigMetadataResponse | null>(null);
  // Template changes sent and not yet answered, shown over the settings as read; and their turn.
  const [templateChanges, setTemplateChanges] = useState<readonly TemplateChange[]>([]);
  const templateTurn = useRef<Promise<unknown>>(Promise.resolve());
  // What a template's editor wrote shows too, until the settings hold it (templateDraftSend.ts).
  const templateDrafts = useTemplateDraftsShown(session, source.id);
  const templates = useMemo(() => withTemplateDrafts(
    templateChanges.reduce<TemplateWire[]>((shown, change) => change(shown), configuration?.templates ?? []), templateDrafts),
  [configuration?.templates, templateChanges, templateDrafts]);
  const reportTaskFeatures = useRef(onTaskFeatures);
  reportTaskFeatures.current = onTaskFeatures;
  useEffect(() => { reportTaskFeatures.current?.(configuration?.taskFeatures ?? null); }, [configuration?.taskFeatures]);
  const dataSourcesSurface = useContext(DatabaseDataSourcesContext);
  const sprints = useDatabaseSprintSurface({
    session, pageId, dataSourceId: source.id,
    // A reader has no toolbar configuration to tell, and a linked view's tab still names its sprint.
    taskDatabase: configuration?.taskFeatures?.tasks?.enabled === true || (readOnly === true && Boolean(contextPageId) && contextPageId !== pageId),
    onChanged: () => void dataSourcesSurface?.reread(),
  });
  const searchRelationDatabases = useMemo(
    () => session && !readOnly ? createRelationDatabaseSearch(session, workspaceId) : undefined,
    [readOnly, session, workspaceId],
  );
  const deletedProperties = useMemo(
    () => session && !readOnly
      ? createDatabaseDeletedPropertiesSurface(session, pageId, source.id, () => dataSourcesSurface?.reread())
      : undefined,
    [dataSourcesSurface, pageId, readOnly, session, source.id],
  );
  const onErrorRef = useRef(onError), onRowCreatedRef = useRef(onRowCreated); // the latest: the operations outlive a view switch
  onErrorRef.current = onError; onRowCreatedRef.current = onRowCreated;
  const service = useMemo(
    () => session ? createDatabaseToolbarConfigService(session, setConfiguration) : null,
    [session],
  );
  useEffect(() => () => service?.dispose(), [service]);
  const unanswered = useToolbarSettingsRead({
    service, pageId, dataSourceId: source.id, readOnly, configurationReady, properties, onError,
    dependencies: configuration?.taskFeatures?.dependencies, onReset: () => { setConfiguration(null); setTemplateChanges([]); },
  });
  // The templates New offers are read ahead, and again as the tab comes
  // back, so a row made from one shows its body at once (rowTemplateHere.ts).
  useEffect(() => {
    if (!session || readOnly || !configuration) return;
    const read = () => readDatabaseTemplatesAhead(session, source.id, configuration.templates);
    read();
    window.addEventListener("focus", read);
    return () => window.removeEventListener("focus", read);
  }, [configuration, readOnly, session, source.id]);

  const snapshotIdentity = useMemo(
    () => graphSnapshotIdentity(
      source,
      activeView,
      views,
      filterGraph.properties,
      filterGraph.rows,
      configuration,
    ),
    [activeView, configuration, filterGraph, source, views],
  );
  const filterSnapshot = useMemo(() => nativeDatabaseFilterSnapshot({
    currentUserId: session?.user.id ?? "",
    graph: filterGraph,
    snapshotIdentity,
    workspaceId,
  }), [filterGraph, session?.user.id, snapshotIdentity, workspaceId]);
  const contextIdentity = `${session?.user.id ?? "anonymous"}:${workspaceId}:${pageId}:${source.id}:${activeView.id}`;
  const generation = useRef({ contextIdentity: "", snapshotIdentity: "", value: 0 });
  if (generation.current.contextIdentity !== contextIdentity || generation.current.snapshotIdentity !== snapshotIdentity) {
    generation.current = { contextIdentity, snapshotIdentity, value: generation.current.value + 1 };
  }
  // A template's property values edit with the cells the database's rows use.
  const { propertyCellEnvironment, propertyValueContext } = useNativePropertyCells({
    adapter: readOnly ? undefined : tableAdapter, contextIdentity, currentUserId: session?.user.id, dataSource: source, dataSources,
    filterGraph, pageId, properties, requestGeneration: generation.current.value, rows, workspaceId,
  });
  const templatePropertyCells = useMemo(
    () => ({ environment: propertyCellEnvironment, valueContext: propertyValueContext }),
    [propertyCellEnvironment, propertyValueContext],
  );
  // Its menus start over when another view is chosen, but not when the chosen
  // view left with its source (moved to the trash or unlinked from "데이터 소스
  // 관리"): that list stays open, as Notion's (seen 2026-11-03).
  const menuView = useRef({ view: activeView.id, source: activeView.dataSourceId, token: activeView.id });
  if (menuView.current.view !== activeView.id) {
    const left = !dataSources.some(({ id }) => id === menuView.current.source);
    menuView.current = { view: activeView.id, source: activeView.dataSourceId, token: left ? menuView.current.token : activeView.id };
  }
  const menuViewToken = menuView.current.token;
  const snapshot = useMemo<DatabaseToolbarSnapshot>(() => ({
    snapshotIdentity,
    contextIdentity,
    menuIdentity: `${session?.user.id ?? "anonymous"}:${workspaceId}:${menuViewToken}`, settingsIdentity: `${session?.user.id ?? "anonymous"}:${workspaceId}:${contextPageId ?? pageId}:${blockId ?? dataSourcesSurface?.databaseId ?? source.id}`,
    requestGeneration: generation.current.value,
    workspaceId,
    pageId,
    dataSource: source,
    activeView,
    views,
    properties,
    rows,
    filterSnapshot,
    templates, templatesRead: configuration !== null,
    taskConfiguration: configuration?.taskFeatures ?? emptyTaskConfiguration,
    writable: Boolean(session && service && tableAdapter && !readOnly),
    templatePropertyCells,
  }), [activeView, blockId, configuration, contextIdentity, contextPageId, dataSourcesSurface?.databaseId, filterSnapshot, menuViewToken, pageId, properties, readOnly, rows, service, session, snapshotIdentity, source, tableAdapter, templatePropertyCells, templates, views, workspaceId]);
  const currentSnapshot = useRef(snapshot);
  currentSnapshot.current = snapshot;
  const shownRows = useRef(rows);
  shownRows.current = rows;

  // The dashboard turns change each render; the operations keep only whether there are any.
  const dashboardTurns = useRef({ into: onTurnIntoDashboard, leave: onLeaveDashboard });
  dashboardTurns.current = { into: onTurnIntoDashboard, leave: onLeaveDashboard };
  const turnsIntoDashboard = Boolean(onTurnIntoDashboard), leavesDashboard = Boolean(onLeaveDashboard);
  const operations = useMemo<DatabaseToolbarOperations>(() => {
    const isCurrent = (context: DatabaseToolbarOperationContext) => {
      const current = currentSnapshot.current;
      return !context.signal.aborted
        && context.contextIdentity === current.contextIdentity
        && context.snapshotIdentity === current.snapshotIdentity
        && context.requestGeneration === current.requestGeneration
        && context.dataSourceId === current.dataSource.id
        && context.viewId === current.activeView.id;
    };
    const graphCurrent = (context: DatabaseToolbarOperationContext) => () => isCurrent(context);
    const graphViewAdapter = tableAdapter ?? boardAdapter ?? timelineAdapter;
    type SettingsOperations = Parameters<NonNullable<typeof service>["mutate"]>[2]["operations"];
    async function mutate(context: DatabaseToolbarOperationContext, operation: SettingsOperations[number] | SettingsOperations, refreshGraph = false): Promise<boolean> {
      // A change asked for is sent, saved and shown while the same database
      // and view stay, though a step before it (a property it made) moved the
      // snapshot and so retired the request meanwhile; the server's revisions
      // keep it from landing on what it was not read from. It goes on the
      // settings once read and this device's writes to the database went (settingsToWriteOn).
      const sameDatabase = () => context.contextIdentity === currentSnapshot.current.contextIdentity;
      const current = service && session ? await settingsToWriteOn(service, session, source.pageId ?? pageId, pageId, source.id, sameDatabase) : null;
      if (!service || !current || !sameDatabase()) return false;
      const response = await service.mutate(pageId, source.id, {
        requestId: newId(), expectedWorkspaceContentRevision: current.workspaceContentRevision, operations: Array.isArray(operation) ? operation : [operation],
      });
      if (!response || !sameDatabase()) return false;
      await readAfterSettingsSaved(service, pageId, source.id, refreshGraph ? () => dataSourcesSurface?.reread() : undefined);
      return true;
    }
    type TemplateOperation = Parameters<typeof mutate>[1];
    // A template's change shows on its menus at once and goes to the server
    // behind it, one after another, each by the revision the server last gave
    // (user decision 2026-11-12: a press answers on the next frame). Shown
    // once answered and read again, a new template's page opened 677–709 ms
    // after the press on a NAS's 100 ms round trip (2026-11-13). Refused, the
    // settings are read again and the person hears why.
    function sendTemplate(context: DatabaseToolbarOperationContext, templateId: string, show: TemplateChange | null,
      build: (template: TemplateWire | undefined) => TemplateOperation | null) {
      if (show) setTemplateChanges((changes) => [...changes, show]);
      const turn = templateTurn.current.then(async () => {
        // Refused for a revision the template has moved past (a change of its
        // read late, or someone else's), it is read again and sent once more
        // on the new one, so what was typed is kept.
        for (let tries = 2; tries > 0; tries -= 1) {
          const operation = build(service?.snapshot()?.templates.find(({ id }) => id === templateId));
          try {
            if (operation && await mutate(context, operation)) return true;
          } catch (error) {
            const current = context.contextIdentity === currentSnapshot.current.contextIdentity;
            if (tries > 1 && current && Reflect.get(Object(error), "code") === databaseToolbarConfigErrorCodes.conflict
              && await service?.refresh(pageId, source.id).catch(() => null)) continue;
            if (current && !aborted(error) && !unanswered(error)) onErrorRef.current(error);
          }
          break;
        }
        if (show) await service?.refresh(pageId, source.id).catch(() => null);
        return false;
      }).finally(() => {
        if (show) setTemplateChanges((changes) => changes.filter((change) => change !== show));
      });
      templateTurn.current = turn.catch(() => false);
      return turn;
    }
    const sendTemplateChange = async (context: DatabaseToolbarOperationContext, templateId: string, change: Partial<TemplateWire>,
      build: (template: TemplateWire) => TemplateOperation) => {
      await sendTemplate(context, templateId, (shown) => withTemplate(shown, templateId, change), (template) => template ? build(template) : null);
    };
    return {
      isCurrent,
      // What the toolbar changes for everyone is a step of the page's ⌘Z (a property shown, all shown, a group);
      // its search stays the screen's (useDatabasePersonalQueries: viewAsSaved).
      async updateView(view, context) {
        await graphViewAdapter?.updateView(view, graphCurrent(context), true);
      },
      ...(turnsIntoDashboard ? {
        async turnIntoDashboard(view: GraphView) {
          const config = { ...view.config };
          delete config.search;
          await dashboardTurns.current.into?.({ ...view, config });
        },
      } : {}),
      ...(leavesDashboard ? {
        async leaveDashboard(dashboard: GraphView, next: GraphView) {
          const config = { ...next.config };
          delete config.search;
          await dashboardTurns.current.leave?.(dashboard, { ...next, config });
        },
      } : {}),
      async commitProperties(next, context) {
        await tableAdapter?.commitProperties(next, graphCurrent(context), undefined, currentSnapshot.current.activeView.id);
      },
      async deleteProperties(propertyIds, context) {
        await tableAdapter?.deleteProperties(propertyIds, graphCurrent(context));
      },
      async configureTaskFeatures(taskConfiguration, context, madeProperties) {
        try {
          // Roles may name properties another screen just made (afterMadePropertiesSaved), or
          // ones a conversion makes with them, in the same change (madeProperties).
          await afterMadePropertiesSaved();
          return await mutate(context, {
            kind: "configureTaskFeatures",
            expectedRevision: taskConfiguration.revision,
            taskFeatures: taskConfiguration,
            ...(madeProperties?.length ? { madeProperties: [...madeProperties] } : {}),
          }, true);
        } catch (error) {
          // Refused, the person hears why while the same database stays, though a
          // step before it (a property it made) moved the snapshot; the settings show
          // what the server keeps, another's change this one met and the revision the
          // next one goes from. Unanswered, it says so (useSettingsWriteUnanswered).
          if (context.contextIdentity === currentSnapshot.current.contextIdentity && !aborted(error) && !unanswered(error)) {
            await service?.refresh(pageId, source.id).catch(() => null);
            onErrorRef.current(error);
          }
          return false;
        }
      },
      async createRow(templateId, context) {
        // Before its templates are read, New starts from the view's default as newPageTemplate names it then.
        const start = templateId === undefined && !configuration && session
          ? rowStartFrom(session, source.id, newPageTemplate(null, currentSnapshot.current.activeView), properties) : null;
        const template = templateId ? templateById(configuration, templateId) : start?.template;
        const filterSeeds = () => graphViewFilterSeeds(currentSnapshot.current.properties, currentSnapshot.current.activeView,
          { currentUserId: session?.user.id, currentPageId: contextPageId ?? pageId }, nativeBoardQueryBounds);
        // New makes its row first, at the top, as Notion's.
        const first = rows.filter((row) => !row.subitemParentId)
          .reduce<GraphRow | undefined>((top, row) => !top || row.position < top.position ? row : top, undefined);
        // A view keeping its own order shows rows it hasn't placed after
        // those it has, so New places its row there as Notion's: a table's,
        // list's or gallery's first, a board's last (viewRowOrder.ts). The
        // place is no step of ⌘Z of its own, as the table's "+ 새 페이지".
        const placeNew = (row: GraphRow) => {
          const view = currentSnapshot.current.activeView;
          const place = view.type === "board" ? "last" : view.type === "table" || view.type === "list" || view.type === "gallery" ? "first" : null;
          // A sorted view puts it where its sorts say.
          if (!place || (Array.isArray(view.config.sorts) && view.config.sorts.length > 0)) return;
          const rowOrder = rowOrderWithNew(view.config, row.id, place, rowsInViewOrder(shownRows.current, view.config).map(({ id }) => id));
          if (!rowOrder || !graphViewAdapter) return;
          void graphViewAdapter.updateView({ ...view, config: { ...view.config, rowOrder } },
            () => context.contextIdentity === currentSnapshot.current.contextIdentity, false).catch((error) => onErrorRef.current(error));
        };
        // A template's page is made here first and opens at once, as any
        // other New (rowMadeHere.ts), with the template's icon, values and
        // body as read ahead here (rowTemplateHere.ts); its making names the
        // template, which fills what it leaves empty as the server makes it.
        // It takes what the view's filters ask for where the template leaves
        // it empty, as Notion's New (seen 2026-11-06: 담당 "나" from the
        // view's filter, the title from the template).
        if (template && tableAdapter) {
          const seeds = {
            ...Object.fromEntries(Object.entries(filterSeeds()).filter(([id]) => !isNonEmptyStoredValue(template.properties[id]))),
            ...session ? rowTemplateValues(session, template, properties, source.id) : {},
          };
          // New puts the page first, as Notion's (seen 2026-11-05: over the table's first row).
          const making = session ? rowTemplateToMakeFrom(session, template.id, start ?? {}) : null; // Unread, as start names the view's default.
          const made = await makeRowsFromTemplate(making?.template,
            () => tableAdapter.createRow(source, null, null, graphCurrent(context), seeds, first?.id, undefined, false, true));
          if (made) placeNew(made);
          if (!made || !isCurrent(context)) return made;
          const given = session && making ? madeFromTemplate(session, made.id, making) : null;
          // It opens with the template's icon, which the template gives it as the server makes it.
          const looked = rowWithTemplateIcon(made, template);
          const row = looked !== made ? tableAdapter.publishCreatedRow(looked, graphCurrent(context)) ?? made : made;
          // Its page opens with the body copied here; the row shown meanwhile is this New's own change.
          await given?.shown;
          onRowCreatedRef.current?.(row.id);
          if (session && given) {
            fillRowFromTemplate(session, row, template.id, (filled) => tableAdapter.publishCreatedRow(filled, () => true),
              (rowId) => shownRows.current.find(({ id }) => id === rowId), (error) => onErrorRef.current(error), given);
          }
          return row;
        }
        // It starts with what the view's filters ask for, as Notion's does.
        // As in Notion, New opens the page it made: its place and its page
        // come with the row, drawn with it (the adapter's `alongside`) — set
        // once the making settled, the page's database was drawn a second
        // time before the page (2026-11-15, NAS condition, CPU ×2).
        let opened = false;
        const open = (made: GraphRow) => {
          opened = true;
          placeNew(made);
          if (isCurrent(context)) onRowCreatedRef.current?.(made.id, made);
        };
        const row = await makeRowFrom(start, async () => await tableAdapter?.createRow(source, null, null, graphCurrent(context), filterSeeds(),
          first?.id, undefined, false, true, open) ?? null, (filled) => void tableAdapter?.publishCreatedRow(filled, () => true),
          (rowId) => shownRows.current.find(({ id }) => id === rowId), (error) => onErrorRef.current(error));
        if (row && !opened) open(row);
        return row;
      },
      async createTemplate(input, context) {
        // A new template's statuses read as their default without keeping it,
        // as Notion's (seen 2026-11-08: "● 시작 전", and the template and a
        // row made from it switched when the default did). It shows and opens
        // at once, its page read once the server has made it.
        const made: TemplateWire = { id: input.id, name: input.name, icon: "", title: "", properties: {}, isDefault: false,
          position: input.position, templateAuthorityRevision: 0, deleted: false, updatedAt: new Date().toISOString() };
        const sent = sendTemplate(context, input.id, (shown) => shown.some(({ id }) => id === made.id) ? shown : [...shown, made],
          () => ({ kind: "createTemplate", templateId: input.id, name: input.name, position: input.position, properties: {} }));
        holdTemplateUntilMade(input.id, sent, session?.user.id);
        await sent;
      },
      // A copy shows and opens at once, named and placed as the server makes it, its body read once copied there (shownTemplateCopy).
      async duplicateTemplate(templateId, targetTemplateId, context) {
        await holdTemplateUntilMade(targetTemplateId, sendTemplate(context, templateId, (shown) => withTemplateCopy(shown, templateId, targetTemplateId),
          (template) => template ? { kind: "duplicateTemplate", templateId, targetTemplateId, expectedRevision: template.templateAuthorityRevision } : null));
      },
      async updateTemplate(templateId, patch, context) {
        const change = Object.fromEntries((["name", "icon", "title", "position", "properties", "propertySpans", "isDefault"] as const)
          .flatMap((key) => patch[key] === undefined ? [] : [[key, patch[key]]])) as Partial<TemplateWire>;
        await sendTemplate(context, templateId, (shown) => withTemplate(shown, templateId, change), (template) => {
          if (!template) return null;
          const changes: Parameters<NonNullable<typeof service>["mutate"]>[2]["operations"] = [{
            kind: "updateTemplate", templateId, expectedRevision: template.templateAuthorityRevision,
            name: patch.name, icon: patch.icon, title: patch.title, position: patch.position,
            properties: patch.properties, propertySpans: patch.propertySpans,
          }];
          if (patch.isDefault !== undefined && patch.isDefault !== template.isDefault) {
            changes.push({
              kind: "setDefaultTemplate", templateId,
              expectedRevision: template.templateAuthorityRevision + 1,
              isDefault: patch.isDefault,
            });
          }
          return changes;
        });
      },
      deleteTemplate: (templateId, context) => sendTemplateChange(context, templateId, { deleted: true },
        (template) => ({ kind: "deleteTemplate", templateId, expectedRevision: template.templateAuthorityRevision })),
      restoreTemplate: (templateId, context) => sendTemplateChange(context, templateId, { deleted: false },
        (template) => ({ kind: "restoreTemplate", templateId, expectedRevision: template.templateAuthorityRevision })),
      // As the trash deletes it, by a row's permanent deletion (database_template_trash.go): the server first.
      async deleteTemplateForever(templateId, context) {
        const template = templateById(configuration, templateId);
        if (!template || !session || !service || !isCurrent(context)) return;
        const deleted = await deleteDatabaseRowPagePermanently(session, templateId, { requestId: newId(), expectedAuthorityRevision: template.templateAuthorityRevision })
          .then(() => true, (error: unknown) => { if (isCurrent(context) && unanswered(error)) return false; throw error; });
        if (deleted && isCurrent(context)) await service.refresh(pageId, source.id);
      },
      setDefaultTemplate: (templateId, isDefault, context) => sendTemplateChange(context, templateId, { isDefault },
        (template) => ({ kind: "setDefaultTemplate", templateId, expectedRevision: template.templateAuthorityRevision, isDefault })),
      setTemplateRepeat: (templateId, repeat, context) => sendTemplateChange(context, templateId, { repeat: repeat ?? undefined },
        (template) => ({ kind: "setTemplateRepeat", templateId, expectedRevision: template.templateAuthorityRevision, repeat })),
      onError(error, context) {
        if (isCurrent(context)) onErrorRef.current(error);
      },
    };
  }, [boardAdapter, configuration, contextPageId, leavesDashboard, pageId, properties, rows, service, session?.user.id, source, tableAdapter, timelineAdapter, turnsIntoDashboard, unanswered]);

  return <ToolbarMountContexts sprints={sprints ? { ...sprints, activeViewId: activeView.id } : sprints} cells={propertyCellEnvironment}>
    <DatabaseToolbar widget={widget} automation={automation} dataSources={dataSources}
      operations={operations} search={search} snapshot={snapshot}
      onCreateProperty={onCreateProperty} onSearchChange={onSearchChange} onOwnQueryChange={onOwnQueryChange}
      onResetOwnQuery={onResetOwnQuery} ownQuery={ownQuery}
      searchRelationDatabases={searchRelationDatabases} deletedProperties={deletedProperties}
      session={session} commandsRef={commandsRef} blockId={blockId} onCopyViewLink={onCopyViewLink} formBarSlotRef={formBarSlotRef} viewControlsSlotRef={viewControlsSlotRef}
      onEditLayout={() => requestPageLayoutBuilder({ pageId: source.pageId ?? pageId, dataSourceId: source.id })} />
  </ToolbarMountContexts>;
}
