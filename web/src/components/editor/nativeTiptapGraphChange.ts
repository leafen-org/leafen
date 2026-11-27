import { compareGraphOrder, graphBlockTypes, type ContentGraph, type GraphBlock, type GraphOperation } from "@/contentGraph";
import type { NativeProjectedBlock } from "./nativeTiptapBinding";
import { nativeTiptapSyncedLayout } from "./nativeTiptapSyncedLayout";
import { nativeTiptapGraphFieldsEqual, nativeTiptapGraphProps } from "./nativeTiptapGraphFields";
import { applyEditorContentDelta, editorContentEqual } from "./editorContentDelta";
import { withPastedViewCopies } from "./blockViewCopies";

const same = editorContentEqual;

function placements(blocks: readonly NativeProjectedBlock[], rootId: string | null, among?: ReadonlySet<string>) {
  const previous = new Map<string | null, string>();
  return new Map(blocks.filter(block => block.id === block.nativeId && (!among || among.has(block.id))).map(block => {
    const parentId = block.parentId ?? rootId;
    const afterId = previous.get(parentId) ?? null;
    previous.set(parentId, block.id);
    return [block.id, { parentId, afterId }];
  }));
}

/** Detects structure/props without serializing text or a whole page on typing. */
export function nativeTiptapStructureChanged(before: readonly NativeProjectedBlock[], after: readonly NativeProjectedBlock[]) {
  if (before.length !== after.length) return true;
  return before.some((block, index) => {
    const next = after[index];
    return block.id !== next.id || block.parentId !== next.parentId || block.nativeId !== next.nativeId
      || block.container.attrs.kind !== next.container.attrs.kind
      || !same(block.container.attrs.props, next.container.attrs.props) || !nativeTiptapGraphFieldsEqual(block, next);
  });
}

/** Publishes only the edited native delta; unprojected scopes stay intact. */
export function nativeTiptapGraphChange(
  graph: ContentGraph,
  before: readonly NativeProjectedBlock[],
  after: readonly NativeProjectedBlock[],
  rootId: string | null = null,
): { graph: ContentGraph; operations: GraphOperation[] } | null {
  if (!nativeTiptapStructureChanged(before, after)) return null;
  ({ before, after } = nativeTiptapSyncedLayout(before, after));
  const old = new Map(graph.blocks.map(block => [block.id, block]));
  const previous = new Map(before.filter(block => block.id === block.nativeId).map(block => [block.id, block]));
  const beforePlaces = placements(before, rootId), afterPlaces = placements(after, rootId);
  const removed = new Set([...previous.keys()].filter(id => !afterPlaces.has(id)));
  // A block moved only if its earlier neighbour among the blocks here both
  // before and after changed: one added or taken away beside it doesn't move
  // it, and saying it did put it after the new block — two places adding a
  // line after the same block, offline or at once, then left one of the lines
  // below the next block (2026-11-06).
  const kept = new Set([...previous.keys()].filter(id => afterPlaces.has(id)));
  const keptBefore = placements(before, rootId, kept), keptAfter = placements(after, rootId, kept);
  const changedParents = new Set<string | null>();
  const operations: GraphOperation[] = [];
  const next = new Map(old);
  for (const block of after) {
    // A synced projection's text writes to its source; it does not create
    // durable blocks named after the display instance.
    if (block.id !== block.nativeId) continue;
    const kind = block.container.attrs.kind;
    if (!graphBlockTypes.includes(kind)) throw new TypeError("Unknown native block kind.");
    const place = afterPlaces.get(block.id)!;
    const prior = old.get(block.id), priorProjection = previous.get(block.id);
    const beforeProps = priorProjection ? nativeTiptapGraphProps(priorProjection) : {};
    const afterProps = nativeTiptapGraphProps(block);
    // Merge the changed props against the latest native graph, keeping peer
    // keys which were not changed by this editing transaction.
    const content = applyEditorContentDelta(prior?.content ?? {}, beforeProps, afterProps);
    const value: GraphBlock = { ...prior, id: block.id, type: kind, content,
      ...place, position: prior?.position ?? "" };
    next.set(block.id, value);
    if (!prior || prior.type !== kind || !same(prior.content, content)) {
      operations.push({ kind: "upsertBlock", block: { ...value, afterId: prior ? null : place.afterId } });
    }
    const from = beforePlaces.get(block.id);
    const keptFrom = keptBefore.get(block.id), keptTo = keptAfter.get(block.id);
    // The group's first block still moves behind a block added before it: a
    // new block goes in after the first one (below) and this move puts it ahead.
    const ledNow = keptFrom?.afterId == null && from?.afterId !== place.afterId;
    if (!prior || !from || from.parentId !== place.parentId || keptFrom?.afterId !== keptTo?.afterId || ledNow) {
      changedParents.add(place.parentId);
      if (prior) {
        changedParents.add(prior.parentId);
        operations.push({ kind: "moveBlock", move: { blockId: block.id, ...place } });
      }
    }
  }
  // Everything that goes is named, holders first: Undo brings back what a
  // change names, in that order (2026-11-05: a callout's line came back on the
  // screen alone, and the next change to it was refused).
  for (const id of removed) {
    changedParents.add(old.get(id)?.parentId ?? null);
    next.delete(id);
    operations.push({ kind: "deleteBlock", deletion: { blockId: id } });
  }
  // Native null insertion appends, and a move cannot target a new block in
  // the same batch. Lead an existing group with the established insert policy.
  for (const operation of operations) {
    if (operation.kind !== "upsertBlock" || old.has(operation.block.id) || operation.block.afterId !== null) continue;
    const first = graph.blocks.filter(block => block.parentId === operation.block.parentId && !removed.has(block.id)).sort(compareGraphOrder)[0];
    if (first) operation.block = { ...operation.block, afterId: first.id };
  }
  for (const parentId of changedParents) {
    const projected = after.filter(block => block.id === block.nativeId && afterPlaces.get(block.id)?.parentId === parentId);
    // Only this visible container is rebalanced. Other graph scopes retain
    // their records, database state and display positions.
    projected.forEach((block, index) => {
      const value = next.get(block.id)!;
      next.set(block.id, { ...value, position: `a${String(index).padStart(8, "0")}:${block.id}` });
    });
  }
  for (const operation of operations) {
    if (operation.kind === "upsertBlock") operation.block = {
      ...next.get(operation.block.id)!, afterId: operation.block.afterId,
    };
  }
  // A linked view pasted takes copies of the views it carried (blockViewCopies.ts).
  return operations.length ? withPastedViewCopies({ graph: { ...graph, blocks: [...next.values()] }, operations }) : null;
}
