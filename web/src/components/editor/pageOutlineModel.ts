import { compareGraphOrder } from "@/contentGraph";
import type { GraphBlock } from "@/contentGraph";
import { isSyntheticNotionImportRootPage } from "@/lib/importedNotionUi";
import type { Page } from "@/lib/types";

// A page's outline, as a breadcrumb and a table of contents show it: they
// follow the page's current place and headings, as Notion's do.

/** A heading in the outline, at the level it shows: at most one deeper than the one before. */
export type PageOutlineHeading = { block: GraphBlock; level: 1 | 2 | 3 | 4 };

const headingLevels: Readonly<Record<string, 1 | 2 | 3 | 4>> = {
  heading_1: 1, heading_2: 2, heading_3: 3, heading_4: 4,
  toggle_heading_1: 1, toggle_heading_2: 2, toggle_heading_3: 3, toggle_heading_4: 4,
};

function childrenByParent(blocks: readonly GraphBlock[]) {
  const children = new Map<string, GraphBlock[]>();
  for (const block of blocks) {
    const parent = block.parentId ?? "";
    children.set(parent, [...(children.get(parent) ?? []), block]);
  }
  for (const siblings of children.values()) {
    siblings.sort(compareGraphOrder);
  }
  return children;
}

// Notion's table of contents (seen 2026-10-06) lists the page's own headings
// and those laid out in columns, callouts and synced blocks; a toggle heading
// is listed, but nothing inside a toggle, a toggle heading, a quote or a list
// item is, opened or not.
const outlineContainers: ReadonlySet<string> = new Set(["column_list", "column", "callout", "synced_block"]);

/**
 * The page's headings in reading order, as its table of contents lists them:
 * headings 1–4, empty ones too, each shown at most one level deeper than the
 * one before it (Notion's, seen 2026-10-31: a first heading 2 sits at the top,
 * a heading 4 after a heading 1 one step in).
 */
export function pageOutlineHeadings(blocks: readonly GraphBlock[]): PageOutlineHeading[] {
  const children = childrenByParent(blocks);
  const headings: PageOutlineHeading[] = [];
  const seen = new Set<string>();
  let previous = 0;
  const visit = (parentId: string) => {
    for (const block of children.get(parentId) ?? []) {
      if (seen.has(block.id)) continue;
      seen.add(block.id);
      const level = headingLevels[block.type];
      if (level) {
        const shown = Math.min(level, previous + 1) as PageOutlineHeading["level"];
        headings.push({ block, level: shown });
        previous = shown;
      } else if (outlineContainers.has(block.type)) visit(block.id);
    }
  };
  visit("");
  return headings;
}

/**
 * The pages from the top of the page's path down to the page itself, as the
 * top bar shows them: an import's own holding page is left out.
 */
export function pageBreadcrumbChain(pageId: string, pagesById: Readonly<Record<string, Page>>): Page[] {
  const chain: Page[] = [];
  const seen = new Set<string>();
  let current: Page | undefined = pagesById[pageId];
  while (current && !seen.has(current.id)) {
    seen.add(current.id);
    if (!isSyntheticNotionImportRootPage(current as Parameters<typeof isSyntheticNotionImportRootPage>[0])) {
      chain.unshift(current);
    }
    current = current.parentId ? pagesById[current.parentId] : undefined;
  }
  return chain;
}
