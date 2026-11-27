import { optionKeyName } from "@/lib/keyboardShortcuts";
import { useEffect, useRef, useState, type RefObject } from "react";
import { flushSync } from "react-dom";
import { i18next } from "@/i18n";
import { DragHandleIcon, Plus } from "@/icons/leafen";
import type { GraphBlock } from "@/contentGraph";
import { ActiveBlockHandle as HandleWithMenus, type BlockHandleStart } from "./BlockHandle";
import { useEditorBlockCommand } from "./editorCommands";
import type { EditorSelection, NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { useNativeTiptapUi } from "./nativeTiptapReactContext";
import { useHandlesAwayUnderCaretMenu, useHandlesDrawn } from "./drawnHandles";
import styles from "./editorStyles.generated.module.css";

// A block's handle draws its two gutter buttons while its row draws them
// (drawnHandles: the pointer at its height, or the focus in it) and stays
// asleep until it is wanted: the pointer reaching the gutter, a button
// pressed, or a command for its block. Only then does it take up its menus,
// drag and shortcuts (ActiveBlockHandle), doing what woke it. Every block of
// a long page set all of that up as the page opened (2026-10-16: a
// 3,000-block page spent most of a second on its handles). Its buttons are
// out of the Tab order, as Notion's: the keyboard opens a block's menu with
// ⌘/ or the menu key (useBlockSelectionKeys).

export function BlockHandle({ row, ...props }: {
  adapter: NativeEditorGraphAdapter;
  block: GraphBlock;
  selection: EditorSelection;
  /** Its block's row, which says whether it draws its buttons. */
  row: RefObject<HTMLElement | null>;
  /** Selects the block, unless it is already among the selected. */
  onSelect?: () => void;
  /** Lets go of the selected blocks. */
  onRelease?: () => void;
}) {
  const [start, setStart] = useState<BlockHandleStart | null>(null);
  return start ? <ActiveBlockHandle {...props} row={row} start={start} />
    : <SleepingBlockHandle blockId={props.block.id} row={row} onWake={setStart} />;
}

// The awake handle follows the editor's selection as it changes, for its
// menu's count; a sleeping one isn't told (nativeTiptapLiveState.ts). It
// shows nothing while a menu at the caret is open, as a sleeping one draws
// nothing then (drawnHandles.ts).
function ActiveBlockHandle({ row, ...props }: Parameters<typeof HandleWithMenus>[0] & { row: RefObject<HTMLElement | null> }) {
  const selection = useNativeTiptapUi(state => state.selection, props.selection);
  useHandlesAwayUnderCaretMenu(row);
  return <HandleWithMenus {...props} selection={selection} />;
}

function SleepingBlockHandle({ blockId, row, onWake }: { blockId: string; row: RefObject<HTMLElement | null>; onWake: (start: BlockHandleStart) => void }) {
  const gutter = useRef<HTMLDivElement>(null);
  const hoverWake = useRef<number | null>(null);
  const drawn = useHandlesDrawn(row);
  // A command finds its block by the row while the buttons are not drawn.
  useEditorBlockCommand(blockId, (command, anchor) => onWake({ kind: "command", command, anchor }), () => gutter.current ?? row.current);
  // The pointer wakes it a frame after it arrives, at once. A press in that
  // frame keeps it asleep for its click, which wakes it doing what was
  // pressed: woken under a press, its buttons would be new ones and the click
  // lost (2026-10-31, a click as the pointer reached the handle opened
  // nothing). Put away before that frame, it stays asleep.
  const cancelHoverWake = () => {
    if (hoverWake.current === null) return;
    window.cancelAnimationFrame(hoverWake.current);
    hoverWake.current = null;
  };
  useEffect(() => {
    if (drawn || hoverWake.current === null) return;
    window.cancelAnimationFrame(hoverWake.current);
    hoverWake.current = null;
  }, [drawn]);
  useEffect(() => () => { if (hoverWake.current !== null) window.cancelAnimationFrame(hoverWake.current); }, []);
  if (!drawn) return null;
  // A tap wakes it by its click: one woken on touch would be gone under the finger.
  return (
    <div ref={gutter} className={styles.gutter} contentEditable={false} data-resting onPointerDownCapture={cancelHoverWake}
      onPointerEnter={(event) => {
        if (event.pointerType !== "mouse" || hoverWake.current !== null) return;
        hoverWake.current = window.requestAnimationFrame(() => { hoverWake.current = null; flushSync(() => onWake({ kind: "hover" })); });
      }}>
      <button type="button" tabIndex={-1} className={styles.gutterBtn} aria-label={i18next.t("blockHandle:buttons.addBlockBelow", { key: optionKeyName() })}
        onClick={(event) => onWake({ kind: "add", above: event.altKey })}><Plus size="regular" aria-hidden="true" /></button>
      <button type="button" tabIndex={-1} className={styles.gutterBtn} aria-label={i18next.t("blockHandle:buttons.dragHint")}
        aria-haspopup="menu" aria-expanded={false} data-native-drag-source
        onClick={() => onWake({ kind: "menu" })}><DragHandleIcon size="regular" aria-hidden="true" /></button>
    </div>
  );
}
