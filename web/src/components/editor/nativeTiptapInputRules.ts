import { Extension, InputRule } from "@tiptap/core";
import { Selection, TextSelection, type EditorState, type Transaction } from "@tiptap/pm/state";
import type { Node as ProseMirrorNode } from "@tiptap/pm/model";
import { contentForBlockTypeChange } from "@/lib/blockDefaults";
import type { BlockType } from "@/lib/types";
import { findInlineEquationShortcut, findInlineMarkdownShortcut, findInlineSymbolShortcut,
  findTypedAutoLinkShortcut } from "./editorInputModel";
import { blockShortcutChecked, blockShortcutForText, numberedListShortcut, numberedListShortcutTyped, toggleHeadingShortcutFor } from "./textBlockInputModel";
import { nativeTiptapSelectionContext } from "./nativeTiptapSelectionContext";
import { fragmentToNativeRichText, nativeOffsetAt, nativeRichTextToFragment, pmOffsetAt } from "./nativeTiptapInline";
import { nativeTiptapInstance, newNativeTiptapAttrs } from "./nativeTiptapIdentity";

/**
 * The number the numbered list a block follows directly comes to next, as the
 * screen counts it (numberedList.ts), or null when none does.
 */
function numberedListNextBefore(doc: ProseMirrorNode, position: number) {
  const $at = doc.resolve(position);
  let first = $at.index();
  while (first > 0 && $at.parent.child(first - 1).attrs.kind === "numbered_list_item") first--;
  if (first === $at.index()) return null;
  let number = 0;
  for (let index = first; index < $at.index(); index++) {
    const start = ($at.parent.child(index).attrs.props as { listStartIndex?: unknown } | undefined)?.listStartIndex;
    number = typeof start === "number" && Number.isSafeInteger(start) && start >= 1 ? start : number + 1;
  }
  return number + 1;
}

/**
 * The shortcut within a line's words its typed text ends with, if any. A
 * Markdown link typed stays words, as Notion's in a page's lines, a heading,
 * a database's text cell and a title (seen 2026-11-14 and 2026-11-15, "**c**"
 * turning bold beside it); a pasted one is a link (pastedLine.ts).
 */
function findInlineTypedRule(text: string) {
  return findInlineEquationShortcut(text) ?? findInlineSymbolShortcut(text) ?? findInlineMarkdownShortcut(text)
    ?? findTypedAutoLinkShortcut(text);
}

// Lines whose head takes a block's typed Markdown, turning them into that
// block, as Notion's (seen 2026-11-11: "- " in an empty heading 2 or at a
// heading's start makes a bulleted item of it, "# " a to-do's heading 1,
// "---" an empty bullet's divider); a callout's doesn't, a quote's makes it
// in the quote (quoteHeldType), and a toggle heading's only as before
// (toggleHeadingShortcutFor).
const HEADING_KINDS: ReadonlySet<string> = new Set(["heading_1", "heading_2", "heading_3", "heading_4"]);
const LINE_KINDS: ReadonlySet<string> = new Set(["paragraph", "bulleted_list_item", "numbered_list_item", "to_do", "toggle", ...HEADING_KINDS]);

/**
 * The block a quote's typed line head makes in the quote, as Notion's (seen
 * 2026-11-11: "- " in an empty quote, a bullet in it; "# " before "qw", a
 * heading 1 "qw" in it; "1. ", "[] ", "> ", "```" and "---" likewise); a
 * quote's own Markdown stays words ('" ' is "“ ").
 */
function quoteHeldType(before: string, wholeLine: boolean): BlockType | null {
  if ((before === "---" || before === "—-") && wholeLine) return "divider";
  const shortcut = blockShortcutForText(before);
  if (shortcut === "quote") return null;
  if (shortcut && shortcut !== "numbered_list_item") return shortcut;
  return numberedListShortcut(before, null) ? "numbered_list_item" : null;
}

/**
 * Makes a quote the holder of its line, as Notion's: its words, the typed
 * head gone, become the first block in it, of the kind the head makes, and
 * it keeps none of its own (EditorBlocks.module.css draws no line for them).
 * The caret goes to the start of that block's words; past a divider, to the
 * next line after the quote, or the line before it when none follows.
 */
function holdInQuote(tr: Transaction, state: EditorState, position: number, head: number, type: BlockType, before: string) {
  const { schema } = state, quote = state.doc.nodeAt(position)!, own = quote.firstChild!;
  const words = own.content.cut(pmOffsetAt(own.content, head));
  const numbered = type === "numbered_list_item" ? numberedListShortcut(before, null) : null;
  // A toggle made so is a new one, closed, as Notion's (a paragraph's "> " keeps its line open).
  const props = { ...contentForBlockTypeChange({}, { type: "paragraph", content: {} }, type),
    ...(type === "toggle" ? { collapsed: true } : {}),
    ...(type === "to_do" && blockShortcutChecked(before) ? { checked: true } : {}),
    ...(numbered?.format ? { listFormat: numbered.format } : {}),
    ...(numbered && numbered.start > 1 ? { listStartIndex: numbered.start } : {}) };
  const body = type === "code" ? schema.nodes.nativeCode.create(null, nativeRichTextToFragment(fragmentToNativeRichText(words), schema, true))
    : type === "divider" ? schema.nodes.nativeWidget.create() : schema.nodes.nativeText.create(null, words);
  const made = schema.nodes.nativeBlock.create(newNativeTiptapAttrs(type, props, nativeTiptapInstance(quote)), body);
  tr.replaceWith(position + 1, position + 1 + own.nodeSize, schema.nodes.nativeText.create());
  // The emptied words end at position + 3; the quote's blocks start there.
  const groupAt = position + 3, madeAt = groupAt + 1;
  if (tr.doc.nodeAt(position)!.lastChild!.type.name === "nativeChildren") tr.insert(madeAt, made);
  else tr.insert(groupAt, schema.nodes.nativeChildren.create(null, made));
  if (type !== "divider") { tr.setSelection(TextSelection.create(tr.doc, madeAt + 2)); return; }
  const after = position + tr.doc.nodeAt(position)!.nodeSize;
  const next = Selection.findFrom(tr.doc.resolve(after), 1, true) ?? Selection.findFrom(tr.doc.resolve(position), -1, true);
  if (next) tr.setSelection(next);
}

function typedRuleRanges(node: ProseMirrorNode, contentFrom: number) {
  return {
    at: (offset: number) => contentFrom + pmOffsetAt(node.content, offset),
    free: (start: number, end: number, ...marks: string[]) => {
      let allowed = true;
      node.nodesBetween(pmOffsetAt(node.content, start), pmOffsetAt(node.content, end), child => {
        if (child.type.name === "nativeAtom" || child.marks.some(mark => marks.includes(mark.type.name))) allowed = false;
      });
      return allowed;
    },
  };
}

/** Makes the shortcut within a line's words that ends at the caret; false when there is none to make. */
function applyInlineTypedRule(tr: Transaction, state: EditorState, node: ProseMirrorNode, contentFrom: number, before: string) {
  const { at, free } = typedRuleRanges(node, contentFrom), caret = before.length;
  const equation = findInlineEquationShortcut(before);
  const symbol = findInlineSymbolShortcut(before), mark = findInlineMarkdownShortcut(before);
  const address = findTypedAutoLinkShortcut(before);
  if (equation && free(equation.start, caret, "code")) {
    tr.replaceWith(at(equation.start), at(caret), state.schema.nodes.nativeAtom.create({
      text: equation.tex, payload: JSON.stringify({ equation: equation.tex }),
    }));
    tr.setSelection(TextSelection.create(tr.doc, at(equation.start) + 1));
  } else if (symbol && free(symbol.start, caret, "code")) {
    tr.insertText(symbol.replacement, at(symbol.start), at(caret));
  } else if (mark && free(mark.start, mark.start + mark.open.length)
    && free(caret - mark.close.length, caret)) {
    const inner = mark.start + mark.open.length, end = inner + mark.innerLength;
    tr.delete(at(end), at(caret)); tr.delete(at(mark.start), at(inner));
    const type = state.schema.marks[mark.mark === "strikethrough" ? "strike" : mark.mark];
    tr.addMark(at(mark.start), at(mark.start) + mark.innerLength, type.create());
    tr.setSelection(TextSelection.create(tr.doc, at(mark.start) + mark.innerLength)); tr.removeStoredMark(type);
  } else if (address && /^https?:\/\//iu.test(address.url) && free(address.start, address.start + address.urlLength, "code", "link")) {
    tr.addMark(at(address.start), at(address.start + address.urlLength), state.schema.marks.link.create({ href: address.url }));
  } else return false;
  return true;
}

/**
 * Has the typed shortcut that ends at the caret made after this transaction,
 * as typing the line's last character there would make it: Notion's paste of
 * a plain line ends so (real ⌘V, seen 2026-11-11 — "a--" is "a—", "# " a
 * heading on an empty line, "---" in a table's cell "-—"), the shortcut a
 * ⌘Z of its own, as typing's is. Tiptap runs it (its "applyInputRules").
 */
export function typedRuleAfter(tr: Transaction) {
  return tr.setMeta("applyInputRules", { from: tr.selection.from, text: "" });
}

/** Shared native shortcut policies run as Tiptap input rules, without DOM writes. */
export const NativeTiptapInputRules = Extension.create<{ enabled(): boolean }>({
  name: "nativeTypedRules",
  addOptions: () => ({ enabled: () => true }),
  addInputRules() {
    return [new InputRule({ undoable: false,
      find: text => {
        const inline = findInlineTypedRule(text);
        if (inline) return { index: inline.start, text: text.slice(inline.start) };
        if (blockShortcutForText(text) || numberedListShortcutTyped(text) || /^(---|—-)$/u.test(text)) return { index: 0, text };
        return null;
      },
      handler: ({ state }) => {
        if (!this.options.enabled()) return null;
        const target = nativeTiptapSelectionContext(state);
        if (!target || !state.selection.empty || target.node.type.name === "nativeCode") return null;
        const before = target.text.slice(0, target.start), tr = state.tr;
        const { at, free } = typedRuleRanges(target.node, target.contentFrom);
        const kind = target.block.container.attrs.kind, heading = HEADING_KINDS.has(kind);
        // Right under a numbered list a marker goes on with it or stays words;
        // a heading's stays words, as Notion's ("1. Intro" stays a heading).
        const numbered = LINE_KINDS.has(kind) && !heading
          ? numberedListShortcut(before, numberedListNextBefore(state.doc, target.block.contentFrom - 2)) : null;
        // "1. " counts as any typed number does, right under a list too.
        const shortcut = blockShortcutForText(before);
        // "—-" is "---" once "--" has turned into a dash, as Notion's; "> "
        // at a heading's start and "# " at a toggle's make a toggle heading.
        const held = target.field === "text" && kind === "quote" ? quoteHeldType(before, target.start === target.text.length) : null;
        const blockType = target.field !== "text" || held ? null : !LINE_KINDS.has(kind) ? toggleHeadingShortcutFor(kind, before)
          : (before === "---" || before === "—-") && target.start === target.text.length ? "divider"
            : toggleHeadingShortcutFor(kind, before)
              ?? (shortcut === "numbered_list_item" ? null : shortcut) ?? (numbered ? "numbered_list_item" : null);
        if (held && free(0, target.start)) holdInQuote(tr, state, target.block.contentFrom - 2, target.start, held, before);
        else if (blockType && free(0, target.start)) {
          const container = target.block.container, position = target.block.contentFrom - 2;
          tr.delete(at(0), at(target.start));
          const props = { ...contentForBlockTypeChange(container.attrs.props, { type: kind, content: container.attrs.props }, blockType),
            ...(blockType === "to_do" && blockShortcutChecked(before) ? { checked: true } : {}),
            ...(blockType === "numbered_list_item" && numbered?.format ? { listFormat: numbered.format } : {}),
            ...(blockType === "numbered_list_item" && numbered && !numbered.follows && numbered.start > 1 ? { listStartIndex: numbered.start } : {}) };
          tr.setNodeMarkup(position, undefined, { ...container.attrs, kind: blockType, props });
          if (blockType === "code" || blockType === "divider") {
            const bodyAt = position + 1, body = tr.doc.nodeAt(bodyAt)!;
            const next = blockType === "code" ? state.schema.nodes.nativeCode.create(null,
              nativeRichTextToFragment(fragmentToNativeRichText(body.content), state.schema, true))
              : state.schema.nodes.nativeWidget.create();
            tr.replaceWith(bodyAt, bodyAt + body.nodeSize, next);
            // As Notion's "```": the caret stays in the code it made (measured
            // 2026-11-15 in Chrome, no script in the page: the next letter typed went into it).
            if (blockType === "code") tr.setSelection(TextSelection.create(tr.doc, bodyAt + 1));
            if (blockType === "divider") {
              // As Notion's "---": an empty line follows the divider and takes the caret
              // (measured 2026-11-15 in Chrome, no script in the page: the next letter went there).
              const after = position + tr.doc.nodeAt(position)!.nodeSize;
              tr.insert(after, state.schema.nodes.nativeBlock.create(newNativeTiptapAttrs("paragraph", {}, nativeTiptapInstance(container)),
                state.schema.nodes.nativeText.create()));
              tr.setSelection(TextSelection.create(tr.doc, after + 2));
            }
          }
        } else {
          if (!applyInlineTypedRule(tr, state, target.node, target.contentFrom, before)) return null;
        }
        tr.setMeta("nativeInputRule", true);
      },
    })];
  },
});

/**
 * A property or comment field's words take the same shortcuts, as Notion's
 * (seen 2026-11-05); a page's or a database's title too.
 */
export const NativeFieldInputRules = Extension.create<{ enabled(): boolean }>({
  name: "nativeFieldTypedRules",
  addOptions: () => ({ enabled: () => false }),
  addInputRules() {
    return [new InputRule({ undoable: false,
      find: text => {
        const inline = findInlineTypedRule(text);
        return inline ? { index: inline.start, text: text.slice(inline.start) } : null;
      },
      handler: ({ state }) => {
        const field = state.doc.firstChild;
        if (!this.options.enabled() || !state.selection.empty || !field) return null;
        const caret = nativeOffsetAt(field.content, state.selection.from - 1);
        const before = fragmentToNativeRichText(field.content).map(span => span.text).join("").slice(0, caret), tr = state.tr;
        if (!applyInlineTypedRule(tr, state, field, 1, before)) return null;
        tr.setMeta("nativeInputRule", true);
      },
    })];
  },
});
