import { Extension } from "@tiptap/core";
import type { Node as ProseNode } from "@tiptap/pm/model";
import { Plugin, PluginKey, type Transaction } from "@tiptap/pm/state";
import { ReplaceAroundStep, ReplaceStep } from "@tiptap/pm/transform";
import { Decoration, DecorationSet, type EditorView } from "@tiptap/pm/view";
import "./prismManual";
import * as Prism from "prismjs";
import { loadedPrismGrammar, loadPrismGrammar, prismGrammarId } from "./codeHighlightPrismGrammars";

// A code block's words take their colors while they are typed, as Notion's
// (seen 2026-11-06: "let" turns blue as "let b" is typed): each Prism piece
// of the code being edited carries its token names, which codeTheme.css colors
// under `.code-highlight`, the same names the colored copy shown at rest uses.
const key = new PluginKey<DecorationSet>("nativeCodeColors");
type Piece = readonly [from: number, to: number, names: string];
const pieceCache = new WeakMap<ProseNode, { grammar: string; pieces: readonly Piece[] }>();

function walk(stream: Prism.TokenStream, names: readonly string[], at: number, out: Piece[]): number {
  if (typeof stream === "string") {
    if (names.length && stream.length) out.push([at, at + stream.length, `token ${names.join(" ")}`]);
    return at + stream.length;
  }
  if (Array.isArray(stream)) return stream.reduce((next: number, part) => walk(part, names, next, out), at);
  const own = [stream.type, ...[stream.alias ?? []].flat()].filter((name) => /^[\w-]+$/u.test(name));
  return walk(stream.content, [...own, ...names], at, out);
}

function piecesOf(code: ProseNode, grammarId: string, grammar: Prism.Grammar) {
  const cached = pieceCache.get(code);
  if (cached?.grammar === grammarId) return cached.pieces;
  const pieces: Piece[] = [];
  walk(Prism.tokenize(code.textContent, grammar), [], 0, pieces);
  pieceCache.set(code, { grammar: grammarId, pieces });
  return pieces;
}

/**
 * Where a transaction changed its document, in the new document's positions,
 * or null when a step of it changes what has no range (a node's attribute):
 * then every code block is looked at again.
 */
function changedRanges(tr: Transaction) {
  if (tr.steps.some(step => !(step instanceof ReplaceStep || step instanceof ReplaceAroundStep))) return null;
  const ranges: [number, number][] = [];
  tr.mapping.maps.forEach((map, index) => map.forEach((_from, _to, start, end) => {
    const after = tr.mapping.slice(index + 1);
    ranges.push([Math.max(0, after.map(start, -1) - 1), Math.min(tr.doc.content.size, after.map(end, 1) + 1)]);
  }));
  return ranges;
}

export const NativeTiptapCodeColors = Extension.create({
  name: "nativeCodeColors",
  addProseMirrorPlugins() {
    let view: EditorView | null = null;
    const loading = new Set<string>();
    // A code block's colors at pos, from its block's language; its grammar is fetched when first needed.
    const codeColors = (node: ProseNode, pos: number, parent: ProseNode | null) => {
      const language = String((parent?.attrs.props as { language?: unknown } | undefined)?.language ?? "");
      const id = language ? prismGrammarId(language) : null;
      if (!id || !node.textContent) return [];
      const grammar = loadedPrismGrammar(id);
      if (!grammar) {
        if (!loading.has(id)) {
          loading.add(id);
          loadPrismGrammar(id).then(() => { if (view && !view.isDestroyed) view.dispatch(view.state.tr.setMeta(key, true)); }, () => undefined);
        }
        return [];
      }
      return [Decoration.node(pos, pos + node.nodeSize, { class: "code-highlight" }),
        ...piecesOf(node, id, grammar).map(([from, to, names]) => Decoration.inline(pos + 1 + from, pos + 1 + to, { class: names }))];
    };
    // The code blocks from from to to; lines' text is not walked into.
    const eachCode = (doc: ProseNode, from: number, to: number, found: (node: ProseNode, pos: number, parent: ProseNode | null) => void) =>
      doc.nodesBetween(from, to, (node, pos, parent) => {
        if (node.type.name !== "nativeCode") return node.isBlock && !node.isTextblock;
        found(node, pos, parent);
        return false;
      });
    const colors = (doc: ProseNode) => {
      const decorations: Decoration[] = [];
      eachCode(doc, 0, doc.content.size, (node, pos, parent) => { decorations.push(...codeColors(node, pos, parent)); });
      return DecorationSet.create(doc, decorations);
    };
    // Only the code blocks a change touched are colored again: the whole page
    // was walked at each of its transactions, 7 ms of an Enter on a
    // 600-block page with no code in it (2026-11-14, CPU ×2).
    const recolor = (tr: Transaction, old: DecorationSet) => {
      const ranges = changedRanges(tr);
      if (!ranges) return colors(tr.doc);
      let set = old.map(tr.mapping, tr.doc);
      for (const [from, to] of ranges) eachCode(tr.doc, from, to, (node, pos, parent) => {
        set = set.remove(set.find(pos, pos + node.nodeSize)).add(tr.doc, codeColors(node, pos, parent));
      });
      return set;
    };
    return [new Plugin<DecorationSet>({
      key,
      state: {
        init: (_, state) => colors(state.doc),
        apply: (tr, old) => tr.getMeta(key) ? colors(tr.doc) : tr.docChanged ? recolor(tr, old) : old,
      },
      view: (editorView) => { view = editorView; return { destroy: () => { view = null; } }; },
      props: { decorations: (state) => key.getState(state) ?? null },
    })];
  },
});
