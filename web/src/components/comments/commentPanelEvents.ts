import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react";
import type { CommentQuotePlaces } from "./commentQuotePlaces";
import { askCaretForNewComment } from "../editor/openedFieldCaret";

export const OPEN_COMMENTS_EVENT = "leafen:open-comments";

export type OpenCommentsDetail = {
  activeCommentId?: string;
  /** Opened at the comment's address (a notice, a link): its thread opens with no ring on it, as Notion's (seen 2026-11-07). */
  quiet?: boolean;
  /** Every discussion of the page, resolved ones too ("All discussions"). */
  all?: boolean;
  blockId?: string | null;
  /** Starts a new comment on the block — its handle's or the selected text's "Comment". */
  compose?: boolean;
  /** The words it quotes take no pending mark while it is written, as from Notion's phone tools (seen 2026-11-10). */
  unmarked?: boolean;
  /** The threads on the text that was pressed: only they show, and the caret stays in the text. */
  threadIds?: readonly string[];
  /** A simple table's rows, whose discussions its own mark opens with its own, as Notion's. */
  tableRowIds?: readonly string[];
  /** The discussion a comment's address (a notice, a link) opened: a wide page opens its margin card alone. */
  linkedThreadId?: string;
  /** Words of a database value pressed (`threadIds`): their margin card is marked, not opened (Notion's, seen 2026-11-12). */
  pressedOnly?: boolean;
  initialQuote?: string;
  initialQuoteEnd?: number;
  initialQuoteStart?: number;
  /** Where the quote lies beyond the block's own text: its caption, the blocks it runs on into. */
  initialQuotePlaces?: CommentQuotePlaces;
  pageId: string;
};

export function openComments(detail: OpenCommentsDetail) {
  // A new comment's box: what is typed from now until it has the caret waits for it (openedFieldCaret).
  if (detail.compose) askCaretForNewComment();
  window.dispatchEvent(new CustomEvent<OpenCommentsDetail>(OPEN_COMMENTS_EVENT, { detail }));
}

// The kinds whose block "댓글" marks none of their words (openBlockComment).
const UNMARKED_BLOCK_COMMENTS: ReadonlySet<string> = new Set(["code", "callout"]);

/**
 * A block's own "Comment" is a comment on all its text, as Notion's (seen
 * 2026-11-04: the block's text takes the mark and the discussion quotes it); a
 * block without text has none to mark.
 *
 * A code block's or a callout's is on the block itself: its words are the
 * quote, none of them marked, as Notion's (seen 2026-11-22 in Chrome at 1440,
 * thirteen kinds through the block menu's "댓글": code and callout a block
 * discussion with their text-context, paragraphs, headings, lists, to-dos,
 * toggles, toggle headings and quotes their text marked).
 */
export function openBlockComment(pageId: string, blockId: string, text: string, unmarked = false, kind?: string) {
  const words = !text.trim() ? {} : kind && UNMARKED_BLOCK_COMMENTS.has(kind) ? { initialQuote: text }
    : { initialQuote: text, initialQuoteStart: 0, initialQuoteEnd: text.length };
  openComments({ pageId, blockId, compose: true, ...(unmarked ? { unmarked } : {}), ...words });
}

// The discussions whose cards are open: their text is marked darker, as
// Notion's (seen 2026-11-04).
const NO_THREADS: readonly string[] = [];
let activeThreads = NO_THREADS;
const activeThreadListeners = new Set<() => void>();

export function setActiveCommentThreads(ids: readonly string[]) {
  if (activeThreads.length === ids.length && activeThreads.every((id, index) => id === ids[index])) return;
  activeThreads = ids.length ? ids : NO_THREADS;
  activeThreadListeners.forEach((listener) => listener());
}

export function useActiveCommentThreads() {
  return useSyncExternalStore((listener) => {
    activeThreadListeners.add(listener);
    return () => activeThreadListeners.delete(listener);
  }, () => activeThreads);
}

/**
 * The text these threads were left on. A press there isn't a press outside
 * their card: it reaches the text, so its caret lands and a double press
 * selects a word with the card still open, and it can open another card in
 * one go (Notion's, seen 2026-11-05).
 */
export function discussionMarks(threadIds: readonly string[]) {
  return threadIds.flatMap((id) => [...document.querySelectorAll(
    `:is([data-rt-editable], [data-rich-cell-text], [data-rich-cell-editor]) [data-comment-id~="${CSS.escape(id)}"]`)]);
}

// The discussions a wide page's margin shows as cards: pressing the words of
// a database value they are on marks the card instead of opening a window
// under the value (Notion's, seen 2026-11-12).
let marginThreads: ReadonlySet<string> = new Set();
export function setMarginThreads(ids: readonly string[]) { marginThreads = new Set(ids); }
export function marginShowsThread(id: string) { return marginThreads.has(id); }

/**
 * A wide page's new comments, written beside their block where the card will
 * stand: `room` is whether the page's margin has room, and the page sends a new
 * comment, or text pressed to read its discussion, there through `compose`.
 */
export function useMarginCompose() {
  const [compose, setCompose] = useState<OpenCommentsDetail | null>(null);
  const closeCompose = useCallback(() => setCompose(null), []);
  const room = useRef(false);
  const setRoom = useCallback((value: boolean) => { room.current = value; }, []);
  /**
   * Takes a new comment or pressed text on a block where the margin has room,
   * which `allowed` may rule out, and a block's discussion opened at its
   * address: its card opens beside its block, and nothing over the block shows
   * it again (Notion's, seen 2026-11-11).
   */
  const take = useCallback((detail: OpenCommentsDetail, allowed: boolean) => {
    const taken = room.current && Boolean(detail.blockId || detail.pressedOnly) && (Boolean(detail.linkedThreadId)
      || (allowed && (detail.compose === true || Boolean(detail.threadIds?.length))));
    setCompose(taken ? detail : null);
    return taken;
  }, []);
  return { compose, closeCompose, setRoom, take };
}

// A line of the inbox standing for comments on several discussions opens its
// page with the top bar's "토론" list open, as Notion's (seen 2026-11-11).
let askedDiscussions: string | null = null;
const askedDiscussionsListeners = new Set<() => void>();

export function askPageDiscussions(pageId: string) {
  askedDiscussions = pageId;
  askedDiscussionsListeners.forEach((listener) => listener());
}

/** Opens a page's "토론" list when it was asked for, once its button stands (`ready`). */
export function usePageDiscussionsAsked(pageId: string | undefined, ready: boolean, open: () => void) {
  const asked = useSyncExternalStore((listener) => {
    askedDiscussionsListeners.add(listener);
    return () => askedDiscussionsListeners.delete(listener);
  }, () => askedDiscussions, () => null);
  const latest = useRef(open); latest.current = open;
  useEffect(() => {
    if (!pageId || !ready || asked !== pageId) return;
    askedDiscussions = null;
    latest.current();
  }, [asked, pageId, ready]);
}
