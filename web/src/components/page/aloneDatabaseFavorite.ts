import { useEffect } from "react";
import { useProductPages } from "@/components/product/productPagesContext";

/** The database opened alone now, which the page link keys copy (aloneDatabaseLinkTarget). */
let linkTarget: { id: string; title: string } | null = null;

/**
 * The ☆ over a database opened alone — one in a page's body or in a row's
 * page's body — which makes the database a favorite, not the page holding
 * it, as Notion's (seen 2026-11-15 twice: the database then stands among
 * the favorites with its table icon). None for a page's own bar. The same
 * database is what the page link keys copy while it is shown.
 */
export function useAloneDatabaseFavorite(blockId: string | undefined, holder: { id: string; workspaceId: string } | undefined, title: string) {
  useEffect(() => {
    if (!blockId) return;
    const shown = { id: blockId, title };
    linkTarget = shown;
    return () => { if (linkTarget === shown) linkTarget = null; };
  }, [blockId, title]);
  return useDatabaseFavorite(blockId, holder, title);
}

/**
 * Whether an inline database is among the reader's favorites, and the change
 * of it, kept under the name it goes by (inlineDatabaseShownName): the ☆ of
 * one opened alone, as a page or in a side peek (PagePeekActions).
 */
export function useDatabaseFavorite(blockId: string | undefined, holder: { id: string; workspaceId: string } | undefined, title: string) {
  const productPages = useProductPages();
  const set = productPages?.setDatabaseFavorite;
  if (!blockId || !holder) return null;
  return {
    favorite: productPages?.databaseFavorites?.some((item) => item.containerId === blockId && item.favorite) === true,
    change: set ? (favorite: boolean) => set({ containerId: blockId, pageId: holder.id, workspaceId: holder.workspaceId, title }, favorite) : undefined,
  };
}

/**
 * The database opened alone whose link ⌘⌥L (⌘L in the Mac app) and the Mac
 * app's "현재 페이지 링크 복사" and "현재 페이지의 이름 복사" copy, as its bar's
 * link does: its own address with its view, never the page holding it.
 * Notion's ⌘⌥L there copies the database's address and says "클립보드에 블록
 * 링크 복사 완료", over a page "클립보드에 페이지 링크 복사됨" (seen 2026-11-15).
 */
export function aloneDatabaseLinkTarget() {
  return linkTarget;
}
