import { isManagedFileBlock, type GraphBlock, type GraphBlockType } from "@/contentGraph";
import { officeDocumentPreviewable } from "@/lib/documentKinds";
import type { BlockTurnChoice } from "./blockTurnInto";
import { ownBlockService } from "./embedServices";
import { keptBlockWidth, keptSized } from "./frameKeptWidth";

export const FILE_MENU_TYPES = new Set<GraphBlockType>(["image", "video", "audio", "file", "pdf", "embed"]);
/** Only explicit, MIME-compatible presentation changes are offered. A file
 * stays a file on upload/import, including a PDF, until the person chooses. */
export function fileBlockTurnChoices(block: GraphBlock): BlockTurnChoice[] {
  if (!isManagedFileBlock(block)) return [];
  const mime = String(block.content.contentType ?? "").split(";")[0].trim().toLowerCase();
  if (block.type !== "file") return block.type === "audio" ? [] : [{ key: "file", type: "file" }];
  // A Word, Excel or PowerPoint file turns into its embed, as Notion's (seen 2026-11-07).
  const type: GraphBlockType | null = mime.startsWith("image/") ? "image" : mime.startsWith("video/") ? "video"
    : mime.startsWith("audio/") ? "audio" : mime === "application/pdf" ? "pdf" : mime === "text/html" ? "embed"
    : officeDocumentPreviewable(String(block.content.name ?? ""), mime) ? "embed" : null;
  return type ? [{ key: type, type }] : [];
}

/**
 * A picture's, video's, PDF's or embed's alignment: the one chosen, or with
 * none chosen the middle straight under the page and the start inside another
 * block (a toggle, a column, a list), as Notion's media box (seen 2026-11-08
 * for a picture and 2026-11-13 for an embed: "align-self: center" under the
 * page, "flex-start" in a toggle and a column). Narrower than its column, a
 * box shows it; one across its column looks the same either way.
 */
export function fileBlockAlignment(value: unknown, nested = false): "left" | "center" | "right" {
  return value === "left" || value === "right" || value === "center" ? value : nested ? "left" : "center";
}

/**
 * An embed's box in px where it has no share of its column: the width
 * Notion's block API gives one it makes with no size ("blockWidth", 400 —
 * notion_block_file_request.go), or a picture's own width where its address
 * made the embed (readLinkedPicture), its frame that less its 8px sides,
 * centred and no wider than the column. A width dragged to since (keptSized) wins, and
 * a service Notion keeps as its own block spans the column however it was
 * made (seen 2026-11-11: a map made through the API, 704 × 900).
 */
export function embedBoxWidth(block: GraphBlock): number | undefined {
  const { blockWidth: box } = block.content;
  if (block.type !== "embed" || keptSized(block.content)) return undefined;
  if (typeof box !== "number" || !Number.isFinite(box) || box <= 16 || ownBlockService(block)) return undefined;
  return box;
}

/**
 * That box as the CSS width of what fills it: its frame, 8px in from each
 * side, or an empty embed's row's place, 6px in, the row keeping 2 of its own.
 * Where the column is narrower the box is the column's, which stands 6px
 * past the block's place each side, so its frame is 2px in from that place,
 * as a code block's is (Notion's, seen 2026-11-13 in a column 337 wide: the
 * box 337, its frame 321).
 */
export function embedBoxFrameWidth(box: number, inset = 8) {
  return `min(${box - 2 * inset}px, calc(100% - ${2 * inset - 12}px))`;
}

/**
 * Where an uploaded video's or PDF's frame stands with no width of its own
 * (EditorMediaBlocks, EditorFileBlocks): "page", Notion's 708px box that an
 * upload in its editor keeps, in the middle of the column and reaching past
 * it in a window narrower than the box; "column", across the column 2px in
 * as a link's, where the block API made it, which Notion gives no box (seen
 * 2026-11-15: its block "width: 100%"). Sized or aligned to a side, it keeps
 * its share and side.
 */
export function uploadedFrameBox(block: GraphBlock, managed: boolean, width: number | undefined, align: "left" | "center" | "right") {
  if (!managed || width !== undefined || keptBlockWidth(block.content) !== undefined) return undefined;
  if (block.content.columnWide === true) return "column";
  return align === "center" ? "page" : undefined;
}
