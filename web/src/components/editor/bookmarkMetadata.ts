import { createUrlMetadataApi } from "@/urlMetadataApi";
import { pageIdFromPageHref } from "@/lib/pageLinks";
import { pageNamesNow } from "@/lib/pageNames";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";

export type BookmarkMetadata = { title: string; description: string; iconUrl: string; imageUrl: string; provider: string; metadataUrl: string };

/** A bookmark whose page is not read (yet): an address changed by hand or a page that gave nothing. */
export const UNREAD_BOOKMARK: BookmarkMetadata = { title: "", description: "", iconUrl: "", imageUrl: "", provider: "", metadataUrl: "" };

/**
 * What a bookmark's card shows of its page, read once through the server's
 * guarded reader, as Notion's card (seen 2026-10-17, 2026-11-05): its name,
 * description, icon and picture; a page of this server its own name. A page
 * that can't be read gives nothing, and the card names its host, as Notion's.
 * The site's name it keeps for its mention (linkMention.ts).
 */
export async function readBookmarkMetadata(adapter: NativeEditorGraphAdapter, url: string, signal?: AbortSignal): Promise<BookmarkMetadata | null> {
  const fields = (found: { title?: string; description?: string; iconUrl?: string; imageUrl?: string; siteName?: string } | null): BookmarkMetadata => ({
    title: found?.title?.trim() ?? "",
    description: found?.description?.trim() ?? "",
    iconUrl: found?.iconUrl && /^https?:/iu.test(found.iconUrl) ? found.iconUrl : "",
    imageUrl: found?.imageUrl && /^https?:/iu.test(found.imageUrl) ? found.imageUrl : "",
    provider: found?.siteName?.trim() ?? "",
    metadataUrl: url,
  });
  const pageId = pageIdFromPageHref(url);
  const page = pageId ? adapter.moveContext?.pages.find((candidate) => candidate.id === pageId && !candidate.inTrash) : undefined;
  if (page) return fields({ title: pageNamesNow.title(page) });
  if (!adapter.session) return null;
  const found = await createUrlMetadataApi().readUrlMetadata(adapter.session, url, signal)
    .then((response) => response.metadata, () => null);
  return signal?.aborted ? null : fields(found);
}
