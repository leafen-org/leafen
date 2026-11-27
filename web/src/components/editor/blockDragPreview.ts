import { setCustomNativeDragPreview } from "@atlaskit/pragmatic-drag-and-drop/element/set-custom-native-drag-preview";

type NativeSetDragImage = Parameters<typeof setCustomNativeDragPreview>[0]["nativeSetDragImage"];

/**
 * What a drag of blocks shows under the pointer, as Notion's (seen
 * 2026-11-07): a copy of the dragged blocks at 40% opacity that keeps where
 * they stood from the pointer (from the handle, the copy sits right of it),
 * while the blocks themselves stay as they are; a database shows its views
 * without its title row and toolbar, which leave their room empty. Without it
 * the browser shows only the handle. This is the browser's drag (a finger's);
 * a mouse's carries carriedBlocks.
 */
export function showBlockDragPreview(block: HTMLElement, blockIds: readonly string[], nativeSetDragImage: NativeSetDragImage, pointer: { x: number; y: number }) {
  const box = block.getBoundingClientRect();
  // A drag image holds the pointer: room is left of the copy for a pointer left of the blocks.
  const room = Math.max(0, box.left - pointer.x);
  setCustomNativeDragPreview({
    nativeSetDragImage,
    getOffset: () => ({ x: Math.max(0, pointer.x - box.left), y: Math.max(0, pointer.y - box.top) }),
    render: ({ container }) => { container.append(blocksCopy(block, blockIds, room)); },
  });
}

/**
 * The same copy for a drag the pointer's own events follow (pointerDrag):
 * laid over the dragged blocks where they stand, for the drag to move with
 * the pointer and take away.
 */
export function carriedBlocks(block: HTMLElement, blockIds: readonly string[]) {
  const box = block.getBoundingClientRect();
  if (!block.isConnected || box.width === 0) return null;
  const copy = blocksCopy(block, blockIds, 0);
  Object.assign(copy.style, { position: "fixed", left: `${box.left}px`, top: `${box.top}px`, margin: "0", zIndex: "2147483647" });
  copy.setAttribute("aria-hidden", "true");
  copy.inert = true;
  copy.dataset.dragCopy = "";
  block.ownerDocument.body.append(copy);
  return copy;
}

function blocksCopy(block: HTMLElement, blockIds: readonly string[], room: number) {
  const box = block.getBoundingClientRect();
  const root = block.closest("[data-editor-drop-layer]") ?? block.ownerDocument;
  const dragged = blockIds.map((id) => root.querySelector<HTMLElement>(`[data-block-id="${CSS.escape(id)}"]`))
    .filter((element): element is HTMLElement => element !== null);
  const shown = dragged.includes(block) ? dragged : [block];
  const style = window.getComputedStyle(block);
  const preview = block.ownerDocument.createElement("div");
  Object.assign(preview.style, {
    boxSizing: "content-box", paddingInlineStart: `${room}px`, width: `${box.width}px`, opacity: "0.4",
    font: style.font, color: style.color, direction: style.direction, pointerEvents: "none",
  });
  for (const element of shown) {
    const copy = element.cloneNode(true) as HTMLElement;
    // The copy is a picture: nothing in it is a block, a field or chosen.
    for (const node of [copy, ...copy.querySelectorAll<HTMLElement>("*")]) {
      node.removeAttribute("data-block-id");
      node.removeAttribute("id");
      if (node.hasAttribute("contenteditable")) node.setAttribute("contenteditable", "false");
      node.removeAttribute("data-selected");
    }
    for (const chrome of copy.querySelectorAll<HTMLElement>("[data-database-chrome]")) chrome.style.visibility = "hidden";
    copy.style.width = `${element.getBoundingClientRect().width}px`;
    preview.append(copy);
  }
  return preview;
}
