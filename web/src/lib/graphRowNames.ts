import { useLayoutEffect, useMemo, useReducer, useRef } from "react";
import type { ContentGraph, GraphProperty, GraphRow, GraphRowValue } from "@/contentGraph";
import type { TextSpan } from "@/lib/types";
import { itemsReplacedFrom, noteItemsReplaced, replaceItems } from "./rowsReplaced";
import {
  flushPageNames, followPageNames, newerPageName, notedPageName, notePageName, pageNameKnown, sameName, type HeldPageName, type PageNameFields,
} from "./pageNameRecords";

/*
 * A page body's database rows named by the owner of pages' names
 * (pageNameRecords.ts), as the body is drawn: what the body holds of each
 * row's name is noted there, and a row it holds named as it was — a body
 * read before the row was titled on its page, a relation's row renamed in
 * another table, a row being titled in a sidebar rename — is drawn with the
 * name known now, so its table cell, card, relation chip, filter page and
 * group read it with no place of their own following it (user, 2026-11-20 on
 * the NAS: the page made from a template, titled "테스트22", still named "새
 * 페이지" in its own body's filter, as Notion names it at once).
 */

// Each data source's title property, by the properties drawn.
const titleIdsOf = new WeakMap<readonly GraphProperty[], ReadonlyMap<string, string>>();

function titleIds(properties: readonly GraphProperty[]) {
  let ids = titleIdsOf.get(properties);
  if (!ids) {
    ids = new Map(properties.filter((property) => property.type === "title").map((property) => [property.dataSourceId, property.id]));
    titleIdsOf.set(properties, ids);
  }
  return ids;
}

// Each row's name, by the row.
const rowNames = new WeakMap<GraphRow, HeldPageName>();

function rowName(row: GraphRow, titleId: string | undefined): HeldPageName {
  let name = rowNames.get(row);
  if (name) return name;
  const value = titleId ? row.values[titleId] : undefined;
  name = {
    id: row.id, title: typeof value?.value === "string" ? value.value : "", titleSpans: value?.spans as TextSpan[] | undefined,
    icon: row.icon ?? "", iconType: row.iconType ?? "none", updatedAt: row.updatedAt,
  };
  rowNames.set(row, name);
  return name;
}

// A row drawn with a newer name, by the row and that name.
const renamedRows = new WeakMap<GraphRow, { name: PageNameFields; row: GraphRow }>();

function renamedRow(row: GraphRow, titleId: string | undefined, name: PageNameFields): GraphRow {
  const kept = renamedRows.get(row);
  if (kept && sameName(kept.name, name)) return kept.row;
  const was = titleId ? row.values[titleId] : undefined;
  const value: GraphRowValue = {
    type: was?.type ?? "title", value: name.title,
    ...(name.titleSpans?.length ? { spans: name.titleSpans as GraphRowValue["spans"] } : {}),
  };
  const renamed: GraphRow = {
    ...row,
    ...(name.iconType !== undefined ? { icon: name.icon ?? "", iconType: name.iconType as GraphRow["iconType"] } : {}),
    values: titleId ? { ...row.values, [titleId]: value } : row.values,
  };
  renamedRows.set(row, { name, row: renamed });
  return renamed;
}

type Drawn = {
  /** The rows last noted. */
  rows: readonly GraphRow[] | null;
  /** Rows whose name may be known newer than the body holds. */
  stale: Set<string>;
  /** Where each row stands, for the rows it was made for. */
  index: Map<string, number> | null;
  indexed: readonly GraphRow[] | null;
};

function indexOf(drawn: Drawn, rows: readonly GraphRow[]) {
  if (drawn.indexed !== rows || !drawn.index) {
    drawn.index = new Map(rows.map((row, at) => [row.id, at]));
    drawn.indexed = rows;
  }
  return drawn.index;
}

/**
 * Notes the rows' names where they changed: a row changed in place (a cell
 * or a page's head written here, another's change come in) as this screen's
 * own — only where its name changed, so a checkbox ticked doesn't name the
 * row again as the body held it — and the rows of a body read as read, which
 * don't name a row over a name of the same last edit or later. A row whose
 * name wasn't taken is named anew as it is drawn.
 */
function noteRows(drawn: Drawn, rows: readonly GraphRow[], titles: ReadonlyMap<string, string>) {
  if (rows === drawn.rows) return;
  const before = drawn.rows, made = itemsReplacedFrom(rows);
  const note = (row: GraphRow, name: HeldPageName, from: "local" | "read") => {
    if (notePageName(row.id, name, from, name, drawn)) drawn.stale.delete(row.id);
    else if (!notedPageName(name)) drawn.stale.add(row.id);
  };
  if (before && made?.from === before) {
    for (const at of made.at) {
      const row = rows[at], was = before[at], name = rowName(row, titles.get(row.dataSourceId));
      if (was?.id === row.id && sameName(rowName(was, titles.get(was.dataSourceId)), name)) continue;
      note(row, name, "local");
    }
  } else {
    // A read names anew only the rows named elsewhere already, or typed: one
    // nothing else names has nothing to tell and nothing newer to be named
    // by (a 100,000-row table read again noted every row).
    for (const row of rows) {
      if (!pageNameKnown(row.id)) continue;
      const name = rowName(row, titles.get(row.dataSourceId));
      if (!notedPageName(name)) note(row, name, "read");
    }
  }
  drawn.rows = rows;
}

function namedGraph(graph: ContentGraph, drawn: Drawn, titles: ReadonlyMap<string, string>): ContentGraph {
  if (!drawn.stale.size) return graph;
  const index = indexOf(drawn, graph.rows);
  const replaced = new Map<number, GraphRow>();
  for (const id of [...drawn.stale]) {
    const at = index.get(id);
    const row = at === undefined ? undefined : graph.rows[at];
    const titleId = row ? titles.get(row.dataSourceId) : undefined;
    const name = row ? newerPageName(rowName(row, titleId)) : undefined;
    if (!row || at === undefined || !name) {
      drawn.stale.delete(id);
      continue;
    }
    replaced.set(at, renamedRow(row, titleId, name));
  }
  if (!replaced.size) return graph;
  const rows = replaceItems(graph.rows, replaced);
  noteItemsReplaced(rows, graph.rows, [...replaced.keys()]);
  return { ...graph, rows };
}

/** The body's graph with its rows named as the owner of pages' names knows them now. */
export function useGraphRowNames(graph: ContentGraph): ContentGraph {
  const [heard, redraw] = useReducer((count: number) => count + 1, 0);
  const own = useRef<Drawn | null>(null);
  const drawn = (own.current ??= { rows: null, stale: new Set(), index: null, indexed: null });
  const shown = useMemo(() => {
    const titles = titleIds(graph.properties);
    noteRows(drawn, graph.rows, titles);
    return namedGraph(graph, drawn, titles);
  // A name heard changed elsewhere draws the rows named anew.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [graph, drawn, heard]);
  // What this drawing noted is told once it is drawn.
  useLayoutEffect(() => flushPageNames(), [shown]);
  useLayoutEffect(() => followPageNames((ids, by) => {
    if (by === drawn || !drawn.rows) return;
    const index = indexOf(drawn, drawn.rows);
    let held = false;
    // The fewer of the two looked through: a whole list read names every page.
    for (const id of ids.size <= index.size ? ids : index.keys()) {
      if (!index.has(id) || !ids.has(id)) continue;
      drawn.stale.add(id);
      held = true;
    }
    if (held) redraw();
  }), [drawn]);
  return shown;
}
