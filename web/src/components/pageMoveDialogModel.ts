import { positionBetween } from "@/lib/ids";
import { pagePlace, placeText, type Place } from "@/lib/pagePath";
import { pageNamesNow } from "@/lib/pageNames";
import type { Page } from "@/types";
import { foldNfcText } from "@/shared/database/natural-order.mjs";
import { pageTrashRoot } from "@/lib/pageTrash";
import { pageSearchRank } from "@/lib/pageSearchRank";
import { bodyOrderedChildren, childTreePages, privateRootPages, rootTreePages } from "./sidebar/pageTreeModel";
import { untitledDatabaseSourcesName } from "./editor/databaseTitles";
import type { InlineDatabaseProjection } from "@/workspaceMembershipWire.generated";
import { pageMapOf, pagesByIdOf } from "@/lib/pageIndex";

/** The top of a teamspace, or with teamspaceId "" of the reader's private pages. */
export type MovePlace = { teamspaceId: string; label: string; path: string; icon?: string };

/**
 * `inside` marks the moving page itself and the pages under it, which it
 * can't go into; a page's `where` is its place as its second line draws it,
 * `path` that place in words.
 */
export type MoveDestination =
  | { type: "place"; id: string; place: MovePlace; label: string; path: string; score: number; disabled?: boolean; disabledReason?: string }
  | {
    type: "page"; id: string; page: Page; label: string; path: string; where: Place; score: number; disabled?: boolean; disabledReason?: string;
    inside?: boolean;
  };

export const MOVE_SEARCH_DEBOUNCE_MS = 220;
/** A search's answers shown at a time, "더 불러오기" showing as many more (moveSearchWindow.tsx). */
export const MOVE_SEARCH_LIMIT = 20;
export const MOVE_REMOTE_PAGE_LIMIT = 64;
/**
 * Before a search, a move window suggests the pages the reader last moved
 * something into, then the pages they visited last, each newest first: of
 * the last ten visited, those it may go to, and ten pages in all, as
 * Notion's (seen 2026-11-10: seven left of ten once the moving page, the two
 * under it and its parent were left out; 2026-11-19 in two Chrome tabs: the
 * place a page was just moved into led the next move's suggestions — its
 * parent now —, a third place moved into put the tenth visited page out).
 */
export const MOVE_RECENT_LIMIT = 10;

/** The suggestions' pages (MOVE_RECENT_LIMIT): those moved into that `movable` allows, then those visited that `visitable` does. */
function suggestedPages(pagesById: Record<string, Page>, input: MoveSuggestionInput,
  movable: (page: Page) => boolean, visitable: (page: Page) => boolean = movable) {
  const pick = (ids: readonly string[], allowed: (page: Page) => boolean) => ids.flatMap((id) => pagesById[id] && allowed(pagesById[id]) ? [pagesById[id]] : []);
  const pages = [...pick(input.movedToPageIds ?? [], movable), ...pick((input.recentPageIds ?? []).slice(0, MOVE_RECENT_LIMIT), visitable)];
  return pages.filter((page, index) => pages.findIndex(({ id }) => id === page.id) === index).slice(0, MOVE_RECENT_LIMIT);
}

/** What a move window suggests from, read where it opens (moveSuggestions.ts), each newest first. */
export type MoveSuggestionInput = { recentPageIds?: readonly string[]; movedToPageIds?: readonly string[] };

export function pageMap(pages: Page[]) {
  return pagesByIdOf(pages);
}

export function mergeMoveRemotePages(
  current: Record<string, Page>,
  incoming: Page[],
  workspaceId: string,
) {
  const next = new Map(
    Object.values(current)
      .filter((page) => page.workspaceId === workspaceId)
      .map((page) => [page.id, page]),
  );
  for (const page of incoming) {
    if (!page?.id || page.workspaceId !== workspaceId) continue;
    next.delete(page.id);
    next.set(page.id, page);
  }
  while (next.size > MOVE_REMOTE_PAGE_LIMIT) {
    const oldest = next.keys().next().value as string | undefined;
    if (!oldest) break;
    next.delete(oldest);
  }
  return Object.fromEntries(next);
}

export function isInSubtree(
  pagesById: Record<string, Page>,
  rootId: string,
  maybeChildId: string,
) {
  if (rootId === maybeChildId) return true;
  let current = pagesById[maybeChildId];
  const guard = new Set<string>();
  while (current?.parentId && !guard.has(current.id)) {
    guard.add(current.id);
    if (current.parentId === rootId) return true;
    current = pagesById[current.parentId];
  }
  return false;
}

/**
 * Whether a page is no choice because a database moves: a database — one a
 * page's body holds, opened alone or among favorites, a full-page one's page
 * or its block — goes into no database. Notion's every "옮기기" dims them in
 * a search and leaves them out of its suggestions and tree (seen 2026-11-11).
 */
export function databaseRefused(page: Page, databaseMoving: boolean | undefined) {
  return Boolean(databaseMoving) && page.kind === "database";
}

export function siblingsFor(
  pagesById: Record<string, Page>,
  parentId: string,
  excludeId: string,
) {
  return Object.values(pagesById)
    .filter((page) => !page.inTrash && page.id !== excludeId && page.parentId === parentId)
    .sort((left, right) => left.position - right.position);
}

export function appendMovePosition(
  pagesById: Record<string, Page>,
  parentId: string,
  excludeId: string,
) {
  const siblings = siblingsFor(pagesById, parentId, excludeId);
  return positionBetween(siblings[siblings.length - 1]?.position, undefined);
}

/**
 * The order of a search's answers: how well each answers it, then, of those
 * answering equally well, the pages the reader visited last, newest first,
 * then the rest by name — as Notion's move window lists its equal answers
 * (seen 2026-11-18 in two Chrome tabs at 1440: four pages named alike, each
 * visited in turn, came first in the order they were visited last, newest
 * first, in the page's window and the block's alike).
 */
function bySearchAnswer(recentPageIds: readonly string[] | undefined) {
  const visited = new Map((recentPageIds ?? []).map((id, index) => [id, index]));
  const visit = (destination: MoveDestination) =>
    destination.type === "page" ? visited.get(destination.id) ?? Number.MAX_SAFE_INTEGER : Number.MAX_SAFE_INTEGER;
  return (left: MoveDestination, right: MoveDestination) =>
    left.score - right.score || visit(left) - visit(right) || left.label.localeCompare(right.label);
}

/** How well a place answers a search, by its name alone (pageSearchRank). */
export function scoreDestination(label: string, query: string, base: number) {
  if (!query) return base;
  return pageSearchRank(foldNfcText(label), query);
}

export function moveDestinations(input: MoveTreeInput & MoveSuggestionInput & {
  places: readonly MovePlace[];
  query: string;
}) {
  const { lockedParentLabel, movingPage, pagesById, places, query } = input;
  const workspaceId = moveWorkspace(input);
  const normalizedQuery = foldNfcText(query.trim());
  const sourceParentLocked = movingSourceParentLocked(input);
  // A page moves to the top of the reader's private pages or of a teamspace,
  // or under a page anywhere in the workspace. As Notion's list does, its
  // parent isn't offered — a page or, for a page at the top, the private
  // pages or teamspace it is at the top of (seen 2026-11-11: no "개인 페이지"
  // for a private page at the top) — but as a place it was just moved into
  // (suggestedPages); a search shows it, the pages under it, a locked page
  // and a database a database can't go into dimmed, which the suggestions
  // leave out.
  const tops: MoveDestination[] = places.filter((place) => !movingPlace(input, place)).map((place, index) => ({
    type: "place" as const,
    id: `place:${place.teamspaceId}`,
    place,
    label: place.label,
    path: place.path,
    score: scoreDestination(place.label, normalizedQuery, index),
    disabled: sourceParentLocked,
    disabledReason: sourceParentLocked ? lockedParentLabel : undefined,
  }));
  const offered = (page: Page) => !pageTrashRoot(page, (id) => pagesById[id])
    && page.workspaceId === workspaceId && page.id !== movingPage.parentId;
  if (!normalizedQuery) {
    const moving = [movingPage.id, ...(input.alsoMovingIds ?? [])];
    const movable = (page: Page) => !pageTrashRoot(page, (id) => pagesById[id]) && page.workspaceId === workspaceId && !page.isLocked
      && !databaseRefused(page, movingPage.kind === "database") && !moving.some((id) => isInSubtree(pagesById, id, page.id));
    const recent = suggestedPages(pagesById, input, movable, (page) => movable(page) && offered(page))
      .map((page, index) => pageDestination(page, input, places.length + index));
    return [...tops, ...recent].sort(byMoveGroup(false));
  }
  const destinations: MoveDestination[] = Object.values(pagesById)
    .filter(offered)
    .map((page, index) => pageDestination(
      page, input, scoreDestination(pageNamesNow.title(page), normalizedQuery, places.length + index),
    ));
  const answerOrder = bySearchAnswer(input.recentPageIds);
  return [...tops, ...destinations]
    .filter((destination) => destination.score < Number.POSITIVE_INFINITY)
    .sort((left, right) => byMoveGroup(true)(left, right) || answerOrder(left, right));
}

/**
 * The groups a move list's rows fall in, in their order, as Notion's (seen
 * 2026-11-13): "추천" — the private pages and, before a search, the pages
 * visited last —, "팀스페이스", and a search's "페이지".
 */
export type MoveGroup = "suggested" | "teamspaces" | "pages";
const MOVE_GROUP_ORDER: Record<MoveGroup, number> = { suggested: 0, teamspaces: 1, pages: 2 };

export function moveGroupOf(destination: MoveDestination, searching: boolean): MoveGroup {
  if (destination.type === "place") return destination.place.teamspaceId ? "teamspaces" : "suggested";
  return searching ? "pages" : "suggested";
}

/** Orders destinations by their groups, keeping the order within each. */
function byMoveGroup(searching: boolean) {
  return (left: MoveDestination, right: MoveDestination) =>
    MOVE_GROUP_ORDER[moveGroupOf(left, searching)] - MOVE_GROUP_ORDER[moveGroupOf(right, searching)];
}

type MoveTreeInput = {
  lockedParentLabel: string;
  movingPage: Page;
  /** The other pages moving with it, which a search dims too, and the tree leaves out, as the moving page. */
  alsoMovingIds?: readonly string[];
  pagesById: Record<string, Page>;
  workspaceId?: string;
  /** The pages the sidebar lists as shared, which the private pages' row leaves out (treePagesUnder). */
  sharedPageIds?: ReadonlySet<string>;
};

/**
 * The pages right under a row of a move window's tree, in the sidebar's
 * order by hand (pageTreeModel): under a place the pages at its top — the
 * private ones the sidebar's private section lists — and under a page its
 * sub-pages. One order whatever the sidebar's sections are sorted by, as
 * Notion's (seen 2026-11-10).
 */
function treePagesUnder(parent: MoveDestination, pagesById: Record<string, Page>, workspaceId: string, shared?: ReadonlySet<string>) {
  const pages = Object.values(pagesById).filter((page) => page.workspaceId === workspaceId);
  if (parent.type === "page") return childTreePages(pages, parent.id);
  const teamspaceId = parent.place.teamspaceId;
  return teamspaceId ? rootTreePages(pages).filter((page) => page.teamspaceId === teamspaceId) : privateRootPages(pages, shared ?? new Set());
}

/** The workspace the list shows: another one the page is copied to, or its own. */
function moveWorkspace(input: { movingPage: Page; workspaceId?: string }) {
  return input.workspaceId || input.movingPage.workspaceId;
}

/**
 * Whether a place is where the moving page is now: the private pages or the
 * teamspace whose top it is at, in its own workspace (a copy to another
 * workspace may go to that one's). A page shared with the reader is at the
 * top of someone else's private pages, not theirs.
 */
function movingPlace(input: MoveTreeInput, place: MovePlace) {
  const { movingPage } = input;
  return moveWorkspace(input) === movingPage.workspaceId && !movingPage.parentId
    && (movingPage.teamspaceId ?? "") === place.teamspaceId
    && (place.teamspaceId !== "" || !input.sharedPageIds?.has(movingPage.id));
}

/**
 * Whether the page's parent is locked, which keeps it there; a copy to
 * another workspace leaves the page where it is.
 */
function movingSourceParentLocked(input: MoveTreeInput) {
  const { movingPage, pagesById } = input;
  if (moveWorkspace(input) !== movingPage.workspaceId) return false;
  return Boolean(movingPage.parentId && pagesById[movingPage.parentId]?.isLocked);
}

/**
 * A page as a place to move to: dimmed when it is the moving page, under it,
 * locked, or a database the moving database can't go into (databaseRefused),
 * with no word of why, as Notion's search shows them (seen 2026-11-10); every
 * page is when the moving page's parent is locked.
 */
function pageDestination(page: Page, input: MoveTreeInput, score: number): MoveDestination {
  const { lockedParentLabel, movingPage, pagesById } = input;
  const sourceParentLocked = movingSourceParentLocked(input);
  const inside = [movingPage.id, ...(input.alsoMovingIds ?? [])].some((id) => isInSubtree(pagesById, id, page.id));
  const where = pagePlace(page, pagesById);
  return {
    type: "page",
    id: page.id,
    page,
    label: pageNamesNow.title(page),
    path: placeText(where),
    where,
    score,
    disabled: inside || sourceParentLocked || page.isLocked || databaseRefused(page, movingPage.kind === "database"),
    disabledReason: sourceParentLocked ? lockedParentLabel : undefined,
    inside,
  };
}

/**
 * The pages right under a suggestion, as Notion's move list opens it (seen
 * 2026-10-01): under a place its top pages, under a page its child pages, in
 * the sidebar's order by hand (treePagesUnder), without the pages moving, a
 * locked page (seen 2026-11-10) or, for a database, a database
 * (databaseRefused). The moving page's parent is one: chosen,
 * the page goes last in it, as Notion's.
 */
export function moveTreeChildren(parent: MoveDestination, input: MoveTreeInput): MoveDestination[] {
  const moving = new Set([input.movingPage.id, ...(input.alsoMovingIds ?? [])]);
  return treePagesUnder(parent, input.pagesById, moveWorkspace(input), input.sharedPageIds)
    .filter((page) => !moving.has(page.id) && !page.isLocked && !databaseRefused(page, input.movingPage.kind === "database"))
    .map((page) => pageDestination(page, input, 0));
}

/** A row of the move list: a place to choose, or an opened place's "no pages"; each under its suggestion's group. */
export type MoveListRow =
  | { kind: "option"; key: string; destination: MoveDestination; depth: number; expanded: boolean; group: MoveGroup }
  | { kind: "empty"; key: string; depth: number; group: MoveGroup };

/**
 * The move list's rows, a page's and a block's alike: what a search found as
 * it is; without one, the suggestions as a tree, each opened one followed by
 * the places under it, or "하위 페이지 없음", as Notion's (seen 2026-10-01;
 * a block's the same, 2026-11-10). A row's key is its place in the tree, its
 * group that of the suggestion it is under (MoveGroup).
 */
export function moveListRows(input: {
  destinations: readonly MoveDestination[];
  searching: boolean;
  expanded: ReadonlySet<string>;
  childrenOf: (destination: MoveDestination) => MoveDestination[];
}): MoveListRow[] {
  const { destinations, expanded, childrenOf } = input;
  if (input.searching) {
    return destinations.map((destination) => ({
      kind: "option", key: destination.id, destination, depth: 0, expanded: false, group: moveGroupOf(destination, true),
    }));
  }
  const out: MoveListRow[] = [];
  const visit = (destination: MoveDestination, depth: number, parentKey: string, group: MoveGroup) => {
    const key = `${parentKey}/${destination.id}`;
    const open = expanded.has(key);
    out.push({ kind: "option", key, destination, depth, expanded: open, group });
    if (!open) return;
    const children = childrenOf(destination);
    if (children.length === 0) out.push({ kind: "empty", key: `${key}/empty`, depth: depth + 1, group });
    for (const child of children) visit(child, depth + 1, key, group);
  };
  for (const destination of destinations) visit(destination, 0, "", moveGroupOf(destination, false));
  return out;
}

/**
 * Whether a move took a private page into a teamspace, to its top or under
 * one of its pages: Notion's notice then offers no "실행 취소" (seen
 * 2026-11-16 at 1440, by "옮기기" and by ⌘⇧P: to a teamspace it says the
 * place alone, under its page "이동" alone), where a teamspace's page moved
 * to another teamspace or to the private pages keeps it.
 */
export function movedOutOfPrivate(before: { teamspaceId?: string | null }, after: { teamspaceId?: string | null }) {
  return !before.teamspaceId && Boolean(after.teamspaceId);
}

/** The opened rows once a row's toggle is pressed. */
export function toggledMoveRow(expanded: ReadonlySet<string>, key: string): ReadonlySet<string> {
  const next = new Set(expanded);
  if (!next.delete(key)) next.add(key);
  return next;
}

/** Where a block goes (or what else a page is chosen for, as "페이지 추가"). */
type BlockMoveInput = {
  pagesById: Record<string, Page>;
  workspaceId: string;
  /** The page holding the block. */
  sourcePageId: string;
  /**
   * Whether that page is a choice: always (a page to add something to),
   * never ("페이지 위치"), or, for a block's move, in the tree and as a place
   * last moved into, not as one visited nor in a search, as Notion's (seen
   * 2026-11-22 in two Chrome tabs); chosen, the block goes last on it.
   */
  sourcePage?: "always" | "browsing" | "never";
  /** Why a page can't be chosen for this choice, if it can't. */
  unavailable: (page: Page) => string | undefined;
  /** Whether what moves is or holds a database, which no database takes (databaseRefused). */
  databaseMoving?: boolean;
  /**
   * The pages moving with the blocks, the sub-pages among them or in them:
   * as a page's own move (pageDestination), they and the pages under them
   * are dimmed in a search and left out of the suggestions and the tree (seen
   * 2026-11-19, a page block's "옮기기").
   */
  movingPageIds?: readonly string[];
  /** Where a page is, as its second line says once searched. */
  location: (page: Page) => Place;
  /** The pages the sidebar lists as shared, which the private pages' row leaves out (treePagesUnder). */
  sharedPageIds?: ReadonlySet<string>;
  /** The databases pages' bodies hold, which a page in the tree opens to beside its sub-pages, when they are choices too. */
  inlineDatabases?: readonly InlineDatabaseProjection[];
};

/**
 * A database a page's body holds, as a choice: found by its database's ID,
 * as a search finds it, under the page holding it, an untitled one called
 * as the sidebar calls it — "새 데이터베이스", or by its sources' names.
 */
export function inlineDatabaseChoice(database: InlineDatabaseProjection): Page {
  const named = database.sourceTitles && database.sourceTitles.length > 1 ? untitledDatabaseSourcesName(database.sourceTitles) : undefined;
  return {
    id: database.containerId, workspaceId: database.workspaceId, teamspaceId: "", title: database.title, content: "", revision: 0, cursor: 0,
    ...(database.titleSpans ? { titleSpans: database.titleSpans as Page["titleSpans"] } : {}), ...(named ? { untitledName: named } : {}),
    parentId: database.pageId, position: 0, isLocked: false, inTrash: false,
    icon: database.icon ?? "", iconType: database.iconType || "none", cover: "", coverPosition: 0, font: "default", smallText: false, fullWidth: false,
    backlinksDisplay: "default", pageCommentsDisplay: "default", tableOfContentsDisplay: "default", inlineCommentsDisplay: "default",
    kind: "database", createdAt: "", updatedAt: "", createdBy: "", lastEditedBy: "",
  };
}

/**
 * A page to choose; a locked one, a database a database can't go into, or one
 * moving with the blocks (movingWithBlocks) is dimmed with no word of why, as
 * Notion's search shows it (seen 2026-11-10, 2026-11-11, 2026-11-19).
 */
function blockPageDestination(page: Page, input: BlockMoveInput, score: number): MoveDestination {
  const reason = input.unavailable(page);
  const where = input.location(page);
  const inside = movingWithBlocks(page, input);
  return {
    type: "page", id: page.id, page, label: pageNamesNow.title(page), path: placeText(where), where, score, inside,
    disabled: inside || Boolean(reason) || Boolean(page.isLocked) || databaseRefused(page, input.databaseMoving), disabledReason: reason,
  };
}

/** Whether a page moves with the blocks, or is under one that does (movingPageIds). */
function movingWithBlocks(page: Page, input: BlockMoveInput) {
  return (input.movingPageIds ?? []).some((id) => isInSubtree(input.pagesById, id, page.id));
}

function blockMoveChoice(page: Page, input: BlockMoveInput, searching: boolean) {
  const own = input.sourcePage ?? "browsing";
  return !pageTrashRoot(page, (id) => input.pagesById[id]) && page.workspaceId === input.workspaceId
    && (page.id !== input.sourcePageId || own === "always" || (own === "browsing" && !searching));
}

/**
 * A page the suggestions and the tree may show: a choice, not locked, not a
 * database a database can't go into, and not moving with the blocks, which
 * Notion's leave out (seen 2026-11-10, 2026-11-11, 2026-11-19).
 */
function blockMoveBrowsable(page: Page, input: BlockMoveInput) {
  return blockMoveChoice(page, input, false) && !page.isLocked && !databaseRefused(page, input.databaseMoving)
    && !movingWithBlocks(page, input);
}

/**
 * A block's move list, as a page's (moveDestinations): before a search the
 * reader's private pages, the pages last moved into and visited (as sourcePage
 * says), the teamspaces (MoveGroup), each opening to the pages in it; a search finds pages alone, all of them (moveSearchWindow.tsx shows them).
 * With sourceFirst its own page leads the suggestions.
 */
export function blockMoveDestinations(input: BlockMoveInput & MoveSuggestionInput & {
  places: readonly MovePlace[]; query: string; sourceFirst?: boolean;
}) {
  const { pagesById, places, query, sourcePageId } = input;
  const normalizedQuery = foldNfcText(query.trim());
  if (!normalizedQuery) {
    const tops: MoveDestination[] = places.map((place, index) => ({
      type: "place" as const, id: `place:${place.teamspaceId}`, place, label: place.label, path: place.path, score: index,
    }));
    const source = input.sourceFirst ? pagesById[sourcePageId] : undefined;
    const pages = [
      ...(source && blockMoveChoice(source, input, false) ? [source] : []),
      ...suggestedPages(pagesById, input, (page) => page !== source && blockMoveBrowsable(page, input),
        (page) => page !== source && page.id !== sourcePageId && blockMoveBrowsable(page, input)),
    ];
    return [...tops, ...pages.map((page, index) => blockPageDestination(page, input, tops.length + index))].sort(byMoveGroup(false));
  }
  return Object.values(pagesById)
    .filter((page) => blockMoveChoice(page, input, true))
    .map((page, index) => blockPageDestination(
      page, input, scoreDestination(pageNamesNow.title(page), normalizedQuery, index),
    ))
    .filter((destination) => destination.score < Number.POSITIVE_INFINITY)
    .sort(bySearchAnswer(input.recentPageIds));
}

/**
 * The pages under a block's suggestion, as moveTreeChildren's for a page;
 * with inlineDatabases, a page's also the databases its body holds, in the
 * body's order among its sub-pages, as Notion's "다음으로 이동:" opens a page
 * (seen 2026-11-11: a database, an untitled one as "새 데이터베이스", then a
 * sub-page, each database with no toggle of its own) — none while a
 * database moves (databaseRefused).
 */
export function blockMoveTreeChildren(parent: MoveDestination, input: BlockMoveInput): MoveDestination[] {
  const pages = treePagesUnder(parent, input.pagesById, input.workspaceId, input.sharedPageIds)
    .filter((page) => blockMoveBrowsable(page, input));
  const held = parent.type === "page" && input.inlineDatabases && !input.databaseMoving
    ? input.inlineDatabases.filter((database) => database.pageId === parent.id && !database.viewBlock) : [];
  if (held.length === 0) return pages.map((page) => blockPageDestination(page, input, 0));
  const byId = pageMapOf(pages);
  return bodyOrderedChildren(pages.map(({ id }) => id), held).map((child) => blockPageDestination(
    child.kind === "page" ? byId.get(child.id)! : inlineDatabaseChoice(child.database), input, 0));
}
