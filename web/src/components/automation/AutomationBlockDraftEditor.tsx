import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Editor } from "@/components/editor/Editor";
import { editorContentEqual } from "@/components/editor/editorContentDelta";
import { createNativeEditorGraphAdapter } from "@/components/editor/nativeEditorGraphAdapter";
import { localEditorOrigin } from "@/lib/blockTextEditing";
import { interactiveAutomationMaxDocumentBytes } from "@/interactiveAutomationWire.generated";
import type { InsertBlocksAutomationAction } from "@/lib/types";
import type { NativeEditorGraphAdapter } from "@/components/editor/editorRuntimeTypes";
import { createAutomationBlockDraft, automationBlocksFromDraft } from "./automationBlockDraft";
import styles from "./automationActionEditor.module.css";
import { NativeAutomationBlockDraft } from "./NativeAutomationBlockDraft";

type DraftProps = { action: InsertBlocksAutomationAction; context?: NativeEditorGraphAdapter; ownerId?: string;
  onChange(action: InsertBlocksAutomationAction): void };
export function AutomationBlockDraftEditor(props: DraftProps) {
  return props.context && props.ownerId
    ? <NativeAutomationBlockDraft action={props.action} context={props.context} ownerId={props.ownerId} onChange={props.onChange} />
    : <LocalAutomationBlockDraft {...props} />;
}
function LocalAutomationBlockDraft(props: DraftProps) {
  const [epoch, setEpoch] = useState(0);
  const expected = useRef(props.action.blocks);
  const incoming = JSON.stringify(props.action.blocks);
  useLayoutEffect(() => {
    if (!editorContentEqual(props.action.blocks, expected.current)) {
      expected.current = props.action.blocks; setEpoch(value => value + 1);
    }
  }, [incoming]);
  return <LocalBlockDraftEditor key={epoch} {...props} onChange={next => {
    expected.current = next.blocks; props.onChange(next);
  }} />;
}
function LocalBlockDraftEditor({ action, context, onChange }: {
  action: InsertBlocksAutomationAction; context?: NativeEditorGraphAdapter;
  onChange(action: InsertBlocksAutomationAction): void;
}) {
  const { t } = useTranslation("databaseView");
  const [error, setError] = useState(false), [, rerender] = useState(0);
  const [draft] = useState(() => createAutomationBlockDraft(action.blocks));
  const latest = useRef({ action, onChange, context }); latest.current = { action, onChange, context };
  const incoming = useRef(action.blocks);
  const [lifecycle] = useState(() => new AbortController());
  const current = useRef(true);
  const graph = useRef(draft.graph);
  const publishQueued = useRef(false);
  function publish() {
    if (publishQueued.current) return;
    publishQueued.current = true;
    queueMicrotask(() => {
      publishQueued.current = false;
      if (!current.current || lifecycle.signal.aborted) return;
      const blocks = automationBlocksFromDraft(graph.current, draft.document, draft.rootId);
      // As many blocks as the settings document holds (64 KB).
      const invalid = new TextEncoder().encode(JSON.stringify({ ...latest.current.action, blocks })).length > interactiveAutomationMaxDocumentBytes;
      setError(invalid);
      if (!invalid && !editorContentEqual(blocks, incoming.current)) {
        incoming.current = blocks; latest.current.onChange({ ...latest.current.action, blocks });
      }
    });
  }
  useEffect(() => {
    current.current = true;
    draft.document.on("update", publish);
    return () => { current.current = false; draft.document.off("update", publish);
      queueMicrotask(() => { if (!current.current) { lifecycle.abort(); draft.document.destroy(); } }); };
  }, []);
  // External definition changes remount through the parent key; own acknowledgements
  // keep this document and its caret. The action ID fences other cards.
  const adapter = createNativeEditorGraphAdapter({
    pageId: `button-draft-${action.id}`, workspaceId: context?.workspaceId ?? "", session: null,
    generation: 1, signal: lifecycle.signal, graph: graph.current, collaborationDocument: draft.document,
    readOnly: context?.readOnly ?? false, mentionSnapshot: context?.mentionSnapshot ?? null,
    database: { pendingToolbarDataSourceIds: new Set() }, pagePlaceholderBlockId: null,
    isCurrent: () => current.current && !lifecycle.signal.aborted && (latest.current.context?.isCurrent() ?? true),
    commit(next) { graph.current = next; rerender(value => value + 1); publish(); },
    changeText(block, text) { draft.document.transact(() => {
      const value = draft.document.getText(`block:${block.id}`); value.delete(0, value.length); value.insert(0, text);
    }, localEditorOrigin); },
  });
  return <div className={styles.blockDraft} data-button-block-draft data-hint={t("blockDraftHint")}>
    <Editor adapter={adapter} rootBlockId={draft.rootId} loading={false} showPageStarter={false}
      onAddParagraph={() => { adapter.insertChild(draft.rootId); }} onAddDatabase={() => setError(true)} />
    {error ? <p role="alert">{t("blockDraftTooLarge")}</p> : null}
  </div>;
}
