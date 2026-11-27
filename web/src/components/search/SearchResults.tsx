import { FileText, Search } from "@/icons/leafen";
import { DataSourceMark } from "@/components/database/DataSourceMark";
import { dataSourceDisplayTitle, untitledDatabaseSourcesName } from "@/components/editor/databaseTitles";
import { SearchHitIcon } from "./SearchHitIcon";
import { teamspaceIdAt } from "@/components/page/TopBarTeamspaceCrumb";
import { PageTitleText, PlaceWithCustomEmoji, useCustomEmojiTitles } from "@/components/page/TitleWithCustomEmoji";
import { VerificationBadge } from "@/components/VerificationBadge";
import { databasePagePlace, pagePlace, placeText, type Place } from "@/lib/pagePath";
import { inlineDatabaseOpenedAlone } from "@/lib/inlineDatabaseFullPage";
import { pathLeavesOwnPages } from "@/lib/pagePathPieces";
import { usePagePathsAbove } from "@/lib/usePagePathsAbove";
import type { Page as RetainedPage } from "@/lib/types";
import { type HTMLAttributes, type ReactNode, type RefObject, useMemo } from "react";
import { useTranslation } from "react-i18next";
import type { Page } from "../../types";
import type { SessionUser } from "@/authWire.generated";
import type { WorkspaceSearchActor } from "@/workspaceSearchWire.generated";
import type { SearchHit, SearchSection } from "./searchModel";
import { searchMarkRuns, searchTitleTypedStart, type SearchMarkStarts } from "./searchMarks";
import { searchEditedTime, searchEditorName, searchLineInFile, searchLineQuery, searchPreviewLine } from "./searchResultDetails";
import { NativePickerOption } from "@/lib/nativeAutocomplete";
import { SearchSkeletonRows } from "./SearchSkeleton";
import { SearchArchivedMark } from "./SearchArchivedMark";
import { SearchRowEnter } from "./SearchRowEnter";
import { searchRowTitled } from "./searchRowTitle";
import type { ListState } from "react-stately";
import controls from "./SearchControls.module.css";
import styles from "./SearchDialog.module.css";

// How near the list's end, in pixels, its scrolling reads the next results.
const READ_ON_DISTANCE = 240;

export function searchHitKey(hit: SearchHit) {
  return `${hit.match}:${hit.page.id}${hit.source ? `:${hit.source.dataSourceId}` : ""}`;
}

/**
 * One of a database's several sources' rows, as Notion's search draws it
 * (seen 2026-11-11): the database's name — its own, else its sources' listed
 * ("A 및 B") — a gray "/", the gray source icon and the source's name, all
 * in 14px of the title's full color, no word marked and no "데이터베이스" after.
 */
function SearchSourceTitle({ hit, source }: { hit: SearchHit; source: NonNullable<SearchHit["source"]> }) {
  const sources = hit.database?.sources ?? [];
  return (
    <span className={styles.resultSourceTitle} data-search-database-source={source.dataSourceId}>
      <span className={styles.resultTitleText}>
        {hit.database?.named ? <PageTitleText page={hit.page} />
          : untitledDatabaseSourcesName(sources.map(({ title, linked }) => linked ? { title, linked } : title))}
      </span>
      <span className={styles.resultSourceSlash} aria-hidden="true">/</span>
      <DataSourceMark source={source} />
      <span className={styles.resultTitleText}>{dataSourceDisplayTitle(source.title, sources.length)}</span>
    </span>
  );
}

/** Each word of the query found on its own, as Notion marks "paste line one
    paste line two" for "paste line two" (seen 2026-11-07), a quoted phrase's
    words too (seen 2026-11-11), each a whole word as it marks them, and the
    whole query standing in its order as one (searchMarkRuns), and a word by its start where `starts` says. */
function keywordRanges(text: string, query: string, starts?: SearchMarkStarts) {
  const found = searchMarkRuns(text, query, starts)
    .sort((a, b) => a.start - b.start || b.end - a.end);
  const ranges: typeof found = [];
  for (const range of found) {
    const previous = ranges.at(-1);
    if (previous && range.start <= previous.end) previous.end = Math.max(previous.end, range.end);
    else ranges.push({ ...range });
  }
  return ranges;
}

/**
 * Text with the keywords marked, as Notion's search marks them: bold in a
 * title, blue elsewhere.
 */
export function HighlightedText({ text, query, strong = false, starts }: { text: string; query: string; strong?: boolean; starts?: SearchMarkStarts }) {
  const ranges = keywordRanges(text, query, starts);
  if (!ranges.length) return <>{text}</>;
  const parts: Array<{ text: string; match: boolean }> = [];
  let cursor = 0;
  for (const range of ranges) {
    if (range.start > cursor) parts.push({ text: text.slice(cursor, range.start), match: false });
    parts.push({ text: text.slice(range.start, range.end), match: true });
    cursor = range.end;
  }
  if (cursor < text.length) parts.push({ text: text.slice(cursor), match: false });
  return (
    <>
      {parts.map((part, index) => part.match ? (
        <mark key={`${part.text}-${index}`} className={strong ? styles.matchTitle : styles.match}>{part.text}</mark>
      ) : part.text)}
    </>
  );
}

/** A row of the results: a found page, a recent search, or a search for a new tab's "새 페이지" (SearchNewTab). */
export type SearchChoice = { key: string; label: string; hit?: SearchHit; recent?: string; create?: true };

type Props = {
  actors: ReadonlyMap<string, WorkspaceSearchActor>;
  currentPageId?: string;
  reader: SessionUser;
  state: ListState<SearchChoice>;
  listBoxProps: HTMLAttributes<HTMLElement>;
  query: string;
  /** The workspace's pages by id, read through as they are (withHitPages). */
  pagesById: Record<string, Page>;
  /** Titles holding custom emoji, by the text a path shows (useCustomEmojiTitles). */
  emojiTitles: ReturnType<typeof useCustomEmojiTitles>;
  /** The rows come the frame after the window (SearchDialog): nothing stands in for them meanwhile. */
  rowsComing?: boolean;
  sections: SearchSection[];
  hits: SearchHit[];
  /** Teamspace names by id, the place of a page directly in a teamspace. */
  teamspaceNames?: Record<string, string>;
  /** Archived teamspaces whose pages the reader still opens, each such page's title marked. */
  archivedTeamspaceIds?: ReadonlySet<string>;
  recentSearches: string[];
  /** Results of words or filters, not the recent pages, whose databases say nothing after their name (seen 2026-11-18). */
  resultsShown?: boolean;
  searching: boolean;
  /** Results past those read, and whether they are being read. */
  continuation: { loading: boolean } | null;
  resultsRef: RefObject<HTMLDivElement | null>;
  /** Beside the first group's name, as the results' sort. */
  sectionAction?: ReactNode;
  /** Reads the next results, as the list nears its end. */
  onLoadMore: () => void;
  /** Searches the trash for what found nothing, as Notion's "삭제된 페이지 검색". */
  onSearchTrash?: (query: string) => void;
  /** Rows over the results: a search for a new tab's "새 페이지". */
  before?: ReactNode;
  /** A phone draws each row as a card (SearchDialog.module.css). */
  phone?: boolean;
};

export function SearchResults({
  actors,
  currentPageId,
  reader,
  state,
  listBoxProps,
  query,
  pagesById: workspacePagesById,
  emojiTitles,
  rowsComing = false,
  sections,
  hits,
  teamspaceNames = {},
  archivedTeamspaceIds,
  recentSearches,
  resultsShown = true,
  searching,
  continuation,
  resultsRef,
  sectionAction,
  onLoadMore,
  onSearchTrash,
  before,
  phone = false,
}: Props) {
  const { t, i18n } = useTranslation(["searchDialog", "common"]);
  const now = Date.now();
  const trimmedQuery = query.trim();
  const pagesById = useMemo(() => withHitPages(workspacePagesById, hits), [hits, workspacePagesById]);
  // A database opened alone is the page on screen, not the page holding it, as Notion marks it (seen 2026-11-10).
  const shownPageId = inlineDatabaseOpenedAlone() ?? currentPageId;
  // A database's page, and a page under one, is placed by the path the
  // server names, as Notion's results (seen 2026-11-10: "〈test page〉 /
  // ... / 첫 소스", "… / ... / 행 나").
  usePagePathsAbove(hits.flatMap((hit) => hit.row || pathLeavesOwnPages(hit.page, (id) => pagesById[id]) ? [hit.page] : []));

  function verificationPresentationPage(page: Page): RetainedPage {
    return {
      id: page.id,
      workspaceId: page.workspaceId,
      parentId: page.parentId,
      parentType: page.parentId ? "page" : "workspace",
      kind: "page",
      title: page.title,
      icon: page.icon,
      iconType: page.iconType === "emoji" || page.iconType === "image" || page.iconType === "custom_emoji" || page.iconType === "icon"
        ? page.iconType
        : "none",
      verifiedAt: page.verifiedAt,
      verifiedBy: page.verifiedBy,
      verificationExpiresAt: page.verificationExpiresAt,
      position: page.position,
    };
  }

  // A phone's row is a card inside the row, the chosen row's ground around it, as Notion's;
  // a wider screen's row keeps the room of an Enter sign at its end (SearchRowEnter).
  const card = (content: ReactNode) => phone ? <span className={styles.resultCard}>{content}</span> : <>{content}<SearchRowEnter /></>;

  // As Notion's list, rows past those read come as it nears its end.
  const readOnNearEnd = (list: HTMLElement) => {
    if (continuation && !continuation.loading && list.scrollHeight - list.scrollTop - list.clientHeight < READ_ON_DISTANCE) onLoadMore();
  };

  return (
    <div className={styles.listbox}>
      <div {...listBoxProps} className={styles.results} ref={resultsRef}
        onScroll={(event) => { listBoxProps.onScroll?.(event); readOnNearEnd(event.currentTarget); }}>
        {before}
        {rowsComing ? null : <>
        {/* The section holding the sort stays as its head counts the results again: its open menu stays with it. */}
        {sections.map((section, sectionIndex) => (
          <section key={sectionIndex === 0 && sectionAction ? "sorted" : section.label} className={styles.section}>
            <div className={styles.groupLabel}>
              {section.label}
              {section.detail && <span>{section.detail}</span>}
              {sectionIndex === 0 ? sectionAction : null}
            </div>
            {section.hits.map((hit) => {
              const found: Place = hit.row ? databasePagePlace(hit.row.containerPageId, pagesById, hit.row, hit.page.id) : pagePlace(hit.page, pagesById);
              const teamspace = hit.page.teamspaceId ? teamspaceNames[hit.page.teamspaceId] : undefined;
              const place: Place = found.length ? found : teamspace ? [teamspace] : [];
              if (section.compact) {
                // One line: the icon, the title and "— its place", as Notion's visited pages; the "—" stands
                // between the two on its own, so they give way by their own widths alone (SearchDialog.module.css).
                return (
                  <NativePickerOption state={state} itemKey={searchHitKey(hit)} key={searchHitKey(hit)}
                    className={`${styles.result} ${styles.resultCompact}`} data-kind="visit">
                    {({ labelProps }) => card(<>
                      <span className={styles.pageIcon}><SearchHitIcon page={hit.page} row={hit.row} /></span>
                      <span className={styles.compactLine}>
                        <span {...labelProps} className={styles.compactTitle}><PageTitleText page={hit.page} /></span>
                        {place.length ? <>
                          <span className={styles.compactDash} aria-hidden="true">—</span>
                          <PlaceWithCustomEmoji place={place} titles={emojiTitles} className={styles.compactPlace} />
                        </> : null}
                      </span>
                    </>)}
                  </NativePickerOption>
                );
              }
              // As Notion's rows: the place, who last edited the page and
              // when, then the line of its body holding the keywords.
              const editor = searchEditorName(hit.page, actors, reader);
              const edited = searchEditedTime(hit.page.updatedAt, now, (key, options) => String(t(key, options)), i18n.language);
              // A page leading the results shows no line, its title marking the words it answered by (searchLead.ts);
              // another's marks the letters the last of several words typed starts a word with (searchTitleTypedStart).
              const titleMarks: SearchMarkStarts = hit.lead ? { lead: true } : { typedStart: searchTitleTypedStart(query) };
              const line = hit.lead ? "" : searchPreviewLine(hit.preview, query);
              const lineMarks = line ? searchLineQuery(query, line, hit.page.title, placeText(place)) : null;
              // A page found by a file block's name shows the name as its line,
              // as Notion's does (seen 2026-11-14); the file is named apart only
              // for words found inside it.
              const inFile = searchLineInFile(hit, line);
              return (
                <NativePickerOption state={state} itemKey={searchHitKey(hit)} key={searchHitKey(hit)}
                  className={styles.result} data-kind={hit.match}>
                  {({ labelProps, descriptionProps }) => card(<>
                  <span className={styles.pageIcon}><SearchHitIcon page={hit.page} row={hit.row} /></span>
                  <span className={styles.resultText}>
                    <span {...labelProps} className={styles.resultTitle}>
                      {hit.source ? <SearchSourceTitle hit={hit} source={hit.source} /> : <span className={styles.resultTitleText}>
                        <PageTitleText page={searchRowTitled(hit.page)} drawText={(text) => <HighlightedText text={text} query={query} strong starts={titleMarks} />} /></span>}
                      {archivedTeamspaceIds?.size && archivedTeamspaceIds.has(teamspaceIdAt(hit.page, (id) => pagesById[id]))
                        ? <SearchArchivedMark /> : null}
                      {/* A favorite has no mark of its own here, as Notion's rows (seen 2026-11-11, 2026-11-14). */}
                      <VerificationBadge page={verificationPresentationPage(hit.page)} />
                      {/* A database, full-page or in a page's body — a wiki among them — and a database
                          template say so after their name, as Notion's results do (seen 2026-11-07,
                          2026-11-10, 2026-11-11). */}
                      {resultsShown && (hit.page.kind === "database" || (hit.page.isWiki && hit.page.wikiDataSourceId)) && !hit.row && !hit.source && (
                        <span className={styles.resultBadge} data-search-database>{t("searchDialog:databaseBadge")}</span>
                      )}
                      {hit.row?.template && (
                        <span className={styles.resultBadge} data-search-template>{t("searchDialog:templateBadge")}</span>
                      )}
                      {hit.page.id === shownPageId && (
                        <span className={styles.resultBadge} data-search-current-page>{t("searchDialog:currentPage")}</span>
                      )}
                    </span>
                    <span {...descriptionProps} className={styles.resultMeta}>
                      {place.length ? (
                        <PlaceWithCustomEmoji place={place} titles={emojiTitles} className={styles.resultPath}
                          drawText={(text) => <HighlightedText text={text} query={query} />} />
                      ) : null}
                      {editor ? <span className={styles.resultEditor} data-search-editor>{editor}</span> : null}
                      {edited ? <span className={styles.resultTime}>{edited}</span> : null}
                    </span>
                    {/* A line found in a document's words names its file (2026-10-16). */}
                    {inFile && hit.file ? (
                      <span className={styles.resultFile} data-search-file>
                        <FileText size="small" aria-hidden="true" /><span>{hit.file.name}</span>
                      </span>
                    ) : null}
                    {line && lineMarks ? (
                      <span className={styles.resultPreview}>
                        <HighlightedText text={line} query={lineMarks.query} starts={{ typedStart: lineMarks.typedStart }} />
                      </span>
                    ) : null}
                    {/* A page whose documents search reads only in part says so. */}
                    {hit.partial ? <span className={styles.resultPartial} data-search-partial>{t("searchDialog:partial")}</span> : null}
                  </span>
                  </>)}
                </NativePickerOption>
              );
            })}
          </section>
        ))}

        {recentSearches.length > 0 && (
          <section className={styles.section}>
            <div className={styles.groupLabel}>{t("searchDialog:sections.recentSearches")}</div>
            {recentSearches.map(recentSearch => {
              return (
                <NativePickerOption state={state} itemKey={`query:${recentSearch}`} key={recentSearch}
                  className={styles.result} data-kind="search">{({ labelProps, descriptionProps }) => card(<>
                  <span className={styles.pageIcon}><Search size="regular" aria-hidden="true" /></span>
                  <span className={styles.resultText}>
                    <span {...labelProps} className={styles.resultTitle}><span className={styles.resultTitleText}>{recentSearch}</span></span>
                    <span {...descriptionProps} className={styles.resultMeta}><span className={styles.resultPath}>{t("searchDialog:searchAgain")}</span></span>
                  </span>
                  </>)}
                </NativePickerOption>
              );
            })}
          </section>
        )}

        {hits.length === 0 && recentSearches.length === 0 && !continuation && !before && (
          // While a search reads with nothing to hold, its gray rows, as Notion's.
          searching ? <SearchSkeletonRows label={t("searchDialog:searchingPageContent")} /> : !trimmedQuery ? (
            <div className={styles.empty} role="status">{t("searchDialog:noPagesFound")}</div>
          ) : (
            // As Notion's: nothing found, and the deleted pages to look in.
            <div className={controls.noResults} role="status" data-search-no-results>
              <strong>{t("searchDialog:noResults.title")}</strong>
              <span>{t("searchDialog:noResults.inTrash")}</span>
              {onSearchTrash ? (
                <button type="button" className={controls.trashLink} data-search-trash onClick={() => onSearchTrash(trimmedQuery)}>
                  {t("searchDialog:noResults.searchTrash")}
                </button>
              ) : null}
            </div>
          )
        )}
        </>}
      </div>
    </div>
  );
}

/**
 * The pages by id, the results' own over the workspace's (a server's answer
 * brings its own copies): the workspace's are read through, not copied —
 * copying 44,000 of them took 70 ms of the press that opened search on a
 * device half as fast (2026-11-14). Read by id only.
 */
function withHitPages(pagesById: Record<string, Page>, hits: readonly SearchHit[]): Record<string, Page> {
  if (hits.every((hit) => pagesById[hit.page.id] === hit.page)) return pagesById;
  const own = Object.create(pagesById) as Record<string, Page>;
  for (const hit of hits) own[hit.page.id] = hit.page;
  return own;
}
