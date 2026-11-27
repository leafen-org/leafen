import { Extension } from "@tiptap/core";
import type { Mark, Node } from "@tiptap/pm/model";
import { Plugin, PluginKey, TextSelection, type EditorState, type Transaction } from "@tiptap/pm/state";
import type { EditorView } from "@tiptap/pm/view";
import * as Y from "yjs";
import { BLOCK_TEXT_ROOT_PREFIX, localEditorOrigin } from "@/lib/blockTextEditing";
import { blockRichText } from "@/lib/blockTextAhead";
import { textHoldsEmbeds, textIndexAt, textOffsetAt, writeRichTextToYText } from "@/lib/collaborationCrdtCodec";
import {
  emptyPiecesBefore, fragmentToNativeRichText, nativeOffsetAt, nativeRichTextToFragment, pmOffsetAt, pmOffsetPastEmptyPieces,
} from "./nativeTiptapInline";
import { reconcileNativeTiptapProjection } from "./nativeTiptapProjection";
import { nativeTiptapTextFields, type NativeTextField } from "./nativeTiptapTextFields";
import { createNativeCompositionText, keepCompositionCaret } from "./nativeTiptapCompositionText";
import { replacedRunIndex } from "./nativeTiptapReplacedCaret";
import { holdTakenCaret } from "./takenCaret";
import { compositionEnds } from "@/lib/fieldKeyBoundary";

export type NativeProjectedBlock = {
  id: string;
  nativeId: string;
  parentId: string | null;
  projectionParentId: string | null;
  container: Node;
  node: Node;
  contentFrom: number;
};

export type NativeTiptapBindingOptions = {
  document: Y.Doc;
  canWrite(): boolean;
  deferProjection?(): boolean;
  /** A synced copy can have a different projection ID for the same Y.Text. */
  nativeId?(projectionId: string): string;
  /** The native graph owner applies structure, before its new text is written. */
  changeStructure(before: readonly NativeProjectedBlock[], after: readonly NativeProjectedBlock[]): void;
  captureLocal?(before: EditorState, after: EditorState, change: () => void, transaction: Transaction): void;
  /** apply=false queries availability without changing native state. */
  undo(redo: boolean, apply?: boolean): boolean;
};

/** Marks a projection transaction so it cannot become a native local edit. */
export const nativeProjectionMeta = "leafen-native-projection";

const identityNativeId = (id: string) => id;
const projectionIndexes = new WeakMap<Node, Map<(id: string) => string, NativeProjectedBlock[]>>();

/** PM documents are immutable; indexes expire with their document revision. */
export function projectedBlocks(doc: Node, nativeId: (id: string) => string = identityNativeId): NativeProjectedBlock[] {
  const indexes = projectionIndexes.get(doc) ?? new Map<(id: string) => string, NativeProjectedBlock[]>();
  const cached = indexes.get(nativeId);
  if (cached) return cached;
  const result: NativeProjectedBlock[] = [];
  // One walk, each block's parent the nearest block container above it, not
  // the flattened order; text is not walked into. Resolving each block's
  // position looked through all its siblings, 15 ms a letter typed on a
  // 3,000-block page (2026-10-18).
  const walk = (parent: Node, from: number, owner: Node | null) => parent.forEach((node, offset) => {
    const position = from + offset;
    if (node.type.name === "nativeBlock" && typeof node.attrs.id === "string") {
      result.push({ id: node.attrs.id, nativeId: node.attrs.sourceId || nativeId(node.attrs.id),
        parentId: owner ? owner.attrs.childrenSourceId || owner.attrs.sourceId || nativeId(owner.attrs.id) : null,
        projectionParentId: owner ? owner.attrs.id : null, container: node, node: node.firstChild!, contentFrom: position + 2 });
    }
    if (node.childCount && !node.isTextblock) walk(node, position + 1, node.type.name === "nativeBlock" ? node : owner);
  });
  walk(doc, 0, null);
  indexes.set(nativeId, result); projectionIndexes.set(doc, indexes);
  return result;
}

// A text point keeps its text index when taken, and the index someone else's
// change settled it at when that change took its character away.
type RelativePoint = { projectionId: string; position: Y.RelativePosition; index?: number; settled?: number }
  | { projectionId: string; field: NativeTextField; offset: number; assoc: number };
export type NativeRelativeSelection = { anchor: RelativePoint; head: RelativePoint };

function relativePoint(blocks: readonly NativeProjectedBlock[], position: number, document: Y.Doc, assoc: number) {
  const target = nativeTiptapTextFields(blocks).find(entry => entry.contentFrom <= position && position <= entry.contentFrom + entry.node.content.size);
  if (!target) return null;
  const { block, node, contentFrom, field } = target;
  const offset = nativeOffsetAt(node.content, position - contentFrom);
  if (field !== "text") return { projectionId: block.id, field, offset, assoc };
  const text = document.getText(BLOCK_TEXT_ROOT_PREFIX + block.nativeId);
  // An empty piece's embed takes a position the native offset doesn't count.
  const index = textIndexAt(text, offset, emptyPiecesBefore(node.content, position - contentFrom));
  if (index === null) return null;
  return { projectionId: block.id, position: Y.createRelativePositionFromTypeIndex(text, index, assoc), index };
}

function relativeSelection(state: EditorState, blocks: readonly NativeProjectedBlock[], document: Y.Doc): NativeRelativeSelection | null {
  const { selection } = state;
  if (!(selection instanceof TextSelection)) return null;
  // A caret keeps to the character before it, so text someone else types at
  // the same spot goes after it and each person's typing stays one run; a
  // range keeps its own characters inside it.
  const collapsed = selection.anchor === selection.head;
  const anchor = relativePoint(blocks, selection.anchor, document, collapsed || selection.anchor > selection.head ? -1 : 1);
  const head = relativePoint(blocks, selection.head, document, collapsed || selection.anchor < selection.head ? -1 : 1);
  return anchor && head ? { anchor, head } : null;
}

export function nativeTiptapSelection(state: EditorState, document: Y.Doc): NativeRelativeSelection | undefined {
  return relativeSelection(state, projectedBlocks(state.doc), document) ?? undefined;
}

export function nativeTiptapRangeSelection(state: EditorState, document: Y.Doc, from: number, to: number): NativeRelativeSelection | undefined {
  const blocks = projectedBlocks(state.doc);
  const anchor = relativePoint(blocks, from, document, 1), head = relativePoint(blocks, to, document, -1);
  return anchor && head ? { anchor, head } : undefined;
}

function absolutePoint(point: RelativePoint, blocks: readonly NativeProjectedBlock[], document: Y.Doc) {
  if ("field" in point) {
    const target = nativeTiptapTextFields(blocks).find(entry => entry.block.id === point.projectionId && entry.field === point.field);
    return target ? target.contentFrom + pmOffsetAt(target.node.content, point.offset, point.assoc) : null;
  }
  const absolute = point.settled === undefined ? Y.createAbsolutePositionFromRelativePosition(point.position, document) : null;
  const block = blocks.find((entry) => entry.id === point.projectionId);
  if (!(absolute || point.settled !== undefined) || !block || !block.node.isTextblock) return null;
  const text = document.getText(BLOCK_TEXT_ROOT_PREFIX + block.nativeId);
  if (absolute && absolute.type !== text) return null;
  const { offset, embeds } = textOffsetAt(text, Math.min(absolute?.index ?? point.settled!, text.length));
  return block.contentFrom + pmOffsetPastEmptyPieces(block.node.content, pmOffsetAt(block.node.content, offset, point.position.assoc), embeds);
}

export function resolveNativeTiptapSelection(doc: Node, document: Y.Doc, saved: NativeRelativeSelection) {
  const blocks = projectedBlocks(doc);
  const anchor = absolutePoint(saved.anchor, blocks, document);
  const head = absolutePoint(saved.head, blocks, document);
  return anchor !== null && head !== null ? { anchor, head } : null;
}

/**
 * Puts a saved selection back. Given the marks its caret held for the next
 * letters (stored marks), it holds them again: a step of the history taking
 * the caret back leaves the next letters as they were to be there, so Redo of
 * a typed "**bold**" types on outside the bold as the typing itself did (the
 * rule's own step takes the bold off them, nativeTiptapInputRules.ts).
 */
export function restoreNativeTiptapSelection(tr: Transaction, document: Y.Doc, saved: NativeRelativeSelection, marks?: readonly Mark[] | null) {
  const position = resolveNativeTiptapSelection(tr.doc, document, saved);
  if (!position) return tr;
  tr.setSelection(TextSelection.create(tr.doc, position.anchor, position.head));
  return marks === undefined ? tr : tr.setStoredMarks(marks);
}

/**
 * Tiptap/PM owns input and selection. Native Y.Text owns persisted text.
 * A remote update replaces just the changed fragment and restores CRDT-relative
 * selection. It never enters independent PM history or the local Yjs Undo origin.
 */
export function createNativeTiptapBinding(options: NativeTiptapBindingOptions) {
  const projectionOrigin = new PluginKey("native-text-projection");
  let view: EditorView | null = null;
  // Ends the hold a caret someone else's change took leaves (takenCaret.ts).
  let stopTakenCaret: (() => void) | null = null;
  let publishing = false;
  let selection: NativeRelativeSelection | null = null;
  const dirtyLocalTypes = new Set<unknown>();
  let pendingStructure: readonly Node[] | null = null;
  const deferredTypes = new Set<unknown>();
  const resolveId = options.nativeId ?? identityNativeId;
  let composition: { id: string; nativeId: string; text: ReturnType<typeof createNativeCompositionText> } | null = null;
  // A composition's end lets the changes held for it in once the editor took
  // its last words, and before a next composition starts (fieldKeyBoundary.ts):
  // macOS Chrome's Korean starts the next syllable in the same key press, and
  // ending a frame later dropped the composition that had begun meanwhile.
  const compositionEnd = compositionEnds(() => view, () => {
    if (!composition) return;
    selection = composingSelection();
    composition = null;
    flushDeferred();
  });

  function composingSelection(state = view?.state): NativeRelativeSelection | null {
    if (!composition || !state) return null;
    const block = projectedBlocks(state.doc, resolveId).find(value => value.id === composition!.id);
    if (!block?.node.isTextblock || state.selection.from < block.contentFrom
      || state.selection.to > block.contentFrom + block.node.content.size) return null;
    const point = (position: number) => ({ projectionId: block.id,
      position: composition!.text.point(nativeOffsetAt(block.node.content, position - block.contentFrom)) });
    return { anchor: point(state.selection.anchor), head: point(state.selection.head) };
  }

  function flushDeferred() {
    if (!view || options.deferProjection?.() || composition) return;
    const changed = new Set(deferredTypes); deferredTypes.clear();
    const blocks = pendingStructure; pendingStructure = null;
    if (blocks) projectStructure(blocks);
    if (changed.size) projectTexts(changed);
  }

  function beforeTransaction(transaction: Y.Transaction) {
    if (!view || publishing || transaction.origin === localEditorOrigin) return;
    selection = composingSelection() ?? relativeSelection(view.state, projectedBlocks(view.state.doc, resolveId), options.document);
  }

  function afterTransaction(transaction: Y.Transaction) {
    if (!view) return;
    const changedTypes = new Set<unknown>(transaction.changed.keys());
    if (publishing) {
      for (const type of changedTypes) dirtyLocalTypes.add(type);
      return;
    }
    if (selection && !composition) selection = { anchor: settledPoint(selection.anchor, transaction), head: settledPoint(selection.head, transaction) };
    projectTexts(changedTypes);
  }

  function settledPoint(point: RelativePoint, transaction: Y.Transaction): RelativePoint {
    if ("field" in point || point.index === undefined || !point.position.item || !view) return point;
    const block = projectedBlocks(view.state.doc, resolveId).find(entry => entry.id === point.projectionId);
    if (!block) return point;
    const text = options.document.getText(BLOCK_TEXT_ROOT_PREFIX + block.nativeId);
    if (!(transaction.changed as Map<unknown, unknown>).has(text)) return point;
    const settled = replacedRunIndex(text, transaction, point.position.item, point.index);
    return settled === null ? point : { ...point, settled };
  }

  // The page's shared texts' names by the texts, so a change names the blocks
  // it wrote: asking the document for every block's text at each change took
  // some 3.5 ms of a ⌘V of six lines in a 1,031-block page at CPU ×2, each
  // line written apart, none of them in the page yet (2026-11-15). The
  // document keeps its types in the order they came, so those come since are
  // named as a change brings one, and all again only when one not known took
  // another's place under its name (a text read before it was asked for as one)
  // — also when others came since: named from the last one known, a block
  // whose text the room brought before the page asked for it lost its first
  // change after, a page's first comment mark drawn only once it opened again
  // (2026-11-22, six of six opened in a tab used before).
  const textNames = new WeakMap<object, string>();
  let named = 0;
  function nameTypes(all: boolean) {
    let index = 0;
    for (const [key, shared] of options.document.share) if (all || index++ >= named) textNames.set(shared, key);
    named = options.document.share.size;
  }
  function changedBlockIds(changedTypes: ReadonlySet<unknown>) {
    const ids = new Set<string>();
    for (const type of changedTypes) {
      // Only a top type of the document can be a block's text.
      if (!(type instanceof Y.AbstractType) || type._item !== null) continue;
      let name = textNames.get(type);
      if (name === undefined) { nameTypes(named === options.document.share.size); name = textNames.get(type); }
      if (name === undefined) { nameTypes(true); name = textNames.get(type); }
      if (name?.startsWith(BLOCK_TEXT_ROOT_PREFIX)) ids.add(name.slice(BLOCK_TEXT_ROOT_PREFIX.length));
    }
    return ids;
  }

  function projectTexts(changedTypes: ReadonlySet<unknown>) {
    if (!view) return;
    if (options.deferProjection?.()) { for (const type of changedTypes) deferredTypes.add(type); return; }
    const ids = changedBlockIds(changedTypes);
    const changed = ids.size ? projectedBlocks(view.state.doc, resolveId).filter(block => {
      if (!block.node.isTextblock || !ids.has(block.nativeId)) return false;
      if (block.nativeId !== composition?.nativeId) return true;
      deferredTypes.add(options.document.getText(BLOCK_TEXT_ROOT_PREFIX + block.nativeId)); return false;
    }) : [];
    if (!changed.length) { selection = null; return; }
    const tr = view.state.tr.setMeta(nativeProjectionMeta, true).setMeta("addToHistory", false);
    // Descending positions keep each earlier block's offsets valid.
    for (const block of changed.sort((a, b) => b.contentFrom - a.contentFrom)) {
      const rich = blockRichText(options.document, block.nativeId);
      const next = nativeRichTextToFragment(rich, view.state.schema, block.node.type.spec.code === true);
      const start = block.node.content.findDiffStart(next);
      if (start === null) continue;
      const end = block.node.content.findDiffEnd(next)!;
      const overlap = start - Math.min(end.a, end.b);
      if (overlap > 0) { end.a += overlap; end.b += overlap; }
      tr.replaceWith(block.contentFrom + start, block.contentFrom + end.a, next.cut(start, end.b));
    }
    if (selection && !composition) {
      const nextBlocks = projectedBlocks(tr.doc, resolveId);
      const anchor = absolutePoint(selection.anchor, nextBlocks, options.document);
      const head = absolutePoint(selection.head, nextBlocks, options.document);
      if (anchor !== null && head !== null) tr.setSelection(TextSelection.create(tr.doc, anchor, head));
    }
    selection = null;
    if (tr.docChanged || tr.selectionSet) {
      publishing = true;
      try { view.dispatch(tr); }
      finally { publishing = false; }
    }
  }

  const plugin = new Plugin({
    key: projectionOrigin,
    filterTransaction: (tr) => !tr.docChanged || tr.getMeta(nativeProjectionMeta) || options.canWrite(),
    props: { handleDOMEvents: {
      compositionstart: (currentView) => {
        keepCompositionCaret(currentView);
        if (options.deferProjection?.() || !options.canWrite()) return false;
        const block = projectedBlocks(currentView.state.doc, resolveId).find(value => value.node.isTextblock
          && currentView.state.selection.from >= value.contentFrom
          && currentView.state.selection.to <= value.contentFrom + value.node.content.size);
        // A text with an empty piece's embed is written as typed text is
        // (writeRichTextToYText), which places the piece where the line has it.
        const shared = block && options.document.getText(BLOCK_TEXT_ROOT_PREFIX + block.nativeId);
        if (block && shared && !textHoldsEmbeds(shared)) composition = { id: block.id, nativeId: block.nativeId,
          text: createNativeCompositionText(options.document, shared) };
        return false;
      },
      compositionend: () => {
        if (composition) compositionEnd.ended();
        return false;
      },
    } },
    view: (mountedView) => {
      view = mountedView;
      options.document.on("beforeTransaction", beforeTransaction);
      options.document.on("afterTransaction", afterTransaction);
      return {
        update: (currentView, previous) => {
          if (publishing || previous.doc === currentView.state.doc) return;
          // Projection transactions are distinguishable without reading an
          // internal TipTap editor or installing a separate collaboration tree.
          const transaction = plugin.getState(currentView.state) as Transaction;
          if (transaction.getMeta(nativeProjectionMeta)) return;
          const before = projectedBlocks(previous.doc, resolveId);
          const after = projectedBlocks(currentView.state.doc, resolveId);
          const previousById = new Map(before.map((block) => [block.id, block]));
          publishing = true;
          try {
            const change = () => {
              options.changeStructure(before, after);
              options.document.transact(() => {
                for (const block of after) {
                  // A line made a block that holds no words — "---" a divider —
                  // keeps none in its text either, which nothing shows but
                  // counts, search and the page's readers took as its words
                  // (2026-11-13: a divider made so held "—-"). Its ⌘Z brings
                  // them back with the line.
                  if (!block.node.isTextblock && previousById.get(block.id)?.node.isTextblock) {
                    const text = options.document.getText(BLOCK_TEXT_ROOT_PREFIX + block.nativeId);
                    if (text.length) text.delete(0, text.length);
                    continue;
                  }
                  if (!block.node.isTextblock || previousById.get(block.id)?.node.content.eq(block.node.content)) continue;
                  const text = options.document.getText(BLOCK_TEXT_ROOT_PREFIX + block.nativeId);
                  const previousBlock = previousById.get(block.id);
                  if (composition?.id === block.id && previousBlock) composition.text.write(
                    fragmentToNativeRichText(previousBlock.node.content), fragmentToNativeRichText(block.node.content));
                  else writeRichTextToYText(text, fragmentToNativeRichText(block.node.content));
                }
                const remaining = new Set(after.map((block) => block.nativeId));
                for (const block of before) {
                  if (!block.node.isTextblock || remaining.has(block.nativeId)) continue;
                  const text = options.document.getText(BLOCK_TEXT_ROOT_PREFIX + block.nativeId);
                  if (text.length) text.delete(0, text.length);
                }
              }, localEditorOrigin);
            };
            if (options.captureLocal) options.captureLocal(previous, currentView.state, change, transaction);
            else change();
          } finally {
            publishing = false;
            // A synced source may appear more than once in this view. Reflect
            // a local text change in every projection after PM finishes its
            // update, without dispatching a nested input transaction.
            if (dirtyLocalTypes.size) queueMicrotask(() => {
              const changed = new Set(dirtyLocalTypes);
              dirtyLocalTypes.clear();
              projectTexts(changed);
            });
          }
        },
        destroy: () => {
          stopTakenCaret?.();
          options.document.off("beforeTransaction", beforeTransaction);
          options.document.off("afterTransaction", afterTransaction);
          view = null;
          selection = null;
          dirtyLocalTypes.clear();
          pendingStructure = null;
          deferredTypes.clear();
          compositionEnd.stop();
          composition = null;
        },
      };
    },
    state: {
      init: (_config, state) => state.tr,
      apply: (tr) => tr,
    },
  });

  return {
    extension: Extension.create({
      name: "nativeCollaboration",
      priority: 1000,
      addProseMirrorPlugins: () => [plugin],
      addKeyboardShortcuts: () => ({
        "Mod-z": () => { options.undo(false); return true; },
        "Mod-Shift-z": () => { options.undo(true); return true; },
        "Mod-y": () => { options.undo(true); return true; },
      }),
      // The native history applies its own transactions, so the command's
      // transaction, made from the page before them, is not dispatched after.
      addCommands: () => ({
        undo: () => ({ tr, dispatch }) => { if (dispatch) tr.setMeta("preventDispatch", true); return options.undo(false, !!dispatch); },
        redo: () => ({ tr, dispatch }) => { if (dispatch) tr.setMeta("preventDispatch", true); return options.undo(true, !!dispatch); },
      }),
    }),
    projectStructure(blocks: readonly Node[]) {
      if (!view) return;
      if (options.deferProjection?.() || composition) { pendingStructure = blocks; return; }
      if (publishing) {
        if (!pendingStructure) queueMicrotask(() => {
          const wanted = pendingStructure;
          pendingStructure = null;
          if (wanted) projectStructure(wanted);
        });
        pendingStructure = blocks;
        return;
      }
      projectStructure(blocks);
    },
    flushDeferred,
    selection(state: EditorState) {
      return composingSelection(state) ?? relativeSelection(state, projectedBlocks(state.doc, resolveId), options.document) ?? undefined;
    },
    cancelComposition() {
      selection = composingSelection(); composition = null;
      flushDeferred();
    },
  };

  function projectStructure(blocks: readonly Node[]) {
    if (!view) return;
    const previous = projectedBlocks(view.state.doc, resolveId);
    const saved = selection ?? relativeSelection(view.state, previous, options.document);
    const tr = reconcileNativeTiptapProjection(view.state.tr, blocks)
      .setMeta(nativeProjectionMeta, true).setMeta("addToHistory", false);
    let caretTaken = false;
    if (saved) {
      const next = projectedBlocks(tr.doc, resolveId);
      caretTaken = view.hasFocus() && !next.some(block => block.id === saved.head.projectionId);
      const restorePoint = (point: RelativePoint, mapped: number) => {
        if ("field" in point) {
          const field = (entries: readonly NativeProjectedBlock[]) => nativeTiptapTextFields(entries)
            .find(entry => entry.block.id === point.projectionId && entry.field === point.field);
          const before = field(previous), after = field(next);
          // PM maps selection through a changed graph field's minimal diff.
          // An unchanged field moved with its block keeps its stable ID.
          if (!before || !after || !before.node.content.eq(after.node.content)) return mapped;
        }
        return absolutePoint(point, next, options.document);
      };
      const anchor = restorePoint(saved.anchor, tr.selection.anchor);
      const head = restorePoint(saved.head, tr.selection.head);
      if (anchor !== null && head !== null) tr.setSelection(TextSelection.create(tr.doc, anchor, head));
    }
    if (!tr.docChanged && !tr.selectionSet) return;
    publishing = true;
    try { view.dispatch(tr); }
    finally { publishing = false; }
    // Someone else took away the block the caret was in: the caret goes with
    // it, the page keeping the focus, as Notion's (takenCaret.ts). PM would
    // have mapped it to the page's end, where the next letters land unseen. A
    // change made here places its own caret before this runs.
    if (caretTaken) {
      const placed = view.state.selection;
      queueMicrotask(() => {
        if (!view?.hasFocus() || !view.state.selection.eq(placed)) return;
        stopTakenCaret?.();
        stopTakenCaret = holdTakenCaret(view, () => { stopTakenCaret = null; });
      });
    }
  }
}
