import type { GraphReadItem } from "@/contentGraphWire.generated";
import type {
  PublicContentPageSummary,
  PublicContentSnapshotRequest,
  PublicContentSnapshotResult,
  PublicSiteConfiguration,
} from "@/publicContentWire.generated";
import type { Page } from "@/types";
import type { DatabasePageLayout } from "@/lib/databasePageLayout";
import type { TextSpan } from "@/lib/types";

export type PublicContentWindowRequest = Omit<
  PublicContentSnapshotRequest,
  "action" | "sourceKind" | "token" | "siteSlug"
>;

export type PublicContentPageDocument = {
  page: Page;
  graph: GraphReadItem[];
  graphNextCursor: string | null;
  graphHasMore: boolean;
  /** A database page's layout, read with its first window. */
  pageLayout?: DatabasePageLayout;
};

export type PublicContentDocument = {
  sourceKind: PublicContentSnapshotResult["sourceKind"];
  authorityId: string;
  rootPageId: string;
  sourceRevision: number;
  pages: PublicContentPageSummary[];
  pagesNextCursor: string | null;
  pagesHasMore: boolean;
  site?: PublicSiteConfiguration;
  /** How a view link shows its page. */
  view?: PublicContentSnapshotResult["view"];
  /** A web link that lets the organization's people comment or edit. */
  linkRole?: PublicContentSnapshotResult["linkRole"];
  documents: Record<string, PublicContentPageDocument>;
};

/**
 * The page read, as the list names it where the read listed it not: an
 * untitled full-page database by its kind or its sources (pageDisplayTitle),
 * as its heading and window name it.
 */
function pageSummary(page: Page): PublicContentPageSummary {
  return {
    id: page.id,
    title: page.title,
    parentId: page.parentId,
    icon: page.icon,
    iconType: page.iconType,
    ...(page.titleSpans ? { titleSpans: page.titleSpans } : {}),
    ...(page.kind === "database" ? { kind: "database" as const } : {}),
    ...(page.sourceTitles?.length ? { sourceTitles: page.sourceTitles } : {}),
  };
}

const emojiToken = (span: TextSpan, tokens: Readonly<Record<string, string>>) => (
  span.mention === "custom_emoji" && span.customEmojiId ? tokens[span.customEmojiId] : undefined);

/**
 * A title with the token of each workspace emoji in it, from the table a
 * public read sends for its titles (customEmoji), as its text's spans carry
 * theirs: the visitor sees the emoji's picture where a member does
 * (TitleWithCustomEmoji).
 */
function titleWithEmoji<T extends { titleSpans?: TextSpan[] }>(named: T, tokens: Readonly<Record<string, string>> | undefined): T {
  if (!tokens || !named.titleSpans?.some((span) => emojiToken(span, tokens))) return named;
  return { ...named, titleSpans: named.titleSpans.map((span) => {
    const token = emojiToken(span, tokens);
    return token ? { ...span, customEmojiPublic: token } : span;
  }) };
}

/** Pages a public read lists, their titles' emoji drawn for the visitor (titleWithEmoji). */
export function publicSummariesWithEmoji(summaries: readonly PublicContentPageSummary[], tokens: Readonly<Record<string, string>> | undefined) {
  return summaries.map((summary) => titleWithEmoji(summary, tokens));
}

function snapshotWithEmoji(snapshot: PublicContentSnapshotResult): PublicContentSnapshotResult {
  if (!snapshot.customEmoji) return snapshot;
  return { ...snapshot, page: titleWithEmoji(snapshot.page, snapshot.customEmoji), pages: publicSummariesWithEmoji(snapshot.pages, snapshot.customEmoji) };
}

function mergeById<T extends { id: string }>(...lists: Array<readonly T[]>): T[] {
  const merged = new Map<string, T>();
  for (const list of lists) {
    for (const item of list) merged.set(item.id, item);
  }
  return Array.from(merged.values());
}

function graphItemKey(item: GraphReadItem) {
  return `${item.kind}:${item.value.id}`;
}

function mergeGraphItems(
  current: readonly GraphReadItem[],
  incoming: readonly GraphReadItem[],
) {
  const merged = new Map<string, GraphReadItem>();
  for (const item of current) merged.set(graphItemKey(item), item);
  for (const item of incoming) merged.set(graphItemKey(item), item);
  return Array.from(merged.values());
}

/** One window of a public page's read, as a page document. */
export function publicContentPageWindow(snapshot: PublicContentSnapshotResult): PublicContentPageDocument {
  return pageDocument(snapshotWithEmoji(snapshot));
}

/** A page document with the next window of its graph read after `cursor`. */
export function appendPublicContentPageWindow(
  current: PublicContentPageDocument,
  next: PublicContentPageDocument,
  cursor: string,
): PublicContentPageDocument {
  if (next.graphHasMore && next.graphNextCursor === cursor) {
    throw new Error("public content graph cursor repeated");
  }
  return {
    page: next.page,
    graph: mergeGraphItems(current.graph, next.graph),
    graphNextCursor: next.graphNextCursor,
    graphHasMore: next.graphHasMore,
    ...(current.pageLayout ? { pageLayout: current.pageLayout } : {}),
  };
}

function pageDocument(snapshot: PublicContentSnapshotResult): PublicContentPageDocument {
  return {
    page: snapshot.page,
    graph: snapshot.graph,
    graphNextCursor: snapshot.graphNextCursor,
    graphHasMore: snapshot.graphHasMore,
    ...(snapshot.pageLayout ? { pageLayout: snapshot.pageLayout } : {}),
  };
}

export function createPublicContentDocument(
  read: PublicContentSnapshotResult,
): PublicContentDocument {
  const snapshot = snapshotWithEmoji(read);
  if (snapshot.sourceKind === "site" && !snapshot.site) {
    throw new Error("public site configuration missing");
  }
  return {
    sourceKind: snapshot.sourceKind,
    authorityId: snapshot.authorityId,
    rootPageId: snapshot.rootPageId,
    sourceRevision: snapshot.sourceRevision,
    pages: mergeById([pageSummary(snapshot.page)], snapshot.pages),
    pagesNextCursor: snapshot.pagesNextCursor,
    pagesHasMore: snapshot.pagesHasMore,
    site: snapshot.site,
    view: snapshot.view,
    linkRole: snapshot.linkRole,
    documents: { [snapshot.page.id]: pageDocument(snapshot) },
  };
}

export function mergePublicContentDocument(
  current: PublicContentDocument,
  read: PublicContentSnapshotResult,
  request: PublicContentWindowRequest,
): PublicContentDocument {
  const snapshot = snapshotWithEmoji(read);
  if (
    snapshot.sourceKind !== current.sourceKind
    || snapshot.authorityId !== current.authorityId
    || snapshot.rootPageId !== current.rootPageId
    || snapshot.sourceRevision !== current.sourceRevision
  ) {
    throw new Error("public content revision mismatch");
  }
  if (
    request.pagesCursor !== undefined
    && snapshot.pagesHasMore
    && snapshot.pagesNextCursor === request.pagesCursor
  ) {
    throw new Error("public content pages cursor repeated");
  }
  if (
    request.graphCursor !== undefined
    && snapshot.graphHasMore
    && snapshot.graphNextCursor === request.graphCursor
  ) {
    throw new Error("public content graph cursor repeated");
  }

  const existing = current.documents[snapshot.page.id];
  const continued = request.graphCursor !== undefined && existing;
  const pagesOnly = request.pagesCursor !== undefined && request.graphCursor === undefined && existing;
  const nextPage = continued
    ? {
        page: snapshot.page,
        graph: mergeGraphItems(existing.graph, snapshot.graph),
        graphNextCursor: snapshot.graphNextCursor,
        graphHasMore: snapshot.graphHasMore,
        ...(existing.pageLayout ? { pageLayout: existing.pageLayout } : {}),
      }
    : pagesOnly
      ? { ...existing, page: snapshot.page }
      : pageDocument(snapshot);
  return {
    ...current,
    pages: mergeById(current.pages, [pageSummary(snapshot.page)], snapshot.pages),
    pagesNextCursor: request.pagesCursor !== undefined
      ? snapshot.pagesNextCursor
      : current.pagesNextCursor,
    pagesHasMore: request.pagesCursor !== undefined
      ? snapshot.pagesHasMore
      : current.pagesHasMore,
    site: snapshot.site ?? current.site,
    view: snapshot.view ?? current.view,
    linkRole: snapshot.linkRole,
    documents: { ...current.documents, [snapshot.page.id]: nextPage },
  };
}

export function activePublicContentPage(
  document: PublicContentDocument,
  requestedPageId?: string,
) {
  return document.documents[requestedPageId || document.rootPageId];
}

export function nextPublicContentRequest(
  document: PublicContentDocument,
  requestedPageId?: string,
  supplementalPageIds: readonly string[] = [],
): PublicContentWindowRequest | undefined {
  const primaryPageId = requestedPageId || document.rootPageId;
  const admittedPageIds = new Set(document.pages.map(({ id }) => id));
  const pageIds = [
    primaryPageId,
    ...supplementalPageIds.filter((pageId, index) => (
      pageId !== primaryPageId
      && admittedPageIds.has(pageId)
      && supplementalPageIds.indexOf(pageId) === index
    )),
  ];
  for (const pageId of pageIds) {
    const page = document.documents[pageId];
    if (!page) return { pageId, sourceRevision: document.sourceRevision };
    if (page.graphHasMore && page.graphNextCursor) {
      return {
        pageId,
        sourceRevision: document.sourceRevision,
        ...(document.pagesHasMore && document.pagesNextCursor
          ? { pagesCursor: document.pagesNextCursor }
          : {}),
        graphCursor: page.graphNextCursor,
      };
    }
  }
  if (document.pagesHasMore && document.pagesNextCursor) {
    return {
      pageId: primaryPageId,
      sourceRevision: document.sourceRevision,
      pagesCursor: document.pagesNextCursor,
    };
  }
  return undefined;
}
