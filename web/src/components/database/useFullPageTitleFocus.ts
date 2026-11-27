import { useEffect, useRef, type RefObject } from "react";
import { openedInApp } from "@/lib/router";
import { openingTakesTitleCaret } from "./peekFocusReturn";
import { caretAtRowTitleEnd } from "./rowPageTitleCaret";

/**
 * A database page opened full takes the caret at its title's end only while its body is
 * empty, as Notion's (seen 2026-11-06: an empty page's "rp2|", none once it had a line);
 * a peek's always does (DatabaseRowPage). Once per page, and not over what has focus.
 * Drawn by the address the document opened at — reloaded, or a link opened in a new
 * tab — it leaves the selection empty, titled or not. Notion's, measured 2026-11-15 in
 * Chrome at 1440 with no script in the page: after ⤡ from a row's side peek a typed "z"
 * went after the title's last letter; after a reload of that page it went nowhere. A
 * touch screen's leaves it (openingTakesTitleCaret). One opened read only (`readOnly`, its
 * row read) takes none when it turns editable where it stands, as a page's (emptyPageTitleCaret.ts).
 */
export function useFullPageTitleFocus(
  wanted: boolean, readOnly: boolean, rowId: string, panelRef: RefObject<HTMLElement | null>, rootRef: RefObject<HTMLElement | null>,
) {
  const done = useRef<string | null>(null);
  useEffect(() => {
    if (readOnly && done.current !== rowId) done.current = rowId;
    if (!wanted || done.current === rowId || !openedInApp() || !openingTakesTitleCaret()) return;
    done.current = rowId;
    const active = document.activeElement;
    if (active !== document.body && active !== panelRef.current) return;
    const title = rootRef.current?.querySelector<HTMLElement>("[data-row-page-title]");
    // Once its editor is made, after the frame the page opens in.
    if (title) return caretAtRowTitleEnd(title);
  }, [wanted, readOnly, rowId, panelRef, rootRef]);
}
