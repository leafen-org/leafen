import { createPdfPreviewObjectURL } from "./pdfPreview";

const PREVIEW_MAX_BYTES = 100 * 1024 * 1024;

/** Storage keeps its attachment policy. Only passive, explicitly typed
 * browser media may be retyped locally; HTML keeps the sandboxed download. */
function previewType(contentType: string) {
  return contentType.split(";", 1)[0].trim().toLowerCase();
}

/** Whether a browser tab can show the file; any other file is a download. */
export function canPreviewPassively(contentType: string) {
  const type = previewType(contentType);
  return type === "application/pdf" || type === "text/plain" || /^image\/(png|jpeg|gif|webp|avif|bmp)$/u.test(type)
    || /^(audio|video)\/[a-z0-9.+-]+$/u.test(type);
}

export async function createPassiveFilePreviewObjectURL(url: string, contentType: string) {
  if (!canPreviewPassively(contentType)) return "";
  const type = previewType(contentType);
  if (type === "application/pdf") return createPdfPreviewObjectURL(() => url);
  // A video or sound plays from its address, in pieces as it goes: read whole
  // first, a long one kept the tab blank for minutes or failed past the limit (2026-11-05).
  if (!(type === "text/plain" || /^image\/(png|jpeg|gif|webp|avif|bmp)$/u.test(type))) return "";
  const response = await fetch(url);
  if (!response.ok) throw new Error("File preview request failed");
  const length = Number(response.headers.get("content-length"));
  if (length > PREVIEW_MAX_BYTES) throw new Error("File preview exceeds the browser limit");
  const reader = response.body?.getReader();
  if (!reader) throw new Error("File preview has no body");
  const chunks: BlobPart[] = [];
  let size = 0;
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      size += value.byteLength;
      if (size > PREVIEW_MAX_BYTES) { await reader.cancel(); throw new Error("File preview exceeds the browser limit"); }
      chunks.push(value);
    }
  } finally { reader.releaseLock(); }
  return URL.createObjectURL(new Blob(chunks, { type }));
}
