import { activePersistentGeneratedLabels } from "@/lib/persistentGeneratedLabels";
import { nativeWorkspaceFileLocator } from "@/lib/workspaceFileTransport";
import { blockWebAddress } from "../editor/mediaBlockUrl";

const IMAGE_EXT = /\.(avif|gif|jpe?g|png|svg|webp)(?:[?#].*)?$/i;
const PDF_EXT = /\.pdf(?:[?#].*)?$/i;

export interface FileAttachment {
  id: string;
  source?: "external";
  fileId?: string;
  key?: string;
  uploadId?: string;
  bucket?: string;
  name: string;
  url: string;
  type?: string;
  contentType?: string;
  size?: number;
  sha256?: string;
  sourceUrl?: string;
  notionFileSource?: string;
  notionFileExpiryTime?: string;
  notionFileCopied?: boolean;
}

function asString(value: unknown) {
  return typeof value === "string" ? value.trim() : "";
}

function nameFromDataUrl(url: string) {
  const labels = activePersistentGeneratedLabels();
  const match = /^data:([^;,]+)/.exec(url);
  if (!match) return labels.uploadedFile;
  if (match[1].startsWith("image/")) return labels.image;
  return match[1].split("/").at(-1) || labels.uploadedFile;
}

function safeDecode(value: string) {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}

export function fileNameFromUrl(url: string) {
  const untitled = activePersistentGeneratedLabels().untitled;
  const value = url.trim();
  if (!value) return untitled;
  if (value.startsWith("data:")) return nameFromDataUrl(value);
  try {
    const parsed = new URL(value);
    const pathname = parsed.pathname.split("/").filter(Boolean).at(-1) ?? "";
    return safeDecode(pathname) || parsed.hostname || untitled;
  } catch {
    return safeDecode(value.split(/[/?#]/).filter(Boolean).at(-1) ?? "") || untitled;
  }
}

function normalizeOne(value: unknown): FileAttachment | null {
  if (!value) return null;
  if (typeof value === "string") {
    const url = value.trim();
    return url ? { id: url, name: fileNameFromUrl(url), url } : null;
  }
  if (typeof value !== "object") return null;

  const record = value as Record<string, unknown>;
  const source = record.source === "external" ? "external" as const : undefined;
  // A linked file keeps its address and name as written, spaces too, as
  // Notion's API keeps them ("a b.com", "   "), so writing the cell back
  // keeps them; what opens of it is fileAttachmentAddress's.
  const given = (field: unknown) => (source && typeof field === "string" ? field : "");
  const fileId = asString(record.fileId);
  const key = asString(record.key) || asString(record.fileKey) || fileId;
  const uploadId = asString(record.uploadId) || asString(record.fileUploadId);
  const bucket = asString(record.bucket) || asString(record.fileBucket);
  const sourceUrl = asString(record.sourceUrl);
  const url = given(record.url) || asString(record.url) || asString(record.src) || asString(record.href) || sourceUrl
    || (fileId ? nativeWorkspaceFileLocator(fileId) : key);
  if (!url && !key && !uploadId) return null;

  const id = asString(record.id) || fileId || uploadId || key || url;
  const name = given(record.name) || asString(record.name) || asString(record.fileName) || fileNameFromUrl(url);
  const type = asString(record.type) || asString(record.mimeType) || asString(record.contentType) || undefined;
  const contentType = asString(record.contentType) || type;
  const size = typeof record.size === "number" && Number.isFinite(record.size)
    ? record.size
    : undefined;
  const notionFileSource = asString(record.notionFileSource) || undefined;
  const notionFileExpiryTime = asString(record.notionFileExpiryTime) || undefined;
  const notionFileCopied = typeof record.notionFileCopied === "boolean" ? record.notionFileCopied : undefined;
  const sha256 = asString(record.sha256) || undefined;

  return {
    id,
    source,
    fileId: fileId || undefined,
    key: key || undefined,
    uploadId: uploadId || undefined,
    bucket: bucket || undefined,
    name,
    url,
    type,
    contentType,
    size,
    sha256,
    sourceUrl: sourceUrl || undefined,
    notionFileSource,
    notionFileExpiryTime,
    notionFileCopied,
  };
}

export function graphFilePropertyValue(value: unknown): Array<
  | { source: "external"; url: string; name: string }
  | { fileId: string; name: string; size: number; contentType: string; sha256: string }
> {
  const result: ReturnType<typeof graphFilePropertyValue> = [];
  for (const file of normalizeFileAttachments(value)) {
    if (file.source === "external") {
      result.push({ source: "external", url: file.url, name: file.name });
      continue;
    }
    if (!file.fileId || file.size == null || !file.contentType || !file.sha256) continue;
    result.push({
      fileId: file.fileId,
      name: file.name,
      size: file.size,
      contentType: file.contentType,
      sha256: file.sha256,
    });
  }
  return result;
}

export function normalizeFileAttachments(value: unknown): FileAttachment[] {
  const values = Array.isArray(value) ? value : value ? [value] : [];
  return values
    .map(normalizeOne)
    .filter((item): item is FileAttachment => item !== null);
}

/**
 * Where a file of a files property shows and opens from: an uploaded file's
 * own address; a linked one's, which may be any text, only when written as
 * a web address with its scheme, as Notion's (seen 2026-11-11: a file at
 * "https://example.com/only.pdf" opens from its chip, its line and "원본
 * 보기", one at "abc" or "example.com" from none of them and draws no link)
 * — never a path or page of this app nor another scheme (blockWebAddress);
 * "" for none.
 */
export function fileAttachmentAddress(file: FileAttachment) {
  if (file.source !== "external") return file.url;
  return /^https?:\/\//iu.test(file.url.trim()) ? blockWebAddress(file.url) : "";
}

export function isImageAttachment(file: FileAttachment) {
  return (
    file.type?.startsWith("image/") ||
    file.url.startsWith("data:image/") ||
    IMAGE_EXT.test(file.name) ||
    IMAGE_EXT.test(file.url)
  );
}

export function isPreviewableImageAttachment(file: FileAttachment) {
  const type = file.type?.split(";", 1)[0]?.trim().toLowerCase();
  if (type === "image/svg+xml") return false;
  if (file.url.toLowerCase().startsWith("data:image/svg+xml")) return false;
  if (/\.svg(?:[?#].*)?$/i.test(file.name) || /\.svg(?:[?#].*)?$/i.test(file.url)) return false;
  return isImageAttachment(file);
}

export function isPdfAttachment(file: FileAttachment) {
  return (
    file.type?.split(";", 1)[0]?.trim().toLowerCase() === "application/pdf" ||
    file.url.toLowerCase().startsWith("data:application/pdf") ||
    PDF_EXT.test(file.name) ||
    PDF_EXT.test(file.url)
  );
}

export function firstImageAttachment(value: unknown) {
  return normalizeFileAttachments(value).find(isImageAttachment);
}

/** A size as Notion writes it (seen 2026-11-05): binary units, one decimal at most — "897 B", "1.5 KiB", "120.6 KiB", "6.7 MiB". */
export function formatFileSize(size?: number) {
  if (size == null || !Number.isFinite(size)) return "";
  if (size < 1024) return `${size} B`;
  const units = ["KiB", "MiB", "GiB", "TiB"];
  let value = size / 1024;
  let unit = 0;
  while (Number(value.toFixed(1)) >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${Number(value.toFixed(1))} ${units[unit]}`;
}
