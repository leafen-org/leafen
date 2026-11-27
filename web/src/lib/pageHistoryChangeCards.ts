import type { PageHistoryChange } from "@/pageHistoryWire.generated";

// An entry's changes as Notion's "버전의 모든 변경 사항 보기" lists them (seen
// 2026-10-01): newest first, a run of edits one person made to one thing as
// its latest, which the server keeps within each page it reads.

/**
 * The changes, newest first, with each run one person made to one thing kept
 * as its latest: edits to a block until another change to it, as a move or a
 * type change. Type changes and deletions stay each.
 */
function coalescePageHistoryChanges(changes: readonly PageHistoryChange[]) {
  const kept: PageHistoryChange[] = [];
  // The runs still open, by block and then by kind and person.
  const open = new Map<string, Set<string>>();
  for (const change of changes) {
    const run = `${change.kind}\u0000${change.actorId}`;
    const merges = change.kind !== "typeChanged" && !change.deleted;
    // The thing changed: a block, or a database's property or view.
    const thing = `${change.blockId}\u0000${change.itemId ?? ""}`;
    if (merges && open.get(thing)?.has(run)) continue;
    // Another change to a block ends its other runs.
    if (thing !== "\u0000" || !merges || !open.has(thing)) open.set(thing, new Set());
    if (merges) open.get(thing)?.add(run);
    kept.push(change);
  }
  return kept;
}

/** The changes read so far with the next page of them, a run across the pages kept once. */
export function appendPageHistoryChanges(current: readonly PageHistoryChange[], next: readonly PageHistoryChange[]) {
  return coalescePageHistoryChanges([...current, ...next]);
}

/** A change's key in the list: its version, kind and block, or its database property or view. */
export function pageHistoryChangeKey(change: PageHistoryChange) {
  return `${change.versionId}\u0000${change.kind}\u0000${change.blockId}\u0000${change.itemId ?? ""}\u0000${change.deleted ? "deleted" : ""}`;
}

/**
 * Where the preview shows a change: the page's name, a block it holds, a
 * deleted block where the preview keeps it, or a row of its databases. The
 * page's other fields and its databases' other changes can't be shown in it.
 */
export function pageHistoryChangeTarget(change: PageHistoryChange, blockIds: ReadonlySet<string>) {
  if (change.kind === "renamed") return { kind: "title" } as const;
  // A database taken off the page edits it with nothing left to mark: the
  // version shows as it is, as Notion's (seen 2026-11-07).
  if (change.kind === "pageEdited" && change.deleted) return { kind: "page" } as const;
  if (change.kind === "rowAdded" || change.kind === "rowEdited" || change.kind === "rowRemoved") {
    return change.itemId ? { kind: "row", rowId: change.itemId } as const : null;
  }
  if (change.kind === "pageEdited" || change.kind === "databaseEdited" || change.kind === "propertyChanged"
    || change.kind === "viewCreated") return null;
  return change.blockId && blockIds.has(change.blockId) ? { kind: "block", blockId: change.blockId } as const : null;
}

/**
 * A stored date or date-time ("2026-10-05", "2026-10-05T09:00…") in the
 * reader's words, as Notion's change lines show it ("2026년 10월 5일").
 */
export function pageHistoryDateText(text: string, language: string) {
  const day = /^(\d{4})-(\d{2})-(\d{2})$/u.exec(text);
  const date = day ? new Date(Number(day[1]), Number(day[2]) - 1, Number(day[3])) : new Date(text);
  if (Number.isNaN(date.getTime())) return text;
  return new Intl.DateTimeFormat(language, day
    ? { year: "numeric", month: "long", day: "numeric" }
    : { year: "numeric", month: "long", day: "numeric", hour: "numeric", minute: "2-digit" }).format(date);
}
