import type { Block } from "@/lib/types";
import { activePersistentGeneratedLabels } from "@/lib/persistentGeneratedLabels";
import { escapeHtml, safeUrl } from "./richtext";
import { calloutCopyIcon } from "./blockIconModel";
import {
  markdownCodeBlock,
  markdownHref,
  markdownTextLiteral,
  pageWikiLink,
  plainRichText,
  spansToMarkdown,
} from "./blockMarkdownInline";
import {
  blockHtml, COPIED_LIST_ITEM_TYPES, copiedListItem, copiedSiblingsHtml, structuredTableHtml, tabLabelHtml, type CopiedListItem,
} from "./blockMarkdownHtml";
import {
  admitBlockMarkdownBlock,
  assertBlockMarkdownChildren,
  assertBlockMarkdownDepth,
  consumeBlockMarkdownInput,
  consumeBlockMarkdownOutput,
  createBlockMarkdownBudget,
  type BlockMarkdownBounds,
  type BlockMarkdownBudget,
} from "./blockMarkdownBudget";

export type { BlockMarkdownBounds } from "./blockMarkdownBudget";

function blockMarkdownWithin(
  block: Block,
  bounds: BlockMarkdownBounds,
) {
  const text = block.type === "code"
    ? plainRichText(block.content?.rich, bounds)
    : spansToMarkdown(block.content?.rich, bounds).trimEnd();
  switch (block.type) {
    case "paragraph": return text;
    case "heading_1": return `# ${text}`;
    case "heading_2": return `## ${text}`;
    case "heading_3": return `### ${text}`;
    case "heading_4": return `#### ${text}`;
    case "toggle_heading_1": return `▶ # ${text}`;
    case "toggle_heading_2": return `▶ ## ${text}`;
    case "toggle_heading_3": return `▶ ### ${text}`;
    case "toggle_heading_4": return `▶ #### ${text}`;
    case "bulleted_list_item": return `- ${text}`;
    case "numbered_list_item": return `1. ${text}`;
    case "to_do": return `- [${block.content?.checked ? "x" : " "}] ${text}`;
    case "toggle": return `▶ ${text}`;
    case "quote": return `> ${text}`;
    // A callout whose icon was taken away has none (Notion's "제거", seen 2026-11-05).
    case "callout": return `> ${`${calloutCopyIconMarkdown(block)} ${text}`.trim()}`;
    case "code": return markdownCodeBlock(text, block.content?.language);
    case "equation": return `$$\n${block.content?.expression ?? text}\n$$`;
    case "divider": return "---";
    case "table":
    case "table_row": return "";
    case "image": {
      const url = safeUrl(block.content?.url);
      return url ? `![${markdownTextLiteral(plainRichText(block.content?.caption, bounds))}](${markdownHref(url)})` : "";
    }
    case "video": {
      const url = safeUrl(block.content?.url);
      return url ? `[Video](${markdownHref(url)})` : "[Video]";
    }
    case "audio": {
      const url = safeUrl(block.content?.url);
      return url ? `[Audio](${markdownHref(url)})` : "[Audio]";
    }
    case "bookmark":
    case "link_preview": {
      const url = safeUrl(block.content?.url);
      return url ? `[${markdownTextLiteral(url)}](${markdownHref(url)})` : "";
    }
    case "embed": {
      const url = safeUrl(block.content?.url);
      return url ? `[Embed](${markdownHref(url)})` : "[Embed]";
    }
    case "pdf": {
      const url = safeUrl(block.content?.url);
      return url ? `[PDF](${markdownHref(url)})` : "[PDF]";
    }
    case "file": {
      const url = safeUrl(block.content?.url);
      return url
        ? `[File: ${markdownTextLiteral(block.content?.name || block.content?.fileName || "File")}](${markdownHref(url)})`
        : markdownTextLiteral(block.content?.name || block.content?.fileName || "File");
    }
    case "child_page":
    case "link_to_page": return pageWikiLink(block.plainText ?? "", block.content?.childPageId, "[[Page]]", bounds);
    case "child_database": return pageWikiLink(block.plainText ?? "", block.content?.childPageId, "[[Database]]", bounds);
    // Its title or "Untitled", as Notion's copy of one (seen 2026-11-04).
    case "inline_database": return markdownTextLiteral(block.plainText || activePersistentGeneratedLabels().untitled);
    case "button": return `[Button: ${markdownTextLiteral(block.content?.buttonLabel ?? block.plainText ?? "New button")}]`;
    case "template": return `[Template: ${markdownTextLiteral(block.plainText ?? "Template")}]`;
    case "table_of_contents": return "[Table of contents]";
    case "breadcrumb": return "[Breadcrumb]";
    case "synced_block": return "[Synced block]";
    case "tab": return "[Tabs]";
    case "column_list":
    case "column": return text;
    default: {
      const exhaustive: never = block.type;
      return exhaustive;
    }
  }
}

function structuredTableMarkdown(
  tableBlock: Block,
  rows: Block[],
  bounds: BlockMarkdownBounds,
) {
  const tableRows = rows.filter((row) => row.type === "table_row");
  if (tableRows.length > bounds.maxTableRows) {
    throw new RangeError("Block table rows exceed their caller-owned bound.");
  }
  // One without width writes nothing, as Notion's exports leave it out.
  const width = Number(tableBlock.content?.tableWidth) || 0;
  if (width < 1) return "";
  if (width > bounds.maxTableColumns) {
    throw new RangeError("Block table columns exceed their caller-owned bound.");
  }
  const rendered = tableRows.map((row) => {
    const cells = Array.isArray(row.content?.cells) ? row.content.cells : [];
    return Array.from({ length: width }, (_, index) => (
      spansToMarkdown(Array.isArray(cells[index]) ? cells[index] : [], bounds)
        .replace(/\|/g, "\\|").replace(/\n/g, "<br>")
    ));
  });
  if (rendered.length === 0) return "";
  const header = rendered[0];
  return [
    `| ${header.join(" | ")} |`,
    `| ${header.map(() => "---").join(" | ")} |`,
    ...rendered.slice(1).map((row) => `| ${row.join(" | ")} |`),
  ].join("\n");
}

function buildChildIndex(
  root: Block,
  blocks: Block[],
  bounds: BlockMarkdownBounds,
  budget: BlockMarkdownBudget,
) {
  const unique = new Map<string, Block>();
  unique.set(root.id, root);
  for (const block of blocks) {
    const existing = unique.get(block.id);
    if (existing && existing !== block) throw new RangeError("Duplicate block IDs are not admitted.");
    unique.set(block.id, block);
  }
  if (unique.size > bounds.maxBlocks) {
    throw new RangeError("Block markdown blocks exceed their caller-owned bound.");
  }
  for (const block of unique.values()) admitBlockMarkdownBlock(block, bounds, budget);
  const childrenByParent = new Map<string, Block[]>();
  for (const block of unique.values()) {
    if (block.id === root.id || !block.parentId) continue;
    const children = childrenByParent.get(block.parentId) ?? [];
    assertBlockMarkdownChildren(children.length + 1, bounds);
    children.push(block);
    childrenByParent.set(block.parentId, children);
  }
  for (const children of childrenByParent.values()) {
    children.sort((a, b) => a.position - b.position);
  }
  return childrenByParent;
}

function tabLabelMarkdown(
  block: Block,
  bounds: BlockMarkdownBounds,
) {
  const icon = typeof block.content?.icon === "string" ? block.content.icon.trim() : "";
  if (!icon) return "";
  const label = blockMarkdownWithin(block, bounds).trim()
    || activePersistentGeneratedLabels().untitled;
  return `[Tab: ${markdownTextLiteral(icon)} ${label}]`;
}

// What a copy of blocks reads as where only text goes, as Notion's copy
// (each kind copied, seen 2026-11-01): blocks a blank line apart, list items
// (bulleted, numbered, to-do, toggle) one line apart; what a list item holds
// 4 spaces in, between blank lines unless it is a list itself; a line of
// text's and a toggle heading's blocks after it; columns and synced blocks
// give what they hold;
// a table of contents and a breadcrumb give nothing. An empty line gives
// nothing, the blocks around it a blank line apart, and no blank lines run
// longer than one, in a block's words or code too (seen 2026-11-11: "a\n\nb"
// for a line, two empty ones and b; "c1\n\nc2" for code "c1\n\n\n\nc2").
const oneBlankLine = (text: string) => text.replace(/\n{3,}/g, "\n\n");
const COPY_LIST_TYPES = new Set<Block["type"]>(["bulleted_list_item", "numbered_list_item", "to_do", "toggle"]);
const COPY_THROUGH_TYPES = new Set<Block["type"]>(["column_list", "column", "synced_block"]);
const COPY_SKIPPED_TYPES = new Set<Block["type"]>(["table_of_contents", "breadcrumb"]);
const COPY_HEADING_LEVELS: Partial<Record<Block["type"], number>> = {
  heading_1: 1, heading_2: 2, heading_3: 3, heading_4: 4,
  toggle_heading_1: 1, toggle_heading_2: 2, toggle_heading_3: 3, toggle_heading_4: 4,
};

export function blocksCopyMarkdown(roots: Block[], blocks: Block[], given: BlockMarkdownBounds) {
  if (roots.length === 0) return "";
  const bounds = { ...given, copied: true };
  const budget = createBlockMarkdownBudget(bounds);
  const childrenByParent = buildChildIndex(roots[0], blocks, bounds, budget);
  const visited = new Set<string>();
  const byId = new Map(blocks.map((block) => [block.id, block]));
  const indent = (text: string) => text.split("\n").map((line) => `    ${line}`).join("\n");
  // What a block holds, its columns and synced blocks opened, its contents
  // and breadcrumbs left out. What a column or synced block holds stands a
  // blank line apart from the blocks around it, list items too (Notion's,
  // seen 2026-11-15: "- a\n\n- b" for a column's last item and the next one's first).
  type Shown = { block: Block; depth: number; apart?: boolean };
  // What a line of text holds comes after it as blocks of its own, not
  // indented, its lists one with the blocks after them (Notion's, seen
  // 2026-11-16 twice, made through its API and on screen: "Root\n\nA\n\nB\n\nSecond"
  // for Root holding A and B; "P3\n\n- D3\n- M4"; in a list item, "- M5\n    \n    Z\n    \n    - Z1\n- M6").
  const holdsInLine = (block: Block) => block.type === "paragraph" && byId.get(block.parentId ?? "")?.type !== "tab";
  const shown = (siblings: Block[], depth: number): Shown[] => {
    const out: Shown[] = [];
    let apart = false;
    for (const block of siblings) {
      if (COPY_SKIPPED_TYPES.has(block.type)) continue;
      if (!COPY_THROUGH_TYPES.has(block.type)) {
        out.push({ block, depth, apart });
        apart = false;
        const held = holdsInLine(block) ? childrenByParent.get(block.id) ?? [] : [];
        if (held.length) {
          assertBlockMarkdownDepth(depth + 1, bounds);
          out.push(...shown(held, depth + 1));
        }
        continue;
      }
      assertBlockMarkdownDepth(depth + 1, bounds);
      const held = shown(childrenByParent.get(block.id) ?? [], depth + 1);
      if (held.length) held[0] = { ...held[0], apart: true };
      out.push(...held);
      apart = true;
    }
    return out;
  };
  const forest = (siblings: Shown[]): string => {
    let text = "", previous: Block | undefined, number = 0;
    for (const { block, depth, apart } of siblings) {
      // Copied, a numbered list counts from 1 whatever starts its items
      // have, as Notion's (seen 2026-11-10: "1. 2. 3. 4." for 1. 2. 7. 8.):
      // the items one after another in one block (2026-11-16: "1. K1\n1. K2"
      // for K1 held by the line before K2).
      number = block.type !== "numbered_list_item" ? 0
        : previous?.type === "numbered_list_item" && (previous.parentId ?? "") === (block.parentId ?? "") ? number + 1 : 1;
      if (previous) text += !apart && COPY_LIST_TYPES.has(previous.type) && COPY_LIST_TYPES.has(block.type) ? "\n" : "\n\n";
      text += one(block, depth, number);
      previous = block;
    }
    return oneBlankLine(text);
  };
  // What a list item or a quote holds: between blank lines where it isn't a
  // list, ending with one line break at most (Notion's, seen 2026-11-11: an
  // empty line first gives a second blank line before the next, one last
  // gives none).
  const held = (children: Shown[]) => {
    const first = children[0].block, last = children[children.length - 1].block;
    const text = oneBlankLine(`${COPY_LIST_TYPES.has(first.type) ? "" : "\n"}${forest(children)}${COPY_LIST_TYPES.has(last.type) ? "" : "\n"}`);
    return text.endsWith("\n\n") ? text.slice(0, -1) : text;
  };
  const one = (block: Block, depth: number, number: number): string => {
    assertBlockMarkdownDepth(depth, bounds);
    if (visited.has(block.id)) throw new RangeError("Cyclic block trees are not admitted.");
    visited.add(block.id);
    const children = block.type === "table" || holdsInLine(block) ? [] : shown(childrenByParent.get(block.id) ?? [], depth + 1);
    // A line's words as Notion's copy gives them (seen 2026-11-11): a
    // paragraph's and a numbered item's with their spaces, only line breaks
    // at the end gone ("trail  ", "   ", "1.   nx  "); the rest's trimmed
    // ("## hx", "- b", "- [ ]  tx", "> qx").
    const words = spansToMarkdown(block.content?.rich, bounds), kept = words.replace(/\n+$/u, ""), text = words.trim();
    const level = COPY_HEADING_LEVELS[block.type];
    if (level) return `${"#".repeat(level)} ${text}${children.length ? `\n\n${forest(children)}` : ""}`;
    // A quote's own words after "> " as they are, then a line of "> " for each
    // line of what it holds, or one alone (Notion's, seen 2026-11-11: "> a\nb\n> ").
    if (block.type === "quote") {
      return `> ${text}\n${(children.length ? `\n${held(children)}` : "").split("\n").map((line) => `> ${line}`).join("\n")}`;
    }
    // A callout as Notion's copy writes one made on screen (seen 2026-11-07):
    // "<aside>" with its icon on the next line, then each of its lines as a
    // paragraph, then "</aside>"; no icon, no line for one. Its own text, as a
    // callout made through Notion's API has, goes beside the icon.
    if (block.type === "callout") {
      const head = `${calloutCopyIconMarkdown(block)} ${text}`.trim();
      return [`<aside>${head ? `\n${head}` : ""}`, ...(children.length ? [forest(children)] : []), "</aside>"].join("\n\n");
    }
    const line = block.type === "bulleted_list_item" || block.type === "toggle" ? `- ${text}`
      : block.type === "numbered_list_item" ? `${number}. ${kept}`
      : block.type === "to_do" ? `- [${block.content?.checked ? "x" : " "}]  ${text}`
      : block.type === "table" ? structuredTableMarkdown(block, childrenByParent.get(block.id) ?? [], bounds)
      : block.type === "paragraph" && byId.get(block.parentId ?? "")?.type === "tab"
        ? tabLabelMarkdown(block, bounds) || text
      : block.type === "paragraph" ? kept
      // A page link or page reads as its page's title (Notion's, seen 2026-11-06), not
      // the wiki link a Markdown file keeps.
      : block.type === "link_to_page" || block.type === "child_page" || block.type === "child_database"
        ? markdownTextLiteral(block.plainText?.trim() || (typeof block.content?.childPageTitle === "string" && block.content.childPageTitle.trim())
          || activePersistentGeneratedLabels().untitled)
      : copyMediaMarkdown(block, bounds) ?? blockMarkdownWithin(block, bounds);
    return children.length ? `${line}\n${indent(held(children))}` : line;
  };
  // Nothing blank is left at either end, as Notion's (seen 2026-11-05: "> qu\n>", "- bu\n    \n    bin";
  // 2026-11-11: no line break where empty lines start it, "lead" for "  lead", nothing for a last "   ").
  return consumeBlockMarkdownOutput(forest(shown(roots, 0)).trim(), bounds, budget);
}

// A copied file or frame reads as its address, a file as its name, a web
// bookmark as a link, an uploaded picture with no words as "!" and its name, one
// from an address as an empty picture link (Notion's, seen 2026-11-05). A caption
// comes as words, twice for what shows no link, after the link for a bookmark.
function copyMediaMarkdown(block: Block, bounds: BlockMarkdownBounds) {
  if (!["image", "embed", "video", "audio", "pdf", "file", "bookmark", "link_preview"].includes(block.type)) return undefined;
  const url = safeUrl(block.content?.url) ?? "";
  const caption = markdownTextLiteral(plainRichText(block.content?.caption, bounds));
  // A bookmark reads as its page's title, or its address with none, as Notion's (seen 2026-11-07).
  if (block.type === "bookmark") {
    const stored = (block.content as Record<string, unknown> | undefined)?.title;
    const title = typeof stored === "string" ? stored.trim() : "";
    const name = markdownTextLiteral(title || block.plainText || url);
    return caption ? `${name}\n\n${caption}` : name;
  }
  if (block.type === "link_preview") {
    const link = url ? `[${markdownTextLiteral(block.plainText ?? "")}](${markdownHref(url)})` : "";
    return caption ? `${link}\n\n${caption}` : link;
  }
  if (caption) return `${caption}\n\n${caption}`;
  if (block.type === "image") {
    const name = block.content?.name || block.content?.fileName;
    return name ? `!${name}` : url ? `![](${markdownHref(url)})` : "";
  }
  if (block.type === "file") return block.content?.name || block.content?.fileName || url;
  // An uploaded video, audio or PDF reads as its file's name, as Notion's (seen 2026-11-07).
  return url || block.content?.name || block.content?.fileName || "";
}

// A block and what it holds as HTML for other apps, a list item as its
// parts for its list to put with the items beside it (copiedSiblingsHtml).
function blockTreePiece(root: Block, blocks: Block[], bounds: BlockMarkdownBounds): Array<string | CopiedListItem> {
  const budget = createBlockMarkdownBudget(bounds);
  const childrenByParent = buildChildIndex(root, blocks, bounds, budget);
  const pieceById = new Map<string, Array<string | CopiedListItem>>();
  const stack: Array<{ block: Block; depth: number; parent?: Block; expanded: boolean }> = [
    { block: root, depth: 0, expanded: false },
  ];
  const active = new Set<string>();
  while (stack.length) {
    const current = stack.pop();
    if (!current) break;
    assertBlockMarkdownDepth(current.depth, bounds);
    if (!current.expanded) {
      if (active.has(current.block.id)) throw new RangeError("Cyclic block trees are not admitted.");
      active.add(current.block.id);
      stack.push({ ...current, expanded: true });
      const children = childrenByParent.get(current.block.id) ?? [];
      for (let index = children.length - 1; index >= 0; index -= 1) {
        stack.push({ block: children[index], depth: current.depth + 1, parent: current.block, expanded: false });
      }
      continue;
    }
    active.delete(current.block.id);
    const children = childrenByParent.get(current.block.id) ?? [];
    const pieces = children.flatMap((child) => pieceById.get(child.id) ?? []);
    // As in its copy's Markdown (Notion's, seen 2026-11-15): a column's or a
    // synced block's blocks a blank line apart from those around them, with no
    // box of their own (another app showed an empty column as a blank line);
    // a table of contents or a breadcrumb nothing.
    if (COPY_SKIPPED_TYPES.has(current.block.type) || COPY_THROUGH_TYPES.has(current.block.type)) {
      pieceById.set(current.block.id, COPY_SKIPPED_TYPES.has(current.block.type) ? [] : ["", ...pieces, ""]);
      continue;
    }
    const childrenHtml = copiedSiblingsHtml(pieces);
    if (COPIED_LIST_ITEM_TYPES.has(current.block.type)) {
      // What it holds besides lists, and whether any of that shows.
      const blocksHeld = children.filter((child) => !COPIED_LIST_ITEM_TYPES.has(child.type));
      const item = copiedListItem(current.block, childrenHtml, {
        holdsBlocks: blocksHeld.length > 0, showsBlocks: blocksHeld.some((child) => (pieceById.get(child.id) ?? []).some(Boolean)),
      }, bounds);
      consumeBlockMarkdownOutput(`${item.words}${item.held}${item.after}`, bounds, budget);
      pieceById.set(current.block.id, [item]);
      continue;
    }
    const html = current.block.type === "table"
      ? structuredTableHtml(current.block, children, bounds, budget)
      : current.parent?.type === "tab" && current.block.type === "paragraph"
      ? tabLabelHtml(current.block, childrenHtml, bounds)
      : blockHtml(current.block, childrenHtml, bounds, budget);
    pieceById.set(current.block.id, [consumeBlockMarkdownOutput(html, bounds, budget)]);
  }
  return pieceById.get(root.id) ?? [];
}

/** Blocks side by side, each with what it holds, as HTML for other apps: list items next to each other one list. */
export function blocksTreeHtml(roots: Block[], blocks: Block[], bounds: BlockMarkdownBounds) {
  return copiedSiblingsHtml(roots.flatMap((root) => blockTreePiece(root, blocks, bounds)));
}

export function blockTreeHtml(root: Block, blocks: Block[], bounds: BlockMarkdownBounds) {
  return blocksTreeHtml([root], blocks, bounds);
}

export function blocksClipboardHtml(
  innerHtml: string,
  bounds: BlockMarkdownBounds,
  copyId?: string,
) {
  const budget = createBlockMarkdownBudget(bounds);
  consumeBlockMarkdownInput(innerHtml, bounds, budget);
  if (copyId) consumeBlockMarkdownInput(copyId, bounds, budget);
  const generation = copyId ? ` data-leafen-copy-id="${escapeHtml(copyId)}"` : "";
  return consumeBlockMarkdownOutput(
    `<html><body><!--StartFragment--><div data-leafen-copy="true"${generation}>${innerHtml}</div><!--EndFragment--></body></html>`,
    bounds,
    budget,
  );
}

// A callout's icon in a Markdown copy: its emoji, or a picture as Notion writes one.
function calloutCopyIconMarkdown(block: Block) {
  const icon = calloutCopyIcon(block.content as Record<string, unknown> | undefined);
  if (!icon) return "";
  return "src" in icon ? `<img src="${escapeHtml(icon.src)}" alt="" width="40px" />` : icon.text;
}
