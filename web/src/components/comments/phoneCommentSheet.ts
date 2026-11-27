import { type RefObject, useLayoutEffect, useState } from "react";
import { bringLineOver } from "@/lib/phoneLineInSight";

/**
 * Where a phone's comment sheet (CommentsPanel's `sheet`) stands: its foot
 * at the foot of what shows of the screen — over the soft keyboard while that
 * is up, in Android Chrome too — and its top rising as its words take more
 * lines. It is laid on the page rather than fixed to the screen
 * (phoneBarGround.ts), so its place is the page's, where what shows ends.
 * Null until it is measured.
 *
 * The window stays where it was while the sheet is open, so the line
 * commented on stays in sight in the dim page: iPhone Safari scrolls the
 * window up to the field the sheet focuses though it shows over the keyboard
 * (seen 2026-11-11: 168px, the top bar and the lines under it out of sight),
 * and is put back. The page under it is scrolled as far as that line needs to
 * stand 10 over the sheet, as over the editing tools, from where they stood
 * as the sheet came, and as its words take more lines (2026-11-11: a line
 * right over the tools on the keyboard was under the sheet's top, 25px in
 * iPhone Safari and 18px in Android Chrome). Notion's sheet stands at the
 * page's foot, behind the keyboard, and Safari scrolls the line out of sight
 * to show it.
 */
export function usePhoneSheetTop(ref: RefObject<HTMLElement | null>, on: boolean, blockId?: string | null) {
  const [top, setTop] = useState<number | null>(null);
  useLayoutEffect(() => {
    if (!on) { setTop(null); return undefined; }
    const viewport = window.visualViewport, held = window.scrollY, found = lineCommentedOn(blockId);
    const line = found && !ref.current?.contains(found) ? found : null;
    const hold = () => { if (window.scrollY !== held) window.scrollTo(window.scrollX, held); };
    window.addEventListener("scroll", hold);
    let frame = 0;
    const place = () => {
      const sheet = ref.current;
      if (!sheet) return;
      const foot = viewport ? viewport.pageTop + viewport.height : window.scrollY + window.innerHeight;
      setTop(foot - sheet.offsetHeight);
      if (!line?.isConnected) return;
      const box = line.getBoundingClientRect();
      bringLineOver(line, box.bottom, foot - window.scrollY - sheet.offsetHeight, { top: box.top });
    };
    const later = () => { window.cancelAnimationFrame(frame); frame = window.requestAnimationFrame(place); };
    place();
    const resized = new ResizeObserver(later);
    if (ref.current) resized.observe(ref.current);
    viewport?.addEventListener("resize", later); viewport?.addEventListener("scroll", later);
    window.addEventListener("resize", later);
    return () => {
      window.cancelAnimationFrame(frame); resized.disconnect(); window.removeEventListener("scroll", hold);
      viewport?.removeEventListener("resize", later); viewport?.removeEventListener("scroll", later);
      window.removeEventListener("resize", later);
    };
  }, [on, ref, blockId]);
  return top;
}

// What the sheet comments on, as it opens: the text of the line the caret is
// in (the tools' comment in a line or the title), or of the block the
// comment is on (its "댓글", the caret elsewhere or nowhere).
function lineCommentedOn(blockId?: string | null): HTMLElement | null {
  const at = window.getSelection()?.focusNode;
  const node = at instanceof Element ? at : at?.parentElement;
  const field = node?.closest<HTMLElement>('[contenteditable="true"]');
  const block = blockId ? document.querySelector<HTMLElement>(`[data-block-id="${CSS.escape(blockId)}"]`) : null;
  if (node && field && (!block || block.contains(node))) return node.closest<HTMLElement>(LINE_TEXT) ?? field;
  return block && (block.querySelector<HTMLElement>(LINE_TEXT) ?? block);
}

const LINE_TEXT = "[data-native-text], [data-native-code], [data-native-caption]";
