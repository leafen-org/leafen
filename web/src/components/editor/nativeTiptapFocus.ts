import { invalidateNativeDomSelection } from "@/lib/tiptapDomSelection";
import { Selection, TextSelection } from "@tiptap/pm/state";
import type { Node as ProseMirrorNode } from "@tiptap/pm/model";
import type { Editor } from "@tiptap/core";
import type { EditorView } from "@tiptap/pm/view";
import { childrenScope } from "./blockOccurrences";
import { projectedBlocks } from "./nativeTiptapBinding";
import { nativeOffsetAt, pmOffsetAt } from "./nativeTiptapInline";
import { nativeTiptapTextFields, type NativeTextField } from "./nativeTiptapTextFields";
import type { createNativeTiptapController } from "./nativeTiptapController";

type Controller = ReturnType<typeof createNativeTiptapController>;
type Position = "start" | "end" | "select" | number;
const views = new Map<HTMLElement, Controller>();
const independentFields = new Map<HTMLElement, Editor>();

/** A property/title view owns its input DOM and field history independently. */
export function registerIndependentTiptapFocus(element: HTMLElement, editor: Editor) {
  const roots = [...new Set([element, editor.view.dom])];
  for (const root of roots) independentFields.set(root, editor);
  const invalidate = () => invalidateNativeDomSelection(editor.view.root);
  editor.on("beforeTransaction", invalidate); editor.on("focus", invalidate); editor.on("blur", invalidate);
  return () => {
    editor.off("beforeTransaction", invalidate); editor.off("focus", invalidate); editor.off("blur", invalidate);
    for (const root of roots) if (independentFields.get(root) === editor) independentFields.delete(root);
  };
}

export function independentTiptapSelection(element: HTMLElement) {
  const editor = independentFields.get(element), field = editor?.state.doc.firstChild;
  if (!editor || editor.isDestroyed || !field?.isTextblock) return null;
  const { from, to, empty } = editor.state.selection;
  return { start: nativeOffsetAt(field.content, Math.max(0, from - 1)),
    end: nativeOffsetAt(field.content, Math.max(0, to - 1)), collapsed: empty,
    length: nativeOffsetAt(field.content, field.content.size) };
}

/** Product controls focus an engine selection rather than rewriting a DOM range. */
export function registerNativeTiptapFocus(root: HTMLElement, controller: Controller) {
  views.set(root, controller);
  return () => { if (views.get(root) === controller) views.delete(root); };
}

function candidates() {
  const active = document.activeElement;
  return [...views].filter(([root, controller]) => root.isConnected && !controller.editor.isDestroyed)
    .sort(([a], [b]) => Number(b.contains(active)) - Number(a.contains(active)));
}

function fieldElement(controller: Controller, contentFrom: number) {
  const dom = controller.editor.view.domAtPos(contentFrom).node;
  return (dom instanceof HTMLElement ? dom : dom.parentElement)?.closest<HTMLElement>("[data-rt-editable]") ?? undefined;
}

const visibleField = (controller: Controller, from: number) => Boolean(fieldElement(controller, from)?.getClientRects().length);

export function focusNativeTiptapBlock(key: string, position: Position = "end", scope?: string) {
  for (const [root, controller] of candidates()) {
    controller.sync();
    const editor = controller.editor;
    if (!editor.isEditable) continue;
    const { from } = editor.state.selection;
    const holds = (block: ReturnType<typeof projectedBlocks>[number]) => from >= block.contentFrom && from <= block.contentFrom + block.node.content.size;
    const all = projectedBlocks(editor.state.doc);
    const blocks = all.filter(block => block.nativeId === key && block.node.isTextblock && visibleField(controller, block.contentFrom));
    const current = blocks.find(holds);
    // Asked for without a place, a block drawn again in a synced copy is
    // taken in the copy the caret is in, beside the line it is on, as a line
    // the slash menu makes after it (blockOccurrences.childrenScope).
    const here = childrenScope(all.find(block => block.node.isTextblock && holds(block))?.id ?? "");
    const block = scope !== undefined ? blocks.find(block => scope ? block.id.startsWith(`${scope}/`) : block.id === block.nativeId)
      : current ?? (here ? blocks.find(block => block.id === `${here}/${block.nativeId}`) : undefined)
        ?? blocks.find(block => block.id === block.nativeId) ?? blocks[0];
    if (!block || !root.isConnected) continue;
    const offset = position === "start" || position === "select" ? 0 : position === "end" ? block.node.content.size
      : pmOffsetAt(block.node.content, Math.max(0, position));
    const focused = editor.view.hasFocus();
    editor.view.dispatch(editor.state.tr.setSelection(TextSelection.create(editor.state.doc, block.contentFrom + offset,
      position === "select" ? block.contentFrom + block.node.content.size : undefined)).scrollIntoView());
    editor.view.focus();
    // The caret comes into sight: an editor without the focus scrolls nothing,
    // the page's selection standing elsewhere till now (2026-11-14: Enter on a
    // chosen picture taller than the window left the line made under it, and
    // the caret, off screen; Notion's page scrolls to it).
    if (!focused) editor.view.dispatch(editor.state.tr.scrollIntoView());
    return true;
  }
  return false;
}

/**
 * The views of the editors standing inside an element other than `except`,
 * each in none of the others: a synced copy's own, drawing its original
 * from another page.
 */
export function nativeTiptapEditorsWithin(element: Element, except: EditorView) {
  const inside = [...views.values()].map((controller) => controller.editor)
    .filter((editor) => !editor.isDestroyed && editor.view !== except && element.contains(editor.view.dom)).map((editor) => editor.view);
  return inside.filter((view) => !inside.some((other) => other !== view && other.dom.contains(view.dom)));
}

/** The view of the editor a view stands in, the innermost; null for a page's own. */
export function nativeTiptapEditorAround(view: EditorView) {
  return [...views.values()].map((controller) => controller.editor)
    .filter((editor) => !editor.isDestroyed && editor.view !== view && editor.view.dom.contains(view.dom))
    .sort((a, b) => Number(a.view.dom.contains(b.view.dom)) - Number(b.view.dom.contains(a.view.dom)))[0]?.view ?? null;
}

/** The focus back in the view an element is in, its caret where the view's selection stands; false in none. */
export function refocusNativeTiptapView(element: Element) {
  const independent = element instanceof HTMLElement ? independentFields.get(element) : undefined;
  // A synced block's own view stands inside its page's: the innermost one holding it.
  const editor = independent ?? [...views.values()].map((controller) => controller.editor)
    .filter((editor) => !editor.isDestroyed && editor.view.dom.contains(element))
    .sort((a, b) => Number(a.view.dom.contains(b.view.dom)) - Number(b.view.dom.contains(a.view.dom)))[0];
  if (!editor || editor.isDestroyed) return false;
  editor.view.focus();
  return true;
}

/**
 * The caret at a place in a field's drawn text, through the selection of the
 * view the place is in, so the view keeps it: a caret set in the page's DOM
 * alone is put back where the view's selection stood when the view next reads
 * it (2026-11-12: a press beside a line a second after the caret sat in
 * another put it at the line's start, and 9ms later back in the other line).
 * False when the place is in no view's own text there.
 */
export function placeNativeTiptapCaretAt(field: HTMLElement, node: Node, offset: number) {
  const editor = [...independentFields.values(), ...[...views.values()].map((controller) => controller.editor)]
    .filter((editor) => !editor.isDestroyed && editor.view.dom.contains(node))
    .sort((a, b) => Number(a.view.dom.contains(b.view.dom)) - Number(b.view.dom.contains(a.view.dom)))[0];
  if (!editor?.isEditable) return false;
  let at: number;
  try { at = editor.view.posAtDOM(node, offset); } catch { return false; }
  if (at < 0 || at > editor.state.doc.content.size || !editor.state.doc.resolve(at).parent.isTextblock) return false;
  if (!field.contains(editor.view.domAtPos(at).node)) return false;
  editor.view.dispatch(editor.state.tr.setSelection(TextSelection.create(editor.state.doc, at)));
  editor.view.focus();
  return true;
}

export function nativeTiptapEditableElement(key: string, scope?: string) {
  for (const [, controller] of candidates()) {
    const block = projectedBlocks(controller.editor.state.doc).find(block => block.nativeId === key && block.node.isTextblock && visibleField(controller, block.contentFrom)
      && (scope === undefined || (scope ? block.id.startsWith(`${scope}/`) : block.id === block.nativeId)));
    if (!block) continue;
    const element = fieldElement(controller, block.contentFrom);
    if (element?.getClientRects().length) return element;
  }
}

export function nativeTiptapBlockHasFocus(key: string) {
  return candidates().some(([, controller]) => controller.editor.view.hasFocus()
    && nativeTiptapTextFields(projectedBlocks(controller.editor.state.doc)).some(field => field.block.nativeId === key
      && controller.editor.state.selection.from >= field.contentFrom
      && controller.editor.state.selection.to <= field.contentFrom + field.node.content.size));
}

export function focusNativeTiptapControl(key: string, selector: string) {
  for (const [, controller] of candidates()) {
    controller.sync();
    const editor = controller.editor;
    if (!editor.isEditable) continue;
    for (const field of nativeTiptapTextFields(projectedBlocks(editor.state.doc))) {
      if (field.block.nativeId !== key) continue;
      const element = fieldElement(controller, field.contentFrom);
      if (!element?.getClientRects().length || !element.closest(selector)) continue;
      editor.view.dispatch(editor.state.tr.setSelection(TextSelection.create(editor.state.doc,
        field.contentFrom + field.node.content.size)).scrollIntoView());
      editor.view.focus(); return true;
    }
  }
  return false;
}

const chosenForTyping = new WeakMap<Editor, { from: number; to: number }>();

/**
 * Whether a field's whole text is chosen by the app, to be typed over, rather
 * than selected by a person: no selection toolbar answers it. Any other
 * selection ends it.
 */
export function fieldChosenForTyping(editor: Editor) {
  const chosen = chosenForTyping.get(editor), { from, to } = editor.state.selection;
  if (chosen?.from === from && chosen.to === to) return true;
  chosenForTyping.delete(editor);
  return false;
}

/** Cross-surface selection stays a PM command, including native atoms. */
export function selectNativeTiptapElement(element: HTMLElement, position: Position = "end", scroll = true) {
  const independent = independentFields.get(element), field = independent?.state.doc.firstChild;
  if (independent && !independent.isDestroyed && independent.isEditable && field?.isTextblock) {
    const offset = position === "start" || position === "select" ? 0 : position === "end" ? field.content.size
      : pmOffsetAt(field.content, Math.max(0, position));
    if (position === "select") chosenForTyping.set(independent, { from: offset + 1, to: field.content.size + 1 });
    const tr = independent.state.tr.setSelection(TextSelection.create(independent.state.doc, offset + 1,
      position === "select" ? field.content.size + 1 : undefined));
    independent.view.dispatch(scroll ? tr.scrollIntoView() : tr);
    independent.view.focus(); return true;
  }
  for (const [, controller] of candidates()) {
    const editor = controller.editor;
    const field = nativeTiptapTextFields(projectedBlocks(editor.state.doc)).find(field =>
      fieldElement(controller, field.contentFrom) === element);
    if (!field) continue;
    const offset = position === "start" || position === "select" ? 0 : position === "end" ? field.node.content.size
      : pmOffsetAt(field.node.content, Math.max(0, position));
    const tr = editor.state.tr.setSelection(TextSelection.create(editor.state.doc, field.contentFrom + offset,
      position === "select" ? field.contentFrom + field.node.content.size : undefined));
    editor.view.dispatch(scroll ? tr.scrollIntoView() : tr);
    editor.view.focus(); return true;
  }
  return false;
}

/**
 * Gives an editor's field the caret now — at its start, its end, a place
 * `at` picks in its doc, or where its selection is — in view: tiptap's own
 * focus command gives the focus a frame on (its requestAnimationFrame), and
 * what was typed in that frame went where the caret had been (2026-11-23: an
 * Enter typed as ⌘⇧R's rename window showed split the page's title, the
 * window staying open with the letter after it in the page's text). Every
 * caret given to a field goes at once (scripts/check-late-field-focus.mjs).
 */
export function focusEditorNow(editor: Editor, at?: "start" | "end" | ((doc: ProseMirrorNode) => Selection)) {
  if (editor.isDestroyed) return;
  const { view } = editor;
  if (!at && view.hasFocus()) return;
  const tr = view.state.tr;
  const next = at === "start" ? Selection.atStart(tr.doc) : at === "end" ? Selection.atEnd(tr.doc) : at?.(tr.doc);
  if (next && !next.eq(view.state.selection)) tr.setSelection(next);
  view.dispatch(tr.scrollIntoView());
  view.focus();
}

/** A line of the page's view, by its projection and field, found before a change. */
export type NativeTiptapLine = { projectionId: string; field: NativeTextField };

/**
 * The line above the caret's block: the last text showing before it (a closed
 * toggle's lines do not), a first line's parent its own; null at the page's top.
 */
export function nativeTiptapLineAbove(editor: Editor): NativeTiptapLine | null {
  const fields = nativeTiptapTextFields(projectedBlocks(editor.state.doc)), { from } = editor.state.selection;
  const at = fields.findIndex(field => from >= field.contentFrom && from <= field.contentFrom + field.node.content.size);
  for (let index = at - 1; index >= 0; index--) {
    const field = fields[index];
    if (field.block.id === fields[at].block.id) continue;
    const dom = editor.view.domAtPos(field.contentFrom).node;
    if ((dom instanceof HTMLElement ? dom : dom.parentElement)?.getClientRects().length) return { projectionId: field.block.id, field: field.field };
  }
  return null;
}

/** The caret at a line's end; false when the line is gone. */
export function focusNativeTiptapLineEnd(editor: Editor, line: NativeTiptapLine) {
  const field = nativeTiptapTextFields(projectedBlocks(editor.state.doc))
    .find(field => field.block.id === line.projectionId && field.field === line.field);
  if (!field) return false;
  editor.view.dispatch(editor.state.tr.setSelection(TextSelection.create(editor.state.doc,
    field.contentFrom + field.node.content.size)).scrollIntoView());
  editor.view.focus(); return true;
}
