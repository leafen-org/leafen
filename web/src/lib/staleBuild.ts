import { reloadKeepingPlace } from "@/lib/reloadKeepingPlace";
import { i18next } from "@/i18n";
import { announceUpdateReady, updateNoticeWordsReady } from "./updateReadyNotice";

// Each build names its files by their content, so a tab still running an
// older build, once a newer one went up, asks for modules the server no
// longer has — and the server says they are missing (internal/app/app.go
// webBuildFilePath). The app then moves to the new build: a screen that
// couldn't load reloads once (ErrorBoundary), and a part a click needed says
// why with a reload (watchStaleBuild), so nothing waits on a module that
// never comes.

export type FailedBuildAsset = { kind: "module" | "stylesheet"; url: URL };

const VITE_CSS_PRELOAD_ERROR_PREFIX = "Unable to preload CSS for ";

/** The build file a failed module or stylesheet load names, if it was one. */
export function failedBuildAsset(error: unknown): FailedBuildAsset | null {
  if (typeof window === "undefined" || !(error instanceof Error)) return null;
  const isViteCssPreloadError = error.message.startsWith(VITE_CSS_PRELOAD_ERROR_PREFIX);
  const candidates = error.message.match(/https?:\/\/[^\s"'<>]+/giu) ?? [];
  for (const candidate of candidates) {
    try {
      const url = new URL(candidate.replace(/[),.;]+$/u, ""));
      if (url.origin !== window.location.origin || !url.pathname.startsWith("/assets/")) {
        continue;
      }
      if (url.pathname.endsWith(".js")) return { kind: "module", url };
      if (isViteCssPreloadError && url.pathname.endsWith(".css")) {
        return { kind: "stylesheet", url };
      }
    } catch {
      // A malformed token is not a same-origin build file.
    }
  }
  return null;
}

/**
 * Whether the server no longer has a build file: true when it says it is
 * missing (or, an older server, answers with the app's page instead), false
 * when it has it — the load failed some other way — and null when it can't
 * be asked.
 */
export async function buildAssetGone(path: string): Promise<boolean | null> {
  try {
    const response = await fetch(path, { method: "HEAD", cache: "no-store", credentials: "same-origin" });
    if (response.status === 404) return true;
    if (!response.ok) return null;
    return response.headers.get("content-type")?.toLowerCase().startsWith("text/html") ?? false;
  } catch {
    return null;
  }
}

let staleBuildAnnounced = false;

async function announceIfStale(error: unknown) {
  const asset = failedBuildAsset(error);
  if (!asset || staleBuildAnnounced) return;
  if (await buildAssetGone(asset.url.pathname) !== true || staleBuildAnnounced) return;
  staleBuildAnnounced = true;
  await updateNoticeWordsReady();
  announceUpdateReady({
    message: i18next.t("serviceWorker:staleBuild"),
    reloadLabel: i18next.t("serviceWorker:reload"),
    reload: reloadKeepingPlace,
  });
}

/**
 * A module a click or a menu needed failed to load because a newer build
 * went up: the app says so once, with a reload, rather than the click doing
 * nothing. A screen's own failure also reloads by itself (ErrorBoundary).
 */
export function watchStaleBuild() {
  if (typeof window === "undefined") return;
  window.addEventListener("vite:preloadError", (event) => {
    void announceIfStale((event as Event & { payload?: unknown }).payload);
  });
  // A module loaded without others to load first has no such event.
  window.addEventListener("unhandledrejection", (event) => {
    void announceIfStale(event.reason);
  });
}
