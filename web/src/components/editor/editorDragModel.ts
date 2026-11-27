const EDITOR_BLOCK_DRAG_MIME = "application/x-leafen-native-block";

export type EditorBlockDragPayload = {
  pageId: string;
  blockIds: string[];
  generation: number;
};

const EDITOR_BLOCKS = "leafen-editor-blocks";

/** What a drag of blocks carries in the page; Pragmatic drag and drop keeps
 * it with the drag. Dragged blocks move, or copy when Alt (Option) is held
 * as they are dropped. */
export function editorBlockDragData(payload: EditorBlockDragPayload): Record<string, unknown> {
  return { type: EDITOR_BLOCKS, ...payload };
}

/** What other targets see: the block ids as text, and a type the editor's own
 * engine recognises so it never inserts them as text itself. */
export function editorBlockDragExternal(payload: EditorBlockDragPayload): Record<string, string> {
  return { [EDITOR_BLOCK_DRAG_MIME]: JSON.stringify(payload), "text/plain": payload.blockIds.join("\n") };
}

/** Whether a native drag carries blocks; what it carries can be read only when they are dropped. */
export function isEditorBlockDrag(dataTransfer: DataTransfer) {
  return Array.from(dataTransfer.types).includes(EDITOR_BLOCK_DRAG_MIME);
}

export function editorBlockDragPayload(data: Record<string | symbol, unknown>): EditorBlockDragPayload | null {
  if (data.type !== EDITOR_BLOCKS || typeof data.pageId !== "string" || typeof data.generation !== "number"
    || !Number.isSafeInteger(data.generation)) return null;
  if (!Array.isArray(data.blockIds) || data.blockIds.some((id) => typeof id !== "string")) return null;
  return { pageId: data.pageId, generation: data.generation, blockIds: [...data.blockIds] as string[] };
}
