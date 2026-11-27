"use client";

import { useEffect, type CSSProperties } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import type { Session } from "@/types";
import { CommentComposer } from "../comments/CommentComposer";
import { CommentThreadCard } from "../comments/CommentThreadCard";
import { useCommentsPanelController } from "../comments/useCommentsPanelController";
import panelStyles from "../CommentsPanel.module.css";
import styles from "./FeedPostThreads.module.css";
import { askCaretForNewComment, giveNewCommentCaret } from "../editor/openedFieldCaret";

const GAP = 4;

/**
 * A feed post's discussion, opened from its line under the post, as
 * Notion's (seen 2026-10-21; 2026-11-25 in Chrome at 1440, two tabs): 480px
 * wide, centered 4px under the line — 4px over it where the window has no
 * room under it — headed "토론", the post page's open
 * threads as the page shows them under its title — each answered from its
 * own reply row its "↩" opens, its replies folded behind their row — and
 * its field, which starts a new thread. Its threads have no "▯ 열기".
 */
export function FeedPostThreads({ rowId, holderPageId, workspaceId, session, canComment, anchor, onClose }: {
  rowId: string;
  /** The page holding the post's database, where the row's discussion is kept. */
  holderPageId: string;
  workspaceId?: string;
  session: Session;
  canComment: boolean;
  /** The line it opened from. */
  anchor: HTMLElement;
  onClose(): void;
}) {
  const { t } = useTranslation("feedPost");
  const controller = useCommentsPanelController({ pageId: holderPageId, rowId, session, workspaceId });
  const { panelRef } = controller;
  const place = usePopoverPlacement(panelRef, (size, viewport) => placeAround(popoverRectOf(anchor) ?? anchor.getBoundingClientRect(),
    size, viewport, { gap: GAP, align: "center" }), [anchor], undefined, { kind: "menu", onEscape: onClose, onOutsidePress: onClose, inside: () => [anchor], returnFocusRef: { current: anchor } });
  const style: CSSProperties = place ? popoverPlacementStyle(place) : { visibility: "hidden" };
  // The box takes the caret, as a property's discussion does.
  const { draftRef } = controller;
  // What is typed as it opens waits for the box (openedFieldCaret).
  useEffect(() => {
    askCaretForNewComment();
    const frame = window.requestAnimationFrame(() => giveNewCommentCaret(panelRef.current, () => draftRef.current?.focus()));
    return () => window.cancelAnimationFrame(frame);
  }, [draftRef, panelRef]);
  const popover = (
    <section ref={panelRef} className={styles.threads} style={style} role="dialog" aria-label={t("discussion")} data-feed-post-threads={rowId}>
      <div className={styles.head}>{t("discussion")}</div>
      <div className={panelStyles.inlinePanel}>
        {controller.visibleThreads.map((comment) => (
          <CommentThreadCard key={comment.id} comment={comment} controller={controller} userId={session.user.id}
            canEditPage={canComment} canComment={canComment} linkTarget={rowId} flat />
        ))}
        {canComment ? (
          <CommentComposer avatarSize={24} attachments={controller.draftAttachments} currentUserId={session.user.id}
            currentUserLabel={session.user.name || session.user.email} draft={controller.draftStore} fieldRef={controller.draftRef}
            inline pageId={rowId} submitting={controller.submitting}
            onSubmit={() => void controller.submit()} onUpdate={controller.setDraft} />
        ) : null}
      </div>
    </section>
  );
  return typeof document === "undefined" ? popover : createPortal(popover, document.body);
}
