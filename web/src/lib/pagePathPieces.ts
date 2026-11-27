import { dataSourceDisplayTitle, untitledDatabaseSourcesName, type DatabaseSourceTitle } from "@/components/editor/databaseTitles";
import type { Page } from "@/types";
import type { TextSpan } from "@/lib/types";
import { rememberInlineDatabaseAddress } from "./inlineDatabaseAddress";
import { pageHref } from "./navigation";

/**
 * The pieces of pages' paths this window has read from the server
 * (page_path_route.go), for the paths the reader's own page list can't draw:
 * above a database's page its database — a full-page one by its page, an
 * inline one by its block — and, while it holds several sources, the page's
 * source ("… / DB / 소스 / 행", Notion's, seen 2026-11-10), and the database
 * pages a page under one sits below. Each piece is a stand-in page linked to
 * the piece above it, so a path goes on from the reader's pages through
 * them.
 */

/** A piece as the server reads it, from the top down. */
export type PagePathPieceWire = {
  kind: "page" | "database" | "source";
  id: string;
  pageId: string;
  blockId?: string;
  viewId?: string;
  title: string;
  /** The title's spans as its reader reads them, while it holds a mention. */
  titleSpans?: TextSpan[];
  icon: string;
  iconType: string;
  teamspaceId?: string;
  row?: boolean;
  /** What a database's page without a title is called (page_path_route.go pagePathUntitledName). */
  untitledName?: string;
  archivedAt?: string;
  sourceTitles?: DatabaseSourceTitle[];
  /** A database template's page: the source it is a template of (page_path_route.go). */
  templateOf?: string;
};

/** What a piece's name and address are read from, as a workspace update's place carries it too. */
export type PagePathPlace = Pick<PagePathPieceWire, "kind" | "id" | "pageId" | "blockId" | "viewId" | "title" | "sourceTitles">;

const pieces = new Map<string, { page: Page; href: string }>();
/** The piece just above each page asked for, "" when none is the reader's. */
const above = new Map<string, string>();
/** Each template's piece: the source it is a template of and the database piece over it. */
const templatePieces = new Map<string, { dataSourceId: string; database?: PagePathPieceWire }>();
const listeners = new Set<() => void>();
let version = 0;

/** An inline database's piece: its block, apart from pages' IDs. */
export function inlineDatabasePieceId(blockId: string) {
  return `inline-database:${blockId}`;
}

function pieceId(wire: PagePathPieceWire) {
  if (wire.kind === "source") return `data-source:${wire.id}`;
  return wire.kind === "database" && wire.blockId ? inlineDatabasePieceId(wire.blockId) : wire.id;
}

/** Where a piece opens: a page at its address, a database or source at its view. */
export function pagePathPlaceHref(wire: PagePathPlace) {
  const view = wire.kind !== "page" && wire.viewId ? new URLSearchParams({ v: wire.viewId }) : undefined;
  if (wire.kind === "page") return pageHref(wire.id);
  if (!wire.blockId) return pageHref(wire.pageId, wire.kind === "source" ? view : undefined);
  rememberInlineDatabaseAddress(wire.blockId, wire.pageId);
  return pageHref(wire.blockId, view);
}

/**
 * A piece's name: a page's and a full-page database's their titles (an
 * untitled one named by its kind where drawn), an inline database's its first
 * source's or, while it holds several, its own, its sources' ("A 및 B")
 * while it has none (inlineDatabaseName), a source's its own ("새 데이터 소스"
 * untitled).
 */
export function pagePathPlaceTitle(wire: PagePathPlace) {
  if (wire.kind === "source") return dataSourceDisplayTitle(wire.title, 2);
  const titles = wire.sourceTitles ?? [];
  return wire.title || (wire.kind === "database" && titles.length > 0 ? untitledDatabaseSourcesName(titles) : "");
}

function piecePage(wire: PagePathPieceWire, id: string, parentId: string, workspaceId: string): Page {
  return {
    id, workspaceId, teamspaceId: wire.teamspaceId ?? "", title: pagePathPlaceTitle(wire), content: "", revision: 0, cursor: 0,
    // Its mentions as a plain title says them (closedMentions.ts).
    ...(wire.titleSpans?.length ? { titleSpans: wire.titleSpans } : {}),
    parentId, position: 0, isLocked: false, inTrash: false, ...(wire.archivedAt ? { archivedAt: wire.archivedAt } : {}),
    ...(wire.untitledName ? { untitledName: wire.untitledName } : {}),
    icon: wire.icon, iconType: wire.iconType || "none", cover: "", coverPosition: 0, font: "default", smallText: false, fullWidth: false,
    backlinksDisplay: "default", pageCommentsDisplay: "default", tableOfContentsDisplay: "default", inlineCommentsDisplay: "default",
    kind: wire.kind === "page" ? "page" : "database", createdAt: "", updatedAt: "", createdBy: "", lastEditedBy: "",
  };
}

/**
 * Keeps the paths the server read, by the page each is above, and by each
 * page on the way: a database page above a page under it is placed by its
 * path too, wherever a list holds it.
 */
export function rememberPagePaths(workspaceId: string, paths: Record<string, readonly PagePathPieceWire[]>) {
  for (const [pageId, wires] of Object.entries(paths)) {
    let parentId = "";
    let database: PagePathPieceWire | undefined;
    for (const wire of wires) {
      const id = pieceId(wire);
      pieces.set(id, { page: piecePage(wire, id, parentId, workspaceId), href: pagePathPlaceHref(wire) });
      if (wire.kind === "page") above.set(id, parentId);
      if (wire.kind === "database") database = wire;
      if (wire.templateOf) templatePieces.set(id, { dataSourceId: wire.templateOf, database });
      parentId = id;
    }
    above.set(pageId, parentId);
  }
  version += 1;
  for (const listener of listeners) listener();
}

/** A database's page as a piece, by the name and mark a list draws it with, under `parentId`. */
export function rowPiecePage(row: { id: string; title: string; icon?: string | null; iconType?: string | null }, parentId: string, workspaceId: string): Page {
  return piecePage({ kind: "page", id: row.id, pageId: row.id, title: row.title, icon: row.icon ?? "", iconType: row.iconType ?? "none", row: true }, row.id, parentId, workspaceId);
}

/**
 * Names a database's page drawn here — opened whole or in a peek, or a row
 * of a sidebar section a page is being made under — as a piece, for the
 * pages under it: such a page's path goes on up through it from its first
 * frame, before the server has read a path above that page. One just made
 * in its body, which the server had yet to make, drew only its own name
 * meanwhile, where Notion's draws "… / 행 / 새 페이지" at once (seen
 * 2026-11-18, two tabs), and the parent key went nowhere. The piece sits
 * under the one the server named above the page, else under `row.parentId`
 * as its drawer places it: its inline database (`database`, named with it
 * unless the server named that), the page it is a sub-item of, the page
 * holding its database. A path the server reads through the page names it
 * again (rememberPagePaths). Paths drawn are drawn again where the piece
 * sits elsewhere than it did; a new piece, and a piece's name and mark, are
 * read by the next drawing — no path drawn runs through a piece not yet
 * named.
 */
export function rememberDrawnRowPiece(row: Page, database?: { page: Page; href: string }) {
  const named = above.get(row.id);
  const parentId = named || row.parentId;
  const was = pieces.get(row.id)?.page.parentId;
  if (!named && database && !pieces.has(database.page.id)) pieces.set(database.page.id, database);
  pieces.set(row.id, { page: parentId === row.parentId ? row : { ...row, parentId }, href: pageHref(row.id) });
  if (was === undefined || was === parentId) return;
  version += 1;
  for (const listener of listeners) listener();
}

export function subscribePagePaths(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function pagePathsVersion() {
  return version;
}

/** A piece the server named, as a page. */
export function knownPathPage(id: string) {
  return pieces.get(id)?.page;
}

/** Where a piece the server named opens, where that isn't a page's own address. */
export function knownPathHref(id: string) {
  const href = pieces.get(id)?.href;
  return href && href !== pageHref(id) ? href : undefined;
}

/**
 * The database template a page sits under, by the path the server read
 * above it: the template's page, the source it is a template of and the
 * database over it — its name and mark as the path names them (a full-page
 * database's page's, an inline one's first source's) and the page holding
 * it. None for a page not under one, or whose path is unread.
 */
export function knownPathTemplate(pageId: string) {
  const seen = new Set<string>();
  for (let id = above.get(pageId); id && !seen.has(id); id = pieces.get(id)?.page.parentId || undefined) {
    seen.add(id);
    const template = templatePieces.get(id);
    if (!template) continue;
    const { database } = template;
    return {
      templateId: id, dataSourceId: template.dataSourceId,
      database: database ? { title: pagePathPlaceTitle(database), icon: database.icon, iconType: database.iconType || "none", pageId: database.pageId } : undefined,
    };
  }
  return undefined;
}

/** Forgets the path read above a page that has gone elsewhere, for it to be read again where it is now. */
export function forgetPagePath(pageId: string) {
  if (!above.delete(pageId)) return;
  version += 1;
  for (const listener of listeners) listener();
}

/** The piece just above a page the server was asked about: "" for none, undefined until asked. */
export function knownPathAboveId(pageId: string) {
  return above.get(pageId);
}

/**
 * The page above a page in its path: a database's piece the server named
 * above it — the database or source between a database page and the page
 * holding them, which no list holds — else the reader's own page above it,
 * else the piece the server named.
 */
export function pathParent(page: Pick<Page, "id" | "parentId">, local: (id: string) => Page | undefined): Page | undefined {
  // One of the reader's own pages at the top has nothing above it, whatever
  // a path read before it got there said: a database's page restored to
  // private pages draws there at once, as Notion's does.
  const mine = page.parentId ? undefined : local(page.id);
  if (mine && !mine.parentId) return undefined;
  const named = above.get(page.id);
  const piece = named ? pieces.get(named) : undefined;
  if (named && piece && piece.page.kind === "database" && !local(named)) return piece.page;
  const own = page.parentId ? local(page.parentId) : undefined;
  if (own) return own;
  const id = named ?? page.parentId;
  return id ? local(id) ?? pieces.get(id)?.page : undefined;
}

/** Whether a page's path goes past the reader's own pages: one above it isn't among them. */
export function pathLeavesOwnPages(page: Pick<Page, "id" | "parentId">, local: (id: string) => Page | undefined) {
  const seen = new Set<string>();
  for (let current: Pick<Page, "id" | "parentId"> | undefined = page; current?.parentId && !seen.has(current.id);) {
    seen.add(current.id);
    current = local(current.parentId);
    if (!current) return true;
  }
  return false;
}
