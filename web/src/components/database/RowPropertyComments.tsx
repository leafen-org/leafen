"use client";

import { useEffect, useRef, type CSSProperties } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { CommentIcon } from "@/icons/leafen";
import { useProductToast } from "@/lib/productToast";
import type { TextSpan } from "@/lib/types";
import { placeAtPoint, placeInWindow, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import type { Session } from "@/types";
import { CommentComposer } from "../comments/CommentComposer";
import { commentDraftEmpty } from "../comments/commentModel";
import { setActiveCommentThreads } from "../comments/commentPanelEvents";
import { CommentThreadCard } from "../comments/CommentThreadCard";
import { useCommentsPanelController } from "../comments/useCommentsPanelController";
import { focusReplyBox } from "../comments/replyRowCaret";
import panelStyles from "../CommentsPanel.module.css";
import { PropertyIcon } from "./PropertyIcon";
import styles from "./RowPropertyComments.module.css";
import { askCaretForNewComment, giveNewCommentCaret } from "../editor/openedFieldCaret";

const GAP = 6;

/** What a database page's property discussions need: the row, where it is, and who comments. */
export type RowPropertyComments = {
  containerPageId: string;
  rowId: string;
  session: Session;
  workspaceId?: string;
  canEditPage: boolean;
  /** How many open threads each property has. */
  openByProperty: ReadonlyMap<string, number>;
  /** The properties whose whole value an open thread is on, tinted; without it, any open thread tints. */
  wholeValueProperties?: ReadonlySet<string>;
  /** Whether threads take new comments, replies and reactions: not in Trash (comment_trash.go). */
  canComment?: boolean;
};

/** "댓글 N개" at the end of a property's line, as Notion's (seen 2026-10-01). */
export function RowPropertyCommentsChip({ count, open, onOpen }: {
  count: number;
  open: boolean;
  onOpen(): void;
}) {
  const { t } = useTranslation("rowProperties");
  return (
    <button type="button" className={styles.chip} aria-label={t("propertyComments", { count })}
      aria-haspopup="dialog" aria-expanded={open} data-row-property-comments-chip onClick={onOpen}>
      <CommentIcon size="regular" aria-hidden="true" />
      <span>{count}</span>
    </button>
  );
}

/**
 * A property's discussion under its line, as Notion's (seen 2026-10-01): its
 * open threads, each quoting "<property>: <value>" with its reply box, or,
 * with none, the field that starts one on the property's value.
 */
export function RowPropertyCommentsPopover({ comments, property, quote, anchor, onClose, threadId, fromWords = false, resolvedToo = false, range, centered, initialDraft, onKeepDraft, onSent }: {
  comments: RowPropertyComments;
  /**
   * The value's property — its type and settings give its icon where the
   * threads quote it; with none, the row's own discussions as a page.
   */
  property?: { id: string; name: string; type?: string; config?: unknown };
  /** The value the property shows, which a new thread quotes. */
  quote: string;
  anchor: { left: number; top: number };
  onClose(): void;
  /** Only this thread: the one whose marked words in a cell were clicked. */
  threadId?: string;
  /**
   * Opened by pressing the thread's words: the caret stays in them, pressing
   * them again — a second or third press choosing a word or the line —
   * leaves the window open, and Escape closes it first, as Notion's (seen
   * 2026-11-12).
   */
  fromWords?: boolean;
  /** The thread shown even once resolved, as a page's updates open one (Notion's, seen 2026-11-12). */
  resolvedToo?: boolean;
  /** A new thread on these words of the value (a cell's "댓글"): its field alone, closed once sent. */
  range?: { start: number; end: number };
  /** Its middle under the anchor, as a cell's comment opens under its words (Notion, seen 2026-10-31). */
  centered?: boolean;
  /** A new thread's draft put away before, back in its field. */
  initialDraft?: readonly TextSpan[];
  /** Escape in a new thread's field holding a draft, or a press elsewhere: the window goes and the draft is kept (CellComments). */
  onKeepDraft?(draft: TextSpan[]): void;
  /** A new thread on words was sent, the window closing. */
  onSent?(): void;
}) {
  const { t } = useTranslation("commentsPanel");
  const { notify } = useProductToast();
  const controller = useCommentsPanelController({
    pageId: comments.containerPageId, rowId: comments.rowId, property, initialQuote: quote,
    initialQuoteStart: range?.start, initialQuoteEnd: range?.end,
    session: comments.session, workspaceId: comments.workspaceId,
    // A discussion the server refused says so, rather than sitting in its field.
    onStatus: (message, tone) => notify(message, tone === "error" ? "error" : "default"),
  });
  const { panelRef, draftRef, setDraft } = controller;
  const firstDraft = useRef(initialDraft);
  useEffect(() => { if (firstDraft.current?.length) setDraft([...firstDraft.current]); }, [setDraft]);
  const threads = range ? [] : threadId
    ? (resolvedToo ? controller.ordered : controller.visibleThreads).filter((comment) => comment.id === threadId) : controller.visibleThreads;
  // A thread's own window never turns into a new one's field.
  const starting = comments.canComment !== false && (Boolean(range) || threads.length === 0 && !controller.loading && !threadId);
  // The words of the threads shown are darker while they show, as Notion's
  // (seen 2026-11-12, CellCommentMarks).
  const shownIds = threads.map((thread) => thread.id).join(" ");
  useEffect(() => {
    if (!shownIds) return;
    setActiveCommentThreads(shownIds.split(" "));
    return () => setActiveCommentThreads([]);
  }, [shownIds]);
  // Each thread quotes the value under its property's icon and name after a
  // yellow bar, as Notion's (seen 2026-11-12: "Notes: gamma", no box).
  const quoteLabel = property ? (
    <span className={styles.quoteProperty}>
      {property.type ? <PropertyIcon property={{ type: property.type, config: property.config }} size="tiny" /> : null}
      <span className={styles.quotePropertyName}>{property.name}</span>:
    </span>
  ) : undefined;
  // Resolving or deleting its last open thread closes the discussion.
  const hadThreads = useRef(false);
  useEffect(() => {
    if (threads.length > 0) hadThreads.current = true;
    else if (hadThreads.current && !controller.loading) onClose();
  }, [controller.loading, onClose, threads.length]);

  // A new thread's field holding words, left by a press elsewhere, goes and
  // keeps its draft as its Escape does (Notion's, seen 2026-11-12).
  const leave = () => {
    const draft = controller.draftStore.get();
    if (range && onKeepDraft && !commentDraftEmpty(draft)) onKeepDraft(draft);
    else onClose();
  };
  // Just under the property, moved in and up as far as the window needs:
  // from its start, or centred under it by its top left.
  const place = usePopoverPlacement(panelRef, (size, viewport) => centered
    ? placeInWindow({ left: anchor.left - size.width / 2, top: anchor.top }, size, viewport)
    : placeAtPoint({ x: anchor.left, y: anchor.top + GAP }, size, viewport),
  [anchor.left, anchor.top, centered], undefined, { kind: "picker", onEscape: leave, onOutsidePress: leave,
    inside: () => fromWords && threadId ? [...document.querySelectorAll(
      `:is([data-rich-cell-text], [data-rich-cell-editor]) [data-comment-id~="${CSS.escape(threadId)}"]`)] : [],
    // Escape with the caret left in the words closes the window first, the
    // field staying in its editing (Notion's, seen 2026-11-12).
    onCaretKeyDown: (event) => {
      if (!fromWords || event.key !== "Escape" || (event.target instanceof Node && panelRef.current?.contains(event.target))) return;
      event.preventDefault(); event.stopPropagation(); leave();
    } });
  const style: CSSProperties = place ? popoverPlacementStyle(place) : { visibility: "hidden" };

  // The field takes the caret: the new thread's — after a draft brought
  // back, at its end — or the first thread's reply, unless the thread's
  // words were pressed.
  // What is typed as a new thread's box or the reply box opens waits for it
  // (openedFieldCaret, replyRowCaret).
  useEffect(() => {
    if (!starting) return fromWords ? undefined : focusReplyBox(() => panelRef.current);
    askCaretForNewComment();
    const frame = window.requestAnimationFrame(() => {
      giveNewCommentCaret(panelRef.current, () => { if (firstDraft.current?.length) draftRef.current?.focusEnd(); else draftRef.current?.focus(); });
    });
    return () => window.cancelAnimationFrame(frame);
  }, [draftRef, fromWords, panelRef, starting]);

  const popover = (
    <section ref={panelRef} className={styles.popover} style={style} role="dialog" data-starting={starting && threads.length === 0 ? "true" : undefined}
      aria-label={property ? t("commentsPanel:property.title", { name: property.name }) : t("commentsPanel:title")} data-row-property-comments={property?.id ?? ""}>
      {/* The threads and the field look as a page's under its title. */}
      <div className={panelStyles.inlinePanel}>
      {threads.map((comment) => (
        <CommentThreadCard key={comment.id} comment={comment} controller={controller} userId={comments.session.user.id}
          canEditPage={comments.canEditPage} canComment={comments.canComment !== false} linkTarget={comments.rowId} inWindow
          quoteLabel={quoteLabel} />
      ))}
      {starting ? (
        <CommentComposer
          attachments={controller.draftAttachments}
          currentUserLabel={comments.session.user.name || comments.session.user.email || t("commentsPanel:mention.you")}
          currentUserId={comments.session.user.id}
          draft={controller.draftStore}
          fieldRef={draftRef}
          inline
          pageId={comments.rowId}
          submitting={controller.submitting}
          onSubmit={() => void controller.submit().then((made) => { if (made && range) { onSent?.(); onClose(); } })}
          onEscapeDraft={range && onKeepDraft ? () => onKeepDraft(controller.draftStore.get()) : undefined}
          onUpdate={controller.setDraft}
        />
      ) : null}
      </div>
    </section>
  );
  return typeof document === "undefined" ? popover : createPortal(popover, document.body);
}
