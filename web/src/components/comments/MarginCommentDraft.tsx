import { useEffect, useRef, useState } from "react";
import type { ProductStatusSink } from "@/lib/productStatus";
import type { Session } from "@/types";
import { caretMayComeBack } from "@/components/editor/focus";
import panelStyles from "../CommentsPanel.module.css";
import { CommentComposer } from "./CommentComposer";
import { commentDraftEmpty } from "./commentModel";
import { useCommentDraft } from "./commentDraftStore";
import type { OpenCommentsDetail } from "./commentPanelEvents";
import styles from "./MarginDiscussions.module.css";
import { usePendingCommentMark } from "./pendingCommentMark";
import { commentTextPlaces } from "./commentQuotePlaces";
import { useCommentsPanelController } from "./useCommentsPanelController";
import { giveNewCommentCaret } from "../editor/openedFieldCaret";

/**
 * A new comment on a wide page is written where its card will stand, as
 * Notion's (seen 2026-11-04): a one-line field beside the block, its text
 * marked meanwhile; posting leaves the card there. It steps aside as the
 * comment goes, its card standing at once (commentService create), and comes
 * back with its words if the comment is refused. Its draft put away (`kept`,
 * MarginDiscussions) marks its text fainter (Notion's, seen 2026-11-12).
 */
export function MarginCommentDraft({ detail, pageId, rowId, session, workspaceId, onStatus, kept, focusTurn, onFilledChange, onEscapeDraft, onClose }: {
  detail: OpenCommentsDetail;
  pageId: string;
  /** A database page's own discussions, which its row keeps. */
  rowId?: string;
  session: Session;
  workspaceId?: string;
  onStatus?: ProductStatusSink;
  /** Its draft is put away: the field is left, its card resting. */
  kept: boolean;
  /** Gives the field the caret again each time it changes. */
  focusTurn: number;
  /** Whether it holds words or files, which leaving it keeps. */
  onFilledChange(filled: boolean): void;
  /** Escape in the field while it holds a draft. */
  onEscapeDraft(): void;
  onClose(): void;
}) {
  const controller = useCommentsPanelController({ pageId, rowId, blockId: detail.blockId, session, workspaceId, onStatus,
    initialQuote: detail.initialQuote, initialQuoteStart: detail.initialQuoteStart, initialQuoteEnd: detail.initialQuoteEnd,
    initialQuotePlaces: detail.initialQuotePlaces });
  usePendingCommentMark(detail.blockId && detail.initialQuoteStart !== undefined && detail.initialQuoteEnd !== undefined
    ? commentTextPlaces(detail.blockId, detail.initialQuoteStart, detail.initialQuoteEnd, detail.initialQuotePlaces) : null,
  kept ? "kept" : "writing");
  const { draftRef } = controller;
  const [sending, setSending] = useState(false);
  const box = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const frame = window.requestAnimationFrame(() => giveNewCommentCaret(box.current, () => draftRef.current?.focus()));
    return () => window.cancelAnimationFrame(frame);
  }, [draftRef, focusTurn]);
  // This field alone redraws as it is typed in (commentDraftStore).
  const draft = useCommentDraft(controller.draftStore);
  const filled = !commentDraftEmpty(draft) || controller.draftAttachments.fileIds.length > 0;
  useEffect(() => onFilledChange(filled), [filled, onFilledChange]);
  return <div ref={box} className={`${panelStyles.inlinePanel} ${styles.draft}`} data-margin-comment-draft hidden={sending}>
    <CommentComposer bare inline attachments={controller.draftAttachments} blockId={detail.blockId} draft={controller.draftStore} fieldRef={draftRef}
      currentUserId={session.user.id} currentUserLabel={session.user.name || session.user.email || ""} pageId={pageId}
      submitting={controller.submitting} onUpdate={controller.setDraft} onEscapeDraft={onEscapeDraft} onEscapeEmpty={onClose}
      onSubmit={() => {
        setSending(true);
        const caretBack = caretMayComeBack();
        void controller.submit().then((made) => {
          if (made) { onClose(); return; }
          setSending(false);
          window.requestAnimationFrame(() => { if (caretBack()) draftRef.current?.focus(); });
        });
      }} />
  </div>;
}
