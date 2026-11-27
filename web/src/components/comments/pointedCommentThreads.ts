import { useEffect, useState } from "react";

/** Text a thread was left on: a page's, or a database value's (commentPanelEvents discussionMarks). */
const MARK = ":is([data-rt-editable], [data-rich-cell-text], [data-rich-cell-editor]) [data-comment-id]";
const NONE: readonly string[] = [];

/**
 * The threads, of those shown, whose text the mouse is over. Notion's margin
 * card for words under the pointer steps toward them and lights as it does
 * under the pointer itself, the words darkening, and goes back as the pointer
 * leaves them (seen 2026-11-16 at 1440 dark on a block's words; the strike's
 * 2026-11-15 note on chosen words).
 */
export function usePointedCommentThreads(shown: readonly string[], enabled: boolean): readonly string[] {
  const [pointed, setPointed] = useState<readonly string[]>(NONE);
  const key = shown.join(" ");
  useEffect(() => {
    setPointed(NONE);
    if (!enabled || !key) return undefined;
    const ids = new Set(key.split(" "));
    let current = "";
    const over = (event: PointerEvent) => {
      if (event.pointerType !== "mouse") return;
      const target = event.target instanceof Element ? event.target : null;
      const mark = target?.closest<HTMLElement>(MARK);
      const next = mark?.dataset.commentId?.split(" ").filter((id) => ids.has(id)).join(" ") ?? "";
      if (next === current) return;
      current = next;
      setPointed(next ? next.split(" ") : NONE);
    };
    document.addEventListener("pointerover", over, { passive: true });
    return () => document.removeEventListener("pointerover", over);
  }, [enabled, key]);
  return pointed;
}
