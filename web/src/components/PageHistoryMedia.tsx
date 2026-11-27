import { useMemo } from "react";
import type { GraphBlock } from "@/contentGraph";
import { loadSession } from "@/lib/authSessionStore";
import type { PageHistoryBlock } from "@/pageHistoryWire.generated";
import { ImageBlock, MediaBlock } from "./editor/MediaBlockContent";
import { PdfBlock } from "./editor/PdfBlock";
import { safeMediaUrl } from "./editor/mediaBlockUrl";
import type { NativeEditorGraphAdapter } from "./editor/editorRuntimeTypes";

// A version's image, PDF, video or audio block shows as the page shows it,
// as Notion's preview draws them (seen 2026-10-18: a PDF as its viewer, an
// image as its picture); a file block stays its row.
const HISTORY_MEDIA_TYPES = new Set(["image", "pdf", "video", "audio"]);

/** Whether a version's block is media with something to show. */
export function historyMediaShown(block: PageHistoryBlock) {
  if (!HISTORY_MEDIA_TYPES.has(block.type)) return false;
  const fileId = typeof block.content.fileId === "string" ? block.content.fileId : "";
  return Boolean(fileId || safeMediaUrl(block.content.url));
}

/**
 * The page's own block, read-only: in that state the image, player and PDF
 * views read only the session and that they can't change the block.
 */
export function PageHistoryMedia({ block }: { block: PageHistoryBlock }) {
  const session = loadSession();
  const adapter = useMemo(
    () => ({ readOnly: true, session }) as unknown as NativeEditorGraphAdapter,
    [session?.token],
  );
  const graphBlock = block as unknown as GraphBlock;
  if (block.type === "image") return <ImageBlock adapter={adapter} block={graphBlock} />;
  if (block.type === "pdf") return <PdfBlock adapter={adapter} block={graphBlock} />;
  return <MediaBlock adapter={adapter} block={graphBlock} />;
}
