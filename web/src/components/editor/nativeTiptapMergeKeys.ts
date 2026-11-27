import { Extension, extensions, type Editor } from "@tiptap/core";
import { Fragment, type Node } from "@tiptap/pm/model";
import { Plugin, TextSelection, type Transaction } from "@tiptap/pm/state";
import type { EditorView } from "@tiptap/pm/view";
import { projectedBlocks, type NativeProjectedBlock } from "./nativeTiptapBinding";
import { nativeTiptapTextFields } from "./nativeTiptapTextFields";
import { blockSpec, isBlockType } from "@/lib/blockSemantics";
import { inlineDatabaseTitleJoin, pageTitleJoin } from "@/lib/inlineDatabaseRequests";
import { detectKeyboardShortcutPlatform } from "@/lib/keyboardShortcuts";
import { quietTableCaret } from "./simpleTableQuietCaret";
import { takeDomSelection } from "@/lib/tiptapDomSelection";
import { imeComposing } from "@/lib/fieldKeyBoundary";

// A line's start and end join it with the text before and after it as
// Notion's do (seen 2026-11-02, audits a1 and a2; 2026-11-03): Backspace at a
// line's start puts the line at the end of the last text that shows before
// it, and Delete at its end pulls the next text that shows into it (an empty
// line just goes); the other keys that do so on a Mac are in macKeys. Delete
// at a code block's end pulls it into the code the same way (Notion's, seen
// 2026-11-09: a line, a heading, a line past a divider, another code block;
// an empty line just goes, a joined line's own lines take its place), and
// so does ⌃D on a Mac; ⌃K and the others keep still there.
// Pictures, files, media, dividers, page links, bookmarks and embeds are
// passed over; a closed toggle's title, a synced block's or column's first
// line take part, Backspace after a table its last cell, and an inline
// database its title (inlineDatabaseTitleJoin); Delete before a table, or
// anything else, stops it.
// The lines under a joined line take its place.
// Tiptap's own joins (2026-10-29 migration) put the next block inside this
// one, and a closed toggle's hidden line took what was typed.

const TEXT_NODES = new Set(["nativeText", "nativeCode"]);
const PASSED_OVER = new Set([
  "divider", "image", "video", "audio", "file", "pdf", "bookmark", "link_preview", "embed", "child_page", "link_to_page",
]);
const STRUCTURE = new Set(["synced_block", "column_list", "column"]);
const COLUMNS = new Set(["column_list", "column"]);

type Blocks = readonly NativeProjectedBlock[];
type MergeOptions = {
  withNextUndo(step: (forward: boolean) => void): void;
  pageId(): string;
  undo(): void;
  /** Takes a block out by the graph's rules, with a change to the text, as one Undo. */
  removeWith(blockId: string, change: () => void): void;
};
/** Where joined text goes: a line's text or a table's cell. */
type Spot = { node: NativeProjectedBlock["node"]; contentFrom: number };
type Database = { database: NativeProjectedBlock };

function hidden(byId: Map<string, NativeProjectedBlock>, block: NativeProjectedBlock) {
  for (let id = block.projectionParentId; id; id = byId.get(id)?.projectionParentId ?? null) {
    if (byId.get(id)?.container.attrs.props?.collapsed === true) return true;
  }
  return false;
}

function within(byId: Map<string, NativeProjectedBlock>, block: NativeProjectedBlock, ancestorId: string) {
  for (let id = block.projectionParentId; id; id = byId.get(id)?.projectionParentId ?? null) if (id === ancestorId) return true;
  return false;
}

/** The text a line joins, looking one way from it; null stops the key there. */
function joinTarget(blocks: Blocks, from: NativeProjectedBlock, step: 1 | -1): NativeProjectedBlock | Spot | Database | null {
  const byId = new Map(blocks.map((block) => [block.id, block]));
  for (let index = blocks.indexOf(from) + step; index >= 0 && index < blocks.length; index += step) {
    const candidate = blocks[index], kind = String(candidate.container.attrs.kind);
    if (hidden(byId, candidate)) continue;
    // Looking back, the line's own parents are not before it, but for a
    // callout's text and one with none, and its column and columns: a
    // column's first line joins the last line of the column before it, the
    // first column's the text before the columns (Notion's, 2026-11-15,
    // worker Chrome, no script before the key: ⌫ at the second of two
    // columns' start, the middle of three's and the first column's, then a letter went in where
    // the texts met; a column left with nothing goes); a synced block's first line the text before it.
    if (step < 0 && within(byId, from, candidate.id) && kind !== "callout" && !STRUCTURE.has(kind)) return null;
    if (TEXT_NODES.has(candidate.node.type.name)) return candidate;
    if (step < 0 && candidate.node.type.name === "nativeTableCells") {
      return nativeTiptapTextFields([candidate]).filter((field) => typeof field.field === "number").at(-1) ?? null;
    }
    if (kind === "inline_database") return { database: candidate };
    if (PASSED_OVER.has(kind) || STRUCTURE.has(kind) || kind === "callout") continue;
    return null;
  }
  return null;
}

/**
 * Whether ⌃⌥⌫ at a line's start or ⌥⌦ at its end joins this: only a line's
 * text or the page's title. Notion's leave code, an inline database and a
 * table alone (seen 2026-11-08 and 2026-11-09).
 */
const wordJoins = (target: NativeProjectedBlock | Spot | Database) => "container" in target && target.node.type.name === "nativeText";

/**
 * Takes a line's block out, the lines under it taking its place (Notion's,
 * seen 2026-11-03); a block's only line leaves no empty line under it (seen
 * 2026-11-09: Delete at a line's end joined its one nested line, none left).
 */
function takeOut(tr: Transaction, later: NativeProjectedBlock) {
  const start = later.contentFrom - 2, children = later.container.lastChild;
  const $start = tr.doc.resolve(start), holder = $start.depth > 1 ? $start.node($start.depth - 1) : null;
  // The last line a callout with no text of its own holds takes the callout along, as Notion's (seen 2026-11-05).
  if (holder?.attrs.kind === "callout" && holder.firstChild?.type.name === "nativeWidget" && $start.parent.childCount === 1
    && children?.type.name !== "nativeChildren") {
    // The page keeps a line of its own.
    if (tr.doc.childCount === 1) tr.replaceWith(0, tr.doc.content.size, later.container.type.create({ ...later.container.attrs, kind: "paragraph", props: {} },
      later.container.type.schema.nodes.nativeText.create()));
    else tr.delete($start.before($start.depth - 1), $start.after($start.depth - 1));
    return;
  }
  const lines = children?.type.name === "nativeChildren" ? children.content : Fragment.empty;
  // The schema wants a block's lines to hold one, so its last going takes the lines with it.
  if (!lines.size && $start.parent.type.name === "nativeChildren" && $start.parent.childCount === 1) {
    tr.delete($start.before(), $start.after());
    return;
  }
  tr.replaceWith(start, start + later.container.nodeSize, lines);
}

/** Whether a block is a callout with no text of its own and this line is inside it. */
function heldBy(holder: NativeProjectedBlock, line: NativeProjectedBlock) {
  return holder.container.attrs.kind === "callout" && holder.node.type.name === "nativeWidget" && line.projectionParentId === holder.id;
}

/** Whether a line is all a synced block or a column holds. */
function holdsItsHolder(doc: Node, later: NativeProjectedBlock) {
  const $start = doc.resolve(later.contentFrom - 2);
  return $start.depth > 1 && $start.parent.childCount === 1 && later.container.lastChild?.type.name !== "nativeChildren"
    && ["synced_block", "column"].includes(String($start.node($start.depth - 1).attrs.kind));
}

/** Whether taking out `later`'s column leaves one of two, `earlier` being in the other. */
function endsColumns(doc: Node, earlier: NativeProjectedBlock | Spot, later: NativeProjectedBlock) {
  const $start = doc.resolve(later.contentFrom - 2), depth = $start.depth - 2;
  return depth > 0 && $start.node(depth + 1).attrs.kind === "column" && $start.node(depth).childCount === 2
    && earlier.contentFrom > $start.start(depth) && earlier.contentFrom < $start.end(depth);
}

/** Joins `later`'s text onto the end of `earlier`. */
function join(editor: Editor, earlier: NativeProjectedBlock | Spot, later: NativeProjectedBlock) {
  const { state, view } = editor;
  const tr = state.tr;
  const at = earlier.contentFrom + earlier.node.content.size;
  const text = earlier.node.type.name === "nativeCode"
    ? Fragment.from(later.node.textContent ? state.schema.text(later.node.textContent) : [])
    : later.node.type.name === "nativeCode" && later.node.textContent ? Fragment.from(state.schema.text(later.node.textContent)) : later.node.content;
  // The last line of a synced block leaves it empty with its hint; of a column,
  // the column goes, and the last column's blocks take the columns' place: the
  // graph's rule, as Notion's (seen 2026-11-04, a8). The caret stays where the
  // texts met, but goes to the joined line's start when the join ends two
  // columns. Notion's, measured in Chrome with no script before the key and a
  // letter typed after it: two columns of one line each, ⌦ at the first's end
  // and ⌫ at the second's start, caret at the joined line's start (2026-11-15,
  // worker w1132 4 of 4, and w1249 in another tab at another time); with three,
  // one going, where the texts met (w1132 2 of 2).
  const options = mergeOptions(editor);
  if (options && holdsItsHolder(state.doc, later)) {
    const caret = endsColumns(state.doc, earlier, later) ? earlier.contentFrom : at;
    options.removeWith(later.nativeId, () => {
      tr.insert(at, text);
      view.dispatch(tr.setSelection(TextSelection.create(tr.doc, caret)).scrollIntoView());
    });
    return true;
  }
  takeOut(tr, later);
  tr.insert(at, text);
  // The caret where the texts meet, as Notion's (2026-11-15, worker Chrome, no
  // script before the key: ⌫ at a line's start, then a letter went in between
  // the joined words, in both).
  tr.setSelection(TextSelection.create(tr.doc, at));
  view.dispatch(tr.scrollIntoView());
  // Joined into a table's cell, the cell isn't outlined until the caret moves, as Notion's.
  if (!("container" in earlier)) quietTableCaret();
  return true;
}

function mergeOptions(editor: Editor) {
  return editor.extensionManager.extensions.find((extension) => extension.name === "nativeMergeKeys")?.options as MergeOptions | undefined;
}

/** The line the caret is in; a code block's only with `code`, its other keys staying the engine's. */
function current(editor: Editor, code = false) {
  const { state, view } = editor;
  if (!editor.isEditable || imeComposing(view) || !(state.selection instanceof TextSelection) || !state.selection.empty) return null;
  const blocks = projectedBlocks(state.doc), { $from } = state.selection;
  const block = blocks.find((candidate) => (candidate.node.type.name === "nativeText" || code && candidate.node.type.name === "nativeCode")
    && candidate.contentFrom <= $from.pos && $from.pos <= candidate.contentFrom + candidate.node.content.size);
  return block ? { blocks, block, offset: $from.pos - block.contentFrom } : null;
}

/**
 * Backspace at a line's start; the structure keys first take a list item to
 * text or a nested line out. `byWord` is ⌃⌥⌫'s join (wordJoins).
 */
export function joinBackward(editor: Editor, byWord = false) {
  const caption = joinCaption(editor, byWord);
  if (caption !== null) return caption;
  const here = current(editor);
  if (!here || here.offset !== 0) return false;
  const target = joinTarget(here.blocks, here.block, -1);
  if (byWord && target && !wordJoins(target)) return true;
  const options = mergeOptions(editor);
  // The page's first line joins its title (Notion's, seen 2026-11-04), as does
  // the first column's first line of columns that open the page, the column
  // going when it held only that line (Notion's, 2026-11-15, worker Chrome, no
  // script before the key; a letter typed next went in where the texts met);
  // nothing else to join with stops the key, rather than Tiptap's putting the
  // line inside a block.
  if (!target) {
    const before = here.blocks.slice(0, here.blocks.indexOf(here.block)), byId = new Map(here.blocks.map((block) => [block.id, block]));
    const first = before.length === 1 && heldBy(before[0], here.block)
      || before.every((block) => COLUMNS.has(String(block.container.attrs.kind)) && within(byId, here.block, block.id));
    if (!first || !options) return true;
    const pageId = options.pageId();
    return joinLineToTitle(editor, here.block, () => pageTitleJoin(pageId), options);
  }
  if (!("database" in target)) return join(editor, target, here.block);
  const id = target.database.nativeId;
  return joinLineToTitle(editor, here.block, () => inlineDatabaseTitleJoin(id), options);
}

/**
 * Puts a line's words at the end of a title and takes the line out; all a
 * column or synced block holds goes by the graph's rules, as join's does
 * (holdsItsHolder), so a column it empties goes rather than staying empty.
 */
function joinLineToTitle(editor: Editor, line: NativeProjectedBlock, titleJoin: () => ReturnType<typeof inlineDatabaseTitleJoin>,
  options: MergeOptions | undefined) {
  const words = line.node.textContent;
  // With no title to take its words the line stays, as joinTitle leaves it.
  if (options && holdsItsHolder(editor.state.doc, line)) {
    if (titleJoin()) options.removeWith(line.nativeId, () => joinTitle(editor, words, () => {}, titleJoin, options));
    return true;
  }
  return joinTitle(editor, words, tr => takeLine(tr, line), titleJoin, options);
}

/**
 * Backspace at a caption's start puts its words at the end of the last text
 * that shows before its block, as a line's go, and the caption closes; with
 * only pictures, media and embeds before it, at the end of the page's title
 * (Notion's, seen 2026-11-06: a bookmark's caption after a video and an
 * embed); a code block's caption, at the end of its code. Null when the
 * caret is not at a caption's start.
 */
function joinCaption(editor: Editor, byWord: boolean) {
  const { state, view } = editor, { selection } = state;
  if (!editor.isEditable || imeComposing(view) || !(selection instanceof TextSelection) || !selection.empty
    || selection.$from.parent.type.name !== "nativeCaption" || selection.$from.parentOffset !== 0) return null;
  const blocks = projectedBlocks(state.doc);
  const field = nativeTiptapTextFields(blocks).find(candidate => candidate.field === "caption" && candidate.contentFrom === selection.from);
  if (!field) return false;
  // A code block's own code is the text before its caption (Notion's, seen 2026-11-06: a diagram's caption joined its code).
  const target = TEXT_NODES.has(field.block.node.type.name) ? field.block : joinTarget(blocks, field.block, -1);
  if (byWord && target && !wordJoins(target)) return true;
  const options = mergeOptions(editor), words = field.node.textContent;
  const close = (tr: Transaction) => {
    const attrs = field.block.container.attrs;
    tr.delete(field.contentFrom, field.contentFrom + field.node.content.size);
    if (attrs.props?.captionVisible === true) tr.setNodeMarkup(field.block.contentFrom - 2, undefined, { ...attrs, props: { ...attrs.props, captionVisible: false } });
  };
  if (!target) {
    const before = blocks.slice(0, blocks.indexOf(field.block));
    if (field.block.projectionParentId || !before.every(block => PASSED_OVER.has(String(block.container.attrs.kind))) || !options) return true;
    const pageId = options.pageId();
    return joinTitle(editor, words, close, () => pageTitleJoin(pageId), options);
  }
  if ("database" in target) {
    const id = target.database.nativeId;
    return joinTitle(editor, words, close, () => inlineDatabaseTitleJoin(id), options);
  }
  const tr = state.tr, end = target.contentFrom + target.node.content.size;
  const text = target.node.type.name === "nativeCode" ? Fragment.from(words ? state.schema.text(words) : []) : field.node.content;
  close(tr);
  tr.insert(end, text);
  view.dispatch(tr.setSelection(TextSelection.create(tr.doc, end)).scrollIntoView());
  return true;
}

/** Takes a joined line out; the page keeps a line of its own, its only one emptied instead. */
function takeLine(tr: Transaction, line: NativeProjectedBlock) {
  if (tr.doc.childCount === 1 && line.container === tr.doc.firstChild) {
    tr.setNodeMarkup(line.contentFrom - 2, undefined, { ...line.container.attrs, kind: "paragraph", props: {} });
    if (line.node.content.size) tr.delete(line.contentFrom, line.contentFrom + line.node.content.size);
  } else takeOut(tr, line);
}

/** Puts words at the end of a title, one Undo bringing both back as they were, as Notion's. */
function joinTitle(editor: Editor, words: string, clear: (tr: Transaction) => void,
  titleJoin: () => ReturnType<typeof inlineDatabaseTitleJoin>, options: MergeOptions | undefined) {
  const title = titleJoin();
  if (!title) return true;
  const prior = title.title, next = prior + words;
  options?.withNextUndo((forward) => {
    const current = titleJoin();
    if (current?.title === (forward ? prior : next)) current.set(forward ? next : prior);
  });
  const tr = editor.state.tr;
  clear(tr);
  editor.view.dispatch(tr);
  title.set(next, prior.length, options?.undo);
  return true;
}

/** Delete at a line's end; `byWord` is ⌥⌦'s join (wordJoins). */
function joinForward(editor: Editor, byWord = false) {
  const here = current(editor, !byWord);
  if (!here || here.offset !== here.block.node.content.size) return false;
  const target = joinTarget(here.blocks, here.block, 1);
  if (byWord && target && !wordJoins(target)) return true;
  if (target && "database" in target) {
    // Its title comes into the line and the database goes, without asking, the
    // caret where the line ended, as Notion's (2026-11-15, worker Chrome, no
    // script before the key: ⌦ at the end of the line before a named inline
    // database, then a letter went in between the line's words and the title's).
    const title = inlineDatabaseTitleJoin(target.database.nativeId);
    if (!title) return true;
    const { state, view } = editor;
    const at = here.block.contentFrom + here.block.node.content.size;
    const tr = state.tr;
    takeOut(tr, target.database);
    if (title.title) tr.insert(at, state.schema.text(title.title));
    view.dispatch(tr.setSelection(TextSelection.create(tr.doc, at)).scrollIntoView());
    return true;
  }
  // An empty line just goes, the next keeping its kind with the caret at its
  // start (Notion's, seen 2026-11-06 before a heading and, past a page link,
  // a toggle; again 2026-11-15, worker Chrome, no script before the key: ⌦ on
  // an empty line before a heading, then a letter went in at the heading's
  // start, in both); one with lines under it, or all a synced block or column holds, joins.
  if (target && "container" in target && !here.block.node.content.size && here.block.node.type.name === "nativeText"
    && here.block.container.lastChild?.type.name !== "nativeChildren" && !holdsItsHolder(editor.state.doc, here.block)) {
    const tr = editor.state.tr;
    takeOut(tr, here.block);
    editor.view.dispatch(tr.setSelection(TextSelection.create(tr.doc, tr.mapping.map(target.contentFrom))).scrollIntoView());
    return true;
  }
  // Looking forward it is otherwise a line's block (a table stops it).
  return target && "container" in target ? join(editor, here.block, target) : true;
}

/**
 * ⌃H at a line's start takes the caret to the end of the text before it,
 * joining nothing (Notion's, seen 2026-11-11 and 2026-11-12: after a line, a
 * list item, a toggle's last line, past a table, a picture or a bookmark;
 * into a synced block's or the last column's line). Code, an inline
 * database and a divider before it stop it, as does the page's start.
 * Notion's own caret is the browser's, left where its delete would have
 * begun: at times in a spot that takes no typing, and none when the caret
 * came by Home; this one is a line's. Not measured again 2026-11-15: a ⌃H
 * the worker Chrome's extension sends is not the Mac's own editing key
 * (Notion's caret stayed, the letter going in at the line's start), and the
 * Notion desktop app, where a real key goes, needed an approval no one gave.
 */
function caretBack(editor: Editor) {
  const here = current(editor);
  if (!here || here.offset !== 0) return false;
  const { blocks, block } = here, byId = new Map(blocks.map((candidate) => [candidate.id, candidate]));
  for (let index = blocks.indexOf(block) - 1; index >= 0; index -= 1) {
    const candidate = blocks[index], kind = String(candidate.container.attrs.kind);
    if (hidden(byId, candidate)) continue;
    if (candidate.node.type.name === "nativeText") {
      const { state, view } = editor;
      view.dispatch(state.tr.setSelection(TextSelection.create(state.doc, candidate.contentFrom + candidate.node.content.size)).scrollIntoView());
      return true;
    }
    if (candidate.node.type.name === "nativeCode" || kind === "divider" || kind === "inline_database") return true;
    if (candidate.node.type.name === "nativeTableCells" || kind === "table" || PASSED_OVER.has(kind) || STRUCTURE.has(kind) || kind === "callout") continue;
    return true;
  }
  return true;
}

type Edge = { place: "text" | "code" | "caption" | "cell"; start: boolean; end: boolean };
const PLACES: Record<string, Edge["place"]> = { nativeText: "text", nativeCode: "code", nativeCaption: "caption", nativeTableCell: "cell" };

/** Where an empty caret stands: in what text, at its start or its end. */
function edge(editor: Editor): Edge | null {
  const { state, view } = editor, { selection } = state;
  if (!editor.isEditable || imeComposing(view) || !(selection instanceof TextSelection) || !selection.empty) return null;
  const { $from } = selection, place = PLACES[$from.parent.type.name];
  return place ? { place, start: $from.parentOffset === 0, end: $from.parentOffset === $from.parent.content.size } : null;
}

const mac = detectKeyboardShortcutPlatform() === "mac";

/**
 * The keys that delete at a line's start as ⌫ does, a list item or a quote
 * first turning to text (nativeTiptapStructureKeys). On a Mac they are the
 * keys macOS deletes a letter or a word back with but ⌃H and ⌃⌥⌫, as Notion's
 * (seen 2026-11-09); elsewhere, as they were.
 */
export const BACKSPACE_KEYS = mac ? ["Backspace", "Shift-Backspace", "Alt-Backspace", "Ctrl-Backspace"]
  : ["Backspace", "Shift-Backspace", "Alt-Backspace", "Ctrl-h"];

/**
 * The Mac's keys at a line's ends, as Notion's in a line of text, a list
 * item, a heading, a toggle and code (seen 2026-11-09; the desktop app's
 * WebKit names the keys as Chrome does):
 * - At a line's start ⇧⌫, ⌥⌫ and ⌃⌫ do what ⌫ does; ⌘⌫ joins at once, with
 *   no list item turning to text first; ⌃⌥⌫ joins as ⌘⌫ but only a line's
 *   text (wordJoins); ⌃H takes the caret back, joining nothing (caretBack).
 *   In code and a table's cell they all keep still, as ⌫ does there; ⌃H in
 *   a caption too.
 * - At its end ⌃D and ⌃K join as ⌦; ⌥⌦ joins only a line's text; ⌘⌦, ⇧⌦,
 *   ⌃⌦ and ⌃⌥⌦ do nothing (⌘⌦ nowhere does anything, words chosen too). At
 *   code's end ⌃D joins as ⌦ does there and the others keep still; in a
 *   caption and a table's cell they all keep still, as ⌦ does there.
 * Notion's ⌃⌥⌫ and ⌥⌦ copy the other line's text and leave that line (a
 * second copy of it); Leafen's move it, as ⌘⌫ and ⌦ do. Away from the ends
 * the keys are the browser's own: ⌃⌥⌫ deletes the word before the caret as
 * ⌥⌫ does (NativeTiptapEngineKeymap).
 */
function macKeys(editor: Editor) {
  const back = (byWord = false) => () => {
    const at = edge(editor);
    if (!at?.start) return false;
    return at.place === "text" || at.place === "caption" ? joinBackward(editor, byWord) : true;
  };
  const forward = (byWord = false, inCode = false) => () => {
    const at = edge(editor);
    if (!at?.end) return false;
    return at.place === "text" || at.place === "code" && inCode ? joinForward(editor, byWord) : true;
  };
  const still = (end: boolean) => () => { const at = edge(editor); return Boolean(at && (end ? at.end : at.start)); };
  return {
    "Shift-Backspace": back(), "Alt-Backspace": back(), "Ctrl-Backspace": back(), "Mod-Backspace": back(),
    "Ctrl-Alt-Backspace": back(true),
    "Ctrl-h": () => { const at = edge(editor); return at?.start ? at.place !== "text" || caretBack(editor) : false; },
    "Ctrl-d": forward(false, true), "Ctrl-k": forward(), "Alt-Delete": forward(true), "Shift-Delete": still(true),
    "Mod-Delete": () => editor.isEditable && editor.state.selection instanceof TextSelection,
  };
}

/**
 * A delete that comes as input rather than as a key — a phone's keyboard,
 * dictation, a script's execCommand — at a line's edge, over more than one
 * line or over chosen blocks does what Backspace or Delete does there. Left
 * to the browser it edits the page's elements itself, and the engine reads
 * what it took out as taken out of the page: an inline database's element
 * gone with the empty line after it took the database away. Within a line's
 * text the browser's own delete stays.
 */
function deleteInputAsKey(view: EditorView, event: Event) {
  const type = (event as InputEvent).inputType ?? "";
  const backward = type.endsWith("Backward") || type === "deleteContent";
  if (!type.startsWith("delete") || !(backward || type.endsWith("Forward")) || imeComposing(view)) return false;
  // Where the browser shows the caret now: a delete just after a click or a script's selection comes before the editor hears of it.
  takeDomSelection(view);
  const { selection } = view.state, { $from, $to } = selection;
  if (selection instanceof TextSelection && $from.parent.inlineContent && (selection.empty
    ? backward ? $from.parentOffset > 0 : $from.parentOffset < $from.parent.content.size
    : $from.sameParent($to))) return false;
  event.preventDefault();
  const key = backward ? "Backspace" : "Delete";
  const press = Object.assign(new Event("keydown", { bubbles: true, cancelable: true }), { key, code: key, keyCode: backward ? 8 : 46 });
  if (!view.someProp("handleKeyDown", handle => handle(view, press as KeyboardEvent)) && !selection.empty) {
    view.dispatch(view.state.tr.deleteSelection().scrollIntoView());
  }
  return true;
}

export const NativeTiptapMergeKeys = Extension.create<MergeOptions>({
  name: "nativeMergeKeys", priority: 1000,
  addOptions: () => ({ withNextUndo: () => {}, pageId: () => "", undo: () => {}, removeWith: () => {} }),
  addProseMirrorPlugins: () => [new Plugin({ props: { handleDOMEvents: { beforeinput: deleteInputAsKey } } })],
  addKeyboardShortcuts() {
    const back = () => joinBackward(this.editor), forward = () => joinForward(this.editor);
    return {
      ...(mac ? macKeys(this.editor) : {
        "Mod-Backspace": back, "Shift-Backspace": back, "Alt-Backspace": back, "Ctrl-h": back,
        "Mod-Delete": forward, "Alt-Delete": forward, "Ctrl-d": forward,
      }),
      Delete: forward,
      // ⌘Enter makes no line break in a line, as Notion's; a to-do's and a
      // toggle's are the product keys' (nativeTiptapProductKeys).
      "Mod-Enter": () => {
        const { $from } = this.editor.state.selection;
        const kind = $from.depth > 0 ? String($from.node($from.depth - 1).attrs.kind ?? "") : "";
        return $from.parent.type.name === "nativeText" && kind !== "to_do" && !(isBlockType(kind) && blockSpec(kind).toggle);
      },
    };
  },
});

/**
 * Tiptap's own keys as macOS reads them, in place of Tiptap's core keymap
 * (nativeTiptapController): ⌃⌥⌫ deletes the word before the caret as ⌥⌫
 * does, where Tiptap took it for Delete and joined the next line at a line's
 * end; ⌥D types its letter, where Tiptap took it for Delete too.
 */
export const NativeTiptapEngineKeymap = extensions.Keymap.extend({
  addKeyboardShortcuts() {
    const keys = { ...this.parent?.() };
    if (keys["Ctrl-Alt-Backspace"] && keys["Alt-Backspace"]) keys["Ctrl-Alt-Backspace"] = keys["Alt-Backspace"];
    delete keys["Alt-d"];
    return keys;
  },
});
