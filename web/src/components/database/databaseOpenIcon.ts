import type { GraphDataSource } from "@/contentGraph";
import { hasExplicitPageIcon, type PageIconSource } from "@/components/page/PageIcon";
import { useProductPages } from "@/components/product/productPagesContext";

/**
 * A source's database icon, as a board group preview's "⋯ › ⛁ 〈DB〉 열기"
 * draws it (Notion's, seen 2026-11-11): a database page's own icon, else the
 * source's — none drawing the cylinder there.
 */
export function useDatabaseIcon(dataSource: GraphDataSource): PageIconSource {
  const page = useProductPages()?.pages.find((candidate) => candidate.id === dataSource.pageId);
  if (page?.kind === "database" && hasExplicitPageIcon(page)) return { icon: page.icon, iconType: page.iconType, kind: "database" };
  return { icon: dataSource.icon ?? "", iconType: dataSource.iconType ?? "none", kind: "database" };
}
