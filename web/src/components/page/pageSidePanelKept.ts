import { parseStored, personValue, storePersonValue } from "@/lib/browserStorage";

/**
 * The panel kept open beside a page: a reload, another tab or a later visit
 * opens the page with it again, as Notion's (seen 2026-11-25 in Chrome at
 * 1440 and 800, two tabs, a page and a database row's page): one page at a
 * time — the last one whose "모든 토론" or "업데이트와 애널리틱스" opened docked
 * beside it — until it is closed there. Opening one beside another page takes
 * its place; visiting other pages meanwhile leaves it as it was. An app beside
 * the page, Leafen's own, is not kept: opening one lets the kept panel go.
 */
export type KeptSidePanel = "discussions" | "updates";

const NAME = "pageSidePanel";

/** The panel kept open beside the page, if it is the page kept. */
export function keptSidePanel(pageId: string): KeptSidePanel | null {
  const kept = parseStored(personValue(NAME));
  if (!kept || typeof kept !== "object") return null;
  const { pageId: keptPage, kind } = kept as { pageId?: unknown; kind?: unknown };
  return keptPage === pageId && (kind === "discussions" || kind === "updates") ? kind : null;
}

/** Keeps the panel open beside the page, in place of any kept before. */
export function keepSidePanel(pageId: string, kind: KeptSidePanel) {
  storePersonValue(NAME, JSON.stringify({ pageId, kind }));
}

/** Lets the page's kept panel go, as it closes there; another page's stays. */
export function forgetSidePanel(pageId: string) {
  if (keptSidePanel(pageId)) storePersonValue(NAME, null);
}
