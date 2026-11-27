import { useCallback, useMemo, useRef, type Dispatch, type SetStateAction } from "react";
import { api } from "@/api";
import { newId } from "@/lib/ids";
import { createPageFavoriteService } from "@/lib/pageFavoriteService";
import { favoriteOrderKey } from "@/components/sidebar/sidebarLayoutModel";
import { putFavoriteFirst } from "@/components/sidebar/useSidebarLayout";
import type { Bootstrap, Session } from "@/types";
import type { DatabaseFavoriteProjection } from "@/workspaceMembershipWire.generated";
import { useCommittedResourceDisposal } from "./useCommittedResourceDisposal";

/** An inline database to keep among favorites: its block, the page it sits in and its name. */
export type DatabaseFavoriteTarget = Pick<DatabaseFavoriteProjection, "containerId" | "pageId" | "workspaceId" | "title">;

function sessionIdentity(session: Session) {
  return session.sessionId ?? `${session.user.id}:${session.token}`;
}

/**
 * A person's favorite inline databases, changed as their favorite pages are
 * — at once, then by request id and revision, put back if the server says
 * no — each kept with its revision once taken out, so it can come back.
 */
export function useDatabaseFavorites({ data, refresh, session, setData }: {
  data: Bootstrap;
  refresh(activeSession?: Session): Promise<unknown>;
  session: Session;
  setData: Dispatch<SetStateAction<Bootstrap>>;
}) {
  const identity = sessionIdentity(session);
  const currentIdentityRef = useRef(identity);
  const statesRef = useRef(data.databaseFavorites ?? []);
  const targetsRef = useRef(new Map<string, DatabaseFavoriteTarget>());
  const refreshRef = useRef(refresh);
  const sessionRef = useRef(session);
  currentIdentityRef.current = identity;
  statesRef.current = data.databaseFavorites ?? [];
  refreshRef.current = refresh;
  sessionRef.current = session;

  const service = useMemo(() => {
    const ownerSession = sessionRef.current;
    return createPageFavoriteService({
      identity,
      currentIdentity: () => currentIdentityRef.current,
      publish(state) {
        setData((current) => {
          const list = current.databaseFavorites ?? [];
          const known = list.find(({ containerId }) => containerId === state.pageId) ?? targetsRef.current.get(state.pageId);
          if (!known) return current;
          const next = { ...known, containerId: state.pageId, favorite: state.favorite, revision: state.revision };
          return {
            ...current,
            databaseFavorites: list.some(({ containerId }) => containerId === state.pageId)
              ? list.map((item) => item.containerId === state.pageId ? { ...item, ...next } : item)
              : [...list, next],
          };
        });
      },
      read: (containerId) => {
        const state = statesRef.current.find((item) => item.containerId === containerId);
        return state && { pageId: state.containerId, favorite: state.favorite, revision: state.revision };
      },
      reconcile: async () => { await refreshRef.current(ownerSession); },
      requestId: newId,
      send: (containerId, input, signal) => api.setDatabaseFavorite(ownerSession, containerId, input, signal)
        .then((response) => ({ ...response, pageId: response.containerId })),
    });
  }, [identity, setData]);
  useCommittedResourceDisposal(service);

  const setDatabaseFavorite = useCallback(async (target: DatabaseFavoriteTarget, favorite: boolean) => {
    targetsRef.current.set(target.containerId, target);
    const state = await service.setFavorite(target.containerId, favorite);
    // One added comes first among the favorites, as Notion's.
    if (favorite && state?.favorite) putFavoriteFirst(sessionRef.current, target.workspaceId, favoriteOrderKey("database", target.containerId));
    // One added from where its name isn't at hand takes it from the server.
    if (favorite && !target.title) await refreshRef.current(sessionRef.current);
  }, [service]);

  return { databaseFavorites: data.databaseFavorites, setDatabaseFavorite };
}
