import { useEffect, useMemo, useState, type ReactNode } from "react";
import type { PagePresentationChange } from "@/components/page/pagePresentation";
import { PageRenamePopover } from "@/components/page/PageRenamePopover";
import { TopBarBreadcrumbs } from "@/components/page/TopBarBreadcrumbs";
import { TopBarTeamspaceCrumb } from "@/components/page/TopBarTeamspaceCrumb";
import { styles as topBarStyles } from "@/components/page/topBarStyles";
import { useProductPages } from "@/components/product/productPagesContext";
import type { DatabaseRowPageRow } from "@/databaseRowPageWire.generated";
import { inlineDatabaseOpenedAlone } from "@/lib/inlineDatabaseFullPage";
import { inlineDatabasePieceId, knownPathAboveId, rememberDrawnRowPiece } from "@/lib/pagePathPieces";
import { pagePathUnread, usePagePathsAbove } from "@/lib/usePagePathsAbove";
import { routeInfoFromPath } from "@/lib/router";
import type { Page } from "@/types";
import type { TextSpan } from "@/lib/types";
import { untitledPageDisplayTitle } from "../../lib/pageTitle";
import { titleWithClosedMentions } from "@/lib/closedMentions";

/** A database page as the last piece of a path, under the page its database lives on; as a page, its menu's too. */
export function rowPathPage(row: DatabaseRowPageRow, title: string, workspaceId: string): Page {
  return {
    id: row.id, workspaceId, teamspaceId: "", title, content: "", revision: 0, cursor: 0,
    parentId: row.containerPageId, position: 0, isLocked: row.locked, inTrash: Boolean(row.trashedAt),
    ...(row.archivedAt ? { archivedAt: row.archivedAt } : {}),
    icon: row.icon, iconType: row.iconType, cover: row.cover, coverPosition: row.coverPosition,
    font: row.font, smallText: row.smallText, fullWidth: row.fullWidth,
    tableOfContentsDisplay: row.tableOfContentsDisplay === "off" ? "off" : "default", inlineCommentsDisplay: "default",
    backlinksDisplay: "default", pageCommentsDisplay: "default", kind: "page",
    createdAt: row.createdAt, updatedAt: row.updatedAt, createdBy: row.createdBy, lastEditedBy: row.lastEditedBy,
  };
}

/** An inline database, as a piece of its pages' path. */
export type RowPathDatabase = {
  blockId: string;
  title: string;
  /** Its name's spans, which say its mentions (closedMentions.ts). */
  titleSpans?: readonly TextSpan[];
  icon: Pick<Page, "icon" | "iconType">;
  /** The database opened alone as a page. */
  href: string;
};

/** The inline database's piece: under the page holding it, over its pages. */
function databasePathPage(database: RowPathDatabase, row: DatabaseRowPageRow, workspaceId: string): Page {
  return {
    ...rowPathPage(row, database.title, workspaceId),
    ...(database.titleSpans ? { titleSpans: [...database.titleSpans] } : {}),
    id: inlineDatabasePieceId(database.blockId), parentId: row.containerPageId, kind: "database",
    icon: database.icon.icon, iconType: database.icon.iconType, cover: "", isLocked: false, inTrash: false,
  };
}

/** The page's stored title and the spans spelling it, for its piece to draw their custom emoji. */
type RowSpelledTitle = { title: string; titleSpans?: readonly TextSpan[] };

/**
 * A database page drawn here as its path's piece, under the piece the
 * server named above it, else its inline database's piece, the page it is
 * a sub-item of or the page holding its database; and its inline database's
 * piece. While the workspace's pages are here it names itself so for the
 * pages under it (pagePathPieces.ts rememberDrawnRowPiece), each time its
 * name, its mark or its place changes.
 */
function useRowPathPiece({ database, pages, row, spelled, title, untitled, workspaceId }: {
  database?: RowPathDatabase; pages?: readonly Page[]; row: DatabaseRowPageRow; spelled?: RowSpelledTitle; title: string; untitled?: string; workspaceId: string;
}) {
  const databasePage = useMemo(() => database ? databasePathPage(database, row, workspaceId) : null, [database, row, workspaceId]);
  // A wiki's page under another of its pages sits under that page in its
  // path, as Notion's (seen 2026-10-29: "… / Sub one / 새 데이터베이스"); a
  // database's own sub-items aren't among the workspace's pages.
  const above = row.subitemParentId && pages?.some(({ id }) => id === row.subitemParentId) ? row.subitemParentId : null;
  const named = knownPathAboveId(row.id);
  // Its piece draws its title's custom emoji by its spans while they spell the title shown (TitleWithCustomEmoji).
  const spelledTitle = spelled?.titleSpans?.length && titleWithClosedMentions(spelled.title, spelled.titleSpans) === title
    ? JSON.stringify({ title: spelled.title, titleSpans: spelled.titleSpans }) : "";
  // While its title is blank — its field emptied as it is typed in too — it goes by its database's name for its untitled pages, as its title's place does (untitledRowPageName).
  const page = useMemo(() => ({
    ...rowPathPage(row, title, workspaceId),
    ...(spelledTitle ? JSON.parse(spelledTitle) as { title: string; titleSpans: TextSpan[] } : {}),
    ...(named ? { parentId: named } : databasePage ? { parentId: databasePage.id } : above ? { parentId: above } : {}),
    ...(untitled ? { untitledName: untitled } : {}),
  }), [above, databasePage, named, row, spelledTitle, title, untitled, workspaceId]);
  const listed = Boolean(pages), databaseHref = database?.href;
  useEffect(() => {
    if (listed) rememberDrawnRowPiece(page, databasePage && databaseHref ? { page: databasePage, href: databaseHref } : undefined);
  }, [databaseHref, databasePage, listed, page]);
  return { databasePage, named, page };
}

const NO_PAGES: readonly [] = [];

/**
 * A database page drawn in a peek, which draws no path, names itself as a
 * piece all the same (useRowPathPiece): a page made in its body draws its
 * path through it once it shows whole — opened so from a center peek, or
 * from the side peek it opens in (pagePeek.ts hrefOfPageMadeIn).
 */
export function RowPathPiece(props: { database?: RowPathDatabase; row: DatabaseRowPageRow; spelled?: RowSpelledTitle; title: string; untitled?: string; workspaceId: string }) {
  const pages = useProductPages()?.pages;
  // The paths read meanwhile place it again.
  usePagePathsAbove(NO_PAGES);
  useRowPathPiece({ ...props, pages });
  return null;
}

/**
 * A database page opened as a full page shows its path, as Notion's does:
 * the pages above the page its database lives on, that page, an inline
 * database (seen 2026-10-04: "Page / Tasks / Alpha"), the page's source
 * while its database holds several (seen 2026-11-10: "… / DB / 소스 / 행",
 * a full-page database's and an inline one's alike), and the database page.
 * The server names the pieces (pagePathPieces.ts): a page whose path it has
 * never named waits for it, its path drawn once whole as Notion's is at once
 * (seen 2026-11-11: never the page and the row without the database between),
 * and one it named before draws that while it reads it again. Without the
 * server the page draws the path its copy here keeps (usePagePathsAbove.ts),
 * as it drew it with the server; a read that fails otherwise, or a page kept
 * without one, draws the path from the reader's pages and the inline
 * database given. The database's piece returns to the database the page was opened over; the
 * page's own renames it, as Notion's (seen 2026-10-27). Without the
 * workspace's pages, as on a public page, or when neither the database's
 * page is among them nor the server names a piece above, it offers the way
 * back instead.
 */
export function RowPagePath({ alone, currentOnly, database, fallback, onReturn, rename, row, spelled, title, trailing, untitled, workspaceId }: {
  /**
   * One of the page's own databases opened alone (RowPageDatabaseAlone):
   * the path's last piece, after the page, which then opens the page.
   */
  alone?: { blockId: string; title: string; titleSpans?: readonly TextSpan[]; icon: Pick<Page, "icon" | "iconType"> };
  /** Only the page's own piece, which opens the path, as on a phone. */
  currentOnly?: boolean;
  /** The inline database the page is in; none for a full-page database. */
  database?: RowPathDatabase;
  fallback: ReactNode;
  /** Returns to the database the page is open over. */
  onReturn(): void;
  /** Renames the page from the rename popover, where its title can change. */
  rename?: {
    title: string;
    /** The title's spans as its reader reads them: its mentions, which the popover shows and keeps. */
    titleSpans?: readonly TextSpan[];
    onTitleChange(title: string, spans?: TextSpan[]): void;
    onPresentationChange?(change: PagePresentationChange): void;
  };
  row: DatabaseRowPageRow;
  spelled?: RowSpelledTitle;
  title: string;
  /** What follows the path in its place, or the fallback in its stead: a database alone's lock (RowPageDatabaseAlone), a full database page's (DatabaseRowPage). */
  trailing?: ReactNode;
  /** What the page is called while its title is blank (untitledRowPageName). */
  untitled?: string;
  workspaceId: string;
}) {
  const productPages = useProductPages();
  const pages = productPages?.pages;
  const [renameAt, setRenameAt] = useState<{ top: number; left: number } | null>(null);
  // A page moved to another database or page reads its path again at once.
  const place = `${row.containerPageId}/${row.dataSourceId}/${row.subitemParentId ?? ""}`;
  usePagePathsAbove(pages ? [{ id: row.id, workspaceId, place }] : [], { fresh: true });
  const { databasePage, named, page } = useRowPathPiece({ database, pages, row, spelled, title, untitled, workspaceId });
  const alonePage = useMemo(() => alone ? {
    ...rowPathPage(row, alone.title, workspaceId), ...(alone.titleSpans ? { titleSpans: [...alone.titleSpans] } : {}),
    id: alone.blockId, parentId: row.id, kind: "database" as const, icon: alone.icon.icon, iconType: alone.icon.iconType,
    cover: "", isLocked: false, inTrash: false,
  } : null, [alone, row, workspaceId]);
  const pathPages = useMemo(() => {
    const listed = pages && databasePage ? [...pages, databasePage] : pages;
    return listed && alonePage ? [...listed, page] : listed;
  }, [alonePage, databasePage, page, pages]);
  if (pathPages && productPages?.session && !currentOnly && pagePathUnread(row.id)) {
    return <div className={topBarStyles.left} data-row-page-path data-path-pending />;
  }
  if (!pathPages || (!named && !pathPages.some(({ id }) => id === row.containerPageId))) return <>{fallback}{trailing}</>;
  return (
    <div className={topBarStyles.left} data-row-page-path>
      {productPages?.session && !currentOnly ? (
        <TopBarTeamspaceCrumb page={page} pages={pathPages} session={productPages.session} />
      ) : null}
      <TopBarBreadcrumbs page={alonePage ?? page} pages={pathPages} currentOnly={currentOnly}
        onRenameCurrent={rename && !alonePage ? setRenameAt : undefined}
        hrefOf={(pageId) => pageId === databasePage?.id ? database?.href : undefined}
        onOpenPage={(pageId) => {
          // Under a database of its own opened alone, the page's piece opens the page.
          if (pageId === row.id || pageId === alonePage?.id) return pageId !== row.id || !alonePage;
          const route = routeInfoFromPath(window.location.pathname);
          // The database's piece goes to the database opened alone, unless that is where the page is open.
          if (pageId === databasePage?.id) {
            if (!database || inlineDatabaseOpenedAlone() !== database.blockId) return false;
            onReturn();
            return true;
          }
          if (pageId !== row.containerPageId || route.kind !== "page" || route.pageId !== pageId) return false;
          onReturn();
          return true;
        }} />
      {rename && renameAt ? (
        <PageRenamePopover page={page} title={rename.title} titleSpans={rename.titleSpans} untitled={untitledPageDisplayTitle(page)} at={renameAt}
          onTitleChange={rename.onTitleChange} onPresentationChange={rename.onPresentationChange} onClose={() => setRenameAt(null)} />
      ) : null}
      {trailing}
    </div>
  );
}
