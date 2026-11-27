const PDF_HEADER = [0x25, 0x50, 0x44, 0x46, 0x2d] as const;
const PDF_HEADER_SCAN_BYTES = 1024;
const PDF_PREVIEW_MAX_BYTES = 100 * 1024 * 1024;
const PDF_OBJECT_URL_LIFETIME_MS = 5 * 60 * 1000;

function hasPdfHeader(bytes: Uint8Array) {
  const scanLength = Math.min(bytes.length, PDF_HEADER_SCAN_BYTES);
  for (let index = 0; index <= scanLength - PDF_HEADER.length; index += 1) {
    if (PDF_HEADER.every((value, offset) => bytes[index + offset] === value)) return true;
  }
  return false;
}

function replacePreviewLocation(previewWindow: Window, url: string) {
  previewWindow.location.replace(url);
}

function declaredContentLength(response: Response) {
  const rawLength = response.headers.get("content-length");
  if (rawLength === null) return null;
  const length = Number(rawLength);
  return Number.isSafeInteger(length) && length >= 0 ? length : null;
}

async function collectPdfChunks(response: Response) {
  const declaredLength = declaredContentLength(response);
  if (declaredLength !== null && declaredLength > PDF_PREVIEW_MAX_BYTES) {
    throw new Error("PDF preview exceeds the browser limit");
  }

  const reader = response.body?.getReader();
  if (!reader) throw new Error("PDF response has no readable body");

  const chunks: BlobPart[] = [];
  const headerProbe = new Uint8Array(PDF_HEADER_SCAN_BYTES);
  let headerLength = 0;
  let totalLength = 0;

  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      if (!(value instanceof Uint8Array)) throw new Error("Invalid PDF response chunk");
      if (totalLength + value.byteLength > PDF_PREVIEW_MAX_BYTES) {
        await reader.cancel();
        throw new Error("PDF preview exceeds the browser limit");
      }

      totalLength += value.byteLength;
      chunks.push(value);
      const remainingHeaderBytes = PDF_HEADER_SCAN_BYTES - headerLength;
      if (remainingHeaderBytes > 0) {
        const copyLength = Math.min(remainingHeaderBytes, value.byteLength);
        headerProbe.set(value.subarray(0, copyLength), headerLength);
        headerLength += copyLength;
      }
    }
  } finally {
    reader.releaseLock();
  }

  if (!hasPdfHeader(headerProbe.subarray(0, headerLength))) {
    throw new Error("Attachment is not a PDF");
  }
  return chunks;
}

export async function createPdfPreviewObjectURL(
  resolveUrl: () => Promise<string> | string,
) {
  const url = (await resolveUrl()).trim();
  if (!url) throw new Error("Missing PDF URL");
  const response = await fetch(url);
  if (!response.ok) throw new Error(`PDF request failed with ${response.status}`);
  return createPdfPreviewObjectURLFromResponse(response);
}

/** A fetched PDF as a local PDF the browser's viewer draws, checked to be a PDF. */
export async function createPdfPreviewObjectURLFromResponse(response: Response) {
  const chunks = await collectPdfChunks(response);
  return URL.createObjectURL(new Blob(chunks, { type: "application/pdf" }));
}

/** A fetched PDF's bytes, checked to be a PDF and no larger than a preview takes. */
export async function readPdfBytes(response: Response) {
  return new Blob(await collectPdfChunks(response)).arrayBuffer();
}

/**
 * Opens a PDF in a browser-owned viewer while retaining the native product's
 * forced-download policy for active or unknown storage content.
 *
 * A blank tab is reserved synchronously so popup blockers see the user's
 * gesture. The attachment is streamed once into a bounded collection, checked
 * for a PDF signature, and re-typed as a local PDF Blob for the browser viewer.
 * If collection is unsafe or unavailable, the tab falls back to the direct URL.
 */
export async function openPdfInNewTab(
  resolveUrl: () => Promise<string> | string,
  fileName: string,
  onViewerReady?: () => void
) {
  const previewWindow = window.open("about:blank", "_blank");
  if (!previewWindow) return false;

  previewWindow.opener = null;
  try {
    previewWindow.document.title = fileName;
  } catch {
    // The initial blank page can be cross-origin in hardened browser modes.
  }

  let url = "";
  try {
    url = (await resolveUrl()).trim();
    const objectUrl = await createPdfPreviewObjectURL(() => url);
    window.setTimeout(() => URL.revokeObjectURL(objectUrl), PDF_OBJECT_URL_LIFETIME_MS);
    onViewerReady?.();
    replacePreviewLocation(previewWindow, objectUrl);
    return true;
  } catch {
    if (url && !previewWindow.closed) {
      replacePreviewLocation(previewWindow, url);
      return true;
    }
    previewWindow.close();
    return false;
  }
}
