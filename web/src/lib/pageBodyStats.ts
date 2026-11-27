import type * as Y from "yjs";
import type { ContentGraph, GraphBlock } from "@/contentGraph";
import { countedSpans, countedText } from "./countedText";
import { rowMenuDocumentStats, type RowMenuDocumentStats } from "./rowMenuDocumentStats";

// A sub-page block's page, as the editor's page blocks name it.
function childPageId(block: GraphBlock) {
  for (const value of [block.content.childPageId, block.content.pageId]) {
    if (typeof value === "string" && value.trim()) return value.trim();
  }
  return "";
}

/**
 * A page's words as its "⋯" counts them, as Notion's (seen 2026-11-04): its
 * blocks' text, a caption's, a simple table's cells' and a sub-page's title
 * (2026-11-14: "let x = 1" in a JavaScript code block, "red words" in red, a
 * bookmark captioned "cap one two" and a sub-page "sub" make 4 + 2 + 3 + 1,
 * the code block's language, the colour, the bookmark's address and name
 * never counted). A database in the body adds its name alone, not the source
 * and view its content names (2026-11-13: Notion's "peek body one" over a
 * database "w729 표" counts 5; Leafen's counted the ids, a wiki's "wiki body
 * one" as 9). Mentions and equations count as drawn (countedText.ts).
 * `titleOf` names a sub-page by its id.
 */
export function pageBodyStats(graph: ContentGraph, document: Y.Doc | null | undefined, pageContent = "",
  titleOf?: (pageId: string) => string | undefined): RowMenuDocumentStats {
  const parts = [pageContent];
  for (const block of graph.blocks) {
    if (block.type === "inline_database") {
      parts.push(graph.dataSources.find((source) => source.id === block.content.dataSourceId)?.title ?? "");
      continue;
    }
    if (block.type === "child_page") {
      const id = childPageId(block);
      parts.push((id && titleOf?.(id)) || (typeof block.content.childPageTitle === "string" ? block.content.childPageTitle : ""));
      continue;
    }
    if (block.type === "table_row") {
      const cells = Array.isArray(block.content.cells) ? block.content.cells : [];
      parts.push(...cells.map(countedSpans));
      continue;
    }
    // A divider holds no words: what a line made one by "---" left in its
    // text before 2026-11-13 is none (nativeTiptapBinding empties it now).
    const collaborativeText = block.type === "divider" ? "" : countedText(document?.getText(`block:${block.id}`));
    parts.push(collaborativeText, countedSpans(block.content.caption));
  }
  return rowMenuDocumentStats(parts, graph.blocks.length + (pageContent ? 1 : 0));
}

/**
 * The body a page is open in — whole, or in a side or center peek — read by
 * that page's "⋯" for its word count, its words as typed there: Notion's peek
 * menus count the body shown (seen 2026-11-11; 2026-11-18 in two tabs, a
 * database's page and a wiki's page in a side and a center peek, with a word
 * typed just before). Each body says it while it is open (RowPageBodyEditor);
 * the product's own page is read from the product's graph (usePageMenuActions).
 */
const openBodies = new Map<string, () => RowMenuDocumentStats>();

export function holdPageBodyStats(pageId: string, read: () => RowMenuDocumentStats) {
  openBodies.set(pageId, read);
  return () => {
    if (openBodies.get(pageId) === read) openBodies.delete(pageId);
  };
}

export function openPageBodyStats(pageId: string): RowMenuDocumentStats | undefined {
  return openBodies.get(pageId)?.();
}
