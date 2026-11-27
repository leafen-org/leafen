import { Editor, Extension, Node as TiptapNode } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { Fragment, Slice, type Node } from "@tiptap/pm/model";
import { Plugin, PluginKey, TextSelection } from "@tiptap/pm/state";
import { Decoration, DecorationSet } from "@tiptap/pm/view";
import { closeHistory } from "@tiptap/pm/history";
import { renderToStaticMarkup } from "react-dom/server";
import { PropertyTypeIcon } from "./PropertyTypeIcon";
import type { FormulaPiece } from "./formulaEditorModel";
import { MAX_FORMULA_EXPRESSION_CHARACTERS } from "./formulaSyntax";
import styles from "./FormulaCode.module.css";
import { formulaTokenClass } from "./formulaTokenTheme";
import { NativeTiptapLineKeys } from "../editor/nativeTiptapLineKeys";
import { NativeTiptapClipboardSelection } from "../editor/nativeTiptapClipboardSelection";
import { AndroidEnter } from "../editor/androidEnter";
import { AppleTouchPlainText } from "../editor/appleTouchPlainText";
import { CompositionEndKey } from "../editor/compositionEndKey";
import { takeDomSelection } from "@/lib/tiptapDomSelection";
import { TiptapReadOnlyMark } from "@/lib/tiptapReadOnlyMark";
import { compositionEnds, fieldKey, imeComposing } from "@/lib/fieldKeyBoundary";
import { formulaKeyEnter } from "./formulaEditorKeys";

const problemMarks = new PluginKey<DecorationSet>("formulaProblemMarks");
// A no-break space is read as a space: a browser may put one in for a typed
// space and a formula copied from a web page carries them, and a formula's
// text never means one (2026-11-11: a typed " + 1" was saved with U+00A0).
const source = (node: Node) => node.isText ? node.text!.replace(/\u00a0/gu, " ") : node.type.name === "hardBreak" ? "\n" : String(node.attrs.source);
const readFragment = (fragment: Fragment) => { let text = ""; fragment.forEach(node => { text += source(node); }); return text; };
function sourceOffset(fragment: Fragment, offset: number) {
  let result = 0;
  fragment.forEach((node, at) => { if (at < offset) result += node.isText ? Math.min(node.nodeSize, offset - at) : source(node).length; });
  return result;
}
function engineOffset(fragment: Fragment, offset: number) {
  let at = 0, result = fragment.size, found = false;
  fragment.forEach((node, position) => {
    if (found) return;
    const length = source(node).length;
    if (offset <= at + length) { result = position + (node.isText ? Math.max(0, offset - at) : offset <= at ? 0 : node.nodeSize); found = true; }
    at += length;
  });
  return result;
}

/** Formula text remains the native expression. Tokens decorate PM text and
 * property references are PM atoms; basic input, DOM and Undo are engine-owned. */
export function createFormulaInputEngine(element: HTMLElement, initial: string, options: {
  pieces(text: string): readonly FormulaPiece[];
  attributes(): Record<string, string>;
  changed(text: string, cursor: number): void;
  cursor(cursor: number): void;
  /** The caret a key found elsewhere than last heard (takeDomSelection): what the key reads of it must be in before it acts. */
  caretTaken?(cursor: number): void;
  keyDown?(event: KeyboardEvent): void;
}) {
  let disposed = false, initializing = true;
  let initialized: Editor | null = null;
  // When the text was last edited, a name being typed included.
  let editedAt = 0;
  const fragment = (text: string, schema: import("@tiptap/pm/model").Schema) => {
    const nodes: Node[] = [];
    for (const piece of options.pieces(text)) {
      if (piece.chip) nodes.push(schema.nodes.formulaProperty.create({ source: piece.text, name: piece.chip.name, type: piece.chip.type }));
      else piece.text.split("\n").forEach((line, index) => { if (index) nodes.push(schema.nodes.hardBreak.create()); if (line) nodes.push(schema.text(line)); });
    }
    return Fragment.from(nodes);
  };
  const read = () => readFragment(editor.state.doc.firstChild!.content);
  const cursor = () => sourceOffset(editor.state.doc.firstChild!.content, editor.state.selection.head - 1);
  // The syllable being composed is published too: the editor's problems and suggestions read it, as Notion's
  // ("마감" composing names 마감 at [0,2], seen 2026-11-27 with macOS's Korean input method). What a key writes
  // finds the composition ended first (fieldKeyBoundary.ts); nothing is written into the field while it runs.
  const publish = () => { if (!initializing && !disposed) options.changed(read(), cursor()); };
  // Problem marks asked for while composing, put in once it ends.
  let waitingMarks: { value: string; places: readonly { start: number; end: number; warning?: boolean }[] } | null = null;
  // What a composition finished is published before the next one starts (fieldKeyBoundary.ts), its names read again first.
  const composition = compositionEnds(() => disposed || editor.isDestroyed ? null : editor.view, () => {
    editor.view.dispatch(editor.state.tr.setMeta(normalize, true)); publish();
    const waiting = waitingMarks;
    waitingMarks = null;
    if (waiting && read() === waiting.value) editor.view.dispatch(editor.state.tr.setMeta(problemMarks, waiting.places).setMeta("addToHistory", false));
  });
  const normalize = "formula-native-tokens";
  const editor = new Editor({ element: { mount: element }, injectCSS: false, enableInputRules: false, enablePasteRules: false,
    extensions: [TiptapReadOnlyMark,
      TiptapNode.create({ name: "doc", topNode: true, content: "formulaText" }),
      // The text keeps every space typed in it whatever stylesheet the field
      // sits in: where its spaces could collapse, the browser typed a space at
      // the end as U+00A0, which the text reads as a space, and dropped that
      // space as the next letter came (2026-11-12: " + 1" saved as "+1").
      TiptapNode.create({ name: "formulaText", group: "block", content: "inline*", code: true,
        renderHTML: () => ["span", { style: "display:block;white-space:pre-wrap" }, 0] }),
      TiptapNode.create({ name: "formulaProperty", inline: true, group: "inline", atom: true, selectable: true,
        addAttributes: () => ({ source: { default: "" }, name: { default: "" }, type: { default: "rich_text" } }),
        renderHTML: ({ node }) => ["span", { "data-formula-source": node.attrs.source, class: styles.chip }, node.attrs.name],
        renderText: ({ node }) => node.attrs.source,
        addNodeView: () => ({ node }) => {
          const dom = document.createElement("span"), icon = document.createElement("span"), name = document.createElement("span");
          dom.className = styles.chip; dom.contentEditable = "false"; dom.dataset.formulaSource = node.attrs.source;
          icon.setAttribute("aria-hidden", "true");
          icon.innerHTML = renderToStaticMarkup(<PropertyTypeIcon type={node.attrs.type} size="tiny"/>);
          name.textContent = node.attrs.name; dom.append(icon, name); return { dom };
        },
      }),
      StarterKit.configure({ document: false, paragraph: false, blockquote: false, bulletList: false, orderedList: false,
        listItem: false, listKeymap: false, heading: false, codeBlock: false, horizontalRule: false, trailingNode: false,
        bold: false, italic: false, underline: false, strike: false, code: false, link: false, gapcursor: false, dropcursor: false }),
      NativeTiptapLineKeys,
      NativeTiptapClipboardSelection,
      AndroidEnter, AppleTouchPlainText, CompositionEndKey,
      Extension.create({ name: "formulaTokenProjection", addProseMirrorPlugins: () => [new Plugin({
        filterTransaction: tr => !tr.docChanged || readFragment(tr.doc.firstChild!.content).length <= MAX_FORMULA_EXPRESSION_CHARACTERS,
        appendTransaction: (transactions, _before, after) => {
          if (imeComposing(initialized?.view) || !transactions.some(tr => tr.docChanged || tr.getMeta(normalize))) return null;
          const current = after.doc.firstChild!.content, next = fragment(readFragment(current), after.schema);
          const from = current.findDiffStart(next);
          if (from === null) return null;
          const anchor = sourceOffset(current, after.selection.anchor - 1), head = sourceOffset(current, after.selection.head - 1);
          const to = current.findDiffEnd(next)!, overlap = from - Math.min(to.a, to.b);
          if (overlap > 0) { to.a += overlap; to.b += overlap; }
          const tr = after.tr.replaceWith(from + 1, to.a + 1, next.cut(from, to.b));
          tr.setSelection(TextSelection.create(tr.doc, engineOffset(next, anchor) + 1, engineOffset(next, head) + 1));
          if (transactions.every(item => item.getMeta("addToHistory") === false)) tr.setMeta("addToHistory", false);
          return tr;
        },
        props: { decorations: state => {
          const current = state.doc.firstChild!.content, decorations: Decoration[] = []; let offset = 0;
          for (const piece of options.pieces(readFragment(current))) {
            const start = offset; offset += piece.text.length;
            if (!piece.chip && piece.kind && piece.text) decorations.push(Decoration.inline(engineOffset(current, start) + 1,
              engineOffset(current, offset) + 1, { "data-token": piece.kind, class: formulaTokenClass(piece.kind) }));
          }
          return DecorationSet.create(state.doc, [...decorations, ...problemMarks.getState(state)!.find()]);
        } },
      }), new Plugin({
        // Where the formula's problems are, as the editor last named them:
        // kept through typing, a letter typed at a mark's end inside it, so
        // the text and its marks agree until the new places come.
        key: problemMarks,
        state: {
          init: () => DecorationSet.empty,
          apply: (tr, marks) => {
            const placed = tr.getMeta(problemMarks) as readonly { start: number; end: number; warning?: boolean }[] | undefined;
            if (!placed) return marks.map(tr.mapping, tr.doc);
            const current = tr.doc.firstChild!.content;
            return DecorationSet.create(tr.doc, placed.map(({ start, end, warning }) => Decoration.inline(engineOffset(current, start) + 1,
              engineOffset(current, end) + 1, warning ? { "data-formula-warning": "" } : { "data-formula-problem": "" }, { inclusiveEnd: true })));
          },
        },
      })] }),
    ], content: { type: "doc", content: [{ type: "formulaText" }] },
    editorProps: { attributes: () => ({ ...options.attributes(), "data-empty": String(initialized ? !initialized.state.doc.firstChild?.content.size : !initial) }),
      clipboardTextSerializer: slice => slice.content.textBetween(0, slice.content.size, "", node => source(node)),
      handleKeyDown: (view, event) => {
        // The input method's key is its own; Enter, Escape and Tab find the formula typed published first (fieldKeyBoundary.ts).
        if (imeComposing(view)) return false;
        const key = fieldKey(event, { publish, enter: () => formulaKeyEnter(event) });
        if (key === "ime") return false;
        // A key on the heels of a click or a caret key (End, an arrow) acts,
        // its suggestion too, where the caret is now: Enter just after End
        // put the suggestion where the formula was clicked (2026-11-13).
        const was = view.state.selection;
        takeDomSelection(view);
        if (!view.state.selection.eq(was)) options.caretTaken?.(cursor());
        options.keyDown?.(event);
        if (event.defaultPrevented) return true;
        if (key === "newline") { view.dispatch(view.state.tr.replaceSelectionWith(view.state.schema.nodes.hardBreak.create()).scrollIntoView()); return true; }
        // An Enter the list didn't take does nothing (formulaEditorKeys.ts).
        return key === "confirm";
      },
      handlePaste: (view, event) => {
        const text = (event.clipboardData?.getData("text/plain") ?? "").replace(/\r\n?/gu, "\n");
        const tr = closeHistory(view.state.tr.replaceSelection(new Slice(fragment(text, view.state.schema), 0, 0)));
        view.dispatch(tr.scrollIntoView()); view.dispatch(closeHistory(view.state.tr)); return true;
      }, handleDrop: () => true,
      handleDOMEvents: { compositionend: () => { composition.ended(); return false; } },
    }, onUpdate: publish, onSelectionUpdate: () => { if (!initializing && !disposed) options.cursor(cursor()); },
    onTransaction: ({ transaction }) => { if (transaction.docChanged && transaction.getMeta("addToHistory") !== false) editedAt = transaction.time; },
  });
  initialized = editor;
  // Initialization is a projection, not a user edit.
  editor.view.dispatch(editor.state.tr.replaceWith(1, 1, fragment(initial, editor.schema)).setMeta("addToHistory", false));
  initializing = false;
  return { editor, read,
    focus(at = read().length) { editor.view.dispatch(editor.state.tr.setSelection(TextSelection.create(editor.state.doc,
      engineOffset(editor.state.doc.firstChild!.content, at) + 1))); editor.view.focus(); },
    /**
     * A change written as typing is, only where the text differs, with the
     * caret at `at`: Undo groups it with the typing beside it. One that
     * completes the name being typed (`completing`) is Undone with that name,
     * however long the name was waited on, as Notion's suggestions are (seen
     * 2026-10-05; measured again 2026-11-15 in Chrome at 1440 with no script
     * in the page: "to", "today()" chosen, a typed "z", then ⌘Z twice leaves
     * it empty with the caret at its start).
     */
    replace(value: string, at: number, completing = false) {
      if (imeComposing(editor.view)) return;
      const current = editor.state.doc.firstChild!.content, next = fragment(value, editor.schema);
      const from = current.findDiffStart(next), tr = editor.state.tr;
      if (from !== null) {
        const to = current.findDiffEnd(next)!, overlap = from - Math.min(to.a, to.b);
        if (overlap > 0) { to.a += overlap; to.b += overlap; }
        tr.replaceWith(from + 1, to.a + 1, next.cut(from, to.b));
      }
      tr.setSelection(TextSelection.create(tr.doc, engineOffset(next, at) + 1));
      if (completing && editedAt) tr.setTime(editedAt);
      editor.view.dispatch(tr.scrollIntoView());
    },
    /** Marks where the problems of `value` are, once the field reads `value`. */
    problems(value: string, places: readonly { start?: number; end?: number; warning?: boolean }[]) {
      if (disposed || read() !== value) return;
      const marks = places.flatMap(({ start, end, warning }) => start !== undefined && end !== undefined && end > start ? [{ start, end, warning }] : []);
      if (imeComposing(editor.view)) { waitingMarks = { value, places: marks }; return; }
      waitingMarks = null;
      editor.view.dispatch(editor.state.tr.setMeta(problemMarks, marks).setMeta("addToHistory", false));
    },
    sync(value: string) {
      editor.setOptions({ editorProps: editor.options.editorProps });
      if (imeComposing(editor.view)) return;
      if (read() !== value) {
        editor.view.dispatch(closeHistory(editor.state.tr.replaceWith(1, editor.state.doc.firstChild!.content.size + 1, fragment(value, editor.schema))));
        editor.view.dispatch(closeHistory(editor.state.tr));
      } else editor.view.dispatch(editor.state.tr.setMeta(normalize, true).setMeta("addToHistory", false));
    },
    destroy() { disposed = true; composition.stop(); editor.destroy(); },
  };
}
