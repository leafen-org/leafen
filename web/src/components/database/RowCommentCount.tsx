import { useContext } from "react";
import { useTranslation } from "react-i18next";
import { CommentIcon } from "@/icons/leafen";
import { CellCommentsContext } from "./cellCommentsContext";
import styles from "./RowCommentCount.module.css";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

/**
 * A row's open discussions as a page, as Notion's views show them (seen
 * 2026-11-05): a small count beside the title in a table, a list or a
 * timeline's table, on a line of its own under the values of a board, gallery
 * or calendar card. Pressing it opens them under it; a timeline's bars don't
 * show it.
 */
export function RowCommentCount({ rowId, line = false, inset = 10, insideButton = false, lineClassName }: { rowId: string; line?: boolean; inset?: number; insideButton?: boolean; lineClassName?: string }) {
  const { t } = useTranslation("commentsPanel");
  const comments = useContext(CellCommentsContext);
  const count = comments?.rowDiscussions(rowId) ?? 0;
  if (!comments || count === 0) return null;
  const open = (event: { preventDefault(): void; stopPropagation(): void; currentTarget: Element }) => {
    event.preventDefault();
    event.stopPropagation();
    comments.openRow({ rowId, rect: event.currentTarget.getBoundingClientRect() });
  };
  const shared = {
    className: styles.count,
    "aria-label": t("commentsPanel:openComments"),
    "data-row-comment-count": count,
    onPointerDown: (event: { stopPropagation(): void }) => event.stopPropagation(),
    onClick: open,
  };
  const content = (
    <>
      <CommentIcon size="small" aria-hidden="true" />
      <span>{count}</span>
    </>
  );
  // A calendar card is a button itself, and a button can't hold another.
  const control = insideButton
    ? <span {...shared} role="button" tabIndex={0} onKeyDown={(event) => { if (fieldEnter(event) || event.key === " ") open(event); }}>{content}</span>
    : <button {...shared} type="button">{content}</button>;
  return line ? <span className={lineClassName ? `${styles.line} ${lineClassName}` : styles.line} style={{ paddingInlineStart: inset }}>{control}</span> : control;
}
