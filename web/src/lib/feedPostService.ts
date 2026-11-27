import { api } from "@/api";
import {
  commentBounds,
  type CommentActorProfile,
  type CommentReaction,
  type CommentRecord,
} from "@/commentWire.generated";
import { compareGraphOrder, materializeContentGraph, type GraphBlockType } from "@/contentGraph";
import type { GraphReadItem } from "@/contentGraphWire.generated";
import { readDatabaseRowPage } from "@/databaseRowPageApi";
import type { DatabaseRowPageBodyBlock } from "@/databaseRowPageWire.generated";
import type { PageHistoryDatabase } from "@/pageHistoryWire.generated";
import { sendContentGraphChange } from "@/lib/contentGraphSend";
import { newId } from "@/lib/ids";
import type { TextSpan } from "@/lib/types";
import { pageAnalyticsBounds, type PageAnalyticsRecentViewer } from "@/pageAnalyticsWire.generated";
import type { Session } from "@/types";

/** A post's viewers: how many people viewed it, and the last few, newest first. */
export type FeedPostViewers = { count: number; recent: PageAnalyticsRecentViewer[] };

/** A feed post shows at most this many body blocks before "Open". */
export const FEED_POST_BODY_BLOCKS = 24;
/** A database in a feed post shows at most this many rows; the page shows them all. */
const FEED_POST_DATABASE_ROWS = 8;
/** How many graph pages a post's databases are read from, at most. */
const FEED_POST_GRAPH_READS = 4;

export type FeedPostBody = {
  blocks: DatabaseRowPageBodyBlock[];
  truncated: boolean;
};

export type FeedPostComments = {
  items: CommentRecord[];
  authors: Record<string, string>;
  nextCursor: string | null;
  /** The post's own reactions, read with its first comments. */
  reactions: CommentReaction[];
};

type FeedPostTransport = Pick<typeof api,
  "listDatabaseRowComments" | "createDatabaseRowComment" | "setDatabaseRowPageReaction" | "getRowPageViews" | "contentGraph">;

function authorNames(profiles: readonly CommentActorProfile[]) {
  return Object.fromEntries(profiles.map((profile) => [profile.userId, profile.displayName]));
}

/**
 * Reads a feed post's body (the row page's first blocks) and its comments,
 * and adds a comment. The row page and comment services stay the owners.
 */
export function createFeedPostService(
  session: Session,
  transport: FeedPostTransport = api,
  readBody: typeof readDatabaseRowPage = readDatabaseRowPage,
) {
  return {
    /** Who reads the feed: their own reactions show in blue. */
    userId: session.user.id,
    /** Their name, by their face beside the comment box. */
    userName: session.user.name || session.user.email,

    async readBody(rowId: string, signal: AbortSignal): Promise<FeedPostBody> {
      const snapshot = await readBody(session, rowId, { limit: FEED_POST_BODY_BLOCKS, signal });
      return { blocks: snapshot.body, truncated: snapshot.hasMore };
    },

    /**
     * The databases in a post's body, by their block, as their tables show
     * them: title, properties and first rows, read from the page's own
     * content (the same the page opens with).
     */
    async readDatabases(rowId: string, blockIds: readonly string[], signal: AbortSignal): Promise<Map<string, PageHistoryDatabase>> {
      const items: GraphReadItem[] = [];
      let cursor: string | null = null;
      for (let read = 0; read < FEED_POST_GRAPH_READS; read += 1) {
        if (signal.aborted) break;
        const page = await transport.contentGraph(session, rowId, cursor);
        items.push(...page.items);
        if (!page.hasMore || !page.nextCursor) break;
        cursor = page.nextCursor;
      }
      const graph = materializeContentGraph(items);
      const databases = new Map<string, PageHistoryDatabase>();
      for (const id of blockIds) {
        const block = graph.blocks.find((candidate) => candidate.id === id);
        const sourceId = String(block?.content.dataSourceId ?? "");
        const source = graph.dataSources.find((candidate) => candidate.id === sourceId);
        if (!block || !source) continue;
        const rows = graph.rows.filter((row) => row.dataSourceId === sourceId && !row.archivedAt && !row.subitemParentId)
          .sort(compareGraphOrder);
        databases.set(id, {
          blockId: id, title: source.title, fullPage: false,
          properties: graph.properties.filter((property) => property.dataSourceId === sourceId)
            .map(({ id: propertyId, name, type, config }) => ({ id: propertyId, name, type, config })),
          rows: rows.slice(0, FEED_POST_DATABASE_ROWS).map((row) => ({ id: row.id, values: row.values })),
          hasMoreRows: rows.length > FEED_POST_DATABASE_ROWS,
        });
      }
      return databases;
    },

    /**
     * Each post's viewers — how many people viewed it and the last few — for
     * the posts whose analytics the member may read; others are left out.
     */
    async readViews(workspaceId: string, rowIds: readonly string[], signal: AbortSignal): Promise<Record<string, FeedPostViewers>> {
      const views: Record<string, FeedPostViewers> = {};
      for (let start = 0; start < rowIds.length; start += pageAnalyticsBounds.batchObservations) {
        const response = await transport.getRowPageViews(
          session, workspaceId, rowIds.slice(start, start + pageAnalyticsBounds.batchObservations), signal,
        );
        for (const row of response.rows) views[row.rowId] = { count: row.uniqueViews, recent: row.recentViewers ?? [] };
      }
      return views;
    },

    async listComments(rowId: string, signal: AbortSignal, cursor?: string): Promise<FeedPostComments> {
      const response = await transport.listDatabaseRowComments(
        session, rowId, { cursor, limit: commentBounds.listPage }, signal,
      );
      return {
        items: response.items,
        authors: authorNames(response.actorProfiles),
        nextCursor: response.hasMore ? response.nextCursor ?? null : null,
        reactions: response.pageReactions ?? [],
      };
    },

    /**
     * Opens or shuts a toggle of the post's row page there, as the page's
     * own caret does (content.collapsed): what changed from `known`, its
     * content as last read or written here, the server keeping the rest as
     * it stands (content_graph_block_merge.go). Answers the content written.
     */
    async setToggleCollapsed(rowId: string, block: DatabaseRowPageBodyBlock, known: Record<string, unknown>, collapsed: boolean) {
      const content = { ...known, collapsed };
      await sendContentGraphChange(session, rowId, [{ kind: "upsertBlock", block: {
        id: block.id, parentId: block.parentId, afterId: null, position: block.position,
        type: block.type as GraphBlockType, content, baseContent: known,
      } }]);
      return content;
    },

    /** Adds or takes back the reader's emoji on the post; answers with all its reactions. */
    async setReaction(rowId: string, emoji: string, reacted: boolean, signal?: AbortSignal) {
      return (await transport.setDatabaseRowPageReaction(session, rowId, { emoji, reacted }, signal)).reactions;
    },

    /**
     * Adds a comment, from its text or from its marks and mentions, with the
     * files it holds, and the name it shows under: the signed-in member's.
     * `id` is the one it was shown under before it went (FeedPost).
     */
    async createComment(rowId: string, text: string | TextSpan[], attachmentIds: readonly string[] = [], signal?: AbortSignal, id = newId()) {
      const rich = typeof text === "string" ? [{ text }] : text;
      const response = await transport.createDatabaseRowComment(session, rowId, {
        id, requestId: `feed-comment:${id}`, body: { rich, ...(attachmentIds.length > 0 ? { attachmentIds: [...attachmentIds] } : {}) },
      }, signal);
      return { comment: response.comment, authors: { [response.comment.authorId]: session.user.name || session.user.email } };
    },
  };
}

export type FeedPostService = ReturnType<typeof createFeedPostService>;

function spansText(value: unknown) {
  if (!Array.isArray(value)) return "";
  return value.map((span) => (
    span && typeof span === "object" && typeof (span as { text?: unknown }).text === "string"
      ? (span as { text: string }).text
      : ""
  )).join("");
}

/** The text of a row page block: its rich spans, or its plain text. */
export function feedPostBlockText(block: Pick<DatabaseRowPageBodyBlock, "content">) {
  const content = block.content ?? {};
  return spansText(content.rich) || (typeof content.text === "string" ? content.text : "");
}
