import { useRef } from "react";
import { absolutePageUrl } from "@/lib/navigation";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { placeAround, popoverPlacementStyle, popoverRectAt, usePopoverPlacement } from "@/lib/popoverPlacement";
import { useOptionalProductToast } from "@/lib/productToast";
import { replaceSelectedBlocks } from "./blockClipboard";
import { blockAnchorId } from "./blockHandleModel";
import { blockItemText } from "./blockItemLabels";
import { choosePageBlocks } from "./pageUndo";
import { pasteSyncedCopy } from "./pastedSyncedCopy";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import type { SlashMenuAnchor } from "./SlashMenu";
import type { PastedBlocksMenuRequest } from "./textBlockClipboardModel";
import styles from "./editorStyles.generated.module.css";

/**
 * What blocks pasted from another page can be instead, as Notion's (seen
 * 2026-11-05): kept as pasted, a synced copy of them, their original page
 * becoming the synced block's original there, or a link to where they came
 * from.
 */
export function PastedBlocksMenu({ adapter, request, anchor, onClose, run = change => change() }: {
  adapter: NativeEditorGraphAdapter;
  request: PastedBlocksMenuRequest;
  anchor: SlashMenuAnchor;
  onClose: (refocus: boolean) => void;
  run?(change: () => void): void;
}) {
  const menuRef = useRef<HTMLDivElement>(null);
  const toast = useOptionalProductToast();
  const placement = usePopoverPlacement(menuRef, (size, viewport) => placeAround(popoverRectAt(anchor), size, viewport), [anchor], undefined, false);
  // A menu at the caret: a press beside it goes on to the page whole (pressGoesOn), as beside the ":" menu (Notion's
  // own paste menu not brought up in Chrome, 2026-11-26).
  const layer = useOverlayLayer(true, { kind: "caret-menu", ref: menuRef, onEscape: () => onClose(true), onOutsidePress: () => onClose(false) });
  const { blockIds, source } = request;
  const pasted = () => !adapter.readOnly && adapter.isCurrent() && !adapter.signal.aborted
    && blockIds.every(id => adapter.graph.blocks.some(block => block.id === id));

  function pageLink() {
    onClose(false);
    if (!pasted()) return;
    const url = new URL(absolutePageUrl(source.pageId));
    url.hash = blockAnchorId(source.blockIds[0]!);
    const link = url.toString();
    let line: string | null = null;
    run(() => { line = replaceSelectedBlocks(adapter, new Set(blockIds), [{ type: "paragraph", content: { rich: [{ text: link, link }] }, plainText: link }]); });
    // The link's line stays chosen, as the pasted blocks were (Notion's, seen 2026-11-05).
    if (line) choosePageBlocks(adapter.pageId, [line]);
  }

  async function pasteAndSync() {
    onClose(false);
    const made = await pasteSyncedCopy(adapter, source, blockIds, pasted, run).catch(() => "failed" as const);
    if (made === "failed") toast?.notify(blockItemText("pastedBlocks.syncFailed"), "error");
    // The blocks stay as pasted, and the words say why and when it can be tried again.
    if (made === "unreachable") toast?.notify(blockItemText("pastedBlocks.syncUnreachable"), "error");
  }

  const actions = [
    { id: "paste", label: blockItemText("pastedBlocks.paste"), choose: () => onClose(true) },
    { id: "sync", label: blockItemText("pastedBlocks.pasteAndSync"), choose: () => void pasteAndSync() },
    { id: "link", label: blockItemText("pastedBlocks.pageLink"), choose: pageLink },
  ];
  const picker = useNativeMenu({ items: actions, open: true, menuRef, label: blockItemText("pastedBlocks.options"),
    keyOf: item => item.id, textOf: item => item.label, onAction: item => item.choose(),
    // Its first choice is the one already made, ready for Enter (Notion's, seen 2026-11-05),
    // shaded and never ringed as a pasted link's (PastedLinkMenu).
    menuOptions: { autoFocus: "first" } });
  return (
    <div {...picker.menuProps} ref={menuRef} className={`${styles.pastedUrlMenu} ${styles.pastedBlocksMenu}`} data-ringless="true"
      style={{ ...popoverPlacementStyle(placement), zIndex: layer.zIndex }} contentEditable={false}
      onMouseDown={(event) => event.preventDefault()} onKeyDown={event => {
        picker.menuProps.onKeyDown?.(event); layer.onKeyDown(event); event.stopPropagation();
      }}>
      {actions.map(item => <NativeMenuItem key={item.id} state={picker.state} itemKey={item.id} data-pasted-blocks-option={item.id}>
        {({ labelProps }) => <span {...labelProps}>{item.label}</span>}
      </NativeMenuItem>)}
    </div>
  );
}
