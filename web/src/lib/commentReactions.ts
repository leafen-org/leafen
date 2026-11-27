import type { CommentApi } from "@/commentApi";
import type { CommentReaction, CommentRecord } from "@/commentWire.generated";
import type { Session } from "@/types";
import type { NativeComment } from "./commentService";

/**
 * What the comment service lends its reaction writes: the page's comments and
 * reactions as shown, and the way to show a change (commentService.ts).
 */
export type CommentReactionStore = {
  readonly session: Session;
  readonly transport: CommentApi;
  /** Stops a read on its way, whose older answer would undo the change shown. */
  supersedeRead(pageId: string): void;
  comments(pageId: string): readonly NativeComment[];
  pageReactions(pageId: string): readonly CommentReaction[];
  /** Shows a comment in place of the one with its id. */
  replace(pageId: string, comment: NativeComment): NativeComment;
  /** Shows a comment as the server answered it. */
  land(pageId: string, record: CommentRecord): NativeComment;
  showPageReactions(pageId: string, reactions: readonly CommentReaction[]): void;
};

/** The reactions after one person adds or takes back an emoji. */
export function reactionsAfter(
  reactions: readonly CommentReaction[],
  emoji: string,
  userId: string,
  reacted: boolean,
): CommentReaction[] {
  const next = reactions
    .map((reaction) => {
      if (reaction.emoji !== emoji || reaction.userIds.includes(userId) === reacted) return reaction;
      return {
        emoji,
        userIds: reacted ? [...reaction.userIds, userId] : reaction.userIds.filter((id) => id !== userId),
      };
    })
    .filter((reaction) => reaction.userIds.length > 0);
  if (reacted && !next.some((reaction) => reaction.emoji === emoji)) next.push({ emoji, userIds: [userId] });
  return next;
}

/**
 * Adds or takes back the person's emoji on a comment. The comment shows
 * the change at once and the server's answer when it comes; a refusal
 * puts back what it showed before.
 */
export async function reactToComment(
  store: CommentReactionStore, pageId: string, comment: NativeComment, emoji: string, reacted: boolean,
) {
  store.supersedeRead(pageId);
  const current = store.comments(pageId).find((item) => item.id === comment.id) ?? comment;
  store.replace(pageId, { ...current, reactions: reactionsAfter(current.reactions, emoji, store.session.user.id, reacted) });
  try {
    const response = await store.transport.setCommentReaction(store.session, pageId, comment.id, { emoji, reacted });
    return store.land(pageId, response.comment);
  } catch (error) {
    const latest = store.comments(pageId).find((item) => item.id === comment.id);
    if (latest) store.replace(pageId, { ...latest, reactions: current.reactions });
    throw error;
  }
}

/**
 * Adds or takes back the person's emoji on the page itself, as on a
 * comment: shown at once, the server's answer when it comes, and what was
 * shown before back on a refusal.
 */
export async function reactToPage(store: CommentReactionStore, pageId: string, emoji: string, reacted: boolean) {
  store.supersedeRead(pageId);
  const before = store.pageReactions(pageId);
  store.showPageReactions(pageId, reactionsAfter(before, emoji, store.session.user.id, reacted));
  try {
    const response = await store.transport.setPageReaction(store.session, pageId, { emoji, reacted });
    store.showPageReactions(pageId, response.reactions);
  } catch (error) {
    store.showPageReactions(pageId, before);
    throw error;
  }
}

/**
 * Resolves the page's reactions together: they leave at once, and come back
 * on a refusal.
 */
export async function resolvePageReactions(store: CommentReactionStore, pageId: string) {
  store.supersedeRead(pageId);
  const before = store.pageReactions(pageId);
  store.showPageReactions(pageId, []);
  try {
    const response = await store.transport.resolvePageReactions(store.session, pageId);
    store.showPageReactions(pageId, response.reactions);
  } catch (error) {
    store.showPageReactions(pageId, before);
    throw error;
  }
}
