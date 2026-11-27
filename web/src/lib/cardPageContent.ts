import { createContext, useContext } from "react";
import { readDatabaseRowPage } from "@/databaseRowPageApi";
import type { DatabaseRowPageBodyBlock } from "@/databaseRowPageWire.generated";
import { feedPostBlockText } from "@/lib/feedPostService";
import { nativeWorkspaceFileLocator } from "@/lib/workspaceFileTransport";
import type { Session } from "@/types";

const CACHED_PAGES = 300;
const PARALLEL_READS = 4;

/** What a card's "페이지 콘텐츠" draws: nothing, a picture across it, or one block's own line. */
export type CardPreviewLine = { image: string } | { block: DatabaseRowPageBodyBlock } | null;

/**
 * The first blocks a card's "페이지 콘텐츠" draws: a line of words — text, a
 * heading, a list item, a to-do, a toggle, a quote, a callout — or a link to
 * a page, as Notion's (seen 2026-11-17 in a gallery's medium cards and,
 * in another tab, a board's cards). A picture is drawn across the preview
 * (cardPreviewLine); anything else first — code, an equation, a table, a
 * bookmark, an embed, a file, a video, an audio, a PDF, a table of contents,
 * a breadcrumb, columns, a synced block, a database, a divider — draws
 * nothing, even with words in it or after it.
 */
const CARD_LINE_BLOCK_TYPES: ReadonlySet<string> = new Set([
  "paragraph",
  "heading_1", "heading_2", "heading_3", "heading_4",
  "toggle_heading_1", "toggle_heading_2", "toggle_heading_3", "toggle_heading_4",
  "bulleted_list_item", "numbered_list_item", "to_do", "toggle", "quote", "callout",
  "child_page", "link_to_page",
]);

/**
 * A card's "페이지 콘텐츠" is its page's first block alone, as Notion's says
 * ("페이지에서 First Block을 사용하세요") and draws it (seen 2026-11-17 in
 * gallery cards of every size and board cards): its own line without what is
 * inside it (a toggle's, a list item's, a quote's or a callout's lines left
 * out), a picture across the whole preview, and nothing for a first block of
 * another kind (CARD_LINE_BLOCK_TYPES), an empty line or a callout with no
 * words — the lines after it never. A callout's words are its first line
 * (one made on screen or imported keeps them there,
 * notionImportCalloutFirstLine), read with it.
 */
export function cardPreviewLine(blocks: readonly DatabaseRowPageBodyBlock[]): CardPreviewLine {
  const first = blocks.find((block) => !block.parentId);
  if (!first) return null;
  if (first.type === "image") {
    const fileId = typeof first.content.fileId === "string" ? first.content.fileId : "";
    const url = typeof first.content.url === "string" ? first.content.url : "";
    const image = fileId ? nativeWorkspaceFileLocator(fileId) : url;
    return image ? { image } : null;
  }
  if (!CARD_LINE_BLOCK_TYPES.has(first.type)) return null;
  if (first.type === "callout" && !feedPostBlockText(first).trim()) {
    const line = blocks.find((block) => block.parentId === first.id);
    const words = line ? feedPostBlockText(line) : "";
    return line && words.trim() ? { block: { ...first, content: { ...first.content, rich: line.content.rich, text: words } } } : null;
  }
  if (first.type === "paragraph" && !feedPostBlockText(first).trim()) return null;
  return { block: first };
}

/**
 * The cards' pages' first block and the first line inside it (a callout's
 * words, cardPreviewLine): each read once at the revision its card has, a few
 * at a time, and read again once its page was open (where it may have
 * changed) — its cards hear of it.
 */
export type CardPageContent = {
  read(rowId: string, revision: number | undefined): Promise<readonly DatabaseRowPageBodyBlock[]>;
  /** How many times the page was marked changed; a card reads again when it grows. */
  changes(rowId: string): number;
  changed(rowId: string): void;
  subscribe(listener: () => void): () => void;
};

export function createCardPageContent(session: Session, readPage: typeof readDatabaseRowPage = readDatabaseRowPage): CardPageContent {
  const cache = new Map<string, Promise<readonly DatabaseRowPageBodyBlock[]>>();
  const changeCounts = new Map<string, number>();
  const listeners = new Set<() => void>();
  const waiting: (() => void)[] = [];
  let running = 0;
  async function inTurn<T>(run: () => Promise<T>) {
    while (running >= PARALLEL_READS) await new Promise<void>((resolve) => waiting.push(resolve));
    running += 1;
    try {
      return await run();
    } finally {
      running -= 1;
      waiting.shift()?.();
    }
  }
  return {
    read(rowId, revision) {
      const key = `${rowId}:${revision ?? ""}:${changeCounts.get(rowId) ?? 0}`;
      const cached = cache.get(key);
      if (cached) return cached;
      const reading = inTurn(() => readPage(session, rowId, { firstBlock: true }).then((snapshot) => snapshot.body));
      // A page the viewer can't read, or a failed read, shows no content and is asked again next time.
      reading.catch(() => cache.delete(key));
      cache.set(key, reading);
      if (cache.size > CACHED_PAGES) cache.delete(cache.keys().next().value as string);
      return reading;
    },
    changes: (rowId) => changeCounts.get(rowId) ?? 0,
    changed(rowId) {
      changeCounts.set(rowId, (changeCounts.get(rowId) ?? 0) + 1);
      for (const listener of listeners) listener();
    },
    subscribe(listener) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
  };
}

export const CardPageContentContext = createContext<CardPageContent | null>(null);

export function useCardPageContent() {
  return useContext(CardPageContentContext);
}
