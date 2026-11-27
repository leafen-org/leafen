import { setBlockRichText } from "@/collaboration";
import type { GraphBlock } from "@/contentGraph";
import { readBookmarkMetadata } from "./bookmarkMetadata";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { focusEditableNextFrame } from "./focus";
import { linkMentionSpan, type LinkPreview } from "./linkMention";

const text = (value: unknown) => typeof value === "string" ? value : "";

/**
 * A bookmark's "멘션으로 전환", as Notion's (seen 2026-10-11, 2026-11-11):
 * the block becomes a line holding a mention of its link — the same block,
 * its caption gone — named by the page's title, its icon before it and the
 * site's name in grey where the page gives one, keeping what the card shows
 * of the page for the mention's card (linkMention.ts). A card that shows no
 * name of its page (one whose address the block API wrote), or one read
 * before the site's name was kept, has its page read first, as Notion names
 * it (seen 2026-11-11: an API-made card of www.wikipedia.org mentioned as
 * "Wikipedia, the free encyclopedia" with its icon, one of github.com as
 * "GitHub" before its title); one that can't be read is named by what it
 * shows, or by its link.
 */
export function turnBookmarkIntoMention(adapter: NativeEditorGraphAdapter, block: GraphBlock, url: string) {
  const collaboration = adapter.collaborationDocument;
  if (adapter.readOnly || !collaboration) return;
  const mention = (preview: LinkPreview) => {
    adapter.changeType([block.id], "paragraph");
    setBlockRichText(collaboration, block.id, [linkMentionSpan(url, preview)]);
    focusEditableNextFrame(block.id, "end");
  };
  const shown: LinkPreview = {
    title: text(block.content.title), description: text(block.content.description),
    iconUrl: text(block.content.iconUrl), imageUrl: text(block.content.imageUrl), provider: text(block.content.provider),
  };
  if (shown.title?.trim() && typeof block.content.provider === "string") return mention(shown);
  void readBookmarkMetadata(adapter, url).then((found) => {
    const live = adapter.graph.blocks.find((candidate) => candidate.id === block.id);
    if (!live || live.type !== "bookmark" || live.content.url !== block.content.url
      || adapter.readOnly || !adapter.isCurrent() || adapter.signal.aborted) return;
    mention(found?.title ? found : shown);
  });
}
