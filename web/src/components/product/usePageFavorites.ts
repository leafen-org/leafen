import { useEffect, useMemo, useRef, type Dispatch, type SetStateAction } from "react";
import { api } from "@/api";
import { DATABASE_ROW_PAGE_FAVORITE_EVENT, type DatabaseRowPageFavoriteChange } from "@/databaseRowPageApi";
import { newId } from "@/lib/ids";
import { createPageFavoriteService } from "@/lib/pageFavoriteService";
import { favoriteOrderKey } from "@/components/sidebar/sidebarLayoutModel";
import { putFavoriteFirst } from "@/components/sidebar/useSidebarLayout";
import type { Bootstrap, Session } from "@/types";
import { useCommittedResourceDisposal } from "./useCommittedResourceDisposal";

function sessionIdentity(session: Session) {
  return session.sessionId ?? `${session.user.id}:${session.token}`;
}

export function usePageFavorites({
  data,
  refresh,
  session,
  setData,
}: {
  data: Bootstrap;
  refresh(activeSession?: Session): Promise<unknown>;
  session: Session;
  setData: Dispatch<SetStateAction<Bootstrap>>;
}) {
  const identity = sessionIdentity(session);
  const currentIdentityRef = useRef(identity);
  const favoriteStatesRef = useRef(data.pageFavorites ?? []);
  const refreshRef = useRef(refresh);
  const sessionRef = useRef(session);
  const pagesRef = useRef(data.pages);
  pagesRef.current = data.pages;
  currentIdentityRef.current = identity;
  favoriteStatesRef.current = data.pageFavorites ?? [];
  refreshRef.current = refresh;
  sessionRef.current = session;

  const pageFavoriteService = useMemo(() => {
    const ownerSession = sessionRef.current;
    return createPageFavoriteService({
      identity,
      currentIdentity: () => currentIdentityRef.current,
      publish(state) {
        setData((current) => ({
          ...current,
          pageFavorites: current.pageFavorites.some(({ pageId }) => pageId === state.pageId)
            ? current.pageFavorites.map((item) => item.pageId === state.pageId ? state : item)
            : [...current.pageFavorites, state],
        }));
      },
      read: (pageId) => favoriteStatesRef.current.find((item) => item.pageId === pageId),
      reconcile: async () => { await refreshRef.current(ownerSession); },
      requestId: newId,
      // A page added to favorites comes first among them, as Notion's.
      send: (pageId, input, signal) => api.setPageFavorite(ownerSession, pageId, input, signal).then((response) => {
        const workspaceId = pagesRef.current.find(({ id }) => id === pageId)?.workspaceId;
        if (input.favorite && !response.duplicate && workspaceId) putFavoriteFirst(ownerSession, workspaceId, favoriteOrderKey("page", pageId));
        return response;
      }),
    });
  }, [identity, setData]);
  useCommittedResourceDisposal(pageFavoriteService);

  // A row's or template's page changes favorite through its own route; the
  // favorites lists follow it, as they follow a page's.
  useEffect(() => {
    const follow = (event: Event) => {
      const { rowId, favorite, containerPageId } = (event as CustomEvent<DatabaseRowPageFavoriteChange>).detail;
      const workspaceId = pagesRef.current.find(({ id }) => id === containerPageId)?.workspaceId;
      if (favorite && workspaceId) putFavoriteFirst(sessionRef.current, workspaceId, favoriteOrderKey("page", rowId));
      setData((current) => ({
        ...current,
        pageFavorites: current.pageFavorites.some(({ pageId }) => pageId === rowId)
          ? current.pageFavorites.map((item) => item.pageId === rowId ? { ...item, favorite } : item)
          : [...current.pageFavorites, { pageId: rowId, favorite, revision: 0 }],
      }));
    };
    window.addEventListener(DATABASE_ROW_PAGE_FAVORITE_EVENT, follow);
    return () => window.removeEventListener(DATABASE_ROW_PAGE_FAVORITE_EVENT, follow);
  }, [setData]);

  const favoritePageIds = useMemo(() => (data.pageFavorites ?? [])
    .filter(({ favorite }) => favorite)
    .map(({ pageId }) => pageId), [data.pageFavorites]);

  return { favoritePageIds, pageFavoriteService };
}
