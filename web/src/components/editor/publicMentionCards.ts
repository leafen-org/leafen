import type { GraphBlock, GraphReadItem } from "@/contentGraph";
import type { PublicContentPageDocument } from "@/lib/publicContentDocument";
import { customEmojiNeverHad } from "@/lib/closedMentions";
import { pagePlace, placeUnder } from "@/lib/pagePath";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import { pageNamesNow } from "@/lib/pageNames";
import type { Page } from "@/types";
import type { TextSpan } from "@/lib/types";
import type { PublicContentPageSummary } from "@/publicContentWire.generated";
import { workspaceSearchBounds } from "@/workspaceSearchWire.generated";
import type { MentionCard } from "./mentionPreviewCard";
import type { PreviewLineBlock } from "@/components/search/SearchPagePreview";

// A published page's mention card (Notion, seen 2026-10-01 on a page
// published to the web): for a page the visitor opens, its icon, the pages
// above it that are published, its title and its first lines; for a
// database, a table icon, the page it sits in and its name over a picture of
// a table. A page or database the visitor can't open has no card.

/** The published pages the visitor reads, as pages to place a card's target among. */
export function publicPagesById(pages: readonly PublicContentPageSummary[], workspaceId: string): Record<string, Page> {
  return Object.fromEntries(pages.map((page) => [page.id, {
    id: page.id,
    title: page.title,
    parentId: page.parentId || null,
    icon: page.icon,
    iconType: page.iconType || "none",
    workspaceId,
    ...(page.titleSpans ? { titleSpans: page.titleSpans } : {}),
  } as Page]));
}

// A line's words as the app's card reads them (searchTextReader), and its
// formatted text, which the card draws as the page does: a custom emoji the
// workspace never had says nothing.
function lineText(block: GraphBlock): { text: string; spans?: TextSpan[] } {
  const rich = Array.isArray(block.content.rich) ? block.content.rich as TextSpan[] : [];
  const unsaid = (span: TextSpan) => span.mention === "custom_emoji" && customEmojiNeverHad(span.customEmojiId ?? "", span.text);
  const spans = rich.filter((span) => !unsaid(span));
  if (rich.some(unsaid)) return { text: spans.map((span) => span.text).join("").trim(), spans };
  return { text: typeof block.content.text === "string" ? block.content.text.trim() : "", ...(spans.length ? { spans } : {}) };
}

/**
 * A page's first lines with text, from its body in order, each a line of
 * its formatted text (cut as a short line where it's drawn,
 * lib/richTextSummary.ts) and its words cut to a line's worth.
 */
export function publicPreviewBlocks(graph: readonly GraphReadItem[]): PreviewLineBlock[] {
  const blocks = graph.flatMap((item) => item.kind === "block" ? [item.value as GraphBlock] : []);
  const children = new Map<string | null, GraphBlock[]>();
  for (const block of blocks) children.set(block.parentId, [...(children.get(block.parentId) ?? []), block]);
  for (const list of children.values()) {
    list.sort((left, right) => (left.position < right.position ? -1 : left.position > right.position ? 1 : left.id < right.id ? -1 : 1));
  }
  const lines: PreviewLineBlock[] = [];
  const visit = (parentId: string | null) => {
    for (const block of children.get(parentId) ?? []) {
      if (lines.length >= workspaceSearchBounds.previewLines) return;
      const { text, spans } = lineText(block);
      if (text) {
        const runes = Array.from(text);
        const line = runes.length > workspaceSearchBounds.previewLineRunes
          ? `${runes.slice(0, workspaceSearchBounds.previewLineRunes).join("").trim()}…` : text;
        lines.push({ type: "paragraph", text: line, depth: 0, ...(spans ? { spans } : {}) });
      }
      visit(block.id);
    }
  };
  visit(null);
  return lines;
}

/**
 * A page mention's card: what the page list knows at once, and its first
 * lines once its window is read; nothing for a page the visitor can't read.
 */
export function publicPageCard(
  pageId: string,
  window: PublicContentPageDocument | null | undefined,
  pagesById: Record<string, Page>,
  workspaceId: string,
): MentionCard | null {
  const listed = pagesById[pageId];
  if (window === null || (!listed && !window)) return null;
  const page = window?.page ?? listed;
  const parentId = listed ? listed.parentId : window?.page.parentId;
  return {
    icon: { icon: page.icon, iconType: page.iconType, workspaceId },
    fallback: "file",
    location: parentId ? placeUnder(parentId, pagesById) : [],
    title: pageNamesNow.title(page),
    named: page,
    lines: [],
    ...(window ? { blocks: publicPreviewBlocks(window.graph) } : {}),
    table: false,
  };
}

/**
 * A database mention's card, from the mention itself: its name, and the page
 * holding it, which is the database's own page for a full-page database.
 */
export function publicDatabaseCard(
  mention: HTMLElement,
  pagesById: Record<string, Page>,
  workspaceId: string,
): MentionCard {
  const holder = pagesById[mention.dataset.databasePageId ?? ""];
  const fullPage = mention.dataset.databasePageKind === "database";
  return {
    icon: fullPage && holder ? holder : { icon: "", iconType: "none", workspaceId },
    fallback: "database",
    location: holder ? (fullPage ? pagePlace(holder, pagesById) : placeUnder(holder.id, pagesById)) : [],
    title: (fullPage && holder ? pageNamesNow.title(holder) : mention.textContent?.trim()) || untitledPageDisplayTitle("database"),
    named: fullPage ? holder : undefined,
    lines: [],
    table: true,
  };
}
