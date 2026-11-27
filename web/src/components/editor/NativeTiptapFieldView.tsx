import { blockChildrenDirection } from "@/lib/textDirection";
import { useEffect, useRef } from "react";
import { NodeViewContent, NodeViewWrapper, type NodeViewProps } from "@tiptap/react";
import { blockItemText } from "./blockItemLabels";
import { blockCaptionSpans, captionLabelKey } from "./blockCaptionModel";
import { useCaretInsideNode } from "./nativeTiptapCaretInside";
import { nodeViewPlace, useNativeTiptapNode } from "./nativeTiptapReactContext";
import styles from "./editorStyles.generated.module.css";
import engineStyles from "./NativeTiptap.module.css";
import { simpleTableCellColor, simpleTableColumnFloor } from "./simpleTableColumns";
import { mergeSpanAt, simpleTableMergeSpans, tableHasMergedCells, type SimpleTableMergeSpan } from "./simpleTableMerges";
import { useTableCellPlace } from "./useTableCellPlace";
import { tableCellColorStyle } from "./blockHandleModel";
import { useSuggestingMode } from "./suggestingMode";
import { useShownLineWay } from "./nativeTiptapLineWay";
import { captionOffersAlt, ImageCaptionAlt } from "./ImageCaptionAlt";
import altStyles from "./ImageCaptionAlt.module.css";

export function NativeTiptapCaptionView(props: NodeViewProps) {
  const { block, adapter } = useNativeTiptapNode(props);
  const suggesting = useSuggestingMode();
  // An empty caption closes once the caret has left it, as Notion's (seen
  // 2026-11-06: an image's opened and left, a board's written and cleared).
  const inside = useCaretInsideNode(props, !suggesting.active);
  const wasInside = useRef(false);
  // Its "ALT" window, while open, holds an empty caption open; closed, the caption closes if still empty —
  // Notion's empty caption is gone too once its "ALT" is pressed (seen 2026-11-15).
  const altOpen = useRef(false);
  const closeIfEmpty = () => {
    if (!block || adapter.readOnly) return;
    const live = adapter.graph.blocks.find(candidate => candidate.id === block.id);
    if (live?.content.captionVisible === true && !blockCaptionSpans(live).some(span => span.text.length > 0)) {
      adapter.updateBlock(live, { captionVisible: false });
    }
  };
  useEffect(() => {
    if (inside) { wasInside.current = true; return; }
    if (!wasInside.current || altOpen.current) return;
    wasInside.current = false;
    closeIfEmpty();
  }, [inside, block, adapter]); // eslint-disable-line react-hooks/exhaustive-deps
  // An empty caption asks "캡션 작성" under a code block, image, file or
  // embed alike, as Notion's (seen 2026-11-13; English "Write a caption…"),
  // apart from the "캡션 추가하기" button over an image. A picture with no alt text offers "ALT" at its end.
  const offersAlt = captionOffersAlt(block, adapter, suggesting.active);
  return <NodeViewWrapper as="figcaption" className={offersAlt ? `${styles.caption} ${altStyles.host}` : styles.caption} data-native-caption="true" data-block-caption="true" data-block-control="block-caption"
    data-caret={inside ? "true" : undefined} contentEditable={suggesting.active ? false : undefined}>
    <NodeViewContent className={styles.richMarks} data-rt-editable="true" data-empty={props.node.content.size === 0 ? "true" : "false"}
      data-placeholder={blockItemText("common.captionPlaceholder")} aria-label={blockItemText(captionLabelKey(block?.type))} />
    {offersAlt && block ? <ImageCaptionAlt block={block} adapter={adapter} onOpenChange={(open) => {
      altOpen.current = open;
      if (!open) { wasInside.current = false; closeIfEmpty(); }
    }} /> : null}
  </NodeViewWrapper>;
}

// A row's cells sit on the table's own columns (its --simple-table-columns).
export function NativeTiptapTableCellsView() {
  return <NodeViewWrapper><NodeViewContent data-native-table-cells="true" /></NodeViewWrapper>;
}

// A table's merged cells, read once for all its cells while its rows stay the same.
const tableMerges = new WeakMap<object, Map<string, SimpleTableMergeSpan[] | null>>();

/**
 * Where a cell of a table with merged cells stands on the table's grid: at
 * its row and column, a merged one across its rows and columns and as wide
 * as their narrowest together; a cell one stands over is not shown and
 * names the merged cell (Notion, seen 2026-11-10). Nothing for a table
 * without merged cells, whose rows lay out their own cells.
 */
function mergedCellPlace(spans: readonly SimpleTableMergeSpan[] | null, rowIds: readonly string[], row: number, column: number) {
  if (!spans || row < 0) return null;
  const merged = mergeSpanAt(spans, row, column);
  const tall = merged ? merged.bottom - merged.top + 1 : 1, wide = merged ? merged.right - merged.left + 1 : 1;
  if (merged && (merged.top !== row || merged.left !== column)) return { covered: `${rowIds[merged.top]}:${merged.left}` };
  return { tall, wide, style: { gridRow: `${row + 1} / span ${tall}`, gridColumn: `${column + 1} / span ${wide}`,
    ...(wide > 1 ? { minWidth: `calc(${Array.from({ length: wide }, (_, at) => `var(${simpleTableColumnFloor(column + at)}, 0px)`).join(" + ")})` } : {}) } };
}

export function NativeTiptapTableCellView(props: NodeViewProps) {
  const { graphIndex, block } = useNativeTiptapNode(props);
  const suggesting = useSuggestingMode();
  const wrapper = useRef<HTMLDivElement>(null);
  const index = useTableCellPlace(wrapper, nodeViewPlace(props)?.index() ?? 0);
  const table = block?.parentId ? graphIndex.byId.get(block.parentId) : undefined;
  const rows = table ? graphIndex.children.get(table.id)?.filter(candidate => candidate.type === "table_row") ?? [] : [];
  const row = rows.findIndex(candidate => candidate.id === block?.id);
  const columnHeader = table?.content.hasColumnHeader === true && row === 0;
  const rowHeader = table?.content.hasRowHeader === true && index === 0;
  const color = simpleTableCellColor(table, block ?? undefined, index);
  let merges = table ? tableMerges.get(graphIndex)?.get(table.id) : null;
  if (table && merges === undefined) {
    merges = tableHasMergedCells(rows) ? simpleTableMergeSpans(rows, Math.max(1, Number(table.content.tableWidth) || 1)) : null;
    tableMerges.set(graphIndex, (tableMerges.get(graphIndex) ?? new Map()).set(table.id, merges));
  }
  const place = mergedCellPlace(merges ?? null, rows.map(candidate => candidate.id), row, index);
  if (place && "covered" in place) return <NodeViewWrapper ref={wrapper} className={engineStyles.cell} data-native-table-cell="true"
    data-table-cell={`${block?.id}:${index}`} data-table-covered={place.covered} aria-hidden="true">
    <NodeViewContent className={`${styles.simpleTableCell} ${styles.richMarks}`} data-rt-editable="true" />
  </NodeViewWrapper>;
  return <NodeViewWrapper ref={wrapper} className={engineStyles.cell} role={columnHeader ? "columnheader" : rowHeader ? "rowheader" : "cell"}
    contentEditable={suggesting.active ? false : undefined} data-header={columnHeader || rowHeader ? "true" : undefined}
    data-native-table-cell="true" data-table-cell={`${block?.id}:${index}`} data-table-color={color || undefined}
    data-table-rows={place && place.tall > 1 ? place.tall : undefined} aria-rowspan={place && place.tall > 1 ? place.tall : undefined}
    aria-colspan={place && place.wide > 1 ? place.wide : undefined}
    style={{ minWidth: `var(${simpleTableColumnFloor(index)}, 0px)`, ...place?.style, ...tableCellColorStyle(color) }}>
    <NodeViewContent className={`${styles.simpleTableCell} ${styles.richMarks}`} data-rt-editable="true" data-empty={props.node.content.size === 0 ? "true" : "false"}
      aria-label={blockItemText(columnHeader || rowHeader ? "simpleTable.headerCellNumbered" : "simpleTable.cellNumbered", { row: row + 1, column: index + 1 })} />
  </NodeViewWrapper>;
}

/** The words of the line whose children these are: its block's text, held by the editor. */
function ownLineWords(props: NodeViewProps) {
  const position = typeof props.getPos === "function" ? props.getPos() : undefined;
  if (position === undefined || position < 0 || position > props.editor.state.doc.content.size) return undefined;
  let words: string | undefined;
  props.editor.state.doc.resolve(position).parent.forEach((child) => {
    if (words === undefined && child.type.name === "nativeText") words = child.textContent;
  });
  return words;
}

export function NativeTiptapChildrenView(props: NodeViewProps) {
  const { block, graphIndex, occurrenceId } = useNativeTiptapNode(props);
  // Its line's side as the line's own view shows it, which turns with the
  // letters typed there and with a line with none (nativeTiptapLineWay.ts).
  const shown = useShownLineWay(props.editor, occurrenceId);
  // A tab block's tabs and a tab's blocks fill the tab box, not indented, as
  // Notion's, and a column's its column; a callout's children stand in its
  // box under its text.
  const inTabs = block?.type === "tab" || (block?.parentId ? graphIndex.byId.get(block.parentId)?.type === "tab" : false);
  return <NodeViewWrapper className={block?.type === "column_list" ? engineStyles.columns
    : block?.type === "table" || block?.type === "column" || inTabs ? undefined : block?.type === "callout" ? styles.calloutChildren : styles.children}
    data-native-children="true" data-direction={block ? blockChildrenDirection(block, ownLineWords(props), shown) : undefined}><NodeViewContent /></NodeViewWrapper>;
}
