// A table just made from "/" shows only "⤢", settings and "새로 만들기"
// until its first property or row, or until the page is read again, as
// Notion's does (seen 2026-11-02 in three workspaces: filter, sort,
// automations and search come with the first change or a reload; a board or
// a list shows them at once, seen 2026-11-06).
const fresh = new Map<string, number>();

/** Notes a database just made, with how many properties it was made with. */
export function markFreshDatabase(dataSourceId: string, properties: number) {
  fresh.set(dataSourceId, properties);
}

/** Whether a database is still as it was made in this reading. */
export function isFreshDatabase(dataSourceId: string, properties: number, rows: number) {
  const made = fresh.get(dataSourceId);
  return made !== undefined && made === properties && rows === 0;
}

// One turned into a simple table as it was made comes back so with the
// turn's Undo, as Notion's (seen 2026-11-13: turned, then ⌘Z, and ⌘⇧Z with
// ⌘Z again, it showed only "⤢", settings and "새로 만들기"): its mark outlasts
// its toolbar going while it is a simple table, till it first changes or its
// page is left.
const turned = new Set<string>();

/** Keeps a database just made so through its turn into a simple table and that turn's Undo and Redo. */
export function keepFreshDatabaseTurned(dataSourceId: string) {
  if (fresh.has(dataSourceId)) turned.add(dataSourceId);
}

/** Lets go of a database once it has changed, for good. */
export function settleFreshDatabase(dataSourceId: string) {
  fresh.delete(dataSourceId);
  turned.delete(dataSourceId);
}

// The page's Undo taking one just made away and its Redo bringing it back
// leave it as it was made, as Notion's (seen 2026-11-12: made from "/", ⌘Z,
// ⌘⇧Z, it showed only "⤢" and settings): a toolbar going in the frames of a
// history step keeps its mark as a turned one's.
const stepping = new Set<string>();

/** Keeps the databases just made so through a history step of the page, its toolbars going meanwhile. */
export function keepFreshDatabasesThroughStep() {
  if (!fresh.size) return;
  for (const dataSourceId of fresh.keys()) stepping.add(dataSourceId);
  window.requestAnimationFrame(() => window.requestAnimationFrame(() => stepping.clear()));
}

/** Its toolbar gone, drawn again it shows all its tools, but for one turned into a simple table on a page still shown. */
export function leaveFreshDatabase(dataSourceId: string, pageLeft: boolean) {
  if (!pageLeft && stepping.has(dataSourceId)) turned.add(dataSourceId);
  if (pageLeft || !turned.has(dataSourceId)) settleFreshDatabase(dataSourceId);
}

// A form just made from "/" takes the caret in its title without moving the
// page, as Notion's (seen 2026-11-05; 2026-11-16 in a 723 window, a page of a
// title and 13 lines: the form came in at 619 with its title below the window
// and the page stayed; the next letter went into the form's title, the page
// scrolling only as far as that letter needed — checked after the action by
// typing one letter, twice in two tabs).
const freshForms = new Set<string>();
export function markFreshForm(viewId: string) {
  freshForms.add(viewId);
}
/** Whether a form's view was just made, once. */
export function takeFreshForm(viewId: string) {
  return freshForms.delete(viewId);
}
