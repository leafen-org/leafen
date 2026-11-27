import type { CSSProperties } from "react";
import { graphTextBlockTypes, type ContentGraph, type GraphBlock } from "@/contentGraph";
import { BLOCK_TEXT_ROOT_PREFIX } from "@/lib/blockTextEditing";
import { countedSpans, countedText } from "@/lib/countedText";
import { countCharacters, countWords } from "@/lib/rowMenuDocumentStats";
import { editorBlockChildren } from "./editorBlockIndex";
import { staticProjectionSpans } from "./nativeTiptapGraph";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";

export const BLOCK_HANDLE_COLORS = [
  "default", "gray", "brown", "orange", "yellow", "green", "blue", "purple", "pink", "red",
] as const;

export const BLOCK_HANDLE_BACKGROUND_COLORS = [
  "gray_background", "brown_background", "orange_background", "yellow_background", "green_background",
  "blue_background", "purple_background", "pink_background", "red_background",
] as const;

const BLOCK_TEXT_COLORS = new Set<string>(BLOCK_HANDLE_COLORS.filter((color) => color !== "default"));
const BLOCK_BACKGROUND_COLORS = new Set<string>(BLOCK_HANDLE_BACKGROUND_COLORS);

/** A block's color token as theme colors: a text color or a background. */
export function blockColorStyle(color: unknown): CSSProperties | undefined {
  if (typeof color !== "string") return undefined;
  if (BLOCK_TEXT_COLORS.has(color)) return { color: `var(--c-${color})` };
  if (BLOCK_BACKGROUND_COLORS.has(color)) {
    return { background: `var(--c-bg-${color.slice(0, -"_background".length)})`, borderRadius: 3 };
  }
  return undefined;
}

/** A table cell's color as theme colors: its text, or its whole background. */
export function tableCellColorStyle(color: string): CSSProperties | undefined {
  if (BLOCK_TEXT_COLORS.has(color)) return { color: `var(--c-${color})` };
  if (BLOCK_BACKGROUND_COLORS.has(color)) return { background: `var(--c-bg-${color.slice(0, -"_background".length)})` };
  return undefined;
}

/** The address fragment that points at a block, as Notion's block links do. */
export function blockAnchorId(blockId: string) {
  return `block-${blockId}`;
}

const chosenRoots = new WeakMap<ReadonlySet<string>, { blocks: readonly GraphBlock[]; ids: string[]; roots: ReadonlySet<string> }>();

/**
 * The blocks a handle's menu is for — what its head names and counts, its rows
 * fit and its actions change: the pressed block alone, or, where it is chosen
 * with others, each chosen block not inside another chosen one, in the order
 * chosen. A range walked past a line's own lines takes them too, but a chosen
 * block holds what is in it, as Notion's (seen 2026-11-26 in Chrome at 1440,
 * two tabs, the screen after the press: two text lines, a to-do inside the
 * first, chosen with Shift+↑ from the second, head "텍스트"; a heading and such
 * a line "블록 2개"; "전환 › 글머리 기호 목록" or "번호 매기기 목록" and
 * "회색 배경" changed the two lines, not the to-do; once, ⌘A over twelve
 * blocks, two holding one each, "블록 12개"). A press on a block inside a
 * chosen one, drawn chosen with it, is for those chosen where they are
 * several; where they are one, whose own rows that handle cannot give, for the
 * pressed block alone. Every chosen block's handle asks; they share one answer
 * per selection and graph.
 */
export function blockMenuSelection(graph: Pick<ContentGraph, "blocks">, block: GraphBlock, selection: ReadonlySet<string>) {
  if (selection.size < 2 || !selection.has(block.id)) return [block.id];
  let chosen = chosenRoots.get(selection);
  if (!chosen || chosen.blocks !== graph.blocks) {
    const parents = new Map(graph.blocks.map((one) => [one.id, one.parentId]));
    const held = (id: string) => {
      const seen = new Set<string>();
      for (let parent = parents.get(id); parent && !seen.has(parent); parent = parents.get(parent)) {
        if (selection.has(parent)) return true;
        seen.add(parent);
      }
      return false;
    };
    const ids = [...selection].filter((id) => !held(id));
    chosen = { blocks: graph.blocks, ids, roots: new Set(ids) };
    chosenRoots.set(selection, chosen);
  }
  return chosen.roots.has(block.id) || chosen.ids.length > 1 ? chosen.ids : [block.id];
}

/** Blocks' words and characters, as the block menu's foot counts them; an emoji is one character. */
export function blockTextCounts(texts: readonly string[]) {
  let words = 0;
  let chars = 0;
  for (const text of texts) {
    words += countWords(text);
    chars += countCharacters(text);
  }
  return { words, chars };
}

/**
 * A block's own texts as the block menu's foot counts them: a text block's
 * words, mentions and equations as drawn (countedText.ts), a sub-page's
 * title, a simple table's cells (seen 2026-11-07) — but not a full-page
 * database's line's title, nor an inline database's, as Notion's (seen
 * 2026-11-26 in Chrome, two tabs at different times, from the handle: a
 * database and a line count the line alone, two inline databases nothing). Any
 * other block has none of its own and counts what it holds: one synced block
 * chosen counts its lines, as Notion's (seen 2026-11-18 in Chrome at 1440:
 * "6개의 단어, 15자", its two lines' words and characters); a divider, a
 * picture or a file, holding no text, shows no count at all (2026-11-13).
 */
export function blockMenuOwnTexts(adapter: NativeEditorGraphAdapter, block: GraphBlock,
  pageOf: (block: GraphBlock) => { title?: string; kind?: string } | undefined): string[] {
  if (block.type === "child_page") { const page = pageOf(block); return page?.kind === "database" ? [] : [page?.title ?? ""]; }
  if (block.type === "table") {
    return adapter.graph.blocks.filter((row) => row.parentId === block.id && row.type === "table_row")
      .flatMap((row) => (Array.isArray(row.content.cells) ? row.content.cells : []).map(countedSpans));
  }
  if (!(graphTextBlockTypes as readonly string[]).includes(block.type)) return [];
  return [countedText(adapter.collaborationDocument?.getText(BLOCK_TEXT_ROOT_PREFIX + block.id))];
}

/**
 * What the block menu's foot counts for the chosen blocks, as Notion's from
 * the handle (seen 2026-11-13): each block's own text and everything inside
 * it — a callout's lines, a toggle's down to its grandchildren, a list item's
 * nested items — once, however many of them are chosen. `own` gives a block's
 * own texts; a simple table's are its cells and a sub-page's its title, what
 * they hold counted through them.
 */
export function blockMenuCountedTexts(graph: ContentGraph, ids: readonly string[], own: (block: GraphBlock) => readonly string[]) {
  const chosen = new Set(ids);
  const byId = new Map(graph.blocks.map((block) => [block.id, block]));
  const texts: string[] = [];
  const add = (block: GraphBlock) => {
    texts.push(...own(block));
    if (block.type === "table" || block.type === "child_page") return;
    for (const child of editorBlockChildren(graph, block.id)) add(child);
  };
  for (const id of ids) {
    const block = byId.get(id);
    const seen = new Set<string>();
    let inside = false;
    for (let parent = block?.parentId; parent && !inside && !seen.has(parent); parent = byId.get(parent)?.parentId) {
      seen.add(parent);
      inside = chosen.has(parent);
    }
    if (block && !inside) add(block);
  }
  return texts;
}

/**
 * The blocks a color chosen for these goes to. A callout's first line, in a
 * callout with no text of its own, stands for the callout's text — its icon
 * is beside it and the callout's handle shows there (EditorRichBlocks) — so
 * the color goes to the callout, as Notion's ⌘/ colors the callout from it
 * (seen 2026-11-15 and 2026-11-16: "파란색 배경" and "초록색 텍스트" from
 * its first line colored the callout; from its second line, that line).
 */
export function blockColorTargets(graph: ContentGraph, ids: readonly string[]): string[] {
  const byId = new Map(graph.blocks.map((block) => [block.id, block]));
  const targets = ids.map((id) => {
    const parent = byId.get(byId.get(id)?.parentId ?? "");
    if (parent?.type !== "callout" || editorBlockChildren(graph, parent.id)[0]?.id !== id) return id;
    const own = staticProjectionSpans(parent.content.rich)
      ?? (typeof parent.content.text === "string" ? [{ text: parent.content.text }] : []);
    return own.length ? id : parent.id;
  });
  return [...new Set(targets)];
}

/**
 * What a color chosen for these blocks changes: each target (blockColorTargets)
 * takes it, and a text color — "기본 텍스트" too — takes every color out of
 * what a target holds, its lines and theirs then showing its color, while a
 * background color leaves them theirs, as Notion's ⌘/ (seen 2026-11-16: from
 * a callout's and a toggle's own line "초록색 텍스트" set the lines under it
 * to default — a blue background, a red text and a grandchild's red
 * background among them — and "빨간색 배경" kept a blue background and a
 * green text under it; "/초록색" from a toggle's line the same). A block
 * chosen itself keeps the color it is given.
 */
export function blockColorChanges(graph: ContentGraph, ids: readonly string[], color: string) {
  const targets = blockColorTargets(graph, ids), chosen = new Set(targets);
  const byId = new Map(graph.blocks.map((block) => [block.id, block]));
  const changes = new Map<string, string>();
  const set = (block: GraphBlock | undefined, next: string) => {
    if (block && String(block.content.color ?? "default") !== next) changes.set(block.id, next);
  };
  const clear = (id: string) => {
    for (const child of editorBlockChildren(graph, id)) {
      if (!chosen.has(child.id)) set(child, "default");
      clear(child.id);
    }
  };
  for (const id of targets) {
    set(byId.get(id), color);
    if (!color.endsWith("_background")) clear(id);
  }
  return [...changes].map(([id, next]) => ({ id, color: next }));
}

/** Gives these blocks a color as blockColorChanges says, each as the adapter holds it then: the block menu's, ⌘⇧H's and the "/" menu's. */
export function applyBlockColor(adapter: Pick<NativeEditorGraphAdapter, "graph" | "updateBlock">, ids: readonly string[], color: string) {
  for (const change of blockColorChanges(adapter.graph, ids, color)) {
    const block = adapter.graph.blocks.find((item) => item.id === change.id);
    if (block) adapter.updateBlock(block, { color: change.color === "default" ? undefined : change.color });
  }
}
