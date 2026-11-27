import { NodeViewContent, NodeViewWrapper, type NodeViewProps } from "@tiptap/react";
import type { KeyboardEvent, PointerEvent } from "react";
import { useEffect, useRef, useState } from "react";
import { ColumnSlot } from "./ColumnListContent";
import { addColumnAfter, columnShares, columnWeights, draggedWeights, moveColumn, removeColumn, setColumnWeights, settledWeights, sharesOf } from "./columnLayoutModel";
import { draggedListWidth, setColumnListWidth, storedListWidth, widestListIn } from "./columnListWidth";
import { focusEditable } from "./focus";
import { useNativeTiptapNode, useNativeTiptapUi } from "./nativeTiptapReactContext";
import { inlineEndKey, inlineSign } from "@/lib/readingDirection";

/** Layout controls keep their native policies; PM retains every child view. */
export function NativeTiptapColumnView(props: NodeViewProps) {
  const context = useNativeTiptapNode(props);
  const { block, adapter, graphIndex, setColumnPreview } = context;
  const columnPreview = useNativeTiptapUi(state => state.columnPreview, new Map<string, number>());
  const cancelResize = useRef<(() => void) | null>(null);
  const [edgeResizing, setEdgeResizing] = useState<"start" | "end" | null>(null);
  useEffect(() => () => cancelResize.current?.(), []);
  useEffect(() => { if (adapter.readOnly || adapter.signal.aborted) cancelResize.current?.(); }, [adapter.readOnly, adapter.signal]);
  if (!block) return <NodeViewWrapper><NodeViewContent /></NodeViewWrapper>;
  const columns = graphIndex.children.get(block.parentId)?.filter(candidate => candidate.type === "column") ?? [];
  const index = columns.findIndex(candidate => candidate.id === block.id);
  const shares = columnShares(columns);
  const share = columnPreview.get(block.id) ?? shares[index] ?? 50;
  // The gap follows the pointer, the pair trading weight in Notion's steps;
  // let go, no weight stays above 100 (columnLayoutModel draggedWeights).
  function resize(event: PointerEvent<HTMLButtonElement>) {
    if (index < 1 || adapter.readOnly) return;
    cancelResize.current?.();
    event.preventDefault(); event.stopPropagation();
    // The columns' own room, the gaps between them left out.
    const list = event.currentTarget.closest("[data-native-column-list]");
    const room = [...list?.querySelectorAll<HTMLElement>("[data-native-column]") ?? []]
      .filter((column) => column.closest("[data-native-column-list]") === list)
      .reduce((sum, column) => sum + column.getBoundingClientRect().width, 0);
    const initial = columnWeights(columns), start = event.clientX, sign = inlineSign(event.currentTarget);
    let weights = initial;
    const update = (move: globalThis.PointerEvent) => {
      weights = draggedWeights(initial, index, (move.clientX - start) * sign, room);
      const next = sharesOf(weights);
      setColumnPreview(new Map(columns.map((column, at) => [column.id, next[at]])));
    };
    const cancel = () => {
      window.removeEventListener("pointermove", update); window.removeEventListener("pointerup", finish);
      window.removeEventListener("pointercancel", cancel); adapter.signal.removeEventListener("abort", cancel);
      cancelResize.current = null;
      setColumnPreview(new Map());
    };
    const finish = () => {
      cancel();
      const settled = settledWeights(weights);
      if (settled.some((weight, at) => weight !== initial[at]) && adapter.isCurrent() && !adapter.readOnly) {
        adapter.changeLayout(graph => setColumnWeights(graph, block!.id, settled));
      }
    };
    cancelResize.current = cancel;
    window.addEventListener("pointermove", update); window.addEventListener("pointerup", finish);
    window.addEventListener("pointercancel", cancel); adapter.signal.addEventListener("abort", cancel, { once: true });
  }
  // A list on the page itself widens by its outer edges, settling as the
  // pointer lets go (columnListWidth draggedListWidth).
  const list = graphIndex.byId.get(block.parentId ?? "");
  const edges = list && !list.parentId ? { start: index === 0, end: index === columns.length - 1 } : undefined;
  function resizeList(event: PointerEvent<HTMLButtonElement>, edge: "start" | "end") {
    if (!list || adapter.readOnly) return;
    cancelResize.current?.();
    event.preventDefault(); event.stopPropagation();
    const box = event.currentTarget.closest<HTMLElement>("[data-native-column-list]");
    const width = (box?.getBoundingClientRect().width ?? 0) + 12, page = (box?.parentElement?.getBoundingClientRect().width ?? 0) + 12;
    const widest = widestListIn(box?.closest<HTMLElement>("[data-page-scroll]") ?? null);
    const start = event.clientX, sign = inlineSign(event.currentTarget) * (edge === "end" ? 1 : -1);
    let at = start;
    const follow = (move: globalThis.PointerEvent) => { at = move.clientX; };
    const cancel = () => {
      window.removeEventListener("pointermove", follow); window.removeEventListener("pointerup", finish);
      window.removeEventListener("pointercancel", cancel); adapter.signal.removeEventListener("abort", cancel);
      cancelResize.current = null;
      setEdgeResizing(null);
    };
    const finish = (up: globalThis.PointerEvent) => {
      cancel();
      const next = draggedListWidth(width, ((up.clientX || at) - start) * sign, page, widest);
      if (next !== storedListWidth(list.content) && adapter.isCurrent() && !adapter.readOnly) adapter.changeLayout(graph => setColumnListWidth(graph, list.id, next));
    };
    cancelResize.current = cancel;
    setEdgeResizing(edge);
    window.addEventListener("pointermove", follow); window.addEventListener("pointerup", finish);
    window.addEventListener("pointercancel", cancel); adapter.signal.addEventListener("abort", cancel, { once: true });
  }
  // ← and → trade one of the drag's steps (Notion's handle takes no keys).
  function resizeKey(event: KeyboardEvent<HTMLButtonElement>) {
    if (index < 1 || !["ArrowLeft", "ArrowRight"].includes(event.key)) return;
    event.preventDefault();
    const weights = columnWeights(columns), total = weights.reduce((sum, weight) => sum + weight, 0);
    const moved = (event.key === inlineEndKey(event.currentTarget) ? 1 : -1) * 100 / (8 * columns.length) / total;
    const next = settledWeights(draggedWeights(weights, index, moved, 1));
    if (next.some((weight, at) => weight !== weights[at])) adapter.changeLayout(graph => setColumnWeights(graph, block!.id, next));
  }
  return <NodeViewWrapper style={{ flex: `${share} 1 0`, minWidth: 0 }} data-native-column>
    <ColumnSlot column={block} index={index} count={columns.length} share={share} manageable={!adapter.readOnly}
      resizing={columnPreview.has(block.id)} onResizeStart={resize} onResizeKey={resizeKey}
      edges={edges} edgeResizing={edgeResizing} onEdgeStart={resizeList}
      onAddAfter={() => {
        let focus = "";
        adapter.changeLayout(graph => { const change = addColumnAfter(graph, block.id); focus = change?.focusId ?? ""; return change; });
        if (focus) focusEditable(focus, "start");
      }} onMove={direction => adapter.changeLayout(graph => moveColumn(graph, block.id, direction))}
      onRemove={() => adapter.changeLayout(graph => removeColumn(graph, block.id))}>
      <NodeViewContent />
    </ColumnSlot>
  </NodeViewWrapper>;
}
