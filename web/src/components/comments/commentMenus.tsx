import type { TFunction } from "i18next";
import { focusAfterPaint } from "@/components/editor/focus";
import { holdingTyping } from "@/lib/pageTitleFocus";
import { useTranslation } from "react-i18next";
import { Bell, BellSlashIcon, EnvelopeIcon, EnvelopeOpenIcon, LinkIcon, Pencil, Trash } from "@/icons/leafen";
import type { NativeComment } from "@/lib/commentService";
import { copyText } from "@/lib/copyText";
import type { ProductStatusSink } from "@/lib/productStatus";
import { useOptionalProductToast } from "@/lib/productToast";
import type { CommentMenuItem } from "./CommentHoverActions";
import { confirmCommentDelete } from "./commentDelete";
import { commentThreadLink } from "./commentLinks";
import type { useCommentsPanelController } from "./useCommentsPanelController";

type CommentsController = ReturnType<typeof useCommentsPanelController>;

/**
 * A thread's first comment's "…", as Notion's: marking the thread unread or
 * read, the person's own edit and the thread's link, then below a line
 * turning its replies' notifications off or on and the person's own delete.
 * The same wherever the comment shows — an open card, a resting one, the
 * panel (seen 2026-11-16 in a resting margin card).
 */
export function threadMenuItems(t: TFunction, { comment, canEdit, canDelete, onEdit, onCopyLink, onThreadState, onDelete }: {
  comment: NativeComment;
  canEdit: boolean;
  canDelete: boolean;
  onEdit: () => void;
  onCopyLink: () => void;
  onThreadState: (change: { repliesMuted?: boolean; unread?: boolean }) => void;
  onDelete: () => void;
}): CommentMenuItem[] {
  return [
    comment.unread
      ? { key: "unread", label: t("commentsPanel:menu.markRead"), icon: <EnvelopeOpenIcon size="regular" aria-hidden="true" />,
        onSelect: () => onThreadState({ unread: false }) }
      : { key: "unread", label: t("commentsPanel:menu.markUnread"), icon: <EnvelopeIcon size="regular" aria-hidden="true" />,
        onSelect: () => onThreadState({ unread: true }) },
    ...(canEdit ? [editItem(t, onEdit)] : []),
    { key: "copyLink", label: t("commentsPanel:menu.copyLink"), icon: <LinkIcon size="regular" aria-hidden="true" />, onSelect: onCopyLink },
    muteItem(t, !!comment.repliesMuted, (muted) => onThreadState({ repliesMuted: muted })),
    ...(canDelete ? [deleteItem(t, onDelete)] : []),
  ];
}

/**
 * A reply's "…": the person's own edit, the thread's link and, below a line,
 * turning the thread's replies off or on and the person's own delete, as
 * Notion's.
 */
export function replyMenuItems(t: TFunction, { canManage, repliesMuted, onEdit, onCopyThreadLink, onMuteReplies, onDelete }: {
  canManage: boolean;
  repliesMuted: boolean;
  onEdit: () => void;
  onCopyThreadLink: () => void;
  onMuteReplies: (muted: boolean) => void;
  onDelete: () => void;
}): CommentMenuItem[] {
  return [
    ...(canManage ? [editItem(t, onEdit)] : []),
    { key: "copyLink", label: t("commentsPanel:menu.copyThreadLink"), icon: <LinkIcon size="regular" aria-hidden="true" />, onSelect: onCopyThreadLink },
    muteItem(t, repliesMuted, onMuteReplies),
    ...(canManage ? [deleteItem(t, onDelete)] : []),
  ];
}

// A comment's edit box, its editor made as it opens.
const EDIT_FIELD = "[data-comment-edit-composer] [contenteditable='true']";

/**
 * "편집하기": the comment's edit box takes the caret at its end once the frame
 * it opens in is painted, what is typed from the press until then held for it
 * and typed there (focusAfterPaint) — the first edit right after a comment
 * was sent opened some frames late, and ⌘A pressed at once went to the box
 * that sent, the words then typed going after the comment's own (2026-11-22).
 * Notion's box takes the caret 0.2 s on and leaves what is typed before to
 * the page — its ⌘A chooses the page's blocks and the letters go nowhere
 * (seen 2026-11-22, two pages in two tabs); none of it is lost here.
 */
function editItem(t: TFunction, onEdit: () => void): CommentMenuItem {
  return { key: "edit", label: t("commentsPanel:menu.edit"), icon: <Pencil size="regular" aria-hidden="true" />, onSelect: () => {
    const open = new Set(document.querySelectorAll(EDIT_FIELD));
    focusAfterPaint(() => {
      const field = [...document.querySelectorAll<HTMLElement>(EDIT_FIELD)].find((element) => !open.has(element));
      // The box took the caret on its own (its autofocus) and what was held is typed there, a ⌘A's
      // choice with it; while it is still being typed, the caret is placed there all the same.
      if (field?.contains(document.activeElement) && !holdingTyping()) return "kept";
      return field ? { field, position: "end" } : null;
    });
    onEdit();
  } };
}

function muteItem(t: TFunction, muted: boolean, onMute: (muted: boolean) => void): CommentMenuItem {
  return muted
    ? { key: "muteReplies", label: t("commentsPanel:menu.unmuteReplies"), icon: <Bell size="regular" aria-hidden="true" />,
      separatorBefore: true, onSelect: () => onMute(false) }
    : { key: "muteReplies", label: t("commentsPanel:menu.muteReplies"), icon: <BellSlashIcon size="regular" aria-hidden="true" />,
      separatorBefore: true, onSelect: () => onMute(true) };
}

function deleteItem(t: TFunction, onDelete: () => void): CommentMenuItem {
  return { key: "delete", label: t("commentsPanel:menu.delete"), icon: <Trash size="regular" aria-hidden="true" />, danger: true,
    onSelect: (opener) => confirmCommentDelete(onDelete, opener) };
}

/**
 * What a thread's menus do beyond the controller's own changes: turning its
 * replies off or on says so, and "copy link" copies the thread's address —
 * a reply's the same one — saying whether it could.
 */
export function useThreadMenuActions(controller: Pick<CommentsController, "setThreadState">, comment: NativeComment, linkTarget: string, onStatus?: ProductStatusSink) {
  const { t } = useTranslation("commentsPanel");
  const toast = useOptionalProductToast();
  function say(message: string, tone?: "success" | "error") {
    if (toast) toast.notify(message, tone);
    else onStatus?.(message, tone ?? "default");
  }
  return {
    say,
    async changeThreadState(change: { repliesMuted?: boolean; unread?: boolean }) {
      if (!(await controller.setThreadState(comment, change)) || change.repliesMuted === undefined) return;
      say(t(change.repliesMuted ? "toast.repliesMuted" : "toast.repliesUnmuted"));
    },
    async copyThreadLink() {
      const copied = await copyText(commentThreadLink(linkTarget, comment.parentId || comment.id));
      say(t(copied ? "toast.linkCopied" : "toast.linkCopyFailed"), copied ? "success" : "error");
    },
  };
}
