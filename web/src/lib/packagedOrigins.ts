// The origins of the packaged client's own page (Wails' asset server): a
// custom scheme on macOS and iOS, a reserved local host name on Windows and
// Android.
export const PACKAGED_ORIGINS = ["wails://localhost", "http://wails.localhost", "https://wails.localhost"];

/**
 * Whether this page is the packaged client's own — the one mark the web
 * reads for "this is Leafen's own app window" (packagedClient.ts passes it on).
 */
export function isPackagedClient(): boolean {
  return typeof window !== "undefined" && PACKAGED_ORIGINS.includes(window.location.origin);
}
