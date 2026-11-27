import type { ContentGraph } from "@/contentGraph";
import { pageTitleJoin } from "@/lib/inlineDatabaseRequests";

/**
 * A Redo that brings back an inline database leaves the caret at the start
 * of the page's title, nothing chosen. As Notion's, measured in Chrome with
 * no script in the page before the action, the screen after it and one
 * letter typed: an inline database made by "/" Enter, ⌘Z, ⌘⇧Z, and the
 * letter went in before the title's words (2026-11-15: worker w1132 2 of 2,
 * a page's first database and a second one made through its start panel;
 * w1249 in another tab at another time and workspace, its panel shut by Esc).
 */
export function databaseRedoCaret(graph: () => ContentGraph, pageId: string, choose: (ids: string[]) => void) {
  let before: ReadonlySet<string> | null = null;
  const inlineDatabases = () => new Set(graph().blocks.filter(block => block.type === "inline_database").map(block => block.id));
  return {
    /** A history step is about to go; a Redo notes the page's inline databases. */
    stepping(redo: boolean) { before = redo ? inlineDatabases() : null; },
    /** After the step: whether it brought one back and the title took the caret. */
    placed() {
      const noted = before, title = pageTitleJoin(pageId);
      before = null;
      if (!noted || !title?.caretAtStart || ![...inlineDatabases()].some(id => !noted.has(id))) return false;
      choose([]);
      title.caretAtStart();
      return true;
    },
  };
}
