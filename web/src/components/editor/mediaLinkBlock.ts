import type { GraphBlock } from "@/contentGraph";
import { createUrlMetadataApi } from "@/urlMetadataApi";
import { blockItemText } from "./blockItemLabels";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { embedLinkMakesBookmark } from "./embedServices";
import { givenWebLink } from "./mediaBlockUrl";
import { blockTypeForPastedAssetUrl } from "./mediaEmbeds";
import { keptSized } from "./frameKeptWidth";

/**
 * A link given in an empty picture's, video's, sound's or PDF's link window
 * makes the block what the link is, as Notion's windows do (measured
 * 2026-11-16 in Chrome at 1440): in the picture's window "a example.dev"
 * became an embed (seen by two workers in tabs an hour apart), as did
 * "https://example.com/" (twice) and "https://example.com/a"; "…/a.png"
 * stayed a picture, a YouTube page and "…/a.mp4" became a video and
 * "…/a.pdf" a PDF. The video's window made a picture of "…/a.png", and the
 * video's, the sound's and the PDF's each an embed of "https://example.com/"
 * (seen once each). A file's window keeps any link as its file (seen once).
 * The kind is read from the address first, as a pasted link's is
 * (blockTypeForPastedAssetUrl). An address with no file ending is taken at
 * once as the embed it most often is (a link a site refuses to be framed by
 * as a bookmark, as an embed's), and then read through the server
 * (readLinkedPicture). Returns the window's refusal, "" when taken.
 */
export function giveMediaLink(adapter: NativeEditorGraphAdapter, block: GraphBlock, value: string, invalidKey: string,
  content: Record<string, unknown> = {}) {
  const url = givenWebLink(value);
  if (!url) return blockItemText(invalidKey);
  const known = blockTypeForPastedAssetUrl(url);
  const kind = known ?? (embedLinkMakesBookmark(url) ? "bookmark" : "embed");
  if (kind === block.type) adapter.updateBlock(block, { url, ...content });
  else adapter.replaceBlocks([{ ...block, type: kind, content: { url } }]);
  if (!known && kind === "embed") void readLinkedPicture(adapter, block.id, url);
  return "";
}

/**
 * The box, in px, Notion gives the picture an SVG link became (its
 * "block_width" 400 with "block_preserve_scale"): the picture no wider than
 * the box less its 8px sides, at its own size where it is narrower, at the
 * box's start (measured 2026-11-16 in Chrome at 1440: "placehold.co/600x400"
 * from the picture's window and from the embed's a 400 block, the picture
 * 384 × 256, by two workers in tabs hours apart; "placehold.co/200x100" a 400
 * block, the picture 200 × 100 at its start, seen once).
 */
export const LINKED_SVG_BOX = 400;

/**
 * An address with no file ending, given to a picture's, video's, sound's,
 * PDF's or embed's link window, is read through the server once taken as an
 * embed: one whose server answers it is an SVG picture becomes a picture in
 * Notion's 400px box (LINKED_SVG_BOX), as Notion's windows make one of it,
 * where any other picture behind such an address stays an embed in a box of
 * the picture's own size, its frame the picture's height and its block its
 * width, no wider than the column (Notion's "block_width" and
 * "block_height"; measured 2026-11-16 in Chrome at 1440 in two tabs:
 * "placehold.co/600x400", "placehold.co/300x200" and "httpbin.org/image/svg",
 * all image/svg+xml, became pictures; "dummyimage.com/300x200",
 * "via.placeholder.com/150" and "httpbin.org/image/png", all image/png,
 * embeds of 300 × 216, 150 × 166 and 100 × 116 — from the embed's window
 * too —, "dummyimage.com/400x1500" 400 × 1516, "dummyimage.com/800x300" and
 * "…/1200x900" the column wide, 316 and 916 high). The embed it was taken as
 * changes only while it is still that embed of that link, never sized.
 */
export async function readLinkedPicture(adapter: NativeEditorGraphAdapter, blockId: string, url: string) {
  if (!adapter.session) return;
  const found = await createUrlMetadataApi().readUrlMetadata(adapter.session, url, adapter.signal)
    .then((response) => response.metadata, () => null);
  const type = found?.contentType ?? "";
  if (!type.startsWith("image/") || adapter.signal.aborted || adapter.readOnly) return;
  const unsized = () => {
    const block = adapter.graph.blocks.find((candidate) => candidate.id === blockId);
    return block?.type === "embed" && block.content.url === url && !keptSized(block.content) ? block : undefined;
  };
  if (type === "image/svg+xml") {
    const block = unsized();
    if (block) adapter.replaceBlocks([{ ...block, type: "image", content: { url, blockWidth: LINKED_SVG_BOX } }]);
    return;
  }
  const size = await pictureSize(url, adapter.signal);
  const block = unsized();
  if (!size || !block || adapter.signal.aborted || adapter.readOnly) return;
  const box = { blockWidth: size.width, height: size.height };
  if (adapter.fillBlock) adapter.fillBlock(block, box);
  else adapter.updateBlock(block, box);
}

/** A picture's own pixel size, read by loading it as the page would; null when it does not load. */
function pictureSize(url: string, signal: AbortSignal) {
  return new Promise<{ width: number; height: number } | null>((resolve) => {
    const image = new Image();
    const done = (size: { width: number; height: number } | null) => {
      signal.removeEventListener("abort", stop);
      image.onload = image.onerror = null;
      resolve(size);
    };
    const stop = () => { image.src = ""; done(null); };
    image.onload = () => done(image.naturalWidth > 0 && image.naturalHeight > 0
      ? { width: image.naturalWidth, height: image.naturalHeight } : null);
    image.onerror = () => done(null);
    signal.addEventListener("abort", stop, { once: true });
    image.referrerPolicy = "no-referrer";
    image.src = url;
  });
}

/** The px box of a picture a link window made from an SVG (LINKED_SVG_BOX), where no drag has sized it since. */
export function linkedPictureBox(block: GraphBlock): number | undefined {
  const { blockWidth: box } = block.content;
  if (block.type !== "image" || keptSized(block.content)) return undefined;
  return typeof box === "number" && Number.isFinite(box) && box > 16 ? box : undefined;
}
