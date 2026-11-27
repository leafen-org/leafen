import { useActiveCommentThreads } from "../comments/commentPanelEvents";

// Words of a database value a thread was left on, as Notion's cell (seen
// 2026-10-31, 2026-11-11): the server marks them with the thread's id in the
// value (comment_cell_anchors.go) and, while the thread is open, they have a
// discussion's yellow ground and underline, faint at rest and darker while a
// window or card shows the thread (Notion's, seen 2026-11-12: .12 under .35,
// then .24 under a full line), and pressing them opens the thread. A
// resolved or deleted thread's words look as any other, and a reopened one's
// are marked again: its mark stays in the value, which
// resolving, reopening or deleting a thread does not write, as Notion's
// lists no edit for them. Words a new comment is being written on look the
// same, and fainter while its draft is put away (seen 2026-11-12:
// rgba(255, 203, 0, 0.12) over a line at 0.35), until it is sent or deleted.
// The line sits 1px under the words, as Notion's (seen 2026-11-12).

const ACTIVE = "background: rgba(255, 203, 0, 0.24); border-bottom: 2px solid rgb(255, 203, 0); padding-bottom: 1px;";
const REST = "background: rgba(255, 203, 0, 0.12); border-bottom: 2px solid rgba(255, 203, 0, 0.35); padding-bottom: 1px;";
const PENDING = `[data-rich-cell-editor] [data-comment-pending="writing"] { ${ACTIVE} }
[data-rich-cell-editor] [data-comment-pending="kept"] { ${REST} }`;

/** The marks of the open threads named, and of words a comment is being written on, in any database value's text. */
export function CellCommentMarks({ threadIds }: { threadIds: readonly string[] }) {
  const active = useActiveCommentThreads();
  const select = (ids: readonly string[]) => ids.map((id) => `:is([data-rich-cell-text], [data-rich-cell-editor]) [data-comment-id~="${CSS.escape(id)}"]`);
  const rule = (selectors: readonly string[], style: string) => selectors.length ? `${selectors.join(",\n")} { ${style} }` : "";
  return <style data-cell-comment-marks>{[
    rule(select(threadIds), REST),
    rule(select(threadIds.filter((id) => active.includes(id))), ACTIVE),
    PENDING,
  ].join("\n")}</style>;
}
