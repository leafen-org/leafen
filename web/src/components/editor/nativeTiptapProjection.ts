import { Fragment, type Node } from "@tiptap/pm/model";
import { AllSelection, Selection, type Transaction } from "@tiptap/pm/state";

function nodeId(node: Node) {
  return typeof node.attrs.id === "string" ? node.attrs.id : null;
}

// A block found by its ID, the walk stopping there; a block's own text holds
// no blocks (2026-10-16: the walk went on past the block found, and the
// reconciler looked each block up several times, so a 3,000-block page took
// seconds to open).
function findBlock(doc: Node, id: string) {
  const search = (node: Node, start: number): { node: Node; position: number } | null => {
    let position = start;
    for (let index = 0; index < node.childCount; index += 1) {
      const child = node.child(index);
      if (nodeId(child) === id) return { node: child, position };
      if (!child.isTextblock && child.childCount > 0) {
        const inner = search(child, position + 1);
        if (inner) return inner;
      }
      position += child.nodeSize;
    }
    return null;
  };
  return search(doc, 0);
}

function groupOf(doc: Node, parentId: string | null, parentPosition?: number) {
  if (parentId === null) {
    return { node: doc, position: -1 };
  }
  const parent = parentPosition === undefined ? findBlock(doc, parentId)
    : { node: doc.nodeAt(parentPosition)!, position: parentPosition };
  if (!parent) throw new Error("Native projection parent disappeared.");
  let offset = 0;
  for (let index = 0; index < parent.node.childCount; index += 1) {
    const node = parent.node.child(index);
    if (node.type.name === "nativeChildren") return { node, position: parent.position + 1 + offset };
    offset += node.nodeSize;
  }
  return null;
}

function childPosition(group: { node: Node; position: number }, index: number) {
  let position = group.position + 1;
  for (let child = 0; child < index; child += 1) position += group.node.child(child).nodeSize;
  return position;
}

const sameAttrs = (a: Node, b: Node) => JSON.stringify(a.attrs) === JSON.stringify(b.attrs);

function patchContent(tr: Transaction, position: number, current: Node, wanted: Node) {
  if (current.type === wanted.type && current.type.name === "nativeTableCells") {
    const count = Math.min(current.childCount, wanted.childCount);
    let offset = 0;
    for (let index = 0; index < count; index += 1) {
      const latest = tr.doc.nodeAt(position)!;
      patchContent(tr, position + 1 + offset, latest.child(index), wanted.child(index));
      offset += wanted.child(index).nodeSize;
    }
    const latest = tr.doc.nodeAt(position)!;
    if (latest.childCount > count) tr.delete(position + 1 + offset, position + latest.nodeSize - 1);
    if (wanted.childCount > count) tr.insert(position + 1 + offset, wanted.content.cut(offset));
    return;
  }
  if (current.type !== wanted.type || !current.isTextblock || !wanted.isTextblock) {
    if (!current.eq(wanted)) tr.replaceWith(position, position + current.nodeSize, wanted);
    return;
  }
  if (!sameAttrs(current, wanted)) tr.setNodeMarkup(position, wanted.type, wanted.attrs, wanted.marks);
  const start = current.content.findDiffStart(wanted.content);
  if (start === null) return;
  const end = current.content.findDiffEnd(wanted.content)!;
  const overlap = start - Math.min(end.a, end.b);
  if (overlap > 0) { end.a += overlap; end.b += overlap; }
  tr.replaceWith(position + 1 + start, position + 1 + end.a, wanted.content.cut(start, end.b));
}

// A block's start stays while its own content changes.
function patchBlock(tr: Transaction, id: string, wanted: Node, at: number) {
  const block = () => ({ node: tr.doc.nodeAt(at)!, position: at });
  let current = block();
  if (current.node.type !== wanted.type || wanted.type.name !== "nativeBlock") {
    if (!current.node.eq(wanted)) tr.replaceWith(current.position, current.position + current.node.nodeSize, wanted);
    return;
  }
  if (!sameAttrs(current.node, wanted)) tr.setNodeMarkup(current.position, wanted.type, wanted.attrs, wanted.marks);
  patchContent(tr, current.position + 1, current.node.firstChild!, wanted.firstChild!);
  current = block();
  const childNamed = (node: Node, name: string) => {
    let child: { node: Node; offset: number } | null = null;
    node.forEach((item, offset) => { if (item.type.name === name) child = { node: item, offset }; });
    return child as { node: Node; offset: number } | null;
  };
  const wantedCaption = childNamed(wanted, "nativeCaption"), existingCaption = childNamed(current.node, "nativeCaption");
  if (wantedCaption && existingCaption) patchContent(tr, current.position + 1 + existingCaption.offset, existingCaption.node, wantedCaption.node);
  else if (wantedCaption) tr.insert(current.position + 1 + current.node.firstChild!.nodeSize, wantedCaption.node);
  else if (existingCaption) tr.delete(current.position + 1 + existingCaption.offset, current.position + 1 + existingCaption.offset + existingCaption.node.nodeSize);
  current = block();
  const wantedGroup = childNamed(wanted, "nativeChildren")?.node ?? null;
  const existingGroup = groupOf(tr.doc, id, at);
  if (!wantedGroup) {
    if (existingGroup) tr.delete(existingGroup.position, existingGroup.position + existingGroup.node.nodeSize);
    return;
  }
  if (!existingGroup) tr.insert(current.position + current.node.nodeSize - 1, wantedGroup.type.create(null, wantedGroup.content));
  reconcileGroup(tr, id, Array.from({ length: wantedGroup.childCount }, (_, index) => wantedGroup.child(index)));
}

function reconcileGroup(tr: Transaction, parentId: string | null, wanted: readonly Node[]) {
  // The parent is found once and followed through the changes made since,
  // rather than looked up through the page for each child.
  const found = parentId === null ? undefined : findBlock(tr.doc, parentId)?.position;
  const since = tr.steps.length;
  const parentPosition = () => found === undefined ? undefined : tr.mapping.slice(since).map(found);
  for (let index = 0; index < wanted.length; index += 1) {
    const node = wanted[index];
    const id = nodeId(node);
    if (!id) throw new Error("A native projected block requires a stable ID.");
    let group = groupOf(tr.doc, parentId, parentPosition())!;
    const child = index < group.node.childCount ? group.node.child(index) : null;
    // Unchanged subtrees need no identity lookup or transaction. Ordinary
    // remote content edits then scan the siblings once rather than looking
    // up every unchanged block through the entire document.
    if (child?.eq(node)) continue;
    if (child && nodeId(child) === id) { patchBlock(tr, id, node, childPosition(group, index)); continue; }
    // Move an existing projection by its stable identity. Its text and child
    // projections are patched separately rather than serializing the page again.
    const existing = findBlock(tr.doc, id);
    const inserted = existing?.node ?? node;
    if (existing) tr.delete(existing.position, existing.position + existing.node.nodeSize);
    group = groupOf(tr.doc, parentId, parentPosition())!;
    const at = childPosition(group, index);
    tr.insert(at, inserted);
    patchBlock(tr, id, node, at);
  }
  const group = groupOf(tr.doc, parentId)!;
  if (group.node.childCount > wanted.length) {
    tr.delete(childPosition(group, wanted.length), group.position + group.node.nodeSize - 1);
  }
}

/** Reconciles native structure by ID and patches only changed node content. */
export function reconcileNativeTiptapProjection(tr: Transaction, blocks: readonly Node[]) {
  // An editor that opened before its page's blocks came holds ProseMirror's
  // fallback selection of everything, which would then span every block, so
  // the first key typed once it is focused would replace the whole page. Its
  // first blocks start with a cursor at the top instead; a select-all someone
  // made stays theirs.
  const blank = Selection.findFrom(tr.doc.resolve(0), 1, true) === null;
  // A page's first projection goes in at once: there is nothing to match.
  if (tr.doc.childCount === 0) tr.insert(0, Fragment.from(blocks));
  else reconcileGroup(tr, null, blocks);
  if (blank && tr.selection instanceof AllSelection) tr.setSelection(Selection.atStart(tr.doc));
  return tr;
}
