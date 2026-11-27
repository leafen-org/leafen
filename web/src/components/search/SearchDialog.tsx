import { FilterCircleIcon, HighlightPaneIcon, Search } from "@/icons/leafen";
import { usePagesWithEditTimes } from "@/lib/pageEditedHere";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { useCallback, useDeferredValue, useEffect, useId, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  archivedPageIds,
  searchContentStateMatches,
  type WorkspaceSearchContentState,
} from "@/lib/pageArchive";
import { isPageVerified } from "@/lib/pageVerification";
import { pageHref } from "@/lib/navigation"; import { openSearchedPage, openUnlistedItem } from "./searchPeopleHits";
import { useKeptTeamspaces } from "@/lib/workspaceTeamspaces";
import { pushRoute, replaceRoute } from "@/lib/router";
import { openPressedPastPaint } from "@/lib/openPastPaint";
import { inlineDatabaseFullPageHref } from "@/lib/inlineDatabaseFullPage";
import { templateAddress } from "@/components/database/useTemplateAddress";
import { useOverlayLayer } from "@/lib/overlayLayer";
import type { Page, Session, Workspace } from "../../types";
import type { WorkspaceSearchActor, WorkspaceSearchResult } from "../../workspaceSearchWire.generated";
import { heldSearchResults, keepSearchAnswer, keptSearchAnswer, searchAnswerKey, type SearchAnswer } from "./searchAnswers";
import {
  continueWorkspaceSearch, findWorkspaceSearchBlock, recordWorkspaceSearch, searchWorkspaceAfterProjection,
} from "../../lib/workspaceSearchService";
import { useReadGeneration } from "../../lib/useReadGeneration";
import { SearchFilterBar } from "./SearchFilterBar";
import phoneStyles from "./SearchDialogPhone.module.css";
import { SearchFooter } from "./SearchFooter";
import { useSearchDismissFocus } from "./searchDismissFocus";
import { useSearchClosedForLink } from "./searchClosedForLink";
import { useSearchKeptFilters } from "./searchKeptFilters";
import { SearchSortMenu } from "./SearchSortMenu";
import {
  browseSearchHits, searchFilterParams, searchFiltersActive, searchFiltersMatchPage, sortSearchHits,
  type SearchFilters,
} from "./searchFilterModel";
import { SearchHighlightPanel, useSearchHighlightsShown } from "./SearchHighlightPanel";
import { SearchResults, searchHitKey, type SearchChoice } from "./SearchResults";
import { searchResultRows } from "./searchDatabaseRows";
import { openAtSearchBlock, opensAtBlock, SEARCH_PAGE_TOP, searchBlockAnchor, type SearchBlockRead } from "./searchResultBlock";
import {
  localTitleHits,
  mergeSearchHits,
  normalizedSearchQuery,
  readRecentSearches,
  REMOTE_SEARCH_DEBOUNCE_MS,
  REMOTE_SEARCH_MIN_LENGTH,
  rememberRecentSearch,
  SEARCH_COUNT_LIMIT,
  SEARCH_MORE_LIMIT,
  SEARCH_RESULT_LIMIT,
  type SearchHit,
  type SearchSection,
} from "./searchModel";
import { searchResultsCount, searchVisitSections, useSearchLead, useSearchVisits, withSearchLead } from "./searchVisits";
import styles from "./SearchDialog.module.css";
import { useSidebarTeamspaces } from "../sidebar/useSidebarTeamspaces";
import { useNativeAutocomplete } from "@/lib/nativeAutocomplete";
import { useCustomEmojiTitles } from "@/components/page/TitleWithCustomEmoji";
import { usePageNames } from "@/lib/pageNames";
import { SEARCH_REQUEST, type SearchRequest } from "@/lib/searchRequest";
import {
  inDesktopTab, openNewPageInNewDesktopTab, pickedForNewTab, setSearchingForNewTab, useSearchingForNewTab,
} from "@/lib/desktopTabs";
import { matchesKeyboardShortcut } from "@/lib/keyboardShortcuts";
import { readPageLinkToCopy } from "@/lib/databasePageLink";
import { copyPageLink } from "@/components/editor/pageLinkClipboard";
import { useOptionalProductToast } from "@/lib/productToast";
import { NEW_PAGE_CHOICE, SearchNewPageCard, SearchNewPageRow } from "./SearchNewTab";
import { editCommandSearchShortcut, hideQuickSearch, useQuickSearchWindow } from "@/lib/quickSearchWindow";
import { outOfTrashIn, pagesByIdOf } from "@/lib/pageIndex";
import { fieldEnter } from "@/lib/fieldKeyBoundary";
import { focusOpenedField } from "@/components/editor/openedFieldCaret";

// How long the keys rest on a result before its block is asked, as its highlights are (SearchHighlightPanel).
const BLOCK_READ_DELAY_MS = 120;
const NO_PAGES: Page[] = [];

type Props = {
  open: boolean;
  /** The page open behind the dialog, which its result names. */
  currentPageId?: string;
  session: Session;
  workspace: Workspace;
  pages: Page[];
  recentPageIds: string[];
  favoritePageIds: string[];
  localBodyResults: (query: string) => WorkspaceSearchResult[];
  onClose: () => void;
  onOpenPage: (page: Page, newTab: boolean) => void;
  /** Opens the trash searching for what the search found nothing for. */
  onSearchTrash?: (query: string) => void;
  /** Makes a page without opening it, for a search for a new tab's "새 페이지"; none for those who may not. */
  onCreatePage?: () => Promise<Page | null>;
};

/**
 * Search (⌘K), as Notion's (seen 2026-09-30): the search over the filters,
 * which can be hidden, the results with their sort and the highlight panel,
 * and the foot with "새 탭에서 열기" and "검색 설정".
 */
export function SearchDialog({
  open,
  currentPageId,
  session,
  workspace,
  pages: listedPages,
  recentPageIds,
  favoritePageIds,
  localBodyResults,
  onClose,
  onOpenPage,
  onSearchTrash,
  onCreatePage,
}: Props) {
  const { t } = useTranslation(["searchDialog", "common"]), pageNames = usePageNames();
  // Each page's edit time as last known, while open (lib/pageEditedHere.ts).
  const pages = usePagesWithEditTimes(listedPages, open);
  const dialogRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const resultsRef = useRef<HTMLDivElement>(null);
  const newTabIntent = useRef(false);
  // The Mac and Windows apps' ⌘T and strip "+" (SearchNewTab.tsx).
  const forNewTab = useSearchingForNewTab();
  const toast = useOptionalProductToast();
  const closeRef = useRef<() => void>(() => undefined);
  // A modal layer: Escape closes it once nothing opened over it (a filter's
  // menu) takes the key, as its key again and a press beside it do (searchDismissFocus.ts).
  const { zIndex } = useOverlayLayer(open, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [dialogRef.current?.parentElement], restoreFocus: true, onEscape: () => dismiss(),
    initialFocus: inputRef, typesOnPhone: true,
  });
  const { dismiss, dismissKey } = useSearchDismissFocus(open, closeRef);
  const dialogId = useId();
  const [query, setQuery] = useState("");
  // The server's last answer, for these words and filters or earlier ones (searchAnswers.ts).
  const [answer, setAnswer] = useState<SearchAnswer | null>(null);
  // The search whose read last ended, answered or not.
  const [readKey, setReadKey] = useState<string | null>(null);
  // Who last edited the results' pages, by the names search gave.
  const [actors, setActors] = useState<ReadonlyMap<string, WorkspaceSearchActor>>(() => new Map());
  const learnActors = useCallback((found: WorkspaceSearchActor[] = []) => {
    if (found.length) setActors((current) => new Map([...current, ...found.map((actor) => [actor.userId, actor] as const)]));
  }, []);
  const [highlightsShown, setHighlightsShown] = useSearchHighlightsShown();
  const [loadingMore, setLoadingMore] = useState(false);
  const [recentSearches, setRecentSearches] = useState<string[]>(readRecentSearches);
  const kept = useSearchKeptFilters();
  const [contentState, setContentState] = useState<WorkspaceSearchContentState>(kept.initial.contentState);
  const [filters, setFilters] = useState<SearchFilters>(kept.initial.filters);
  // A phone's search has no filters to show, as Notion's in iPhone Safari (seen 2026-11-14); a search within pages still shows its scope.
  const { phone } = useSidebarLayout(true);
  const [filtersShown, setFiltersShown] = useState(() => !phone);
  // Opened to search within some pages (searchRequest.ts): those pages are
  // its "검색 범위".
  useEffect(() => {
    const onRequest = (event: Event) => {
      const { inPages } = (event as CustomEvent<SearchRequest>).detail ?? { inPages: [] };
      if (inPages.length) setFilters((current) => ({ ...current, inPages }));
    };
    window.addEventListener(SEARCH_REQUEST, onRequest);
    return () => window.removeEventListener(SEARCH_REQUEST, onRequest);
  }, []);
  const filterKey = JSON.stringify(filters);
  // The time a date filter counts from is read when the filters change.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const filterParams = useMemo(() => searchFilterParams(filters, new Date()), [filterKey]);

  const trimmedQuery = normalizedSearchQuery(query);
  // The press draws the window — its field, filters and foot — on its own
  // frame, and its rows, which go through every page of the workspace, the
  // frame after: drawn together they took 200 ms of the press on a
  // 44,000-page workspace with a device half as fast (2026-11-14), as the
  // settings window draws its frame and then its section. Shut, it goes
  // through no page: it did on every change to the pages while shut.
  const listShown = useDeferredValue(open, false) && open;
  const listPages = listShown ? pages : NO_PAGES;
  const beginSearchRead = useReadGeneration({
    principalId: session.user.id,
    sessionToken: session.token,
    route: `quick-find:${open ? "open" : "closed"}:${workspace.id}:${contentState}:${filterKey}:${trimmedQuery}`,
  });
  const archivedIds = useMemo(() => archivedPageIds(listPages), [listPages]);
  const pagesById = pagesByIdOf(listPages);
  // Paths and titles draw their custom emoji, in the rows and the preview (TitleWithCustomEmoji).
  const emojiTitles = useCustomEmojiTitles(listPages);
  const filtersActive = searchFiltersActive(filters);
  // Out of the trash, and from under any page in it (lib/pageIndex.ts), as the server's search finds.
  const outOfTrash = useMemo(() => outOfTrashIn(listPages), [listPages]);
  const searchable = useCallback(
    (page: Page) => outOfTrash(page) && searchContentStateMatches(contentState, archivedIds.has(page.id), isPageVerified(page)) &&
      (!filtersActive || searchFiltersMatchPage(page, filters, pagesById, new Date())),
    // `filters` follows `filterKey`.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [archivedIds, contentState, filterKey, filtersActive, outOfTrash, pagesById],
  );
  const searchablePages = useMemo(() => listPages.filter(searchable), [listPages, searchable]);
  const recentOrder = useMemo(
    () => new Map(recentPageIds.map((id, index) => [id, index])),
    [recentPageIds],
  );
  const favoriteIds = useMemo(() => new Set(favoritePageIds), [favoritePageIds]);
  const localHits = useMemo(
    () => localTitleHits(searchablePages, workspace.id, trimmedQuery, recentOrder, favoriteIds),
    [favoriteIds, recentOrder, searchablePages, trimmedQuery, workspace.id],
  );
  const localBodyHits = useMemo<SearchHit[]>(
    // Only pages the reader still has: an open page's text stays in memory
    // after its share is taken away, and kept matching (2026-11-05).
    () => filters.titleOnly || !listShown ? [] : localBodyResults(trimmedQuery)
      .filter((hit) => Boolean(pagesById[hit.page.id]) && searchable(hit.page))
      .map((hit, index) => ({ ...hit, score: 3 + index / 100 })),
    [listShown, localBodyResults, pagesById, searchable, filters.titleOnly, trimmedQuery],
  );
  // Filters or archived content without keywords browse the workspace.
  const browsing = !trimmedQuery && (filtersActive || contentState !== "active");
  // Browsing, or with words enough, the server searches too (searchAnswers.ts):
  // its answer for this search, else the one this tab read for it before, else
  // what its last answer still holds for these words and filters, stands until
  // its answer comes.
  const readsServer = open && (browsing || trimmedQuery.length >= REMOTE_SEARCH_MIN_LENGTH);
  const answerKey = searchAnswerKey(contentState, filterKey, trimmedQuery);
  const answerScope = `${session.user.id}\u0000${workspace.id}`;
  const freshAnswer = readsServer && answer?.key === answerKey ? answer : null;
  const keptAnswer = readsServer && !freshAnswer ? keptSearchAnswer(answerScope, answerKey) : null;
  const remoteResults = useMemo(() => {
    if (!readsServer) return [];
    if (freshAnswer) return freshAnswer.results;
    const from = keptAnswer ?? answer;
    if (!from) return [];
    return heldSearchResults(from.results, { query: trimmedQuery, filters, contentState, pagesById, archivedIds }, from === keptAnswer);
    // `filters` follows `filterKey`.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [answer, archivedIds, contentState, filterKey, freshAnswer, keptAnswer, pagesById, readsServer, trimmedQuery]);
  // "더 검색" goes on from the server's answer to this search alone.
  const nextCursor = freshAnswer?.nextCursor ?? null;
  // Some of the workspace's pages still wait for the index (Leafen's own).
  const indexBacklog = (freshAnswer ?? keptAnswer)?.backlog ?? false;
  const searching = readsServer && readKey !== answerKey;
  // The pages the reader visited (searchVisits.ts): listed before a search, and up to three of them leading one.
  const visited = useSearchVisits({ session, workspaceId: workspace.id, open, listShown, pagesById, searchable, outOfTrash });
  const lead = useSearchLead(visited, trimmedQuery, { on: listShown && !browsing && filters.sort === "best", pages: listPages,
    rows: !filtersActive && contentState === "active" });
  const hits = useMemo(
    () => withSearchLead(lead, searchResultRows(browsing ? browseSearchHits(localHits, remoteResults, filters.sort)
      : sortSearchHits(trimmedQuery ? mergeSearchHits([...localHits, ...localBodyHits], remoteResults) : localHits, filters.sort), trimmedQuery)),
    [browsing, filters.sort, lead, localBodyHits, localHits, remoteResults, trimmedQuery],
  );
  // A top-level teamspace page's place is its teamspace, as Notion's rows name it.
  // An archived teamspace whose pages the reader still opens names them too, and marks them (SearchArchivedMark).
  const teamspaces = useSidebarTeamspaces({ session, workspace }).joinedTeamspaces;
  const archivedPlaces = useKeptTeamspaces(session.user.id, workspace.id)?.archived;
  const teamspaceNames = useMemo(() => Object.fromEntries([...(archivedPlaces ?? []), ...teamspaces]
    .map((teamspace) => [teamspace.id, teamspace.name])), [archivedPlaces, teamspaces]);
  const archivedTeamspaceIds = useMemo(() => new Set((archivedPlaces ?? []).map((place) => place.id)), [archivedPlaces]);
  const visitSections = useMemo<SearchSection[] | null>(
    () => !listShown || browsing || trimmedQuery ? null : searchVisitSections(visited, currentPageId, (key) => t(`searchDialog:visits.${key}`)),
    [browsing, currentPageId, listShown, t, trimmedQuery, visited]);
  // The head counts the results once they are all read, or past 100, as
  // Notion's "검색 결과(31개)" over its first rows and "검색 결과(100개
  // 이상)"; until then it reads "검색 결과", as Notion's while it searches
  // (seen 2026-11-14).
  const count = searchResultsCount(hits);
  const countedPast = count > SEARCH_COUNT_LIMIT || (count === SEARCH_COUNT_LIMIT && Boolean(nextCursor));
  const counted = !searching && (!nextCursor || countedPast);
  const sections = useMemo<SearchSection[]>(() => {
    if (!listShown) return [];
    if (visitSections) return visitSections;
    if (browsing || trimmedQuery) {
      // Results are one list, as Notion's search shows them.
      return hits.length || searching || indexBacklog ? [{
        // Browsing by filters alone counts its results as a search does, as
        // Notion's "검색 결과(11개)" under a "검색 범위" and no words (seen
        // 2026-11-14). A phone, with no sort to choose, names the order
        // instead, as Notion's "결과 상위 일치" in iPhone Safari (seen 2026-11-14).
        label: phone ? t(`searchDialog:sort.${filters.sort}`) : !counted ? t("searchDialog:resultsHead")
          : countedPast ? t("searchDialog:resultsCountMore", { count: SEARCH_COUNT_LIMIT })
            : t("searchDialog:resultsCount", { count }),
        // Results held meanwhile say they are searched again; gray rows, as Notion's, say nothing.
        detail: searching ? (hits.length ? t("searchDialog:searchingDetail") : undefined)
          : indexBacklog ? t("searchDialog:indexBacklog") : undefined,
        hits,
      }] : [];
    }
    const recentHits = [...hits]
      .filter((hit) => recentOrder.has(hit.page.id))
      .sort((left, right) => (recentOrder.get(left.page.id) ?? Infinity) - (recentOrder.get(right.page.id) ?? Infinity));
    const recentIds = new Set(recentHits.map((hit) => hit.page.id));
    const favoriteHits = hits.filter((hit) => favoriteIds.has(hit.page.id) && !recentIds.has(hit.page.id));
    const visibleFavoriteIds = new Set(favoriteHits.map((hit) => hit.page.id));
    const otherHits = hits.filter((hit) => !recentIds.has(hit.page.id) && !visibleFavoriteIds.has(hit.page.id));
    return [
      ...(recentHits.length ? [{ label: t("searchDialog:sections.recentlyViewed"), hits: recentHits }] : []),
      ...(favoriteHits.length ? [{ label: t("searchDialog:sections.favorites"), hits: favoriteHits }] : []),
      ...(otherHits.length ? [{ label: t("searchDialog:sections.pages"), hits: otherHits }] : []),
    ];
  }, [browsing, count, counted, countedPast, favoriteIds, filters.sort, hits, indexBacklog, listShown, phone, recentOrder, searching, t, trimmedQuery, visitSections]);
  // The rows in the order shown, which the keys move through.
  const listedHits = useMemo(() => sections.flatMap((section) => section.hits), [sections]);

  // Earlier searches stand under the visited pages only, not under results a filter browses (Notion's, seen 2026-11-14).
  const visibleRecentSearches = trimmedQuery || browsing || !listShown ? [] : recentSearches;
  const titleId = `${dialogId}-title`;

  // A finished search tells the workspace its query and whether it opened
  // a result, for the owner's search analytics, which count its members'.
  const openedResult = useRef(false);
  const reportSearch = useCallback((opened: boolean) => {
    const value = normalizedSearchQuery(inputRef.current?.value ?? "");
    if (value.length < REMOTE_SEARCH_MIN_LENGTH || workspace.guest) return;
    void recordWorkspaceSearch(session, workspace.id, { query: value, clicked: opened }).catch(() => undefined);
  }, [session, workspace.guest, workspace.id]);

  const close = useSearchClosedForLink(useCallback(() => {
    if (!openedResult.current) {
      reportSearch(false);
      // The command search window goes away with its search left (Esc).
      hideQuickSearch();
    }
    openedResult.current = false;
    setSearchingForNewTab(false);
    onClose();
    setQuery("");
    setAnswer(null);
    setReadKey(null);
    // As Notion's, the filters start over each search unless kept.
    const next = kept.onClose({ filters, contentState });
    setFilters(next.filters);
    setContentState(next.contentState);
  }, [contentState, filters, kept, onClose, reportSearch]));

  const rememberSearch = useCallback((value: string) => {
    setRecentSearches((current) => rememberRecentSearch(current, value));
  }, []);

  // The block a result found by its body opens at (workspace_search_block.go),
  // asked once it is chosen or pressed, so that opening it waits for no answer.
  const blocks = useRef(new Map<string, SearchBlockRead>());
  const readBlock = useCallback((hit: SearchHit) => {
    const key = `${hit.page.id}\u0000${trimmedQuery}\u0000${hit.preview}`;
    const kept = blocks.current.get(key);
    if (kept) return kept;
    const entry: SearchBlockRead = { reading: Promise.resolve("") };
    entry.reading = findWorkspaceSearchBlock(session, hit.page.id, trimmedQuery, hit.preview).then((blockId) => {
      entry.found = blockId;
      return blockId;
    }, () => {
      // Asked again when opened or chosen again.
      blocks.current.delete(key);
      return "";
    });
    blocks.current.set(key, entry);
    return entry;
  }, [session, trimmedQuery]);

  const openHit = useCallback((hit: SearchHit, newTab: boolean) => {
    rememberSearch(inputRef.current?.value ?? query);
    // From a large page the window stays for the press's frame and closes as
    // the page changes over, one step past it (lib/openPastPaint.ts): closing
    // it over the large page alone took 190 ms of that frame (2026-11-13).
    const leaving = (go: () => void, toPageId: string) => newTab ? go() : openPressedPastPaint(go, toPageId);
    const opened = () => {
      reportSearch(true);
      openedResult.current = true;
    };
    // A database held in a page's body opens alone as its own page, as Notion's does (seen 2026-11-10).
    if (hit.database) {
      opened();
      // One of several sources' rows opens that source's view (searchResultRows).
      const href = inlineDatabaseFullPageHref(hit.database.pageId, hit.database.containerId, hit.source?.viewId);
      if (newTab) window.open(href, "_blank", "noopener,noreferrer");
      leaving(() => {
        if (!newTab) pushRoute(href);
        close();
      }, hit.database.pageId);
      return;
    }
    if (hit.row) {
      // A database item opens full as its page, as Notion's opens it at its
      // own address with its database in its bar (seen 2026-11-11), from the
      // page that holds its database; a template whole at its own address (useTemplateAddress);
      // one found by its body at its line's block, chosen (opensAtBlock).
      const container = pages.find((page) => page.id === hit.row?.containerPageId);
      if (!container) { if (openUnlistedItem(hit, newTab, close)) opened(); return; }
      opened();
      const href = hit.row.template ? templateAddress(hit.row.rowId)
        : pageHref(container.id, new URLSearchParams({ p: hit.row.rowId, pm: "f" }));
      const openRow = (blockId: string) => {
        const at = href + searchBlockAnchor(hit.row!.rowId, blockId);
        // Searched from the page holding it, the item opens as a step of its
        // own, which "뒤로" leaves for that page, as Notion's (seen 2026-11-14).
        if (newTab) window.open(at, "_blank", "noopener,noreferrer");
        else if (hit.row!.template || window.location.pathname === pageHref(container.id)) pushRoute(at);
        else {
          onOpenPage(container, false);
          replaceRoute(at);
        }
      };
      const block = opensAtBlock(hit) ? readBlock(hit) : SEARCH_PAGE_TOP;
      // A new tab opens at the press when its block is known.
      const known = block.found !== undefined;
      if (newTab && known) openRow(block.found ?? "");
      leaving(() => {
        close();
        if (!newTab || !known) openAtSearchBlock(block, newTab, openRow);
      }, container.id);
      return;
    }
    opened();
    // A line found in a file block's document opens the page at that file;
    // a page found by its body, at its line's block, chosen, as Notion's
    // does (seen 2026-11-11); one found by its title, or whose line's block
    // isn't found, at its top.
    const openAt = (blockId: string) => {
      if (!blockId) {
        onOpenPage(hit.page, newTab);
        return;
      }
      const href = pageHref(hit.page.id) + searchBlockAnchor(hit.page.id, blockId);
      if (newTab) {
        window.open(href, "_blank", "noopener,noreferrer");
      } else {
        onOpenPage(hit.page, false);
        replaceRoute(href);
      }
    };
    leaving(() => {
      close();
      if (hit.file?.blockId) {
        openAt(hit.file.blockId);
      } else if (opensAtBlock(hit)) {
        // The line's block, asked for this result alone while it was chosen or
        // pressed (openAtSearchBlock).
        openAtSearchBlock(readBlock(hit), newTab, openAt);
      } else {
        openSearchedPage(hit.page, newTab, onOpenPage);
      }
    }, hit.page.id);
  }, [close, onOpenPage, pages, query, readBlock, rememberSearch, reportSearch]);

  const applyRecentSearch = useCallback((value: string) => { setQuery(value); focusOpenedField(() => inputRef.current); }, []);

  useEffect(() => {
    if (!open) return;
    setRecentSearches(readRecentSearches());
    blocks.current.clear();
  }, [open]);

  // The server's answer replaces what stands for this search once it comes;
  // the words and filters changing meanwhile, a later read takes its place.
  useEffect(() => {
    setLoadingMore(false);
    if (!readsServer) return;
    const read = beginSearchRead();
    const key = answerKey;
    const timer = window.setTimeout(() => {
      if (!read.isCurrent()) return;
      searchWorkspaceAfterProjection(session, workspace.id, trimmedQuery, {
        limit: SEARCH_RESULT_LIMIT,
        contentState,
        filterParams,
        signal: read.signal,
      })
        .then((result) => {
          if (!read.isCurrent()) return;
          const next = { key, results: result.results, nextCursor: result.hasMore ? result.nextCursor : null, backlog: Boolean(result.backlog) };
          keepSearchAnswer(answerScope, next);
          learnActors(result.actors);
          setAnswer(next);
        })
        .catch(() => undefined)
        .finally(() => {
          if (read.isCurrent()) setReadKey(key);
        });
    }, REMOTE_SEARCH_DEBOUNCE_MS);
    return () => window.clearTimeout(timer);
  }, [answerKey, answerScope, beginSearchRead, contentState, filterParams, learnActors, readsServer, session, trimmedQuery, workspace.id]);

  // A filter's change searches again from the first result.
  function changeFilters(next: SearchFilters, nextContentState = contentState) {
    picker.state.selectionManager.setFocusedKey(null);
    setFilters(next);
    setContentState(nextContentState);
  }

  const loadMore = useCallback(async () => {
    if (!nextCursor || loadingMore || !freshAnswer) return;
    const read = beginSearchRead();
    const shown = freshAnswer;
    setLoadingMore(true);
    try {
      const result = await continueWorkspaceSearch(session, workspace.id, trimmedQuery, {
        cursor: nextCursor,
        limit: SEARCH_MORE_LIMIT,
        contentState,
        filterParams,
        signal: read.signal,
      });
      if (!read.isCurrent()) return;
      learnActors(result.actors);
      const results = "restarted" in result && result.restarted ? result.results
        : [...new Map([...shown.results, ...result.results].map((hit) => [`${hit.match}:${hit.page.id}`, hit])).values()];
      const next = { ...shown, results, nextCursor: result.hasMore ? result.nextCursor : null };
      keepSearchAnswer(answerScope, next);
      setAnswer(next);
    } catch {
      // The partial results stay; "더 검색" can be tried again.
    } finally {
      if (read.isCurrent()) setLoadingMore(false);
    }
  }, [answerScope, beginSearchRead, contentState, filterParams, freshAnswer, learnActors, loadingMore, nextCursor, session, trimmedQuery, workspace.id]);

  // Past its first page a search reads on until its head can count it
  // (SEARCH_COUNT_LIMIT), once for each place it stopped; further on, as
  // the list nears its end (SearchResults), as Notion's loads more rows.
  const remoteCount = freshAnswer?.results.length ?? 0;
  const readOnFrom = useRef<string | null>(null);
  useEffect(() => {
    if (!nextCursor || loadingMore || remoteCount > SEARCH_COUNT_LIMIT || readOnFrom.current === nextCursor) return;
    readOnFrom.current = nextCursor;
    void loadMore();
  }, [loadMore, loadingMore, nextCursor, remoteCount]);

  const newPageRow = forNewTab && !trimmedQuery && !browsing && Boolean(onCreatePage);
  // The page the "새 페이지" row makes opens in a new tab in front at the end once the server has made it,
  // its title taking the caret, as every page made for a new tab (openNewPageInNewDesktopTab).
  function createInNewTab() {
    if (!onCreatePage) return;
    close();
    void onCreatePage().then((page) => { if (page) openNewPageInNewDesktopTab(page.id); }, () => undefined);
  }
  const choices: SearchChoice[] = [
    ...(newPageRow ? [{ key: NEW_PAGE_CHOICE, label: t("searchDialog:newTab.newPage"), create: true as const }] : []),
    ...listedHits.map(hit => ({ key: searchHitKey(hit), label: pageNames.title(hit.page), hit })),
    ...visibleRecentSearches.map(recent => ({ key: `query:${recent}`, label: recent, recent })),
  ];
  function choose(choice: SearchChoice) {
    // A search for a new tab (the Mac app's ⌘T) opens what it finds in one, in front at the end.
    const newTab = newTabIntent.current || forNewTab;
    newTabIntent.current = false;
    if (choice.create) createInNewTab();
    else if (choice.hit) {
      if (forNewTab) pickedForNewTab();
      openHit(choice.hit, newTab);
    }
    else if (choice.recent) applyRecentSearch(choice.recent);
  }
  const picker = useNativeAutocomplete({ items: choices, query, onQuery: setQuery, inputRef, listRef: resultsRef, popoverRef: dialogRef, label: t("searchDialog:quickFind"), open, commitTab: false,
    keyOf: choice => choice.key, textOf: choice => choice.label, onPick: choose, wrap: "stop" });
  const focusedHit = choices.find(choice => choice.key === picker.activeKey)?.hit ?? null;
  const newPageFocused = newPageRow && picker.activeKey === NEW_PAGE_CHOICE;
  // A chosen result found by its body has its block asked once the keys rest on it, as its highlights are.
  const focusedAtBlock = focusedHit && opensAtBlock(focusedHit) ? focusedHit : null;
  useEffect(() => {
    if (!focusedAtBlock) return;
    const timer = window.setTimeout(() => readBlock(focusedAtBlock), BLOCK_READ_DELAY_MS);
    return () => window.clearTimeout(timer);
  }, [focusedAtBlock, readBlock]);
  // The result found by its body under the pointer.
  const pointerRest = useRef(0);
  useEffect(() => () => window.clearTimeout(pointerRest.current), [open]);
  function resultAt(target: EventTarget) {
    const key = (target as Element).closest?.("[role=option]")?.getAttribute("data-key");
    const hit = key ? choices.find(choice => choice.key === key)?.hit : undefined;
    return hit && opensAtBlock(hit) ? hit : undefined;
  }

  closeRef.current = close;
  useQuickSearchWindow(open, close);
  if (!open) return null;
  // While the preview is on the window is wide, and the preview shows the
  // chosen row — a visited page before anything is typed; with nothing
  // found the wide window keeps its message, as Notion's (seen 2026-10-31).
  // While a search reads with nothing to hold, the preview's card is gray, as Notion's (seen 2026-11-14).
  const previewLoading = searching && Boolean(trimmedQuery) && listedHits.length === 0;
  // The preview stands from the window's first frame, its rows the frame after: the window keeps its width.
  const showHighlights = highlightsShown && (!listShown || listedHits.length > 0 || newPageRow || previewLoading);
  return (
    <div className={styles.overlay} style={{ zIndex }} data-search-overlay>
      <button type="button" className={`${styles.backdrop} ${phoneStyles.shade}`} onClick={dismiss} tabIndex={-1} aria-label={t("searchDialog:closeSearch")} />
      <div ref={dialogRef} className={`${styles.dialog} ${phoneStyles.dialog}`} role="dialog" aria-modal="true" aria-labelledby={titleId}
        onPointerDownCapture={event => {
          newTabIntent.current = event.metaKey || event.ctrlKey;
          // A result pressed has its block asked at the press, if the pointer didn't rest on it before.
          const hit = resultAt(event.target);
          if (hit) readBlock(hit);
        }}
        onPointerOver={event => {
          // As the keys resting on a result, the pointer resting on one asks its block.
          window.clearTimeout(pointerRest.current);
          const hit = resultAt(event.target);
          if (hit) pointerRest.current = window.setTimeout(() => readBlock(hit), BLOCK_READ_DELAY_MS);
        }}
        onKeyDownCapture={event => {
          newTabIntent.current = event.metaKey || event.ctrlKey; if (dismissKey(event.nativeEvent)) return;
          // Native Cmd/Ctrl+Enter opens the engine's focused result in a new tab.
          if (fieldEnter(event) && newTabIntent.current) {
            const choice = choices.find(choice => choice.key === picker.activeKey);
            if (choice) { event.preventDefault(); event.stopPropagation(); choose(choice); }
          }
          // The app's ⌘L copies the chosen result's link, as Notion's app's ("클립보드에 결과 링크 복사됨").
          if (inDesktopTab() && focusedHit && matchesKeyboardShortcut("copyPageLink", event.nativeEvent)) {
            event.preventDefault();
            event.stopPropagation();
            // A full-page database's names its view, as its preview's "링크 복사" (databasePageLink.ts).
            void readPageLinkToCopy(session, focusedHit.page).then((link) => copyPageLink(link, focusedHit.page.id, pageNames.title(focusedHit.page)))
              .then((ok) => { if (ok) toast?.notify(t("searchDialog:copiedResultLink"), "success"); });
          }
        }}
        data-highlights={highlightsShown ? "shown" : undefined}>
        <div id={titleId} className={styles.srOnly}>{t("searchDialog:quickFind")}</div>
        <div className={`${styles.searchRow} ${phoneStyles.row}`}>
          <div className={phoneStyles.field}>
          <Search size="wide" aria-hidden="true" />
          <input {...picker.inputProps} ref={inputRef}
            placeholder={forNewTab ? t("searchDialog:newTab.placeholder") : t("searchDialog:searchPlaceholder", { workspace: workspace.name })} />
          <button type="button" className={styles.highlightToggle} aria-pressed={highlightsShown} data-search-highlights-toggle
            aria-label={t(highlightsShown ? "searchDialog:highlights.hide" : "searchDialog:highlights.show")}
            onClick={() => setHighlightsShown(!highlightsShown)}>
            <HighlightPaneIcon size="regular" aria-hidden="true" />
          </button>
          <button type="button" className={styles.highlightToggle} aria-pressed={filtersShown} data-search-filters-toggle
            aria-label={t(filtersShown ? "searchDialog:filters.hide" : "searchDialog:filters.show")}
            onClick={() => setFiltersShown(!filtersShown)}>
            <FilterCircleIcon size="wide" aria-hidden="true" />
          </button>
          </div>
          <button type="button" className={phoneStyles.close} onClick={close}>{t("searchDialog:phoneCancel")}</button>
        </div>
        {filtersShown ? (
          <SearchFilterBar session={session} workspace={workspace} pages={pages} pagesById={pagesById} teamspaces={teamspaces} filters={filters}
            onChange={changeFilters} contentState={contentState}
            onContentState={(next) => changeFilters(filters, next)} />
        ) : null}
        <div className={styles.resultsArea}>
        <SearchResults
          actors={actors}
          currentPageId={currentPageId}
          reader={session.user}
          state={picker.state}
          listBoxProps={picker.listBoxProps}
          query={query} resultsShown={Boolean(trimmedQuery) || browsing}
          pagesById={pagesById}
          emojiTitles={emojiTitles}
          rowsComing={!listShown}
          sections={sections}
          hits={listedHits}
          teamspaceNames={teamspaceNames}
          archivedTeamspaceIds={archivedTeamspaceIds}
          recentSearches={visibleRecentSearches}
          searching={searching}
          continuation={nextCursor ? { loading: loadingMore } : null}
          resultsRef={resultsRef}
          sectionAction={!phone && (trimmedQuery || browsing)
            ? <SearchSortMenu sort={filters.sort} onChange={(sort) => changeFilters({ ...filters, sort })} /> : undefined}
          onLoadMore={() => void loadMore()}
          onSearchTrash={onSearchTrash && !workspace.guest ? (value) => {
            close();
            onSearchTrash(value);
          } : undefined}
          before={newPageRow ? <SearchNewPageRow state={picker.state} /> : undefined}
          phone={phone}
        />
        {showHighlights && newPageFocused ? <SearchNewPageCard /> : showHighlights ? (
          <SearchHighlightPanel hit={focusedHit} loading={previewLoading} onOpen={(hit) => openHit(hit, false)} pagesById={pagesById} emojiTitles={emojiTitles}
            query={trimmedQuery} results={Boolean(trimmedQuery) || browsing} session={session} workspaceId={workspace.id} />
        ) : null}
        </div>
        <SearchFooter keepFilters={kept.keep} onKeepFilters={kept.setKeep} copyLink={inDesktopTab()}
          onEditShortcut={() => { close(); editCommandSearchShortcut(); }} />
      </div>
    </div>
  );
}
