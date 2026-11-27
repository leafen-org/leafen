import { Extension, selectionToInsertionEnd } from "@tiptap/core";
import { Fragment, Slice, type Node, type Schema } from "@tiptap/pm/model";
import { Plugin, Selection, TextSelection, type EditorState } from "@tiptap/pm/state";
import type { EditorView } from "@tiptap/pm/view";
import type { GraphBlockType } from "@/contentGraph";
import { sanitizeTextSpans } from "@/lib/textOperations";
import type { TextSpan } from "@/lib/types";
import { clipboardHtmlForEngine } from "@/lib/clipboardHtml";
import { LEAFEN_WORDS_MIME, readTextWithMime } from "@/lib/clipboard";
import { EDITOR_CLIPBOARD_BOUNDS, EDITOR_PASTE_BOUNDS } from "./editorClipboardPolicy";
import { parseInternalPastedBlocks, parsePastedHtml, parsePastedMarkdown, type PastedBlock } from "./markdownPaste";
import { nativeTiptapBody } from "./nativeTiptapBody";
import { nativeTiptapInstance, newNativeTiptapAttrs } from "./nativeTiptapIdentity";
import { admitNativeTiptapClipboard, admitNativeTiptapSlice } from "./nativeTiptapPasteBudget";
import { fragmentToNativeRichText, nativeRichTextToFragment } from "./nativeTiptapInline";
import { singleLineCaptionSpans } from "./blockCaptionModel";
import { graphWireBounds } from "@/contentGraphWire.generated";
import { PASTE_CHARACTERS, firstSpanCharacters, fitsField } from "@/lib/pasteBound";
import { dispatchPastedBlocksMenu, dispatchPastedUrlMenu, pastedUrl } from "./textBlockClipboardModel";
import { internalPasteSource, type InternalPasteSource } from "./internalBlockPaste";
import { pastesServerBlocks } from "./blockClipboard";
import { readPastedLine } from "./pastedLine";
import { typedRuleAfter } from "./nativeTiptapInputRules";
import { notionCopy, pastesTextOverHtml } from "./pastedHtmlChoice";
import { LINE_KINDS, pastedLineSpans } from "./pastedHtmlLine";

function childGroup(node: Node) {
  return Array.from({ length: node.childCount }, (_, index) => node.child(index)).find(child => child.type.name === "nativeChildren");
}

function inlineFieldSlice(slice: Slice, schema: Schema, caption: boolean) {
  const parts: Fragment[] = [];
  let inlineRun: Node[] = [];
  const flush = () => { if (inlineRun.length) { parts.push(Fragment.fromArray(inlineRun)); inlineRun = []; } };
  const visit = (node: Node) => {
    if (node.isTextblock) { flush(); parts.push(node.content); }
    else if (node.isInline) inlineRun.push(node);
    else node.forEach(visit);
  };
  slice.content.forEach(visit);
  flush();
  let content = Fragment.empty;
  parts.forEach((part, index) => {
    if (index) content = content.append(Fragment.from(schema.nodes.hardBreak.create()));
    content = content.append(part);
  });
  // A caption takes a paste's first 100,000 characters, as Notion's (PASTE_CHARACTERS).
  if (caption) content = nativeRichTextToFragment(firstSpanCharacters(singleLineCaptionSpans(fragmentToNativeRichText(content)), PASTE_CHARACTERS), schema);
  return new Slice(content, 0, 0);
}

/**
 * Whether the caption the selection is in holds `slice` beside its other
 * words: a caption is a rich text of graphWireBounds.textCharacters, as the
 * server bounds it; a paste it can't hold is refused.
 */
function captionTakes(state: EditorState, slice: Slice) {
  const { $from, $to } = state.selection, caption = $from.parent, start = $from.start();
  if (caption.type.name !== "nativeCaption") return true;
  const end = $to.parent === caption ? $to.pos - start : $from.pos - start;
  const around = caption.textBetween(0, $from.pos - start, "", "\ufffc") + caption.textBetween(end, caption.content.size, "", "\ufffc");
  return fitsField(around, slice.content.textBetween(0, slice.content.size, "", "\ufffc"), graphWireBounds.textCharacters);
}

/**
 * What this paste read itself goes in as this, not as "paste": Tiptap's
 * Markdown paste rules then read only what ProseMirror pastes as it is, a
 * plain line, as Notion's (seen 2026-11-05: a plain "*ii*" line pasted there
 * is italic; its own HTML "<code>*c*</code>" keeps the asterisks, which here
 * became italic again on an empty line).
 */
const READ_PASTE = "paste-read";

function pastedNode(schema: Schema, pasted: PastedBlock, instance = ""): Node {
  const kind = pasted.type === "child_page" || pasted.type === "child_database" ? "link_to_page" : pasted.type;
  // A page's copy or cut it names is the server's to make (blockClipboard.ts); the line links to the page.
  const { rich, text, copiedPage: _copied, cutPage: _cut, ...props } = (pasted.content ?? {}) as Record<string, unknown>;
  const spans = sanitizeTextSpans(rich) ?? (typeof text === "string" ? [{ text }] : pasted.plainText ? [{ text: pasted.plainText }] : []);
  const body = nativeTiptapBody({ type: kind as GraphBlockType, content: props }, spans, schema);
  const children = (pasted.children ?? []).map(child => pastedNode(schema, child, instance));
  return schema.nodes.nativeBlock.create(newNativeTiptapAttrs(kind, props, instance),
    children.length ? [...body, schema.nodes.nativeChildren.create(null, children)] : body);
}

/**
 * What a page's title hands its body (pageBodyStart.ts): the blocks at the very
 * start, above what is there. The body's only line, when empty, is the first
 * of them, as a paste onto an empty line makes it.
 */
export function pasteAtDocStart(state: EditorState, blocks: readonly PastedBlock[]) {
  const { doc, schema } = state;
  const nodes = blocks.map(block => pastedNode(schema, block));
  admitNativeTiptapSlice(new Slice(Fragment.fromArray(nodes), 0, 0));
  const only = doc.childCount === 1 ? doc.firstChild! : null;
  const tr = state.tr;
  if (only?.attrs.kind === "paragraph" && only.childCount === 1 && only.firstChild!.type.name === "nativeText" && !only.firstChild!.content.size) {
    nodes[0] = nodes[0].type.create({ ...nodes[0].attrs, id: only.attrs.id, sourceId: only.attrs.sourceId }, nodes[0].content);
    tr.replaceWith(0, doc.content.size, nodes);
  } else tr.insert(0, nodes);
  return { tr: tr.setMeta("uiEvent", READ_PASTE), blockIds: nodes.map(node => String(node.attrs.sourceId || node.attrs.id)) };
}

/** Existing native import policy creates a PM slice; PM owns paste/selection. */
export const NativeTiptapClipboard = Extension.create<{ onRefused(error: RangeError): void; onBlocksPasted?(ids: readonly string[], copied: boolean): void;
  onDatabasesPasted?(blocks: readonly PastedBlock[], afterId: string, emptyLine: boolean, source: InternalPasteSource | null): void; }>({
  name: "nativeClipboard",
  addOptions: () => ({ onRefused(error) { throw error; } }),
  addProseMirrorPlugins() {
    let refused = false, typed = false, ownWords = false, asText = false;
    const refuse = (error: RangeError) => { refused = true; this.options.onRefused(error); };
    return [new Plugin({
      // A plain line ProseMirror pasted ends as typing it would (typedRuleAfter).
      appendTransaction: (transactions, _before, state) => {
        if (!typed || !transactions.some(tr => tr.getMeta("uiEvent") === "paste")) return null;
        typed = false;
        return state.selection.empty ? typedRuleAfter(state.tr) : null;
      },
      props: {
      transformPastedHTML: html => clipboardHtmlForEngine(html, EDITOR_PASTE_BOUNDS.richtext.maxHtmlCharacters),
      handleDOMEvents: {
        paste: (view, event) => {
          refused = false; typed = false;
          if (!event.clipboardData) return false;
          try { admitNativeTiptapClipboard(event.clipboardData); }
          catch (error) {
            if (!(error instanceof RangeError)) throw error;
            event.preventDefault(); refuse(error); return true;
          }
          // Words copied over a page's lines paste back as the editor's own
          // HTML of them (nativeTiptapCopiedWords), not the lines' HTML other
          // apps take; in code, as their text.
          const code = !view.editable || view.state.selection.$from.parent.type.spec.code;
          const own = code ? "" : readTextWithMime(event.clipboardData, LEAFEN_WORDS_MIME, EDITOR_CLIPBOARD_BOUNDS);
          if (own) {
            event.preventDefault();
            ownWords = true;
            try { view.pasteHTML(own, event); } finally { ownWords = false; }
            return true;
          }
          // Outside HTML its text loses nothing of is read as that text
          // alone, every way a paste reads text (pastedHtmlChoice.ts).
          if (code || !pastesTextOverHtml(event.clipboardData)) return false;
          event.preventDefault();
          asText = true;
          try { view.pasteText(event.clipboardData.getData("text/plain"), event); } finally { asText = false; }
          return true;
        },
      },
      transformPasted: slice => {
        try { admitNativeTiptapSlice(slice); }
        catch (error) {
          if (!(error instanceof RangeError)) throw error;
          refuse(error); return Slice.empty;
        }
        const point = this.editor.state.selection.$from;
        if (point.parent.type.name === "nativeTableCell" || point.parent.type.name === "nativeCaption") {
          const field = inlineFieldSlice(slice, this.editor.schema, point.parent.type.name === "nativeCaption");
          if (captionTakes(this.editor.state, field)) return field;
          refuse(new RangeError("The caption can't hold the paste.")); return Slice.empty;
        }
        const instance = nativeTiptapInstance(point.depth > 1 ? point.node(point.depth - 1) : null);
        const clone = (node: Node): Node => {
          if (node.type.name === "nativeBlock") {
            const props = node.attrs.props;
            const reference = node.attrs.kind === "synced_block" && (props.syncedBlockId || props.sourceId);
            const content = reference ? Fragment.from(node.firstChild!) : Fragment.fromArray(Array.from({ length: node.childCount }, (_, i) => clone(node.child(i))));
            return node.type.create(newNativeTiptapAttrs(node.attrs.kind, props, instance), content, node.marks);
          }
          return node.isLeaf ? node : node.copy(Fragment.fromArray(Array.from({ length: node.childCount }, (_, i) => clone(node.child(i)))));
        };
        return new Slice(Fragment.fromArray(Array.from({ length: slice.content.childCount }, (_, i) => clone(slice.content.child(i)))), slice.openStart, slice.openEnd);
      },
      handlePaste: (view, event) => {
        if (refused) { refused = false; return true; }
        if (ownWords || !this.editor.isEditable || !event.clipboardData || view.state.selection.$from.parent.type.spec.code) return false;
        const data = event.clipboardData;
        const html = asText ? "" : data.getData("text/html"), text = data.getData("text/plain");
        // PM owns its exact native slice; transformPasted above gives copies
        // fresh native identities instead of retaining a synced occurrence ID.
        if (html.includes("data-native-")) return false;
        try {
          const point = view.state.selection.$from;
          const container = point.depth > 1 ? point.node(point.depth - 1) : null;
          const instance = nativeTiptapInstance(container);
          const internal = parseInternalPastedBlocks(data, EDITOR_PASTE_BOUNDS);
          const url = pastedUrl(text);
          // A page's link copied here pastes as its mention where the caret
          // is, the caret after it, as Notion's (seen 2026-11-07).
          const mention = url ? pageLinkMention(internal) : null;
          if (mention && point.parent.inlineContent) {
            view.dispatch(view.state.tr.replaceSelection(new Slice(nativeRichTextToFragment(mention, view.state.schema), 0, 0))
              .setMeta("uiEvent", READ_PASTE).scrollIntoView());
            return true;
          }
          // One line of outside text is read as Notion's paste reads one (pastedLine.ts).
          const field = point.parent.type.name === "nativeTableCell" || point.parent.type.name === "nativeCaption";
          const read = !internal.length && !html && !text.trimEnd().includes("\n") && point.parent.inlineContent
            ? readPastedLine(text, EDITOR_PASTE_BOUNDS, !field) : null;
          // A link alone — a bare address, or a Markdown link named by its
          // words; at the caret, words around it, a space even, make it a
          // link among words with no menu, as Notion's (seen 2026-11-11).
          const spaced = !html && view.state.selection.empty && /^\s|\s$/u.test(text.replace(/[\r\n]+$/u, ""));
          const link = !internal.length && url && !spaced ? { text: url, href: url, url } : read?.kind === "link" ? read : null;
          // It goes in at the caret in a line of text or a table's cell, at
          // its start, its end or between words, as a link whose "붙여넣기
          // 형식" menu opens (PastedLinkMenu); not in a caption or code, as
          // Notion's (seen 2026-11-11, a Markdown link's too).
          const line = point.parent.type.name === "nativeText" ? container
            : point.parent.type.name === "nativeTableCell" && point.depth > 2 ? point.node(point.depth - 2) : null;
          if (link && view.state.selection.empty && line?.type.name === "nativeBlock") {
            const at = view.state.selection.from;
            const tr = view.state.tr.replaceSelection(new Slice(nativeRichTextToFragment([{ text: link.text, link: link.href }], view.state.schema), 0, 0));
            const request = link.url ? { blockId: line.attrs.sourceId || line.attrs.id, url: link.url, text: link.text,
              from: tr.mapping.map(at, -1), to: tr.mapping.map(at, 1) } : null;
            view.dispatch(tr.setMeta("uiEvent", READ_PASTE).scrollIntoView());
            if (request) queueMicrotask(() => { if (!this.editor.isDestroyed) dispatchPastedUrlMenu(request); });
            return true;
          }
          // In a caption it is a link there, with no menu (Notion's, seen 2026-11-11).
          if (link && view.state.selection.empty && point.parent.type.name === "nativeCaption") {
            const slice = new Slice(nativeRichTextToFragment([{ text: link.text, link: link.href }], view.state.schema), 0, 0);
            if (!captionTakes(view.state, slice)) { refuse(new RangeError("The caption can't hold the paste.")); return true; }
            view.dispatch(view.state.tr.replaceSelection(slice).setMeta("uiEvent", READ_PASTE).scrollIntoView());
            return true;
          }
          // A link pasted over chosen words links them, the caret after them, as Notion's (seen 2026-11-03, a Markdown link's 2026-11-11;
          // again 2026-11-15, worker Chrome, no script before ⌘V: a letter typed next went in right after the linked word, in both).
          if (link && !view.state.selection.empty && point.sameParent(view.state.selection.$to) && point.parent.inlineContent) {
            const { from, to } = view.state.selection;
            const tr = view.state.tr.addMark(from, to, view.state.schema.marks.link.create({ href: link.href }));
            view.dispatch(tr.setSelection(TextSelection.create(tr.doc, to)).setMeta("uiEvent", READ_PASTE));
            return true;
          }
          // Words with Markdown go in where the caret is, their marks read (a
          // link too, over words of more than one line); plain words go in as
          // ProseMirror pastes them.
          if (read?.kind === "words") { typed = true; return false; }
          const spans = read?.kind === "spans" ? read.spans : read?.kind === "link" ? [{ text: read.text, link: read.href }] : null;
          if (spans) {
            const words = new Slice(nativeRichTextToFragment(spans, view.state.schema), 0, 0);
            const slice = field ? inlineFieldSlice(words, view.state.schema, point.parent.type.name === "nativeCaption") : words;
            if (!captionTakes(view.state, slice)) { refuse(new RangeError("The caption can't hold the paste.")); return true; }
            view.dispatch(view.state.tr.replaceSelection(slice).setMeta("uiEvent", READ_PASTE).scrollIntoView());
            return true;
          }
          // "---" anywhere but an empty line is words that end as typing them would.
          if (read?.kind === "divider") { if (pasteDivider(view, read.rest, instance)) return true; typed = true; return false; }
          if (field) {
            if (!internal.length) return false;
            const slice = new Slice(Fragment.fromArray(internal.map(block => pastedNode(view.state.schema, block, instance))), 0, 0);
            const words = inlineFieldSlice(slice, view.state.schema, point.parent.type.name === "nativeCaption");
            if (!captionTakes(view.state, words)) { refuse(new RangeError("The caption can't hold the paste.")); return true; }
            view.dispatch(view.state.tr.replaceSelection(words).setMeta("uiEvent", READ_PASTE).scrollIntoView());
            return true;
          }
          // Several lines read as Markdown, a plain line going on the block
          // before it, as Notion's (real ⌘V, seen 2026-11-04).
          if (!internal.length && !html && !read && !text.trimEnd().includes("\n")) return false;
          // Chosen words stay and the copy goes after their last line (chosenEnd), as Notion's (real ⌘V, seen 2026-11-04).
          if (pastesServerBlocks(internal) && this.options.onDatabasesPasted && container?.type.name === "nativeBlock") {
            const line = chosenEnd(view.state.selection).block;
            const after = line?.type.name === "nativeBlock" ? line : container;
            this.options.onDatabasesPasted(internal, String(after.attrs.sourceId || after.attrs.id),
              view.state.selection.empty && container.attrs.kind === "paragraph" && !point.parent.content.size,
              internalPasteSource(data, EDITOR_PASTE_BOUNDS));
            return true;
          }
          const blocks = internal.length ? internal : html ? parsePastedHtml(html, EDITOR_PASTE_BOUNDS, text, notionCopy(data))
            : read?.kind === "blocks" ? read.blocks : parsePastedMarkdown(text, EDITOR_PASTE_BOUNDS, { softLineBreaks: true });
          if (!blocks.length) return false;
          // Outside HTML whose text is one line goes in the line (pastedLineSpans);
          // with more lines, its blocks go after it, one paragraph too (seen
          // 2026-11-13: '<p><b>가줄</b> 나줄</p>' with '가줄↵나줄').
          const inLine = !internal.length && html && !text.trimEnd().includes("\n");
          if (inLine && blocks[0].type !== "table" && (blocks.length > 1 || blocks[0].children?.length
            || LINE_KINDS.has(blocks[0].type) && text.trim())) {
            view.dispatch(view.state.tr.replaceSelection(new Slice(nativeRichTextToFragment(pastedLineSpans(blocks), view.state.schema), 0, 0))
              .setMeta("uiEvent", READ_PASTE).scrollIntoView());
            return true;
          }
          if (inLine && blocks.length === 1 && blocks[0].type === "paragraph" && !blocks[0].children?.length) {
            const node = pastedNode(view.state.schema, blocks[0], instance);
            view.dispatch(view.state.tr.replaceSelection(new Slice(node.firstChild!.content, 0, 0))
              .setMeta("uiEvent", READ_PASTE).scrollIntoView());
          } else {
            const nodes = blocks.map(block => pastedNode(view.state.schema, block, instance));
            const { selection } = view.state;
            const point = selection.$from;
            const container = point.depth > 1 ? point.node(point.depth - 1) : null;
            const emptyLine = container?.type.name === "nativeBlock" && container.attrs.kind === "paragraph"
              && !point.parent.content.size && selection.empty;
            // Chosen words stay, in one line or over several, and the blocks
            // go after where the choice ends (chosenEnd), chosen, as Notion's
            // (seen 2026-11-13: outside lines, Markdown, an equation alone and
            // its own blocks over '둘째 글' 1 → '끝 글' 1 or within '둘째 글');
            // words copied over lines go in as words do. Undo brings the words
            // back chosen as they were (Notion's for its own blocks; for
            // outside text it chose from the page's start, a difference kept).
            const chosenStay = !selection.empty && selection instanceof TextSelection;
            const tr = view.state.tr;
            // Copies of synced blocks take an empty line's place, as Notion's
            // ⌘V of a synced block does (seen 2026-11-01).
            const syncedCopies = blocks.every(block => block.type === "synced_block"
              && typeof (block.content as Record<string, unknown> | undefined)?.syncedBlockId === "string");
            if (emptyLine && internal.length && syncedCopies && !childGroup(container)) {
              const from = point.before(point.depth - 1), fragment = Fragment.fromArray(nodes);
              tr.replaceWith(from, point.after(point.depth - 1), fragment);
              tr.setSelection(Selection.near(tr.doc.resolve(from + fragment.size), -1));
            } else if (selection.empty && !emptyLine || chosenStay) {
              // After the line, which stays whole wherever the caret is in it,
              // as Notion's (real ⌘V, seen 2026-11-04); a body with no line
              // yet (a page just made) takes them where the caret stands
              // (2026-11-10: they threw and nothing was saved).
              const { at, block } = chosenEnd(selection), fragment = Fragment.fromArray(nodes);
              tr.insert(at, fragment);
              if (!block) tr.setSelection(Selection.near(tr.doc.resolve(at + fragment.size), -1));
            } else {
              if (emptyLine) {
                const first = nodes[0];
                const oldChildren = childGroup(container), newChildren = childGroup(first);
                const children = [
                  ...(oldChildren ? Array.from({ length: oldChildren.childCount }, (_, index) => oldChildren.child(index)) : []),
                  ...(newChildren ? Array.from({ length: newChildren.childCount }, (_, index) => newChildren.child(index)) : []),
                ];
                const body = Array.from({ length: first.childCount }, (_, index) => first.child(index)).filter(child => child.type.name !== "nativeChildren");
                nodes[0] = first.type.create({ ...first.attrs, id: container.attrs.id, sourceId: container.attrs.sourceId }, children.length
                  ? [...body, view.state.schema.nodes.nativeChildren.create(null, children)] : body);
              }
              const startLen = tr.steps.length;
              tr.replaceSelection(new Slice(Fragment.fromArray(nodes), 0, 0));
              selectionToInsertionEnd(tr, startLen, -1);
            }
            // Copied blocks left chosen show themselves (choosePasted), not the caret's line.
            const copied = internal.length > 0 || (!!html && notionCopy(data) !== false), chosen = selection.empty || chosenStay;
            view.dispatch(copied && chosen ? tr.setMeta("uiEvent", READ_PASTE) : tr.setMeta("uiEvent", READ_PASTE).scrollIntoView());
            if (chosen) this.options.onBlocksPasted?.(nodes.map(node => String(node.attrs.sourceId || node.attrs.id)), copied);
            // Blocks from another page offer to sync with or link to where they came from (PastedBlocksMenu).
            const source = internal.length ? internalPasteSource(data, EDITOR_PASTE_BOUNDS) : null;
            if (source) dispatchPastedBlocksMenu({ blockIds: nodes.map(node => String(node.attrs.sourceId || node.attrs.id)), source });
          }
          return true;
        } catch (error) {
          // The caller reports refusal; no inadmissible rich payload falls
          // through into an unbounded HTML/Markdown parser.
          if (error instanceof RangeError) { this.options.onRefused(error); return true; }
          throw error;
        }
      },
      handleDrop: () => { const blocked = refused; refused = false; return blocked; },
    } })];
  },
});

/**
 * The block a paste of blocks goes after: the caret's line, or of the blocks
 * holding both ends of a choice, the one holding its end, as Notion's (seen
 * 2026-11-13: '둘째 글' 1 → its indented '끝 글' 1, or '앞 글' 1 → that
 * '끝 글' 1, went after '둘째 글' and its children; that '끝 글' 1 → '다음
 * 글' 1 after '다음 글'). With no line yet, where the caret stands.
 */
function chosenEnd(selection: Selection): { at: number; block: Node | null } {
  const { $from, $to } = selection;
  let depth = $from.sharedDepth($to.pos);
  while (depth > 0 && $to.node(depth).type.name !== "nativeChildren") depth--;
  return depth < $to.depth ? { at: $to.after(depth + 1), block: $to.node(depth + 1) } : { at: $to.pos, block: null };
}

/**
 * "---" pasted on an empty line makes it a divider, a line after it taking
 * the caret, as typing it does and Notion's paste (real ⌘V, seen
 * 2026-11-11); further dashes go on that line ("----": "-"). Anywhere else
 * it goes in as words, ending as typing them would ("-—").
 */
function pasteDivider(view: EditorView, rest: string, instance: string) {
  const { selection, schema } = view.state, point = selection.$from;
  const container = point.depth > 1 ? point.node(point.depth - 1) : null;
  if (!selection.empty || container?.type.name !== "nativeBlock" || container.attrs.kind !== "paragraph"
    || point.parent.type.name !== "nativeText" || point.parent.content.size || childGroup(container)) return false;
  const divider = pastedNode(schema, { type: "divider", content: {} }, instance);
  const next = pastedNode(schema, { type: "paragraph", content: { rich: rest ? [{ text: rest }] : [] } }, instance);
  const from = point.before(point.depth - 1);
  const tr = view.state.tr.replaceWith(from, point.after(point.depth - 1), [
    divider.type.create({ ...divider.attrs, id: container.attrs.id, sourceId: container.attrs.sourceId }, divider.content), next]);
  // The caret ends the line after it, inside its block and its text.
  tr.setSelection(TextSelection.create(tr.doc, from + divider.nodeSize + next.nodeSize - 2));
  view.dispatch(tr.setMeta("uiEvent", READ_PASTE).scrollIntoView());
  return true;
}

/** The mention a page's link copy holds (pageLinkClipboard.ts): one line of
 * nothing but a page's mention. */
function pageLinkMention(blocks: readonly PastedBlock[]): TextSpan[] | null {
  if (blocks.length !== 1) return null;
  const [block] = blocks;
  const rich = (block.content as { rich?: unknown }).rich;
  if (block.type !== "paragraph" || block.children?.length || !Array.isArray(rich) || rich.length !== 1) return null;
  const [span] = rich as TextSpan[];
  return span?.mention === "page" && typeof span.pageId === "string" && span.pageId ? [span] : null;
}
