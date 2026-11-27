import type { GraphProperty, GraphRow, GraphView } from "@/contentGraph";
import { holdTypingWhileRowOpens, requestPageTitleFocus } from "@/lib/pageTitleFocus";
import { projectRows } from "./viewRowProjection";

/** The view a row is made in, as its filters and search read a row. */
export type NewRowView = {
  view: GraphView;
  properties: readonly GraphProperty[];
  workspaceId: string;
  contextPageId?: string;
  currentUserId?: string;
};

/**
 * Whether a row made in a view with these values, its filters' (querySeeds.ts),
 * shows there; a search aside, which a row made in place lets go of
 * (tableRowAfterSearch). A "+ 새 페이지" — a table's, a list's, a board
 * column's, and that of a view showing nothing (DatabaseNoResults) — makes its
 * row where the view shows it, its name typed in place; one the view would
 * leave out opens at its title instead (makeRowThatOpens), as Notion's (seen
 * 2026-11-27 in Chrome, judged by the screen and one letter typed after the
 * press): a plain page's table and list showing nothing by a filter on the
 * name ("zz", "zzq", "zzqx") or the date ("이번 주") made their row in place
 * with that name or today's date, the letter typed there (each twice, two tabs
 * at different times); in a database template's body, a view kept to "this
 * page" — which no row holds there (NativeDatabaseViewMount) — opened the row's
 * page at its empty title from its board's "+ 새 페이지" showing nothing (twice),
 * and from its list's and its gallery's, a board column's "+ 새 페이지" and a
 * calendar day's "+" (once each), the letter typed next in the title.
 */
export function newRowShows(values: Readonly<Record<string, unknown>>, at: NewRowView): boolean {
  const given = at.properties.flatMap((property) => Object.hasOwn(values, property.id) && values[property.id] !== undefined
    ? [[property.id, { type: property.type, value: values[property.id] }]] : []);
  // Made now by the one pressing, as the server writes its times and people (a view kept to "created by me" shows it).
  const now = new Date().toISOString(), by = at.currentUserId;
  const row = { id: "new-row", dataSourceId: at.view.dataSourceId, subitemParentId: null, position: "", values: Object.fromEntries(given),
    createdAt: now, updatedAt: now, createdBy: by, lastEditedBy: by } as GraphRow;
  const view = { ...at.view, config: { ...at.view.config, search: undefined } };
  return projectRows({ currentUserId: at.currentUserId, pageId: at.contextPageId ?? "", properties: at.properties, rows: [row], view, workspaceId: at.workspaceId }).length > 0;
}

/** newRowShows for seeds as a list's and a board's give them: the title apart from the other values. */
export function newRowSeedsShow(seeds: { title?: string; properties: Readonly<Record<string, unknown>> }, at: NewRowView): boolean {
  const title = at.properties.find((property) => property.type === "title");
  return newRowShows(title && seeds.title != null ? { ...seeds.properties, [title.id]: seeds.title } : seeds.properties, at);
}

/**
 * The one way a view's press makes a row whose page opens: what is typed from
 * the press on is held for its title (holdTypingWhileRowOpens), which takes the
 * caret as the page opens (requestPageTitleFocus). The page opens as the row
 * shows — make() passes it on with the row as it is drawn (nativeTableRowSteps.ts)
 * — or else once the row is made; once. It opens however the view changed
 * meanwhile: the row made changes it by itself — it shows there or, in a
 * template's body, leaves it while the template's value takes it
 * (templateBodyRows.ts) — and opening it by the view as the press found it
 * left it shut (2026-11-27: a board's and a calendar's new row in a template's
 * body made with nothing opened, what was typed lost and a typed space
 * pressing the board's button again).
 */
export async function makeRowThatOpens<Row extends { id: string }>(
  make: (opened: (row: Row) => void) => Promise<Row | null | undefined>, open: (rowId: string) => void,
): Promise<Row | null> {
  let opened: string | null = null;
  const openOnce = (row: Row) => {
    if (opened !== null) return;
    opened = row.id;
    requestPageTitleFocus(row.id);
    open(row.id);
  };
  return holdTypingWhileRowOpens(async () => {
    const row = await make(openOnce);
    if (row) openOnce(row);
    return row ?? null;
  });
}
