import { createContext, type ReactNode, useContext, useEffect, useMemo, useRef, useSyncExternalStore } from "react";
import { api } from "@/api";
import { isReactionThread } from "@/components/comments/commentModel";
import { commentBlockIds } from "@/components/comments/commentQuotePlaces";
import { suggestionFacts } from "@/components/comments/suggestionModel";
import { commentServiceFor, databaseRowCommentServiceFor } from "@/lib/commentService";
import type { Session } from "@/types";
import { CommentAnchorMarks, type OpenCommentThread } from "./CommentAnchorMarks";
import { SettlingSuggestionMarks } from "./SettlingSuggestionMarks";
import { TextReactionProvider } from "./TextReactions";

/** An open thread begun by a reaction to a block's text: its first emoji and how many reacted. */
export type BlockReactionThread = { id: string; emoji: string; count: number };

/** What a block's open discussions are: all suggestions, all comments, or some of each. */
export type BlockDiscussionKind = "suggestion" | "comment" | "both";

const NO_COUNTS: ReadonlyMap<string, number> = new Map();
const NO_REACTION_THREADS: ReadonlyMap<string, readonly BlockReactionThread[]> = new Map();
const NO_THREADS: readonly BlockReactionThread[] = [];
const NO_ROWS: readonly string[] = [];
const NO_KINDS: ReadonlyMap<string, BlockDiscussionKind> = new Map();
const BlockCommentCounts = createContext<ReadonlyMap<string, number>>(NO_COUNTS);
const BlockDiscussionKinds = createContext(NO_KINDS);
const BlockReactionThreads = createContext(NO_REACTION_THREADS);
const OpenCommentThreads = createContext<ReadonlySet<string>>(new Set());
const NO_BLOCKS: ReadonlySet<string> = new Set();
const UnreadCommentBlocks = createContext<ReadonlySet<string>>(NO_BLOCKS);

type BlockDiscussions = {
  counts: ReadonlyMap<string, number>;
  kinds: ReadonlyMap<string, BlockDiscussionKind>;
  reactionThreads: ReadonlyMap<string, readonly BlockReactionThread[]>;
  openThreads: readonly OpenCommentThread[];
  discussed: ReadonlySet<string>;
  unreadBlocks: ReadonlySet<string>;
};

/** `next`, or `kept` where it holds the same. */
function keptMap<V>(kept: ReadonlyMap<string, V> | undefined, next: ReadonlyMap<string, V>, same: (left: V, right: V) => boolean = Object.is) {
  return kept && kept.size === next.size && [...next].every(([key, value]) => kept.has(key) && same(kept.get(key) as V, value)) ? kept : next;
}

function keptSet(kept: ReadonlySet<string> | undefined, next: ReadonlySet<string>) {
  return kept && kept.size === next.size && [...next].every((key) => kept.has(key)) ? kept : next;
}

function withKind(kind: BlockDiscussionKind | undefined, next: BlockDiscussionKind | undefined): BlockDiscussionKind | undefined {
  return !kind ? next : !next || next === kind ? kind : "both";
}

function sameReactionThreads(left: readonly BlockReactionThread[], right: readonly BlockReactionThread[]) {
  return left.length === right.length && left.every((thread, index) =>
    thread.id === right[index].id && thread.emoji === right[index].emoji && thread.count === right[index].count);
}

/**
 * Counts each block's unresolved comment threads from the page's comments,
 * which the page's comment button reads too, so a block with open threads
 * shows how many and of what kind and its threads begun by a reaction show their emoji, and
 * marks the text the open threads were left on. A database page's are its
 * row's comments.
 */
export function BlockCommentCountsProvider({ children, pageId, workspaceId, row = false, session }: {
  children: ReactNode;
  pageId: string;
  workspaceId?: string;
  row?: boolean;
  session: Session | null | undefined;
}) {
  const service = useMemo(
    () => session ? (row ? databaseRowCommentServiceFor : commentServiceFor)(session, api) : null,
    [row, session],
  );
  const subscribe = useMemo(
    () => (listener: () => void) => service ? service.subscribe(pageId, listener) : () => {},
    [pageId, service],
  );
  const getSnapshot = useMemo(() => () => service ? service.getSnapshot(pageId) : null, [pageId, service]);
  const snapshot = useSyncExternalStore(subscribe, getSnapshot, getSnapshot);

  useEffect(() => {
    if (service) void service.load(pageId).catch(() => {});
  }, [pageId, service]);

  // What the blocks read stays the same while it says the same, so a change
  // that leaves the blocks' marks as they were — a suggestion settled, a
  // reaction, a read of the page's comments — draws no block again
  // (2026-11-25: each drew every block of a page of 170).
  const kept = useRef<BlockDiscussions>(undefined);
  const { counts, kinds, reactionThreads, openThreads, discussed, unreadBlocks } = useMemo((): BlockDiscussions => {
    const next = new Map<string, number>();
    const nextKinds = new Map<string, BlockDiscussionKind>();
    // A block with an open discussion unread to the reader marks it with a red dot, as Notion's (seen 2026-11-06).
    const unread = new Set<string>();
    const reactions = new Map<string, BlockReactionThread[]>();
    const open: OpenCommentThread[] = [];
    const discussedThreads = new Set<string>();
    // A block's mark counts the comments of its open discussions, replies too,
    // as Notion's (seen 2026-11-06: one comment and its reply show 2).
    const repliesTo = new Map<string, number>();
    for (const comment of snapshot?.comments ?? []) {
      if (comment.parentId) repliesTo.set(comment.parentId, (repliesTo.get(comment.parentId) ?? 0) + 1);
    }
    for (const comment of snapshot?.comments ?? []) {
      // One that lost what it was left on marks no block, as Notion's (seen 2026-11-27; comment_original_gone.go).
      if (!comment.blockId || comment.parentId || comment.resolved || comment.originalGone) continue;
      open.push({ id: comment.id, blockIds: commentBlockIds(comment) });
      if (!isReactionThread(comment)) {
        discussedThreads.add(comment.id);
        if (comment.unread) unread.add(comment.blockId);
        next.set(comment.blockId, (next.get(comment.blockId) ?? 0) + 1 + (repliesTo.get(comment.id) ?? 0));
        const kind = suggestionFacts(comment) ? "suggestion" : "comment";
        nextKinds.set(comment.blockId, withKind(nextKinds.get(comment.blockId), kind) ?? kind);
      } else if (comment.reactions.length > 0) {
        // Pressing text reacted to opens its card too, as Notion's (seen 2026-11-09).
        discussedThreads.add(comment.id);
        const count = comment.reactions.reduce((total, reaction) => total + reaction.userIds.length, 0);
        reactions.set(comment.blockId, [...(reactions.get(comment.blockId) ?? []),
          { id: comment.id, emoji: comment.reactions[0].emoji, count }]);
      }
    }
    const before = kept.current;
    return kept.current = {
      counts: keptMap(before?.counts, next.size > 0 ? next : NO_COUNTS),
      kinds: keptMap(before?.kinds, nextKinds.size > 0 ? nextKinds : NO_KINDS),
      reactionThreads: keptMap(before?.reactionThreads, reactions.size > 0 ? reactions : NO_REACTION_THREADS, sameReactionThreads),
      openThreads: open,
      discussed: keptSet(before?.discussed, discussedThreads),
      unreadBlocks: keptSet(before?.unreadBlocks, unread.size > 0 ? unread as ReadonlySet<string> : NO_BLOCKS),
    };
  }, [snapshot]);
  return (
    <BlockCommentCounts.Provider value={counts}>
      <BlockDiscussionKinds.Provider value={kinds}>
        <BlockReactionThreads.Provider value={reactionThreads}>
          <OpenCommentThreads.Provider value={discussed}>
            <UnreadCommentBlocks.Provider value={unreadBlocks}>
              <TextReactionProvider pageId={pageId} workspaceId={workspaceId} service={service}>{children}</TextReactionProvider>
            </UnreadCommentBlocks.Provider>
          </OpenCommentThreads.Provider>
          <CommentAnchorMarks pageId={pageId} openThreads={openThreads} />
          <SettlingSuggestionMarks pageId={pageId} />
        </BlockReactionThreads.Provider>
      </BlockDiscussionKinds.Provider>
    </BlockCommentCounts.Provider>
  );
}

/** The page's open discussions, which pressing their text opens. */
export function useOpenCommentThreads() {
  return useContext(OpenCommentThreads);
}

/**
 * A block's open discussions' comments, with those of `rows` — a simple
 * table's, on their cells' words — as Notion's table mark counts them (seen
 * 2026-11-22, inline comments minimal: "💬 2" for the table's own and a row's).
 */
export function useBlockCommentCount(blockId: string, rows: readonly string[] = NO_ROWS) {
  const counts = useContext(BlockCommentCounts);
  return rows.reduce((total, row) => total + (counts.get(row) ?? 0), counts.get(blockId) ?? 0);
}

/**
 * Whether the block's open discussions, with those of `rows`, are all
 * suggestions, all comments or some of each, which its mark shows by its
 * icon, as Notion's (seen 2026-11-25 at 1000 and 1440, dpr 2, two tabs: a
 * pencil on the bubble for suggestions alone, their replies counted too; the
 * bubble for comments alone; two bubbles for both). A comment being written
 * there leaves it as it was (seen once in Notion: a suggestion's mark stays).
 */
export function useBlockDiscussionKind(blockId: string, rows: readonly string[] = NO_ROWS) {
  const kinds = useContext(BlockDiscussionKinds);
  return rows.reduce((kind, row) => withKind(kind, kinds.get(row)), kinds.get(blockId));
}

/** Whether the block, or one of `rows`, holds an open discussion unread to the reader. */
export function useBlockCommentUnread(blockId: string, rows: readonly string[] = NO_ROWS) {
  const unread = useContext(UnreadCommentBlocks);
  return unread.has(blockId) || rows.some((row) => unread.has(row));
}

export function useBlockReactionThreads(blockId: string) {
  return useContext(BlockReactionThreads).get(blockId) ?? NO_THREADS;
}
