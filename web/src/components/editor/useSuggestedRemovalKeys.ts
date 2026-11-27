import { useEffect, useRef, type Dispatch, type RefObject, type SetStateAction } from "react";
import { arrowBlockSelection, selectedRootsInOrder, type BlockSelectionWalk } from "./blockSelectionKeyboard";
import { chosenPlace, placeChoice } from "./blockOccurrences";
import { editorOwnsKeyboard } from "./editorScope";
import type { EditorSelection, NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import type { createNativeTiptapController } from "./nativeTiptapController";
import { suggestedRemovalBlocks } from "./suggestedRemoval";

function textFocused() {
  const active = document.activeElement as HTMLElement | null;
  return Boolean(active && (active.isContentEditable || active.closest("input, textarea, select, [role=dialog], [role=alertdialog], [role=menu]")));
}

/**
 * Chosen blocks while suggesting answer the keys Notion's do then (seen
 * 2026-11-18 in Chrome: a block chosen with Escape, ⇧↓ three times took in
 * the blocks under it, an empty line and a list item with a child among
 * them; a database row's side peek alike): ↑ and ↓ move the choice, ⇧↑ and
 * ⇧↓ stretch it, and Backspace or Delete suggests the chosen blocks away,
 * one suggestion for them all, the blocks staying chosen. The page's own
 * block keys take no part while suggesting (useBlockSelectionKeys).
 */
export function useSuggestedRemovalKeys({ adapter, rootRef, nested, rootBlockId, selectionRef, setSelection, engine, suggesting }: {
  adapter: NativeEditorGraphAdapter;
  rootRef: RefObject<HTMLDivElement | null>;
  nested: boolean;
  rootBlockId: string | null;
  selectionRef: RefObject<EditorSelection>;
  setSelection: Dispatch<SetStateAction<EditorSelection>>;
  engine?: ReturnType<typeof createNativeTiptapController>;
  suggesting: boolean;
}) {
  // The ends of the choice the arrows last made, while it is still the choice.
  const walk = useRef<{ ends: BlockSelectionWalk | null; selection: EditorSelection } | null>(null);
  useEffect(() => {
    if (!engine || !suggesting) return;
    const core = engine;
    function onKeyDown(event: KeyboardEvent) {
      const arrow = event.key === "ArrowUp" || event.key === "ArrowDown";
      if ((!arrow && event.key !== "Backspace" && event.key !== "Delete") || event.defaultPrevented || event.metaKey || event.ctrlKey
        || event.altKey || !adapter.isCurrent() || !editorOwnsKeyboard(rootRef.current, nested)) return;
      const chosen = selectionRef.current;
      if (!chosen?.size || textFocused()) return;
      event.preventDefault();
      if (arrow) {
        const moved = arrowBlockSelection(adapter.graph, chosen, event.key === "ArrowUp" ? "up" : "down", event.shiftKey, rootBlockId,
          walk.current?.selection === chosen ? walk.current.ends : null, { pageId: adapter.pageId, at: (id) => chosenPlace(chosen, id) });
        const next = placeChoice(moved.selection, moved.places);
        walk.current = { ends: moved.walk, selection: next };
        setSelection(next);
        return;
      }
      const ids = suggestedRemovalBlocks(selectedRootsInOrder(adapter.graph, chosen, rootBlockId));
      if (ids.length) core.suggestRemoval(ids);
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [adapter, engine, nested, rootBlockId, rootRef, selectionRef, setSelection, suggesting]);
}
