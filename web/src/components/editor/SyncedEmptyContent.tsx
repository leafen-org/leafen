import { blockItemText } from "./blockItemLabels";
import { editorBlockChildren } from "./editorGraphModel";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { focusMadeLine } from "./focus";
import styles from "./editorStyles.generated.module.css";

/**
 * Starts writing in an empty original, including through one of its copies:
 * the line takes the caret where the block was pressed, `at`, inside the
 * copy when it is a copy drawing its original (focusMadeLine).
 */
export function SyncedEmptyContent({ adapter, originalId, at, drawsOriginal = false }: {
  adapter: NativeEditorGraphAdapter; originalId: string; at: string; drawsOriginal?: boolean;
}) {
  // Not the page's words: out of the editable area, so not spell-checked, as an empty toggle's hint.
  return <button type="button" contentEditable={false} className={styles.toggleEmptyChild} disabled={adapter.readOnly} data-editor-empty-content
    onClick={() => focusMadeLine(() => editorBlockChildren(adapter.graph, originalId)[0] ?? adapter.insertChild(originalId), at, drawsOriginal)}>
    {blockItemText(adapter.readOnly ? "synced.empty" : "synced.addContent")}
  </button>;
}
