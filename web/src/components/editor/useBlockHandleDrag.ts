import { useEffect, useRef, type RefObject } from "react";
import { draggableByPointer } from "@/lib/pointerDrag";
import { carriedBlocks, showBlockDragPreview } from "./blockDragPreview";
import { editorBlockDragData, editorBlockDragExternal, type EditorBlockDragPayload } from "./editorDragModel";

// The drop layers a dragged block is in, each one out to the page's, take the
// margin beside the blocks while it is dragged (EditorCanvas.module.css); the
// ones marked are given back to put the mark away.
function markDropLayers(handle: Element): Element[] {
  const layers: Element[] = [];
  for (let layer = handle.closest("[data-editor-drop-layer]"); layer; layer = layer.parentElement?.closest("[data-editor-drop-layer]") ?? null) {
    layer.setAttribute("data-block-dragging", "true");
    layers.push(layer);
  }
  return layers;
}

/**
 * A block's handle drags its selected blocks; the drop layer
 * (EditorDropLayer) and the sidebar's rows decide where they land. A mouse's
 * or a pen's drag follows the pointer's own events (pointerDrag), carrying a
 * see-through copy of the blocks (carriedBlocks), as Notion's: the browser's
 * own drop was lost at times in WebKit, the desktop app's window. A finger's
 * is the browser's drag, its picture the same copy (showBlockDragPreview).
 */
export function useBlockHandleDrag(handleRef: RefObject<HTMLElement | null>, payload: EditorBlockDragPayload, readOnly: boolean) {
  const dragged = useRef(payload);
  dragged.current = payload;
  useEffect(() => {
    const handle = handleRef.current;
    if (!handle || readOnly) return undefined;
    const blockOf = () => handle.closest<HTMLElement>("[data-block-id]");
    let marked: Element[] = [];
    return draggableByPointer({
      element: handle,
      getInitialData: () => editorBlockDragData(dragged.current),
      getInitialDataForExternal: () => editorBlockDragExternal(dragged.current),
      onGenerateDragPreview: ({ nativeSetDragImage, location }) => {
        const block = blockOf();
        if (block) showBlockDragPreview(block, dragged.current.blockIds, nativeSetDragImage,
          { x: location.current.input.clientX, y: location.current.input.clientY });
      },
      onDragStart: () => {
        blockOf()?.setAttribute("data-dragging", "true");
        marked = markDropLayers(handle);
        // No caret blinks while a block is dragged, as Notion's.
        if (document.activeElement instanceof HTMLElement) document.activeElement.blur();
      },
      onDrop: () => {
        blockOf()?.removeAttribute("data-dragging");
        for (const layer of marked) layer.removeAttribute("data-block-dragging");
        marked = [];
      },
    }, { carry: () => { const block = blockOf(); return block ? carriedBlocks(block, dragged.current.blockIds) : null; } });
  }, [handleRef, readOnly]);
}
