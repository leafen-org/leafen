import type { GraphView, GraphViewType } from "@/contentGraph";
import type { DatabaseContainerSource } from "@/lib/databaseContainerPicker";
import { linkedViewDefaults, newViewDefaults } from "@/components/database/viewLayoutDefaults";
import { fullPageDatabaseAtAddress } from "@/lib/inlineDatabaseAddress";

// The linked starter's order, observed in Notion 2026-10-09. Form is a
// source-owned view and the linked page follows the existing form policy.
export const DATABASE_STARTER_LINKED_TYPES: readonly GraphViewType[] = [
  "table", "board", "timeline", "calendar", "list", "gallery", "chart", "feed", "map", "dashboard",
];

/**
 * A linked view waiting for its view in "새 보기" (DatabaseLinkedStarter),
 * set as it stays when left so (linkedViewDefaults); a view that waited in
 * another layout keeps it, with that layout's first settings.
 */
export function linkedStarterViewConfig(source: Pick<DatabaseContainerSource, "id" | "viewType" | "viewConfig">, type: GraphViewType = "table"): Record<string, unknown> {
  return { ...(type === "table" ? linkedViewDefaults(source) : newViewDefaults(type, [])), linkedStarterPending: true, starterSourceContainerId: source.id };
}

/** A view's settings without what marks it waiting in "새 보기". */
export function withoutLinkedStarter(config: Record<string, unknown>): Record<string, unknown> {
  const { linkedStarterPending: _pending, starterSourceContainerId: _source, ...rest } = config;
  void _pending; void _source;
  return rest;
}

/** A waiting view left without a choice: the layout it waited as (a table, a board), unnamed. */
export function leftLinkedView(view: GraphView): GraphView {
  return { ...view, name: "", config: withoutLinkedStarter(view.config) };
}

// The linked views this reading is making, by the page each shows on. A view
// just linked to a source asks for its view in "새 보기" here alone and once,
// as Notion's (seen 2026-11-09): its page opened again, read again or seen by
// anyone else shows the window no more, and a view whose window went without
// a choice as its page was left is the unnamed table it waited as.
const making = new Map<string, { path: string } | "left">();

// The page shown now, by its path. A page that became a full-page database
// shows at its database's address from then on (inlineDatabaseAddress.ts):
// the same page, not one left — the linked view an empty page started as is
// drawn at the page's path first when its read comes before the page list's
// (2026-11-18: its "새 보기" then went with the address, the view left an
// unnamed table).
function shownPath() {
  if (typeof window === "undefined") return "";
  const { pathname } = window.location, page = fullPageDatabaseAtAddress(pathname);
  return page ? `/p/${encodeURIComponent(page)}` : pathname;
}

/** Notes a linked view made or shown asking for its view, on the page shown now. */
export function beginLinkedViewMaking(viewId: string) {
  making.set(viewId, { path: shownPath() });
}

/** Whether the view, waiting in "새 보기", asks for its view here. */
export function makingLinkedView(viewId: string) {
  const entry = making.get(viewId);
  return entry !== undefined && entry !== "left";
}

/**
 * Its "새 보기" gone: chosen or closed, the making is over; gone as its page
 * was left, the view was left unchosen; drawn again on the same page, it asks on.
 */
export function endLinkedViewMaking(viewId: string, chosen: boolean) {
  const entry = making.get(viewId);
  if (chosen) making.delete(viewId);
  else if (entry && entry !== "left" && entry.path !== shownPath()) making.set(viewId, "left");
}

/** Whether a view's "새 보기" was left without a choice, once: it is then put as left (leftLinkedView). */
export function takeLeftLinkedView(viewId: string) {
  if (making.get(viewId) !== "left") return false;
  making.delete(viewId);
  return true;
}

/**
 * Whether the view asks for its view in "새 보기" here, now: linked and
 * waiting (linkedStarterViewConfig) where this reading made it
 * (makingLinkedView). It is called "새 보기" there alone, unnamed as stored:
 * opened by any other reading — after a reload, in another tab or on
 * another device — it is the unnamed table it waits as, its layout's name
 * and no window, as Notion's (seen 2026-11-18 in two tabs: "새 보기" in the
 * tab whose window stands, "표" in the other; after a reload "표").
 */
export function askingLinkedViewHere(view: { id?: string; config?: object }) {
  const waits = (view.config as { linkedStarterPending?: unknown } | undefined)?.linkedStarterPending === true;
  return waits && view.id !== undefined && makingLinkedView(view.id);
}
