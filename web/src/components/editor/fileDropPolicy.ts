import { i18next } from "@/i18n";
import { isActiveContentFile, isSandboxedHtmlFile } from "@/lib/fileSecurity";
import { waitingNoteText } from "@/lib/pendingChoice";
import type { UploadProgress } from "@/lib/storage";
import type { BlockType } from "@/lib/types";

// One upload progress, the storage's (it says when the server isn't answering).
export type { UploadProgress, UploadProgressPhase } from "@/lib/storage";

export type DroppedFileBlockType = Extract<BlockType, "image" | "video" | "audio" | "pdf" | "file">;
export type BlockUploadProgress = UploadProgress & { fileName: string };

export function fileBlockType(file: File): DroppedFileBlockType {
  if (file.type.startsWith("image/")) return "image";
  if (file.type.startsWith("video/")) return "video";
  if (file.type.startsWith("audio/")) return "audio";
  if (file.type.split(";", 1)[0]?.trim().toLowerCase() === "application/pdf") return "pdf";
  return "file";
}

/**
 * The block a file given to a media block's or an embed's upload makes, as
 * Notion's (seen 2026-11-07): a picture, video, sound or PDF its own kind of
 * block, whichever block it was given to; any other file an embed of it — a
 * Word, Excel, PowerPoint or CSV file its document, a text file its words,
 * a .bin its row in the frame (ManagedEmbedBlock). An HTML or other active
 * file stays a file row, as the upload's own rules keep it.
 */
export function uploadBlockType(file: File): DroppedFileBlockType | "embed" {
  const type = fileBlockType(file);
  if (type !== "file") return type;
  return isActiveContentFile(file) || isSandboxedHtmlFile(file) ? "file" : "embed";
}

// The documents a drop from outside shows in an embed, by the name's extension
// whatever type the browser gives it, as Notion's (seen 2026-11-13 by hand
// from Finder, a .docx framed and a .pdf a row, and 2026-11-15 by a script's
// drop in two tabs each: a .docx with no type framed, a .bin typed as Word a
// row). Every other file — a PDF, .xlsx, .xltx, .pptm, .ppsx, .potx,
// OpenDocument text, slides and drawings, CSV, text, Markdown, JSON, ZIP,
// EPUB — is a row there too.
const DROPPED_EMBED_EXTENSIONS: ReadonlySet<string> = new Set([
  "doc", "docx", "docm", "dot", "dotx", "rtf", "xls", "xlsm", "xlsb", "ods", "ppt", "pptx", "key", "pages", "numbers",
]);

/**
 * The block a file dropped or pasted on a page makes: a picture, video or
 * sound its own kind; dropped, a Word, Excel, PowerPoint, OpenDocument sheet,
 * RTF or Apple iWork document of the extensions above an embed of it
 * (ManagedEmbedBlock), as Notion's; anything else a file row — a PDF too,
 * dropped or pasted (Notion's, seen 2026-11-06 and 2026-11-13), and every
 * pasted document (Notion's, seen 2026-11-15 in two tabs: a pasted .docx,
 * .pptx or .doc a row). An HTML file, which Notion's drop frames, or another
 * active file stays a row, as the upload's own rules keep it.
 */
export function insertedFileBlockType(file: File, how: "dropped" | "pasted"): DroppedFileBlockType | "embed" {
  const type = fileBlockType(file);
  if (type === "image" || type === "video" || type === "audio") return type;
  const extension = /\.([^./\\]+)$/u.exec(file.name.trim())?.[1]?.toLowerCase() ?? "";
  return how === "dropped" && DROPPED_EMBED_EXTENSIONS.has(extension) && !isActiveContentFile(file) && !isSandboxedHtmlFile(file)
    ? "embed" : "file";
}

export function blockUploadScope(type: DroppedFileBlockType) {
  if (type === "image") return "blocks/images";
  if (type === "video") return "blocks/videos";
  if (type === "audio") return "blocks/audio";
  return "blocks/files";
}

export function blockUploadProgressLabel(progress: UploadProgress) {
  if (progress.waiting) return waitingNoteText();
  if (progress.phase === "preparing") return i18next.t("blockItem:uploadPreparing");
  if (progress.phase === "finalizing") return i18next.t("blockItem:uploadFinalizing");
  if (progress.phase === "complete") return i18next.t("blockItem:uploadComplete");
  return i18next.t("blockItem:uploadUploading");
}
