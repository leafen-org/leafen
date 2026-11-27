import { useCallback, useEffect, useState } from "react";
import { readFormFavorites, saveFormFavorite } from "@/databaseToolbarConfigApi";
import type { DatabaseFormFavorite } from "@/databaseToolbarConfigWire.generated";
import type { Session } from "@/types";

export type FormFavorite = DatabaseFormFavorite;

const CHANGED = "leafen:form-favorites-changed";

/** Tells the screens that show favorites that one changed elsewhere ("내 작업"'s star). */
export function formFavoritesChanged() {
  window.dispatchEvent(new Event(CHANGED));
}

/** A person's favorite forms in the workspace, and how one is added or taken out, kept current across the screens that show them. */
export function useFormFavorites(session: Session | null | undefined, workspaceId: string | null | undefined) {
  const [favorites, setFavorites] = useState<readonly FormFavorite[]>([]);
  const [myTasks, setMyTasks] = useState(false);
  useEffect(() => {
    if (!session || !workspaceId) return undefined;
    const controller = new AbortController();
    const read = () => {
      void readFormFavorites(session, workspaceId, controller.signal)
        .then((result) => { setFavorites(result.favorites); setMyTasks(result.myTasks === true); }, () => undefined);
    };
    read();
    window.addEventListener(CHANGED, read);
    return () => { controller.abort(); window.removeEventListener(CHANGED, read); };
  }, [session, workspaceId]);
  const setFavorite = useCallback(async (token: string, favorite: boolean) => {
    if (!session) return;
    await saveFormFavorite(session, token, favorite);
    window.dispatchEvent(new Event(CHANGED));
  }, [session]);
  return { favorites, myTasks, setFavorite };
}
