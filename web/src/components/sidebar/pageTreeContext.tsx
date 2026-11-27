import type { ProductStatusSink } from "@/lib/productStatus";
import {
  createContext,
  type ReactNode,
  useCallback,
  useContext,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { parseStored, personValue, storePersonValue } from "@/lib/browserStorage";
import type { NativePageMetadataChange } from "@/lib/nativePageMetadata";
import type { PageDuplicationOptions } from "@/lib/pageDuplicationService";
import type { PageActionAccessReader } from "@/lib/pageActionAccess";
import { usePageActionAccessReader } from "@/lib/pageActionAccessContext";
import type { Page, Session } from "@/types";
import { useProductPages } from "@/components/product/productPagesContext";
import type { DatabaseFavoriteProjection, InlineDatabaseProjection } from "@/workspaceMembershipWire.generated";
import { pageTreeMap } from "./pageTreeModel";
import { createPageTreeStore, type PageTreeState, type PageTreeStore } from "./pageTreeRowStore";

type MetadataResult = boolean | Page | void;

export type PageTreeContextValue = {
  expandedPageIds: ReadonlySet<string>;
  favoritePageIds: ReadonlySet<string>;
  pages: Page[];
  pagesById: Record<string, Page>;
  selectedPageId: string | null;
  session: Session;
  pageAccess: PageActionAccessReader;
  canCreateRoot: boolean;
  /**
   * Makes a page under the parent, or a row in a full-page database, which
   * opens on its own: then null. A parent the page list doesn't hold (a
   * database row's page) comes with its workspace.
   */
  onCreateSubpage(parentId: string, workspaceId?: string): Promise<Page | null>;
  onDuplicatePage(page: Page, signal: AbortSignal, options?: PageDuplicationOptions): Promise<Page>;
  onFavoriteChange(pageId: string, favorite: boolean): Promise<void>;
  onOpenPage(page: Page, newTab: boolean): void;
  onPageChange(page: Page): void;
  onPageMetadataChange(
    page: Page,
    change: NativePageMetadataChange,
    signal: AbortSignal,
  ): Promise<MetadataResult>;
  onStatus: ProductStatusSink;
  onTransferBlock(input: {
    blockId: string;
    copy: boolean;
    destinationPageId: string;
    signal: AbortSignal;
    sourcePageId: string;
  }): Promise<void>;
  setPageExpanded(pageId: string, expanded: boolean): void;
};

const PageTreeContext = createContext<PageTreeContextValue | null>(null);

/**
 * What a row reads beside its own part of the tree's state: the person, what
 * they may do and the tree's actions, which stay as pages open and change,
 * so a row draws again only for its own part (pageTreeRowStore.ts).
 */
export type PageTreeRowContextValue = Pick<PageTreeContextValue,
  | "session" | "pageAccess" | "canCreateRoot" | "onCreateSubpage" | "onDuplicatePage" | "onFavoriteChange" | "onOpenPage"
  | "onPageChange" | "onPageMetadataChange" | "onStatus" | "onTransferBlock" | "setPageExpanded"> & { store: PageTreeStore };

const PageTreeRowContext = createContext<PageTreeRowContextValue | null>(null);

const NO_INLINE_DATABASES: readonly InlineDatabaseProjection[] = [];
const NO_DATABASE_FAVORITES: readonly DatabaseFavoriteProjection[] = [];

function sameMembers(left: ReadonlySet<string>, right: ReadonlySet<string>) {
  if (left.size !== right.size) return false;
  for (const id of left) if (!right.has(id)) return false;
  return true;
}

// The pages a person opened in a workspace's tree are theirs, on this browser.
const EXPANDED_KEY = "leafen:page-tree-expanded";

function expandedPlace(session: Session, pages: readonly Page[]) {
  return { userId: session.user.id, part: pages[0]?.workspaceId ?? "none" };
}

function readExpandedPageIds(session: Session, pages: readonly Page[]) {
  const place = expandedPlace(session, pages);
  const parsed = parseStored(personValue(EXPANDED_KEY, { ...place, previous: [`${EXPANDED_KEY}:${place.userId}:${place.part}`] }));
  return new Set(
    Array.isArray(parsed)
      ? parsed.filter((id): id is string => typeof id === "string")
      : [],
  );
}

export function PageTreeProvider({
  children,
  value,
}: {
  children: ReactNode;
  value: Omit<PageTreeContextValue, "expandedPageIds" | "favoritePageIds" | "pagesById" | "setPageExpanded" | "pageAccess"> & {
    favoritePageIds: readonly string[];
    /** Pages in the trash with the page above them: a favorite among them still draws its row. */
    trashedPages?: readonly Page[];
  };
}) {
  const [expandedPageIds, setExpandedPageIds] = useState<Set<string>>(
    () => readExpandedPageIds(value.session, value.pages),
  );
  const pagesById = useMemo(() => pageTreeMap([...(value.trashedPages ?? []), ...value.pages]), [value.pages, value.trashedPages]);
  const favoritePageIds = useMemo(() => new Set(value.favoritePageIds), [value.favoritePageIds]);
  const pageAccess = usePageActionAccessReader();
  const scope = expandedPlace(value.session, value.pages);
  const scopeKey = `${scope.userId}:${scope.part}`;
  const previousScopeKey = useRef(scopeKey);
  const latestValue = useRef(value);
  latestValue.current = value;

  useEffect(() => {
    // Opening a page leaves the tree as it is: a sub-page opened from a page's
    // body, a link, a mention, search, back and forward or a reload keeps its
    // place closed, its row unshown, as Notion's (seen 2026-11-14 by "/page" and
    // 2026-11-16 by a page block and a reload); a page moved into a closed one
    // leaves it closed (seen 2026-11-03). A row's own "+" opens the row it adds to.
    const scopeChanged = previousScopeKey.current !== scopeKey;
    previousScopeKey.current = scopeKey;
    setExpandedPageIds((current) => {
      const source = scopeChanged
        ? readExpandedPageIds(value.session, value.pages)
        : current;
      // A favorite's or recent page's opening ("favorite:…") is kept apart
      // from the same page's elsewhere.
      const next = new Set([...source].filter((id) => pagesById[id.slice(id.indexOf(":") + 1)]));
      return sameMembers(next, current) ? current : next;
    });
  }, [pagesById, scopeKey, value.pages, value.session]);

  const setPageExpanded = useCallback((pageId: string, expanded: boolean) => {
    setExpandedPageIds((current) => {
      const next = new Set(current);
      if (expanded) next.add(pageId);
      else next.delete(pageId);
      // Without storage the expansion stays in memory.
      storePersonValue(EXPANDED_KEY, JSON.stringify([...next]), expandedPlace(latestValue.current.session, latestValue.current.pages));
      return next;
    });
  }, []);

  const onCreateSubpage = useCallback((parentId: string, workspaceId?: string) => (
    latestValue.current.onCreateSubpage(parentId, workspaceId)
  ), []);
  const onDuplicatePage = useCallback((page: Page, signal: AbortSignal, options?: PageDuplicationOptions) => (
    latestValue.current.onDuplicatePage(page, signal, options)
  ), []);
  const onFavoriteChange = useCallback((pageId: string, favorite: boolean) => (
    latestValue.current.onFavoriteChange(pageId, favorite)
  ), []);
  const onOpenPage = useCallback((page: Page, newTab: boolean) => (
    latestValue.current.onOpenPage(page, newTab)
  ), []);
  const onPageChange = useCallback((page: Page) => latestValue.current.onPageChange(page), []);
  const onPageMetadataChange = useCallback((
    page: Page,
    change: NativePageMetadataChange,
    signal: AbortSignal,
  ) => latestValue.current.onPageMetadataChange(page, change, signal), []);
  const onStatus = useCallback<ProductStatusSink>((message, tone) => latestValue.current.onStatus(message, tone), []);
  const onTransferBlock = useCallback((input: Parameters<PageTreeContextValue["onTransferBlock"]>[0]) => (
    latestValue.current.onTransferBlock(input)
  ), []);

  const context = useMemo<PageTreeContextValue>(() => ({
    expandedPageIds,
    favoritePageIds,
    pages: value.pages,
    pagesById,
    selectedPageId: value.selectedPageId,
    session: value.session,
    pageAccess,
    canCreateRoot: value.canCreateRoot,
    onCreateSubpage,
    onDuplicatePage,
    onFavoriteChange,
    onOpenPage,
    onPageChange,
    onPageMetadataChange,
    onStatus,
    onTransferBlock,
    setPageExpanded,
  }), [
    expandedPageIds,
    favoritePageIds,
    onCreateSubpage,
    onDuplicatePage,
    onFavoriteChange,
    onOpenPage,
    onPageChange,
    onPageMetadataChange,
    onStatus,
    onTransferBlock,
    pagesById,
    pageAccess,
    setPageExpanded,
    value.canCreateRoot,
    value.pages,
    value.selectedPageId,
    value.session,
  ]);

  const productPages = useProductPages();
  const inlineDatabases = productPages?.inlineDatabases ?? NO_INLINE_DATABASES;
  const databaseFavorites = productPages?.databaseFavorites ?? NO_DATABASE_FAVORITES;
  const treeState = useMemo<PageTreeState>(() => ({
    pages: value.pages,
    pagesById,
    selectedPageId: value.selectedPageId,
    expandedPageIds,
    favoritePageIds,
    inlineDatabases,
    databaseFavorites,
  }), [databaseFavorites, expandedPageIds, favoritePageIds, inlineDatabases, pagesById, value.pages, value.selectedPageId]);
  const [store] = useState(() => createPageTreeStore(treeState));
  useLayoutEffect(() => store.publish(treeState), [store, treeState]);
  const rowContext = useMemo<PageTreeRowContextValue>(() => ({
    store,
    session: value.session,
    pageAccess,
    canCreateRoot: value.canCreateRoot,
    onCreateSubpage,
    onDuplicatePage,
    onFavoriteChange,
    onOpenPage,
    onPageChange,
    onPageMetadataChange,
    onStatus,
    onTransferBlock,
    setPageExpanded,
  }), [onCreateSubpage, onDuplicatePage, onFavoriteChange, onOpenPage, onPageChange, onPageMetadataChange, onStatus, onTransferBlock,
    pageAccess, setPageExpanded, store, value.canCreateRoot, value.session]);

  return (
    <PageTreeContext.Provider value={context}>
      <PageTreeRowContext.Provider value={rowContext}>{children}</PageTreeRowContext.Provider>
    </PageTreeContext.Provider>
  );
}

export function usePageTreeContext() {
  const value = useContext(PageTreeContext);
  if (!value) throw new Error("PageTreeItem must be rendered inside PageTreeProvider.");
  return value;
}

export function usePageTreeRowContext() {
  const value = useContext(PageTreeRowContext);
  if (!value) throw new Error("PageTreeItem must be rendered inside PageTreeProvider.");
  return value;
}
