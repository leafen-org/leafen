import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import type { ContentGraph, GraphBlock, GraphDataSource, GraphRow } from "@/contentGraph";
import { createDatabaseAutomationService } from "@/databaseAutomationApi";
import { databaseAutomationEditorProperties, databaseAutomationEditorViews } from "@/components/database/databaseAutomationPanelModel";
import type { AutomationSurface } from "@/components/database/DatabaseToolbarMenus";
import { DatabaseOwnPageContext } from "@/components/database/databaseFullPageLink";
import { DatabasePagelessRowsContext } from "@/components/database/databasePagelessRows";
import type { DatabaseViewOpenMode } from "@/components/database/DatabaseView";
import { NativeDatabaseViewMount } from "@/components/database/NativeDatabaseViewMount";
import { DatabaseFilterBarsOpenContext } from "@/components/database/databaseFilterBar";
import { DatabaseSourceLockContext } from "@/components/database/databaseStructureLock";
import { PeopleDatabaseIcon, PERSON_ICON_PREFIX, PersonIconContext } from "@/components/page/PageIcon";
import type { NativeDatabaseViewProductAdapter } from "@/lib/nativeDatabaseViewProductAdapter";
import { peopleRowPerson, type PeopleDatabase } from "@/lib/peopleDatabase";
import type { PeopleDirectoryPerson } from "@/lib/peopleProfiles";
import type { Session, Workspace } from "@/types";
import { PersonAvatar } from "./PersonAvatar";
import { usePeopleDatabaseWrites } from "./usePeopleDatabaseWrites";
import { usePeoplePagelessRows } from "./usePeoplePagelessRows";
import styles from "./PeopleView.module.css";

/**
 * The people list: the workspace's People database (people_database.go), as
 * Notion's People is a database whose rows are its people's pages (seen
 * 2026-11-15): "Directory View" and "Card View" at first, guests and the
 * deactivated left out until their filter is changed, a person opening their
 * profile. Its five own properties stay as the server keeps them — a person's
 * Name, About, Membership Type and Person, and their Email as the reader may
 * see it, which its read gives as every view of People's does
 * (people_database_rules.go) — and its owner writes it as any database: its
 * views, the properties added after the five, of any kind a database takes,
 * and their values, and anyone's About, into their profile
 * (peopleSourceCells.ts, as every view of People writes it), as Notion's
 * People lets its owner (seen 2026-11-15); its members read it,
 * a restricted member the rows of those they share a place with. Its "⚡" is
 * its database's automations (people_automation_source.go), its owner's to
 * change and its members' to read.
 */
export function PeopleDatabaseView({ session, workspace, people, database, onOpenPerson, onReread }: {
  session: Session;
  workspace: Workspace;
  people: readonly PeopleDirectoryPerson[];
  database: PeopleDatabase;
  /** Opens a person's profile the way its view's "페이지 보기 선택" says. */
  onOpenPerson(userId: string, mode: DatabaseViewOpenMode): void;
  /** Reads People again from the server. */
  onReread(): Promise<boolean>;
}) {
  const { t } = useTranslation("peopleProfiles");
  const ids = database.propertyIds;
  const sourceId = database.dataSourceId;
  const editable = database.editable;
  const [graph, setGraph] = useState<ContentGraph>(database.graph);
  useEffect(() => setGraph(database.graph), [database]);
  const automations = useMemo(() => workspace.restricted ? null : createDatabaseAutomationService(api, session, database.pageId, sourceId),
    [database.pageId, session, sourceId, workspace.restricted]);
  // How many it has, read as it opens: its "⚡" blue while it has any, as a database page's read says.
  const [automationCount, setAutomationCount] = useState<number>();
  useEffect(() => {
    if (!automations) return undefined;
    const controller = new AbortController();
    automations.list(controller.signal).then((items) => setAutomationCount(items.length), () => {});
    return () => controller.abort();
  }, [automations]);
  const stored = graph.dataSources.find(({ id }) => id === sourceId);
  // People's, which no view of it adds a page to and whose five stay as kept (databaseSourceRules.tsx), as its read says.
  const source = useMemo<GraphDataSource>(() => ({ ...stored, id: sourceId, pageId: database.pageId, title: t("people"), automationCount,
    peopleDatabase: true, fixedPropertyIds: [ids.name, ids.email, ids.about, ids.membership, ids.person] }), [automationCount, database.pageId, ids, sourceId, stored, t]);
  // With the databases its relations reach, which its read brings (people_database_routes.go).
  const dataSources = useMemo(() => [source, ...graph.dataSources.filter(({ id }) => id !== sourceId)], [graph.dataSources, source, sourceId]);
  // Its five, whose values the server keeps.
  const own = useMemo(() => new Set([ids.name, ids.email, ids.about, ids.membership, ids.person]), [ids]);
  const byPerson = useMemo(() => new Map(people.map((person) => [person.userId, person])), [people]);
  const properties = useMemo(() => graph.properties.filter(({ dataSourceId }) => dataSourceId === sourceId), [graph.properties, sourceId]);
  const writes = useMemo(() => editable ? { session, workspaceId: workspace.id, pageId: database.pageId } : null,
    [database.pageId, editable, session, workspace.id]);
  // Its owner's writes; of the five only About, which goes to its person's profile (peopleSourceCells.ts).
  const adapters = usePeopleDatabaseWrites({ input: writes, graph, setGraph, onReread });
  // Each row as its read gives it — its Email, the five its reader doesn't change — with its person's picture where it has no icon of its own.
  const rows = useMemo<GraphRow[]>(() => graph.rows.filter(({ dataSourceId }) => dataSourceId === sourceId).map((row) => {
    const userId = peopleRowPerson(row, ids);
    const person = byPerson.get(userId);
    return !row.icon && person?.avatar ? { ...row, icon: `${PERSON_ICON_PREFIX}${userId}`, iconType: "image" as const } : row;
  }), [byPerson, graph.rows, ids, sourceId]);
  const personOf = useCallback((rowId: string) => peopleRowPerson(rows.find(({ id }) => id === rowId) ?? { values: {} }, ids), [ids, rows]);
  const pageless = usePeoplePagelessRows({ rows, properties: properties.filter(({ id }) => !own.has(id)), nameId: ids.name, aboutId: ids.about,
    writes: editable, readerId: session.user.id });
  const views = useMemo(() => graph.views.filter(({ dataSourceId }) => dataSourceId === sourceId), [graph.views, sourceId]);
  const [activeId, setActiveId] = useState(() => new URLSearchParams(window.location.search).get("v") ?? "");
  const activeView = views.find((view) => view.id === activeId) ?? views[0];
  const stock = graph.blocks.find((candidate) => candidate.type === "inline_database");
  const block = useMemo<GraphBlock>(() => ({
    id: stock?.id ?? sourceId, parentId: null, afterId: null, position: "a0", type: "inline_database",
    content: { dataSourceId: sourceId, viewId: activeView?.id ?? "" },
  }), [activeView?.id, sourceId, stock?.id]);
  // Choosing a view is the reader's own; its owner's other changes are the database's.
  const viewAdapter = useMemo<NativeDatabaseViewProductAdapter>(() => ({
    ...(adapters.databaseViewAdapter ?? PEOPLE_READ_ONLY_VIEWS),
    selectView: (_block, view) => setActiveId(view.id),
  }), [adapters.databaseViewAdapter]);
  const tableAdapter = adapters.tableAdapter;
  const graphForFilters = useMemo(() => ({
    properties: graph.properties, rows: [...rows, ...graph.rows.filter(({ dataSourceId }) => dataSourceId !== sourceId)],
  }), [graph.properties, graph.rows, rows, sourceId]);
  // Its automations apply to all of People or one of its views, as Notion's
  // offers (seen 2026-11-14: "Directory View · 필터 1개"), a view's run
  // reading that view's own filters; a condition names any of its properties
  // but its Email, as Notion's offers Name, About, Membership Type and Person
  // and its owner's own; each step writes another page, a notification or a
  // webhook, never a person's row.
  const triggerPropertyIds = useMemo(() => properties.map(({ id }) => id).filter((id) => id !== ids.email), [ids.email, properties]);
  const automation = useMemo<AutomationSurface | null>(() => automations ? {
    readOnly: !editable, databaseId: sourceId, automationCount, editorPages: [], properties: databaseAutomationEditorProperties(properties),
    service: automations, views: databaseAutomationEditorViews(views), currentUserId: session.user.id, triggerPropertyIds, editsItsPage: false,
    databaseIcon: <PeopleDatabaseIcon pixels={16} />,
  } : null, [automationCount, automations, editable, properties, session.user.id, sourceId, triggerPropertyIds, views]);
  const personIcon = useCallback((userId: string, size: number) => {
    const person = byPerson.get(userId);
    return <PersonAvatar userId={userId} avatar={person?.avatar} name={person?.displayName} size={size} />;
  }, [byPerson]);
  if (!activeView) return null;
  return (
    <PersonIconContext.Provider value={personIcon}>
    <DatabaseOwnPageContext.Provider value>
    {/* What its source allows its views is its source's (databaseSourceRules.tsx); its owner alone changes it;
        its views show their pills at first. */}
    <DatabaseSourceLockContext.Provider value={!editable}>
    <DatabaseFilterBarsOpenContext.Provider value>
    <DatabasePagelessRowsContext.Provider value={pageless}>
      {/* Its tabs and first column 2px in from the title, as a database
          page's view stands in its block (EditorDatabaseBlocks). `?p=〈person〉` is their profile, which
          PeopleView opens, so a reload opens it again as Notion's People (seen 2026-11-15); the view, opening
          its rows itself, opens no row page for it (useDatabaseViewController). */}
      <div className={styles.directoryView}>
        <NativeDatabaseViewMount block={block} source={source} displayTitle={t("people")} dataSources={dataSources} filterGraph={graphForFilters}
          views={views} activeView={activeView} properties={properties} rows={rows} databaseViewAdapter={viewAdapter} tableAdapter={tableAdapter}
          boardAdapter={adapters.boardAdapter} timelineAdapter={adapters.timelineAdapter}
          pageId={database.pageId} workspaceId={workspace.id} session={session} readOnly={!tableAdapter} configurationReady={false}
          onAddProperty={adapters.addGraphProperty}
          onOpenRow={(rowId, mode) => { const userId = personOf(rowId); if (userId) onOpenPerson(userId, mode); }} listOnly listAutomations={automation} />
      </div>
    </DatabasePagelessRowsContext.Provider>
    </DatabaseFilterBarsOpenContext.Provider>
    </DatabaseSourceLockContext.Provider>
    </DatabaseOwnPageContext.Provider>
    </PersonIconContext.Provider>
  );
}

/** What a reader who changes none of People's views does with them: nothing, but choosing one. */
const PEOPLE_READ_ONLY_VIEWS: NativeDatabaseViewProductAdapter = {
  changeTitle() {}, changeIcon() {}, changeDescription() {}, changeCover() {}, showRowContent() {}, selectView() {},
  setLocked() {}, setArchived() {}, setLinkedPresentation() {},
  createView: async () => null, updateViews: async () => {}, deleteView: async () => {}, restoreView: async () => {},
  addDashboardWidget: async () => null, duplicateDashboardWidget: async () => null,
  retypeDashboardWidget: async () => {}, removeDashboardWidget: async () => null, arrangeDashboard: async () => {},
  turnViewIntoDashboard: async () => false, turnDashboardInto: async () => false,
};
