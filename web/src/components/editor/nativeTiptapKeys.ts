import { Extension } from "@tiptap/core";
import { TextSelection } from "@tiptap/pm/state";
import { canSplit } from "@tiptap/pm/transform";
import type { Node as PMNode } from "@tiptap/pm/model";
import { projectedBlocks } from "./nativeTiptapBinding";
import { nativeTiptapNestingTransaction } from "./nativeTiptapStructureKeys";
import { joinBackward } from "./nativeTiptapMergeKeys";
import { blockSpec, isBlockType } from "@/lib/blockSemantics";
import { nativeTiptapInstance, newNativeTiptapAttrs } from "./nativeTiptapIdentity";
import { imeComposing } from "@/lib/fieldKeyBoundary";

/** Native block policies use PM transactions; the engine owns editable DOM. */
export const NativeTiptapKeys = Extension.create({
  name: "nativeBlockKeys", priority: 1000,
  addKeyboardShortcuts() {
    const thisEditor = this.editor;
    const split = (leaveCode = false) => {
      if (imeComposing(this.editor.view) || !this.editor.isEditable) return false;
      const { state, view } = this.editor;
      const { $from } = state.selection;
      const block = $from.depth > 0 ? $from.node($from.depth - 1) : null;
      if (block?.type.name !== "nativeBlock" || !["nativeText", "nativeCode"].includes($from.parent.type.name) || !isBlockType(block.attrs.kind)) return false;
      if ($from.parent.type.name === "nativeCode" && !leaveCode) {
        const text = $from.parent.textContent;
        const start = $from.parentOffset === 0 ? 0 : text.lastIndexOf("\n", $from.parentOffset - 1) + 1;
        const indent = text.slice(start).match(/^[\t ]*/u)?.[0] ?? "";
        view.dispatch(state.tr.insertText(`\n${indent}`).scrollIntoView());
        return true;
      }
      const policy = blockSpec(block.attrs.kind);
      const instance = nativeTiptapInstance(block);
      const tr = state.tr.deleteSelection();
      const point = tr.selection.$from;
      const position = point.before(point.depth - 1);
      // An empty last list item, to-do or toggle list under another block
      // steps out a level as it is: under a list item, a toggle, a toggle
      // heading, a quote, a callout or a line of text, as Notion's (seen
      // 2026-11-11, toggles 2026-11-14); in a column, not the last, or at the
      // top it turns to text in place, keeping what it holds. An empty toggle
      // heading, open or closed and wherever it is, turns into its heading,
      // what it held going on after it, the caret in it (Notion's, measured
      // 2026-11-15 in Chrome by keys, no script in the page: the next letter
      // typed went into the heading, open and closed alike). An empty
      // paragraph stays where it is and Enter starts another under the same
      // parent, as Notion's does under any block (seen 2026-10-31).
      const emptyStepsOut = policy.continuesOnEnter || block.attrs.kind === "toggle";
      if (!point.parent.content.size && !leaveCode && /^toggle_heading_\d$/u.test(block.attrs.kind)) {
        const node = tr.doc.nodeAt(position)!;
        const held: PMNode[] = [];
        const own: PMNode[] = [];
        node.forEach(part => { if (part.type.name === "nativeChildren") part.forEach(child => { held.push(child); }); else own.push(part); });
        const { collapsed: _collapsed, ...props } = node.attrs.props as Record<string, unknown>;
        const heading = node.type.create({ ...node.attrs, kind: block.attrs.kind.replace("toggle_", ""), props }, own);
        tr.replaceWith(position, position + node.nodeSize, [heading, ...held]);
        tr.setSelection(TextSelection.create(tr.doc, position + 2));
        view.dispatch(tr.scrollIntoView());
        return true;
      }
      if (!point.parent.content.size && !leaveCode) {
        const blocks = projectedBlocks(tr.doc);
        const current = blocks.find(candidate => candidate.id === block.attrs.id)!;
        const parent = blocks.find(candidate => candidate.id === current.projectionParentId);
        const final = blocks.filter(candidate => candidate.projectionParentId === current.projectionParentId).at(-1)?.id === current.id;
        // Enter on a callout's empty last line goes on after the callout, the
        // line going with it unless it is all the callout holds (Notion's,
        // measured 2026-11-15 in Chrome by keys, no script in the page: a
        // callout with words and one or two lines under them let the empty
        // line out, and the next letter went there; one holding only the
        // empty line was not measured).
        if (parent?.container.attrs.kind === "callout" && final && block.attrs.kind === "paragraph") {
          const lifted = nativeTiptapNestingTransaction(state, blocks, current, true);
          if (lifted) { view.dispatch(lifted); return true; }
          const $line = tr.doc.resolve(position), outer = $line.after($line.depth - 1);
          const made = block.type.create(newNativeTiptapAttrs("paragraph", {}, nativeTiptapInstance(parent.container)), state.schema.nodes.nativeText.create());
          tr.insert(outer, made);
          tr.setSelection(TextSelection.create(tr.doc, outer + 2));
          view.dispatch(tr.scrollIntoView());
          return true;
        }
        if (parent && isBlockType(parent.container.attrs.kind) && final
          && emptyStepsOut && !blockSpec(parent.container.attrs.kind).structural) {
          const lifted = nativeTiptapNestingTransaction(state, blocks, current, true);
          if (lifted) { view.dispatch(lifted); return true; }
        }
      }
      if (!point.parent.content.size && (emptyStepsOut || policy.emptyEnterExitsToParagraph)) {
        tr.setNodeMarkup(position, block.type, { ...block.attrs, kind: "paragraph", props: {} });
        view.dispatch(tr.scrollIntoView());
        return true;
      }
      // Enter at a line's start leaves an empty line above: over a toggle
      // list, open or closed, a closed toggle; over a toggle heading, plain
      // text, the caret staying at the heading's start (Notion's, measured
      // 2026-11-15 in Chrome by keys, no script in the page: the next letter
      // went before the heading's words); over a numbered item, one with its
      // start and format, so both show its number (seen 2026-11-10: "5. 5."),
      // and over a bullet one with its format (seen 2026-11-11: "▪ ▪").
      if (!leaveCode && state.selection.empty && point.parentOffset === 0 && point.parent.content.size > 0) {
        const sameKind = policy.continuesOnEnter || block.attrs.kind === "toggle";
        const kind = sameKind ? block.attrs.kind : "paragraph";
        const inserted = block.type.create(newNativeTiptapAttrs(kind, lineAboveProps(kind, block.attrs.props), instance), state.schema.nodes.nativeText.create());
        tr.insert(position, inserted);
        tr.setSelection(TextSelection.create(tr.doc, position + inserted.nodeSize + 2));
        view.dispatch(tr.scrollIntoView());
        return true;
      }
      // A callout keeps what follows inside it: Enter starts a line in the
      // callout, the rest of its text going there with the caret at its start
      // (2026-10-16: it went on after the callout). Notion's, measured
      // 2026-11-15 in Chrome by keys, no script in the page: the next letter
      // went before the moved words, inside the callout.
      if (!leaveCode && block.attrs.kind === "callout" && point.parentOffset > 0) {
        const tail = point.parent.content.cut(point.parentOffset);
        tr.delete(point.pos, point.end());
        const made = block.type.create(newNativeTiptapAttrs("paragraph", {}, instance), state.schema.nodes.nativeText.create(null, tail));
        const callout = tr.doc.nodeAt(position)!;
        const groupAt = position + 1 + callout.firstChild!.nodeSize;
        const insertedAt = groupAt + 1;
        if (callout.childCount > 1) tr.insert(insertedAt, made);
        else tr.insert(groupAt, state.schema.nodes.nativeChildren.create(null, made));
        tr.setSelection(TextSelection.create(tr.doc, insertedAt + 2));
        view.dispatch(tr.scrollIntoView());
        return true;
      }
      if (!leaveCode && policy.toggle && point.parentOffset === point.parent.content.size) {
        const closed = block.attrs.props.collapsed === true;
        // A closed toggle list goes on as another; a closed toggle heading is
        // followed by a plain line beside it, taking the caret (Notion's,
        // measured 2026-11-15 in Chrome by keys, no script in the page: the
        // next letter went into that line).
        const another = closed && block.attrs.kind === "toggle";
        const made = block.type.create(newNativeTiptapAttrs(another ? "toggle" : "paragraph", another ? { collapsed: true } : {}, instance), state.schema.nodes.nativeText.create());
        let insertedAt: number;
        if (closed) {
          insertedAt = position + block.nodeSize;
          tr.insert(insertedAt, made);
        } else {
          const groupAt = position + 1 + block.firstChild!.nodeSize;
          if (block.childCount > 1) { insertedAt = groupAt + 1; tr.insert(insertedAt, made); }
          else { insertedAt = groupAt + 1; tr.insert(groupAt, state.schema.nodes.nativeChildren.create(null, made)); }
        }
        tr.setSelection(TextSelection.create(tr.doc, insertedAt + 2));
        view.dispatch(tr.scrollIntoView());
        return true;
      }
      const kind = policy.continuesOnEnter ? block.attrs.kind : "paragraph";
      const props = kind === "to_do" ? { checked: false } : {};
      const types = [
        { type: block.type, attrs: newNativeTiptapAttrs(kind, props, instance) },
        { type: state.schema.nodes.nativeText },
      ];
      if (!canSplit(tr.doc, tr.selection.from, 2, types)) return false;
      tr.split(tr.selection.from, 2, types);
      view.dispatch(tr.scrollIntoView());
      return true;
    };
    return {
      Enter: () => split(),
      // Beside Tiptap's own ⌘⇧S, as the shortcuts' list has both. In code only
      // ⌘⇧X and the toolbar strike words through, as Notion's (seen 2026-11-05).
      "Mod-Shift-x": () => this.editor.commands.toggleStrike(),
      "Mod-Shift-s": () => this.editor.state.selection.$from.parent.type.spec.code === true,
      Tab: () => codeTab(false), "Shift-Tab": () => codeTab(true),
      "Shift-Enter": () => {
        if (this.editor.state.selection.$from.parent.type.name === "nativeCode") return split(true);
        return this.editor.commands.setHardBreak();
      },
      Backspace: () => {
        if (imeComposing(this.editor.view)) return false;
        const { selection } = this.editor.state;
        if (!(selection instanceof TextSelection) || !selection.empty || selection.$from.parentOffset !== 0) return false;
        return joinBackward(this.editor);
      },
    };
    function codeTab(backward: boolean) {
      const { state, view } = thisEditor;
      if (!thisEditor.isEditable || imeComposing(view) || state.selection.$from.parent.type.name !== "nativeCode"
        || state.selection.$from.parent !== state.selection.$to.parent) return false;
      const { $from, $to } = state.selection;
      const text = $from.parent.textContent, start = $from.start();
      const first = $from.parentOffset === 0 ? 0 : text.lastIndexOf("\n", $from.parentOffset - 1) + 1;
      // Tab indents the caret's line at its start wherever the caret stands,
      // the caret keeping its place in the words, as Notion's code block
      // (seen 2026-11-07: a tab at the caret broke a Mermaid diagram).
      const end = $to.parentOffset;
      const lines = [first];
      for (let at = text.indexOf("\n", first); at >= 0 && at < end; at = text.indexOf("\n", at + 1)) {
        if (at + 1 < end) lines.push(at + 1);
      }
      const tr = state.tr;
      for (const offset of lines.reverse()) {
        if (!backward) tr.insertText("\t", start + offset);
        else {
          const length = text[offset] === "\t" ? 1 : text.slice(offset).match(/^ {1,2}/u)?.[0].length ?? 0;
          if (length) tr.delete(start + offset, start + offset + length);
        }
      }
      view.dispatch(tr.scrollIntoView());
      return true;
    }
  },
});

/**
 * The props of the empty line of `kind` Enter leaves above a line at its
 * start, from that line's props: a to-do as checked as the line, a closed
 * toggle, a numbered item with the line's start and format, a bullet with
 * its format. A break suggested there makes the same line
 * (nativeTiptapSuggestedBreak.ts, and the server's startSplitBlockContent).
 */
export function lineAboveProps(kind: string, props: Record<string, unknown>): Record<string, unknown> {
  const { listStartIndex, listFormat } = props as { listStartIndex?: unknown; listFormat?: unknown };
  return kind === "to_do" ? { checked: props.checked === true } : kind === "toggle" ? { collapsed: true }
    : kind === "numbered_list_item" ? { ...(listStartIndex === undefined ? {} : { listStartIndex }), ...(listFormat === undefined ? {} : { listFormat }) }
    : kind === "bulleted_list_item" && listFormat !== undefined ? { listFormat } : {};
}
