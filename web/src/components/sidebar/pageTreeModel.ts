import { positionBetween } from "@/lib/ids";
import type { Page } from "@/types";
import { inlineStartX } from "@/lib/readingDirection";
import { pageOutsideList, pagesOutsideListChanges } from "@/lib/pagesOutsideList";
import { listEditsOf, pageMapOf, pagesByIdOf } from "@/lib/pageIndex";

export type PageTreeDropIntent = "before" | "inside" | "after";

export type PageDropPlan = {
  parentId: string;
  position: number;
  /** At the top of another place: the target's teamspace, or "" for private pages. */
  teamspaceId?: string;
};

function stablePageOrder(left: Page, right: Page) {
  return left.position - right.position || left.id.localeCompare(right.id);
}

// A page in a button's draft belongs to the button's template, which
// Notion keeps out of the sidebar (seen 2026-10-10); an archived page leaves
// it too, with the pages under it (seen 2026-10-11), and so does one hanging
// on a table's cell or a caption, though one on a line or the title shows
// (seen 2026-11-13), and one hanging on a line that no longer mentions it
// until the mention is back (seen 2026-11-18,
// mention_anchored_unmentioned.go). One opened though the list lacks it
// stands in no section (pagesOutsideList.ts). The library lists archived
// pages too while its "보관된 페이지 포함" is on (withArchived).
function inPageTree(page: Page, withArchived = false) {
  return !page.inTrash && !page.buttonTemplate && (withArchived || !page.archivedAt) && !page.fieldAnchored
    && !page.anchorUnmentioned && !pageOutsideList(page.id);
}

/**
 * Where a row's "⋯" menu opens, as Notion's sidebar opens it (seen
 * 2026-10-31): from where the button was pressed (lib/pressedPoint), or on the
 * button's top left when a key pressed it — its top right where the screen
 * reads from the right (placeAtPoint).
 */
export function treeMenuPoint(event: { clientX: number; clientY: number; detail: number; currentTarget: Element }) {
  if (event.detail > 0) return { x: event.clientX, y: event.clientY, pressed: true };
  const rect = event.currentTarget.getBoundingClientRect();
  return { x: inlineStartX(rect, 0, event.currentTarget), y: rect.top };
}

export function pageTreeMap(pages: readonly Page[]) {
  return pagesByIdOf(pages);
}

// A list's top pages, worked out once for the list (and whether the archived
// ones count): the sidebar's sections and the library's views ask for them
// as they draw, each over every page of the list.
const treeRoots = [new WeakMap<readonly Page[], readonly Page[]>(), new WeakMap<readonly Page[], readonly Page[]>()];

// The list each was last worked out for, with its result and the pages kept
// outside lists then: a list made from it by new records of its pages that
// stay where they were (sameTreePlace) takes that result with those records.
type LastTree<R> = { pages: readonly Page[]; result: R; outside: number };
const lastRoots: Array<LastTree<readonly Page[]> | null> = [null, null];

/**
 * Whether a page's new record stands where the old one did in the tree: the
 * same parent and place among its siblings, in it or out of it alike.
 */
function sameTreePlace(was: Page, now: Page, withArchived = false) {
  return was.parentId === now.parentId && was.position === now.position
    && inPageTree(was, withArchived) === inPageTree(now, withArchived);
}

/** What changed in the tree from the last list: new records in their places, and pages leaving and entering places. */
type TreeEdits = { stay: ReadonlyMap<string, Page>; leave: readonly Page[]; enter: readonly Page[] };

/**
 * How the tree changed from the last list to `pages` when `pages` is the last
 * list with a few pages replaced, left out or added at its end (listEditsOf):
 * the new records standing where the old ones did, the old records of pages
 * that leave their places (left out, moved, out of the tree now) and the new
 * records of pages that enter places. Null when the last result does not
 * stand for it (2026-11-15: a page put in the trash elsewhere left the
 * sidebar's 47,000-page list one page shorter, and its tree was made again
 * from every page).
 */
function treeEdits<R>(last: LastTree<R> | null, pages: readonly Page[], withArchived = false): TreeEdits | null {
  if (!last || last.outside !== pagesOutsideListChanges()) return null;
  const edits = listEditsOf(last.pages, pages);
  if (!edits) return null;
  const stay = new Map<string, Page>(), leave: Page[] = [], enter: Page[] = [];
  for (const page of edits.removed) if (inPageTree(page, withArchived)) leave.push(page);
  for (const [was, now] of edits.replaced) {
    const before = last.pages[was], after = pages[now];
    if (sameTreePlace(before, after, withArchived)) {
      stay.set(after.id, after);
      continue;
    }
    if (inPageTree(before, withArchived)) leave.push(before);
    if (inPageTree(after, withArchived)) enter.push(after);
  }
  for (const now of edits.added) if (inPageTree(pages[now], withArchived)) enter.push(pages[now]);
  return { stay, leave, enter };
}

/** Whether the tree's edits change nothing. */
function noTreeEdits({ stay, leave, enter }: TreeEdits) {
  return stay.size === 0 && leave.length === 0 && enter.length === 0;
}

/**
 * A list of sibling pages (or of top pages) after the tree's edits: those
 * leaving gone, new records in their places, those entering (`entering`) put
 * in the tree's order; the very same list when none of them is in it.
 */
function editedSiblings(siblings: readonly Page[], edits: TreeEdits, entering: readonly Page[]): readonly Page[] {
  const leaving = edits.leave.length ? new Set(edits.leave.map(({ id }) => id)) : null;
  let changed = entering.length > 0;
  const kept: Page[] = [];
  for (const page of siblings) {
    if (leaving?.has(page.id)) {
      changed = true;
      continue;
    }
    const now = edits.stay.get(page.id);
    if (now) changed = true;
    kept.push(now ?? page);
  }
  if (!changed) return siblings;
  if (entering.length) {
    kept.push(...entering);
    kept.sort(stablePageOrder);
  }
  return kept;
}

export function rootTreePages(pages: readonly Page[], withArchived = false): readonly Page[] {
  const kept = treeRoots[withArchived ? 1 : 0];
  let roots = kept.get(pages);
  if (!roots) {
    const last = lastRoots[withArchived ? 1 : 0];
    const edits = treeEdits(last, pages, withArchived);
    roots = last && edits
      ? (noTreeEdits(edits) ? last.result : editedSiblings(last.result, edits, edits.enter.filter((page) => !page.parentId)))
      : pages.filter((page) => inPageTree(page, withArchived) && !page.parentId).sort(stablePageOrder);
    kept.set(pages, roots);
    lastRoots[withArchived ? 1 : 0] = { pages, result: roots, outside: pagesOutsideListChanges() };
  }
  return roots;
}

/**
 * The reader's private pages at the top, in the order they are put in by
 * hand: those the sidebar's private section lists, the shared ones left to
 * the shared section. A move window's tree lists them so whatever the
 * section's sort, as Notion's (seen 2026-11-10: by last edit in the
 * sidebar, by hand in the window, as the sidebar is once set to "수동").
 */
export function privateRootPages(pages: readonly Page[], sharedPageIds: ReadonlySet<string>) {
  return privateRootsOf(rootTreePages(pages), sharedPageIds);
}

/** privateRootPages from the tree's top pages (rootTreePages) already worked out. */
export function privateRootsOf(roots: readonly Page[], sharedPageIds: ReadonlySet<string>) {
  return roots.filter((page) => !page.teamspaceId && !sharedPageIds.has(page.id));
}

// A shared page heads the Shared section when the reader sees no page
// above it; the pages under it follow it there.
const NO_SHARED: readonly Page[] = [];
let lastShared: (LastTree<readonly Page[]> & { shared: ReadonlySet<string>; withArchived: boolean }) | null = null;

export function sharedTreePages(pages: readonly Page[], sharedPageIds: ReadonlySet<string>, withArchived = false): readonly Page[] {
  const last = lastShared?.shared === sharedPageIds && lastShared.withArchived === withArchived ? lastShared : null;
  if (last?.pages === pages) return last.result;
  const edits = treeEdits(last, pages, withArchived);
  let result: readonly Page[];
  // A page leaving or entering may change which shared pages head the section: it is worked out again.
  if (last && edits && edits.leave.length === 0 && edits.enter.length === 0) {
    result = edits.stay.size ? last.result.map((page) => edits.stay.get(page.id) ?? page) : last.result;
  } else if (sharedPageIds.size === 0) {
    result = NO_SHARED;
  } else {
    const visible = pageMapOf(pages);
    result = pages
      .filter((page) => (
        inPageTree(page, withArchived) && sharedPageIds.has(page.id) && (!page.parentId || !visible.has(page.parentId))
      ))
      .sort(stablePageOrder);
  }
  lastShared = { pages, result, outside: pagesOutsideListChanges(), shared: sharedPageIds, withArchived };
  return result;
}

/**
 * The Shared section's heads: the shared pages it lists, less those everyone
 * in the workspace can open, which it leaves to search — the sidebar and a
 * path piece's menu list the same ones.
 */
export function sharedSectionPages(pages: readonly Page[], sharedPageIds: ReadonlySet<string>, workspaceWidePageIds: readonly string[] = []) {
  return sharedTreePages(pages, sharedPageIds).filter((page) => !workspaceWidePageIds.includes(page.id));
}

const NO_PAGES: readonly Page[] = [];
// Each page list's pages under each page, in the tree's order, made once per
// list: a sidebar row reads its own, as do the rows beside it, instead of each
// row going through the whole list on every render. A list is not changed in
// place, so it names what is under each page for as long as it is used.
const childrenOfList = new WeakMap<readonly Page[], ReadonlyMap<string, readonly Page[]>>();

let lastChildren: LastTree<ReadonlyMap<string, readonly Page[]>> | null = null;

function treeChildren(pages: readonly Page[]) {
  const known = childrenOfList.get(pages);
  if (known) return known;
  const edits = treeEdits(lastChildren, pages);
  if (lastChildren && edits) {
    // Only the sibling lists a page left, entered or holds a new record in are
    // made again; the rest are the same lists, so the rows that read them
    // aren't drawn again.
    let patched = lastChildren.result;
    if (!noTreeEdits(edits)) {
      const next = new Map(lastChildren.result);
      const parents = new Set<string>();
      for (const page of edits.stay.values()) parents.add(page.parentId);
      for (const page of edits.leave) parents.add(page.parentId);
      for (const page of edits.enter) parents.add(page.parentId);
      for (const parentId of parents) {
        const siblings = lastChildren.result.get(parentId) ?? NO_PAGES;
        const edited = editedSiblings(siblings, edits, edits.enter.filter((page) => page.parentId === parentId));
        if (edited === siblings) continue;
        if (edited.length) next.set(parentId, edited);
        else next.delete(parentId);
      }
      patched = next;
    }
    childrenOfList.set(pages, patched);
    lastChildren = { pages, result: patched, outside: pagesOutsideListChanges() };
    return patched;
  }
  const children = new Map<string, Page[]>();
  for (const page of pages) {
    if (!inPageTree(page)) continue;
    const siblings = children.get(page.parentId);
    if (siblings) siblings.push(page);
    else children.set(page.parentId, [page]);
  }
  for (const siblings of children.values()) siblings.sort(stablePageOrder);
  childrenOfList.set(pages, children);
  lastChildren = { pages, result: children, outside: pagesOutsideListChanges() };
  return children;
}

/** The pages under a page in the tree, in its order; shared by every reader of the same list. */
export function childTreePages(pages: readonly Page[], parentId: string): readonly Page[] {
  return treeChildren(pages).get(parentId) ?? NO_PAGES;
}

/**
 * A page's sub-pages and the databases its body holds, in the body's order,
 * as Notion's sidebar lists them (seen 2026-11-07): each database after the
 * sub-page whose block comes last before it (afterPageId), those before
 * every sub-page first, one whose sub-page isn't listed last.
 */
export function bodyOrderedChildren<D extends { afterPageId?: string }>(childIds: readonly string[], databases: readonly D[]) {
  const listed = new Set(childIds);
  const ordered: Array<{ kind: "page"; id: string } | { kind: "database"; database: D }> = [];
  const databasesAfter = (pageId: string) => databases.filter((database) => (database.afterPageId ?? "") === pageId)
    .forEach((database) => ordered.push({ kind: "database", database }));
  databasesAfter("");
  for (const id of childIds) {
    ordered.push({ kind: "page", id });
    databasesAfter(id);
  }
  databases.filter((database) => database.afterPageId && !listed.has(database.afterPageId))
    .forEach((database) => ordered.push({ kind: "database", database }));
  return ordered;
}

function isPageInSubtree(
  pagesById: Record<string, Page>,
  rootId: string,
  maybeChildId: string,
) {
  if (rootId === maybeChildId) return true;
  let current = pagesById[maybeChildId];
  const visited = new Set<string>();
  while (current?.parentId && !visited.has(current.id)) {
    visited.add(current.id);
    if (current.parentId === rootId) return true;
    current = pagesById[current.parentId];
  }
  return false;
}

export function pageTreeDropIntent(clientY: number, top: number, height: number): PageTreeDropIntent {
  const y = clientY - top;
  if (y < height * 0.28) return "before";
  if (y > height * 0.72) return "after";
  return "inside";
}

// A page drops into or beside a page of any section; beside a page at the
// top it takes that page's place, except beside a shared page, whose place
// is not the reader's. Nothing drops into a locked page. A locked page, or a
// page under one, doesn't move, but may be copied, as the server duplicates
// it.
export function pageDropPlan({
  copy = false,
  draggedPageId,
  intent,
  pages,
  targetPageId,
  targetSharedRoot = false,
  canEditPage,
  canCreateRoot,
}: {
  copy?: boolean;
  draggedPageId: string;
  intent: PageTreeDropIntent;
  pages: readonly Page[];
  targetPageId: string;
  targetSharedRoot?: boolean;
  canEditPage(pageId: string): boolean;
  canCreateRoot: boolean;
}): PageDropPlan | null {
  const pagesById = pageTreeMap(pages);
  const dragged = pagesById[draggedPageId];
  const target = pagesById[targetPageId];
  if (
    !dragged
    || !target
    || dragged.workspaceId !== target.workspaceId
    || (!copy && dragged.isLocked)
    || (!copy && !canEditPage(dragged.id))
    || isPageInSubtree(pagesById, dragged.id, target.id)
  ) return null;
  const sourceParent = dragged.parentId ? pagesById[dragged.parentId] : undefined;
  if (!copy && dragged.parentId && (sourceParent?.isLocked || !canEditPage(dragged.parentId))) return null;

  if (intent === "inside") {
    if (target.isLocked || !canEditPage(target.id)) return null;
    const children = childTreePages(pages, target.id).filter(({ id }) => id !== dragged.id);
    return {
      parentId: target.id,
      position: positionBetween(children.at(-1)?.position, undefined),
    };
  }

  const parentId = target.parentId;
  const parent = parentId ? pagesById[parentId] : undefined;
  if (parentId && (parent?.isLocked || !canEditPage(parentId))) return null;
  if (!parentId && (!canCreateRoot || (targetSharedRoot && !canEditPage(target.id)))) return null;
  const crossesPlace = !parentId && (dragged.teamspaceId ?? "") !== (target.teamspaceId ?? "");
  if (crossesPlace && targetSharedRoot) return null;
  const siblings = pages
    .filter((page) => inPageTree(page) && page.id !== dragged.id && page.parentId === parentId)
    .sort(stablePageOrder);
  const targetIndex = siblings.findIndex(({ id }) => id === target.id);
  if (targetIndex < 0) return null;
  const previous = intent === "before" ? siblings[targetIndex - 1] : siblings[targetIndex];
  const next = intent === "before" ? siblings[targetIndex] : siblings[targetIndex + 1];
  return {
    parentId,
    position: positionBetween(previous?.position, next?.position),
    ...(crossesPlace ? { teamspaceId: target.teamspaceId ?? "" } : {}),
  };
}

/**
 * Where a page dropped on a teamspace's name goes: the top of that
 * teamspace, above its first page, as Notion's (seen 2026-11-13: a 4px line
 * under the name, where the first page's "before" line falls, and the page
 * lands first there). A locked page, or one under a locked page or a page
 * the reader may not edit, stays where it is, as between rows.
 */
export function teamspaceTopDropPlan({ pages, draggedPageId, teamspaceId, canEditPage }: {
  pages: readonly Page[];
  draggedPageId: string;
  teamspaceId: string;
  canEditPage(pageId: string): boolean;
}): PageDropPlan | null {
  const pagesById = pageTreeMap(pages);
  const dragged = pagesById[draggedPageId];
  if (!dragged || dragged.isLocked || !canEditPage(dragged.id)) return null;
  if (dragged.parentId && (pagesById[dragged.parentId]?.isLocked || !canEditPage(dragged.parentId))) return null;
  const first = rootTreePages(pages).find((page) => page.teamspaceId === teamspaceId && page.id !== dragged.id);
  if (first && first.workspaceId !== dragged.workspaceId) return null;
  return {
    parentId: "",
    position: positionBetween(undefined, first?.position),
    ...((dragged.teamspaceId ?? "") !== teamspaceId ? { teamspaceId } : {}),
  };
}

/**
 * Where a page dropped on a section's heading goes, as Notion's (seen
 * 2026-11-14): the private pages' heading takes any page the reader may move
 * to the top of their private pages, a teamspace's page included; the shared
 * pages' heading reorders a page heading that section to its top. A locked
 * page, or one under a locked page or a page the reader may not edit, stays
 * where it is, as between rows.
 */
export function sectionTopDropPlan({ section, pages, draggedPageId, sharedPageIds, canEditPage, canCreateRoot }: {
  section: "private" | "shared";
  pages: readonly Page[];
  draggedPageId: string;
  sharedPageIds: ReadonlySet<string>;
  canEditPage(pageId: string): boolean;
  canCreateRoot: boolean;
}): PageDropPlan | null {
  const pagesById = pageTreeMap(pages);
  const dragged = pagesById[draggedPageId];
  if (!dragged || dragged.isLocked || !canEditPage(dragged.id) || !canCreateRoot) return null;
  if (dragged.parentId && (pagesById[dragged.parentId]?.isLocked || !canEditPage(dragged.parentId))) return null;
  const shared = sharedPageIds.has(dragged.id);
  if (section === "shared" ? !shared || Boolean(dragged.parentId) : shared && !dragged.parentId) return null;
  const tops = section === "shared"
    ? sharedTreePages(pages, sharedPageIds).filter((page) => !page.parentId)
    : privateRootPages(pages, sharedPageIds);
  const first = tops.find((page) => page.id !== dragged.id);
  if (first && first.workspaceId !== dragged.workspaceId) return null;
  return {
    parentId: "",
    position: positionBetween(undefined, first?.position),
    ...(section === "private" && dragged.teamspaceId ? { teamspaceId: "" } : {}),
  };
}

const TREE_PAGE = "leafen-tree-page";

/** What a sidebar row's drag carries: its page. */
export function treePageDragData(pageId: string): Record<string, unknown> {
  return { type: TREE_PAGE, pageId };
}

/** The page a sidebar row's drag carries, or null for any other drag. */
export function treePageDragId(data: Record<string | symbol, unknown>) {
  return data.type === TREE_PAGE && typeof data.pageId === "string" && data.pageId ? data.pageId : null;
}
