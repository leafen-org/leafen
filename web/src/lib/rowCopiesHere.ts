import type { GraphRow, GraphRowValue } from "@/contentGraph";
import { duplicateRowCopyId } from "@/databaseRowPageApi";
import { graphPositionBetween } from "@/lib/graphPosition";
import { pageCopyTitle } from "@/lib/pageDuplicationService";
import { rowsWithSubitems } from "@/lib/rowsShownAtPress";

// A database item's copy is drawn at the press, with its sub-items' copies,
// under the IDs the server gives them, and made by the server behind (user
// decision 2026-11-12: a press answers on the next frame — ⌘D, a row's and a
// card's "복제" showed the copies only once the server had made them, 645–737
// ms after the press on a NAS-class server, 2026-11-14). It is drawn as the
// server makes it (database_row_duplicate_tree.go planDatabaseRowDuplicate),
// so the server's rows take its place unchanged.

// The values the database numbers or works out for each item, which a copy
// doesn't take (databaseRowDuplicateUncopiedTypes): an ID reads "..." until
// its number comes (uniqueIdText.ts).
const UNCOPIED_TYPES = new Set(["unique_id", "created_time", "last_edited_time", "created_by", "last_edited_by", "last_visited_time"]);

/** Each row of an item's tree — it and its sub-items — by its copy's ID, as the server names them for the request. */
export async function rowCopyIds(userId: string, rows: readonly GraphRow[], rootId: string, requestId: string): Promise<ReadonlyMap<string, string>> {
  const tree = [...rowsWithSubitems(rows, [rootId])];
  const ids = await Promise.all(tree.map((id) => duplicateRowCopyId(userId, id, requestId)));
  return new Map(tree.map((id, index) => [id, ids[index]!]));
}

/**
 * A title as its copy's: "Name (1)", the formatting of the words it keeps
 * kept and its number plain (duplicate_copy_title.go duplicateCopyTitleValue).
 */
function copyTitleValue(value: GraphRowValue): GraphRowValue {
  const title = typeof value.value === "string" ? value.value : "";
  if (!title) return value;
  const named = pageCopyTitle(title);
  const kept = named === `${title} (1)` ? title.length : named.lastIndexOf("(");
  if (!value.spans?.length) return { ...value, value: named };
  const spans: NonNullable<GraphRowValue["spans"]> = [];
  let remaining = kept;
  for (const span of value.spans) {
    if (remaining <= 0) break;
    const text = span.text.slice(0, remaining);
    remaining -= text.length;
    spans.push({ ...span, text });
  }
  return { ...value, value: named, spans: [...spans, { text: named.slice(kept) }] };
}

/**
 * The places of copies of rows of one database, by the place each copies:
 * right after it, before the first row of the database whose place sorts
 * after its own, as the server gives them (database_row_copy_place.go
 * databaseRowCopyPlace). One pass over the rows, the database not sorted at
 * the press: a row is the next only of the copied place last below its own,
 * a copied row itself being next of the one before. A copy's place was its
 * row's with "~" and its ID added, so a copy of a copy grew by 37 characters
 * each time and the seventh was refused (2026-11-25).
 */
function copyPlaces(rows: readonly GraphRow[], dataSourceId: string, copied: readonly string[]) {
  const lows = [...new Set(copied)].sort();
  const next: (string | undefined)[] = lows.map(() => undefined);
  for (const row of rows) {
    if (row.dataSourceId !== dataSourceId) continue;
    let low = 0, high = lows.length;
    while (low < high) {
      const middle = (low + high) >> 1;
      if (lows[middle]! < row.position) low = middle + 1;
      else high = middle;
    }
    const below = low - 1;
    if (below >= 0 && (next[below] === undefined || row.position < next[below]!)) next[below] = row.position;
  }
  return new Map(lows.map((place, index) => {
    try {
      return [place, graphPositionBetween(place, next[index])];
    } catch {
      // None left between them: the server refuses the copy.
      return [place, place];
    }
  }));
}

/**
 * The copies of an item and its sub-items as the server makes them: the
 * item's right after it, or at `position`, each sub-item's right after its
 * own under its parent's copy; values the person may not change left for the
 * server, relations within the tree pointing at the copies, the title named
 * as a copy's, made now by the person.
 */
export function rowCopiesHere(rows: readonly GraphRow[], targets: ReadonlyMap<string, string>, rootId: string,
  position: string | undefined, userId: string, now: string): GraphRow[] {
  const copied = rows.filter((row) => targets.has(row.id));
  const places = copyPlaces(rows, copied[0]?.dataSourceId ?? "", copied.map((row) => row.position));
  return rows.flatMap((row) => {
    const id = targets.get(row.id);
    if (!id) return [];
    const root = row.id === rootId;
    const unwritable = new Set([...row.readOnlyValues ?? [], ...row.hiddenValues ?? []]);
    const values = Object.fromEntries(Object.entries(row.values).flatMap(([propertyId, value]) => {
      if (!value || UNCOPIED_TYPES.has(value.type) || unwritable.has(propertyId)) return [];
      if (value.type === "title") return [[propertyId, copyTitleValue(value)]];
      if (value.type === "relation" && Array.isArray(value.value)) {
        return [[propertyId, { ...value, value: value.value.map((related) => typeof related === "string" ? targets.get(related) ?? related : related) }]];
      }
      return [[propertyId, value]];
    }));
    const parent = row.subitemParentId;
    return [{
      id, dataSourceId: row.dataSourceId, subitemParentId: root || !parent ? parent : targets.get(parent) ?? parent,
      position: root && position ? position : places.get(row.position)!, values,
      ...(row.icon !== undefined ? { icon: row.icon, iconType: row.iconType } : {}),
      ...(row.cover !== undefined ? { cover: row.cover, coverPosition: row.coverPosition } : {}),
      ...(row.hasContent !== undefined ? { hasContent: row.hasContent } : {}),
      ...(row.hiddenValues ? { hiddenValues: row.hiddenValues } : {}), ...(row.readOnlyValues ? { readOnlyValues: row.readOnlyValues } : {}),
      createdAt: now, createdBy: userId, updatedAt: now, lastEditedBy: userId,
    }];
  });
}
