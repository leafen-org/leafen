import type { NodeViewRenderer } from "@tiptap/core";
import { ReactNodeViewRenderer } from "@tiptap/react";
import { NativeTiptapBlockView } from "./NativeTiptapBlockView";
import { NativeTiptapTextView } from "./NativeTiptapTextView";
import { NativeTiptapWidgetView } from "./NativeTiptapWidgetView";
import { NativeTiptapAtomView } from "./NativeTiptapAtomView";
import { NativeTiptapCaptionView, NativeTiptapChildrenView, NativeTiptapTableCellView, NativeTiptapTableCellsView } from "./NativeTiptapFieldView";
import { keepsOwnDrawing } from "./nativeTiptapDomRead";
import { marksItsRun } from "./nativeTiptapBlockRuns";

// What a person presses inside a widget by tag or by role: a database's
// cell, card or row drawn as role="button" as much as a button or an input.
export const OWN_PRESS = 'button, input, select, textarea, label, a[href], [role="button"], [role="link"], [role="checkbox"], '
  + '[role="switch"], [role="radio"], [role="option"], [role="menuitem"], [role="tab"], [role="slider"]';

/**
 * A press on a widget's control is the control's, as tiptap already makes a
 * press whose own element is a button or an input. Elsewhere in the widget
 * ProseMirror still selects the block. Tiptap reads only the pressed element
 * itself, so a press on a role="button" cell, or on a button's icon, selected
 * the whole widget instead — a database's text painted as selected under the
 * app's cell editor, with focus left in the page (2026-10-16).
 */
function ownPresses(renderer: NodeViewRenderer): NodeViewRenderer {
  return (props) => {
    const view = renderer(props);
    // WebKit doesn't focus a button it presses: the focus goes to the nearest
    // thing that takes it, here the page, whose caret ProseMirror reads as the
    // whole widget chosen. With the pointer moved a hair the press then drags
    // that selection and the button never gets its click (2026-11-05: "+ 새
    // 페이지" in a database just made made no row). The widget takes it itself.
    if (view.dom instanceof HTMLElement) view.dom.tabIndex = -1;
    const stopEvent = view.stopEvent?.bind(view);
    view.stopEvent = (event: Event) => {
      // A player takes the keyboard from its press, as Notion's: Space stops the
      // video instead of scrolling the page (seen 2026-11-05). The engine's caret
      // stands elsewhere then, so nothing done on the player is the engine's:
      // a paste went into the caption the caret had last been in.
      if (event.target instanceof HTMLMediaElement) return true;
      if (event.type === "mousedown" && event.target instanceof Element && view.dom instanceof Element) {
        // Anywhere in a database is the database's, as no selection starts there either: a press on
        // its header row's empty end chose nothing in Notion, and here painted its title, "속성 추가"
        // and "새 페이지" as the page's selection (2026-11-05). Anywhere in a block's own panel is
        // the panel's: a button's message box takes the caret (seen 2026-11-21, BlockFrame's select).
        const control = event.target.closest(`${OWN_PRESS}, [data-database-view], [data-block-panel]`);
        if (control && control !== view.dom && view.dom.contains(control) && !view.contentDOM?.contains(control)) return true;
      }
      // A drag inside a widget's own panel (a button's actions reordered) is the panel's: the
      // block isn't picked up and painted chosen under it (2026-11-03). So is a press there, above.
      if ((event.type.startsWith("drag") || event.type === "drop") && event.target instanceof Element && view.dom instanceof Element) {
        const panel = event.target.closest("[data-block-panel]");
        if (panel && view.dom.contains(panel)) return true;
      }
      return stopEvent ? stopEvent(event) : false;
    };
    return view;
  };
}

/**
 * WebKit, the desktop app's engine, starts its own selection on a press inside
 * a widget in an editable page, a database's title, column names and "새
 * 페이지" painted blue. A press on a control, or anywhere in a database, starts
 * no selection in any engine.
 */
function pressOnControlStartsNoSelection(event: Event) {
  const target = event.target instanceof Element ? event.target : event.target instanceof Node ? event.target.parentElement : null;
  if (!target || target.closest("input, textarea, select")) return;
  const island = target.closest("[contenteditable]");
  if (island?.getAttribute("contenteditable") !== "false" || !island.parentElement?.closest('[contenteditable="true"]')) return;
  if (target.closest(`${OWN_PRESS}, [data-database-view]`)) event.preventDefault();
}
if (typeof document !== "undefined") document.addEventListener("selectstart", pressOnControlStartsNoSelection, true);

/**
 * Each text field and container owns one engine contentDOM. The table is made
 * when an editor first asks for it: the views' components import editors that
 * import this module (a synced block holds a page's editor), so they are read
 * once all of them have loaded, whichever loads first.
 */
export function nativeTiptapNodeViews() {
  return madeNodeViews ??= makeNodeViews();
}

let madeNodeViews: ReturnType<typeof makeNodeViews> | null = null;

/** Each view keeps its own drawing in the page (keepsOwnDrawing). */
function keptDrawings<Views extends Record<string, NodeViewRenderer>>(views: Views): Views {
  return Object.fromEntries(Object.entries(views).map(([name, view]) => [name, keepsOwnDrawing(view)])) as Views;
}

const makeNodeViews = () => keptDrawings({
  nativeBlock: marksItsRun(ReactNodeViewRenderer(NativeTiptapBlockView)),
  nativeText: ReactNodeViewRenderer(NativeTiptapTextView),
  nativeCode: ReactNodeViewRenderer(NativeTiptapTextView, { contentDOMElementTag: "code" }),
  nativeWidget: ownPresses(ReactNodeViewRenderer(NativeTiptapWidgetView)),
  nativeCaption: ReactNodeViewRenderer(NativeTiptapCaptionView),
  nativeTableCells: ReactNodeViewRenderer(NativeTiptapTableCellsView),
  nativeTableCell: ReactNodeViewRenderer(NativeTiptapTableCellView),
  nativeChildren: ReactNodeViewRenderer(NativeTiptapChildrenView),
  // Atom controls own their pointer press. PM still owns keyboard selection,
  // clipboard and surrounding text; selecting the node on mousedown must not
  // replace the React click target before its date/equation menu opens.
  nativeAtom: ReactNodeViewRenderer(NativeTiptapAtomView, { stopEvent: ({ event }) =>
    ["mousedown", "mouseup", "click", "pointerdown", "pointerup"].includes(event.type) }),
});
