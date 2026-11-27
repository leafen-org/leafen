import { i18next } from "@/i18n";
import { untitledPageDisplayTitle } from "./pageTitle";
import { pageNamesNow } from "./pageNames";
import { knownTeamspaceName } from "./teamspaceNames";
import { pageIdFromPageHref } from "./pageLinks";
import { outOfTrashIn } from "./pageIndex";
import type { Block, Page, TextSpan } from "./types";

export interface PageReferenceHit {
  block: Block;
  page: Page;
  targetPage: Page;
  kind: "mention" | "link";
  preview: string;
  /** The titles of the pages above the source, from the top. */
  path: string[];
}

export type BlocksResult = {
  blocks: Block[];
  complete?: boolean;
  sourceCursor?: string | null;
  [key: string]: unknown;
};

export type PageBacklinksRemoteOptions = {
  requestScope?: string;
  sourceCursor?: string;
};

export type PageBacklinksRemote = (
  targetPageId: string,
  limit: number,
  workspaceId: string,
  options?: PageBacklinksRemoteOptions,
) => Promise<BlocksResult>;

// A backlinks request asks for one window of sources, at most maxBlocks of
// them, with at most maxInflight requests at once. What it brings back, and
// the pages and blocks already here, are read whole: the server bounds a
// window, and nothing else stops the list (2026-10-13: a parse budget over
// everything loaded — 50,000 nodes, 8,000,000 characters — failed the whole
// list in a large workspace or deep tree).
export type BacklinkBounds = {
  maxInflight: number;
  maxBlocks: number;
};

const pageBacklinksInflight = new Map<string, Promise<BlocksResult>>();
// The first window last read of each page, by who read it: a page opened
// again shows its backlinks from its first paint while they are read anew.
const PAGE_BACKLINKS_KEPT = 64;
const pageBacklinksLast = new Map<string, BlocksResult>();
const lastKey = (scope: string | undefined, workspaceId: string, targetPageId: string, limit: number) =>
  JSON.stringify([scope ?? null, workspaceId, targetPageId, limit]);

/** What the first window of `targetPageId`'s backlinks held when last read, if it was. */
export function lastPageBacklinks(scope: string | undefined, workspaceId: string, targetPageId: string, limit: number) {
  return pageBacklinksLast.get(lastKey(scope, workspaceId, targetPageId, limit));
}

function validBound(value: number) {
  return Number.isSafeInteger(value) && value >= 0;
}

export async function listPageBacklinks(
  targetPageId: string,
  limit: number,
  workspaceId: string,
  remote: PageBacklinksRemote,
  bounds: BacklinkBounds,
  options: PageBacklinksRemoteOptions = {},
): Promise<BlocksResult> {
  if (!validBound(bounds.maxInflight) || !validBound(bounds.maxBlocks)) {
    throw new RangeError("Invalid backlink bounds.");
  }
  if (!Number.isSafeInteger(limit) || limit < 0 || limit > bounds.maxBlocks) {
    throw new RangeError("Backlink request limit exceeded.");
  }
  const key = JSON.stringify([
    options.requestScope ?? null,
    workspaceId,
    targetPageId,
    limit,
    options.sourceCursor ?? null,
  ]);
  const existing = pageBacklinksInflight.get(key);
  if (existing) return existing;
  if (pageBacklinksInflight.size >= bounds.maxInflight) {
    throw new RangeError("Backlink in-flight capacity exceeded.");
  }
  const request = options.sourceCursor
    ? remote(targetPageId, limit, workspaceId, options)
    : remote(targetPageId, limit, workspaceId);
  pageBacklinksInflight.set(key, request);
  try {
    const result = await request;
    if (!Array.isArray(result.blocks) || result.blocks.length > bounds.maxBlocks) {
      throw new RangeError("Backlink result block bound exceeded.");
    }
    if (!options.sourceCursor) {
      const kept = lastKey(options.requestScope, workspaceId, targetPageId, limit);
      pageBacklinksLast.delete(kept);
      pageBacklinksLast.set(kept, result);
      if (pageBacklinksLast.size > PAGE_BACKLINKS_KEPT) pageBacklinksLast.delete(pageBacklinksLast.keys().next().value!);
    }
    return result;
  } finally {
    if (pageBacklinksInflight.get(key) === request) pageBacklinksInflight.delete(key);
  }
}

function pageTitle(page: Page) {
  return pageNamesNow.title(page);
}

function record(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value)
    ? value as Record<string, unknown>
    : null;
}

function spans(block: Block, field: "rich" | "caption") {
  const content = record(block.content);
  const value = content?.[field];
  return Array.isArray(value)
    ? value.map(record).filter((span): span is Record<string, unknown> => !!span)
    : [];
}

function isSyntheticImportRoot(page: Page) {
  const properties = record(page.properties) ?? {};
  return (
    page.kind === "page"
    && typeof properties.notionImportJobId === "string"
    && typeof properties.notionPageId !== "string"
    && typeof properties.notionDatabaseId !== "string"
    && typeof properties.notionDataSourceId !== "string"
  );
}

function referenceTargets(block: Block): Array<{ pageId: string; kind: "mention" | "link" }> {
  const mentions: string[] = [];
  const links: string[] = [];
  for (const field of ["rich", "caption"] as const) {
    for (const span of spans(block, field)) {
      if (span.mention === "page" && typeof span.pageId === "string" && span.pageId) {
        mentions.push(span.pageId);
      }
    }
  }
  for (const field of ["rich", "caption"] as const) {
    for (const span of spans(block, field)) {
      const pageId = pageIdFromPageHref(span.link);
      if (pageId) links.push(pageId);
    }
  }
  const content = record(block.content);
  // A database page's one-way relation links to the pages it holds.
  if (Array.isArray(content?.relation)) {
    for (const pageId of content.relation) if (typeof pageId === "string" && pageId) links.push(pageId);
  }
  if (
    block.type === "link_to_page"
    && typeof content?.childPageId === "string"
    && content.childPageId
  ) {
    links.push(content.childPageId);
  }

  const seen = new Set<string>();
  const targets: Array<{ pageId: string; kind: "mention" | "link" }> = [];
  for (const pageId of mentions) {
    if (seen.has(pageId)) continue;
    seen.add(pageId);
    targets.push({ pageId, kind: "mention" });
  }
  for (const pageId of links) {
    if (seen.has(pageId)) continue;
    seen.add(pageId);
    targets.push({ pageId, kind: "link" });
  }
  return targets;
}

function richText(spansValue: TextSpan[] | undefined) {
  const parts: string[] = [];
  for (const span of spansValue ?? []) parts.push(span.text);
  return parts.join("").trim();
}

function referencePreview(block: Block, kind: PageReferenceHit["kind"]) {
  const text = richText(block.content?.rich)
    || richText(block.content?.caption)
    || block.plainText?.trim();
  if (text) return text;
  if (kind === "link") {
    return i18next.t(block.type === "link_to_page" ? "pageBacklinks:fallback.linkedPageBlock" : "pageBacklinks:fallback.linkedToPage");
  }
  return i18next.t("pageBacklinks:fallback.mentioned");
}

export function mergedBlocks(
  fetchedBlocks: Block[],
  localBlocksByPage: Record<string, Block[]>,
  loadedPages: Set<string>,
) {
  const out = fetchedBlocks.filter((block) => !loadedPages.has(block.pageId));
  for (const pageId in localBlocksByPage) {
    if (!Object.prototype.hasOwnProperty.call(localBlocksByPage, pageId)) continue;
    if (loadedPages.has(pageId)) out.push(...localBlocksByPage[pageId]);
  }
  return out;
}

/** The pages above a page, its teamspace first, as Notion's paths name them. */
export function pagePath(page: Page, pagesById: Readonly<Record<string, Page>>): string[] {
  const labels: string[] = [];
  const seen = new Set<string>();
  // To the top however deep, each page once, so a cycle ends.
  let current = page.parentId ? pagesById[page.parentId] : undefined;
  let top = page;
  while (current && !seen.has(current.id)) {
    seen.add(current.id);
    top = current;
    if (!isSyntheticImportRoot(current)) labels.unshift(pageNamesNow.title(current));
    current = current.parentId ? pagesById[current.parentId] : undefined;
  }
  // A teamspace's pages sit under its name, as Notion's path has it (seen 2026-10-31).
  const team = knownTeamspaceName(top.teamspaceId);
  return team ? [team, ...labels] : labels;
}

/** The inline database a database page source sits in, after the page that holds it. */
function sourceRowDatabase(block: Block) {
  const sourceRow = (block as { sourceRow?: { inlineDatabase?: boolean; databaseTitle?: string } }).sourceRow;
  if (!sourceRow?.inlineDatabase) return null;
  return sourceRow.databaseTitle?.trim() || untitledPageDisplayTitle("database");
}

// A page's mention of itself is one of its backlinks, as Notion's (seen
// 2026-10-31). A database page's path ends with its inline database, as
// Notion's does (seen 2026-11-10). A source in the trash or under a page in
// it is none — one read here before its parent went to the trash too
// (outOfTrashIn, the server's rule as well) — as a sub-page left Notion's
// list when its parent went to the trash (seen 2026-11-24 in Chrome).
export function pageReferenceHits(
  blocks: Block[],
  pagesById: Record<string, Page>,
  opts: { targetPageId?: string } = {},
): PageReferenceHit[] {
  const pathByPage = new Map<string, string[]>();
  const hits: PageReferenceHit[] = [];
  const outOfTrash = outOfTrashIn(Object.values(pagesById));

  for (const block of blocks) {
    const page = pagesById[block.pageId];
    if (!page || !outOfTrash(page)) continue;
    for (const target of referenceTargets(block)) {
      if (opts.targetPageId && target.pageId !== opts.targetPageId) continue;
      const targetPage = pagesById[target.pageId];
      // The page shown keeps its backlinks in the trash, as Notion's keeps
      // "N개의 백링크" over a trashed page's title (seen 2026-11-13 on a phone's
      // web and 2026-11-15 in Chrome at 1440, its line on hover, the icon and
      // cover ones gone); hits for every target leave the trashed ones out,
      // with the pages under them.
      if (!targetPage || (!opts.targetPageId && !outOfTrash(targetPage))) continue;
      let path = pathByPage.get(page.id);
      if (path === undefined) {
        path = pagePath(page, pagesById);
        const database = sourceRowDatabase(block);
        if (database !== null) path = [...path, database];
        pathByPage.set(page.id, path);
      }
      hits.push({
        block,
        page,
        targetPage,
        kind: target.kind,
        preview: referencePreview(block, target.kind),
        path,
      });
    }
  }

  return hits.sort((a, b) => {
    const updated = (b.page.updatedAt ?? "").localeCompare(a.page.updatedAt ?? "");
    return updated
      || pageTitle(a.page).localeCompare(pageTitle(b.page))
      || a.block.position - b.block.position;
  });
}
