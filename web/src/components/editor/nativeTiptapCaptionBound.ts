import { Extension } from "@tiptap/core";
import type { Node as ProseMirrorNode } from "@tiptap/pm/model";
import { Plugin, type Transaction } from "@tiptap/pm/state";
import { graphWireBounds } from "@/contentGraphWire.generated";
import { fieldTakes } from "@/lib/pasteBound";
import { nativeProjectionMeta } from "./nativeTiptapBinding";

// A caption's words as the server counts them: a mention or an equation by its words.
const words = (node: ProseMirrorNode) => node.textBetween(0, node.content.size, "", leaf =>
  leaf.type.name === "nativeAtom" ? String(leaf.attrs.text) : leaf.type.name === "hardBreak" ? "\n" : "");

/** The captions a transaction wrote into, by where each starts after it. */
function captionsWritten(tr: Transaction) {
  const written = new Map<number, ProseMirrorNode>();
  tr.steps.forEach((step, index) => {
    const after = tr.mapping.slice(index + 1);
    step.getMap().forEach((_oldStart, _oldEnd, newStart, newEnd) => {
      if (newEnd <= newStart) return;
      const from = after.map(newStart, -1), to = Math.min(after.map(newEnd, 1), tr.doc.content.size);
      tr.doc.nodesBetween(from, to, (node, position) => {
        if (node.type.name !== "nativeCaption") return !node.isTextblock;
        written.set(position, node);
        return false;
      });
    });
  });
  return written;
}

/**
 * A caption holds a text's characters (graphWireBounds.textCharacters):
 * typing or pasting past them is refused where it is typed, as a page's
 * title refuses it (fieldTakes), rather than written and refused by the
 * server. What the page's document brings in is taken as it comes.
 */
export const NativeTiptapCaptionBound = Extension.create({
  name: "nativeCaptionBound",
  addProseMirrorPlugins: () => [new Plugin({
    filterTransaction: tr => {
      if (!tr.docChanged || tr.getMeta(nativeProjectionMeta)) return true;
      for (const [position, caption] of captionsWritten(tr)) {
        if (!fieldTakes(words(caption), graphWireBounds.textCharacters, () => {
          const before = tr.before.nodeAt(tr.mapping.invert().map(position, 1));
          return before?.type.name === "nativeCaption" ? words(before) : "";
        })) return false;
      }
      return true;
    },
  })],
});
