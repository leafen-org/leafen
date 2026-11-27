import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { FileText, LinkIcon, OpenInNew } from "@/icons/leafen";
import { personValue, storePersonValue } from "@/lib/browserStorage";
import { copyPageLink } from "@/components/editor/pageLinkClipboard";
import { filePreviewWidth, useWorkspaceFileUrl } from "@/lib/fileUrls";
import { readPageLinkToCopy } from "@/lib/databasePageLink";
import { databasePagePlace, pagePlace, placeText } from "@/lib/pagePath";
import { usePageNames } from "@/lib/pageNames";
import { usePagePathsAbove } from "@/lib/usePagePathsAbove";
import { useOptionalProductToast } from "@/lib/productToast";
import { hasExplicitPageIcon, PageIconGlyph } from "@/components/page/PageIcon";
import { coverImage } from "@/components/page/pagePresentation";
import { PageTitleText, PlaceWithCustomEmoji, useCustomEmojiTitles } from "@/components/page/TitleWithCustomEmoji";
import type { Page, Session } from "@/types";
import type { PagePreviewBlock } from "@/workspaceSearchWire.generated";
import type { SearchHit } from "./searchModel";
import { searchLineInFile, searchPreviewLine } from "./searchResultDetails";
import { HighlightedText } from "./SearchResults";
import { searchTypedStart } from "./searchMarks";
import { SearchPagePreview } from "./SearchPagePreview";
import { CoverCredit } from "@/components/page/CoverCredit";
import { SearchSkeletonCard } from "./SearchSkeleton";
import styles from "./SearchHighlightPanel.module.css";

const NO_PAGES: readonly Page[] = [];

const HIGHLIGHTS_SHOWN_KEY = "leafen:v2:quick-find:highlights";
const HIGHLIGHTS_READ_DELAY_MS = 120;

/** Whether the reader keeps the highlights beside the results, as Notion's search remembers. */
export function useSearchHighlightsShown() {
  // The person's own choice; without storage it lasts until the dialog closes.
  const [shown, setShown] = useState(() => personValue(HIGHLIGHTS_SHOWN_KEY, { previous: [HIGHLIGHTS_SHOWN_KEY] }) !== "hidden");
  const change = (next: boolean) => {
    setShown(next);
    storePersonValue(HIGHLIGHTS_SHOWN_KEY, next ? "shown" : "hidden");
  };
  return [shown, change] as const;
}

/**
 * The preview beside search results, as Notion's (seen 2026-10-31): a card
 * with buttons to copy the chosen result's link and open it, its place and
 * title, then the other lines of its page that hold the keywords — or, with
 * none (no search, or a match in the title alone), the page itself.
 */
export function SearchHighlightPanel({ hit, loading = false, onOpen, pagesById, emojiTitles, query, results = Boolean(query), session, workspaceId }: {
  hit: SearchHit | null;
  /** No result yet, while the search reads: the card's gray lines, as Notion's (SearchSkeleton). */
  loading?: boolean;
  onOpen: (hit: SearchHit) => void;
  pagesById: Record<string, Page>;
  /** Titles holding custom emoji, its place and title drawn with them (TitleWithCustomEmoji). */
  emojiTitles: ReturnType<typeof useCustomEmojiTitles>;
  query: string;
  /** Results are listed — of words, or of filters alone browsing — not the pages visited. */
  results?: boolean;
  session: Session;
  workspaceId: string;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation("searchDialog");
  const toast = useOptionalProductToast();
  const [read, setRead] = useState<{ key: string; highlights: string[] } | null>(null);
  const [body, setBody] = useState<{ pageId: string; blocks: PagePreviewBlock[] } | null>(null);
  // What this search read already, shown at once when the keys come back to a result.
  const bodies = useRef(new Map<string, PagePreviewBlock[]>());
  const highlightsKept = useRef(new Map<string, string[]>());
  const pageId = hit?.page.id ?? "";
  // The lines beyond the one the result's row shows (workspace_search_highlights.go); none for a page leading the
  // results, which shows itself, as Notion's (searchLead.ts).
  const leading = Boolean(hit?.lead);
  const line = hit && !leading ? searchPreviewLine(hit.preview, query) : "";
  const key = `${pageId}\u0000${query}\u0000${line}`;
  const keptLines = highlightsKept.current.get(key);
  const highlightsRead = !query || leading || read?.key === key || keptLines !== undefined;
  const lines = !hit || leading ? [] : read?.key === key ? read.highlights : keptLines ?? [];
  // With no line beyond the one in the result's row the page itself shows, as
  // Notion's (seen 2026-11-05); a database's own page shows no body (its views
  // aren't drawn).
  // A line that is a file block's name is the page's own line, as Notion's.
  const inFile = hit ? searchLineInFile(hit, line) : false;
  const showsBody = Boolean(hit) && !inFile && hit?.page.kind !== "database" && highlightsRead && lines.length === 0;
  const keptBody = showsBody ? bodies.current.get(pageId) : undefined;

  useEffect(() => {
    if (!pageId || !query || leading || highlightsKept.current.has(key)) return;
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      api.readWorkspaceSearchHighlights(session, workspaceId, pageId, query, { signal: controller.signal, line })
        .then((result) => {
          highlightsKept.current.set(key, result.highlights);
          setRead({ key, highlights: result.highlights });
        })
        .catch(() => undefined);
    }, HIGHLIGHTS_READ_DELAY_MS);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [key, line, pageId, query, session, leading, workspaceId]);

  useEffect(() => {
    if (!pageId || !showsBody || bodies.current.has(pageId)) return;
    const controller = new AbortController();
    // With words the keys rested on the result while its lines were read; without, they rest first.
    const timer = window.setTimeout(() => {
      api.readPagePreview(session, pageId, { signal: controller.signal, blocks: true })
        .then((result) => {
          const blocks = result.blocks ?? [];
          bodies.current.set(pageId, blocks);
          setBody({ pageId, blocks });
        })
        .catch(() => undefined);
    }, query ? 0 : HIGHLIGHTS_READ_DELAY_MS);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [pageId, query, session, showsBody]);

  // A full-page database's link names its view, as Notion's preview copies it
  // (seen 2026-11-14: /p/<it>?v=<its view>): its views are read while it
  // shows, so its "링크 복사" copies at once (databasePageLink.ts).
  const linkPage = hit?.page;
  useEffect(() => { if (linkPage) void readPageLinkToCopy(session, linkPage); }, [linkPage, session]);

  // Drawn again as the server names a found page's path (SearchResults asks).
  usePagePathsAbove(NO_PAGES);
  // Over results the card stands at the list's top, over the pages visited 34px down, as Notion's
  // (seen 2026-11-07; 2026-11-14 under a "검색 범위" and no words).
  const searching = results ? "" : undefined;
  if (!hit) {
    return (
      <aside className={styles.highlightPanel} data-searching={searching} aria-hidden="true">
        <div className={styles.highlightCard}>
          {loading ? <><div className={styles.highlightBand} /><SearchSkeletonCard /></> : null}
        </div>
      </aside>
    );
  }
  const location = hit.row ? databasePagePlace(hit.row.containerPageId, pagesById, hit.row, hit.page.id) : pagePlace(hit.page, pagesById);
  // Its lines mark the last word typed by the letters it starts a word with, unless its title holds it (searchTypedStart).
  const typedStart = searchTypedStart(query, hit.page.title, placeText(location));
  // A database, full-page or in a page's body, says so above its place, as Notion's preview does (seen 2026-11-14:
  // "w925 합성 표" in a page's body and "w925 전체 표"), its rows for each of its sources too, though they carry no
  // mark of their own (seen 2026-11-18 in two tabs: found by its name, named or not, full-page or inline); a wiki
  // when found by its lines or by filters alone, not by its name (seen 2026-11-11: "W388 Zelquor fulldb" and the
  // wiki "W388 Zelquor wiki" by "zelquor brindavo", not the wiki by "zelquor"; the wiki "위키 s214" under its own
  // "검색 범위" and no words). A database's template says "템플릿" there (seen 2026-11-14). Among the recent pages,
  // before any words or filters, a database's says nothing there, named or not, as Notion's (seen 2026-11-18 in two
  // tabs and a third: its row there has no mark either).
  const database = !hit.row && ((hit.page.kind === "database" && results)
    || Boolean(hit.page.isWiki && hit.page.wikiDataSourceId && ((results && !query) || searchPreviewLine(hit.preview, query))));
  const kind = database ? t("databaseBadge") : hit.row?.template ? t("templateBadge") : "";
  const untitledHead = !hit.page.title && (hit.row || (hit.page.kind === "database" && (!hit.database || hit.source)));
  return (
    <aside className={styles.highlightPanel} data-searching={searching} aria-label={t("highlights.label")} data-search-highlights>
      <div className={styles.highlightCard}>
        <div className={styles.highlightButtons}>
          {/* "링크 복사됨", as Notion's search preview says (seen 2026-11-11; "Link copied" in its English),
              not the page menu's "클립보드에 페이지 링크 복사됨"; a failed copy shows the link to copy by hand. */}
          <button type="button" aria-label={t("highlights.copyLink")} data-search-highlight-copy
            onClick={() => void readPageLinkToCopy(session, hit.page).then((link) => copyPageLink(link, hit.page.id, pageNames.title(hit.page)))
              .then((ok) => { if (ok) toast?.notify(t("highlights.copiedLink"), "success"); })}>
            <LinkIcon size="small" aria-hidden="true" />
          </button>
          <button type="button" aria-label={t("highlights.open")} data-search-highlight-open onClick={() => onOpen(hit)}>
            <OpenInNew size="small" aria-hidden="true" />
          </button>
        </div>
        <SearchPreviewBand page={hit.page} />
        <div className={styles.highlightBody}>
          {kind ? <div className={styles.highlightKind}><span data-search-highlight-database={database ? "" : undefined}
            data-search-highlight-template={database ? undefined : ""}>{kind}</span></div> : null}
          {/* The place and title as written, unmarked, as Notion's preview shows them. */}
          {location.length ? <PlaceWithCustomEmoji place={location} titles={emojiTitles} className={styles.highlightPath} /> : null}
          {/* An untitled row heads its preview "제목 없음", where its line reads its template's name (Notion's, seen 2026-11-08),
            as does an untitled full-page database, its line "새 데이터베이스" or its sources' names (seen 2026-11-18, one source and two),
            and a row of one of an untitled database's sources, its line "A, B, C / ⛁ C" (seen 2026-11-18 in two tabs). */}
          {untitledHead ? <div className={styles.highlightTitle} data-untitled="">{t("common:labels.untitled")}</div>
            : <div className={styles.highlightTitle}><PageTitleText page={hit.page} /></div>}
          {/* A line found in a document's words shows under its file. */}
          {inFile && hit.file ? (<>
            <div className={styles.highlightFile} data-search-highlight-file>
              <FileText size="small" aria-hidden="true" /><span>{hit.file.name}</span>
            </div>
            {searchPreviewLine(hit.preview, query) ? (
              <p className={styles.highlightLine}><HighlightedText text={searchPreviewLine(hit.preview, query)} query={query} starts={{ typedStart }} /></p>
            ) : null}
          </>) : null}
          {lines.map((line, index) => (
            <p key={`${index}:${line}`} className={styles.highlightLine}><HighlightedText text={line} query={query} starts={{ typedStart }} /></p>
          ))}
          {showsBody && (body?.pageId === pageId || keptBody) ? (
            <SearchPagePreview blocks={body?.pageId === pageId ? body.blocks : keptBody ?? []} workspaceId={workspaceId} />
          ) : null}
        </div>
      </div>
    </aside>
  );
}

/**
 * The band over the preview, as Notion's (seen 2026-11-02): the page's cover
 * where its head shows it (at the same height in it), or grey; its icon in a
 * 48px box 24px in, 28px up from the band's foot, over the title's room.
 */
function SearchPreviewBand({ page }: { page: Page }) {
  const cover = page.cover ?? "";
  const coverUrl = useWorkspaceFileUrl(cover || undefined, ["data:image/"], { enabled: Boolean(cover), width: filePreviewWidth(376) });
  const style = cover
    ? { backgroundImage: coverImage(cover, coverUrl || undefined), backgroundPosition: `center ${page.coverPosition ?? 50}%` }
    : undefined;
  return (
    <div className={styles.highlightBand} style={style} data-cover={cover ? "true" : undefined}>
      <CoverCredit cover={cover} />
      {hasExplicitPageIcon(page) ? (
        <span className={styles.highlightIcon} data-search-highlight-icon>
          <PageIconGlyph page={page} size={48} box={48} fallback="none" />
        </span>
      ) : null}
    </div>
  );
}
