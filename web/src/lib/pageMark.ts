// Leafen's mark as the page holds it: index.html's loading screen carries the
// picture's data in the page (scripts/page-own-parts.mjs), and the screens
// drawn while the app starts, signs in or waits for its server take that same
// picture, so none of them shows a broken one when a request fails or stalls
// and none asks the server for it (2026-11-25). A page without it (a test's)
// takes the file.
const MARK_FILE = "/icon-96.png";
let kept: string | null = null;

export function pageMarkSource(): string {
  if (kept) return kept;
  if (typeof document === "undefined") return MARK_FILE;
  const inPage = document.querySelector<HTMLImageElement>("img[data-page-mark]")?.getAttribute("src");
  if (!inPage) return MARK_FILE;
  kept = inPage;
  return kept;
}
