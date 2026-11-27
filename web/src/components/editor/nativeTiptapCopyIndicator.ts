import { Extension } from "@tiptap/core";
import { Plugin, PluginKey } from "@tiptap/pm/state";
import { Decoration, DecorationSet } from "@tiptap/pm/view";
import { blockItemText } from "./blockItemLabels";
import { projectedBlocks } from "./nativeTiptapBinding";
import styles from "./editorStyles.generated.module.css";

const COPYING = "leafen:copying-block";
const key = new PluginKey<Indicators>("nativeCopyIndicator");
/** A copy's place: after a block, until the copy (`until`, once known) is there. */
export type CopyPlace = { after: string; until?: string };
type Request = { owner: string; places: readonly CopyPlace[] };
type Indicators = { asked: ReadonlyMap<string, readonly CopyPlace[]>; set: DecorationSet };

/** Shows that a copy is being made on the server, right after the block it copies; null takes it away. */
export function requestEditorCopyIndicator(blockId: string | null) {
  requestEditorCopyIndicators("duplicate", blockId ? [{ after: blockId }] : []);
}

/**
 * Shows copies an action of `owner`'s is waiting for, each right after the
 * block it will follow, several after one in their order, until the copy is
 * there; none takes them away.
 */
export function requestEditorCopyIndicators(owner: string, places: readonly CopyPlace[]) {
  window.dispatchEvent(new CustomEvent<Request>(COPYING, { detail: { owner, places } }));
}

function indicator() {
  const element = document.createElement("div");
  element.className = styles.copyIndicator;
  element.contentEditable = "false";
  element.setAttribute("role", "status");
  element.textContent = blockItemText("database.copying");
  return element;
}

/**
 * A copy made on the server has its place at once, as Notion's "불러오는 중..."
 * block has, chosen, until the copy takes it (seen 2026-11-05); so has each
 * database a paste brings, among the pasted lines (seen 2026-11-08). It is
 * drawn over the page: nothing of it is the page's until the copy comes.
 */
export const NativeTiptapCopyIndicator = Extension.create({
  name: "nativeCopyIndicator",
  addProseMirrorPlugins() {
    const draw = (doc: Parameters<typeof DecorationSet.create>[0], asked: Indicators["asked"]) => {
      if (!asked.size) return DecorationSet.empty;
      const blocks = new Map(projectedBlocks(doc).map((block) => [block.nativeId, block]));
      const widgets = [...asked].flatMap(([owner, places]) => places.flatMap(({ after, until }, index) => {
        const block = blocks.get(after);
        return block && !(until && blocks.has(until))
          ? [Decoration.widget(block.contentFrom - 2 + block.container.nodeSize, indicator, { side: -1, key: `copy-indicator-${owner}-${index}` })] : [];
      }));
      return DecorationSet.create(doc, widgets);
    };
    return [new Plugin<Indicators>({
      key,
      state: {
        init: () => ({ asked: new Map(), set: DecorationSet.empty }),
        apply(tr, value) {
          const request = tr.getMeta(key) as Request | undefined;
          if (!request) return !value.asked.size ? value : { ...value, set: tr.docChanged ? draw(tr.doc, value.asked) : value.set.map(tr.mapping, tr.doc) };
          const asked = new Map(value.asked);
          if (request.places.length) asked.set(request.owner, request.places); else asked.delete(request.owner);
          return { asked, set: draw(tr.doc, asked) };
        },
      },
      props: { decorations: (state) => key.getState(state)?.set },
      view: (view) => {
        const listen = (event: Event) => view.dispatch(view.state.tr.setMeta(key, (event as CustomEvent<Request>).detail).setMeta("addToHistory", false));
        window.addEventListener(COPYING, listen);
        return { destroy: () => window.removeEventListener(COPYING, listen) };
      },
    })];
  },
});
