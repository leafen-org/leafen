import { Extension } from "@tiptap/core";
import { DOMParser, type Fragment } from "@tiptap/pm/model";
import { Plugin, PluginKey, type EditorState } from "@tiptap/pm/state";

const NO_BREAK = " ";
const typedSpaces = new PluginKey<readonly number[]>("nativeFieldTypedSpaces");

/**
 * A space typed in a field is saved as a space. A browser types U+00A0 for a
 * space where the field's element would fold an ordinary one away — at the
 * end of its words, when the element is read without the stylesheet that
 * keeps its white space (2026-11-17: a table cell's typed "now @Road" was
 * saved "now Roadmap " once its "@" became a page mention) — and does
 * not always turn it back as more is typed. With the stylesheet Chrome types
 * ordinary spaces (seen 2026-11-18 at 1440). The field keeps the characters
 * as the browser wrote them, so the caret and its spacing stay, and
 * remembers where they are; its words are saved with spaces there
 * (fieldWordsTyped). A U+00A0 pasted or already in the value stays one. The
 * formula editor reads every one as a space (formulaInputEngine.tsx): a
 * formula's text never means one.
 */
export const FieldTypedSpaces = Extension.create({
  name: "nativeFieldTypedSpaces",
  addProseMirrorPlugins() {
    const parser = DOMParser.fromSchema(this.editor.schema);
    // Set while the view reads what the browser changed in the field: the
    // change it makes of that, in the same task, is typing.
    let reading = false;
    const reader = Object.create(parser) as DOMParser;
    reader.parse = (dom, options) => {
      reading = true;
      queueMicrotask(() => { reading = false; });
      return parser.parse(dom, options);
    };
    return [new Plugin<readonly number[]>({
      key: typedSpaces,
      // A paste is read as before, by the schema's own parser.
      props: { domParser: reader, clipboardParser: parser },
      state: {
        init: () => [],
        apply(tr, places, _before, state) {
          if (!tr.docChanged) return places;
          const typed = reading;
          reading = false;
          const next = new Set<number>();
          for (const place of places) {
            const mapped = tr.mapping.mapResult(place, 1);
            if (!mapped.deleted) next.add(mapped.pos);
          }
          if (typed) tr.mapping.maps.forEach((map, index) => map.forEach((_from, _to, start, end) => {
            const after = tr.mapping.slice(index + 1), from = after.map(start, -1), to = after.map(end, 1);
            state.doc.nodesBetween(from, to, (node, position) => {
              if (!node.isText) return;
              for (let at = node.text!.indexOf(NO_BREAK); at >= 0; at = node.text!.indexOf(NO_BREAK, at + 1)) {
                if (position + at >= from && position + at < to) next.add(position + at);
              }
            });
          }));
          return [...next].filter(at => at + 1 <= state.doc.content.size && state.doc.textBetween(at, at + 1) === NO_BREAK)
            .sort((a, b) => a - b);
        },
      },
    })];
  },
});

/** A field's words as they are saved: the spaces typed as U+00A0 are spaces. */
export function fieldWordsTyped(state: EditorState): Fragment {
  const places = typedSpaces.getState(state);
  if (!places?.length) return state.doc.firstChild!.content;
  const tr = state.tr;
  for (const at of places) tr.replaceWith(at, at + 1, state.schema.text(" ", state.doc.nodeAt(at)?.marks));
  return tr.doc.firstChild!.content;
}
