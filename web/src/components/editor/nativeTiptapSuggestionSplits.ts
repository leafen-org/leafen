import type { Node as ProseMirrorNode } from "@tiptap/pm/model";
import { Plugin, PluginKey } from "@tiptap/pm/state";
import { Decoration, DecorationSet } from "@tiptap/pm/view";

const key = new PluginKey<DecorationSet>("nativeSuggestionSplits");

type Removal = { id: string; value: string };

/** The suggested removal an annotation holds, by its id and the annotation's whole value. */
function removalOf(node: ProseMirrorNode | null): Removal | null {
  const value = node?.isText ? node.marks.find((mark) => mark.type.name === "nativeAnnotation")?.attrs.stringValue : undefined;
  if (typeof value !== "string") return null;
  try {
    const parsed = JSON.parse(value) as { suggestion?: unknown; suggestionId?: unknown };
    return parsed.suggestion === "delete" && typeof parsed.suggestionId === "string" ? { id: parsed.suggestionId, value } : null;
  } catch {
    return null;
  }
}

function splits(doc: ProseMirrorNode): DecorationSet {
  const found: Decoration[] = [];
  doc.descendants((node, pos) => {
    if (!node.isTextblock) return true;
    let previous: { removal: Removal; from: number; to: number } | null = null;
    node.forEach((child, offset) => {
      const removal = removalOf(child);
      const from = pos + 1 + offset;
      if (removal && previous && previous.removal.id === removal.id && previous.removal.value !== removal.value) {
        found.push(Decoration.inline(previous.from, previous.to, { "data-suggestion-split": "end" }));
      }
      previous = removal ? { removal, from, to: from + child.nodeSize } : null;
    });
    return false;
  });
  return found.length ? DecorationSet.create(doc, found) : DecorationSet.empty;
}

/**
 * A suggested removal that a comment's edge splits into pieces, each drawn
 * as its own span, keeps 2px after each piece another piece of it follows,
 * as Notion's does (seen 2026-11-15 twice: margin-inline-end 2px on the
 * piece before the comment's edge, either way across it; none on a removal
 * left whole or on its last piece). The piece is marked here, for the
 * annotation mark's editors (nativeTiptapSchema.ts), and its span spaced in
 * EditorInlineMarks.module.css.
 */
export function suggestionSplitsPlugin() {
  return new Plugin<DecorationSet>({
    key,
    state: {
      init: (_config, state) => splits(state.doc),
      apply: (tr, set) => tr.docChanged ? splits(tr.doc) : set,
    },
    props: { decorations: (state) => key.getState(state) },
  });
}
