import { normalizePastedLink } from "./editorInputModel";
import { useEffect, useLayoutEffect, useMemo, useRef, useState, type RefObject } from "react";
import { useTranslation } from "react-i18next";
import { useLibraryPages, usePageVisits } from "@/components/library/useLibraryData";
import { libraryRowPage } from "@/components/library/libraryModel";
import { PageIconGlyph } from "@/components/page/PageIcon";
import { useProductPages } from "@/components/product/productPagesContext";
import { GlobeIcon } from "@/icons/leafen";
import { NativePickerOption, useNativeAutocomplete } from "@/lib/nativeAutocomplete";
import { databasePagePlace, pagePlace } from "@/lib/pagePath";
import { pathLeavesOwnPages } from "@/lib/pagePathPieces";
import { PlaceLine } from "../PlaceLine";
import { usePageNames } from "@/lib/pageNames";
import { searchWorkspacePages, type PageSearchWindowResult } from "@/lib/workspacePageSearch";
import { pageSearchRank } from "@/lib/pageSearchRank";
import { foldNfcText } from "@/shared/database/natural-order.mjs";
import type { Page, Session } from "@/types";
import styles from "./editorStyles.generated.module.css";
import rowStyles from "./SelectionToolbarLinkRows.module.css";
import { usePagePathsAbove } from "@/lib/usePagePathsAbove";
import { linkedPage } from "./linkOpening";
import { outOfTrashIn, pagesByIdOf } from "@/lib/pageIndex";
import { pageArchiveRoot } from "@/lib/pageArchive";

// Notion's link picker showed three recent pages (seen 2026-10-08).
const RECENT = 3;
const FOUND = 8;
const SEARCH_DELAY_MS = 150;

/** Where the picker finds pages; without it only a typed address links. */
export type SelectionLinkPages = { session: Session; workspaceId: string };

type Choice = { key: string; page?: Page; label: string };

type PickerProps = {
  inputRef: RefObject<HTMLInputElement | null>;
  value: string;
  onValue: (value: string) => void;
  source?: SelectionLinkPages;
  onPage: (page: Page) => void;
  onAddress: () => void;
  onClose?: () => void;
  popoverRef: RefObject<HTMLElement | null>;
  placeholder?: string;
  /** The link's own address, standing in the field: nothing to choose until it changes. */
  current?: string;
  /**
   * The link's address where it leads to a page here: while the field hasn't
   * the focus, its empty field shows the page instead (LinkPageChip).
   */
  pageLink?: string;
  /** How large a listed page's glyph is drawn: 20, or a phone sheet's 22 (PhoneLinkSheet). */
  pageIconSize?: number;
  /** The keyboard's action key the field asks for, a phone sheet's (PhoneLinkSheet). */
  enterKeyHint?: "next";
};

const NO_VISITS: readonly { pageId: string }[] = [];
const NO_ROW_PAGES: ReadonlyMap<string, Page> = new Map();

/**
 * The selected text's link, as Notion's picker asks for it (seen
 * 2026-10-08): one field, "링크 붙여넣기 또는 페이지 검색", over "최근
 * 페이지" — each with its icon, title and where it sits — or, once something
 * is typed, the pages it finds and the typed text itself as an address to
 * link ("연결할 전체 URL 입력"). Choosing a page links its address. No
 * page in the trash or under one in it is listed (outOfTrashIn). Pages are
 * found by their titles alone, as the "@" menu finds them (pageSearchRank),
 * the server's search too (titles): not by a body holding the words — a
 * child page's title in its parent's — nor by the pages above them (seen
 * 2026-11-25 in Notion's link window, two tabs).
 */
export function SelectionToolbarLinkPicker(props: PickerProps) {
  return props.source ? <VisitedLinkPicker {...props} source={props.source} /> : <LinkPicker {...props} visits={NO_VISITS} />;
}

/**
 * The reader's visits are read only where there is a reader to read them for.
 * A database row's page visited isn't in the page list: the server reads it,
 * as Home's and the search's lists of visits do, for the first ones listed.
 */
function VisitedLinkPicker(props: PickerProps & { source: SelectionLinkPages }) {
  const { session, workspaceId } = props.source;
  const visits = usePageVisits(session, workspaceId);
  const pages = useProductPages()?.pages;
  const [unlistedIds, setUnlistedIds] = useState<readonly string[]>([]);
  const unlisted = useLibraryPages(session, workspaceId, unlistedIds).pages;
  // The visits the list may show, in order, until it has its rows: those
  // not in the page list are read, those read as no row's page are passed.
  const wanted = useMemo(() => {
    if (!pages) return [];
    const pagesById = pagesByIdOf(pages), outOfTrash = outOfTrashIn(pages), ids: string[] = [];
    let shown = 0;
    for (const id of new Set(visits.map(({ pageId }) => pageId))) {
      if (shown >= RECENT) break;
      const page = pagesById[id];
      if (page) { if (outOfTrash(page)) shown += 1; continue; }
      const detail = unlisted.get(id);
      if (detail && !detail.row) continue;
      ids.push(id);
      shown += 1;
    }
    return ids;
  }, [pages, unlisted, visits]);
  const wantedKey = wanted.join(",");
  useEffect(() => {
    setUnlistedIds((current) => current.join(",") === wantedKey ? current : wanted);
    // The ids to read are the ones wantedKey names.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [wantedKey]);
  const rowPages = useMemo(() => new Map(wanted.flatMap((id) => {
    const detail = unlisted.get(id), page = detail ? libraryRowPage(detail, workspaceId) : null;
    return page ? [[id, page] as const] : [];
  })), [unlisted, wanted, workspaceId]);
  return <LinkPicker {...props} visits={visits} rowPages={rowPages} />;
}

function LinkPicker({ inputRef, value, onValue, source, onPage, onAddress, onClose, popoverRef, placeholder, current, pageLink, pageIconSize = 20, enterKeyHint, visits, rowPages = NO_ROW_PAGES }: PickerProps & {
  visits: readonly { pageId: string }[];
  /** Visited pages of database rows, which the page list lacks. */
  rowPages?: ReadonlyMap<string, Page>;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation("selectionToolbar");
  const listRef = useRef<HTMLDivElement>(null);
  const pages = useProductPages()?.pages;
  const query = value.trim();
  const [found, setFound] = useState<readonly Page[]>([]);
  // The database pages found, by page: where each sits, for its path.
  const [foundDatabasePages, setFoundDatabasePages] = useState<NonNullable<PageSearchWindowResult["databasePages"]>>({});

  useEffect(() => {
    if (!query || !source) return undefined;
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      void searchWorkspacePages(source.session, query, FOUND, source.workspaceId, { signal: controller.signal, titles: true })
        .then((result) => {
          if (controller.signal.aborted) return;
          setFound(result.pages);
          setFoundDatabasePages(result.databasePages ?? {});
        })
        .catch(() => {
          // The pages already known still answer the search.
        });
    }, SEARCH_DELAY_MS);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [query, source]);

  const pagesById = pagesByIdOf(pages ?? []);
  const choices = useMemo<Choice[]>(() => {
    // A whole address links that address alone, as Notion's (seen 2026-11-03).
    if (!source || !pages || normalizePastedLink(query)) return query ? [{ key: "address", label: query }] : [];
    const outOfTrash = outOfTrashIn(pages), listed = (page: Page) => outOfTrash(page) && page.workspaceId === source.workspaceId;
    if (!query) return [];
    const folded = foldNfcText(query);
    const known = pages.filter((page) => listed(page) && pageSearchRank(foldNfcText(pageNames.title(page)), folded) < Number.POSITIVE_INFINITY);
    const seen = new Set(known.map((page) => page.id));
    const more = found.filter((page) => listed(page) && !seen.has(page.id));
    return [...[...known, ...more].slice(0, FOUND).map((page) => ({ key: page.id, page, label: pageNames.title(page) })),
      { key: "address", label: query }];
  }, [found, pages, query, source, pageNames]);
  const recent = useMemo<Choice[]>(() => {
    if (!source || !pages || query) return [];
    const outOfTrash = outOfTrashIn(pages);
    return [...new Set(visits.map(({ pageId }) => pageId))].flatMap((id) => {
      const page = pagesById[id] ?? rowPages.get(id);
      return page && outOfTrash(page) ? [{ key: page.id, page, label: pageNames.title(page) }] : [];
    }).slice(0, RECENT);
  }, [pages, pagesById, query, rowPages, source, visits, pageNames]);
  const rows = value === current ? [] : query ? choices : recent;
  // A database's page is placed by the path the server names, its database
  // last ("〈test page〉 / ... / DB"), as Notion's link field (seen
  // 2026-11-15) and its search place one.
  const databasePlaceOf = (page: Page) => foundDatabasePages[page.id]
    ?? (rowPages.has(page.id) ? { containerPageId: page.parentId ?? "" } : undefined);
  usePagePathsAbove(rows.flatMap((row) => row.page && (databasePlaceOf(row.page)
    || pathLeavesOwnPages({ id: row.page.id, parentId: row.page.parentId ?? "" }, (id) => pagesById[id])) ? [row.page] : []));
  const placeOf = (page: Page) => {
    const database = databasePlaceOf(page);
    return database ? databasePagePlace(database.containerPageId, pagesById, database, page.id) : pagePlace(page, pagesById);
  };

  const collection = useNativeAutocomplete({ items: rows, query: value, onQuery: onValue, inputRef, listRef, popoverRef,
    label: t("linkEditor.urlAriaLabel"), keyOf: (row) => row.key, textOf: (row) => row.label, commitTab: false,
    // Past either end the arrows rest on the field, no row lit, where Enter
    // does nothing, as Notion's (seen 2026-11-14).
    wrap: "throughNone", onEnterNone: (event) => event.preventDefault(),
    onPick: (row) => (row.page ? onPage(row.page) : onAddress()), onClose });

  // A page link's empty field shows its page while the focus is elsewhere; a
  // press on the page, or the focus reaching it, gives the field back, focused.
  const [inField, setInField] = useState(false);
  const page = pageLink && !inField && !value ? linkedPage(pageLink, pages) : undefined;
  useLayoutEffect(() => {
    if (inField && document.activeElement !== inputRef.current) inputRef.current?.focus();
  }, [inField, inputRef]);

  return (
    <>
      {page ? <LinkPageChip page={page} iconSize={pageIconSize > 20 ? pageIconSize : 16} onPress={() => setInField(true)} />
        : <input {...collection.inputProps} ref={inputRef} className={styles.tbLinkInput} placeholder={placeholder ?? t("linkEditor.placeholder")} enterKeyHint={enterKeyHint}
          onFocus={(event) => { setInField(true); collection.inputProps.onFocus?.(event); }}
          onBlur={(event) => { setInField(false); collection.inputProps.onBlur?.(event); }} />}
      <div {...collection.listBoxProps} ref={listRef} className={styles.tbLinkList} hidden={rows.length === 0}>
        {!query && rows.length ? <div className={styles.tbLinkHeading}>{t("linkEditor.recentPages")}</div> : null}
        {rows.map((row) => {
          // A page at the top has no place under its title, and its row is one line.
          const place = row.page ? placeOf(row.page) : null;
          return (
            <NativePickerOption key={row.key} state={collection.state} itemKey={row.key}
              className={place?.length === 0 ? rowStyles.tbLinkRow : `${rowStyles.tbLinkRow} ${rowStyles.tbLinkRowPlaced}`}
              data-link-choice={row.page ? row.page.id : "address"}>
              {({ labelProps }) => <>
                {/* Its parts are named for a sheet that draws them its own way (NativePhoneToolbar.module.css .linkField). */}
                <span className={rowStyles.tbLinkIcon} aria-hidden="true" data-link-part="icon">
                  {row.page ? <PageIconGlyph page={row.page} size={pageIconSize} /> : <GlobeIcon size="regular" />}
                </span>
                <span className={rowStyles.tbLinkText}>
                  {/* An archived page, or one under it, greys its title, as Notion's link field (PickerArchivedPage.tsx). */}
                  <span {...labelProps} className={rowStyles.tbLinkTitle} data-link-part="title"
                    data-archived={row.page && pageArchiveRoot(row.page, (id) => pagesById[id]) ? "" : undefined}>{row.label}</span>
                  {place?.length === 0 ? null : <span className={rowStyles.tbLinkPlace} data-link-part="place">
                    {place ? <PlaceLine place={place} /> : t(normalizePastedLink(query) ? "linkEditor.webPageLink" : "linkEditor.fullAddress")}
                  </span>}
                </span>
              </>}
            </NativePickerOption>
          );
        })}
      </div>
    </>
  );
}

/**
 * A page link's field while another part of its window has the focus, as
 * Notion's (seen 2026-11-12, a text's and a picture's link alike): the page's
 * icon and title on a 28px chip, 4px higher than the field and 8px in from
 * it; a press — taking the focus, which its window reads as the field's —
 * gives back the empty field ("링크 수정 또는 페이지 검색") to search again.
 * A sheet draws it its own way, its glyph at the sheet's size (NativePhoneToolbar.module.css .linkField).
 */
function LinkPageChip({ page, iconSize, onPress }: { page: Page; iconSize: number; onPress: () => void }) {
  const pageNames = usePageNames();
  return <button type="button" className={styles.linkPageChip} onMouseDown={(event) => { event.preventDefault(); onPress(); }} onFocus={onPress}
    data-link-page="">
    <span className={styles.linkPageChipIcon} aria-hidden="true"><PageIconGlyph page={page} size={iconSize} /></span>
    <span className={styles.linkPageChipTitle}>{pageNames.title(page)}</span>
  </button>;
}
