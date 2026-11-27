import { useEffect } from "react";
import type { GraphBlock } from "@/contentGraph";
import type { Session } from "@/types";
import { createUrlMetadataApi } from "@/urlMetadataApi";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { blockWebAddress } from "./mediaBlockUrl";

/**
 * The sites whose video Notion draws in its own shape, that of the still its
 * oEmbed names: Loom's (seen 2026-11-06: stills of 1024×640 and 1280×720 drawn
 * 704×440 and 704×396). YouTube's still is 4:3 with bars while Notion draws it
 * 16:9, and Vimeo isn't compared yet; both keep the 16:9 frame.
 */
function takesSiteShape(url: string) {
  try {
    return /(^|\.)loom\.com$/iu.test(new URL(url).hostname);
  } catch {
    return false;
  }
}

type ReadShape = Promise<{ videoWidth?: number; videoHeight?: number } | null>;
/**
 * One read a link in a tab: an effect run again as its editor renews joins
 * the read under way instead of starting another (2026-11-11: a Loom link
 * was asked for every few seconds while its page stayed open), and a failed
 * one is tried again when the page is opened again.
 */
const shapeReads = new Map<string, ReadShape>();
function readShape(session: Session, address: string): ReadShape {
  let read = shapeReads.get(address);
  if (!read) {
    read = createUrlMetadataApi().readUrlMetadata(session, address, undefined, { videoSize: true })
      .then((response) => response.metadata ?? null, () => null);
    shapeReads.set(address, read);
  }
  return read;
}

/**
 * The shape of a streaming site's video frame, as "width / height": read once
 * through the server's link reader and kept on the block, so it stays the
 * shape it had when it was put in, as Notion's. Unread, or without one, the
 * frame is 16:9.
 */
export function useStreamedVideoShape(adapter: NativeEditorGraphAdapter, block: GraphBlock, streamed: boolean) {
  const link = typeof block.content.url === "string" ? block.content.url : "";
  // Read at the address the block plays, one kept without its scheme over https (blockWebAddress).
  const address = blockWebAddress(link);
  const width = block.content.videoWidth, height = block.content.videoHeight;
  const shaped = typeof width === "number" && typeof height === "number" && width > 0 && height > 0;
  const readFor = typeof block.content.metadataUrl === "string" ? block.content.metadataUrl : "";
  const wanted = streamed && Boolean(address) && takesSiteShape(address);
  useEffect(() => {
    if (!wanted || shaped || readFor === link || adapter.readOnly || !adapter.session) return;
    let shown = true;
    void readShape(adapter.session, address)
      .then((found) => {
        // A page without a size is kept as read.
        if (!found || !shown || !adapter.isCurrent() || adapter.signal.aborted) return;
        const live = adapter.graph.blocks.find((candidate) => candidate.id === block.id);
        if (!live || live.content.url !== link) return;
        const shape = { videoWidth: found.videoWidth ?? 0, videoHeight: found.videoHeight ?? 0, metadataUrl: link };
        if (adapter.fillBlock) adapter.fillBlock(live, shape);
        else adapter.updateBlock(live, shape);
      });
    return () => { shown = false; };
  }, [adapter, address, block.id, link, readFor, shaped, wanted]);
  return wanted && shaped ? `${width} / ${height}` : undefined;
}
