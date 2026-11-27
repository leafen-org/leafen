import type {
  PageHistoryBlock, PageHistoryDatabase, PageHistoryDatabaseRow, PageHistorySnapshotResponse,
} from "@/pageHistoryWire.generated";

// What a version changed, as Notion's version history marks it (seen
// 2026-10-01): the block it changed outlined in blue, the words it added on
// its author's color's tint, and the words it took out struck through in
// that color where they stood. A database's rows are marked the same way: a
// new row on the tint, a changed row outlined with its changed values on the
// tint, and a row taken away kept where it stood, outlined, on red.

export type PageHistoryTextPart = { text: string; change?: "added" | "removed" };

// A page's maker and last editor, each with when, which a version reads from
// the versions (page_history_automatic_values.go), not a value it changed:
// Notion's version preview outlines the value an edit changed, never its
// last edited time or editor (seen 2026-11-18 at 1416).
const AUTOMATIC_VALUE_TYPES = new Set(["created_time", "created_by", "last_edited_time", "last_edited_by"]);

/** Whether a property's value in a version is an automatic one. */
function automaticValue(before?: { type?: string }, after?: { type?: string }) {
  return AUTOMATIC_VALUE_TYPES.has(after?.type ?? before?.type ?? "");
}

/** Two stored values the same apart from their keys' order, which a version's
 *  copy and a restored one keep differently: a restore isn't a change. */
function sameStoredValue(left: unknown, right: unknown): boolean {
  if (left === right) return true;
  if (typeof left !== "object" || typeof right !== "object" || left === null || right === null) return false;
  if (Array.isArray(left) || Array.isArray(right)) {
    return Array.isArray(left) && Array.isArray(right) && left.length === right.length
      && left.every((item, index) => sameStoredValue(item, right[index]));
  }
  const leftKeys = Object.keys(left), rightKeys = Object.keys(right);
  return leftKeys.length === rightKeys.length && leftKeys.every((key) => Object.hasOwn(right, key)
    && sameStoredValue((left as Record<string, unknown>)[key], (right as Record<string, unknown>)[key]));
}

// Past this many word pairs the comparison marks the block alone.
const MAX_TOKEN_PAIRS = 40_000;

function tokens(text: string) {
  return text.match(/\s+|[^\s]+/gu) ?? [];
}

function pushPart(parts: PageHistoryTextPart[], text: string, change?: PageHistoryTextPart["change"]) {
  const last = parts.at(-1);
  if (last && last.change === change) last.text += text;
  else parts.push(change ? { text, change } : { text });
}

/** The words of one text against another's, kept, added and removed, in reading order. */
export function pageHistoryTextChanges(before: string, after: string): PageHistoryTextPart[] {
  const from = tokens(before);
  const to = tokens(after);
  if (from.length * to.length > MAX_TOKEN_PAIRS) return after ? [{ text: after }] : [];
  // The longest run of words both share, then the rest as added or removed.
  const lengths = Array.from({ length: from.length + 1 }, () => new Array<number>(to.length + 1).fill(0));
  for (let i = from.length - 1; i >= 0; i -= 1) {
    for (let j = to.length - 1; j >= 0; j -= 1) {
      lengths[i][j] = from[i] === to[j] ? lengths[i + 1][j + 1] + 1 : Math.max(lengths[i + 1][j], lengths[i][j + 1]);
    }
  }
  const parts: PageHistoryTextPart[] = [];
  let i = 0;
  let j = 0;
  while (i < from.length || j < to.length) {
    if (i < from.length && j < to.length && from[i] === to[j]) {
      pushPart(parts, to[j]);
      i += 1;
      j += 1;
    } else if (i < from.length && (j === to.length || lengths[i + 1][j] >= lengths[i][j + 1])) {
      // A replaced word reads taken out, then put in.
      pushPart(parts, from[i], "removed");
      i += 1;
    } else {
      pushPart(parts, to[j], "added");
      j += 1;
    }
  }
  return parts;
}

/**
 * The blocks a version changed from the one before it, each with its text's
 * parts: a new block's text added whole, a changed one's words compared. A
 * restore brings its new blocks back whole, their words as they read, each
 * among `returned` (the preview tints the block, PageHistoryPreview).
 */
export function pageHistoryBlockChanges(
  before: readonly PageHistoryBlock[],
  after: readonly PageHistoryBlock[],
  textOf: (block: PageHistoryBlock) => string,
  /** Whether `before` holds all its version's blocks; a partial one marks no block as new. */
  beforeComplete = true,
  returned?: Set<string>,
) {
  const previous = new Map(before.map((block) => [block.id, block]));
  const changes = new Map<string, PageHistoryTextPart[]>();
  for (const block of after) {
    const old = previous.get(block.id);
    const text = textOf(block);
    if (!old) {
      if (!beforeComplete) continue;
      returned?.add(block.id);
      changes.set(block.id, text ? [returned ? { text } : { text, change: "added" }] : []);
      continue;
    }
    if (
      old.type === block.type && old.parentId === block.parentId && old.plainText === block.plainText
      && sameStoredValue(old.content, block.content)
    ) continue;
    changes.set(block.id, pageHistoryTextChanges(textOf(old), text));
  }
  return changes;
}

export type PageHistoryRowMark = {
  row: PageHistoryDatabaseRow;
  change?: "added" | "changed" | "removed";
  /** A changed row's properties whose values changed. */
  changed?: ReadonlySet<string>;
};

/**
 * A version's database rows against the entry before it, in the order the
 * version shows them with each row taken away after the row it followed.
 * Without the database before, every row is new; rows past the first rows
 * either side reads are left unmarked.
 */
export function pageHistoryDatabaseRowChanges(
  before: PageHistoryDatabase | undefined,
  after: PageHistoryDatabase,
): PageHistoryRowMark[] {
  const afterRows = after.rows ?? [];
  if (!before) return afterRows.map((row) => ({ row, change: "added" }));
  const beforeRows = before.rows ?? [];
  const previous = new Map(beforeRows.map((row) => [row.id, row]));
  const marks = afterRows.map((row): PageHistoryRowMark => {
    const old = previous.get(row.id);
    if (!old) return before.hasMoreRows ? { row } : { row, change: "added" };
    const changed = new Set<string>();
    for (const propertyId of new Set([...Object.keys(old.values), ...Object.keys(row.values)])) {
      if (!automaticValue(old.values[propertyId], row.values[propertyId])
        && !sameStoredValue(old.values[propertyId] ?? null, row.values[propertyId] ?? null)) changed.add(propertyId);
    }
    return changed.size ? { row, change: "changed", changed } : { row };
  });
  if (after.hasMoreRows) return marks;
  const kept = new Set(afterRows.map((row) => row.id));
  const removedAfter = new Map<string, PageHistoryRowMark[]>();
  let followed = "";
  for (const row of beforeRows) {
    if (kept.has(row.id)) {
      followed = row.id;
      continue;
    }
    removedAfter.set(followed, [...(removedAfter.get(followed) ?? []), { row, change: "removed" }]);
  }
  return [
    ...(removedAfter.get("") ?? []),
    ...marks.flatMap((mark) => [mark, ...(removedAfter.get(mark.row.id) ?? [])]),
  ];
}

/**
 * Each database of a version against the entry before it, by its block. A
 * version's databases come whole with its first blocks, so one missing
 * before is new.
 */
export function pageHistoryDatabaseChanges(before: readonly PageHistoryDatabase[], after: readonly PageHistoryDatabase[]) {
  const previous = new Map(before.map((database) => [database.blockId, database]));
  return new Map(after.map((database) => [
    database.blockId, pageHistoryDatabaseRowChanges(previous.get(database.blockId), database),
  ]));
}

/**
 * Each database's properties whose names a version changed from the entry
 * before it, by its block, as Notion marks a renamed property's column head
 * (seen 2026-10-01).
 */
export function pageHistoryRenamedProperties(before: readonly PageHistoryDatabase[], after: readonly PageHistoryDatabase[]) {
  const previous = new Map(before.map((database) => [database.blockId, database]));
  const renamed = new Map<string, ReadonlySet<string>>();
  for (const database of after) {
    const names = new Map((previous.get(database.blockId)?.properties ?? []).map((property) => [property.id, property.name]));
    const changed = new Set((database.properties ?? [])
      .filter((property) => names.has(property.id) && names.get(property.id) !== property.name)
      .map((property) => property.id));
    if (changed.size) renamed.set(database.blockId, changed);
  }
  return renamed;
}

/**
 * The databases a version changed themselves, not only their rows, by their
 * blocks: their properties (a name, a kind, an option) or their views, as
 * Notion counts its database block as a change of its own and outlines it
 * (seen 2026-11-13 and again 2026-11-15, a property renamed by hand and
 * through the API); and a row added, which a person adds through a view,
 * changing its rows' order (Notion's "1 / 2개 변경 사항", the database then
 * the row, seen 2026-11-13 and 2026-11-15 by hand; a row only the API added
 * left the view alone, one change, which a version here can't tell apart).
 * A row's value changed alone leaves the database block unmarked, as
 * Notion's (seen 2026-11-15).
 */
export function pageHistoryChangedDatabases(
  before: readonly PageHistoryDatabase[],
  after: readonly PageHistoryDatabase[],
  rowChanges: ReadonlyMap<string, readonly PageHistoryRowMark[]>,
) {
  const previous = new Map(before.map((database) => [database.blockId, database]));
  const changed = new Set<string>();
  for (const database of after) {
    const old = previous.get(database.blockId);
    if (!old) continue;
    if (!sameStoredValue(old.properties ?? [], database.properties ?? []) || !sameStoredValue(old.views ?? [], database.views ?? [])
      || rowChanges.get(database.blockId)?.some((mark) => mark.change === "added")) {
      changed.add(database.blockId);
    }
  }
  return changed;
}

/**
 * A database page's properties whose values a version changed from the entry
 * before it, as Notion's preview outlines a row page's changed value and counts
 * it as a change ("1 / 1개", seen once by worker w1181 on 2026-11-15: a status
 * set to "할 일"). Without the values before (the page's first entry, or a page
 * that isn't a database's), none.
 */
export function pageHistoryChangedValues(before: PageHistorySnapshotResponse["values"], after: PageHistorySnapshotResponse["values"]) {
  const changed = new Set<string>();
  if (!before || !after) return changed;
  for (const propertyId of new Set([...Object.keys(before), ...Object.keys(after)])) {
    if (!automaticValue(before[propertyId], after[propertyId])
      && !sameStoredValue(before[propertyId]?.value ?? null, after[propertyId]?.value ?? null)) changed.add(propertyId);
  }
  return changed;
}

/**
 * What a version changed from the entry before it, for its preview: its
 * blocks' words, its databases' rows and renamed properties, a database
 * page's changed values, and, the version whole, the entry before's blocks
 * to keep the deleted ones from.
 */
export function pageHistoryComparison(
  preview: PageHistorySnapshotResponse | null,
  before: PageHistoryComparedVersion | null,
  textOf: (block: PageHistoryBlock) => string,
  /**
   * What made the version (pageHistoryGroupSources). A restore of an older
   * one ("restore"): the blocks it brought back are tinted whole with their
   * words as they read, as Notion's preview marks a block a restore brought
   * back (seen 2026-11-15 and twice 2026-11-16 in Chrome — a block added
   * new, by hand or through the API, keeps its words on the tint). A page
   * locked or unlocked ("page_lock"): its title is the change, outlined, as
   * Notion's preview of a lock's version (seen 2026-11-16, a lock and an
   * unlock: "1 / 1", the title outlined).
   */
  sources: ReadonlySet<string> = new Set(),
) {
  if (!preview || !before) return null;
  const restored = sources.has("restore");
  // A database the version deleted isn't among what it marks: Notion's
  // preview leaves an inline or a full-page one out, uncounted, its change
  // list naming only the page edited (seen 2026-11-07).
  const databaseBlocks = new Set(before.databases.map((database) => database.blockId));
  const titleBefore = (before.title ?? "").trim();
  const titleAfter = (preview.title ?? "").trim();
  const rowChanges = pageHistoryDatabaseChanges(before.databases, preview.databases ?? []);
  const returnedBlocks = restored ? new Set<string>() : undefined;
  return {
    changes: pageHistoryBlockChanges(before.blocks, preview.blocks, textOf, before.complete, returnedBlocks),
    returnedBlocks: returnedBlocks as ReadonlySet<string> | undefined,
    rowChanges,
    renamedProperties: pageHistoryRenamedProperties(before.databases, preview.databases ?? []),
    changedDatabases: pageHistoryChangedDatabases(before.databases, preview.databases ?? [], rowChanges),
    changedValues: pageHistoryChangedValues(before.values, preview.values),
    removedFrom: preview.hasMore ? undefined : before.blocks.filter((block) => !databaseBlocks.has(block.id)),
    // The title's words as a block's are marked: those added on the
    // author's tint, those taken out struck through (Notion's, seen
    // 2026-11-13 and 2026-11-15, a title written and one added to).
    titleParts: titleBefore !== titleAfter ? pageHistoryTextChanges(titleBefore, titleAfter)
      : sources.has("page_lock") ? [{ text: titleAfter }] : undefined,
  };
}

/** The version a preview is compared with: the entry before it, read whole or not. */
export type PageHistoryComparedVersion = {
  versionId: string;
  blocks: PageHistoryBlock[];
  databases: PageHistoryDatabase[];
  complete: boolean;
  title?: string;
  /** A database page's property values in that version. */
  values?: PageHistorySnapshotResponse["values"];
};

/**
 * What the oldest entry of a whole history is compared with: an empty page,
 * so everything it holds reads as added, as Notion's marks the entry that made
 * the page (seen 2026-10-01: its title outlined, its text and database rows
 * on the author's tint).
 */
export const pageHistoryEmptyVersion: PageHistoryComparedVersion = {
  versionId: "", blocks: [], databases: [], complete: true, title: "",
};
