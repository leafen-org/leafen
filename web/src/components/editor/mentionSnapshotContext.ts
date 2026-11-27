import type { EditorMoveContext } from "./editorRuntimeTypes";
import { createContext, useContext } from "react";
import type { BlockPickerSnapshot } from "./blockPickerModel";

// What the @ menu can mention on the page being edited, for text written
// outside its blocks' own editing, such as a database cell on the page.
export const MentionSnapshotContext = createContext<BlockPickerSnapshot | null>(null);

export function useMentionSnapshot() {
  return useContext(MentionSnapshotContext);
}

/** What a field's "@", "[[" and "+" menus make a page with, where the page's editing can, and the page whose sharing their invite opens. */
export const FieldPagesContext = createContext<{ moveContext: EditorMoveContext; workspaceId: string; pageId: string } | null>(null);
