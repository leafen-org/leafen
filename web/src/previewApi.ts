import type { request as apiRequest } from "./apiTransport";
import type { Session } from "./types";
import type {
  BlockMentionPreviewResponse,
  DatabaseMentionPreviewResponse,
  DataSourceMentionPreviewResponse,
  PagePreviewResponse,
} from "./workspaceSearchWire.generated";

/** The previews a mention's card, its name and search's preview read. */
export function createPreviewApi(request: typeof apiRequest) {
  return {
    /**
     * A page's title, icon and first lines, for the card its mention shows on
     * hover; its first blocks too for search's preview; whether it is in the
     * trash and who sent it there, and, with trash, one there the reader may
     * open, as a mention reads it.
     */
    readPagePreview: (session: Session, pageId: string, options: { signal?: AbortSignal; blocks?: boolean; trash?: boolean } = {}) => {
      const query = [options.blocks ? "blocks=1" : "", options.trash ? "trash=1" : ""].filter(Boolean).join("&");
      return request<PagePreviewResponse>(`/pages/${encodeURIComponent(pageId)}/preview${query ? `?${query}` : ""}`, { signal: options.signal }, session);
    },
    /** A mentioned database's name and the page holding it, for its mention to open and show. */
    readDatabaseMentionPreview: (session: Session, containerId: string) => request<DatabaseMentionPreviewResponse>(
      `/database-containers/${encodeURIComponent(containerId)}/preview`,
      {},
      session,
    ),
    /** A mentioned data source's name, icon and database at its view, for its mention to show and open. */
    readDataSourceMentionPreview: (session: Session, dataSourceId: string) => request<DataSourceMentionPreviewResponse>(
      `/data-sources/${encodeURIComponent(dataSourceId)}/mention-preview`,
      {},
      session,
    ),
    /** A mentioned block's words now and its page's title, for its mention to show as Notion's. */
    readBlockMentionPreview: (session: Session, pageId: string, blockId: string) => request<BlockMentionPreviewResponse>(
      `/pages/${encodeURIComponent(pageId)}/blocks/${encodeURIComponent(blockId)}/mention-preview`,
      {},
      session,
    ),
  };
}
