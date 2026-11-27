import type { NativeComment } from "@/lib/commentService";
import { CommentReactionChips, useReactionPicker } from "./CommentReactions";
import styles from "./MarginDiscussions.module.css";
import type { useCommentsPanelController } from "./useCommentsPanelController";

/**
 * A discussion begun by a reaction to words stands in a wide page's margin as
 * its reactions' chips, in the cards' order, 12px under where a card on the
 * words would stand, as Notion's (seen 2026-11-22 in Chrome at 1440: "😀 1"
 * by a paragraph's word at 224.5 by its block at 220, by a table row's cell
 * words at 434.5 by the row at 431.5; the card under it 10 under its chip,
 * a card over it ending 22 over its chip; a block's own chip before its card).
 * Hovered, it offers another reaction and resolving, as Notion's, and keeps
 * offering them while the picker its "+" opened is open (seen 2026-11-26 in
 * Chrome at 1440 in two tabs at different times); a chip pressed takes the
 * person's reaction back or gives it.
 */
export function MarginReactionEntry({ thread, controller, userId, canComment, top }: {
  thread: NativeComment;
  controller: ReturnType<typeof useCommentsPanelController>;
  userId: string;
  canComment: boolean;
  top: number | undefined;
}) {
  const picker = useReactionPicker((emoji) => controller.react(thread, emoji, true),
    { workspaceId: thread.workspaceId, pageId: thread.pageId });
  return <div className={styles.reactionEntry} data-margin-discussion={thread.id} data-margin-reaction
    data-picking={picker.picker ? "" : undefined} style={{ top: top ?? 0, visibility: top === undefined ? "hidden" : undefined }}>
    <CommentReactionChips reactions={thread.reactions} userId={userId} workspaceId={thread.workspaceId} canReact={canComment}
      variant="page" className={styles.reactionRow} onToggle={(emoji, reacted) => controller.react(thread, emoji, reacted)}
      onOpenPicker={picker.open} onResolve={canComment ? () => controller.toggleResolved(thread) : undefined} />
    {picker.picker}
  </div>;
}
