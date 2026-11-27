import { useEffect, useState } from "react";
import { shownBlockElement } from "@/lib/shownBlockCopy";

/**
 * The text of a block of the page as the page shows it now, following its
 * edits; empty while the block is not in the page.
 */
export function useShownBlockText(blockId: string | null | undefined) {
  const [text, setText] = useState("");
  useEffect(() => {
    const block = blockId ? shownBlockElement(blockId) : null;
    const read = () => setText((block?.querySelector("[data-rt-editable]")?.textContent ?? "").trim());
    read();
    if (!block) return;
    const observer = new MutationObserver(read);
    observer.observe(block, { characterData: true, childList: true, subtree: true });
    return () => observer.disconnect();
  }, [blockId]);
  return text;
}
