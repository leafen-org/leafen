import type {
  PageHistoryActorProfile,
  PageHistoryChange,
  PageHistorySnapshotResponse,
  PageHistoryVersion,
  PageLockRecord,
} from "@/pageHistoryWire.generated";
import type { TextSpan } from "./types";
import { pageHistoryThrough } from "./pageHistoryGroups";
import { columnsOnly, pageHistoryPreviewRowsWithRemoved } from "./pageHistoryRows";
import { richTextParts, type RichTextParts } from "./richTextSummary";
import type { PageUpdateProperty } from "./pageUpdateRecord";
import { updateLineBare, updateLineWordless } from "./updateLineKinds";

// A page's updates, as Notion's updates tab lists them (seen 2026-09-30): a
// burst of edits, by one person or several, is one update, which shows what
// the burst did against the page as it was before it: the title it gave, new
// lines whole, new words in a changed line, and each line, box or sub-page
// it deleted struck under "삭제됨" (seen 2026-11-13, as the workspace's
// updates show them). Text only taken out of a line is not shown. Its first
// five things show, the title counted as one, and the rest fold under
// "N개 항목 더 보기" (updates/UpdateChangesMore).
// Edits made through an app's screen or an agent are a burst of their own,
// named with it as the page's history names them, never one with edits made
// by hand.

/** Edits this close together are one update (Notion's split edits six minutes apart). */
export const PAGE_UPDATE_GAP_MS = 5 * 60_000;

export type PageUpdate = {
  /** The update's last version, whose state it shows. */
  version: PageHistoryVersion;
  /** Its first version, from which its changes are read in the order they were made. */
  first: PageHistoryVersion;
  /** The version the update is compared with; none when the page's history begins inside it. */
  before: PageHistoryVersion | null;
  /** Who made the update, the latest first. */
  actorIds: string[];
  /**
   * The integrations that made it, named alone as Notion's updates name an
   * integration's edit (seen 2026-11-06); their people are not among actorIds.
   */
  integrations?: string[];
  /** The app or agent its people made it through, as the page's history names it beside them. */
  through?: string;
  /**
   * A database page's update of its title and properties ("record") or of
   * its body, apart, as Notion lists them (seen 2026-11-13: a row's values
   * and title one update, a line written between them another).
   */
  part?: PageUpdatePart;
  /**
   * Where, inside it, the page became a database's page (`row_made`): the
   * values it got then are its making, and its record's changes are read from
   * them (pageUpdateRecordDiff).
   */
  made?: PageHistoryVersion;
};

export type PageUpdatePart = "record" | "body";

/** What tells an update apart in the list: a database page's making can be both its record's and its body's. */
export const pageUpdateKey = (update: Pick<PageUpdate, "version" | "part">) =>
  update.part === "record" ? `record:${update.version.id}` : update.version.id;

/** A database page's version of its title and values (database_row_page_history.go), not its body. */
const recordVersion = (version: PageHistoryVersion) => version.source === "metadata";
/** The version where a page became a database's page, its values its making: listed in no update (pageHistoryVersionRowMade). */
const rowMade = (version: PageHistoryVersion) => version.source === "row_made";

/**
 * How much of a page's history the versions read hold: all ("whole"), the
 * newest with more left ("more"), or as many as its updates read at most
 * ("cut", pageUpdatesReading.ts), the oldest compared against as a baseline is.
 */
export type PageVersionsRead = "whole" | "more" | "cut";

export type PageUpdatesGrouped = {
  updates: PageUpdate[];
  /**
   * While an update may be unread: the newest time it can be at. What is
   * older (a lock, a discussion, a trash move) waits, so nothing is listed
   * under an update missing above it.
   */
  waitingFrom: number | null;
};

/**
 * Groups a page's versions, newest first, into updates. A page's making (its
 * first version) is one of them, compared with an empty page, as Notion lists
 * a page made with a title as edited (seen 2026-11-06); a later baseline,
 * where a page's history starts again, is compared against, not listed. An
 * update whose version before it is not read yet is left for the next page
 * of versions. A database page's record and body (`parts`) group apart, its
 * making beginning both.
 */
export function groupPageUpdates(
  versions: readonly PageHistoryVersion[],
  read: PageVersionsRead,
  gapMs = PAGE_UPDATE_GAP_MS,
  /** The actors who are an app's own account (pageHistoryThrough). */
  apps: ReadonlySet<string> = new Set(),
  parts = false,
): PageUpdatesGrouped {
  // A version that only locked a page or an inline database is its lock, listed from its lock records, not an edit.
  const ordered = versions.filter((version) => version.source !== "lock" && version.source !== "page_lock")
    .sort((left, right) => right.versionCursor - left.versionCursor);
  // The page's making keeps the first version; a baseline after it restarts the history.
  const listed = (version: PageHistoryVersion) => version.source !== "baseline" || version.versionCursor === 0;
  let end = 0;
  while (end < ordered.length && listed(ordered[end])) end += 1;
  // Cut, the oldest version read is compared against, unless it is the page's making.
  if (read === "cut" && end === ordered.length && end > 0 && ordered[end - 1].versionCursor !== 0) end -= 1;
  const complete = read !== "more";
  // Whatever is older than the oldest version read is not read.
  let waitingFrom = read === "whole" || versions.length === 0 ? null : Math.min(...versions.map(({ createdAt }) => createdAt));
  if (read === "cut" && ordered[end]) waitingFrom = Math.max(waitingFrom ?? 0, ordered[end].createdAt);
  const updates: PageUpdate[] = [];
  for (const part of parts ? (["record", "body"] as const) : [undefined]) {
    const indexes = [...ordered.keys()].slice(0, end).filter((index) => !rowMade(ordered[index]) && (!part
      || ordered[index].versionCursor === 0 || (part === "record") === recordVersion(ordered[index])));
    let at = 0;
    while (at < indexes.length) {
      const group = [ordered[indexes[at]]];
      const newest = indexes[at];
      let last = indexes[at];
      at += 1;
      const through = pageHistoryThrough(group[0], apps);
      while (
        at < indexes.length
        && group[group.length - 1].createdAt - ordered[indexes[at]].createdAt <= gapMs
        && pageHistoryThrough(ordered[indexes[at]], apps) === through
      ) {
        group.push(ordered[indexes[at]]);
        last = indexes[at];
        at += 1;
      }
      // Compared with the page as it was just before, whichever part changed it then.
      const before = ordered[last + 1] ?? null;
      if (!before && !complete) {
        waitingFrom = Math.max(waitingFrom ?? 0, group[0].createdAt);
        break;
      }
      const made = part === "body" ? undefined : ordered.slice(newest, last + 1).find(rowMade);
      const integration = (version: PageHistoryVersion) => version.connectionKind === "integration" && Boolean(version.connectionName);
      const integrations = [...new Set(group.filter(integration).map(({ connectionName }) => connectionName ?? ""))];
      updates.push({
        version: group[0],
        first: group[group.length - 1],
        before,
        actorIds: [...new Set(group.filter((version) => !integration(version)).map(({ actorId }) => actorId).filter(Boolean))],
        ...(integrations.length ? { integrations } : {}),
        ...(through ? { through } : {}),
        ...(part ? { part } : {}),
        ...(made ? { made } : {}),
      });
    }
  }
  return { updates: updates.sort((left, right) => right.version.versionCursor - left.version.versionCursor), waitingFrom };
}

/**
 * A lock or unlock for everyone, an update of its own beside the edits, as
 * Notion lists them (seen 2026-11-10): never grouped, with no changes or
 * version to open. Who made it is named as an edit's maker is.
 */
export type PageLockUpdate = Pick<PageUpdate, "actorIds" | "integrations" | "through"> & { record: PageLockRecord };

export type PageUpdateEntry = { edit: PageUpdate; at: number } | { lock: PageLockUpdate; at: number };

function pageLockUpdate(record: PageLockRecord, apps: ReadonlySet<string>): PageLockUpdate {
  const integration = record.connectionKind === "integration" && Boolean(record.connectionName);
  // An agent's or app screen's name goes beside its person (pageHistoryThrough).
  const through = !integration && !apps.has(record.actorId) ? record.connectionName ?? "" : "";
  return {
    record,
    actorIds: integration || !record.actorId ? [] : [record.actorId],
    ...(integration ? { integrations: [record.connectionName ?? ""] } : {}),
    ...(through ? { through } : {}),
  };
}

/**
 * A page's edits and its locks in one list, the newest first, `limit` at
 * most. While an update may be unread (PageUpdatesGrouped), an older lock
 * waits for it, so no edit is missed between.
 */
export function pageUpdateEntries(
  updates: readonly PageUpdate[],
  locks: readonly PageLockRecord[],
  waitingFrom: number | null,
  limit: number,
  apps: ReadonlySet<string> = new Set(),
): PageUpdateEntry[] {
  const entries: PageUpdateEntry[] = [
    ...updates.map((edit) => ({ edit, at: edit.version.createdAt })),
    ...locks.filter((record) => waitingFrom === null || record.createdAt >= waitingFrom)
      .map((record) => ({ lock: pageLockUpdate(record, apps), at: record.createdAt })),
  ];
  return entries.sort((left, right) => right.at - left.at).slice(0, limit);
}

/** Whether an update changed anything shown: a page made empty lists no update (Notion, seen 2026-11-06). */
export function pageUpdateChanged(diff: PageUpdateDiff | undefined): boolean {
  return Boolean(diff && (diff.title || diff.lines.length || diff.properties?.length));
}

export type PageUpdateLine = {
  /** The changed block the update links back to. */
  blockId: string;
  /**
   * A new line is added whole; a changed one keeps its text around what was
   * added; a deleted one's words, as they were, are its added part, struck.
   */
  change: "added" | "changed" | "deleted";
  before: string;
  added: string;
  after: string;
  depth: number;
  /**
   * The same three parts as the line's spans, so its equations, mentions and
   * marks draw as in the page, as Notion's updates show them (seen
   * 2026-10-04); none when the line's text has no spans to match.
   */
  spans?: { before: TextSpan[]; added: TextSpan[]; after: TextSpan[] };
  /**
   * Its block's kind, which its line is drawn by as Notion's (seen
   * 2026-11-13 and 2026-11-17, updates/UpdateLine): none for a line of text;
   * an equation's words are its expression; a code block's language; a
   * to-do's whether it is done; a changed equation's expression before the
   * change; a deleted button's its workflow's steps.
   */
  blockType?: string;
  language?: string;
  checked?: boolean;
  was?: string;
  steps?: number;
  /** A sub-page made or deleted, its words its name, drawn with a page's mark. */
  page?: true;
};

export type PageUpdateDiff = {
  /** The title the update gave the page. */
  title?: string;
  /** That title split around what the update added to it, as Notion marks the added words (seen 2026-11-06). */
  titleParts?: { before: string; added: string; after: string };
  /** How many of the lines were made before the title, which goes after them. */
  titleAfter?: number;
  /** The changed lines in the order they were made, all of them. */
  lines: PageUpdateLine[];
  /** A database page's changed properties, in its order, before its title (pageUpdateRecord.ts). */
  properties?: PageUpdateProperty[];
  /** The title was given while it was a page, before a database took it in: a page's title, named so. */
  pageTitle?: true;
};

/** One thing an update changed, as it lists them: a line, a property's value, or the title it gave the page. */
export type PageUpdateItem = { line: PageUpdateLine } | { property: PageUpdateProperty } | { title: string };

/**
 * An update's changed lines and its title in the order they were made, each
 * one item to fold (updates/UpdateChangesMore); a database page's record its
 * properties and then its title, as Notion's (seen 2026-11-13).
 */
export function pageUpdateItems(diff: PageUpdateDiff): PageUpdateItem[] {
  const lines: PageUpdateItem[] = [...diff.lines.map((line) => ({ line })), ...(diff.properties ?? []).map((property) => ({ property }))];
  if (!diff.title) return lines;
  const at = diff.properties ? lines.length : diff.titleAfter ?? 0;
  return [...lines.slice(0, at), { title: diff.title }, ...lines.slice(at)];
}

type PageUpdateState = Pick<PageHistorySnapshotResponse, "title" | "blocks">;

/** The part of a line's new text between what it kept at its start and its end. */
export function addedText(previous: string, next: string) {
  let start = 0;
  while (start < previous.length && start < next.length && previous[start] === next[start]) start += 1;
  let end = 0;
  while (
    end < previous.length - start && end < next.length - start
    && previous[previous.length - 1 - end] === next[next.length - 1 - end]
  ) end += 1;
  return { before: next.slice(0, start), added: next.slice(start, next.length - end), after: next.slice(next.length - end) };
}

/** A block's words in its update's line: an equation's its expression. */
function blockWords(block: PageUpdateState["blocks"][number]) {
  const expression = block.content.expression;
  return (block.type === "equation" ? typeof expression === "string" ? expression : "" : block.plainText).trim();
}

/** What a line says of its block's kind besides its words (updates/UpdateLine); none for a line of text. */
function markedBlock(block: PageUpdateState["blocks"][number]): Pick<PageUpdateLine, "blockType" | "language" | "checked"> {
  if (block.type === "paragraph") return {};
  const { language, checked } = block.content;
  return {
    blockType: block.type,
    ...(block.type === "code" && typeof language === "string" ? { language } : {}),
    ...(block.type === "to_do" && checked === true ? { checked } : {}),
  };
}

/** A code block's and an equation's words are plain in their box; any other line's keep their marks. */
const plainBlock = (block: PageUpdateState["blocks"][number]) => block.type === "code" || block.type === "equation";

/** A deleted button's line: its name, boxed, and its workflow's steps (Notion's, seen 2026-11-17, two pages). */
function deletedButton(block: PageUpdateState["blocks"][number]) {
  const label = typeof block.content.label === "string" ? block.content.label.trim() : "";
  const document = block.content.buttonActionDocument as { actions?: unknown } | undefined;
  return { label, steps: Array.isArray(document?.actions) ? document.actions.length : 0 };
}

/** A sub-page's block, whose line is its name with a page's mark. */
const pageBlock = (block: PageUpdateState["blocks"][number]) => block.type === "child_page" || block.type === "link_to_page";

/** A line's spans cut at its parts, where its text has spans to match. */
function withSpans(block: PageUpdateState["blocks"][number], parts: RichTextParts<string>) {
  const rich = Array.isArray(block.content.rich) ? block.content.rich as TextSpan[] : [];
  const spans = richTextParts(rich, block.plainText, parts);
  return spans ? { spans } : {};
}

/**
 * When an update's title and each of its lines were first changed in it
 * (page_history_changes.go), as a key to order them by: the change's time,
 * and in a version of typing, which takes in what is typed for a minute
 * (page_history_storage.go), the order its lines were first typed in. A new
 * line is placed by when it was made (pageUpdateDiff): its words can be kept
 * a version before it and in no order, as a paste's. What its first version
 * made — a page made with its title and text at once — has no key and goes
 * first, the title before the lines.
 */
export type PageUpdateMadeAt = {
  title?: number;
  blocks: ReadonlyMap<string, number>;
  /** The version that first changed each of blocks, and each version's moved blocks, for a block made around lines (pageUpdateDiff). */
  madeIn?: ReadonlyMap<string, string>;
  moved?: ReadonlyMap<string, ReadonlySet<string>>;
};

/** A key's room for the lines typed in one version: its time's millisecond times this. */
const TYPED_PLACES = 1000;

/**
 * When each thing was first changed in an update, from its changes newest
 * first; `typed` says which versions are typing (source "collaboration").
 */
export function pageUpdateMadeAt(changes: readonly PageHistoryChange[], typed: (versionId: string) => boolean = () => false): PageUpdateMadeAt {
  let title: number | undefined;
  const blocks = new Map<string, number>(), madeIn = new Map<string, string>(), moved = new Map<string, Set<string>>();
  let place = 0;
  let version = "";
  for (const change of [...changes].reverse()) {
    place = change.versionId === version && typed(change.versionId) ? Math.min(place + 1, TYPED_PLACES - 1) : 0;
    version = change.versionId;
    const key = change.createdAt * TYPED_PLACES + place;
    if (change.kind === "renamed") title ??= key;
    else if (change.kind === "moved" && change.blockId) {
      moved.set(version, (moved.get(version) ?? new Set()).add(change.blockId));
    } else if ((change.kind === "edited" || change.kind === "typeChanged") && change.blockId && !blocks.has(change.blockId)) {
      blocks.set(change.blockId, key);
      madeIn.set(change.blockId, version);
    }
  }
  return { ...(title === undefined ? {} : { title }), blocks, madeIn, moved };
}

/**
 * What an update added: the title it gave the page and its new and changed
 * lines, all in the order they were made, as Notion lists them (seen
 * 2026-11-13: a line, then the title, then another line, and a line written
 * last last even above one written before it; a new line typed before words
 * added to a line above it, first) — in the page's order, title first, where
 * the order is not known.
 */
export function pageUpdateDiff(
  before: PageUpdateState | null,
  after: PageUpdateState,
  madeAt?: PageUpdateMadeAt,
  /** A sub-page's name, from its block; none where the reader can't see it, whose line is left out. */
  pageName: (block: PageUpdateState["blocks"][number]) => string | undefined = () => undefined,
): PageUpdateDiff {
  const previous = new Map((before?.blocks ?? []).map((block) => [block.id, block]));
  const lines: PageUpdateLine[] = [];
  // The blocks it deleted are kept where they stood (pageHistoryRows.ts).
  // A synced block is a line of its own, an empty band once made (lib/updateLineKinds.ts).
  for (const { block, depth, removed } of pageHistoryPreviewRowsWithRemoved(before?.blocks ?? [], after.blocks, columnsOnly)) {
    const earlier = previous.get(block.id);
    if (pageBlock(block)) {
      // A sub-page made or deleted shows by its name; one renamed or moved is its own update.
      const name = earlier && !removed ? undefined : pageName(block);
      if (name !== undefined) {
        lines.push({ blockId: block.id, change: removed ? "deleted" : "added", before: "", added: name, after: "", depth, page: true });
      }
      continue;
    }
    const text = blockWords(block);
    const marked = markedBlock(block);
    const spansOf = (parts: RichTextParts<string>) => plainBlock(block) ? {} : withSpans(block, parts);
    if (removed && block.type === "button") {
      const { label, steps } = deletedButton(block);
      lines.push({ blockId: block.id, change: "deleted", before: "", added: label, after: "", depth, ...marked, steps });
      continue;
    }
    if (removed) {
      // A deleted line shows its words as they were, an empty one nothing.
      if (!text) continue;
      const parts = { before: "", added: text, after: "" };
      lines.push({ blockId: block.id, change: "deleted", ...parts, depth, ...marked, ...spansOf(parts) });
      continue;
    }
    if (!earlier) {
      // A made line with no words shows nothing, but a bare one (lib/updateLineKinds.ts).
      if (!text && !updateLineBare(block.type)) continue;
      const parts = { before: "", added: updateLineWordless(block.type) ? "" : text, after: "" };
      lines.push({ blockId: block.id, change: "added", ...parts, depth, ...marked, ...(parts.added ? spansOf(parts) : {}) });
      continue;
    }
    const was = blockWords(earlier);
    const parts = addedText(was, text);
    // An equation struck where it lost words shows any change of it.
    if (block.type === "equation" ? text && text !== was : parts.added.trim()) {
      lines.push({ blockId: block.id, change: "changed", ...parts, depth, ...marked,
        ...(block.type === "equation" ? { was } : spansOf(parts)) });
    }
  }
  // A new line where it was made, which a paste does in the page's order.
  const madeLine = new Map(after.blocks.filter((block) => !previous.has(block.id) && block.createdAt > 0)
    .map((block) => [block.id, block.createdAt * TYPED_PLACES]));
  const first = (blockId: string) => Math.min(madeAt?.blocks.get(blockId) ?? -Infinity, madeLine.get(blockId) ?? Infinity);
  // A block made around lines made before it — a line turned into a callout
  // or a synced block, made as a new block holding the line it was — goes
  // right before the first of them, as Notion's line turned into one is that
  // line (workspace_activity_change_order.go).
  const children = new Map<string, string[]>(), keys = new Map<string, number>();
  for (const block of after.blocks) {
    const held = children.get(block.parentId);
    if (held) held.push(block.id);
    else children.set(block.parentId, [block.id]);
  }
  const at = (blockId: string): number => {
    const known = keys.get(blockId);
    if (known !== undefined) return known;
    const moved = madeAt?.moved?.get(madeAt?.madeIn?.get(blockId) ?? "");
    let key = first(blockId);
    for (const child of moved ? children.get(blockId) ?? [] : []) {
      if (moved?.has(child) && Number.isFinite(first(child))) key = Math.min(key, at(child) - 1 / 1024);
    }
    keys.set(blockId, key);
    return key;
  };
  // A stable sort keeps the page's order among lines made at once.
  const ordered = madeAt ? [...lines].sort((left, right) => at(left.blockId) - at(right.blockId)) : lines;
  const titled = before?.title !== after.title && after.title.trim();
  const titleAt = madeAt?.title ?? -Infinity;
  const titleAfter = titled ? ordered.filter((line) => at(line.blockId) < titleAt).length : 0;
  return {
    ...(titled ? { title: after.title.trim(), titleParts: addedText((before?.title ?? "").trim(), after.title.trim()) } : {}),
    ...(titleAfter ? { titleAfter } : {}),
    lines: ordered,
  };
}

/** Who made an update, the reader first as Notion names them, then the others by name. */
export function pageUpdateActors(
  actorIds: readonly string[],
  profiles: ReadonlyMap<string, PageHistoryActorProfile>,
  readerId: string,
  unknown: string,
) {
  const ordered = [...actorIds.filter((id) => id === readerId), ...actorIds.filter((id) => id !== readerId)];
  const others = ordered.filter((id) => id !== readerId);
  // An app's own account is no person: named alone, as a connection is
  // (the page's history marks it "(앱)").
  const isApp = (id: string) => profiles.get(id)?.app === true;
  return {
    reader: actorIds.includes(readerId),
    others: others.filter((id) => !isApp(id)).map((id) => profiles.get(id)?.displayName.trim() || unknown),
    apps: others.filter(isApp).map((id) => profiles.get(id)?.displayName.trim() || unknown),
  };
}

/** A Korean name with the "and" that follows it: 와 after a vowel, 과 after a final consonant. */
export function koreanWithAnd(name: string) {
  const last = name.trim().at(-1) ?? "";
  const code = last.charCodeAt(0) - 0xac00;
  if (code < 0 || code > 11171) return `${name}와`;
  return `${name}${code % 28 === 0 ? "와" : "과"}`;
}

/**
 * A page at a version, every block of it, read a window at a time as the
 * page itself is opened: an update compares all of it.
 */
export async function readWholeSnapshot<T extends { title: string; hasMore: boolean; nextCursor?: string | null }, B>(
  readWindow: (cursor?: string) => Promise<T & { blocks: readonly B[] }>,
): Promise<{ title: string; blocks: B[] }> {
  let snapshot = await readWindow();
  const blocks = [...snapshot.blocks];
  while (snapshot.hasMore && snapshot.nextCursor) {
    const cursor: string = snapshot.nextCursor;
    snapshot = await readWindow(cursor);
    blocks.push(...snapshot.blocks);
    if (snapshot.nextCursor === cursor) break;
  }
  return { title: snapshot.title, blocks };
}
