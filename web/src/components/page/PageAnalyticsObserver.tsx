import { useEffect, useRef } from "react";
import { newId } from "@/lib/ids";
import { usePageAnalyticsRuntime } from "@/lib/pageAnalyticsRuntime";

export type PageAnalyticsObservation =
  | {
      kind: "member";
      workspaceId: string;
      pageId: string;
      /** A database row page, observed through the page that holds it. */
      rowId?: string;
      /** An inline database opened alone, observed as itself through the page that holds it. */
      containerId?: string;
      inTrash: boolean;
    }
  | { kind: "public"; token: string; pageId: string }
  | { kind: "publicSite"; siteSlug: string; pageId: string };

function observationScope(input: PageAnalyticsObservation) {
  if (input.kind === "member") {
    return `member:${input.workspaceId}:${input.pageId}:${input.rowId ?? ""}:${input.containerId ?? ""}:${input.inTrash}`;
  }
  if (input.kind === "public") return `public:${input.token}:${input.pageId}`;
  return `public-site:${input.siteSlug}:${input.pageId}`;
}

export function PageAnalyticsObserver({ input }: { input: PageAnalyticsObservation }) {
  const runtime = usePageAnalyticsRuntime();
  const scope = observationScope(input);
  const visit = useRef({ scope: "", id: "" });
  if (visit.current.scope !== scope) visit.current = { scope, id: newId() };

  useEffect(() => {
    if (!runtime || (input.kind === "member" && input.inTrash)) return;
    if (input.kind === "member") {
      runtime.observeMember({
        workspaceId: input.workspaceId,
        pageId: input.pageId,
        ...(input.rowId ? { rowId: input.rowId } : {}),
        ...(input.containerId ? { containerId: input.containerId } : {}),
        visitId: visit.current.id,
      });
      return;
    }
    if (input.kind === "public") {
      runtime.observePublic({
        token: input.token,
        pageId: input.pageId,
        visitId: visit.current.id,
      });
      return;
    }
    runtime.observePublicSite({
      siteSlug: input.siteSlug,
      pageId: input.pageId,
      visitId: visit.current.id,
    });
  }, [runtime, scope]);

  return null;
}
