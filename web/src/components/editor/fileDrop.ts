import { i18next } from "@/i18n";
import { logProductError } from "@/lib/safeErrorLog";
import { isServerUnreachable } from "@/lib/settingsErrors";
import { uploadRefusalReason } from "@/lib/uploadRefusal";

export {
  blockUploadProgressLabel,
  blockUploadScope,
  fileBlockType,
  insertedFileBlockType,
  uploadBlockType,
  type BlockUploadProgress,
  type DroppedFileBlockType,
  type UploadProgress,
  type UploadProgressPhase,
} from "./fileDropPolicy";

export type FileDropBounds = {
  types: number;
};

/** How many kinds of data a drag of files may carry, wherever files are
 * taken: page blocks and file cells. A pick or drop adds every file it holds. */
export const FILE_DROP_BOUNDS: FileDropBounds = { types: 32 };

export type UploadErrorIdentity = "too_large" | "unsafe" | "unknown";

function validDropBound(bound: number) {
  return Number.isSafeInteger(bound) && bound >= 0;
}

/** Whether a drag from outside carries files, and no more kinds of data than
 * the caller allows; the files themselves can be read only at the drop. */
export function externalDragHasFiles(types: readonly string[], bounds: FileDropBounds) {
  if (!validDropBound(bounds.types) || types.length > bounds.types) return false;
  return types.includes("Files");
}

/** A drop's files that hold something, however many. */
export function droppedFiles(files: readonly File[]) {
  return files.filter((file) => file.size > 0);
}

export function blockUploadErrorMessage(
  error: unknown,
  fileName: string,
  identity: UploadErrorIdentity,
) {
  if (identity === "too_large") return i18next.t("blockItem:uploadFileTooLarge");
  if (identity === "unsafe") return i18next.t("blockItem:uploadUnsafeFileType");
  // An upload isn't kept to send later: offline, it says so once, in words.
  if (isServerUnreachable(error)) return i18next.t("blockItem:uploadServerUnreachable", { fileName });
  const reason = uploadRefusalReason(error);
  if (reason) return reason;
  const message = error instanceof Error ? error.message.trim() : "";
  if (message) logProductError("error", "file-drop.upload", error);
  return fileName
    ? i18next.t("blockItem:uploadFailed", { fileName })
    : i18next.t("blockItem:uploadFailedUnknown");
}
