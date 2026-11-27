import { createContext, useContext } from "react";
import type { BlockPickerSnapshot } from "@/components/editor/blockPickerModel";

// What a comment's @ menu offers — dates, the people and the pages, as the
// page body's @ menu does, as Notion's comment box (seen 2026-10-15) — for
// comments written outside the body's editing, a read-only page's too.
export const CommentMentionContext = createContext<BlockPickerSnapshot | null>(null);

export function useCommentMentionSnapshot() {
  return useContext(CommentMentionContext);
}
