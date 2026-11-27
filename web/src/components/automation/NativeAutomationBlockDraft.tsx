import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Editor } from "@/components/editor/Editor";
import type { NativeEditorGraphAdapter } from "@/components/editor/editorRuntimeTypes";
import type { InsertBlocksAutomationAction } from "@/lib/types";
import type { GraphOperation } from "@/contentGraph";
import { localEditorOrigin } from "@/lib/blockTextEditing";
import { createAutomationBlockDraft } from "./automationBlockDraft";
import styles from "./automationActionEditor.module.css";

/** Draft blocks belong to the host's native graph/text/file owners. The button
 * shows them only inside its setup; ordinary page and database services work. */
export function NativeAutomationBlockDraft({ action, context, ownerId, onChange }: {
  action: InsertBlocksAutomationAction; context: NativeEditorGraphAdapter; ownerId: string;
  onChange(action: InsertBlocksAutomationAction): void;
}) {
  const { t } = useTranslation("databaseView");
  const initializing = useRef(false);
  const [rootId, setRootId] = useState(action.sourceBlockId);
  useEffect(() => {
    if (action.sourceBlockId) { setRootId(action.sourceBlockId); return; }
    if (initializing.current || context.readOnly || !context.isCurrent()) return;
    initializing.current = true;
    const draft = createAutomationBlockDraft(action.blocks, true);
    const blocks = draft.graph.blocks.map(block => block.id === draft.rootId
      ? { ...block, parentId: ownerId, content: { buttonDraft: true } } : block);
    const operations: GraphOperation[] = blocks.map(block => ({ kind: "upsertBlock", block }));
    const admitted = context.changeLayout(graph => graph.blocks.some(block => block.id === ownerId)
      ? { graph: { ...graph, blocks: [...graph.blocks, ...blocks] }, operations } : null);
    if (admitted) {
      context.collaborationDocument?.transact(() => {
        for (const block of blocks) {
          const delta = draft.document.getText(`block:${block.id}`).toDelta();
          if (delta.length) context.collaborationDocument!.getText(`block:${block.id}`).applyDelta(delta);
        }
      }, localEditorOrigin);
      setRootId(draft.rootId);
      onChange({ ...action, sourceBlockId: draft.rootId, blocks: [] });
    }
    else initializing.current = false;
    draft.document.destroy();
  }, [action.sourceBlockId, context.generation, ownerId]);
  if (!rootId || !context.graph.blocks.some(block => block.id === rootId)) return <div aria-busy="true" />;
  return <div className={styles.blockDraft} data-button-block-draft data-hint={t("blockDraftHint")}>
    <Editor adapter={context} rootBlockId={rootId} loading={false} showPageStarter={false}
      onAddParagraph={() => { context.insertChild(rootId); }}
      onAddDatabase={() => { const block = context.insertChild(rootId); if (block) context.database.onChooseDatabase?.({ lineId: block.id, replaceLine: true, create: true }); }} />
  </div>;
}
