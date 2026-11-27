import { type MutableRefObject, useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { AtIcon, SendCircleIcon } from "@/icons/leafen";
import type { TextSpan } from "@/lib/types";
import box from "./CommentReplyComposer.module.css";
import toolStyles from "./CommentTools.module.css";
import { CommentAttachButton, CommentPendingAttachments } from "./CommentAttachControls";
import type { CommentAttachmentDraft } from "./commentAttachmentDraft";
import { useCommentDraft, type CommentDraftSource } from "./commentDraftStore";
import { commentDraftEmpty } from "./commentModel";
import { giveFocusBack } from "@/lib/focusGiveBack";
import { CommentRichField, type CommentFieldHandle } from "./CommentRichField";

/**
 * An open thread's reply box, as Notion's (seen 2026-09-30): always under
 * the thread, "회신…" until typed in, a blue ring while focused — "댓글
 * 추가" and no ring in a window under a block or a database value (seen
 * 2026-10-28, 2026-11-12) — with mention and send while it has focus or
 * words, or while its thread is the one chosen: the thread whose box was
 * last typed in, which holds the panel's reply draft, or an open margin
 * card's (Notion's, seen 2026-11-12: "모든 토론" shows them on the thread last
 * chosen alone, a window's stay once its box was focused, a margin card's
 * show as it opens).
 * Escape in a box holding a reply asks whether to delete it, as Notion's
 * (seen 2026-11-12): "삭제" empties the box, "취소" leaves it, the caret
 * back in it either way. A press that opens the box gives it the caret
 * (replyRowCaret.ts).
 */
export function CommentReplyComposer({
  active,
  activeRef,
  attachments,
  draft: draftSource,
  inWindow = false,
  toolsShown = false,
  underTitle = false,
  pageId,
  submitting,
  onStart,
  onDraft,
  onSubmit,
}: {
  /** This thread holds the reply draft. */
  active: boolean;
  /** Where the panel finds the box of the reply being written. */
  activeRef?: MutableRefObject<CommentFieldHandle | null>;
  /** The files the reply will hold, while this thread holds the draft. */
  attachments?: CommentAttachmentDraft;
  /** The words typed in the box: a letter redraws the box alone, not its thread or list. */
  draft: CommentDraftSource;
  /** In a window under a block or a database value, where Notion's box reads "댓글 추가" and takes no ring. */
  inWindow?: boolean;
  /** Its buttons show though it is empty and left: an open margin card's. */
  toolsShown?: boolean;
  /**
   * A thread's own box under a page's title: no ring and its tools only while
   * focused or holding words, as the field under the threads; on a phone the
   * tools on a line under it (CommentReplyComposer.module.css).
   */
  underTitle?: boolean;
  pageId?: string;
  submitting: boolean;
  onStart: () => void;
  onDraft: (value: TextSpan[]) => void;
  onSubmit: () => void;
}) {
  const { t } = useTranslation("commentsPanel");
  const fieldRef = useRef<CommentFieldHandle>(null);
  const draft = useCommentDraft(draftSource);
  useEffect(() => {
    if (!active || !activeRef) return;
    activeRef.current = fieldRef.current;
    return () => { if (activeRef.current === fieldRef.current) activeRef.current = null; };
  }, [active, activeRef]);
  const empty = commentDraftEmpty(draft);
  const blank = empty && !(active && attachments?.fileIds.length);
  // With files, its tools sit under them, as Notion's (seen 2026-11-06).
  const withFiles = Boolean(active && attachments?.items.length);
  const tools = (
    <>
      {attachments ? (
        // Picking a file takes the reply draft to this thread first.
        <span className={box.attach} onPointerDownCapture={() => { if (!active) onStart(); }}><CommentAttachButton draft={attachments} /></span>
      ) : null}
      {/* "@" in a box not yet typed in takes the draft there first. */}
      <button type="button" className={toolStyles.tool} aria-label={t("mention.trigger")} title={t("mention.trigger")} data-reply-mention
        onMouseDown={(event) => event.preventDefault()}
        onClick={() => { if (!active) onStart(); fieldRef.current?.mention(); }}><AtIcon size="regular" aria-hidden="true" /></button>
      <button type="button" className={`${toolStyles.tool} ${toolStyles.send}`} aria-label={t("reply.send")} title={t("reply.send")} data-reply-send
        disabled={!active || (empty && !(attachments?.fileIds.length)) || attachments?.uploading || submitting}
        onClick={() => { fieldRef.current?.publish(); onSubmit(); }}>
        <SendCircleIcon />
      </button>
    </>
  );
  return (
    <div className={box.box} data-comment-reply-box data-empty={blank ? "true" : undefined}
      data-tools-shown={(active && !underTitle) || toolsShown ? "true" : undefined} data-window={inWindow ? "true" : undefined}
      data-under-title={underTitle ? "true" : undefined}>
      <div className={box.row}>
        <CommentRichField
          handleRef={fieldRef}
          spans={draft}
          pageId={pageId}
          ariaLabel={t(inWindow ? "reply.addComment" : "reply.placeholder")}
          placeholder={t(inWindow ? "reply.addComment" : "reply.placeholder")}
          className={box.field}
          containerClassName={box.fieldBox}
          dataAttributes={active ? { "data-comment-reply-input": "true", "data-comment-reply-active": "true" } : { "data-comment-reply-input": "true" }}
          onFocus={() => { if (!active) onStart(); }}
          onChange={(spans) => {
            if (!active) onStart();
            onDraft(spans);
          }}
          onSubmit={onSubmit}
          // Escape, the @ list closed: a box holding a reply asks whether to
          // delete it and takes the key; an empty one leaves the box and
          // passes it on to close what it sits in, a popover — a margin card
          // stays, as Notion's (MarginDiscussions).
          onEscape={() => {
            if (empty && !(active && attachments?.fileIds.length)) {
              (document.activeElement as HTMLElement | null)?.blur();
              return false;
            }
            void confirmAction({
              message: t("reply.discardConfirm"),
              confirmLabel: t("reply.discard"),
              // A filled red "삭제" over an outlined "취소", as Notion's.
              danger: "filled",
            }).then((discard) => {
              if (discard) {
                onDraft([]);
                attachments?.clear();
              }
              giveFocusBack({ give: () => fieldRef.current?.focusEnd(), unlessOpened: true });
            });
            return true;
          }}
        />
        {withFiles ? null : tools}
      </div>
      {withFiles && attachments ? (
        <>
          <CommentPendingAttachments draft={attachments} />
          <div className={box.foot}>{tools}</div>
        </>
      ) : null}
    </div>
  );
}
