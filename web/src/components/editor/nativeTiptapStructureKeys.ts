import { Extension } from "@tiptap/core";
import { Fragment, type Node } from "@tiptap/pm/model";
import { TextSelection, type EditorState, type Transaction } from "@tiptap/pm/state";
import { blockSpec, isBlockType, TAB_INTO_BLOCK_TYPES } from "@/lib/blockSemantics";
import { projectedBlocks, type NativeProjectedBlock } from "./nativeTiptapBinding";
import { BACKSPACE_KEYS } from "./nativeTiptapMergeKeys";
import { imeComposing } from "@/lib/fieldKeyBoundary";

const positionOf = (block: NativeProjectedBlock) => block.contentFrom - 2;
function childrenOf(block: NativeProjectedBlock) {
  let group: { node: Node; position: number } | null = null;
  block.container.forEach((node, offset) => {
    if (node.type.name === "nativeChildren") group = { node, position: positionOf(block) + 1 + offset };
  });
  return group as { node: Node; position: number } | null;
}

function withFollowing(block: Node, following: readonly Node[], state: EditorState) {
  if (!following.length) return block;
  const children: Node[] = [];
  let found = false;
  block.forEach(node => {
    if (node.type.name === "nativeChildren") {
      node = node.copy(node.content.append(Fragment.fromArray(following)));
      found = true;
    }
    children.push(node);
  });
  if (!found) children.push(state.schema.nodes.nativeChildren.create(null, following));
  return block.copy(Fragment.fromArray(children));
}

function withoutChildren(block: Node) {
  const kept: Node[] = [];
  block.forEach(node => { if (node.type.name !== "nativeChildren") kept.push(node); });
  return block.copy(Fragment.fromArray(kept));
}

function preserveSelection(tr: Transaction, state: EditorState, block: NativeProjectedBlock) {
  const moved = projectedBlocks(tr.doc).find(candidate => candidate.id === block.id)!;
  tr.setSelection(TextSelection.create(tr.doc,
    moved.contentFrom + state.selection.anchor - block.contentFrom,
    moved.contentFrom + state.selection.head - block.contentFrom));
  return tr.scrollIntoView();
}

/** Keeps native IDs and selection when changing one block's nesting. */
export function nativeTiptapNestingTransaction(state: EditorState, blocks: readonly NativeProjectedBlock[], block: NativeProjectedBlock, lift: boolean) {
  const siblings = blocks.filter(candidate => candidate.projectionParentId === block.projectionParentId);
  const index = siblings.findIndex(candidate => candidate.id === block.id);
  const tr = state.tr;
  if (lift) {
    const parent = blocks.find(candidate => candidate.id === block.projectionParentId);
    if (!parent || !isBlockType(parent.container.attrs.kind) || blockSpec(parent.container.attrs.kind).structural) return null;
    // The one line a callout holds stays in it, as Notion's (seen 2026-11-05).
    if (parent.container.attrs.kind === "callout" && parent.node.type.name === "nativeWidget" && siblings.length === 1) return null;
    const group = childrenOf(parent)!;
    // ⇧Tab takes the lines after it along as its last children, hidden under
    // a closed toggle, as Notion's (seen 2026-11-08); a block that holds no
    // children, as a heading, leaves them in the parent and comes out alone.
    const kind = block.container.attrs.kind;
    const adopts = isBlockType(kind) && blockSpec(kind).canContainChildren;
    const following = adopts ? siblings.slice(index + 1).map(candidate => candidate.container) : [];
    // Removing the only children also removes their optional wrapper;
    // schema fitting must not synthesize an unrelated empty native block.
    if (siblings.length === 1) tr.delete(group.position, group.position + group.node.nodeSize);
    else if (!adopts) tr.delete(positionOf(block), positionOf(block) + block.container.nodeSize);
    else if (index === 0) tr.delete(group.position, group.position + group.node.nodeSize);
    else tr.delete(positionOf(block), group.position + group.node.nodeSize - 1);
    const remainingParent = projectedBlocks(tr.doc).find(candidate => candidate.id === parent.id)!;
    tr.insert(positionOf(remainingParent) + remainingParent.container.nodeSize, withFollowing(block.container, following, state));
  } else {
    const previous = siblings[index - 1];
    // A table's children are its rows: Tab after one does nothing, the caret
    // staying (TAB_INTO_BLOCK_TYPES).
    if (!previous || !TAB_INTO_BLOCK_TYPES.has(previous.container.attrs.kind)) return null;
    // Tab takes the line one level in, but what it holds stays where the
    // reader sees it: its lines follow it in the block before, the toggle
    // left empty; a closed toggle's hidden lines go in with it (Notion's for
    // toggles, toggle headings, list items, to-dos, quotes and text, seen
    // 2026-11-08). A chosen block's Tab takes them along (blockSelectionKeyboard).
    const kind = block.container.attrs.kind;
    const held = childrenOf(block);
    const closed = isBlockType(kind) && blockSpec(kind).toggle && block.container.attrs.props.collapsed === true;
    const moving = !held || closed ? Fragment.from(block.container) : Fragment.from(withoutChildren(block.container)).append(held.node.content);
    tr.delete(positionOf(block), positionOf(block) + block.container.nodeSize);
    if (blockSpec(previous.container.attrs.kind).toggle && previous.container.attrs.props.collapsed === true) {
      tr.setNodeMarkup(positionOf(previous), undefined, { ...previous.container.attrs,
        props: { ...previous.container.attrs.props, collapsed: false } });
    }
    const remainingPrevious = projectedBlocks(tr.doc).find(candidate => candidate.id === previous.id)!;
    const group = childrenOf(remainingPrevious);
    if (group) tr.insert(group.position + group.node.nodeSize - 1, moving);
    else tr.insert(positionOf(remainingPrevious) + remainingPrevious.container.nodeSize - 1,
      state.schema.nodes.nativeChildren.create(null, moving));
  }
  return preserveSelection(tr, state, block);
}

/** Native nesting policy uses engine transactions, never DOM/caret rewrites. */
export const NativeTiptapStructureKeys = Extension.create({
  name: "nativeStructureKeys", priority: 1050,
  addKeyboardShortcuts() {
    const active = () => {
      const { state, view } = this.editor;
      if (!this.editor.isEditable || imeComposing(view) || !(state.selection instanceof TextSelection)) return null;
      const blocks = projectedBlocks(state.doc);
      const block = blocks.find(candidate => candidate.node.type.name === "nativeText"
        && state.selection.from >= candidate.contentFrom && state.selection.to <= candidate.contentFrom + candidate.node.content.size);
      return block ? { blocks, block, state } : null;
    };
    const nest = (lift: boolean) => {
      const context = active();
      if (!context) return false;
      const { blocks, block, state } = context;
      // A callout keeps its own lines: ⇧Tab takes one no further out, as Notion's (seen 2026-11-05).
      const holder = lift ? blocks.find(candidate => candidate.id === block.projectionParentId) : undefined;
      if (holder?.container.attrs.kind === "callout" && holder.node.type.name === "nativeWidget") return true;
      const tr = nativeTiptapNestingTransaction(state, blocks, block, lift);
      if (tr) this.editor.view.dispatch(tr);
      return true;
    };
    const backspace = () => {
      const context = active();
      if (!context || !context.state.selection.empty || context.state.selection.$from.parentOffset !== 0) return false;
      const { block, blocks, state } = context;
      const parent = blocks.find(candidate => candidate.id === block.projectionParentId);
      // A callout's lines join what comes before them, as Notion's (seen
      // 2026-11-05), and so do a synced block's, its first the line before it,
      // the block kept, empty if that was all (seen 2026-11-15).
      if (parent && isBlockType(parent.container.attrs.kind) && !blockSpec(parent.container.attrs.kind).structural
        && parent.container.attrs.kind !== "callout" && parent.container.attrs.kind !== "synced_block") return nest(true);
      // A heading joins the text before it at once, as a paragraph does; the
      // others turn to text first (Notion's, seen 2026-11-04).
      if (block.container.attrs.kind === "paragraph" || /^heading_\d$/u.test(String(block.container.attrs.kind))) return false;
      this.editor.view.dispatch(state.tr.setNodeMarkup(positionOf(block), undefined, { ...block.container.attrs, kind: "paragraph", props: {} }));
      return true;
    };
    // The keys that do at a line's start what ⌫ does, as Notion's (seen
    // 2026-11-02 and 2026-11-09); ⌘⌫ joins a list item or a quote to the block
    // before it there, as a line's does, and ⌃H and ⌃⌥⌫ have their own
    // (the key table in nativeTiptapMergeKeys; seen 2026-11-08).
    return {
      Tab: () => nest(false), "Shift-Tab": () => nest(true),
      ...Object.fromEntries(BACKSPACE_KEYS.map(key => [key, backspace])),
    };
  },
});
