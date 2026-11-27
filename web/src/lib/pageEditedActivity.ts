import { useEffect, useState } from "react";
import { api } from "@/api";
import { listPageHistoryVersions } from "@/lib/pageHistoryService";
import { editorName } from "@/lib/peopleDirectory";
import { pageEditTime } from "@/lib/pageEditedHere";
import type { Page, Session } from "@/types";

export type EditedFootPerson = { id: string | null; name: string; at?: string | null };
/** Who edited a page last and who made it, for the panel its menu's foot opens. */
export type EditedFootActivity = { editor: EditedFootPerson; creator?: EditedFootPerson };

/** A page's last editor and maker, as its menus' feet show them (Notion, seen 2026-10-20). */
export function pageEditedActivity(page: {
  id?: string; lastEditedBy?: string | null; createdBy?: string | null; updatedAt?: string | null; createdAt?: string | null;
}, currentUserId: string): EditedFootActivity {
  // Its last edit as last known, one made here after the list's (lib/pageEditedHere.ts).
  const edited = pageEditTime({ id: page.id, updatedAt: page.updatedAt ?? undefined, lastEditedBy: page.lastEditedBy ?? undefined });
  const editorId = edited.lastEditedBy || page.createdBy || null;
  return {
    editor: { id: editorId, name: editorName(editorId, currentUserId), at: edited.updatedAt ?? page.updatedAt },
    creator: page.createdBy ? { id: page.createdBy, name: editorName(page.createdBy, currentUserId), at: page.createdAt } : undefined,
  };
}

/**
 * The connection a page's last edit came through; an integration's names the
 * edit alone. `maker` is the integration that made the page and who it made
 * it as, which Notion's activity names as its maker alone too (seen 2026-11-05).
 */
export type PageLastEditVia = { name: string; integration: boolean; maker?: { actorId: string; name: string } };

/**
 * The connection a page's last edit came through, "" for one made in the
 * app, read as a menu that names the last editor opens: Notion names it
 * beside the person there ("나 · 〈연결〉와(과)", seen 2026-10-29). Every edit
 * makes or extends a version that names its connection, one through another
 * connection never extending it (page_history_connections.go), so the
 * page's newest version is its last edit — taken only when it is that
 * editor's.
 */
export function usePageLastEditVia(session: Session | undefined, pageId: string, editorId: string | null, enabled: boolean): PageLastEditVia {
  const [via, setVia] = useState<PageLastEditVia & { key: string }>({ key: "", name: "", integration: false });
  const key = `${pageId}\u0000${editorId ?? ""}`;
  useEffect(() => {
    if (!enabled || !editorId || !session) return undefined;
    const controller = new AbortController();
    void listPageHistoryVersions(session, pageId, { limit: 1, signal: controller.signal }).then((list) => {
      const last = list.versions[0];
      const own = last && last.actorId === editorId;
      const made = list.creation?.connectionKind === "integration" && list.creation.connectionName ? list.creation : undefined;
      if (!controller.signal.aborted) setVia({ key, name: own ? last.connectionName ?? "" : "", integration: Boolean(own && last.connectionKind === "integration"),
        ...(made ? { maker: { actorId: made.actorId, name: made.connectionName ?? "" } } : {}) });
    }, () => undefined);
    return () => controller.abort();
  }, [editorId, enabled, key, pageId, session]);
  return enabled && via.key === key ? { name: via.name, integration: via.integration && via.name !== "", ...(via.maker ? { maker: via.maker } : {}) } : { name: "", integration: false };
}

/**
 * A page's last edit as its menu's foot shows it, read again from the server
 * when the menu opens: an edit made elsewhere (an agent, another person)
 * reaches the open text at once but not the page's own fields, which Notion's
 * foot shows at once (seen 2026-11-07: an API edit read 12:11 three seconds on;
 * Leafen's foot kept the minute before for tens of seconds).
 */
export function usePageLastEdit(session: Session | undefined, page: Pick<Page, "id" | "updatedAt" | "lastEditedBy">, enabled: boolean) {
  const [read, setRead] = useState<{ id: string; updatedAt?: string; lastEditedBy?: string; imported?: Pick<Page, "importedCreatedBy" | "importedLastEditedBy"> } | null>(null);
  useEffect(() => {
    if (!enabled || !session) return undefined;
    const controller = new AbortController();
    void api.readPage(session, page.id, controller.signal).then((fresh) => {
      if (!controller.signal.aborted) setRead({ id: page.id, updatedAt: fresh.updatedAt, lastEditedBy: fresh.lastEditedBy,
        imported: { importedCreatedBy: fresh.importedCreatedBy, importedLastEditedBy: fresh.importedLastEditedBy } });
    }, () => undefined);
    return () => controller.abort();
  }, [enabled, page.id, session]);
  // The list's, or one made here after it (lib/pageEditedHere.ts); a Notion
  // person or integration a migration kept stands for its account where the
  // page is as the migration left it (notion_imported_authors.go).
  const listed = pageEditTime(page);
  const newer = read?.id === page.id && read.updatedAt && (!listed.updatedAt || Date.parse(read.updatedAt) > Date.parse(listed.updatedAt));
  const imported = read?.id === page.id ? read.imported : undefined;
  const last = newer ? { updatedAt: read.updatedAt, lastEditedBy: read.lastEditedBy || listed.lastEditedBy } : { updatedAt: listed.updatedAt, lastEditedBy: listed.lastEditedBy };
  return { ...last, ...(imported?.importedLastEditedBy ? { lastEditedBy: imported.importedLastEditedBy } : {}),
    ...(imported?.importedCreatedBy ? { createdBy: imported.importedCreatedBy } : {}) };
}
