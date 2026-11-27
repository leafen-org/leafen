import { useEffect, useRef, type RefObject } from "react";
import { EDITOR_CLIPBOARD_BOUNDS } from "@/components/editor/editorClipboardPolicy";
import { LEAFEN_BLOCKS_MIME, readTextWithMime } from "@/lib/clipboard";
import { choiceOut } from "./viewChoiceSteps";

/**
 * Blocks pasted with an inline database's items chosen go on its page after
 * the database's block, chosen there, the items let go, as Notion's (seen
 * 2026-11-27 in Chrome, two tabs at two times: a table's and a gallery's cut
 * items pasted with another database's item chosen stood after that database
 * as sub-pages). The page takes the choice on the database's block
 * (choiceOut) and is handed the paste once it has it, as a paste on chosen
 * blocks goes after them (useBlockSelectionKeys). Only Leafen's own blocks,
 * the paste Notion was seen to take so; a full-page database's view, which
 * no page holds, takes none.
 */
export function useViewChoicePaste(root: RefObject<HTMLElement | null>, chosen: boolean, letGo: () => void) {
  const release = useRef(letGo);
  useEffect(() => { release.current = letGo; });
  useEffect(() => {
    if (!chosen) return;
    const paste = (event: ClipboardEvent) => {
      const view = root.current, data = event.clipboardData, focused = document.activeElement;
      // Not while a field of the view takes the words: a cell's, a title's, its search.
      const field = focused?.matches("textarea, input:not([type='checkbox'])") || focused?.closest("[contenteditable]")?.getAttribute("contenteditable") === "true";
      if (event.defaultPrevented || !view || !data || focused && focused !== document.body && !view.contains(focused) || field
        || !readTextWithMime(data, LEAFEN_BLOCKS_MIME, EDITOR_CLIPBOARD_BOUNDS) || !choiceOut(view, false)) return;
      event.preventDefault();
      event.stopPropagation();
      const handed = new DataTransfer();
      for (const type of data.types) if (type !== "Files") handed.setData(type, data.getData(type));
      release.current();
      // Once the page holds the database's block chosen.
      window.setTimeout(() => document.dispatchEvent(new ClipboardEvent("paste", { clipboardData: handed, bubbles: true, cancelable: true })));
    };
    document.addEventListener("paste", paste, true);
    return () => document.removeEventListener("paste", paste, true);
  }, [root, chosen]);
}
