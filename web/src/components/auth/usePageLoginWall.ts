import { useEffect, useState } from "react";
import { routeInfoFromPath } from "@/lib/router";
import { serverPath } from "@/lib/serverOrigin";
import { afterPageMade } from "@/lib/pagesNotYetMade";

/** The workspace a page is in, its icon, and the app's address when it is an app's own. */
export type PageWorkspace = { name: string; icon?: string; appSlug?: string };

// A page made on this device is asked about once the server made it (pagesNotYetMade).
async function readPageWorkspace(pageId: string, signal: AbortSignal): Promise<PageWorkspace> {
  await afterPageMade(pageId, signal);
  return fetch(serverPath(`/api/leafen/v1/page-login-wall/${encodeURIComponent(pageId)}`), { signal })
    .then(response => (response.ok ? response.json() as Promise<LoginWallAnswer> : null))
    .then(answer => ({ ...workspaceOf(answer), ...(answer?.appSlug ? { appSlug: answer.appSlug } : {}) }));
}

type LoginWallAnswer = { workspaceName?: string; workspaceIcon?: string; appSlug?: string } | null;

function workspaceOf(answer: LoginWallAnswer): PageWorkspace {
  return { name: answer?.workspaceName ?? "", ...(answer?.workspaceIcon ? { icon: answer.workspaceIcon } : {}) };
}

/** A page's workspace, undefined until read, its name empty for a page the server doesn't have. */
export function usePageWorkspace(pageId: string) {
  const [read, setRead] = useState<{ pageId: string; workspace: PageWorkspace } | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    void readPageWorkspace(pageId, controller.signal)
      .then(workspace => setRead({ pageId, workspace }))
      .catch(() => { if (!controller.signal.aborted) setRead({ pageId, workspace: { name: "" } }); });
    return () => controller.abort();
  }, [pageId]);
  return read?.pageId === pageId ? read.workspace : undefined;
}

// A members' form's link names its workspace as a page's address does (page_login_wall.go).
async function readFormWorkspace(token: string, signal: AbortSignal): Promise<PageWorkspace> {
  const response = await fetch(serverPath(`/api/leafen/v1/form-login-wall/${encodeURIComponent(token)}`), { signal });
  return workspaceOf(response.ok ? await response.json() as LoginWallAnswer : null);
}

/**
 * The workspace a page waits in, for the sign-in screen at that page's
 * address to name and head with its icon, as Notion's (seen 2026-11-03) — a
 * members' form's link's too (seen 2026-11-15). None anywhere else, or for a
 * page or form the server doesn't know.
 */
export function usePageLoginWall(enabled = true): PageWorkspace | null {
  const [workspace, setWorkspace] = useState<PageWorkspace | null>(null);
  useEffect(() => {
    if (!enabled) return undefined;
    const route = routeInfoFromPath(window.location.pathname);
    if (route.kind !== "page" && route.kind !== "form") return undefined;
    const controller = new AbortController();
    void (route.kind === "page" ? readPageWorkspace(route.pageId, controller.signal) : readFormWorkspace(route.formToken, controller.signal))
      .then((read) => { if (read.name) setWorkspace(read); })
      .catch(() => undefined);
    return () => controller.abort();
  }, [enabled]);
  return enabled ? workspace : null;
}
