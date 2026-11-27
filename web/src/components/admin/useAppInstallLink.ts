import { useEffect } from "react";

/**
 * "설치" on the apps page (/apps) opens Server › Apps asking for one catalog
 * app ("catalog/app"): its review opens at once (2026-10-16: an
 * administrator found no install on the apps page), saying it reads the app
 * until it is read.
 */
export function useAppInstallLink(wanted: string | undefined, read: (catalogId: string, appId: string) => void) {
  useEffect(() => {
    const [catalogId, appId] = (wanted ?? "").split("/");
    if (catalogId && appId) read(catalogId, appId);
    // Once for each app asked for.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [wanted]);
}
