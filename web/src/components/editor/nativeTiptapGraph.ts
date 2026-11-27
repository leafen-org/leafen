import type { Node, Schema } from "@tiptap/pm/model";
import type * as Y from "yjs";
import { compareGraphOrder, type ContentGraph, type GraphBlock } from "@/contentGraph";
import { TEXT_BLOCK_TYPES } from "@/lib/blockSemantics";
import { blockRichText } from "@/lib/blockTextAhead";
import { sanitizeTextSpans } from "@/lib/textOperations";
import { nativeTiptapBody } from "./nativeTiptapBody";
import { isDatabaseSourceTitle } from "./databaseTitles";
import { syncedCopyDrawing } from "./blockOccurrences";
import type { TextSpan } from "@/lib/types";

/** Server-only public labels are rendering metadata, never editor writes. */
export function staticProjectionSpans(value: unknown): TextSpan[] | undefined {
  if (!Array.isArray(value)) return undefined;
  return value.flatMap(source => {
    const span = sanitizeTextSpans([source])?.[0];
    if (!span) return [];
    if (source.unavailable === true) span.unavailable = true;
    if (source.mentionHasContent === true) span.mentionHasContent = true;
    if (source.mentionViewBlock === true) span.mentionViewBlock = true;
    if (Array.isArray(source.mentionSourceTitles) && source.mentionSourceTitles.every(isDatabaseSourceTitle)) {
      span.mentionSourceTitles = source.mentionSourceTitles;
    }
    for (const key of ["mentionIcon", "mentionIconType", "mentionPageId", "mentionPageKind", "customEmojiPublic"] as const) {
      if (typeof source[key] === "string") Object.assign(span, { [key]: source[key] });
    }
    return [span];
  });
}

/** What a block's node was last made from, and the node. */
type Projected = { type: string; content: GraphBlock["content"]; rich: readonly TextSpan[]; nested: readonly Node[];
  status: string | null; childrenSourceId: string | null; node: Node };

/**
 * One editor's blocks as last projected, by their place's ID: a block made
 * again of the same kind, content, spans and nested nodes is the node it
 * was — wherever it now stands among its siblings —
 * which the engine finds unchanged at once (reconcileNativeTiptapProjection).
 * Made anew, each block of a long page was made and compared again after
 * each of the page's changes, its own Enter and paste too (2026-11-14:
 * a 600-block page's Enter, 6 ms at CPU ×2; a 1,022-block page's paste with
 * a picture chosen, 13 ms).
 */
export type NativeProjectionCache = Map<string, Projected>;

const NO_SPANS: readonly TextSpan[] = [];
const sameNodes = (left: readonly Node[], right: readonly Node[]) =>
  left.length === right.length && left.every((node, index) => node === right[index]);

/** Builds a disposable view from the existing native tree and text authorities. */
export function nativeTiptapBlocks(graph: ContentGraph, document: Y.Doc | null, schema: Schema, rootId: string | null = null, pageId = "",
  onlyBlockId: string | null = null, cache?: NativeProjectionCache): Node[] {
  const children = new Map<string | null, GraphBlock[]>();
  const ids = new Set(graph.blocks.map(block => block.id));
  for (const block of graph.blocks) {
    const group = children.get(block.parentId) ?? [];
    group.push(block);
    children.set(block.parentId, group);
  }
  for (const group of children.values()) group.sort(compareGraphOrder);
  function project(block: GraphBlock, instance = "", trail: ReadonlySet<string> = new Set()): Node {
    const rich = !TEXT_BLOCK_TYPES.has(block.type) ? NO_SPANS : document ? blockRichText(document, block.id)
      : staticProjectionSpans(block.content.rich) ?? (typeof block.content.text === "string" ? [{ text: block.content.text }] : []);
    // A synced copy on its original's page draws the original's blocks (blockOccurrences).
    const original = syncedCopyDrawing(block, pageId, graph.blocks);
    const localCopy = original !== null;
    const nestedSource = original ?? block.id;
    const recursive = trail.has(nestedSource);
    const nextTrail = new Set([...trail, nestedSource]);
    const nextInstance = localCopy ? `${instance}${block.id}/` : instance;
    const nested = recursive || block.type === "template" || block.type === "button" ? []
      : (children.get(nestedSource) ?? []).map(child => project(child, nextInstance, nextTrail));
    const id = instance ? `${instance}${block.id}` : block.id;
    const childrenSourceId = original;
    const status = recursive || original !== null && !ids.has(original) ? "unavailable" : null;
    const kept = cache?.get(id);
    if (kept && kept.type === block.type && kept.content === block.content && kept.rich === rich && kept.status === status
      && kept.childrenSourceId === childrenSourceId && sameNodes(kept.nested, nested)) return kept.node;
    // A callout made on the screen holds its lines with no text of its own, as Notion's (seen 2026-11-05).
    const content = nativeTiptapBody(block, rich, schema, block.type === "callout" && !rich.length && nested.length > 0);
    // Equal empty widget nodes otherwise keep a previous parent's React view
    // when projection moves or replaces their surrounding native blocks.
    for (let index = 0; index < content.length; index++) {
      if (content[index].type.name === "nativeWidget") content[index] = schema.nodes.nativeWidget.create({ blockId: block.id });
    }
    if (nested.length) content.push(schema.nodes.nativeChildren.create(null, nested));
    const node = schema.nodes.nativeBlock.create({ id, sourceId: instance ? block.id : null, childrenSourceId,
      projectionStatus: status, kind: block.type, props: block.content }, content);
    cache?.set(id, { type: block.type, content: block.content, rich, nested, status, childrenSourceId, node });
    return node;
  }
  const blocks = (onlyBlockId ? graph.blocks.filter(block => block.id === onlyBlockId) : children.get(rootId) ?? []).map(block => project(block));
  // A block gone from the page goes from what is kept.
  if (cache) {
    const drawn = new Set<Node>();
    const walk = (node: Node) => {
      drawn.add(node);
      if (node.lastChild?.type.name === "nativeChildren") node.lastChild.forEach(walk);
    };
    blocks.forEach(walk);
    for (const [id, kept] of cache) if (!drawn.has(kept.node)) cache.delete(id);
  }
  return blocks;
}
