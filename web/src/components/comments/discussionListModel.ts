import type { CommentActorProfile } from "@/commentWire.generated";
import type { NativeComment } from "@/lib/commentService";
import { authorLabel, commentPropertyId, isReactionThread, richTextSpans, spansToPlainTextSafe } from "./commentModel";
import { commentBlockIds } from "./commentQuotePlaces";
import { suggestionFacts, suggestionSummary } from "./suggestionModel";

/**
 * Which discussions "All discussions" shows, as Notion's filter (seen
 * 2026-09-30): comments and suggestions; open ones, unread or read, and
 * resolved ones; and the threads of the people picked, or everyone's.
 */
export type DiscussionFilter = {
  comments: boolean;
  suggestions: boolean;
  unread: boolean;
  read: boolean;
  resolved: boolean;
  people: readonly string[];
};

export const EVERY_DISCUSSION: DiscussionFilter = {
  comments: true, suggestions: true, unread: true, read: true, resolved: true, people: [],
};

/** What a phone's "모든 토론" starts with, as Notion's (seen 2026-10-27): the open ones. */
export const OPEN_DISCUSSIONS: DiscussionFilter = { ...EVERY_DISCUSSION, resolved: false };

/** Whether a filter differs from the one it started as. */
export function filterChanged(filter: DiscussionFilter, from: DiscussionFilter) {
  return (Object.keys(from) as (keyof DiscussionFilter)[])
    .some((key) => key === "people" ? filter.people.length > 0 : filter[key] !== from[key]);
}

export function discussionShown(root: NativeComment, replies: readonly NativeComment[], filter: DiscussionFilter) {
  if (suggestionFacts(root) ? !filter.suggestions : !filter.comments) return false;
  if (root.resolved ? !filter.resolved : root.unread ? !filter.unread : !filter.read) return false;
  return filter.people.length === 0 || [root, ...replies].some((comment) => filter.people.includes(comment.authorId));
}

const commentTime = (comment: NativeComment) => Date.parse(comment.createdAt ?? "") || 0;

/** Each discussion's latest reply, by its first comment. */
export function latestRepliesOf(comments: readonly NativeComment[]): ReadonlyMap<string, NativeComment> {
  const latest = new Map<string, NativeComment>();
  for (const comment of comments) {
    if (!comment.parentId) continue;
    const known = latest.get(comment.parentId);
    if (!known || commentTime(comment) >= commentTime(known)) latest.set(comment.parentId, comment);
  }
  return latest;
}

/** When a discussion was last written in: its latest reply's time, or its first comment's. */
export function discussionLastAt(root: NativeComment, latestReplies?: ReadonlyMap<string, NativeComment>) {
  return Math.max(commentTime(root), commentTime(latestReplies?.get(root.id) ?? root));
}

/**
 * The discussions a page's lists show — its bar's "토론", a phone's sheet
 * and "모든 토론" — as Notion's (seen 2026-11-09): those begun by words,
 * not the ones its text was only reacted to, newest first by their latest
 * comment, a reply too (a reaction moves none). Given no replies, by their
 * first comments.
 */
export function listedDiscussions(threads: readonly NativeComment[], latestReplies?: ReadonlyMap<string, NativeComment>) {
  return threads.filter((root) => !isReactionThread(root))
    .sort((left, right) => discussionLastAt(right, latestReplies) - discussionLastAt(left, latestReplies));
}

/**
 * A discussion under a page's title shown there whole: the page's own, its
 * first comment alone with no replies to fold, not marked unread by the
 * reader. As Notion's (seen 2026-11-25 in Chrome at 1440, two tabs at
 * different times), such a one new to the reader keeps the blue dot of its
 * row in the bar's "토론" but puts no red dot on the bar's comments button and
 * no dot on the thread under the title; one with replies folded does both, as
 * a block's does.
 */
export function discussionShownWhole(root: NativeComment, replies: readonly NativeComment[] | undefined) {
  return !root.blockId && !replies?.length && !root.markedUnread;
}

/** How many read discussions the bar's "토론" lists under its unread ones (popoverDiscussions). */
export const POPOVER_READ_ROWS = 5;

/**
 * The bar's "토론" rows out of a page's listed discussions, as Notion's (seen
 * 2026-11-25 in Chrome at 1440, two tabs at different times): every unread
 * one, newest first, then the five newest read ones — eight read gave five
 * rows; one unread and seven read, six; six unread and three read, all nine.
 * "모두 보기" shows the rest, and a phone's sheet lists them all. Given
 * `unreadFirst` false (a web link's visitor), the five newest of all.
 */
export function popoverDiscussions(listed: readonly NativeComment[], unreadFirst = true) {
  const unread = unreadFirst ? listed.filter((root) => root.unread) : [];
  return [...unread, ...listed.filter((root) => !unreadFirst || !root.unread).slice(0, POPOVER_READ_ROWS)];
}

/** The section a discussion row opens, where it opens one: "신규" or "이전". */
export type DiscussionSection = "new" | "earlier";

export const DISCUSSION_SECTION_LABELS = { new: "discussions.newSection", earlier: "discussions.earlierSection" } as const;

/**
 * A list's discussion rows in Notion's two sections, the bar's "토론" and a
 * phone's "모든 토론" alike (seen 2026-11-11 in Chrome at 1440; 2026-11-25 on
 * a Galaxy A90 in Notion's app and in Chrome, two unread of eight and one of
 * eight): once both kinds are there, the unread ones under "신규", then the
 * rest under "이전", each kind in the order given (newest first); with one
 * kind only — as a phone's filter of "읽지 않음" alone leaves — no heads, the
 * order as given. A resolved one a filter shows goes by its own reading.
 * Given `sectioned` false (a web link's visitor), the rows as they are.
 */
export function sectionedDiscussions(rows: readonly NativeComment[], sectioned = true): { root: NativeComment; section?: DiscussionSection }[] {
  const unread = rows.filter((root) => root.unread);
  if (!sectioned || unread.length === 0 || unread.length === rows.length) return rows.map((root) => ({ root }));
  const opening = (section: DiscussionSection) => (root: NativeComment, index: number) => (index === 0 ? { root, section } : { root });
  return [...unread.map(opening("new")), ...rows.filter((root) => !root.unread).map(opening("earlier"))];
}

/**
 * "모든 토론"'s discussions, as Notion's (seen 2026-11-25 in Chrome at 1440,
 * two tabs): those begun by words, oldest first by their first comment — a
 * reply moves none, and no day heads them.
 */
function panelDiscussions(threads: readonly NativeComment[]) {
  return threads.filter((root) => !isReactionThread(root)).sort((left, right) => commentTime(left) - commentTime(right));
}

/**
 * The discussions a panel shows: all of them, as "모든 토론" (above); a property's;
 * a block's own, as Notion's block card shows only that block's (seen
 * 2026-10-10) — or, for text pressed in a block, the threads whose text lies
 * there, a thread over several blocks too (seen 2026-11-09); or the page's,
 * which are neither a block's nor a property's.
 */
export function discussionsInScope(
  threads: readonly NativeComment[],
  scope: { all?: boolean; propertyId?: string; blockId?: string | null; pressed?: boolean;
    /** A simple table's rows, whose discussions are the table's too, after its own (Notion's, seen 2026-11-22). */
    rows?: readonly string[] },
) {
  if (scope.all) return panelDiscussions(threads);
  if (scope.propertyId !== undefined) return threads.filter((comment) => commentPropertyId(comment.body) === scope.propertyId);
  const { blockId } = scope;
  if (blockId && scope.pressed) return threads.filter((comment) => commentBlockIds(comment).includes(blockId));
  if (blockId) return [...threads.filter((comment) => comment.blockId === blockId),
    ...threads.filter((comment) => comment.blockId && scope.rows?.includes(comment.blockId))];
  return threads.filter((comment) => !comment.blockId && !commentPropertyId(comment.body));
}

/** The people who wrote in the discussions, first ones first. */
export function discussionPeople(comments: readonly NativeComment[]) {
  return [...new Set(comments.map((comment) => comment.authorId).filter(Boolean))];
}

/** A person a discussions filter offers: their name, `self` the reader. */
export type DiscussionPerson = { id: string; label: string; self?: boolean };

/**
 * The people a discussions filter offers — the desktop menu and a phone's
 * sheet alike — named as their comments name them, the reader's own row
 * marked as theirs after the name (SelfMark), as Notion's (seen 2026-11-25
 * in its Android app twice: "<name>(나)", the mark grey with no gap).
 */
export function discussionFilterPeople(
  ids: readonly string[], userId: string, profiles: Readonly<Record<string, CommentActorProfile>>,
): DiscussionPerson[] {
  return ids.map((id) => ({
    id, label: authorLabel({ authorId: id } as NativeComment, userId, profiles), ...(userId && id === userId ? { self: true } : {}),
  }));
}

/**
 * A discussion's latest comment, as Notion's "토론" shows it: its latest
 * reply's text, or its first comment's. Its line breaks are kept, as
 * Notion's (seen 2026-11-22): a list one line high runs them together as
 * spaces (white-space: nowrap), a closed card shows them.
 */
export function discussionLatestLine(root: NativeComment, latest?: NativeComment) {
  return latestReplyWords(latest) || discussionLine(root);
}

/** The words of a discussion's latest reply; none without one, or for one without words. */
export function latestReplyWords(latest?: NativeComment) {
  return latest ? commentWords(latest) : "";
}

/** A discussion's first comment: its text, its line breaks kept, or what it suggests. */
export function discussionLine(root: NativeComment) {
  const suggestion = suggestionFacts(root);
  if (suggestion) return suggestionSummary(suggestion);
  return commentWords(root);
}

/** A comment's words as it was written, line breaks and spaces kept, without the blank ends. */
function commentWords(comment: NativeComment) {
  return spansToPlainTextSafe(richTextSpans(comment.body)).trim();
}
