import { setBlockRichText } from "@/collaboration";
import { graphTextBlockTypes, type ContentGraph, type GraphBlock, type GraphBlockType, type GraphOperation } from "@/contentGraph";
import { copyTextWithBlocks, writeTextWithBlocks } from "@/lib/clipboard";
import { clipboardServer, copiedBlockContent, copiedWords } from "@/lib/clipboardServer";
import { yTextToRichText } from "@/lib/collaborationCrdtCodec";
import { newId } from "@/lib/ids";
import { TOGGLE_BLOCK_TYPES } from "@/lib/blockSemantics";
import { spansToPlainText, type Block, type TextSpan } from "@/lib/types";
import { blocksClipboardHtml, blocksTreeHtml, blocksCopyMarkdown } from "./blockMarkdown";
import { selectedRootsInOrder } from "./blockSelectionKeyboard";
import { syncedBlockSource } from "./syncedBlockModel";
import { contentWithOwnViews, withPastedViewCopies } from "./blockViewCopies";
import { tidyColumns } from "./columnLayoutModel";
import { editorBlockChildren, insertEditorBlock, insertEditorChildBlock, removeEditorBlocks } from "./editorGraphModel";
import { normalizePastedLink, pastedBlockTextSpans } from "./editorInputModel";
import type { EditorBlockSpot, NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { pastedLineBlocks } from "./pastedLine";
import { cutRowsPasted } from "./cutPagePaste";
import { notionCopy, pastesTextOverHtml } from "./pastedHtmlChoice";
import {
  parseInternalPastedBlocks, parsePastedHtml, parsePastedMarkdown, type PastedBlock,
} from "./markdownPaste";

import {
  EDITOR_CLIPBOARD_BOUNDS as CLIPBOARD_BOUNDS,
  EDITOR_MARKDOWN_BOUNDS as MARKDOWN_BOUNDS,
  EDITOR_PASTE_BOUNDS as PASTE_BOUNDS,
} from "./editorClipboardPolicy";

const textBlockTypes = new Set<string>(graphTextBlockTypes);

function blockSpans(adapter: NativeEditorGraphAdapter, block: GraphBlock): TextSpan[] {
  const document = adapter.collaborationDocument;
  if (!textBlockTypes.has(block.type) || !document) return [];
  return yTextToRichText(document.getText(`block:${block.id}`)) ?? [];
}

/**
 * Copies blocks with what they hold, as Markdown text, as HTML and as Leafen
 * blocks, which paste back as the same blocks — a synced block, original or
 * copy, as a copy of its original, as Notion's ⌘C of a synced block pastes
 * (seen 2026-11-01), its text going to other apps. A line whose words were
 * all copied (`line`) gives them as words are copied (copiedWords). Words
 * copied over lines (`chosen`, by line the words of it chosen when not all)
 * give the lines they run over, what those hold only where the words run.
 */
function editorBlockClipboard(adapter: NativeEditorGraphAdapter, blockIds: readonly string[], cut = false, line = false,
  chosen?: ReadonlyMap<string, TextSpan[] | null>) {
  const graph = adapter.graph;
  const roots = selectedRootsInOrder(graph, new Set(blockIds));
  if (roots.length === 0) return null;
  const modelBlocks: Block[] = [];
  // A page's line, or a link to one, copies as the page's title, which the
  // line shows from the workspace's pages rather than keeps.
  const linkedPageTitle = (block: GraphBlock) => {
    if (block.type !== "child_page" && block.type !== "link_to_page" && block.type !== "child_database") return undefined;
    const id = [block.content.childPageId, block.content.pageId].find((value): value is string => typeof value === "string" && value.trim() !== "");
    return (id && adapter.moveContext?.pages.find((page) => page.id === id)?.title.trim()) || undefined;
  };
  // Each block names itself, for a paste on another server to link to it there (otherServerBlocks.ts).
  const pasted = (block: GraphBlock, index: number): PastedBlock & { id: string } => ({ id: block.id, ...pastedBlock(block, index) });
  const pastedBlock = (block: GraphBlock, index: number): PastedBlock => {
    const rich = chosen?.get(block.id) ?? blockSpans(adapter, block);
    const source = block.type === "inline_database" ? graph.dataSources.find((candidate) => candidate.id === block.content.dataSourceId) : undefined;
    // Its text for other apps links to the database's own address, as Notion's (seen 2026-11-06).
    const databasePage = block.type === "inline_database" ? { childPageId: block.id } : {};
    // A page mention copies as its page's title now, which it shows rather than keeps.
    const shown = rich.map((span) => {
      const title = span.mention === "page" && span.pageId ? adapter.moveContext?.pages.find((page) => page.id === span.pageId)?.title.trim() : "";
      return title ? { ...span, text: title } : span;
    });
    // Around suggested edits other apps take every word shown, and a paste
    // here the blocks as they were before them (copiedWords).
    const given = copiedWords(shown, false), kept = copiedWords(shown, !line);
    const content = copiedBlockContent(block.content);
    modelBlocks.push({ id: block.id, pageId: adapter.pageId, parentId: block.parentId, type: block.type as Block["type"],
      content: { ...content, ...databasePage, rich: given } as Block["content"], plainText: source ? source.title : linkedPageTitle(block) ?? spansToPlainText(rich), position: index });
    // A table's rows go with it whole.
    const children = editorBlockChildren(graph, block.id)
      .filter((child) => !chosen || block.type === "table" || chosen.has(child.id)).map(pasted);
    if (block.type === "inline_database" && !block.content.linkedDatabaseSource) {
      // Cut, it names its workspace's trash: pasted, the same database comes back from it (moveCutDatabaseBlock).
      const cutOf = cut ? { cut: true, workspaceId: adapter.workspaceId } : {};
      return { type: "inline_database", content: { [COPIED_DATABASE]: { blockId: block.id, pageId: adapter.pageId, ...cutOf } } as Block["content"],
        ...(source ? { plainText: source.title } : {}) };
    }
    // A sub-page cut moves with it: pasted, the page comes back from the trash into the page pasted in (moveCutPageBlock).
    if (cut && block.type === "child_page" && block.content.childPageId === block.id) {
      return { type: "child_page", content: { ...block.content, rich, [CUT_PAGE]: block.id } as Block["content"], plainText: spansToPlainText(rich) };
    }
    // A sub-page copied names its page: pasted, the page is copied there (copyPageBlock), as Notion's.
    if (block.type === "child_page" && block.content.childPageId === block.id) {
      return { type: "child_page", content: { ...content, rich: kept, [COPIED_PAGE]: block.id } as Block["content"],
        plainText: spansToPlainText(copiedWords(rich, !line)), children };
    }
    if (block.type === "synced_block") {
      const source = syncedBlockSource(block, adapter.pageId, graph.blocks);
      return { type: "synced_block", content: { syncedBlockId: source.blockId, ...(source.pageId ? { syncedPageId: source.pageId } : {}) } as Block["content"] };
    }
    // A copied toggle pastes closed, as Notion's (seen 2026-11-07); a cut one comes back as it was.
    const closed = !cut && TOGGLE_BLOCK_TYPES.has(block.type as never) ? { collapsed: true } : {};
    // A linked database view goes by its database's name, its tabs with it; a page's mention, by its page's (pasted on another server, its link's words).
    return { type: block.type as Block["type"], content: { ...contentWithOwnViews(content, graph, block.id), rich: kept, ...closed } as Block["content"],
      plainText: source ? source.title : spansToPlainText(copiedWords(rich, !line)), children };
  };
  const blocks = roots.map(pasted);
  try {
    const rootsById = new Map(modelBlocks.map((block) => [block.id, block]));
    const markdown = blocksCopyMarkdown(roots.map((root) => rootsById.get(root.id)!), modelBlocks, MARKDOWN_BOUNDS);
    const copyId = newId();
    const html = blocksClipboardHtml(blocksTreeHtml(roots.map((root) => rootsById.get(root.id)!), modelBlocks, MARKDOWN_BOUNDS),
      MARKDOWN_BOUNDS, copyId);
    // Words copied over lines carry no blocks: a page takes them back as words (nativeTiptapCopiedWords).
    if (chosen) return { markdown, html, data: "" };
    // Where the blocks came from, for a paste in another page to sync with or link to them, and on
    // which server, whose pages and files they name (internalBlockPaste.ts).
    const source = { pageId: adapter.pageId, blockIds: roots.map((root) => root.id) };
    // A line's words copied whole say so, for a field of words to take them as words (richTextCellLines.ts).
    const words = line ? { line: true } : {};
    return { markdown, html, data: JSON.stringify({ version: 2, copyId, text: markdown, blocks, source, server: clipboardServer(), ...words }) };
  } catch {
    return null;
  }
}

/**
 * What words copied over lines give other apps: the lines they run over as a
 * copy of those blocks gives them, as text (blocksCopyMarkdown) and as HTML
 * (blocksTreeHtml), the first's and the last's words only those chosen
 * (`words`); a block holding the line they start in is left out, its lines
 * among them taken out of it; a table is whole — as Notion's ⌘C of words
 * over lines (seen 2026-11-11: its HTML is its block copy's, `<h2>`,
 * `<ul><li>`). Null when the page has none of the lines.
 */
export function copiedLines(adapter: NativeEditorGraphAdapter, lines: readonly { id: string; words: TextSpan[] | null }[]) {
  const chosen = new Map(lines.map((line) => [line.id, line.words]));
  const copied = editorBlockClipboard(adapter, [...chosen.keys()], false, false, chosen);
  return copied ? { markdown: copied.markdown, html: copied.html } : null;
}

export async function copyEditorBlocks(adapter: NativeEditorGraphAdapter, blockIds: readonly string[], cut = false) {
  const value = editorBlockClipboard(adapter, blockIds, cut);
  return value ? copyTextWithBlocks(value.markdown, value.data, value.html, CLIPBOARD_BOUNDS) : false;
}

// Lines whose words, copied whole, carry the line itself.
const WHOLE_LINE_TYPES = new Set<string>(["paragraph", "heading_1", "heading_2", "heading_3", "heading_4", "bulleted_list_item",
  "numbered_list_item", "to_do", "quote", "toggle", "toggle_heading_1", "toggle_heading_2", "toggle_heading_3", "toggle_heading_4"]);

/**
 * The line whose words a text selection takes all of, when ⌘A chose them, as
 * Notion's copy then carries the line: pasted, it becomes a line of its own
 * after the one the caret is in, while part of a line goes into it (seen
 * 2026-11-07: "날짜 〈date〉 끝" by ⌘A, ⌘C, then ⌘V at another line's end).
 * All of a line's words dragged over copy as words, as Notion's (seen
 * 2026-11-11), which the caller sees to (lineChosenByKey). Only a line
 * holding no blocks, outside a table's cells; null otherwise.
 */
export function wholeLineCopied(root: HTMLElement | null, adapter: NativeEditorGraphAdapter): string | null {
  const selection = document.getSelection();
  if (!root || !selection || selection.isCollapsed || selection.rangeCount !== 1) return null;
  const range = selection.getRangeAt(0);
  const start = range.startContainer instanceof Element ? range.startContainer : range.startContainer.parentElement;
  const field = start?.closest<HTMLElement>("[data-rt-editable]");
  if (!field || !root.contains(field) || field.closest("[data-native-table-cell]") || !field.contains(range.endContainer)) return null;
  const id = field.closest<HTMLElement>("[data-block-id]")?.dataset.blockId;
  const block = id ? adapter.graph.blocks.find((candidate) => candidate.id === id) : undefined;
  if (!id || !block || !WHOLE_LINE_TYPES.has(block.type) || editorBlockChildren(adapter.graph, id).length > 0) return null;
  // No words before the selection or after it in the line.
  const before = document.createRange(), after = document.createRange();
  before.setStart(field, 0);
  before.setEnd(range.startContainer, range.startOffset);
  after.setStart(range.endContainer, range.endOffset);
  after.setEnd(field, field.childNodes.length);
  const words = (part: Range) => part.toString().replace(/[\u200b\ufeff]/g, "");
  return words(before) || words(after) ? null : id;
}

export function writeEditorBlockClipboard(target: DataTransfer, adapter: NativeEditorGraphAdapter, blockIds: readonly string[], cut = false, line = false) {
  const value = editorBlockClipboard(adapter, blockIds, cut, line);
  return value ? writeTextWithBlocks(target, value.markdown, value.data, value.html, CLIPBOARD_BOUNDS) : false;
}

// A pasted page block the server doesn't copy here (copyPastedDatabases) links to its page, which stays where it is.
function pastedType(pasted: PastedBlock): GraphBlockType {
  return pasted.type === "child_page" || pasted.type === "child_database" ? "link_to_page" : pasted.type as GraphBlockType;
}

function pastedContent(pasted: PastedBlock): Record<string, unknown> {
  const { rich: _rich, text: _text, cutPage: _cut, copiedPage: _copied, ...content } = (pasted.content ?? {}) as Record<string, unknown>;
  return content;
}

type Plan = { graph: ContentGraph; operations: Map<string, GraphOperation>; texts: { blockId: string; spans: TextSpan[] }[] };

function record(plan: Plan, graph: ContentGraph, operations: readonly GraphOperation[]) {
  plan.graph = graph;
  for (const operation of operations) {
    if (operation.kind === "upsertBlock") plan.operations.set(operation.block.id, operation);
    if (operation.kind === "upsertView") plan.operations.set(`view:${operation.view.id}`, operation);
  }
}

function place(plan: Plan, pasted: PastedBlock, where: { after: string | null } | { inside: string }): string {
  const type = pastedType(pasted);
  const result = "after" in where
    ? insertEditorBlock(plan.graph, where.after, type, pastedContent(pasted))
    : insertEditorChildBlock(plan.graph, where.inside, type, pastedContent(pasted));
  // A linked view's copy takes copies of its views (blockViewCopies.ts).
  const made = withPastedViewCopies({ graph: result.graph, operations: [...result.operations] });
  record(plan, made.graph, made.operations);
  if (textBlockTypes.has(type)) plan.texts.push({ blockId: result.block.id, spans: pastedBlockTextSpans(pasted) ?? [] });
  for (const child of pasted.children ?? []) place(plan, child, { inside: result.block.id });
  return result.block.id;
}

const COPIED_DATABASE = "copiedDatabase";
/** A copied database block pastes as a copy made on the server, its rows with it. */
function copiedDatabase(block: PastedBlock) {
  const source = (block.content as Record<string, unknown> | undefined)?.[COPIED_DATABASE] as
    { blockId?: unknown; pageId?: unknown; cut?: unknown; workspaceId?: unknown } | undefined;
  if (typeof source?.blockId !== "string" || typeof source.pageId !== "string") return null;
  return { blockId: source.blockId, pageId: source.pageId,
    ...(source.cut === true && typeof source.workspaceId === "string" ? { cutFrom: source.workspaceId } : {}) };
}
const CUT_PAGE = "cutPage";
/** A sub-page's block cut names its page, which a paste brings back rather than links to. */
function cutPage(block: PastedBlock) {
  const pageId = (block.content as Record<string, unknown> | undefined)?.[CUT_PAGE];
  return block.type === "child_page" && typeof pageId === "string" ? pageId : null;
}
const COPIED_PAGE = "copiedPage";
/** A sub-page's block copied names its page, which a paste copies rather than links to. */
function copiedPage(block: PastedBlock) {
  const pageId = (block.content as Record<string, unknown> | undefined)?.[COPIED_PAGE];
  return block.type === "child_page" && typeof pageId === "string" ? pageId : null;
}
/** Whether a paste brings cut databases or pages, which move rather than come from somewhere still there. */
export const pastesCut = (pasted: readonly PastedBlock[]) =>
  pasted.some((block) => copiedDatabase(block)?.cutFrom !== undefined || cutPage(block) !== null);
/** Whether a paste brings blocks the server makes or moves: copied databases and pages, or cut ones. */
export const pastesServerBlocks = (pasted: readonly PastedBlock[]) => pasted.some((block) => madeOnServer(block));
/** Whether a paste brings copied databases and nothing else, which no line of text can take. */
export function holdsOnlyCopiedDatabases(data: DataTransfer) {
  try {
    const pasted = parseInternalPastedBlocks(data, PASTE_BOUNDS);
    return pasted.length > 0 && pasted.every((block) => copiedDatabase(block) !== null);
  } catch {
    return false;
  }
}

/** Whether a pasted block is one the server makes or brings back: a copied or cut database or page. */
function madeOnServer(block: PastedBlock) {
  return copiedDatabase(block) !== null || cutPage(block) !== null || copiedPage(block) !== null;
}

/** Where a paste goes: after `afterId`, or in place of the empty line `replacing`, which `lead` is before. */
export type PastePlace = EditorBlockSpot & { replacing: string | null; lead: string | null };

export function pastePlace(adapter: NativeEditorGraphAdapter, afterId: string | null, replacing: boolean): PastePlace {
  const parentId = afterId ? adapter.graph.blocks.find((block) => block.id === afterId)?.parentId ?? null : null;
  const siblings = editorBlockChildren(adapter.graph, parentId);
  const index = siblings.findIndex((block) => block.id === afterId);
  return { parentId, afterId, replacing: replacing ? afterId : null, lead: replacing && index > 0 ? siblings[index - 1].id : null };
}

/**
 * Puts in at once, in their order, the pasted blocks the page makes itself,
 * leaving out the server's, which come later in their places between them
 * (pastedSpot). Returns by pasted block the block put in for it.
 */
export function placePastedLines(adapter: NativeEditorGraphAdapter, place: PastePlace, pasted: readonly PastedBlock[]) {
  const placed: (string | null)[] = pasted.map(() => null);
  let last = place.afterId, from = 0;
  const putIn = (to: number) => {
    const ids = to > from ? pasteBlocksAfter(adapter, last, pasted.slice(from, to)) : [];
    ids.forEach((id, offset) => { placed[from + offset] = id; });
    last = ids.at(-1) ?? last;
  };
  pasted.forEach((block, index) => {
    if (!madeOnServer(block)) return;
    putIn(index);
    from = index + 1;
  });
  putIn(pasted.length);
  return placed;
}

/** Where a pasted block the server makes goes: after the nearest pasted block before it still there, else where the paste went. */
export function pastedSpot(adapter: NativeEditorGraphAdapter, place: PastePlace, placed: readonly (string | null)[], index: number): EditorBlockSpot {
  const blocks = new Map(adapter.graph.blocks.map((block) => [block.id, block]));
  for (let before = index - 1; before >= 0; before--) {
    const block = blocks.get(placed[before] ?? "");
    if (block) return { parentId: block.parentId, afterId: block.id };
  }
  if (place.afterId && blocks.has(place.afterId)) return { parentId: place.parentId, afterId: place.afterId };
  return { parentId: place.parentId, afterId: place.lead && blocks.has(place.lead) ? place.lead : null };
}

/** Whether a block stands in its spot. */
export function standsAt(graph: ContentGraph, blockId: string, spot: EditorBlockSpot) {
  const siblings = editorBlockChildren(graph, spot.parentId);
  const index = siblings.findIndex((block) => block.id === blockId);
  return index >= 0 && (spot.afterId === null ? index === 0 : siblings[index - 1]?.id === spot.afterId);
}

/**
 * Has the server copy the copied databases and sub-pages among pasted
 * blocks, and bring back the cut ones, one at a time in their order. Each
 * goes where `spot` says before the page reads it, so it first shows there,
 * and `arrived` hears of it once the page has it: null for a page that can't
 * be copied or come back, which pastes as a link to it. Starts none once
 * `wanted` says the paste is gone. Cut database items it brought here are a
 * step of the page's history of their own once they all came (cutRowsPasted).
 */
export async function copyPastedDatabases(adapter: () => NativeEditorGraphAdapter, pasted: readonly PastedBlock[],
  spot: (index: number, blockId: string | null) => EditorBlockSpot | null, arrived: (index: number, blockId: string | null) => void, wanted: () => boolean) {
  const cutRows = cutRowsPasted(adapter);
  try { await copyEach(); } finally { cutRows.settle(); }
  async function copyEach() { for (const [index, block] of pasted.entries()) {
    if (!madeOnServer(block) || !wanted()) continue;
    const where = (blockId: string | null) => spot(index, blockId);
    const page = cutPage(block), copied = copiedPage(block), source = copiedDatabase(block);
    // A page that can't be copied, or come back, pastes as a link to it.
    let made = page ? await adapter().moveCutPageBlock(page, where).catch(() => null)
      : copied ? await adapter().copyPageBlock(copied, where).catch(() => null) : null;
    // A database cut comes back itself, once; pasted again, or gone from the trash, it is copied.
    if (source) {
      const moved = source.cutFrom ? await adapter().moveCutDatabaseBlock({ blockId: source.blockId, pageId: source.pageId, workspaceId: source.cutFrom }, where).catch(() => null) : null;
      made = moved ?? await adapter().copyDatabaseBlock(source, where);
    }
    // The page takes in what the server made a moment after it is read.
    for (let waited = 0; made && waited < 300 && !adapter().graph.blocks.some((candidate) => candidate.id === made); waited++) {
      await new Promise((resolve) => setTimeout(resolve, 10));
    }
    arrived(index, made);
    cutRows.arrived(page ? made : null);
  } }
}

/** What a paste onto selected blocks brings: blocks — `copied` ones, this page's or Notion's, not text or another app's HTML read as blocks —, or a lone link. */
export type SelectionPaste = { blocks: PastedBlock[]; copied: boolean } | { url: string };

/**
 * Reads a paste onto selected blocks as Leafen blocks, then HTML its text
 * would lose something of (pastedHtmlChoice.ts), then Markdown text; a lone
 * link stays a link.
 */
export function readSelectionPaste(data: DataTransfer): SelectionPaste | null {
  try {
    const internal = parseInternalPastedBlocks(data, PASTE_BOUNDS);
    const html = internal.length === 0 && !pastesTextOverHtml(data) ? data.getData("text/html") : "";
    const htmlBlocks = html ? parsePastedHtml(html, PASTE_BOUNDS, undefined, notionCopy(data)) : [];
    const text = data.getData("text/plain");
    if (internal.length === 0 && htmlBlocks.length === 0) {
      if (!text.trim()) return null;
      const url = text.trim().includes("\n") ? "" : normalizePastedLink(text);
      if (url) return { url };
    }
    // One line is read as a line (pastedLine.ts), several as Markdown, as the body's paste reads them.
    const blocks = internal.length > 0 ? internal : htmlBlocks.length > 0 ? htmlBlocks
      : text.trimEnd().includes("\n") ? parsePastedMarkdown(text, PASTE_BOUNDS, { softLineBreaks: true })
        : pastedLineBlocks(text, PASTE_BOUNDS);
    return blocks.length > 0 ? { blocks, copied: internal.length > 0 || (htmlBlocks.length > 0 && notionCopy(data) !== false) } : null;
  } catch {
    return null;
  }
}

/**
 * Puts pasted blocks after a block, or at the page's very start without one,
 * as one change. Returns the blocks put in.
 */
export function pasteBlocksAfter(adapter: NativeEditorGraphAdapter, afterId: string | null, pasted: readonly PastedBlock[]) {
  const document = adapter.collaborationDocument;
  if (!document || pasted.length === 0) return [];
  const plan: Plan = { graph: adapter.graph, operations: new Map(), texts: [] };
  const ids: string[] = [];
  let last = afterId;
  for (const item of pasted) {
    last = place(plan, item, { after: last });
    ids.push(last);
  }
  if (!adapter.changeLayout(() => ({ graph: plan.graph, operations: [...plan.operations.values()] }))) return [];
  for (const { blockId, spans } of plan.texts) setBlockRichText(document, blockId, spans);
  return ids;
}

/**
 * Puts a typed line in place of selected blocks: it goes where the first
 * selected block was and the selected blocks go away, as one change. Returns
 * the last block put in, or null when nothing changed.
 */
export function replaceSelectedBlocks(adapter: NativeEditorGraphAdapter, selection: ReadonlySet<string>, pasted: readonly PastedBlock[]) {
  const document = adapter.collaborationDocument;
  const roots = selectedRootsInOrder(adapter.graph, selection);
  if (!document || roots.length === 0 || pasted.length === 0) return null;
  const plan: Plan = { graph: adapter.graph, operations: new Map(), texts: [] };
  let last = roots[0]!.id;
  for (const item of pasted) last = place(plan, item, { after: last });
  const removed = removeEditorBlocks(plan.graph, roots.map((root) => root.id));
  const tidied = tidyColumns(removed.graph, roots.flatMap((root) => root.parentId ? [root.parentId] : []));
  const operations = [...plan.operations.values(), ...removed.operations, ...tidied.operations];
  if (!adapter.changeLayout(() => ({ graph: tidied.graph, operations }))) return null;
  for (const { blockId, spans } of plan.texts) setBlockRichText(document, blockId, spans);
  return last;
}
