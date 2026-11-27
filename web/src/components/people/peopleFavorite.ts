import { api } from "@/api";
import { DATABASE_ROW_PAGE_FAVORITE_EVENT, type DatabaseRowPageFavoriteChange } from "@/databaseRowPageApi";
import { favoriteOrderKey } from "@/components/sidebar/sidebarLayoutModel";
import { putFavoriteFirst } from "@/components/sidebar/useSidebarLayout";
import { randomUUID } from "@/lib/browserCrypto";
import type { Session } from "@/types";

function tellFavorites(pageId: string, favorite: boolean) {
  window.dispatchEvent(new CustomEvent<DatabaseRowPageFavoriteChange>(DATABASE_ROW_PAGE_FAVORITE_EVENT,
    { detail: { rowId: pageId, favorite, containerPageId: "" } }));
}

/**
 * Makes People's own page a favorite or not, and tells the favorites lists:
 * no list of pages holds it, so the sidebar's and the library's favorites
 * follow it as they follow a row's page (usePageFavorites) — at the press, a
 * new one first among them, as Notion's sidebar lists a starred People at
 * once (seen 2026-11-16), and back as it was if the server refuses.
 */
export async function setPeopleDatabaseFavorite(session: Session, workspaceId: string, pageId: string, favorite: boolean, expectedRevision: number) {
  tellFavorites(pageId, favorite);
  if (favorite) putFavoriteFirst(session, workspaceId, favoriteOrderKey("page", pageId));
  try {
    const saved = await api.setPageFavorite(session, pageId, { requestId: randomUUID(), favorite, expectedRevision });
    if (saved.favorite !== favorite) tellFavorites(pageId, saved.favorite);
    return saved;
  } catch (error) {
    tellFavorites(pageId, !favorite);
    throw error;
  }
}
