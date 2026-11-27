import type { NativeProjectedBlock } from "./nativeTiptapBinding";
import { nativeTiptapGraphFieldsEqual } from "./nativeTiptapGraphFields";

type Projected = readonly NativeProjectedBlock[];
const propsEqual = (a: NativeProjectedBlock, b: NativeProjectedBlock) =>
  a.container.attrs.kind === b.container.attrs.kind
  && (a.container.attrs.props === b.container.attrs.props
    || JSON.stringify(a.container.attrs.props) === JSON.stringify(b.container.attrs.props)) && nativeTiptapGraphFieldsEqual(a, b);

function groups(blocks: Projected) {
  const result = new Map<string | null, NativeProjectedBlock[]>();
  for (const block of blocks) {
    const group = result.get(block.projectionParentId) ?? [];
    group.push(block);
    result.set(block.projectionParentId, group);
  }
  return result;
}

function normalized(block: NativeProjectedBlock): NativeProjectedBlock {
  return { ...block, id: block.nativeId, projectionParentId: block.parentId };
}

function canonical(blocks: Projected) {
  const result = new Map<string, NativeProjectedBlock>();
  for (const block of blocks) {
    if (!result.has(block.nativeId) || block.id === block.nativeId) result.set(block.nativeId, normalized(block));
  }
  return result;
}

function ordered(blocks: Map<string, NativeProjectedBlock>, orders: Map<string | null, string[]>) {
  const grouped = new Map<string | null, NativeProjectedBlock[]>();
  for (const block of blocks.values()) {
    const group = grouped.get(block.parentId) ?? [];
    group.push(block);
    grouped.set(block.parentId, group);
  }
  const result: NativeProjectedBlock[] = [], seen = new Set<string>();
  function visit(parentId: string | null) {
    const group = grouped.get(parentId) ?? [];
    const wanted = orders.get(parentId);
    if (wanted) {
      const indices = new Map(wanted.map((id, index) => [id, index]));
      group.sort((a, b) => (indices.get(a.id) ?? Number.MAX_SAFE_INTEGER) - (indices.get(b.id) ?? Number.MAX_SAFE_INTEGER));
    }
    for (const block of group) {
      if (seen.has(block.id)) continue;
      seen.add(block.id); result.push(block); visit(block.id);
    }
  }
  visit(null);
  // A root-scoped view starts below a native parent absent from this view.
  for (const parent of grouped.keys()) if (parent !== null && !blocks.has(parent)) visit(parent);
  return result;
}

/** A structural edit in one synced occurrence changes its original once. */
export function nativeTiptapSyncedLayout(before: Projected, after: Projected) {
  if (![...before, ...after].some(block => block.id !== block.nativeId || block.container.attrs.childrenSourceId)) {
    return { before, after };
  }
  const old = canonical(before), next = canonical(after);
  const oldGroups = groups(before), nextGroups = groups(after);
  const beforeById = new Map(before.map(block => [block.id, block]));
  const afterById = new Map(after.map(block => [block.id, block]));
  const orders = new Map<string | null, string[]>();
  const removed = new Set<string>(), placed = new Set<string>();
  for (const block of after) {
    const previous = beforeById.get(block.id);
    if (previous && !propsEqual(previous, block)) next.set(block.nativeId, normalized(block));
  }
  for (const [projectionParentId, previous] of oldGroups) {
    // Removing a copy removes its reference, never its original's children.
    if (projectionParentId !== null && !afterById.has(projectionParentId)) continue;
    const current = nextGroups.get(projectionParentId) ?? [];
    if (previous.length === current.length && previous.every((block, index) => block.id === current[index].id)) continue;
    const ids = new Set(current.map(block => block.nativeId));
    for (const block of previous) if (!ids.has(block.nativeId)) removed.add(block.nativeId);
    const parentId = current[0]?.parentId ?? previous[0]?.parentId ?? null;
    orders.set(parentId, current.map(block => block.nativeId));
    for (const block of current) { placed.add(block.nativeId); next.set(block.nativeId, normalized(block)); }
  }
  for (const [projectionParentId, current] of nextGroups) {
    if (oldGroups.has(projectionParentId)) continue;
    const parentId = current[0]?.parentId ?? null;
    orders.set(parentId, current.map(block => block.nativeId));
    for (const block of current) { placed.add(block.nativeId); next.set(block.nativeId, normalized(block)); }
  }
  for (const id of placed) removed.delete(id);
  // The unchanged occurrence is stale until the resulting native graph is
  // projected again. It must not reintroduce a child deleted in its copy.
  let grew = true;
  while (grew) {
    grew = false;
    for (const block of next.values()) if (block.parentId && removed.has(block.parentId) && !placed.has(block.id) && !removed.has(block.id)) {
      removed.add(block.id); grew = true;
    }
  }
  for (const id of removed) next.delete(id);
  return { before: ordered(old, new Map()), after: ordered(next, orders) };
}
