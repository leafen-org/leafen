import * as Y from "yjs";
import { emptyContentGraph, compareGraphOrder, graphTextBlockTypes, type ContentGraph, type GraphBlock } from "@/contentGraph";
import { newId } from "@/lib/ids";
import { writeRichTextToYText, yTextToRichText } from "@/lib/collaborationCrdtCodec";
import type { ButtonTemplateBlock } from "@/lib/types";

const textTypes = new Set<string>(graphTextBlockTypes);
/** An action draft owns an isolated document. No page, block or network write happens here. */
export function createAutomationBlockDraft(blocks: readonly ButtonTemplateBlock[] = [], freshIds = false) {
  const document = new Y.Doc(), rootId = newId();
  const graph: ContentGraph = { ...emptyContentGraph, blocks: [{ id: rootId, parentId: null, afterId: null, position: "root", type: "template", content: {} }] };
  function add(items: readonly ButtonTemplateBlock[], parentId: string) {
    items.forEach((item, index) => {
      const id = freshIds ? newId() : item.id ?? newId();
      const content = { ...item.content };
      if (textTypes.has(item.type)) writeRichTextToYText(document.getText(`block:${id}`),
        content.rich ?? [{ text: content.text ?? "" }]);
      delete content.text; delete content.rich;
      graph.blocks.push({ id, parentId, afterId: null, position: `a${String(index).padStart(8, "0")}:${id}`, type: item.type, content });
      add(item.children ?? [], id);
    });
  }
  add(blocks.length ? blocks : [{ type: "paragraph", content: { text: "" } }], rootId);
  return { document, graph, rootId };
}

export function automationBlocksFromDraft(graph: ContentGraph, document: Y.Doc, rootId: string): ButtonTemplateBlock[] {
  const children = new Map<string, GraphBlock[]>();
  for (const block of graph.blocks) if (block.parentId) {
    const siblings = children.get(block.parentId) ?? []; siblings.push(block); children.set(block.parentId, siblings);
  }
  const visit = (parentId: string): ButtonTemplateBlock[] => (children.get(parentId) ?? []).sort(compareGraphOrder).map(block => {
    const content = { ...block.content };
    if (textTypes.has(block.type)) {
      content.rich = yTextToRichText(document.getText(`block:${block.id}`)) ?? [];
    }
    const nested = visit(block.id);
    return { id: block.id, type: block.type, content, ...(nested.length ? { children: nested } : {}) };
  });
  return visit(rootId);
}
