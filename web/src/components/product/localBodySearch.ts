import type { ContentGraph } from "@/contentGraph";
import type { Page } from "@/types";
import type { WorkspaceSearchResult } from "@/workspaceSearchWire.generated";
import { readSearchKeywords, searchKeywordsHeld } from "@/components/search/searchModel";

type TextPiece = { insert?: unknown; attributes?: Record<string, unknown> };

/**
 * The open page's own words for a search, which its index may not hold yet:
 * as search keeps them, without a page or database mention's words, which
 * name a page the searcher may not read — Notion's search finds no page by
 * a mentioned page's title (seen 2026-11-09).
 */
export function localBodySearch(
  page: Page | null,
  graph: ContentGraph,
  document: { getText(name: string): { toDelta(): TextPiece[] } } | null | undefined,
  query: string,
): WorkspaceSearchResult[] {
  if (!page || !query.trim()) return [];
  // A line holding the keywords as the server's search judges them (searchModel.ts).
  const keywords = readSearchKeywords(query);
  const previews: string[] = [];
  if (searchKeywordsHeld(keywords, page.content, true)) {
    previews.push(page.content);
  }
  for (const block of graph.blocks) {
    if (block.type !== "paragraph") continue;
    const text = searchWords(document?.getText(`block:${block.id}`).toDelta() ?? []);
    if (searchKeywordsHeld(keywords, text, true)) previews.push(text);
  }
  return previews.slice(0, 1).map((preview) => ({ match: "body", page, preview }));
}

function searchWords(pieces: TextPiece[]) {
  return pieces.map(({ insert, attributes }) => {
    const mention = attributes?.mention;
    return typeof insert !== "string" || mention === "page" || mention === "database" ? "" : insert;
  }).join("");
}
