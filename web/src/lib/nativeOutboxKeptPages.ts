import type { OutboxBrowserRuntime } from "./outboxBrowserRuntime";

/**
 * The pages whose changes this device keeps for the server, or "every" when
 * which can't be known now: what a page's text kept here waits behind before
 * it goes, wherever it goes (pageTextBehindKeptChanges.ts), so it never
 * reaches the server ahead of the blocks it was written in.
 */
export type PagesWithKeptChanges = ReadonlySet<string> | "every";

function recordOf(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : null;
}

/**
 * The pages a kept outbox entry is of, by the causality keys every app keeps
 * on the entry itself (normalizeOutboxOperation), whatever the format of the
 * change inside: a page's ("page:") or a database row's, a page too ("row:").
 * One that names no page, or no keys, holds "every" page.
 */
export function pagesOfKeptEntry(stored: unknown): string[] | "every" {
  const keys = recordOf(stored)?.causalityKeys;
  if (!Array.isArray(keys)) return "every";
  const pages = keys.flatMap((key) => (
    typeof key === "string" && (key.startsWith("page:") || key.startsWith("row:")) ? [key.slice(key.indexOf(":") + 1)] : []
  ));
  return pages.length ? pages : "every";
}

/**
 * Every entry the outbox keeps, a step at a time, read for its pages, with
 * `alsoKept` (the changes written down as a page went, not yet in the
 * store). Entries of every app's format count, those this app can't read
 * too: the app that wrote one sends it, and the text after it. A store that
 * can't be read now (another tab keeps it past the wait) holds "every" page.
 */
export async function pagesWithKeptChanges(
  outbox: Pick<OutboxBrowserRuntime, "listReplayStep">,
  maxEntriesPerStep: number,
  alsoKept: PagesWithKeptChanges,
): Promise<PagesWithKeptChanges> {
  if (alsoKept === "every") return "every";
  const pages = new Set(alsoKept);
  try {
    let afterSeq: number | null = null;
    for (;;) {
      const step = await outbox.listReplayStep({ afterSeq, limit: maxEntriesPerStep });
      for (const item of step.items) {
        const of = pagesOfKeptEntry(item.entry.value);
        if (of === "every") return "every";
        for (const pageId of of) pages.add(pageId);
      }
      // The steps go on by sequence; one that doesn't move on is the end.
      if (step.nextAfterSeq === null || step.items.length === 0
        || (afterSeq !== null && step.nextAfterSeq <= afterSeq)) break;
      afterSeq = step.nextAfterSeq;
    }
  } catch {
    return "every";
  }
  return pages;
}
