import { useActiveCommentThreads } from "../comments/commentPanelEvents";

// Text a thread was left on, as Notion's discussions (seen 2026-11-04): the
// server marks it with the threads' ids in the page's text, and while a thread
// is open the text has a faint yellow background and underline, darker while
// its card is open or its comment is being written, and darker again where two
// discussions lie on the same text. It stays text to edit. A resolved or
// deleted thread's text looks as any other. A thread over words chosen
// across blocks, or a caption's words, marks them all alike (seen 2026-11-09).
// The line sits 1px under the words (seen 2026-11-12). Words that are also a
// suggested edit show the edit's marks instead of the yellow — at rest the
// edit's own, while the thread's card is open the edit's open ones below
// (Notion's, seen 2026-11-15 at 1440 dark: its open-discussion rules for
// suggested words come after its yellow ones).

const REST = "background: rgba(255, 203, 0, 0.12); border-bottom: 2px solid rgba(255, 203, 0, 0.35); padding-bottom: 1px;";
const ACTIVE = "background: rgba(255, 203, 0, 0.24); border-bottom: 2px solid rgb(255, 203, 0); padding-bottom: 1px;";
const OVERLAP = "background: rgba(255, 203, 0, 0.36); border-bottom: 2px solid rgb(255, 203, 0); padding-bottom: 1px;";
const ACTIVE_OVERLAP = "background: rgba(255, 203, 0, 0.48); border-bottom: 2px solid rgb(255, 203, 0); padding-bottom: 1px;";
// Text a new comment is being written on has no thread yet: the page's
// editor wraps it (pendingCommentMark.ts), as dark as an open card's while it
// is written and as faint as a resting one's while its draft is put away.
const PENDING = `[data-rt-editable] [data-comment-pending="writing"] { ${ACTIVE} }
[data-rt-editable] [data-comment-pending="kept"] { ${REST} }`;
// A suggested edit whose card is open is marked darker, as Notion's (seen
// 2026-11-15 at 1440 light and dark, the rules Notion writes for the open
// discussion): words suggested in or restyled on a darker blue tint and line,
// their blue kept; words suggested away on a darker gray tint and line, in
// secondary text and no longer struck through. Closed, they are as
// EditorInlineMarks draws them.
const SUGGESTED_OPEN = "background: var(--suggestion-insert-open-tint); border-bottom: 2px solid var(--suggestion-insert-open-line);";
const REMOVED_OPEN = "background: var(--suggestion-delete-open-tint); border-bottom: 2px solid var(--suggestion-delete-open-line); color: var(--text-secondary); text-decoration: none;";

/** An open thread and the blocks its text lies in, its own first. */
export type OpenCommentThread = { id: string; blockIds: readonly string[] };

export function CommentAnchorMarks({ pageId, openThreads }: { pageId: string; openThreads: readonly OpenCommentThread[] }) {
  const active = useActiveCommentThreads();
  const select = (id: string) => `[data-page-id="${CSS.escape(pageId)}"] [data-rt-editable] [data-comment-id~="${CSS.escape(id)}"]`;
  const plain = (selector: string) => `${selector}:not([data-suggestion])`;
  const rule = (selectors: readonly string[], style: string) => selectors.length ? `${selectors.join(",\n")} { ${style} }` : "";
  // Only threads whose text lies in a block they share can lie on the same text.
  const overlaps: { first: string; second: string }[] = [];
  openThreads.forEach((first, index) => {
    for (const second of openThreads.slice(index + 1)) {
      if (first.blockIds.some((blockId) => second.blockIds.includes(blockId))) overlaps.push({ first: first.id, second: second.id });
    }
  });
  const both = ({ first, second }: { first: string; second: string }) => `${select(first)}[data-comment-id~="${CSS.escape(second)}"]`;
  // An open card's suggested words: its own edit's, or the edits within a thread's words.
  const suggested = (id: string) => [`[data-page-id="${CSS.escape(pageId)}"] [data-rt-editable] [data-suggestion-id="${CSS.escape(id)}"]`, select(id)];
  return <style data-comment-anchor-marks>{[
    rule(openThreads.map(({ id }) => plain(select(id))), REST),
    rule(overlaps.map((pair) => plain(both(pair))), OVERLAP),
    rule(openThreads.filter(({ id }) => active.includes(id)).map(({ id }) => plain(select(id))), ACTIVE),
    rule(overlaps.filter(({ first, second }) => active.includes(first) || active.includes(second)).map((pair) => plain(both(pair))), ACTIVE_OVERLAP),
    rule(active.flatMap(suggested).map((words) => `${words}:is([data-suggestion="insert"], [data-suggestion="format"])`), SUGGESTED_OPEN),
    rule(active.flatMap(suggested).map((words) => `${words}[data-suggestion="delete"]`), REMOVED_OPEN),
    PENDING,
  ].join("\n")}</style>;
}
