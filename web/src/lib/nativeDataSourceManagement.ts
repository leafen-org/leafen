import { blockInPlace, compareGraphOrder } from "@/contentGraph";
import type { ContentGraph, GraphBlock, GraphOperation, GraphView } from "@/contentGraph";
import type { DatabaseContainerSource } from "@/lib/databaseContainerPicker";
import { newId } from "@/lib/ids";
import { graphPositionBetween } from "@/lib/nativeBoardProductAdapter";
import { beginLinkedViewMaking, linkedStarterViewConfig } from "@/lib/databaseStarterLinked";
import { viewTakenAfter } from "@/lib/viewTakenAfter";
import { isDashboardWidget } from "@/lib/dashboardLayout";
import { dataSourceDisplayTitle } from "@/components/editor/databaseTitles";

// A database block lists the sources it owns first and the sources it links
// from other databases after them. These helpers rewrite that membership the
// way the server reads it; moving an own source or putting it in trash goes to
// the server, which then changes the page (nativeDataSourceManager.ts).

export type DatabaseLinkedMember = { dataSourceId: string; sourceContainerId: string };

function memberId(value: unknown): value is string {
  return typeof value === "string" && value !== "";
}

export function databaseBlockMembers(block: GraphBlock): { own: string[]; linked: DatabaseLinkedMember[] } {
  const first = memberId(block.content.dataSourceId) ? block.content.dataSourceId : "";
  const additional = Array.isArray(block.content.additionalDataSourceIds)
    ? block.content.additionalDataSourceIds.filter(memberId)
    : [];
  const linked = (Array.isArray(block.content.linkedMembers) ? block.content.linkedMembers : [])
    .flatMap((member: unknown) => {
      const value = member as Partial<DatabaseLinkedMember> | null;
      return value && memberId(value.dataSourceId) && memberId(value.sourceContainerId)
        ? [{ dataSourceId: value.dataSourceId, sourceContainerId: value.sourceContainerId }]
        : [];
    });
  if (block.content.linkedDatabaseSource === true) {
    const sourceContainerId = memberId(block.content.sourceContainerId) ? block.content.sourceContainerId : "";
    return { own: [], linked: first ? [{ dataSourceId: first, sourceContainerId }, ...linked] : linked };
  }
  return { own: first ? [first, ...additional] : additional, linked };
}

/**
 * The own sources a database still shows: one sent to the trash from 데이터 소스 관리 took its views along and no
 * longer counts, as Notion's (seen 2026-11-11: one of two so sent leaves the database named "새 데이터베이스" and its
 * other untitled source the same), unless every one went so; one in the trash with its tabs (the API's) still counts.
 */
export function databaseOwnNamingSourceIds(block: GraphBlock): string[] {
  const gone = Array.isArray(block.content.trashedWithViewsDataSourceIds) ? block.content.trashedWithViewsDataSourceIds : [];
  const own = databaseBlockMembers(block).own;
  const kept = own.filter((id) => !gone.includes(id));
  return kept.length > 0 ? kept : own;
}

/**
 * The sources a database goes by on the screen — how many name it, as an untitled source of several is "새 데이터
 * 소스" and a database of several takes a name of its own: its own it still shows and those it links from other
 * databases, as Notion's counts them (seen 2026-11-25 in Chrome: one of one source of its own and one it links,
 * opened alone, titled "C 및 X 보기" until named, its untitled own "새 데이터 소스"); its API names it by its own alone.
 * Its own first, those it links after, whatever order they came in, as Notion's ("A, B, X 보기" with X linked before B,
 * seen 2026-11-25) and the server's (database_container_name.go databaseNamingSources). A linked database has none. */
export function databaseNamingSourceIds(block: GraphBlock): string[] {
  const own = databaseOwnNamingSourceIds(block);
  return own.length > 0 ? [...own, ...databaseBlockMembers(block).linked.map(({ dataSourceId }) => dataSourceId)] : [];
}

/**
 * Whether a source a database block shows is in the trash where it stands:
 * its own database keeps it in the trash (the server's
 * database_source_trash_in_place.go), or, shown through a link, the page's
 * graph read it so. Its database stays and shows it to read only.
 */
export function databaseSourceInTrash(block: GraphBlock, source: { id: string; sourceInTrash?: boolean }): boolean {
  if (!databaseBlockMembers(block).own.includes(source.id)) return source.sourceInTrash === true;
  const listed = block.content.trashedDataSourceIds;
  return Array.isArray(listed) && listed.includes(source.id);
}

/**
 * The name a database block's page layout builder is headed with, as
 * Notion's: of a database of two or more sources, the source its pages are in
 * (seen 2026-11-07: "첫 소스"), an untitled one named as its database's sources
 * are while it holds them; of a database of one, the database's name as its
 * path shows it (`databaseTitle`), which its one source shares, as Notion's
 * (the server keeps the two one value, full_page_database_name.go).
 */
export function databaseBlockSourceName(block: GraphBlock, source: { title: string }, databaseTitle?: string) {
  const sources = databaseNamingSourceIds(block).length;
  return (sources < 2 && databaseTitle?.trim()) || dataSourceDisplayTitle(source.title, sources);
}

/**
 * Whether a database block only shows views, with no database of its own,
 * as Notion's view blocks (seen 2026-11-09): a linked database, a linked view
 * still waiting for its source and a dashboard made by "/대시보드 보기" hold
 * no data source (the API's `data_sources` is empty, "데이터 소스 관리" lists
 * none), are named by the block's own name, "제목 없음" while it has none,
 * and keep a view block's menu — "이름 바꾸기 ⌘⇧R" in it, "보기 잠금", a
 * duplicate without a choice of content, no simple table, CSV merge or
 * archive. Here a dashboard and a waiting link hold an unnamed source their
 * views stand on, so a block is one while all its sources are linked or
 * every tab of it is a dashboard or waits for its source and none of its
 * dashboards' widgets stands on a source of its own: a database whose one
 * tab became a dashboard keeps its source in its first widget, the view it
 * was, and stays a database, its widgets' "+" charting that source at once
 * (Notion's, seen 2026-11-15 twice: a chart widget and "새 보기", no source
 * asked). `views` is the graph's views or the block's tabs with its
 * dashboards' widgets (the server's database_view_block.go says the same).
 */
export function databaseViewBlock(block: GraphBlock, views: readonly GraphView[]) {
  if (block.type !== "inline_database") return false;
  const { own } = databaseBlockMembers(block);
  if (!own.length) return true;
  const ownView = (view: GraphView) => !view.containerId && own.includes(view.dataSourceId);
  if (views.some((view) => isDashboardWidget(view) && ownView(view))) return false;
  const tabs = views.filter((view) => !isDashboardWidget(view) && (view.containerId === block.id || ownView(view)));
  return tabs.length > 0 && tabs.every((view) => view.type === "dashboard" || view.config.linkPending === true);
}

/**
 * The own sources a database block lists wherever sources are offered or
 * named — "데이터 소스 관리", a view's or a widget's "데이터베이스", a
 * dashboard's first widget: none for a view block, whose unnamed source only
 * holds its dashboard or waiting link up (databaseViewBlock), as Notion's
 * view block lists no source (seen 2026-11-12: "출처" empty, "연결됨" alone).
 */
export function databaseListedOwnSourceIds(block: GraphBlock, views: readonly GraphView[]): string[] {
  return databaseViewBlock(block, views) ? [] : databaseBlockMembers(block).own;
}

/** Whether the database links the source rather than holding it. */
export function databaseLinksSource(block: GraphBlock, dataSourceId: string) {
  return databaseBlockMembers(block).linked.some((member) => member.dataSourceId === dataSourceId);
}

// A database left without own sources becomes a linked database of its first
// linked member, as on the server.
export function withDatabaseBlockMembers(
  block: GraphBlock, own: readonly string[], linked: readonly DatabaseLinkedMember[], viewId?: string,
): GraphBlock {
  const content: Record<string, unknown> = { ...block.content };
  for (const key of ["dataSourceId", "additionalDataSourceIds", "linkedDatabaseSource", "sourceContainerId", "linkedMembers"]) {
    delete content[key];
  }
  let remaining = [...linked];
  if (own.length) {
    content.dataSourceId = own[0];
    if (own.length > 1) content.additionalDataSourceIds = own.slice(1);
  } else if (remaining.length) {
    content.dataSourceId = remaining[0].dataSourceId;
    content.linkedDatabaseSource = true;
    content.sourceContainerId = remaining[0].sourceContainerId;
    remaining = remaining.slice(1);
  }
  if (remaining.length) content.linkedMembers = remaining;
  if (viewId) content.viewId = viewId;
  return { ...block, content };
}

function lastPosition(views: readonly GraphView[]) {
  return views.map(({ position }) => position).sort().at(-1);
}

// A database's tabs are one row across its sources: a source's first view
// goes after the last of them.
function lastDatabaseViewPosition(block: GraphBlock, graph: ContentGraph | undefined) {
  const { own } = databaseBlockMembers(block);
  return lastPosition((graph?.views ?? []).filter((view) => view.containerId === block.id
    || (!view.containerId && own.includes(view.dataSourceId))));
}

export function addDataSourceOperations(
  block: GraphBlock, title: string, titlePropertyName: string, viewName: string, dataSourceId = newId(), graph?: ContentGraph,
  viewId = newId(),
): GraphOperation[] {
  const { own, linked } = databaseBlockMembers(block);
  const view: GraphView = {
    id: viewId, dataSourceId, type: "table", name: viewName,
    position: graphPositionBetween(lastDatabaseViewPosition(block, graph), undefined), config: { rowOrder: [] }, // unwrapped beside the others (layoutDefaults)
  };
  return [
    { kind: "upsertDataSource", dataSource: { id: dataSourceId, title } },
    {
      kind: "upsertProperty",
      property: {
        id: newId(), dataSourceId, type: "title", name: titlePropertyName,
        position: graphPositionBetween(undefined, undefined), config: {},
      },
    },
    { kind: "upsertView", view },
    { kind: "upsertBlock", block: blockInPlace(withDatabaseBlockMembers(block, [...own, dataSourceId], linked, view.id)) },
  ];
}

// A linked source shows in the database through the database's own copy of
// the chosen view, so the original database's views stay unchanged.
export function linkDataSourceOperations(
  block: GraphBlock, graph: ContentGraph, candidate: DatabaseContainerSource,
  /** A new view that then asks which of the source's views to take, as Notion's "새 보기" (seen 2026-11-02). */
  starter?: { viewId: string; name: string },
): GraphOperation[] | null {
  const { own, linked } = databaseBlockMembers(block);
  if (own.includes(candidate.dataSourceId) || linked.some(({ dataSourceId }) => dataSourceId === candidate.dataSourceId)) {
    return null;
  }
  if (starter) beginLinkedViewMaking(starter.viewId); // Made here, it asks for its view here, as the two ways below.
  const view: GraphView = starter ? {
    id: starter.viewId, dataSourceId: candidate.dataSourceId, containerId: block.id, type: "table", name: starter.name,
    position: graphPositionBetween(lastDatabaseViewPosition(block, graph), undefined),
    config: linkedStarterViewConfig(candidate),
  } : {
    id: newId(), dataSourceId: candidate.dataSourceId, containerId: block.id,
    type: candidate.viewType, name: candidate.viewName,
    position: graphPositionBetween(lastDatabaseViewPosition(block, graph), undefined),
    config: { ...candidate.viewConfig },
  };
  return [
    { kind: "upsertView", view },
    {
      kind: "upsertBlock",
      block: blockInPlace(withDatabaseBlockMembers(block, own, [
        ...linked, { dataSourceId: candidate.dataSourceId, sourceContainerId: candidate.id },
      ], view.id)),
    },
  ];
}

/**
 * A database just made, or a linked view waiting for its source or back from
 * its "새 보기", becoming a linked view of the chosen source in place, as
 * Notion's start panel links it (seen 2026-10-30): the same block, so one
 * opened alone or in a side peek stays where it is (Notion's, seen
 * 2026-11-09), with a new view that then asks which of the source's views to
 * take. Its own empty sources and the views it owned go.
 */
export function linkSourceInPlaceOperations(
  block: GraphBlock, graph: ContentGraph, candidate: DatabaseContainerSource,
): GraphOperation[] {
  const { own } = databaseBlockMembers(block);
  // Unnamed as stored: "새 보기" only where it asks for its view (askingLinkedViewHere).
  const view: GraphView = {
    id: newId(), dataSourceId: candidate.dataSourceId, containerId: block.id, type: "table", name: "",
    position: graphPositionBetween(undefined, undefined), config: linkedStarterViewConfig(candidate),
  };
  // Made here, it asks for its view here (beginLinkedViewMaking).
  beginLinkedViewMaking(view.id);
  return [
    { kind: "upsertView", view },
    {
      kind: "upsertBlock",
      block: blockInPlace(withDatabaseBlockMembers(block, [], [{ dataSourceId: candidate.dataSourceId, sourceContainerId: candidate.id }], view.id)),
    },
    ...graph.views.filter((owned) => owned.containerId === block.id)
      .map(({ id }): GraphOperation => ({ kind: "deleteView", viewDeletion: { id } })),
    ...own.map((id): GraphOperation => ({ kind: "deleteDataSource", dataSourceDeletion: { id } })),
  ];
}

/**
 * One view of a database showing views alone linked in place to the chosen
 * source, as Notion's (seen 2026-11-11 at 1440px and 402pt): a view waiting
 * for its source, or a linked view back from its "새 보기", becomes a view of
 * that source where its tab stands, its layout kept, asking which of the
 * source's views to take (as "+ 새 보드 보기" for a board); the block's other
 * views stay as they are, those waiting still waiting. The database then
 * links the source, its own empty source going with no view left on it: its
 * views still waiting stand on the source it links, showing none of its rows
 * (InlineDatabaseBlock draws them waiting). Null for a view that neither
 * waits nor is linked: a database just made links in its place
 * (linkSourceInPlaceOperations).
 */
export function linkViewInPlaceOperations(
  block: GraphBlock, graph: ContentGraph, viewId: string, candidate: DatabaseContainerSource,
): GraphOperation[] | null {
  const view = graph.views.find(({ id }) => id === viewId);
  const { own, linked } = databaseBlockMembers(block);
  const ownView = (other: GraphView) => !other.containerId && own.includes(other.dataSourceId);
  const linkedView = (other: GraphView) => other.containerId === block.id && !own.includes(other.dataSourceId);
  if (!view || isDashboardWidget(view) || !(linkedView(view) || (ownView(view) && view.config.linkPending === true))) return null;
  const tabs = graph.views.filter((other) => other.id !== view.id && !isDashboardWidget(other) && (other.containerId === block.id || ownView(other)));
  // A database with views of its own keeps its own sources.
  const keepsOwn = tabs.some((other) => ownView(other) && other.config.linkPending !== true);
  // A source linked before that no view stands on any more leaves it (switchViewDataSourceOperations).
  const left = linkedView(view) && view.dataSourceId !== candidate.dataSourceId
    && !tabs.some((other) => other.containerId === block.id && other.dataSourceId === view.dataSourceId);
  const members = [...linked.filter(({ dataSourceId }) => !left || dataSourceId !== view.dataSourceId),
    ...(linked.some(({ dataSourceId }) => dataSourceId === candidate.dataSourceId) ? [] : [{ dataSourceId: candidate.dataSourceId, sourceContainerId: candidate.id }])];
  const onCandidate = (other: GraphView, config: Record<string, unknown>): GraphView => ({ ...other, containerId: block.id, dataSourceId: candidate.dataSourceId, config });
  const waiting = keepsOwn ? [] : tabs.filter((other) => ownView(other) && other.config.linkPending === true);
  // Made here, it asks for its view here (beginLinkedViewMaking).
  beginLinkedViewMaking(view.id);
  return [
    { kind: "upsertView", view: { ...onCandidate(view, linkedStarterViewConfig(candidate, view.type)), name: "" } },
    ...waiting.map((other): GraphOperation => ({ kind: "upsertView", view: onCandidate(other, { ...sourceFreeViewConfig(other), linkPending: true }) })),
    { kind: "upsertBlock", block: blockInPlace(withDatabaseBlockMembers(block, keepsOwn ? own : [], members, view.id)) },
    ...(keepsOwn ? [] : own.map((id): GraphOperation => ({ kind: "deleteDataSource", dataSourceDeletion: { id } }))),
  ];
}

// What a view keeps of its settings when it shows another source: the settings no property names, a widget its
// dashboard — not a table's wrapping, its columns' (tableConfigAfterWrapAll), so it starts unwrapped (seen 2026-11-18).
// The rest starts as the layout's first, as Notion's view does on its new source (seen 2026-10-11: a board grouped
// by the new source's select).
const SOURCE_FREE_VIEW_KEYS = [
  "dashboardViewId", "icon", "description", "showDataSourceTitle", "showPageIcons", "wrap", "fitImage", "cardSize", "cardLayout", "openPageIn",
  "rowHeight", "initialLoadLimit", "madeLoadLimit", "showVerticalLines", "calendarLayout", "showWeekends", "mapHeight", "chartType",
  "chartColorTheme", "chartHeight", "chartShowDataLabels", "timelineZoom", "timelineShowTable",
  "timelineHideDependencies",
] as const;

function sourceFreeViewConfig(view: GraphView): Record<string, unknown> {
  return Object.fromEntries(SOURCE_FREE_VIEW_KEYS.flatMap((key) => key in view.config && !(key === "wrap" && view.type === "table")
    ? [[key, view.config[key]]] : []));
}

/**
 * A view of the database turned to another source, as Notion's view
 * settings "데이터베이스 ›" turns it (seen 2026-10-11): a source of the
 * database's own keeps the view with it; another database's is linked first,
 * the database owning the view of it. A linked source no other view of the
 * database shows any more leaves it, as Notion's linked view turned to
 * another source then lists that one alone under its sources (seen
 * 2026-11-08), the source staying in its own database.
 */
export function switchViewDataSourceOperations(
  block: GraphBlock, graph: ContentGraph, viewId: string, candidate: DatabaseContainerSource, defaults: Record<string, unknown>,
): GraphOperation[] | null {
  const view = graph.views.find(({ id }) => id === viewId);
  if (!view || view.dataSourceId === candidate.dataSourceId) return null;
  const { own, linked } = databaseBlockMembers(block);
  const owned = own.includes(candidate.dataSourceId);
  const member = owned || linked.some(({ dataSourceId }) => dataSourceId === candidate.dataSourceId);
  const config = sourceFreeViewConfig(view);
  const { containerId: _container, ...rest } = view;
  void _container;
  const switched: GraphView = { ...rest, dataSourceId: candidate.dataSourceId, ...(owned ? {} : { containerId: block.id }), config: { ...config, ...defaults } };
  const left = linked.some(({ dataSourceId }) => dataSourceId === view.dataSourceId)
    && !graph.views.some((other) => other.id !== viewId && other.containerId === block.id && other.dataSourceId === view.dataSourceId);
  const members = [...linked, ...(member ? [] : [{ dataSourceId: candidate.dataSourceId, sourceContainerId: candidate.id }])]
    .filter(({ dataSourceId }) => !left || dataSourceId !== view.dataSourceId);
  return [
    { kind: "upsertView", view: switched },
    ...(member && !left ? [] : [{ kind: "upsertBlock" as const, block: blockInPlace(withDatabaseBlockMembers(block, own, members)) }]),
  ];
}

/**
 * A view takes after one of its source's views in that source's own
 * database — layout, name and settings (viewTakenAfter) — as Notion's new
 * view does from "〈소스〉의 조회수" (seen 2026-10-11).
 */
export function adoptSourceViewOperations(graph: ContentGraph, viewId: string, saved: GraphView): GraphOperation[] | null {
  const view = graph.views.find(({ id }) => id === viewId);
  if (!view || view.dataSourceId !== saved.dataSourceId || saved.type === "dashboard" || isDashboardWidget(saved)) return null;
  // A widget stays on its dashboard.
  const dashboard = isDashboardWidget(view) ? { dashboardViewId: view.config.dashboardViewId } : {};
  const taken = viewTakenAfter(view, saved);
  return [{ kind: "upsertView", view: { ...taken, config: { ...taken.config, ...dashboard } } }];
}

// Removing a linked source drops the views the database owns for it; the
// source stays in its own database. Its views still waiting for a source of
// their own stand on another source it links (linkViewInPlaceOperations). A
// database keeps at least one source.
export function unlinkDataSourceOperations(
  block: GraphBlock, graph: ContentGraph, dataSourceId: string,
): GraphOperation[] | null {
  const { own, linked } = databaseBlockMembers(block);
  const remaining = linked.filter((member) => member.dataSourceId !== dataSourceId);
  if (remaining.length === linked.length || own.length + remaining.length === 0) return null;
  const standing = (view: GraphView) => view.config.linkPending === true && remaining.length > 0;
  const shown = graph.views.filter((view) => view.containerId === block.id && view.dataSourceId === dataSourceId);
  const removed = shown.filter((view) => !standing(view));
  const moved = shown.filter(standing).map((view) => ({ ...view, dataSourceId: remaining[0].dataSourceId }));
  const kept = [...moved, ...graph.views
    .filter((view) => view.containerId === block.id
      ? view.dataSourceId !== dataSourceId
      : !view.containerId && own.includes(view.dataSourceId))]
    .sort(compareGraphOrder);
  const viewId = removed.some(({ id }) => id === block.content.viewId) ? kept[0]?.id : undefined;
  if (removed.some(({ id }) => id === block.content.viewId) && !viewId) return null;
  return [
    ...moved.map((view): GraphOperation => ({ kind: "upsertView", view })),
    { kind: "upsertBlock", block: blockInPlace(withDatabaseBlockMembers(block, own, remaining, viewId)) },
    ...removed.map((view): GraphOperation => ({ kind: "deleteView", viewDeletion: { id: view.id } })),
  ];
}
