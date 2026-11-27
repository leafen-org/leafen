import { useEffect, useState } from "react";
import { discussionSideOf } from "@/components/comments/discussionSide";
import { useSearchParams } from "@/lib/router";

/**
 * Whether a page's own discussion shows under its title: while it has open
 * threads or the page's own reactions (a reaction to the title's words,
 * Notion's seen 2026-11-14), or once "댓글 추가" in its head was pressed on
 * this visit, as Notion's (seen 2026-11-12). A discussion begun that way leaves its empty
 * "댓글 추가" row and line when its last thread is resolved, under the page
 * or in "모든 토론", until the page is opened again; one the page opened
 * with goes at once with its row and line, whoever resolves it and wherever,
 * and answering it on this visit changes nothing.
 *
 * "댓글 추가" stays in the head while the discussion holds only the page's
 * reactions, and pressed there gives the caret to the field under them; it
 * leaves the head for this visit once pressed, the caret going into that
 * field itself leaving it be — as Notion's (seen 2026-11-14, 1440 wide).
 *
 * A title thread the address opens in a view of its own (discussionSide.ts)
 * keeps the discussion drawn for that view alone (sideOnly) when nothing else
 * shows it, its place under the title empty: resolved in the view, or
 * resolved and opened at its address, the view stays and opens, as Notion's
 * (seen 2026-11-25 in Chrome at 1440, two tabs).
 */
export function usePageDiscussionPlace(pageId: string, open: number, reactions: number) {
  const [composing, setComposing] = useState(false);
  const side = discussionSideOf(useSearchParams()) !== null;
  useEffect(() => {
    setComposing(false);
  }, [pageId]);
  const listed = open > 0 || reactions > 0 || composing;
  return { composing, setComposing, shown: listed || side, sideOnly: side && !listed, addInHead: open === 0 && !composing };
}
