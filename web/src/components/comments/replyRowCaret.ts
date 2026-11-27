import { focusOpenedField } from "@/components/editor/openedFieldCaret";

/**
 * A thread's reply box a press opens — its "↩", the thread pressed or
 * reopened in "모든 토론" or in its own view, its card pressed open beside its
 * block — takes the caret at its end once it is drawn, and what is typed from
 * the press until then goes in there (focusOpenedField), as Notion's, whose
 * box is there at the press (seen 2026-11-25: a letter typed right after a
 * block's thread was pressed in "모든 토론" went into its reply row; given the
 * caret a render on, the box lost the letters typed meanwhile). The box is
 * the one in `within` — a thread's card, a margin card — the page left where
 * it is. Called in the press itself; gives what calls it off.
 */
export function focusReplyBox(within: () => Element | null | undefined): () => void {
  return focusOpenedField(() => within()?.querySelector<HTMLElement>("[data-comment-reply-input]"), { scroll: false });
}
