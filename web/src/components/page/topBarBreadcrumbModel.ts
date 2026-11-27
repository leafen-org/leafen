import { pageMapOf } from "@/lib/pageIndex";

export type TopBarBreadcrumbPage = {
  id: string;
  parentId: string;
};

export type TopBarBreadcrumb<T extends TopBarBreadcrumbPage> = {
  id: string;
  label: string;
  page: T;
};

/**
 * A page's path from the top: the pages above it in the given list, or as
 * `above` finds the one above each — a piece the list doesn't hold, as a
 * database's (pagePathPieces.ts).
 */
export function projectTopBarBreadcrumbs<T extends TopBarBreadcrumbPage>(
  page: T,
  pages: readonly T[],
  include: (candidate: T) => boolean,
  label: (candidate: T) => string,
  above?: (candidate: T, local: (id: string) => T | undefined) => T | undefined,
): TopBarBreadcrumb<T>[] {
  const pagesById = pageMapOf(pages);
  const local = (id: string) => id === page.id ? page : pagesById.get(id);
  const chain: TopBarBreadcrumb<T>[] = [];
  const seen = new Set<string>();
  let current: T | undefined = page;
  while (current && !seen.has(current.id)) {
    seen.add(current.id);
    if (include(current)) {
      chain.unshift({ id: current.id, label: label(current), page: current });
    }
    current = above ? above(current, local) : current.parentId ? local(current.parentId) : undefined;
  }
  return chain;
}

const letters = typeof Intl.Segmenter === "function"
  ? (text: string) => Array.from(new Intl.Segmenter("und", { granularity: "grapheme" }).segment(text), ({ segment }) => segment)
  : (text: string) => Array.from(text);

/**
 * The name of a piece the "…" spread out, as Notion's (seen 2026-11-08): a
 * title's first two letters and "…" ("abc" → "ab…", "한글 이름" → "한글…"), a
 * title of two letters or fewer whole, and an untitled page's whole
 * placeholder ("새 페이지").
 */
export function revealedBreadcrumbLabel(title: string, untitled: string) {
  const name = letters(title.trim());
  if (name.length === 0) return untitled;
  return name.length > 2 ? `${name.slice(0, 2).join("")}…` : name.join("");
}

export function partitionTopBarBreadcrumbs<T extends TopBarBreadcrumbPage>(
  breadcrumbs: readonly TopBarBreadcrumb<T>[],
) {
  const hidden = breadcrumbs.length > 3 ? breadcrumbs.slice(1, -2) : [];
  const visible = hidden.length > 0
    ? [breadcrumbs[0], ...breadcrumbs.slice(-2)]
    : [...breadcrumbs];
  return { hidden, visible };
}
