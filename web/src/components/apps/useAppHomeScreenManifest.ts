import { useEffect } from "react";

/**
 * While an app's screen is open, the page names that app's web app manifest
 * instead of Leafen's, so the browser's "install" puts the app itself on the
 * home screen; leaving the app names Leafen's again.
 */
export function useAppHomeScreenManifest(manifestUrl: string | undefined) {
  useEffect(() => {
    const link = document.querySelector<HTMLLinkElement>('link[rel="manifest"]');
    if (!link || !manifestUrl) return undefined;
    const leafen = link.getAttribute("href") ?? "/manifest.webmanifest";
    link.setAttribute("href", manifestUrl);
    return () => link.setAttribute("href", leafen);
  }, [manifestUrl]);
}
