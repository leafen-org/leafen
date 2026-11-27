import { useEffect, useRef } from "react";
import type { Session } from "@/types";
import { readMentionReach, type CopyMention } from "@/lib/collaborationCopyMentions";
import { mentionReachKey, setMarkedMentionReaches, type MentionReachNow } from "@/lib/markedMentionTitles";
import { mentionPageTitlesNow } from "@/lib/peopleDirectory";
import { useAccessRevision } from "@/lib/workspaceChangeCheck";
import { readShownPreviews } from "./pagePreviews";

// A share, a group, a teamspace or a place in the workspace taken from the
// reader closes pages to them without changing the pages mentioning them:
// what draws such a mention — a line's text kept in the page's document, a
// row's title, a database's name — keeps the target's title as the server
// last sent it, and until a reload a mention of a page closed to the reader
// went on showing its title (2026-11-25). The workspace's page list, read
// again on such a change, drops the page, and the app is told the reader's
// access may have changed (useWorkspaceChangeFollow.ts): the mentions on
// screen the list doesn't hold — a page's it dropped, a row's, a template's,
// a database's, a data source's — are asked how they stand to the reader now
// (page_copy_mentions.go), and each closed to them, or gone, reads so
// (markedMentionTitles.ts) until a later change opens it again, as the
// server's own read of the page would draw it. One opened to them reads its
// title from the list, or as the preview reads it (unlistedMentionedPages.ts).
// Asked as such mentions come on screen and again at each change of access,
// and once the list has stayed still after a change (a page deleted for good
// leaves it, its mentions "찾을 수 없음"), as the names of what is mentioned
// are read again (PageMentionIconStyles.tsx). Every one of them is asked, as
// many asks as it takes (readMentionReach): the first 256 alone were, and a
// mention past them kept a closed page's title until the page opened again
// (2026-11-25).
// How long the list stays still before the mentions on screen are asked again.
const MENTION_REACH_LIST_DELAY_MS = 1000;

/**
 * The mentions on screen: pages' (those drawn closed or missing `marked`) and
 * databases' (drawn closed or missing too), a data source's by its address
 * (dataSourceMentionNames.ts) among them.
 */
export type MentionsOnScreen = { pages: ReadonlySet<string>; marked: ReadonlySet<string>; databases: ReadonlySet<string> };

/**
 * Follows how the mentions on screen stand to their reader, asked on the
 * open page (`pageId`). A page the workspace's list holds is open to them;
 * `list`, that list, is asked about again as it changes.
 */
export function useMentionReach(pageId: string | undefined, session: Session | undefined, onScreen: MentionsOnScreen, list: readonly unknown[]) {
  const access = useAccessRevision();
  const last = useRef<{ access: number; pageId?: string; list: readonly unknown[]; keys: ReadonlySet<string> }>({ access, pageId, list, keys: new Set() });
  const { pages, marked, databases } = onScreen;
  useEffect(() => {
    if (!session || !pageId) return;
    const listed = mentionPageTitlesNow();
    const asks = new Map<string, CopyMention>();
    for (const id of [...pages, ...marked]) if (!listed.has(id)) asks.set(mentionReachKey("page", id), { kind: "page", id });
    for (const id of databases) asks.set(mentionReachKey("database", id), { kind: "database", id });
    const keys = [...asks.keys()];
    const now = access !== last.current.access || pageId !== last.current.pageId || keys.some((key) => !last.current.keys.has(key));
    if (!now && list === last.current.list) return;
    if (keys.length === 0) {
      last.current = { access, pageId, list, keys: new Set() };
      setMarkedMentionReaches(new Map());
      return;
    }
    // A read the screen moved past asks no more of its parts (mentionReachParts).
    const ask = new AbortController();
    const stop = readShownPreviews(now ? 0 : MENTION_REACH_LIST_DELAY_MS, () => readMentionReach(session, pageId, keys.map((key) => asks.get(key)!), ask.signal), (answer) => {
      last.current = { access, pageId, list, keys: new Set(keys) };
      setMarkedMentionReaches(new Map(answer.flatMap((mention) => mention.reach === "open" ? []
        : [[mentionReachKey(mention.kind, mention.id), mention.reach as MentionReachNow] as const])));
    });
    return () => {
      stop();
      ask.abort();
    };
  }, [access, databases, list, marked, pageId, pages, session]);
}
