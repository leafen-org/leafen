import { useId } from "react";
import { useTranslation } from "react-i18next";
import type { Session } from "@/types";
import { CommentsPanel } from "../CommentsPanel";
import styles from "./DatabaseRowPage.module.css";
import { ROW_DISCUSSIONS_HEAD } from "./rowDiscussionWords";

type RowDiscussionsProps = {
  canComment: boolean;
  canEditPage: boolean;
  containerPageId: string;
  rowId: string;
  session: Session;
  workspaceId?: string;
};

/**
 * A database page's own comments under its properties, as Notion's (seen
 * 2026-09-30, 2026-10-08; head 2026-11-25): its head ("토론"), its open
 * threads and, always, the field for a new one.
 */
export function RowPageComments({ autoFocus = false, ...row }: RowDiscussionsProps & {
  /** Puts the caret in the field for a new one, as a wiki page's "댓글 추가" does. */
  autoFocus?: boolean;
}) {
  const { t } = useTranslation(["commentsPanel", "pageLayout"]);
  return (
    <>
      <div className={styles.rowPageCommentsHeading}>{t(ROW_DISCUSSIONS_HEAD)}</div>
      <RowPageDiscussions {...row} autoFocus={autoFocus} className={styles.rowPageComments} />
    </>
  );
}

/**
 * The layout builder's preview of a database page's discussion: the
 * previewed page's open threads, read and drawn as the page itself shows
 * them, as Notion's builder does (seen 2026-11-25 in Chrome at 1440, two
 * tabs: a resolved thread left out, the open ones over "댓글 추가"). It only
 * shows them: no field (the preview draws its own, PageLayoutCanvas), and
 * nothing in it takes a press, a hover or the focus — a press chooses the
 * module, as on any card.
 */
export function RowPageDiscussionsPreview({ className, ...row }: Omit<RowDiscussionsProps, "canComment" | "canEditPage"> & { className?: string }) {
  return <RowPageDiscussions {...row} canComment={false} canEditPage={false} className={className} preview />;
}

/** The page's threads, one discussion read and drawn for the page and the preview alike (CommentsPanel inline). */
function RowPageDiscussions({ autoFocus = false, canComment, canEditPage, className, containerPageId, preview = false, rowId, session, workspaceId }: RowDiscussionsProps & {
  autoFocus?: boolean;
  className?: string;
  preview?: boolean;
}) {
  // The same database page shown twice keeps each discussion in its own place;
  // a preview's place is no page's, so the page's own field is never looked for in it.
  const hostId = `${preview ? "layout-preview-discussions" : "page-comments-inline"}-${rowId}-${useId()}`;
  return (
    <>
      <div id={hostId} className={className} data-row-page-comments data-layout-preview-discussions={preview || undefined}
        inert={preview || undefined} />
      <CommentsPanel inline inlineHostId={hostId} autoFocus={autoFocus} pageId={containerPageId} rowId={rowId} blockId={null}
        session={session} canEditPage={canEditPage} canComment={canComment} workspaceId={workspaceId} onClose={() => {}} />
    </>
  );
}
