import { confirmActions } from "@/components/ConfirmActionDialog";
import { i18next } from "@/i18n";
import { CommentIcon } from "@/icons/leafen";
import styles from "./KeptDraftQuestion.module.css";

/** A new comment's draft put away with words in it: going back to it, or deleting it. */
export type KeptCommentDraft = { back(): void; drop(): void };

// One draft is kept at a time, wherever it was written — a page's text or a
// database value's words: starting any new comment asks about it first
// (Notion's, seen 2026-11-12: a page's kept draft asked when a cell's
// "댓글" was pressed).
let kept: { owner: string; draft: KeptCommentDraft } | null = null;

/** Keeps `owner`'s draft as the one a new comment asks about; null lets it go. */
export function keepCommentDraft(owner: string, draft: KeptCommentDraft | null) {
  if (draft) kept = { owner, draft };
  else if (kept?.owner === owner) kept = null;
}

/**
 * Starts a new comment, or, with a draft kept, asks first: "back" takes the
 * draft up again and "delete" deletes it, and either way nothing new opens;
 * Escape asks nothing more.
 */
export function startNewComment(start: () => void) {
  const asked = kept;
  if (!asked) { start(); return; }
  void askAboutKeptDraft().then((answer) => {
    if (kept !== asked) return;
    if (answer === "back") asked.draft.back();
    if (answer === "delete") { kept = null; asked.draft.drop(); }
  });
}

/**
 * Notion's question when a new comment is started while a draft is kept
 * (seen 2026-11-12, a value's words and a page's text alike): its comment
 * mark with a yellow dot, "댓글 초안 작성을 완료하시겠습니까?", what starting
 * another does, a blue "초안으로 돌아가기" over a red-outlined "삭제".
 */
function askAboutKeptDraft() {
  return confirmActions({
    message: i18next.t("commentsPanel:draft.unfinished"),
    detail: i18next.t("commentsPanel:draft.unfinishedDetail"),
    icon: <span className={styles.draftMark}><CommentIcon style={{ width: 32, height: 32 }} /><span className={styles.draftDot} /></span>,
    accent: true,
    cancelLabel: null,
    actions: [
      { value: "back", label: i18next.t("commentsPanel:draft.back") },
      { value: "delete", label: i18next.t("common:actions.delete"), danger: true },
    ],
  });
}
