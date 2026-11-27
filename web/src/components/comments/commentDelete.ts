import { confirmAction } from "@/components/ConfirmActionDialog";
import { i18next } from "@/i18n";

/**
 * Asks before a comment goes, in the middle of the window as Notion's
 * "이 댓글을 삭제하시겠어요?" does (seen 2026-10-28), its "삭제" first — once
 * the menu that asked has closed, so the focus comes back to its "…". A
 * discussion's first comment asks the same: only it goes, its replies
 * staying in the discussion (seen 2026-11-21).
 *
 * Answered by Escape it gives the "…" (`opener`, the control its menu opened
 * from) the focus back ringed, its tip shown, and by a press on its shade
 * ringless until a key comes, as Notion's (seen 2026-11-26 in Chrome at 1440,
 * two tabs at different times, judged after the action by the screen and one
 * letter typed). A menu closed by a press gives its "…" the focus back only
 * once that closing is painted, after this frame: the question is handed the
 * "…" as its opener, not the focus's place as it opens.
 */
export function confirmCommentDelete(remove: () => void, opener?: HTMLElement | null) {
  window.requestAnimationFrame(() => {
    void confirmAction({
      opener,
      message: i18next.t("commentsPanel:delete.commentConfirm"),
      confirmLabel: i18next.t("common:actions.delete"),
      // A filled red "삭제" over an outlined "취소", as Notion's (seen 2026-11-06).
      danger: "filled",
      tabKeepsAction: true,
    }).then((confirmed) => { if (confirmed) remove(); });
  });
}
