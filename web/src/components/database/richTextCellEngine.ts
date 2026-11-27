import { Editor, Extension, type NodeViewRenderer } from "@tiptap/core";
import { Plugin, TextSelection } from "@tiptap/pm/state";
import { closeHistory } from "@tiptap/pm/history";
import { Fragment, Slice, type Node as ProseMirrorNode, type Schema } from "@tiptap/pm/model";
import { Decoration, DecorationSet, type EditorView } from "@tiptap/pm/view";
import type { Transaction } from "@tiptap/pm/state";
import type { TextSpan } from "@/lib/types";
import { subscribePeopleDirectory, withCurrentMentionName } from "@/lib/peopleDirectory";
import { followPageName } from "@/lib/pageNameRecords";
import { pushRoute } from "@/lib/router";
import { openPressedLink, opensLink } from "../editor/linkOpening";
import { fieldTypedSave, type FieldKept } from "./fieldTypedSave";
import { nativeInlineFieldExtensions } from "../editor/nativeTiptapSchema";
import { NativeTiptapLinkTarget } from "../editor/nativeTiptapLinkTarget";
import { NativeFieldInputRules, typedRuleAfter } from "../editor/nativeTiptapInputRules";
import { fragmentToNativeRichText, nativeOffsetAt, nativeRichTextToFragment, pmOffsetAt,
  readNativeInlineMetadata } from "../editor/nativeTiptapInline";
import { coalesce, safeUrl, spansToHtml } from "../editor/richtext";
import { showCustomEmojiPictures } from "../editor/InlineCustomEmoji";
import { CELL_RICHTEXT_BOUNDS, cellMarkShortcut, cellValueFromSpans, linkCellRange, toggleCellMark } from "./richTextCellModel";
import { registerIndependentTiptapFocus } from "../editor/nativeTiptapFocus";
import { fieldWordsTyped } from "../editor/fieldTypedSpaces"; import { typedWordsShown } from "./typedWordsShown";
import { AndroidEnter } from "../editor/androidEnter";
import { AppleTouchPlainText } from "../editor/appleTouchPlainText";
import { CompositionEndKey } from "../editor/compositionEndKey";
import { clipboardHtmlForEngine } from "@/lib/clipboardHtml";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { AutomationMentionIcon } from "../editor/AutomationMentionIcon";
import { chordLetter, primaryModifier } from "@/lib/keyboardShortcuts";
import { compositionEnds, fieldKey, imeComposing } from "@/lib/fieldKeyBoundary";
import { fieldTakes } from "@/lib/pasteBound";
import { takeDomSelection } from "@/lib/tiptapDomSelection";
import { TiptapReadOnlyMark } from "@/lib/tiptapReadOnlyMark";
import { pastedFieldSpans } from "../editor/pastedLine";
import { pastedCellLines } from "./richTextCellLines";
import { pastesTextOverHtml } from "../editor/pastedHtmlChoice";
import { EDITOR_PASTE_BOUNDS } from "../editor/editorClipboardPolicy";
import { editorHoldsPageFocus } from "../editor/editorHoldsPageFocus";
import { mergeTitleValues, sameTitle, textChanges, titleFromUnits, titleUnitText, titleUnits, type TitleValue } from "@/lib/titleMerge";

/** A value written; a title that follows (`follows`) says the value from the server it was typed on. */
export type RichTextCellChange = { value: string; spans?: TextSpan[]; typedOn?: { value: string; spans?: TextSpan[] } };
const projection = "native-property-projection";
// One value whatever shape its spans come in (sameTitle): a field left as it
// was writes nothing, its row's edited time staying.
const same = (a: RichTextCellChange, b: RichTextCellChange) => sameTitle({ title: a.value, spans: a.spans }, { title: b.value, spans: b.spans });

/** Property storage remains native. This transient Tiptap view owns all
 * basic input, DOM, selection and one local field history. */
// One space for each run of line breaks, in the stylesheet's hands: a cell
// reads "a b" for lines a, b with or without a blank line between them.
function lineSpaces(doc: ProseMirrorNode) {
  const field = doc.firstChild!, spaces: Decoration[] = [];
  field.forEach((node, offset, index) => {
    if (node.type.name === "hardBreak" && (!index || field.child(index - 1).type.name !== "hardBreak")) {
      spaces.push(Decoration.widget(offset + 1, () => {
        const space = document.createElement("span");
        space.dataset.cellLineSpace = "";
        space.textContent = " ";
        return space;
      }, { side: -1, ignoreSelection: true, key: "cell-line-space" }));
    }
  });
  return spaces.length ? DecorationSet.create(doc, spaces) : null;
}

/** Words a new comment is being written on, its draft kept for, or just sent on (CellComments). */
export type CellCommentPending = { start: number; end: number; kept: boolean; sent?: boolean };

// Those words marked as Notion's (seen 2026-11-12): a thread's yellow while
// the comment is written, fainter while its draft is put away, and once sent
// until the value brings the thread's own mark; the colors are CellCommentMarks'.
function pendingCommentWords(doc: ProseMirrorNode, pending: CellCommentPending | null | undefined) {
  if (!pending || pending.end <= pending.start) return null;
  const field = doc.firstChild!.content;
  const from = 1 + pmOffsetAt(field, pending.start), to = Math.min(1 + pmOffsetAt(field, pending.end), 1 + field.size);
  if (to <= from) return null;
  let marked = false;
  if (pending.sent) doc.nodesBetween(from, to, (node) => {
    marked ||= node.marks.some((mark) => mark.type.name === "nativeAnnotation" && String(mark.attrs.stringValue).includes("\"commentId\""));
  });
  return marked ? null : DecorationSet.create(doc, [Decoration.inline(from, to, { "data-comment-pending": pending.kept ? "kept" : "writing" })]);
}

/** A pasted slice's words, when it is one line of them. */
function pastedWords(slice: Slice) {
  const { content } = slice, only = content.childCount === 1 ? content.firstChild! : null;
  if (only?.type.name === "nativePropertyText") return only.content.size ? only.content : null;
  let words = content.size > 0;
  content.forEach(node => { words &&= node.isInline; });
  return words ? content : null;
}

/** A field's mention, equation or custom emoji, drawn as the page's text draws it. */
export const fieldAtomView: NodeViewRenderer = ({ node }) => {
  const dom = document.createElement("span");
  dom.contentEditable = "false"; dom.setAttribute("data-native-atom", "true");
  const span = { ...readNativeInlineMetadata(node.attrs.payload), text: String(node.attrs.text) };
  const draw = () => {
    dom.innerHTML = spansToHtml([withCurrentMentionName(span)], CELL_RICHTEXT_BOUNDS);
    showCustomEmojiPictures(dom);
    // An automation message's mention leads with its kind's icon, as Notion's chips (seen 2026-10-23).
    const icon = span.mention === "automation" ? dom.querySelector("[data-automation-icon]") : null;
    if (icon) icon.innerHTML = renderToStaticMarkup(createElement(AutomationMentionIcon, { token: span.automation ?? "", type: span.automationType ?? "", size: "tiny" }));
  };
  draw();
  // A person, page or database mention reads with its name now, redrawn as
  // names change: a page's head follows a rename of the page its title
  // mentions, as Notion's does within seconds (seen 2026-11-16 at 1440).
  const named = span.mention === "person" || span.mention === "page" || span.mention === "database";
  // A page's, as it is renamed or typed anywhere (lib/pageNames.ts).
  const stops = named ? [subscribePeopleDirectory(draw), ...(span.mention === "page" && span.pageId ? [followPageName(span.pageId, draw)] : [])] : [];
  return { dom, destroy: named ? () => { for (const stop of stops) stop(); } : undefined };
};

/**
 * ⌘↑ and ⌘↓ take a database cell's caret to its start and end, with Shift
 * keeping still, and ↑ and ↓ on its first and last line do so too, Shift
 * choosing to there, as Notion's cell does (seen 2026-11-12; measured once
 * 2026-11-15 in Chrome 1440 with no script before the key: in a title cell
 * pressed into, ↑ then a letter put it at the start, ⌘↓ at the end). The browser's
 * own left the cell, its editing closing: ⌘↑ and ⌘↓ for the start or end of
 * the page around an inline database (2026-11-11), ↑ for its name.
 */
function cellArrow(view: EditorView, event: KeyboardEvent) {
  if (event.ctrlKey || event.altKey || event.key !== "ArrowUp" && event.key !== "ArrowDown") return false;
  const up = event.key === "ArrowUp";
  if (!event.metaKey && !view.endOfTextblock(up ? "up" : "down")) return false;
  if (event.metaKey && event.shiftKey) return true;
  const { state } = view, to = up ? 1 : 1 + state.doc.firstChild!.content.size;
  view.dispatch(state.tr.setSelection(TextSelection.create(state.doc, event.shiftKey ? state.selection.anchor : to, to)).scrollIntoView());
  return true;
}

// A field's schema, the same for every field: known once one was made.
let fieldSchema: Schema | null = null;

// The document a field opens with its words in, as putting them in it shows
// them (replace below, with the bounds the field keeps); null to put them in
// once it is made. A field made in the page with its words already in reads
// no selection: putting them in afterwards read the browser's, which laid
// the page out in the middle of drawing it (2026-11-13: a page of three
// databases opening, each database's name 20 to 90 ms).
function openingDocument(spans: readonly TextSpan[]) {
  if (!fieldSchema || !spans.length) return null;
  const words = nativeRichTextToFragment(coalesce([...spans], CELL_RICHTEXT_BOUNDS), fieldSchema);
  let kept: boolean;
  try {
    const shown = fragmentToNativeRichText(words);
    coalesce(shown, CELL_RICHTEXT_BOUNDS);
    kept = shown.reduce((sum, span) => sum + span.text.length, 0) <= CELL_RICHTEXT_BOUNDS.maxTextCharacters;
  } catch (error) { if (error instanceof RangeError) kept = false; else throw error; }
  return { type: "doc", content: [{ type: "nativePropertyText", ...(kept && words.size ? { content: words.toJSON() } : {}) }] };
}

/**
 * The attributes a field's editor gives the element it is made in: those its
 * owner gives (`given`) and its own. A field ⇧Enter puts lines in shows them
 * while it is edited only (data-cell-breaks, DatabaseCellBasics.module.css):
 * read in a column not set to wrap, it stays on one line, as Notion's (seen
 * 2026-11-07).
 */
export function fieldElementAttributes(mount: HTMLElement, given: Record<string, string>, multiline: boolean, shiftBreaks: boolean): Record<string, string> {
  return { ...given, style: multiline || mount.tagName === "H1" ? "white-space:pre-wrap;" : "", ...(shiftBreaks ? { "data-cell-breaks": "true" } : {}) };
}

export function createRichTextCellEngine(mount: HTMLElement, initial: TextSpan[], options: {
  multiline(): boolean;
  shiftBreaks?(): boolean;
  /** A multiline field Enter still ends, its lines taken with ⇧Enter. */
  enterEnds?(): boolean;
  commitOn(): "input" | "blur";
  attributes(): Record<string, string>;
  changed(change: RichTextCellChange): void;
  updated(): void;
  linkRequested(): void;
  finished(): void;
  /** Enter, which ends a one-line field, also goes on (a title to its page's body). */
  entered?(): void;
  /** A key the field's owner takes first: true when it handled it, false to
   * leave it to the page, as a comment box sends on Enter or lets Escape close
   * what it sits in; undefined for the field's own handling. */
  keyDown?(event: KeyboardEvent): boolean | undefined;
  /** A database's cell, whose ↑ ↓ ⌘↑ ⌘↓ keep the caret in it (cellArrow). */
  cellArrows?(): boolean;
  /** What a paste puts in the field, given the text around it; null when it went elsewhere. */
  pasted(data: DataTransfer, around: string): string | null;
  /** A cell that shows its line breaks as spaces until it is edited (its stylesheet). */
  linesOnOneLine(): boolean;
  /** The field couldn't take what was pasted. */
  pasteRefused(): void;
  /** Whether its words take the shortcuts a line's do. */
  typedRules(): boolean;
  /** Whether a paste keeps its marks and Markdown though typing takes no shortcuts (typedRules does both). */
  pastedMarks?(): boolean;
  /** The most characters it holds, typing past them refused (fieldTakes); none for no bound but a change's. */
  maxCharacters?(): number | undefined;
  /** A date mention pressed: true when its owner opened it. */
  datePressed?(mention: HTMLElement): boolean;
  /** Its links open at a press while it is edited, as a page's text: a database's value (RichTextCellEditor). */
  opensLinks?(): boolean;
  /**
   * A title being written takes a value saved elsewhere at once, merged with
   * what is typed here, and says what it was typed on when it is written (see `follow`).
   */
  follows?(): boolean;
  /** Words a new comment is on, marked (pendingCommentWords). */
  commentPending?(): CellCommentPending | null | undefined;
  /** Whether its words can be written in; a field only shown (a read-only database's description) can't. */
  editable?(): boolean;
  /**
   * The words as typed, at each change and while an input method composes
   * them (`composing`), over the words last saved: a title shown wherever
   * its page is named at once (RichTextCellEditor `names`), saved as
   * `commitOn` says.
   */
  shown?(words: string, over: string, composing: boolean): void;
  /** What a field saved as it is left keeps on this device at each key, given its change as it would save it (fieldTypedSave.ts). */
  kept?(change: RichTextCellChange): FieldKept | null | undefined;
}) {
  let saved = cellValueFromSpans(initial), projected = saved, dirty = false;
  // The words `shown` named the page by, not saved yet, each in its turn: given back where the page
  // is named, at once or a render behind the keys, they are the field's own, not a value saved (sync).
  const named = typedWordsShown();
  const show = () => {
    if (!options.shown || disposed || editor.isDestroyed) return;
    const words = cellValueFromSpans(typed()).value;
    if (words !== saved.value) named.note(words);
    options.shown(words, saved.value, imeComposing(editor.view));
  };
  let pending: TextSpan[] | null = null, disposed = false;
  const attributes = () => fieldElementAttributes(mount, options.attributes(), options.multiline(), options.shiftBreaks?.() === true);
  const read = () => fragmentToNativeRichText(editor.state.doc.firstChild!.content);
  // Its words as they are saved, a space typed as U+00A0 a space (fieldTypedSpaces.ts).
  const typed = () => fragmentToNativeRichText(fieldWordsTyped(editor.state));
  const selection = () => ({ start: nativeOffsetAt(editor.state.doc.firstChild!.content, editor.state.selection.from - 1),
    end: nativeOffsetAt(editor.state.doc.firstChild!.content, editor.state.selection.to - 1) });
  const publish = () => {
    const next = cellValueFromSpans(typed());
    if (same(next, saved)) { typing.unchanged(); return false; }
    saved = next; dirty = true; named.clear(); typing.send(() => options.changed(options.follows?.() ? { ...next, typedOn: projected } : next));
    // Saved, it is the page's name: no title stands typed over it.
    show(); return true;
  };
  // A field that saves on leaving also saves what is typed meanwhile, a composition's kept too (fieldTypedSave.ts).
  const typing = fieldTypedSave(() => {
    if (!disposed && !editor.isDestroyed && !imeComposing(editor.view) && options.commitOn() === "blur") publish();
  });
  const typedAgain = () => typing.typed(options.kept?.({ ...cellValueFromSpans(typed()), typedOn: projected }));
  const update = () => {
    if (disposed) return;
    if (options.commitOn() !== "input") typedAgain();
    if (imeComposing(editor.view)) return;
    if (options.commitOn() === "input") publish();
    options.updated();
  };
  function dispatchAction(tr: Transaction) {
    editor.view.dispatch(closeHistory(tr));
    editor.view.dispatch(closeHistory(editor.state.tr));
  }
  function replace(spans: readonly TextSpan[], start: number, end = start, remote = false) {
    const next = nativeRichTextToFragment(coalesce([...spans], CELL_RICHTEXT_BOUNDS), editor.schema);
    const current = editor.state.doc.firstChild!.content, from = current.findDiffStart(next), tr = editor.state.tr;
    if (from !== null) {
      const to = current.findDiffEnd(next)!;
      const overlap = from - Math.min(to.a, to.b);
      if (overlap > 0) { to.a += overlap; to.b += overlap; }
      tr.replaceWith(from + 1, to.a + 1, next.cut(from, to.b));
    }
    tr.setSelection(TextSelection.create(tr.doc, 1 + pmOffsetAt(tr.doc.firstChild!.content, start), 1 + pmOffsetAt(tr.doc.firstChild!.content, end)));
    if (remote) tr.setMeta(projection, true).setMeta("addToHistory", false);
    if (remote) editor.view.dispatch(tr); else dispatchAction(tr);
    if (!remote) editor.view.focus();
  }
  // A title being written takes a title saved elsewhere — this person's other
  // window, another person — as it comes, caret in it or not, merged with
  // what is typed here and not saved yet, as a page's title does
  // (pageTitleFollow.ts): neither's typing is lost, and the merge is saved
  // as typing is. `followFrom` is the server's title the typing here was
  // made on; a composition (Korean, Japanese …) is left to end first.
  let followFrom: RichTextCellChange | null = null;
  const titleOf = (change: RichTextCellChange): TitleValue => ({ title: change.value, ...(change.spans ? { spans: change.spans } : {}) });
  function follow() {
    if (!followFrom || disposed || editor.isDestroyed || imeComposing(editor.view)) return;
    const base = followFrom, mine = cellValueFromSpans(typed());
    followFrom = null;
    // Change by change, a mention whole, so the caret keeps its place among the words around it.
    const before = titleUnits(titleOf(mine)), after = titleUnits(mergeTitleValues(titleOf(base), titleOf(mine), titleOf(projected)));
    const places = [1];
    for (const unit of before) { const { text, atom } = titleUnitText(unit); places.push(places[places.length - 1] + (atom ? 1 : text.length)); }
    const tr = editor.state.tr;
    for (const change of textChanges(before, after).reverse()) {
      const inserted = titleFromUnits(change.insert);
      tr.replaceWith(places[change.from], places[change.to], change.insert.length
        ? nativeRichTextToFragment(inserted.spans ?? [{ text: inserted.title }], editor.schema) : Fragment.empty);
    }
    if (tr.docChanged) editor.view.dispatch(tr.setMeta(projection, true).setMeta("addToHistory", false));
    saved = projected; pending = null;
    if (same(cellValueFromSpans(typed()), projected)) { dirty = false; return; }
    if (options.commitOn() === "input") publish();
    else typedAgain();
  }
  // What a composition finished is published before the next one starts
  // (fieldKeyBoundary.ts): a frame later, macOS Chrome's Korean was composing
  // the next syllable already and the words stopped at the last pause.
  const composition = compositionEnds(() => disposed || editor.isDestroyed ? null : editor.view, () => { update(); follow(); show(); });
  const opening = openingDocument(initial);
  const editor = new Editor({ element: { mount }, editable: options.editable?.() ?? true, extensions: [
    ...nativeInlineFieldExtensions(fieldAtomView),
    AndroidEnter, AppleTouchPlainText, CompositionEndKey, TiptapReadOnlyMark,
    NativeTiptapLinkTarget,
    NativeFieldInputRules.configure({ enabled: () => options.typedRules() }),
    Extension.create({ name: "nativePropertyLineSpaces", addProseMirrorPlugins: () => [new Plugin({ props: {
      decorations: state => options.linesOnOneLine() ? lineSpaces(state.doc) : null,
    } })] }),
    Extension.create({ name: "nativePropertyCommentPending", addProseMirrorPlugins: () => [new Plugin({ props: {
      decorations: state => pendingCommentWords(state.doc, options.commentPending?.()),
    } })] }),
    Extension.create({ name: "nativePropertyBounds", addProseMirrorPlugins: () => [new Plugin({
      filterTransaction: tr => {
        if (!tr.docChanged) return true;
        try {
          if (tr.doc.childCount !== 1 || tr.doc.firstChild?.type.name !== "nativePropertyText") return false;
          const spans = fragmentToNativeRichText(tr.doc.firstChild.content);
          coalesce(spans, CELL_RICHTEXT_BOUNDS);
          const max = tr.getMeta(projection) ? undefined : options.maxCharacters?.();
          return spans.reduce((sum, span) => sum + span.text.length, 0) <= CELL_RICHTEXT_BOUNDS.maxTextCharacters
            && (!max || fieldTakes(spans.map(span => span.text).join(""), max,
              () => fragmentToNativeRichText(tr.before.firstChild!.content).map(span => span.text).join("")));
        } catch (error) { if (error instanceof RangeError) return false; throw error; }
      },
    })] }),
  ], content: opening ?? { type: "doc", content: [{ type: "nativePropertyText" }] }, injectCSS: false, enableInputRules: ["nativeFieldTypedRules"], enablePasteRules: false,
  editorProps: {
    attributes,
    transformPastedHTML: html => clipboardHtmlForEngine(html, CELL_RICHTEXT_BOUNDS.maxHtmlCharacters),
    handleKeyDown: (view, event) => {
      // A key the input method takes is its own; one that sends, confirms,
      // leaves or moves finds the words typed published first, the owner's
      // Enter sending them all (fieldKeyBoundary.ts). Enter puts a line in a
      // multiline field, and in a one-line field that takes ⇧Enter's lines;
      // otherwise it ends the field, as Escape does.
      if (imeComposing(view)) return false;
      const key = fieldKey(event, { publish, enter: () => event.shiftKey && !event.metaKey && !event.ctrlKey && !options.multiline()
        ? options.shiftBreaks?.() ? "newline" : "confirm"
        : !options.multiline() || primaryModifier(event) || options.enterEnds?.() && !event.shiftKey ? "confirm" : "newline" });
      if (key === "ime") return false;
      // ⌘K or ⌘B just after ⇧← acts on the words shown chosen, as the page's keys do.
      takeDomSelection(view);
      const owned = options.keyDown?.(event);
      if (owned !== undefined) return owned;
      if (options.cellArrows?.() && cellArrow(view, event)) return true;
      // ⌘K over chosen words links them; with none chosen it is the app's
      // search, as Notion's row titles, table cells and page titles (seen
      // 2026-11-13: a row page's title, a table's title cell being edited).
      if (primaryModifier(event) && !event.shiftKey && !event.altKey && chordLetter(event) === "k" && !view.state.selection.empty) {
        options.linkRequested(); return true;
      }
      const mark = cellMarkShortcut(event);
      if (mark) {
        const at = selection();
        if (at.start < at.end) replace(toggleCellMark(read(), at.start, at.end, mark), at.start, at.end);
        // With nothing selected it marks what is typed next, as Notion's.
        else editor.commands.toggleMark(mark === "strikethrough" ? "strike" : mark);
        return true;
      }
      if (key === "escape" || key === "confirm") {
        view.dom.blur();
        if (key === "confirm") options.entered?.();
        return true;
      }
      if (key === "newline") {
        dispatchAction(view.state.tr.replaceSelectionWith(view.state.schema.nodes.hardBreak.create())); return true;
      }
      return false;
    },
    handlePaste: (view, event, slice) => {
      event.stopPropagation();
      if (!event.clipboardData) return true;
      const at = selection(), value = cellValueFromSpans(read()).value;
      const text = options.pasted(event.clipboardData, value.slice(0, at.start) + value.slice(at.end));
      if (!text) return true;
      try {
        // Lines a field that takes a line's shortcuts keeps keep their marks, and a copy of blocks there
        // is read as blocks, one as several (richTextCellLines.ts); a line of outside text has its
        // Markdown read (pastedLine.ts).
        // Outside HTML its text loses nothing of is read as that text alone (pastedHtmlChoice.ts).
        const reads = options.typedRules() || options.pastedMarks?.() === true;
        const holdsLines = options.multiline() || options.linesOnOneLine();
        const html = pastesTextOverHtml(event.clipboardData) ? "" : event.clipboardData.getData("text/html");
        const lines = reads && (text.includes("\n") || holdsLines && html)
          ? pastedCellLines(event.clipboardData, text, editor.schema, CELL_RICHTEXT_BOUNDS.maxHtmlCharacters) : null;
        const marked = lines ?? (reads && !html ? pastedFieldSpans(text, EDITOR_PASTE_BOUNDS) : null);
        const lone = !lines && marked?.length === 1 && marked[0].link && marked[0].text ? marked[0].link : null;
        const link = /^https?:\/\/\S+$/iu.test(text.trim()) && safeUrl(text.trim()) === text.trim() ? text.trim() : lone;
        const length = Math.max(text.length, (marked ?? []).reduce((sum, span) => sum + span.text.length, 0));
        if (at.start < at.end && link) {
          replace(linkCellRange(read(), at.start, at.end, link), at.start, at.end);
        } else if (value.length - (at.end - at.start) + length <= CELL_RICHTEXT_BOUNDS.maxTextCharacters) {
          // Words copied with their mentions and formatting keep them, as Notion's (seen 2026-11-05),
          // but for words the owner cut (a title's paste), which go in as it cut them.
          const words = !marked && reads && !text.includes("\n") ? pastedWords(slice) : null;
          const rich = words && words.textBetween(0, words.size, "", "").length <= text.length ? words : null;
          const tr = view.state.tr.replaceSelection(new Slice(rich ?? nativeRichTextToFragment(marked ?? [{ text }], editor.schema), 0, 0)).setMeta("uiEvent", "paste");
          // A plain line ends as typing it would, as Notion's (typedRuleAfter).
          const plain = !marked && options.typedRules() && !text.includes("\n") && !html;
          dispatchAction(plain ? typedRuleAfter(tr) : tr);
        } else options.pasteRefused();
      } catch (error) {
        if (!(error instanceof RangeError)) throw error;
        options.pasteRefused();
      }
      return true;
    },
    handleDrop: () => true,
    handleClick: (_view, _pos, event) => {
      // A date mention pressed opens its date, as in a page's text.
      const date = (event.target as HTMLElement).closest<HTMLElement>("[data-mention=\"date\"]");
      if (date && options.datePressed?.(date)) { event.preventDefault(); return true; }
      const anchor = (event.target as HTMLElement).closest<HTMLAnchorElement>("a[href]");
      const href = anchor?.getAttribute("href");
      if (!anchor || !href) return false;
      if (anchor.dataset.mention === "page") { event.preventDefault(); pushRoute(href); return true; }
      // An outside page's mention, or any link pressed with ⌘ or Ctrl, opens in a new tab. A database value's link
      // opens at a plain press too while the value is edited, the field staying open, as Notion's (seen 2026-11-18
      // in Chrome at 1440: a table's text cell being edited, and a row page's text value with its field open — one
      // click on the linked word, one new tab). This is a single click's: the second click of a double-click
      // chooses the word and comes here no more; the click that follows it in a page's text opens nothing again
      // (openPressedLink's press).
      const newTab = anchor.dataset.mention === "external" || event.metaKey || event.ctrlKey;
      if (!opensLink(event) || !newTab && !options.opensLinks?.()) return false;
      event.preventDefault(); openPressedLink(href, newTab, event); return true;
    },
    handleDOMEvents: { compositionend: () => { composition.ended(); return false; },
    // A menu's virtual focus announcement is not a real blur of this field.
    blur: (view, event) => !event.isTrusted && view.dom.ownerDocument.activeElement === view.dom },
  },
  onUpdate: ({ transaction }) => { if (!transaction.getMeta(projection)) { update(); show(); } },
  onSelectionUpdate: () => options.updated(),
  onBlur: () => {
    if (disposed) return;
    const changed = options.commitOn() === "blur" && publish();
    // Another tab or window in front blurs it while the page's focus stays in
    // it (editorHoldsPageFocus): what was typed is saved and it is still being
    // written, as Notion's database names go on across a tab switch (seen
    // 2026-11-21 by hand); it ends when the focus goes elsewhere in the page.
    if (editorHoldsPageFocus(editor)) return;
    if (pending && !changed && !dirty) {
      const at = selection(); saved = cellValueFromSpans(pending); replace(pending, at.start, at.end, true);
    }
    pending = null; options.finished();
  },
  });
  const releaseFocus = registerIndependentTiptapFocus(mount, editor);
  fieldSchema ??= editor.schema;
  if (!opening) replace(initial, 0, 0, true);
  return { editor, read, selection, replace,
    /** Publishes the words shown now, a composition's too: a send button reads them next (fieldKeyBoundary.ts). */
    publish() { if (!disposed && !editor.isDestroyed) publish(); },
    /** Draws the words a new comment is on again, as they change. */
    redrawCommentPending() { if (!disposed && !editor.isDestroyed) editor.view.dispatch(editor.state.tr.setMeta("addToHistory", false)); },
    sync(spans: TextSpan[]) {
      editor.setOptions({ editable: options.editable?.() ?? true, editorProps: { attributes } });
      const next = cellValueFromSpans(spans);
      if (named.has(next.value) && !same(next, saved)) return;
      if (same(next, cellValueFromSpans(typed()))) { projected = next; saved = next; pending = null; dirty = false; followFrom = null; return; }
      // A render of the same canonical value is not a new remote change.
      // In particular, blur must not replace a just-admitted local draft
      // with the value from before its native receipt arrives.
      if (same(next, projected)) return;
      const before = projected;
      projected = next;
      if (editorHoldsPageFocus(editor) || imeComposing(editor.view) || dirty) {
        if (options.follows?.()) { followFrom ??= before; follow(); } else pending = spans;
        return;
      }
      saved = next; pending = null;
      const at = selection(); replace(spans, at.start, at.end, true);
    },
    // A field that saves on leaving saves what was typed when it goes too, as
    // its page closes or another opens: no blur comes then.
    destroy() {
      if (!disposed && !editor.isDestroyed && options.commitOn() === "blur") publish();
      typing.stop();
      disposed = true; composition.stop(); releaseFocus(); editor.destroy();
    },
  };
}

export type RichTextCellEngine = ReturnType<typeof createRichTextCellEngine>;
