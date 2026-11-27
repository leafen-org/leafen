import { blockSpec, isBlockType } from "@/lib/blockSemantics";
import { LEAFEN_BLOCKS_MIME, readTextWithMime } from "@/lib/clipboard";
import { clipboardHtmlFragment } from "@/lib/clipboardHtml";
import { otherClipboardServer } from "@/lib/clipboardServer";
import type { BlockContent } from "@/lib/types";
import {
  admitPastedBlock,
  assertMarkdownPasteChildren,
  assertMarkdownPasteHtml,
  consumeMarkdownPasteInput,
  createMarkdownPasteContext,
  type MarkdownPasteBounds,
  type MarkdownPasteContext,
} from "./markdownPasteBudget";
import type { PastedBlock } from "./markdownPasteTypes";
import { otherServerBlock, pastedContent, type OtherServerCopy } from "./otherServerBlocks";

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

const validId = (value: unknown): value is string =>
  typeof value === "string" && value.length > 0 && value.length <= 255 && !/\s/u.test(value);

/**
 * The other server a copy of Leafen blocks was made on, by the server it
 * names (clipboardServer.ts), with the page it was copied from there: null
 * for this one. A copy naming no server a paste can trust is another one's,
 * which it can't link to.
 */
function otherServerCopy(payload: Record<string, unknown>): OtherServerCopy | null {
  const server = otherClipboardServer(payload.server);
  if (server === null) return null;
  const source: Record<string, unknown> = isRecord(payload.source) ? payload.source : {};
  return { server: server ?? null, pageId: validId(source.pageId) ? source.pageId : null };
}

function normalizeClipboardText(value: string) {
  return value.replace(/\r\n?/g, "\n");
}

function matchesInternalClipboardGeneration(
  payload: Record<string, unknown>,
  data: DataTransfer,
  context: MarkdownPasteContext,
) {
  if (payload.version !== 2) return false;
  if (
    typeof payload.copyId !== "string" ||
    payload.copyId.length === 0 ||
    payload.copyId.length > 128 ||
    typeof payload.text !== "string"
  ) return false;
  const text = data.getData("text/plain");
  consumeMarkdownPasteInput(text, context);
  if (normalizeClipboardText(payload.text) !== normalizeClipboardText(text)) return false;
  const html = data.getData("text/html");
  // A copy that gives other apps its text alone (the desktop app's link
  // copy, pageLinkClipboard.ts) says so, and has no HTML to match.
  if (!html) return payload.textOnly === true;
  if (typeof DOMParser === "undefined") return false;
  assertMarkdownPasteHtml(html, context);
  const document = clipboardHtmlFragment(html, context.bounds.richtext.maxHtmlCharacters);
  return Array.from(document.querySelectorAll<HTMLElement>("[data-leafen-copy-id]"))
    .some((element) => element.dataset.leafenCopyId === payload.copyId);
}

type ProcessFrame = {
  kind: "process";
  value: unknown;
  depth: number;
  target: PastedBlock[];
};

type FinalizeFrame = {
  kind: "finalize";
  block: PastedBlock;
  children: PastedBlock[];
};

function normalizeInternalBlocks(values: unknown[], context: MarkdownPasteContext, from: OtherServerCopy | null) {
  const roots: PastedBlock[] = [];
  const stack: Array<ProcessFrame | FinalizeFrame> = [];
  for (let index = values.length - 1; index >= 0; index--) {
    stack.push({ kind: "process", value: values[index], depth: 0, target: roots });
  }
  while (stack.length) {
    const frame = stack.pop();
    if (!frame) break;
    if (frame.kind === "finalize") {
      if (frame.children.length > 0) frame.block.children = frame.children;
      continue;
    }
    const value = frame.value;
    if (!isRecord(value)) continue;
    if (!isBlockType(value.type) || blockSpec(value.type).clipboard !== "preserve_internal") continue;
    const copied: PastedBlock = {
      type: value.type,
      ...(isRecord(value.content) ? { content: value.content as BlockContent } : {}),
      ...(typeof value.plainText === "string" ? { plainText: value.plainText } : {}),
    };
    // Blocks from another server name what is there, not here; from this
    // one, their words come with no comment or suggested-edit thread.
    const block = from ? otherServerBlock(copied, validId(value.id) ? value.id : null, from)
      : copied.content ? { ...copied, content: pastedContent(copied.content as Record<string, unknown>) as BlockContent } : copied;
    admitPastedBlock(block, frame.depth, context);
    frame.target.push(block);
    if (!Array.isArray(value.children)) continue;
    assertMarkdownPasteChildren(value.children.length, context);
    const children: PastedBlock[] = [];
    stack.push({ kind: "finalize", block, children });
    for (let index = value.children.length - 1; index >= 0; index--) {
      stack.push({
        kind: "process",
        value: value.children[index],
        depth: frame.depth + 1,
        target: children,
      });
    }
  }
  return roots;
}

export function parseInternalPastedBlocks(
  data: DataTransfer,
  bounds: MarkdownPasteBounds,
): PastedBlock[] {
  const context = createMarkdownPasteContext(bounds);
  const raw = readTextWithMime(data, LEAFEN_BLOCKS_MIME, bounds.clipboard);
  if (!raw) return [];
  consumeMarkdownPasteInput(raw, context);
  try {
    const payload = JSON.parse(raw) as unknown;
    if (
      !isRecord(payload) ||
      !matchesInternalClipboardGeneration(payload, data, context) ||
      !Array.isArray(payload.blocks)
    ) return [];
    const from = otherServerCopy(payload);
    // A page's link copied on another server pastes as its address, as one copied from another app.
    if (from && payload.pageLink === true) return [];
    return normalizeInternalBlocks(payload.blocks, context, from);
  } catch (error) {
    if (error instanceof RangeError) throw error;
    return [];
  }
}

/** Whether a paste of Leafen blocks is a line's words copied whole (blockClipboard.ts), not blocks chosen as blocks. */
export function internalPasteIsLine(data: DataTransfer, bounds: MarkdownPasteBounds): boolean {
  const context = createMarkdownPasteContext(bounds);
  const raw = readTextWithMime(data, LEAFEN_BLOCKS_MIME, bounds.clipboard);
  if (!raw) return false;
  try {
    const payload = JSON.parse(raw) as unknown;
    return isRecord(payload) && payload.line === true && matchesInternalClipboardGeneration(payload, data, context);
  } catch {
    return false;
  }
}

/** The page and blocks of this server a paste of Leafen blocks was copied from, when it says so. */
export type InternalPasteSource = { pageId: string; blockIds: string[] };

export function internalPasteSource(data: DataTransfer, bounds: MarkdownPasteBounds): InternalPasteSource | null {
  const context = createMarkdownPasteContext(bounds);
  const raw = readTextWithMime(data, LEAFEN_BLOCKS_MIME, bounds.clipboard);
  if (!raw) return null;
  try {
    const payload = JSON.parse(raw) as unknown;
    if (!isRecord(payload) || !matchesInternalClipboardGeneration(payload, data, context) || !isRecord(payload.source)) return null;
    // Blocks of another server's page have nothing here to sync with or link to.
    if (otherServerCopy(payload)) return null;
    const { pageId, blockIds } = payload.source;
    if (!validId(pageId) || !Array.isArray(blockIds) || blockIds.length === 0 || blockIds.length > bounds.maxBlocks || !blockIds.every(validId)) return null;
    return { pageId, blockIds };
  } catch {
    return null;
  }
}
