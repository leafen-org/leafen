import { Extension, type NodeViewRenderer } from "@tiptap/core";
import type { Node as ProseNode } from "@tiptap/pm/model";
import { Plugin, PluginKey, type Transaction } from "@tiptap/pm/state";
import type { EditorView } from "@tiptap/pm/view";
import { blockRun, type BlockRun } from "./blockRun";

/**
 * Where a block runs on from the one above it or into the one below it
 * (blockRun.ts), marked on its view's own box as `data-run-above` and
 * `data-run-below` for the rules that space a run (EditorBlocks.module.css).
 * Read by the rules from the neighbour beside it (`+`, `:has(+ …)`), every
 * line made or pasted in a long page had the browser restyle the blocks
 * after it (2026-11-14, 600 blocks at CPU ×2: some 2,200 elements, 22 ms,
 * for a paste of 11 lines and as much for an Enter); the engine marks only
 * the blocks whose neighbours changed.
 *
 * The marks are written on the boxes themselves, not kept as the engine's
 * decorations: a set of them over a long page was carried through each
 * change and handed down to every block as the page was drawn again
 * (2026-11-15: 422 marks in a 1,031-block page, some 5 ms of a paste of six
 * lines with a picture chosen at CPU ×2). A block's view marks itself as it
 * is made (marksItsRun); after a change, the blocks it touched and their
 * neighbours are marked again; the whole page, block by block, as the view
 * starts.
 */
const key = new PluginKey<Unmarked>("nativeBlockRuns");

// A block's run by its node, which the engine keeps while it is unchanged.
const runs = new WeakMap<ProseNode, BlockRun | null>();

function runOf(node: ProseNode | null | undefined): BlockRun | null {
  if (!node) return null;
  let run = runs.get(node);
  if (run === undefined) {
    const words = node.firstChild?.type.name === "nativeText" ? node.firstChild.content : null;
    run = node.type.name === "nativeBlock" ? blockRun(String(node.attrs.kind), words) ?? null : null;
    runs.set(node, run);
  }
  return run;
}

function markOne(box: Element, name: string, on: boolean) {
  if (on === box.hasAttribute(name)) return;
  if (on) box.setAttribute(name, "");
  else box.removeAttribute(name);
}

/** Marks the box of `holder`'s block at `index` by its neighbours there. */
function markBox(box: Element, holder: ProseNode, index: number) {
  const run = runOf(holder.child(index));
  markOne(box, "data-run-above", run !== null && index > 0 && runOf(holder.child(index - 1)) === run);
  markOne(box, "data-run-below", run !== null && index + 1 < holder.childCount && runOf(holder.child(index + 1)) === run);
}

/** A block's children, when it holds any. */
function heldBy(block: ProseNode): ProseNode | null {
  const children = block.lastChild;
  return block.type.name === "nativeBlock" && children?.type.name === "nativeChildren" ? children : null;
}

/** Where a block's children's first one stands, the block standing at `at`. */
const heldStart = (block: ProseNode, held: ProseNode, at: number) => at + block.nodeSize - held.nodeSize;

type Drawn = Element & { pmViewDesc?: { node?: ProseNode } };

/**
 * The box drawn for `block` at `at`: the one after the box of the block
 * before it when that is the one (a whole page marked block by block), else
 * the engine's, which counts its way there from the page's start.
 */
function boxOf(view: EditorView, block: ProseNode, at: number, guess: Element | null) {
  if (guess && (guess as Drawn).pmViewDesc?.node === block) return guess;
  const box = view.nodeDOM(at);
  return box instanceof Element ? box : null;
}

/**
 * Marks `holder`'s blocks `lo` to `hi` (its first block standing at `start`)
 * by their neighbours; of those `deep` takes, all the blocks inside them too.
 */
function markBlocks(view: EditorView, holder: ProseNode, start: number, lo: number, hi: number, deep: (at: number, size: number) => boolean) {
  let at = start, guess: Element | null = null;
  for (let index = 0; index < lo; index++) at += holder.child(index).nodeSize;
  for (let index = lo; index <= hi; index++) {
    const block = holder.child(index);
    const box = boxOf(view, block, at, guess);
    if (box && block.type.name === "nativeBlock") markBox(box, holder, index);
    guess = box?.nextElementSibling ?? null;
    const held = deep(at, block.nodeSize) ? heldBy(block) : null;
    if (held?.childCount) markBlocks(view, held, heldStart(block, held, at), 0, held.childCount - 1, () => true);
    at += block.nodeSize;
  }
}

/**
 * Marks `holder`'s blocks again where the change from `from` to `to` (in the
 * page now) touches them, and their neighbours, going into the blocks it
 * reaches inside.
 */
function remark(view: EditorView, holder: ProseNode, start: number, from: number, to: number) {
  let first = -1, last = -1;
  for (let index = 0, at = start; index < holder.childCount; index++) {
    const end = at + holder.child(index).nodeSize;
    if (at <= to && end >= from) { if (first < 0) first = index; last = index; }
    at = end;
  }
  if (first < 0) return;
  const lo = Math.max(0, first - 1), hi = Math.min(holder.childCount - 1, last + 1);
  const inside = (at: number, size: number) => at >= from && at + size <= to;
  markBlocks(view, holder, start, lo, hi, inside);
  for (let index = 0, at = start; index <= hi; index++) {
    const block = holder.child(index), end = at + block.nodeSize;
    const held = index >= lo && !inside(at, block.nodeSize) ? heldBy(block) : null;
    if (held) {
      const heldAt = heldStart(block, held, at);
      if (to >= heldAt && from <= heldAt + held.content.size) remark(view, held, heldAt, from, to);
    }
    at = end;
  }
}

// More changed places than this in one change mark the page anew.
const MOST_CHANGED_PLACES = 16;

/**
 * The places a change changed, in the page after it; null when a step names
 * none it changed (the page is marked anew).
 */
function changedPlaces(tr: Transaction): Array<[number, number]> | null {
  const places: Array<[number, number]> = [];
  for (let index = 0; index < tr.steps.length; index++) {
    const rest = tr.mapping.slice(index + 1);
    const map = tr.mapping.maps[index];
    let named = false;
    map.forEach((_oldStart, _oldEnd, newStart, newEnd) => { named = true; places.push([rest.map(newStart, -1), rest.map(newEnd, 1)]); });
    if (named) continue;
    // A step that moves nothing: its marks or attributes, at its place.
    const step = tr.steps[index] as unknown as { from?: number; to?: number; pos?: number };
    const after = tr.docs[index + 1] ?? tr.doc;
    if (typeof step.from === "number" && typeof step.to === "number") places.push([rest.map(step.from, -1), rest.map(step.to, 1)]);
    else if (typeof step.pos === "number") {
      const node = after.nodeAt(step.pos);
      if (!node) return null;
      places.push([rest.map(step.pos, -1), rest.map(step.pos + node.nodeSize, 1)]);
    } else return null;
  }
  return places.length > MOST_CHANGED_PLACES ? null : places;
}

/**
 * The places changed since the page's boxes were last marked, in the page
 * of the state holding them (null: the whole page). `marked` is set once the
 * view has marked them, so the next change starts anew.
 */
type Unmarked = { places: Array<[number, number]> | null; marked: boolean };

function unmarkedAfter(tr: Transaction, before: Unmarked): Unmarked {
  if (!tr.docChanged) return before;
  const own = changedPlaces(tr);
  const kept = before.marked ? [] : before.places?.map(([from, to]): [number, number] => [tr.mapping.map(from, -1), tr.mapping.map(to, 1)]) ?? null;
  const places = own && kept ? [...kept, ...own] : null;
  return { places: places && places.length <= MOST_CHANGED_PLACES ? places : null, marked: false };
}

function markChanged(view: EditorView, unmarked: Unmarked) {
  const { doc } = view.state;
  unmarked.marked = true;
  if (!doc.childCount) return;
  if (!unmarked.places) markBlocks(view, doc, 0, 0, doc.childCount - 1, () => true);
  else for (const [from, to] of unmarked.places) remark(view, doc, 0, Math.min(from, to), Math.max(from, to));
}

// The views whose page has been marked whole once (the views made with it
// are marked then, in one walk, rather than each counting its way there).
const marking = new WeakSet<EditorView>();

/**
 * A block's view marks its own box as it is made, by its neighbours in the
 * page it is made for: the engine makes a block's view anew when it can't
 * draw the block in the one it had, wherever that is.
 */
export function marksItsRun(renderer: NodeViewRenderer): NodeViewRenderer {
  return (props) => {
    const made = renderer(props);
    const at = marking.has(props.view) ? props.getPos() : undefined;
    if (typeof at === "number" && made.dom instanceof Element) {
      const place = props.view.state.doc.resolve(at), index = place.index();
      if (place.parent.maybeChild(index) === props.node) markBox(made.dom, place.parent, index);
    }
    return made;
  };
}

export const NativeTiptapBlockRuns = Extension.create({
  name: "nativeBlockRuns",
  addProseMirrorPlugins() {
    return [new Plugin<Unmarked>({
      key,
      state: {
        init: () => ({ places: null, marked: false }),
        apply: unmarkedAfter,
      },
      view(made) {
        let page = made.state.doc;
        // The whole page is marked as the view starts, its blocks' views just made.
        const first = key.getState(made.state);
        if (first) first.marked = true;
        markChanged(made, { places: null, marked: false });
        marking.add(made);
        return {
          update(view) {
            if (view.state.doc === page) return;
            page = view.state.doc;
            const unmarked = key.getState(view.state);
            // A page not changed from the one marked last (a state put back) is marked anew.
            markChanged(view, unmarked && !unmarked.marked ? unmarked : { places: null, marked: false });
          },
          destroy() { marking.delete(made); },
        };
      },
    })];
  },
});
