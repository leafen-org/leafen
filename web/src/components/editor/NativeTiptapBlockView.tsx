import { useCallback, useState } from "react";
import { NodeViewContent, NodeViewWrapper, type NodeViewProps } from "@tiptap/react";
import { TOGGLE_BLOCK_TYPES } from "@/lib/blockSemantics";
import { BlockFrame } from "./BlockFrame";
import { blockRun } from "./blockRun";
import { storedListWidth, useWideColumnList } from "./columnListWidth";
import { NativeTiptapColumnView } from "./NativeTiptapColumnView";
import { StructuredTableBlock } from "./SpecialBlockContent";
import { tableHasMergedCells } from "./simpleTableMerges";
import { drawNewTableRowWhole } from "./simpleTableRowArrival";
import { SyncedBlockContent } from "./SyncedBlockContent";
import { SyncedEmptyContent } from "./SyncedEmptyContent";
import { SYNCED_SOURCELESS, syncedBlockSource } from "./syncedBlockModel";
import { SyncedMirror } from "./syncedBlockContext";
import { syncedFrameChoice } from "./blockOccurrences";
import { TabBlockContent } from "./TabBlockContent";
import { blockItemText } from "./blockItemLabels";
import { focusMadeLine } from "./focus";
import { useCaretInsideNode } from "./nativeTiptapCaretInside";
import { nodeViewPlace, useNativeTiptapNode, useNativeTiptapUi } from "./nativeTiptapReactContext";
import styles from "./editorStyles.generated.module.css";
import engineStyles from "./NativeTiptap.module.css";

export function NativeTiptapBlockView(props: NodeViewProps) {
  const context = useNativeTiptapNode(props);
  const { block, adapter, graphIndex, selection, setSelection, depth, occurrenceId, attrs, setActiveTabs } = context;
  const selectTab = useCallback((id: string) => setActiveTabs(current => current.get(occurrenceId) === id ? current
    : new Map([...current, [occurrenceId, id]])), [occurrenceId, setActiveTabs]);
  // A view draws again when its own block is selected or not, not for
  // every block selected.
  const selected = useNativeTiptapUi(state => block ? state.selection.has(block.id) : false, false);
  const tab = block?.parentId ? graphIndex.byId.get(block.parentId) : undefined;
  let tabOccurrence = "";
  if (block && tab?.type === "tab") {
    const resolved = nodeViewPlace(props);
    tabOccurrence = tab.id;
    for (let index = resolved?.depth ?? 0; index > 0; index--) if (resolved!.node(index).type.name === "nativeBlock") {
      tabOccurrence = resolved!.node(index).attrs.id; break;
    }
  }
  const openTab = useNativeTiptapUi(state => tabOccurrence ? state.activeTabs.get(tabOccurrence) : undefined);
  // A synced block shows its frame and toolbar while the caret is in it, and
  // while it, or what is chosen inside it, is chosen there (syncedFrameChoice).
  const caretInside = useCaretInsideNode(props, block?.type === "synced_block");
  const synced = block?.type === "synced_block" && occurrenceId ? occurrenceId : null;
  const chosen = useNativeTiptapUi(state => synced ? syncedFrameChoice(state.selection, synced, graphIndex.byId) : null, null);
  const source = block && syncedBlockSource(block, adapter.pageId, adapter.graph.blocks);
  const localSynced = block?.type === "synced_block" && !!source && (!source.copy || source.pageId === adapter.pageId);
  // A column list as wide as its outer edges were dragged (columnListWidth).
  const [wrapper, setWrapper] = useState<HTMLElement | null>(null);
  const wide = useWideColumnList(wrapper, block?.type === "column_list" ? storedListWidth(block.content) : null);
  if (!block || !source) return <NodeViewWrapper><NodeViewContent /></NodeViewWrapper>;
  if (block.type === "column") return <NativeTiptapColumnView {...props} />;
  if (block.type === "table_row") return <NodeViewWrapper ref={(row: HTMLElement | null) => drawNewTableRowWhole(props.editor, row)}
    data-block-id={occurrenceId} data-table-row-id={block.id} role="row">
    <NodeViewContent /></NodeViewWrapper>;
  const children = graphIndex.children.get(block.id) ?? [];
  const content = <NodeViewContent className={engineStyles.blockContent} />;
  const parent = tab;
  if (parent?.type === "tab") {
    // A tab's item may be any block, as Notion's (a heading, a callout…): its
    // own line names the tab, its blocks are the tab's.
    const first = graphIndex.children.get(parent.id)?.[0];
    const active = (openTab ?? first?.id) === block.id;
    return <NodeViewWrapper data-native-tab-item={block.id} role="tabpanel" id={`tabpanel-${parent.id}-${block.id}`}
      aria-labelledby={`tab-${parent.id}-${block.id}`} style={{ display: active ? undefined : "none" }}>
      {content}
      {!children.length && <button type="button" contentEditable={false} className={styles.tabEmptyHint} disabled={adapter.readOnly}
        aria-label={blockItemText("tabs.addBlockInsideEmpty")} onClick={() => focusMadeLine(() => adapter.insertChild(block.id), occurrenceId)}>{blockItemText("tabs.empty")}</button>}
    </NodeViewWrapper>;
  }
  const unavailable = attrs.projectionStatus === "unavailable";
  const body = localSynced ? <SyncedBlockContent adapter={adapter} block={block} caretInside={caretInside} chosen={chosen} engineContent={<>{content}
    {unavailable ? <div className={styles.syncedMissing} contentEditable={false}>{blockItemText("synced.unavailable")}</div>
      : !props.node.content.content.some(node => node.type.name === "nativeChildren")
      ? <SyncedEmptyContent adapter={adapter} originalId={source.blockId} at={occurrenceId} drawsOriginal={source.copy} /> : null}</>} />
    : block.type === "table" ? <StructuredTableBlock adapter={adapter} block={block}
      rowsContent={<div role="table" className={engineStyles.table} aria-rowcount={children.length}
        aria-colcount={Number(block.content.tableWidth) || 1} data-table-merged={tableHasMergedCells(children) ? "true" : undefined}>{content}</div>} />
    : block.type === "tab" ? <div className={styles.tabBox}><div contentEditable={false}><TabBlockContent adapter={adapter} block={block}
      items={children} panels={new Map()} renderPanel={false} onActiveTabChange={selectTab}
      onRename={() => setSelection(current => current.size ? new Set() : current)} /></div>
      <div>{content}</div></div>
    : content;
  const collapsed = TOGGLE_BLOCK_TYPES.has(block.type) && block.content.collapsed === true;
  // A sub-page its reader may not open isn't drawn, as Notion's (seen
  // 2026-11-09, a guest); the server says so (content_graph_reader_mentions.go).
  const closedPage = block.type === "child_page" && block.content.pageLinkUnavailable === true;
  // Nor is a copy syncing from no block (SYNCED_SOURCELESS), as Notion's.
  const sourceless = block.type === "synced_block" && source.blockId === SYNCED_SOURCELESS;
  // A line that is only a link runs on with its neighbours as a page block does.
  const words = props.node.firstChild?.type.name === "nativeText" ? props.node.firstChild.content : null;
  return <NodeViewWrapper ref={block.type === "column_list" ? setWrapper : undefined} style={wide} data-wide-columns={wide ? "true" : undefined}
    className={engineStyles.block} data-native-block="true" data-native-column-list={block.type === "column_list" ? "true" : undefined}
    data-native-collapsed={collapsed ? "true" : undefined} data-kind={block.type} data-native-occurrence={occurrenceId}
    hidden={closedPage || sourceless || undefined} data-page-closed={closedPage ? "true" : undefined}>
    <SyncedMirror.Provider value={Boolean(attrs.sourceId)}>
      <BlockFrame adapter={adapter} block={block} depth={depth} selection={selection} setSelection={setSelection} selected={selected}
        run={blockRun(block.type, words)} occurrence={occurrenceId || block.id}>
        {body}
        {TOGGLE_BLOCK_TYPES.has(block.type) && !collapsed && !children.length && !adapter.readOnly ? <button type="button" contentEditable={false}
          className={styles.toggleEmptyHint} data-heading={block.type.startsWith("toggle_heading") ? "true" : undefined}
          onClick={() => focusMadeLine(() => adapter.insertChild(block.id), occurrenceId)}>
          {blockItemText("emptyTogglePrompt")}</button> : null}
      </BlockFrame>
    </SyncedMirror.Provider>
  </NodeViewWrapper>;
}
