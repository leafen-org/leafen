import { useEffect, useMemo, useState } from "react";
import type { PublicContentPageDocument } from "@/lib/publicContentDocument";
import { usePublicPageSource } from "@/lib/publicPageSource";
import { useCustomEmojiTitles } from "@/components/page/TitleWithCustomEmoji";
import type { PublicContentPageSummary } from "@/publicContentWire.generated";
import { MentionPreviewCard, useMentionHover } from "./mentionPreviewCard";
import { publicDatabaseCard, publicPageCard, publicPagesById } from "./publicMentionCards";

/**
 * The card a published page's page or database mention shows while the
 * pointer rests on it, as Notion's public pages do (seen 2026-10-01). A
 * mentioned page's first lines come from its first window, read through the
 * share link or site once while the page is open; a mention the visitor
 * can't open has no card.
 */
export function PublicMentionPreview({ pages, workspaceId }: {
  /** The published pages the visitor reads, for where a card's target sits. */
  pages: readonly PublicContentPageSummary[];
  workspaceId: string;
}) {
  const source = usePublicPageSource();
  const { shown, cardRef } = useMentionHover();
  const [windows, setWindows] = useState<Record<string, PublicContentPageDocument | null>>({});
  const pagesById = useMemo(() => publicPagesById(pages, workspaceId), [pages, workspaceId]);
  // Its place and title draw their custom emoji, as a member's card does (TitleWithCustomEmoji).
  const emojiTitles = useCustomEmojiTitles(pages);

  const pageId = shown?.kind === "page" ? shown.id : "";
  const known = pageId ? pageId in windows : true;
  useEffect(() => {
    if (!pageId || known || !source) return;
    const controller = new AbortController();
    source.read(pageId, undefined, controller.signal)
      .then((window) => setWindows((all) => ({ ...all, [pageId]: window })))
      .catch(() => {
        if (!controller.signal.aborted) setWindows((all) => ({ ...all, [pageId]: null }));
      });
    return () => controller.abort();
  }, [known, pageId, source]);

  // A data source's mention shows no card on a published page, as before.
  if (!shown || shown.kind === "dataSource") return null;
  const card = shown.kind === "database"
    ? publicDatabaseCard(shown.element, pagesById, workspaceId)
    : publicPageCard(shown.id, windows[shown.id], pagesById, workspaceId);
  return card ? <MentionPreviewCard shown={shown} card={{ ...card, emojiTitles }} cardRef={cardRef} /> : null;
}
