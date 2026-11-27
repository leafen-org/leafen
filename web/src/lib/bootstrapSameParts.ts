import type { Bootstrap } from "@/types";
import { notePageEditTimes, type PageEditTime } from "./pageEditedHere";
import { pageMapOf } from "./pageIndex";

// A page list read again is mostly the list shown. Each part of it — its
// pages, favorites, databases, sharing, workspaces — equal to the shown
// one's is the shown one, and each page equal to the shown page is that
// page: what is worked out once for a list (lib/pageIndex.ts, the sidebar's
// tree) is kept, and the product draws again only what changed (2026-11-14:
// every read of a 47,000-page list for a page made elsewhere redrew all of
// it, 400 ms at CPU ×2, though the page was already shown). The pages are
// compared a slice at a time, a task each, so the comparing blocks no frame
// for long either.

const SLICE_PAGES = 4000;

// The next task, not held back as a hidden tab's timers are.
export function nextTask(): Promise<void> {
  return new Promise((resolve) => {
    const channel = new MessageChannel();
    channel.port1.onmessage = () => {
      channel.port1.close();
      resolve();
    };
    channel.port2.postMessage(null);
  });
}

/** Whether two values read as JSON are the same. */
export function same(left: unknown, right: unknown): boolean {
  if (left === right) return true;
  if (typeof left !== "object" || typeof right !== "object" || left === null || right === null) return false;
  if (Array.isArray(left) || Array.isArray(right)) {
    if (!Array.isArray(left) || !Array.isArray(right) || left.length !== right.length) return false;
    for (let index = 0; index < left.length; index += 1) if (!same(left[index], right[index])) return false;
    return true;
  }
  const leftFields = left as Record<string, unknown>;
  const rightFields = right as Record<string, unknown>;
  let fields = 0;
  for (const key in leftFields) {
    fields += 1;
    if (!Object.hasOwn(rightFields, key)) return false;
    const value = leftFields[key];
    if (value !== rightFields[key] && !same(value, rightFields[key])) return false;
  }
  for (const key in rightFields) if (Object.hasOwn(rightFields, key)) fields -= 1;
  return fields === 0;
}

/** The read part, or the shown one when the same. */
function sharedPart<Item>(read: Item[] | undefined, shown: Item[] | undefined): Item[] | undefined {
  return read && shown && same(read, shown) ? shown : read;
}

type ListedPage = Bootstrap["pages"][number];

/** Whether a read page is the shown one but for when and by whom it was last edited. */
function sameButEdited(page: ListedPage, before: ListedPage): boolean {
  if (page.updatedAt === before.updatedAt && page.lastEditedBy === before.lastEditedBy) return false;
  const timed: Record<string, unknown> = { ...page };
  delete timed.updatedAt;
  delete timed.lastEditedBy;
  if (Object.hasOwn(before, "updatedAt")) timed.updatedAt = before.updatedAt;
  if (Object.hasOwn(before, "lastEditedBy")) timed.lastEditedBy = before.lastEditedBy;
  return same(timed, before);
}

/**
 * The read pages, each the shown page when the same, and the shown list when
 * all are — in its order: a page read ahead of the list sits at its end
 * (bootstrapNewPages.ts) and in the read among its part's, and the list is
 * a set of pages, each placed by its own parent and position. A page changed
 * only in its edit time is the shown page too, its time kept apart
 * (lib/pageEditedHere.ts): what lists pages isn't drawn again for it.
 */
async function sharedPages(read: Bootstrap["pages"], shown: Bootstrap["pages"]): Promise<Bootstrap["pages"]> {
  if (!shown.length) return read;
  const byId = pageMapOf(shown);
  let changed = read.length !== shown.length;
  const pages: Bootstrap["pages"] = new Array(read.length);
  const edited: PageEditTime[] = [];
  for (let start = 0; start < read.length; start += SLICE_PAGES) {
    if (start) await nextTask();
    for (let index = start; index < Math.min(read.length, start + SLICE_PAGES); index += 1) {
      const page = read[index];
      const before = byId.get(page.id);
      if (before && same(page, before)) pages[index] = before;
      else if (before && sameButEdited(page, before)) {
        pages[index] = before;
        edited.push({ id: page.id, updatedAt: page.updatedAt, lastEditedBy: page.lastEditedBy });
      } else {
        pages[index] = page;
        changed = true;
      }
    }
  }
  notePageEditTimes(edited);
  return changed ? pages : shown;
}

/** The list read, sharing the parts and pages the same as the list shown. */
export async function sharingShownParts(read: Bootstrap, shown: Bootstrap): Promise<Bootstrap> {
  if (read === shown || !shown.pages.length) return read;
  const pages = await sharedPages(read.pages, shown.pages);
  const shared: Bootstrap = {
    ...read,
    workspaces: sharedPart(read.workspaces, shown.workspaces) ?? read.workspaces,
    pages,
    pageFavorites: sharedPart(read.pageFavorites, shown.pageFavorites) ?? read.pageFavorites,
  };
  for (const key of ["databaseFavorites", "inlineDatabases", "sharedPageIds", "workspaceWidePageIds"] as const) {
    const part = sharedPart<unknown>(read[key], shown[key]);
    if (part !== read[key]) (shared as Record<string, unknown>)[key] = part;
  }
  return shared;
}
