import type { Bootstrap, Page } from "@/types";
import type { RecordCacheFacade } from "./recordCache";
import { nextTask, same } from "./bootstrapSameParts";

// A long page list is kept on this device in parts beside the rest of the
// list (its head), each a run of the list in its own order, kept as its
// text: kept whole, a 47,000-page list was one 37 MB value that each change
// wrote again — written out, sealed and put — and a change now writes the
// head and the runs whose pages changed, the others staying as kept. A start
// reads the runs' texts, a run a task, and makes the list's pages in one
// reading of their joined text, as one read of the whole list made them:
// read back run by run, its pages made each run apart, every later drawing
// of the list went through them a fifth to a half slower (2026-11-15, CPU ×2).

/** Pages a run holds; a list shorter than two runs is kept whole. */
const PART_PAGES = 1_500;

/** A run's key, under the list's own key. */
export const bootstrapPartKey = (listKey: string, part: number) => `${listKey}:pages:${part}`;

/** A run as the store keeps it: the mark the head names it by, how many pages, and their JSON text without its brackets. */
type StoredPagesPart = { readonly mark: string; readonly count: number; readonly text: string };

/** A run as last kept or read here: its mark and its pages. */
type KeptPagesPart = { readonly mark: string; readonly pages: readonly Page[] };

/** The list's head as kept beside its runs: the list without its pages, and its runs' marks in order. */
export type BootstrapHead = Bootstrap & { pageParts?: readonly string[] };

/** Whether two runs hold the same pages, the very same ones, or (`alike`) ones the same as read. */
function samePages(left: readonly Page[], right: readonly Page[], alike = false) {
  if (left.length !== right.length) return false;
  for (let index = 0; index < left.length; index += 1) {
    if (left[index] !== right[index] && !(alike && same(left[index], right[index]))) return false;
  }
  return true;
}

// A run's mark is new each time it changes, here or in another tab.
const newMark = () => `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 10)}`;

/**
 * The list's pages in runs, each the run kept before when it holds the same
 * pages: a run is new only when a page in it changed or moved. A run whose
 * pages are other objects — a list read again from the server — is compared
 * page by page, a task each, and the run kept stays when they read the same.
 */
async function bootstrapParts(pages: readonly Page[], before: readonly KeptPagesPart[] | undefined): Promise<KeptPagesPart[]> {
  if (pages.length < PART_PAGES * 2) return [];
  const parts: KeptPagesPart[] = [];
  for (let start = 0, index = 0; start < pages.length; start += PART_PAGES, index += 1) {
    const run = pages.slice(start, start + PART_PAGES);
    const kept = before?.[index];
    if (kept && samePages(kept.pages, run)) {
      parts.push(kept);
      continue;
    }
    if (kept && kept.pages.length === run.length) await nextTask();
    parts.push(kept && samePages(kept.pages, run, true) ? kept : { mark: newMark(), pages: run });
  }
  return parts;
}

/**
 * A list's keeping in runs on this device's store: its runs read with its
 * head, its writes with the runs each last kept, one after another.
 */
export function createBootstrapPartsKeeping(cache: RecordCacheFacade) {
  // The runs each list was last kept or read in here, and its write under way.
  const keptParts = new Map<string, readonly KeptPagesPart[]>();
  const writing = new Map<string, Promise<void>>();
  const memoOf = (principalId: string, key: string) => `${principalId}\n${key}`;

  /**
   * The list's pages from the runs its head names, in order; null when one
   * isn't the head's. Each run is read and opened on its own: read together
   * in one store read, they were opened in one go and held the page longer
   * (2026-11-15, CPU ×2: its longest frame 285 → 441 ms).
   */
  async function readPages(principalId: string, key: string, marks: readonly string[]): Promise<Page[] | null> {
    const parts = await Promise.all(marks.map((_, index) => cache.getMeta<StoredPagesPart>(principalId, bootstrapPartKey(key, index))));
    // A run another write kept than the head's: the copy isn't whole.
    if (!parts.every((part, index): part is StoredPagesPart => part?.mark === marks[index]
      && typeof part.text === "string" && Number.isSafeInteger(part.count))) return null;
    const pages = JSON.parse(`[${parts.map(({ text }) => text).join(",")}]`) as Page[];
    if (!Array.isArray(pages) || pages.length !== parts.reduce((total, { count }) => total + count, 0)) return null;
    let start = 0;
    keptParts.set(memoOf(principalId, key), parts.map(({ mark, count }) => {
      const run = { mark, pages: pages.slice(start, start + count) };
      start += count;
      return run;
    }));
    return pages;
  }

  /**
   * Keeps the snapshot under `key`: a long list's head, and the runs the head
   * kept in the store doesn't name already, none of those a shorter list no
   * longer has; a short list whole, as before. The list is never changed
   * once read (a change makes a new one): kept frozen, its head and runs
   * aren't copied on their way to the store (recordCacheMetaBatch.ts). A
   * run's text is written out in its own task.
   */
  function write<Snapshot extends { principalId: string; value: Bootstrap }>(snapshot: Snapshot, key: string): Promise<void> {
    const memo = memoOf(snapshot.principalId, key);
    const kept = (writing.get(memo) ?? Promise.resolve()).catch(() => undefined).then(async () => {
      const before = keptParts.get(memo);
      const parts = await bootstrapParts(snapshot.value.pages, before);
      const texts = new Map<number, StoredPagesPart>();
      for (const [index, part] of parts.entries()) {
        if (before?.[index] === part) continue;
        if (texts.size) await nextTask();
        texts.set(index, Object.freeze({ mark: part.mark, count: part.pages.length, text: JSON.stringify(part.pages).slice(1, -1) }));
      }
      // A head is no whole list by itself: a reader that doesn't read runs takes no copy from it.
      const head = parts.length
        ? Object.freeze({ ...snapshot, complete: false, value: Object.freeze({ ...snapshot.value, pages: [], pageParts: Object.freeze(parts.map(({ mark }) => mark)) }) })
        : Object.freeze({ ...snapshot });
      await cache.mutations.writeMetaFrom<{ value?: BootstrapHead }>(snapshot.principalId, key, (stored) => {
        const keptMarks = stored?.value?.pageParts ?? [];
        return [
          { key, value: head },
          ...parts.flatMap((part, index) => {
            if (keptMarks[index] === part.mark) return [];
            const text = texts.get(index)
              ?? Object.freeze({ mark: part.mark, count: part.pages.length, text: JSON.stringify(part.pages).slice(1, -1) });
            return [{ key: bootstrapPartKey(key, index), value: text }];
          }),
          ...keptMarks.slice(parts.length).map((_, extra) => ({ key: bootstrapPartKey(key, parts.length + extra), value: undefined })),
        ];
      }, { propagateFailure: true });
      keptParts.set(memo, parts);
    });
    writing.set(memo, kept);
    return kept;
  }

  /** A page of a list as kept: a long list's from its runs, read one at a time until it is found. */
  async function keptPage(userId: string, key: string, head: BootstrapHead | undefined, pageId: string): Promise<Page | undefined> {
    const marks = head?.pageParts;
    if (!Array.isArray(marks) || !marks.length) return Array.isArray(head?.pages) ? head.pages.find(({ id }) => id === pageId) : undefined;
    const named = JSON.stringify(pageId);
    for (let index = 0; index < marks.length; index += 1) {
      const part = await cache.getMeta<StoredPagesPart>(userId, bootstrapPartKey(key, index));
      if (typeof part?.text !== "string" || !part.text.includes(named)) continue;
      const found = (JSON.parse(`[${part.text}]`) as Page[]).find(({ id }) => id === pageId);
      if (found) return found;
    }
    return undefined;
  }

  return { keptPage, readPages, write };
}
