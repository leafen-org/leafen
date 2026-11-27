import { useEffect, useState, useSyncExternalStore } from "react";
import { request } from "@/apiTransport";
import type { GraphViewType } from "@/contentGraph";
import type { Page } from "@/lib/types";
import type { Session } from "@/types";
import { keepRowPageReads, readKeptRowPage } from "./keptRowPage";
import { isServerUnreachable } from "./serverReach";
import { useReadAgainWhenAnswered } from "./unansweredRead";

// A database's page layout, as Notion's "레이아웃 사용자 지정" (seen
// 2026-10-12): one per data source, for every page in it. Read once per
// data source and shared by its pages and the layout builder, so applying
// it shows on every open page.

export type PageLayoutModule =
  | { id: string; kind: "properties" }
  | { id: string; kind: "property"; propertyId: string }
  | { id: string; kind: "comments" }
  | { id: string; kind: "content" }
  /** Notion's "관계형 그룹" (seen 2026-11-06): relation properties, each one line or a section of the page. */
  | { id: string; kind: "relations"; relations: { propertyId: string; display: "minimized" | "section" }[] };

export type PageLayoutGroupItem =
  | { kind: "section"; id: string; name: string }
  | { kind: "property"; propertyId: string };

export type DatabasePageLayout = {
  heading: { pinned: string[]; labels: boolean; backlinks: "" | "show" | "hover" | "off" };
  page: PageLayoutModule[];
  panel: PageLayoutModule[];
  /** Kept as layouts carry it; a page's panel opens closed and the builder's column unfolded whatever it says (Notion's, seen 2026-11-12). */
  panelOpen: boolean;
  propertyGroup: PageLayoutGroupItem[];
  structure: "simple" | "tabbed";
  tabs: PageLayoutTab[];
  /** The content tab's own name, icon and description, as Notion's "설정 보기" keeps them (seen 2026-11-01). */
  contentTab?: PageLayoutContentTab;
  options: {
    inlineComments: "default" | "minimal";
    pageComments: boolean;
    propertyIcons: boolean;
    fullWidth: boolean;
    templates: boolean;
  };
};

/**
 * A tab beside "콘텐츠" (tabbed structure): a data source's pages, those the
 * open page relates to through relationPropertyId, or all of them, in a view
 * of their own (a table until it is set).
 */
export type PageLayoutTab = { id: string; dataSourceId: string; relationPropertyId?: string; name?: string; view?: PageLayoutTabView };

/**
 * The content tab's name (else "콘텐츠"), a built-in icon (else a page's), a
 * description, and its place: after as many data tabs as `position` says,
 * first when unset (pageLayoutTabOrder).
 */
export type PageLayoutContentTab = { name?: string; icon?: string; description?: string; position?: number };

/** A tab's layout and settings, as a database view's config. */
export type PageLayoutTabView = { type: GraphViewType; config: Record<string, unknown> };

export type DatabasePageLayoutState = {
  dataSourceId: string;
  revision: number;
  layout: DatabasePageLayout | null;
  loaded: boolean;
  /** The server wasn't there to read it: laid out as the original page, or as a database page kept it. */
  unanswered?: boolean;
};

/** A data source's page layout as the server answered it. */
export type DatabasePageLayoutAnswer = { revision: number; layout: DatabasePageLayout | null };

export const PAGE_LAYOUT_MAX_PINNED = 15;

/**
 * "레이아웃 사용자 지정" asks the database's own view to open the layout
 * builder, the view holding its properties and pages; a database page
 * names its data source.
 */
export const OPEN_PAGE_LAYOUT_EVENT = "leafen:open-page-layout";
export type OpenPageLayoutDetail = { pageId: string; dataSourceId?: string; rowId?: string;
  /** A page that isn't one of its rows, previewed first and listed first: a template opening it (Notion's, seen 2026-11-16). */
  preview?: Page };

export function requestPageLayoutBuilder(detail: OpenPageLayoutDetail) {
  // The first view of the database that hears it opens the builder.
  window.dispatchEvent(new CustomEvent<OpenPageLayoutDetail>(OPEN_PAGE_LAYOUT_EVENT, { detail, cancelable: true }));
}

/** The layout a database's pages have before one is applied: Notion's original page. */
export function defaultDatabasePageLayout(): DatabasePageLayout {
  return {
    heading: { pinned: [], labels: true, backlinks: "" },
    page: [{ id: "properties", kind: "properties" }, { id: "comments", kind: "comments" }, { id: "content", kind: "content" }],
    panel: [],
    panelOpen: false,
    propertyGroup: [],
    structure: "simple",
    tabs: [],
    options: { inlineComments: "default", pageComments: true, propertyIcons: true, fullWidth: false, templates: true },
  };
}

/** The properties a layout places outside its property group: pinned under the title, or modules of their own. */
export function pageLayoutPlacedProperties(layout: DatabasePageLayout): Set<string> {
  const placed = new Set(layout.heading.pinned);
  for (const module of [...layout.page, ...layout.panel]) {
    if (module.kind === "property") placed.add(module.propertyId);
    if (module.kind === "relations") for (const relation of module.relations) placed.add(relation.propertyId);
  }
  return placed;
}

const path = (pageId: string, dataSourceId: string) =>
  `/pages/${encodeURIComponent(pageId)}/data-sources/${encodeURIComponent(dataSourceId)}/page-layout`;

type LayoutResponse = { dataSourceId: string; revision: number; layout: DatabasePageLayout | null };

const empty = (dataSourceId: string): DatabasePageLayoutState => ({ dataSourceId, revision: 0, layout: null, loaded: false });
const states = new Map<string, DatabasePageLayoutState>();
const loading = new Map<string, Promise<void>>();
const listeners = new Set<() => void>();

function publish(response: LayoutResponse, unanswered = false) {
  states.set(response.dataSourceId, { dataSourceId: response.dataSourceId, revision: response.revision, layout: response.layout, loaded: true,
    ...(unanswered ? { unanswered } : {}) });
  for (const listener of listeners) listener();
}

function load(session: Session, pageId: string, dataSourceId: string): Promise<void> {
  const pending = loading.get(dataSourceId);
  if (pending) return pending;
  const next = request<LayoutResponse>(path(pageId, dataSourceId), {}, session)
    .then(publish)
    // A page whose layout can't be read lays out as the original page.
    .catch((error: unknown) => publish({ dataSourceId, revision: 0, layout: null }, isServerUnreachable(error)))
    .finally(() => loading.delete(dataSourceId));
  loading.set(dataSourceId, next);
  return next;
}

/** A data source's page layout, read the first time one of its pages asks; null keeps the original page. */
export function useDatabasePageLayout(
  session: Session | null | undefined,
  pageId: string | undefined,
  dataSourceId: string | undefined,
  /** The database page drawn with it, which keeps it to draw with when the server isn't there (keptRowPage.ts). */
  keptWith?: string,
): DatabasePageLayoutState {
  const state = useSyncExternalStore(
    (listener) => { listeners.add(listener); return () => { listeners.delete(listener); }; },
    () => (dataSourceId ? states.get(dataSourceId) ?? emptyState(dataSourceId) : NO_SOURCE),
  );
  useEffect(() => {
    if (session && pageId && dataSourceId) void load(session, pageId, dataSourceId);
  }, [session, pageId, dataSourceId]);
  useReadAgainWhenAnswered(Boolean(state.unanswered), () => {
    if (session && pageId && dataSourceId) void load(session, pageId, dataSourceId);
  });
  const [kept, setKept] = useState<{ pageId: string; answer: DatabasePageLayoutAnswer } | null>(null);
  useEffect(() => {
    if (!session || !keptWith || !state.loaded) return undefined;
    if (!state.unanswered) {
      keepRowPageReads(session, keptWith, { layout: { revision: state.revision, layout: state.layout } });
      return undefined;
    }
    let current = true;
    void readKeptRowPage(session, keptWith).then((reads) => {
      if (current && reads?.layout) setKept({ pageId: keptWith, answer: reads.layout });
    });
    return () => { current = false; };
  }, [keptWith, session, state]);
  return state.unanswered && kept && kept.pageId === keptWith ? { ...state, ...kept.answer } : state;
}

const NO_SOURCE = empty("");
const emptyStates = new Map<string, DatabasePageLayoutState>();
function emptyState(dataSourceId: string) {
  let state = emptyStates.get(dataSourceId);
  if (!state) emptyStates.set(dataSourceId, state = empty(dataSourceId));
  return state;
}

/** Applies a layout to every page of the data source ("모든 페이지에 적용"). */
export async function applyDatabasePageLayout(session: Session, pageId: string, dataSourceId: string, expectedRevision: number, layout: DatabasePageLayout) {
  publish(await request<LayoutResponse>(path(pageId, dataSourceId), {
    method: "PUT", body: JSON.stringify({ expectedRevision, layout }),
  }, session));
}

/**
 * Saves a tab's view for every page of the data source, as changing it on
 * one does in Notion; a layout changed meanwhile is read again first.
 */
export async function saveDatabasePageLayoutTabView(session: Session, pageId: string, dataSourceId: string, tabId: string, view: PageLayoutTabView) {
  for (let attempt = 0; ; attempt++) {
    const state = states.get(dataSourceId);
    if (!state?.layout) return;
    const layout = { ...state.layout, tabs: state.layout.tabs.map((tab) => (tab.id === tabId ? { ...tab, view } : tab)) };
    try {
      await applyDatabasePageLayout(session, pageId, dataSourceId, state.revision, layout);
      return;
    } catch (error) {
      if (attempt > 0 || (error as { code?: string }).code !== "LEAFEN_DATABASE_PAGE_LAYOUT_CONFLICT") throw error;
      loading.delete(dataSourceId);
      await load(session, pageId, dataSourceId);
    }
  }
}

/** The comments a data source's pages show: its page comments, and its inline comments in full or as a mark. */
export type DatabasePageLayoutComments = Partial<Pick<DatabasePageLayout["options"], "pageComments" | "inlineComments">>;

/** Those comments as a page menu's switch and choice show them: the original page's until a layout says otherwise. */
export function pageLayoutCommentsStyle(layout: DatabasePageLayout | null | undefined) {
  return { pageCommentsDisplay: layout?.options.pageComments === false ? "off" : "default",
    inlineCommentsDisplay: layout?.options.inlineComments === "minimal" ? "minimal" : "default" } as const;
}

/** A page menu's change of its comments switch or choice, as the comments its data source's pages show. */
export function pageLayoutCommentsChange(change: { pageCommentsDisplay?: string; inlineCommentsDisplay?: string }): DatabasePageLayoutComments {
  return {
    ...(change.pageCommentsDisplay ? { pageComments: change.pageCommentsDisplay !== "off" } : {}),
    ...(change.inlineCommentsDisplay ? { inlineComments: change.inlineCommentsDisplay === "minimal" ? "minimal" : "default" } : {}),
  };
}

/**
 * Changes only which comments the data source's pages show, on the layout as
 * last read (the original page's until one is applied) — what its database's
 * lock leaves changeable, from a page's "페이지 사용자 지정", as Notion's
 * (seen 2026-11-16 twice on the A90, the second row's page changing with the
 * first's). Every open page of the source shows it at once; the changes are
 * written one after another, those made meanwhile kept on top of each answer;
 * a refusal reads the layout again, which puts it back, and a layout changed
 * meanwhile is read again and changed once more.
 */
export async function saveDatabasePageLayoutComments(session: Session, pageId: string, dataSourceId: string, comments: DatabasePageLayoutComments) {
  if (!states.get(dataSourceId)?.loaded || states.get(dataSourceId)?.unanswered) return;
  const change = commentChanges.get(dataSourceId) ?? { pending: {}, writing: false };
  commentChanges.set(dataSourceId, change);
  change.pending = { ...change.pending, ...comments };
  showComments(dataSourceId, change.pending);
  if (change.writing) return;
  change.writing = true;
  try {
    for (let conflicts = 0; Object.keys(change.pending).length > 0;) {
      const sending = change.pending, shown = states.get(dataSourceId);
      change.pending = {};
      try {
        await applyDatabasePageLayout(session, pageId, dataSourceId, shown?.revision ?? 0, shown?.layout ?? defaultDatabasePageLayout());
        showComments(dataSourceId, change.pending);
      } catch (error) {
        loading.delete(dataSourceId);
        await load(session, pageId, dataSourceId);
        if (conflicts++ > 0 || (error as { code?: string }).code !== "LEAFEN_DATABASE_PAGE_LAYOUT_CONFLICT") { change.pending = {}; throw error; }
        change.pending = { ...sending, ...change.pending };
        showComments(dataSourceId, change.pending);
      }
    }
  } finally {
    commentChanges.delete(dataSourceId);
  }
}

const commentChanges = new Map<string, { pending: DatabasePageLayoutComments; writing: boolean }>();

/** The layout as read, with the comment changes not yet written on top. */
function showComments(dataSourceId: string, pending: DatabasePageLayoutComments) {
  const state = states.get(dataSourceId);
  if (!state || Object.keys(pending).length === 0) return;
  const base = state.layout ?? defaultDatabasePageLayout();
  publish({ dataSourceId, revision: state.revision, layout: { ...base, options: { ...base.options, ...pending } } });
}

/** Puts the data source's pages back to the original page ("원래 페이지 레이아웃으로 되돌리기"); no property is deleted. */
export async function resetDatabasePageLayout(session: Session, pageId: string, dataSourceId: string, expectedRevision: number) {
  publish(await request<LayoutResponse>(path(pageId, dataSourceId), {
    method: "DELETE", body: JSON.stringify({ expectedRevision }),
  }, session));
}
