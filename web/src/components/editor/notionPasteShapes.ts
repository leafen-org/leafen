import { NEW_CALLOUT_COLOR } from "@/lib/blockDefaults";
import type { BlockContent, TextSpan } from "@/lib/types";
import type { PastedBlock } from "./markdownPasteTypes";

/*
 * The shapes Notion's copy gives other apps for blocks they cannot hold, read
 * back as those blocks (seen 2026-11-05): a to-do is a list item "[x]  text",
 * a callout is lines between "<aside>" and "</aside>" with its icon first, an
 * equation is lines between "$$", in its HTML and its Markdown alike.
 * Leafen's own Markdown copy and export write a callout the same way.
 */

const ICON = /^((?:\p{Extended_Pictographic}|\p{Emoji_Presentation})(?:️|⃣|[\u{1F3FB}-\u{1F3FF}]|‍(?:\p{Extended_Pictographic}|\p{Emoji_Presentation}))*)(?:\s+|$)/u;

export const isAsideOpen = (line: string) => /^<aside>$/i.test(line.trim());
export const isAsideClose = (line: string) => /^<\/aside>$/i.test(line.trim());

/** "[x] " or "[ ] " starting a list item's text. */
export function taskMark(text: string) {
  const match = text.match(/^\[([ xX])\](?:\s+|$)/);
  return match ? { checked: match[1] !== " ", length: match[0].length } : null;
}

// A picture icon, as Notion writes an uploaded one: <img src="attachment:…" alt="…" width="40px" /> (seen 2026-11-07).
const PICTURE = /^<img\s[^>]*?\bsrc="([^"]*)"[^>]*>(?:\s+|$)/iu;

type CalloutIcon = { icon: string; length: number; picture?: boolean };

/**
 * The icon a callout's first line starts with, as Notion writes it: an emoji,
 * or a picture as an <img> tag — kept when its address is on the web, as a
 * linked picture is; Notion's own attachment address leads nowhere outside it.
 */
export function calloutIcon(text: string): CalloutIcon | null {
  const picture = text.match(PICTURE);
  if (picture) {
    const src = picture[1]!.replace(/&amp;/gu, "&").replace(/&quot;/gu, "\"");
    return { icon: /^https?:\/\//iu.test(src) ? src : "", length: picture[0].length, picture: true };
  }
  const match = text.match(ICON);
  return match ? { icon: match[1]!, length: match[0].length } : null;
}

/** A callout holding its lines, as one made on the screen (no icon written: none). */
export function calloutContent(icon: Pick<CalloutIcon, "icon" | "picture"> | null): BlockContent {
  if (icon?.picture && icon.icon) return { rich: [], icon: icon.icon, iconType: "external", color: NEW_CALLOUT_COLOR } as BlockContent;
  return icon?.icon && !icon.picture
    ? { rich: [], icon: icon.icon, color: NEW_CALLOUT_COLOR }
    : { rich: [], icon: "", iconType: "none", color: NEW_CALLOUT_COLOR } as BlockContent;
}

/** "$$" lines around an expression, or "$$expression$$" on one line. */
export function equationExpression(text: string) {
  const lines = text.trim().split("\n").map(line => line.trim());
  if (lines.length >= 3 && lines[0] === "$$" && lines.at(-1) === "$$") {
    const expression = lines.slice(1, -1).join("\n").trim();
    return expression || null;
  }
  const single = lines.length === 1 ? lines[0]!.match(/^\$\$(.+)\$\$$/) : null;
  return single ? single[1]!.trim() || null : null;
}

export function dropLeadingText(spans: readonly TextSpan[], count: number) {
  const next: TextSpan[] = [];
  let left = count;
  for (const span of spans) {
    if (left >= span.text.length) { left -= span.text.length; continue; }
    next.push(left ? { ...span, text: span.text.slice(left) } : span);
    left = 0;
  }
  return next;
}

// A paragraph's lines as it reads them, its breaks kept.
export function htmlLines(node: Node) {
  let text = "";
  const walk = (current: Node) => {
    for (const child of current.childNodes) {
      if (child.nodeType === Node.TEXT_NODE) text += (child.textContent ?? "").replace(/\s+/g, " ");
      else if (child instanceof HTMLElement && child.tagName.toLowerCase() === "br") text += "\n";
      else walk(child);
    }
  };
  walk(node);
  return text.split("\n").map((line) => line.trim()).filter(Boolean);
}

// The paragraph without its "<aside>" first line or its "</aside>" last one.
function withoutAsideLine(element: Element, end: "start" | "end") {
  const clone = element.cloneNode(true) as Element;
  const walker = clone.ownerDocument.createTreeWalker(clone, NodeFilter.SHOW_TEXT);
  const texts: Node[] = [];
  for (let text = walker.nextNode(); text; text = walker.nextNode()) if (text.textContent?.trim()) texts.push(text);
  const text = end === "start" ? texts[0] : texts.at(-1);
  if (!text) return clone;
  text.textContent = end === "start"
    ? (text.textContent ?? "").replace(/^\s*<aside>/i, "")
    : (text.textContent ?? "").replace(/<\/aside>\s*$/i, "");
  if (!(text.textContent ?? "").trim()) {
    const next = (node: Node) => end === "start" ? node.nextSibling : node.previousSibling;
    let sibling = next(text);
    while (sibling?.nodeType === Node.TEXT_NODE && !sibling.textContent?.trim()) sibling = next(sibling);
    if (sibling instanceof Element && sibling.tagName.toLowerCase() === "br") sibling.remove();
    text.parentNode?.removeChild(text);
  }
  return clone;
}

// Notion's callout: from a paragraph whose first line is "<aside>" to the one
// whose last line closes it, an aside inside it read again inside.
export function asideRun(nodes: readonly ChildNode[], start: number) {
  const first = nodes[start];
  if (!(first instanceof HTMLElement) || !["p", "div"].includes(first.tagName.toLowerCase())) return null;
  const firstLines = htmlLines(first);
  if (!isAsideOpen(firstLines[0] ?? "")) return null;
  const inner: Node[] = [];
  let open = 0;
  for (let index = start; index < nodes.length; index++) {
    const node = nodes[index]!;
    if (!(node instanceof Element)) {
      if (node.nodeType === Node.TEXT_NODE && node.textContent?.trim()) inner.push(node.cloneNode(true));
      continue;
    }
    const lines = index === start ? firstLines : htmlLines(node);
    const opens = index === start || isAsideOpen(lines[0] ?? "");
    const closes = isAsideClose(lines.at(-1) ?? "") && !(opens && lines.length === 1);
    let piece: Element = index === start ? withoutAsideLine(node, "start") : node.cloneNode(true) as Element;
    if (opens) open++;
    if (closes) open--;
    if (closes && open === 0) piece = withoutAsideLine(piece, "end");
    if (piece.textContent?.trim() || piece.querySelector("img,hr,table,pre")) inner.push(piece);
    if (open === 0) return { inner, end: index };
  }
  return null;
}

/**
 * Notion's own copy of blocks (notionCopy in pastedHtmlChoice.ts): each
 * block's words as its own clipboard kind holds them
 * (text/_notion-blocks-v3-production), in the order the page shows them,
 * children after their block. Its HTML drops the spaces at a block's ends,
 * which that kind keeps and Notion's paste of the copy keeps (seen
 * 2026-11-16 in Chrome, in two tabs: ' Plain `코드`⇧↵Next ', ' item **bold**
 * end ', a heading ' Head ' and '  two lead and trail  ' copied as blocks and
 * pasted in Notion kept every edge space; the HTML had none).
 */
export type NotionCopy = { titles: readonly string[] };

const NOTION_COPY_MOST_BLOCKS = 5000;

/** The blocks' words in Notion's own copy, or none where it can't be read. */
export function notionCopyTitles(json: string): string[] {
  const titles: string[] = [];
  try {
    const copy = JSON.parse(json) as { blocks?: Array<{ blockId?: string; blockSubtree?: { block?: Record<string, { value?: NotionCopyBlock }> } }> };
    for (const entry of Array.isArray(copy.blocks) ? copy.blocks : []) {
      const subtree = entry.blockSubtree?.block ?? {};
      const walk = (id: string | undefined, depth: number) => {
        const value = id ? subtree[id]?.value : undefined;
        if (!value || depth > 64 || titles.length >= NOTION_COPY_MOST_BLOCKS) return;
        const title = value.properties?.title;
        titles.push(Array.isArray(title) ? title.map(piece => Array.isArray(piece) && typeof piece[0] === "string" ? piece[0] : "").join("") : "");
        for (const child of Array.isArray(value.content) ? value.content : []) walk(child, depth + 1);
      };
      walk(entry.blockId, 0);
    }
  } catch {
    return [];
  }
  return titles;
}

type NotionCopyBlock = { properties?: { title?: unknown[] }; content?: string[] };

const EDGE_START = /^[ \t ]+/u, EDGE_END = /[ \t ]+$/u;

/**
 * Blocks read from Notion's copy's HTML with the spaces at their ends that
 * its own kind keeps: a block takes them from the next title, in order, whose
 * words are its words but for them. A space goes in its own unmarked piece,
 * as Notion's title keeps it apart from a mark beside it.
 */
export function withNotionCopyEdges(blocks: PastedBlock[], copy: NotionCopy): PastedBlock[] {
  let at = 0;
  const edged = (block: PastedBlock): PastedBlock => {
    let next = block;
    const rich = block.content?.rich, text = block.plainText ?? "";
    if (Array.isArray(rich) && rich.length && text.trim()) {
      const found = copy.titles.findIndex((title, index) => index >= at && title.trim() === text.trim());
      if (found >= 0) {
        at = found + 1;
        const title = copy.titles[found]!;
        const start = EDGE_START.test(text) ? "" : title.match(EDGE_START)?.[0] ?? "";
        const end = EDGE_END.test(text) ? "" : title.match(EDGE_END)?.[0] ?? "";
        if (start || end) {
          const spans = [...(start ? [{ text: start }] : []), ...rich, ...(end ? [{ text: end }] : [])];
          next = { ...block, content: { ...block.content, rich: coalesceEdges(spans) }, plainText: start + text + end };
        }
      }
    }
    return block.children?.length ? { ...next, children: next.children!.map(edged) } : next;
  };
  return copy.titles.length ? blocks.map(edged) : blocks;
}

// An edge's space joins the unmarked piece beside it.
function coalesceEdges(spans: TextSpan[]): TextSpan[] {
  const plain = (span: TextSpan) => Object.entries(span).every(([key, value]) => key === "text" || !value);
  return spans.reduce<TextSpan[]>((out, span) => {
    const last = out.at(-1);
    if (last && plain(last) && plain(span)) out[out.length - 1] = { text: last.text + span.text };
    else out.push(span);
    return out;
  }, []);
}
