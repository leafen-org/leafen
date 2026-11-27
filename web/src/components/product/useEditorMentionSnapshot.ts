import { useEffect, useMemo } from "react";
import { warmMentionPeople } from "@/lib/mentionPeopleSeen";
import { api } from "@/api";
import type { BlockPickerSnapshot } from "@/components/editor/blockPickerModel";
import type { PagePresenceSnapshot } from "@/lib/pagePresenceView";
import type { Page as ModelPage } from "@/lib/types";
import type { Page, Session } from "@/types";
import type { InlineDatabaseProjection } from "@/workspaceMembershipWire.generated";
import { trashedPageIdsOf } from "@/lib/pageIndex";

/**
 * What the editor's @ menu can mention on a page: the workspace's live pages
 * and the databases they hold in their bodies, database row pages and the
 * workspace's people by search, and the people on the page now.
 */
export function useEditorMentionSnapshot({
  session,
  workspaceId,
  organizationId,
  pages,
  presence,
  templateMentions = false,
  inlineDatabases,
}: {
  session: Session;
  workspaceId: string | undefined;
  organizationId: string | null | undefined;
  pages: readonly Page[];
  presence: PagePresenceSnapshot | undefined;
  templateMentions?: boolean;
  inlineDatabases?: readonly InlineDatabaseProjection[];
}): BlockPickerSnapshot | null {
  const peers = presence?.peers;
  // The people an empty "@" lists are looked for as the page opens (lib/mentionPeopleSeen.ts).
  useEffect(() => {
    if (workspaceId) warmMentionPeople(session.user.id, organizationId ?? undefined, (input, options) => api.searchOrganizationPeople(session, { ...input, workspaceId, invitees: true }, options), workspaceId);
  }, [organizationId, session, workspaceId]);
  // A page under a page in the trash is in the trash too, so it is no
  // mention, link or database to pick. Worked out once for the list, not as
  // the people on the page come and go, and only when something reads it (the
  // "@" menu, a page block's title): made as each new list came, it held a
  // 44,000-page workspace some 25 ms at CPU ×2 on every title saved (2026-11-15).
  const livePages = useMemo(() => {
    let made: Record<string, ModelPage> | null = null;
    return () => {
      if (made) return made;
      const trashed = trashedPageIdsOf(pages);
      const byId: Record<string, ModelPage> = {};
      for (const page of pages) {
        if (page.workspaceId === workspaceId && !trashed.has(page.id)) byId[page.id] = page as unknown as ModelPage;
      }
      made = byId;
      return byId;
    };
  }, [pages, workspaceId]);
  return useMemo(() => {
    if (!workspaceId) return null;
    return {
      currentUserId: session.user.id,
      organizationId: organizationId ?? undefined,
      workspaceId,
      organizationProfiles: [],
      pagePresencePeers: peers ?? [],
      get pagesById() {
        return livePages();
      },
      // The workspace's people, as Notion's "@" offers, not the organization's
      // other workspaces', and those it invited by an address without an account yet.
      searchOrganizationPeople: (input, options) => api.searchOrganizationPeople(session, { ...input, workspaceId, invitees: true }, options),
      searchRowPages: async (query, signal) => (
        await api.searchRowPageMentions(session, workspaceId, query, { signal })
      ).items,
      inlineDatabases: inlineDatabases?.filter((database) => database.workspaceId === workspaceId),
      templateMentions,
      session,
    };
  }, [inlineDatabases, livePages, organizationId, peers, session, templateMentions, workspaceId]);
}
