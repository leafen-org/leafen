import { useContext, useEffect, useRef, useState } from "react";
import { TextSelection, type Transaction } from "@tiptap/pm/state";
import { popoverAnchorOf, type PopoverRect } from "@/lib/popoverPlacement";
import { InlineEquationEditor } from "./InlineEquationEditor";
import { NativeTiptapReactContext, useNativeTiptapAdapter } from "./nativeTiptapReactContext";
import { showEquationDraft, withoutEquationDraft } from "./nativeTiptapEquationDraft";
import { NATIVE_EQUATION_REQUEST, type NativeEquationRequest } from "./nativeTiptapInlineUiEvents";
import { captureNativeTiptapRange, mapNativeTiptapRange, resolveNativeTiptapRange, type NativeTiptapRangeLease } from "./nativeTiptapRangeLease";

/**
 * Independent equation input never edits the contentDOM or provisional Y.Text.
 * Its piece stands in the line while its TeX is typed (nativeTiptapEquationDraft):
 * at a caret, empty; over chosen words, in their place and of them, as
 * Notion's √x turns them at once (seen 2026-09-30) — Escape or a click
 * elsewhere gives them back (2026-11-13).
 */
export function NativeTiptapInlineUi() {
  const { adapter: live, controller } = useContext(NativeTiptapReactContext)!;
  const adapter = useNativeTiptapAdapter(live);
  const editor = controller.editor;
  const [request, setRequest] = useState<(NativeTiptapRangeLease & { anchor: PopoverRect; initial: string }) | null>(null);
  const requestRef = useRef(request); requestRef.current = request;
  const latest = useRef(adapter); latest.current = adapter;
  useEffect(() => {
    function open(event: Event) {
      if (!editor.isEditable || !latest.current.isCurrent() || latest.current.readOnly) return;
      const range = (event as CustomEvent<NativeEquationRequest>).detail;
      const lease = captureNativeTiptapRange(editor.state, latest.current.collaborationDocument, range.from, range.to);
      if (!lease) return;
      const initial = editor.state.doc.textBetween(range.from, range.to, "", node => node.attrs.text ?? "");
      showEquationDraft(editor.view, { at: range.from, to: range.to, tex: initial });
      const piece = editor.view.dom.querySelector("[data-equation-draft]");
      const anchor = piece ? popoverAnchorOf(piece) : editor.view.coordsAtPos(range.from);
      setRequest({ ...lease, anchor, initial });
    }
    function map({ transaction }: { transaction: Transaction }) {
      const range = requestRef.current;
      if (range) {
        mapNativeTiptapRange(range, transaction);
        if (!resolveNativeTiptapRange(editor.state, latest.current.collaborationDocument, range)) {
          requestRef.current = null; setRequest(null);
          queueMicrotask(() => { if (!editor.isDestroyed) showEquationDraft(editor.view, null); });
        }
      }
    }
    const dom = editor.view.dom;
    dom.addEventListener(NATIVE_EQUATION_REQUEST, open); editor.on("transaction", map);
    return () => {
      dom.removeEventListener(NATIVE_EQUATION_REQUEST, open); editor.off("transaction", map);
      if (requestRef.current && !editor.isDestroyed) showEquationDraft(editor.view, null);
    };
  }, [editor]);
  useEffect(() => {
    if (!adapter.readOnly || !requestRef.current) return;
    if (!editor.isDestroyed) showEquationDraft(editor.view, null);
    requestRef.current = null; setRequest(null);
  }, [adapter.readOnly, editor]);
  if (!request || adapter.readOnly) return null;
  return <InlineEquationEditor anchor={request.anchor} initial={request.initial}
    onPreview={tex => { if (!editor.isDestroyed) showEquationDraft(editor.view, { tex }); }}
    onCancel={() => {
      requestRef.current = null; setRequest(null);
      if (editor.isDestroyed) return;
      // Closed, it leaves the line as it was and the caret after its place,
      // the words no longer chosen, as Notion's (2026-11-15, Chrome, no
      // script before the action; judged by the screen and one typed letter:
      // a word chosen, ⌘⇧E, Escape — the letter went in right after the word).
      const range = adapter.isCurrent() ? resolveNativeTiptapRange(editor.state, adapter.collaborationDocument, request) : null;
      const tr = withoutEquationDraft(editor.state.tr).setMeta("addToHistory", false);
      if (range) tr.setSelection(TextSelection.create(tr.doc, range.to));
      editor.view.dispatch(tr); editor.view.focus();
    }} onDone={tex => {
      requestRef.current = null; setRequest(null);
      if (editor.isDestroyed) return;
      const range = adapter.isCurrent() && !adapter.readOnly ? resolveNativeTiptapRange(editor.state, adapter.collaborationDocument, request) : null;
      if (!range) { showEquationDraft(editor.view, null); return; }
      controller.run(() => {
        const tr = withoutEquationDraft(editor.state.tr);
        if (tex) {
          const marks = tr.doc.resolve(range.from).marks();
          const atom = editor.schema.nodes.nativeAtom.create({ text: tex, payload: JSON.stringify({ equation: tex }) }, null, marks);
          tr.replaceWith(range.from, range.to, atom);
        } else tr.delete(range.from, range.to);
        tr.setSelection(TextSelection.create(tr.doc, range.from + (tex ? 1 : 0)));
        editor.view.dispatch(tr); editor.view.focus();
      });
    }} />;
}
