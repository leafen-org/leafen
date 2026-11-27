import { Fragment, useMemo, type ReactNode } from "react";
import { PlaceLine } from "@/components/PlaceLine";
import { InlineCustomEmoji } from "@/components/editor/InlineCustomEmoji";
import { namesOtherPages, plainTitlePieces, titleCustomEmojiPieces } from "@/lib/closedMentions";
import type { Place } from "@/lib/pagePath";
import { useMentionedPageNames, useNamedPage, usePageName, type HeldPageName } from "@/lib/pageNames";
import type { TextSpan } from "@/lib/types";
import { usePeopleDirectoryRevision } from "@/lib/workspacePeopleDirectory";

/** What names a title: its words and the spans spelling them. */
export type NamedTitle = { title?: string | null; titleSpans?: readonly TextSpan[]; workspaceId?: string };

/**
 * A page's title as every list, path, link and card draws it: its plain
 * title (titleWithClosedMentions) with each custom emoji in it drawn as its
 * picture, as Notion's do (seen 2026-11-11: the sidebar, the path, a
 * favorite, a search result and its path, a mention, a page link and a
 * page's card; one the workspace hasn't as the dashed "?" at 1.25em, 0.5em
 * after it, on the text's top, as in a line of text — InlineCustomEmoji).
 * Text that isn't the title spelled out (a name it stands in for, a cut one)
 * is drawn as it is; its words by drawText, where a list marks them.
 */
export function TitleWithCustomEmoji({ text, named, workspaceId, drawText }: {
  text: string;
  named?: NamedTitle | null;
  workspaceId?: string;
  drawText?: (text: string) => ReactNode;
}) {
  const pieces = titleCustomEmojiPieces(named?.title ?? "", named?.titleSpans);
  if (!pieces || plainTitlePieces(pieces) !== text) return <>{drawText ? drawText(text) : text}</>;
  return (
    <>
      {pieces.map((piece, index) => typeof piece === "string"
        ? <Fragment key={index}>{drawText ? drawText(piece) : piece}</Fragment>
        : <InlineCustomEmoji key={index} workspaceId={workspaceId ?? named?.workspaceId} emojiId={piece.customEmojiId ?? ""} name={piece.text}
          publicToken={piece.customEmojiPublic} />)}
    </>
  );
}

/** A page as its name is shown. */
type NamedPage = Parameters<typeof usePageName>[0];

/**
 * A page's name as every place shows it, "" for none, by the owner of pages'
 * names (lib/pageNames.ts): read again as the page is renamed anywhere, and
 * as a page or database its title mentions is renamed — the titles kept for
 * mentions tell of it (setMentionPageTitles) — as Notion's sidebar row and
 * search result follow the name (closedMentions.ts).
 */
export function usePageDisplayTitle(page: NamedPage) {
  const named = useNamedPage(page as (NonNullable<NamedPage> & HeldPageName) | null | undefined);
  usePeopleDirectoryRevision(namesOtherPages(named?.titleSpans));
  useMentionedPageNames(named?.titleSpans);
  return usePageName(named);
}

/** A page's name as every place shows it, its custom emoji drawn (TitleWithCustomEmoji). */
export function PageTitleText({ page, drawText }: {
  page: NamedPage & { workspaceId?: string };
  drawText?: (text: string) => ReactNode;
}) {
  const named = useNamedPage(page as (NonNullable<NamedPage> & HeldPageName & { workspaceId?: string }) | null | undefined);
  return <TitleWithCustomEmoji text={usePageDisplayTitle(named)} named={named} drawText={drawText} />;
}

/** Whether a title holds a custom emoji a screen draws. */
function holdsCustomEmoji(named: NamedTitle) {
  return Boolean(named.titleSpans?.some((span) => span.mention === "custom_emoji" && span.customEmojiId));
}

/**
 * The titles holding a custom emoji among the given pages, by their plain
 * title, for a place of their names to draw them (PlaceWithCustomEmoji).
 */
export function useCustomEmojiTitles(pages: readonly NamedTitle[] | undefined) {
  return useMemo(() => {
    const titles = new Map<string, NamedTitle>();
    for (const page of pages ?? []) {
      if (!holdsCustomEmoji(page)) continue;
      const pieces = titleCustomEmojiPieces(page.title ?? "", page.titleSpans);
      if (pieces) titles.set(plainTitlePieces(pieces), page);
    }
    return titles as ReadonlyMap<string, NamedTitle>;
  }, [pages]);
}

/**
 * A place of titles (PlaceLine), each title holding a custom emoji drawn
 * with it as Notion's paths do (seen 2026-11-11, a search result's path:
 * 15px at 12px text, 6px after); its words by drawText.
 */
export function PlaceWithCustomEmoji({ place, titles, drawText, className }: {
  place: Place;
  titles: ReadonlyMap<string, NamedTitle>;
  drawText?: (text: string) => ReactNode;
  className?: string;
}) {
  return <PlaceLine place={place} className={className}
    drawTitle={(title) => <TitleWithCustomEmoji text={title} named={titles.get(title)} drawText={drawText} />} />;
}
