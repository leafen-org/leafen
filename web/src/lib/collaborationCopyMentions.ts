import * as Y from "yjs";
import { request } from "@/apiTransport";
import type { Session } from "@/types";
import { BLOCK_TEXT_ROOT_PREFIX } from "./blockTextEditing";
import { isServerUnreachable } from "./serverReach";

// A page's copy kept in this browser holds its page and database mentions as
// the server last sent them to this reader: a title, or marks for one closed
// to them or missing (collaboration_reader_mentions.go). The server sends a
// text once, so the copy keeps those after the target opens, closes or goes:
// before the page opens from its copy, the copy's mentions are read against
// how they stand to the reader now (page_copy_mentions.go), and a copy that
// shows one otherwise is let go, so the page opens from the server's.

const CLOSED_MARK = "";
const MISSING_MARK = "";
// The server answers this many at a time (maxPageCopyMentions); a page with
// more is read in as many asks as it takes, all of them.
const MENTIONS_PER_READ = 256;

type Reach = "open" | "closed" | "missing";
/** A page's or a database's mention, by its kind and its target's ID. */
export type CopyMention = { kind: "page" | "database"; id: string };

/**
 * How mentions stand to their reader now, asked on a page they may read
 * (page_copy_mentions.go), every one however many: MENTIONS_PER_READ at a
 * time, one ask after another, each part (`part`, the mentions it asked) as
 * its answer comes. The one way a page's copy and an open screen ask
 * (useMentionReach.ts) (2026-11-25: the screen asked its first 256 alone,
 * and a mention past them kept the title of a page closed to its reader until
 * the page opened again). One that stops reading, or whose `signal` is
 * aborted, asks no more; the ask under way is let finish, its answer
 * dropped, as a POST given up on counts as a write not known and has the
 * screen read everything again (lib/uncertainWrites.ts).
 */
export async function* mentionReachParts(session: Session, pageId: string, mentions: readonly CopyMention[], signal?: AbortSignal) {
  for (let start = 0; start < mentions.length; start += MENTIONS_PER_READ) {
    signal?.throwIfAborted();
    const part = mentions.slice(start, start + MENTIONS_PER_READ);
    const answer = await request<{ mentions?: Array<CopyMention & { reach: Reach }> }>(
      `/pages/${encodeURIComponent(pageId)}/copy-mentions`,
      { method: "POST", body: JSON.stringify({ mentions: part }) },
      session,
    );
    signal?.throwIfAborted();
    yield { part, answer: answer.mentions ?? [] };
  }
}

/** How every one of the mentions stands to their reader now (mentionReachParts), all the answers together. */
export async function readMentionReach(session: Session, pageId: string, mentions: readonly CopyMention[], signal?: AbortSignal) {
  const all: Array<CopyMention & { reach: Reach }> = [];
  for await (const { answer } of mentionReachParts(session, pageId, mentions, signal)) all.push(...answer);
  return all;
}

function shownReach(text: string): Reach {
  if (text && [...text].every((character) => character === CLOSED_MARK)) return "closed";
  if (text && [...text].every((character) => character === MISSING_MARK)) return "missing";
  return "open";
}

/** The page's page and database mentions, each with how it shows them; a mention shown two ways is "mixed". */
function shownMentions(doc: Y.Doc) {
  const shown = new Map<string, { mention: CopyMention; reach: Reach | "mixed" }>();
  for (const name of [...doc.share.keys()]) {
    if (!name.startsWith(BLOCK_TEXT_ROOT_PREFIX)) continue;
    for (const op of doc.getText(name).toDelta() as Array<{ insert?: unknown; attributes?: Record<string, unknown> }>) {
      const attributes = op.attributes ?? {};
      if (typeof op.insert !== "string") continue;
      const pageId = typeof attributes.pageId === "string" ? attributes.pageId : "";
      const databaseId = typeof attributes.databaseId === "string" ? attributes.databaseId : "";
      const kind = attributes.mention === "database" || (!attributes.mention && databaseId) ? "database"
        : attributes.mention === "page" || (!attributes.mention && pageId) ? "page" : null;
      const id = kind === "database" ? databaseId : kind === "page" ? pageId : "";
      if (!kind || !id) continue;
      const key = `${kind}:${id}`, reach = shownReach(op.insert);
      const known = shown.get(key);
      shown.set(key, { mention: { kind, id }, reach: known && known.reach !== reach ? "mixed" : reach });
    }
  }
  return [...shown.values()];
}

/** An ask the server didn't answer for now: out of reach, the reader to sign in again, or asking too often. */
function unanswered(error: unknown) {
  const status = error && typeof error === "object" ? Reflect.get(error, "status") : undefined;
  return isServerUnreachable(error) || status === 401 || status === 408 || status === 429;
}

/**
 * Whether the page, opened from its copy, shows a mention otherwise than it
 * stands to its reader now, every mention read however many there are; null
 * when that can't be known yet, the server out of reach, and the copy is
 * what there is till it answers. A copy whose mentions the server answers
 * about otherwise than read through (a refusal, an answer short of one) is
 * no copy to vouch for: it is taken as outdated, and the page opens from
 * the server's (2026-11-11: past 2,048 mentions a copy was kept unread,
 * names of pages closed since showing).
 */
export async function copyMentionsOutdated(
  session: Session, pageId: string, doc: Y.Doc, signal?: AbortSignal,
): Promise<boolean | null> {
  const shown = shownMentions(doc);
  if (shown.some(({ reach }) => reach === "mixed")) return true;
  const shownAs = new Map(shown.map(({ mention, reach }) => [`${mention.kind}:${mention.id}`, reach]));
  try {
    // The first part answered otherwise settles it: the rest go unasked.
    for await (const { part, answer } of mentionReachParts(session, pageId, shown.map(({ mention }) => mention), signal)) {
      const now = new Map(answer.map((mention) => [`${mention.kind}:${mention.id}`, mention.reach]));
      if (part.some((mention) => now.get(`${mention.kind}:${mention.id}`) !== shownAs.get(`${mention.kind}:${mention.id}`))) return true;
    }
  } catch (error) {
    return signal?.aborted || unanswered(error) ? null : true;
  }
  return false;
}
