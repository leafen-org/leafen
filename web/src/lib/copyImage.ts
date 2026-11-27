import { i18next } from "@/i18n";
import { loadSession } from "@/lib/authSessionStore";
import { appToastStack } from "@/lib/productToast";
import { isActiveServerOrigin, serverOrigin } from "@/lib/serverOrigin";

// The one way the screens put a picture on the clipboard — an image block's
// "이미지 복사", the Mac app's right-press "이미지 복사", a chart's "PNG로 복사" —
// as Notion's web client and app do (their code, read 2026-11-15). The write
// starts at the press with the picture still to come: WebKit (Safari, the Mac
// app) copies only what a press starts, so the picture is fetched and made
// into PNG, the one kind browsers take, while the clipboard waits for it.
// Leafen sets no cookies: a picture on the server is read at the address it
// was given, as any other origin reads it. An address is the active server's
// (lib/serverOrigin.ts), not the page's: a packaged app's page comes from the
// app itself, its server from elsewhere.

/**
 * Writes the picture the promise brings as PNG; whether it was copied. The
 * clipboard's write is called before anything is awaited, so call it from
 * the press itself.
 */
export async function copyImage(picture: Promise<Blob>): Promise<boolean> {
  // A picture that fails before the clipboard asks for it is not left unheard.
  picture.catch(() => undefined);
  if (typeof ClipboardItem === "undefined" || typeof navigator === "undefined" || !navigator.clipboard?.write) return false;
  try {
    await navigator.clipboard.write([new ClipboardItem({ "image/png": picture })]);
    return true;
  } catch {
    return false;
  }
}

/**
 * The picture at an address, as PNG. A refused read is tried once more past
 * the cache, as Notion's app does: WebKit can answer from the copy an image
 * on the page loaded without asking the server's leave. A site that gives no
 * leave to read its pictures (CORS) is read through this server
 * (outside_image_route.go), as Notion's web client reads one through its own
 * (seen 2026-11-15: app.notion.com/image/<address>, then copied).
 */
export async function imagePng(address: string | Promise<string>): Promise<Blob> {
  const given = await address;
  if (!given) throw new Error("No picture to copy");
  const url = new URL(given, serverOrigin()).href;
  const response = await fetch(url).catch(() => fetch(url, { cache: "no-store" })).catch(() => outsidePicture(url));
  if (!response.ok) throw new Error(`The picture was not read (${response.status})`);
  const blob = await response.blob();
  return blob.type === "image/png" ? blob : await drawnAsPng(blob);
}

// Another site's picture as this server reads it for the person signed in
// here; one on the server itself, at any of its addresses, is not another's.
async function outsidePicture(url: string): Promise<Response> {
  const session = loadSession(), address = new URL(url);
  if (!session || isActiveServerOrigin(address.origin) || !/^https?:$/u.test(address.protocol)) throw new Error("The picture was not read");
  const { requestBinaryResponse } = await import("@/apiTransport");
  return requestBinaryResponse(`/outside-image?url=${encodeURIComponent(address.href)}`, session);
}

/** Any picture the page can draw, as PNG. */
function drawnAsPng(blob: Blob): Promise<Blob> {
  return new Promise((resolve, reject) => {
    const image = new Image();
    image.crossOrigin = "anonymous";
    const objectUrl = URL.createObjectURL(blob);
    image.onload = () => {
      URL.revokeObjectURL(objectUrl);
      const canvas = document.createElement("canvas");
      canvas.width = image.naturalWidth;
      canvas.height = image.naturalHeight;
      const context = canvas.getContext("2d");
      if (!context) { reject(new Error("No canvas to draw the picture")); return; }
      try {
        context.drawImage(image, 0, 0);
        canvas.toBlob((png) => png ? resolve(png) : reject(new Error("The picture was not made into PNG")), "image/png");
      } catch (cause) {
        reject(cause);
      }
    };
    image.onerror = () => { URL.revokeObjectURL(objectUrl); reject(new Error("The picture could not be drawn")); };
    image.src = objectUrl;
  });
}

/**
 * What a picture's copy says, Notion's snackbars word for word and in the
 * same plain toast either way: "클립보드에 이미지 복사 완료" (seen 2026-11-11) or
 * "이미지를 클립보드에 복사하지 못했습니다." (seen 2026-11-15 on an image block
 * whose picture is gone; its language file's action.copyImageError).
 */
export function imageCopyNotice(copied: boolean): string {
  return i18next.t(copied ? "blockHandle:media.copiedImage" : "blockHandle:media.couldNotCopyImage");
}

// "복사 중..." stays up at least this long, as Notion's web client keeps it.
const COPYING_SHOWN_MS = 500;

/**
 * Copies a picture and says how it went, Notion's toasts for it (seen
 * 2026-11-15 at 1440): an image block's "이미지 복사" says "복사 중..." from
 * the press for at least 500ms, then "클립보드에 이미지 복사 완료" (twice, a
 * picture with the CORS leave and one without); a chart's "PNG로 복사" says
 * only "클립보드에 이미지 복사 완료". `progress` says "복사 중..." first;
 * `still` is false once what was pressed is gone, and then nothing is said.
 * The clipboard's write starts here: call it from the press itself.
 */
export function copyImageTold(picture: Promise<Blob>, { progress = false, still = () => true }: { progress?: boolean; still?(): boolean } = {}): Promise<boolean> {
  const toasts = appToastStack(), copying = i18next.t("blockHandle:media.copyingImage");
  if (progress) toasts?.notify(copying, undefined, undefined, undefined, undefined, undefined, true);
  const sayFrom = Date.now() + (progress ? COPYING_SHOWN_MS : 0);
  return copyImage(picture).then(async (copied) => {
    const wait = sayFrom - Date.now();
    if (wait > 0) await new Promise((resolve) => setTimeout(resolve, wait));
    if (progress) toasts?.dismissMessage(copying);
    if (still()) toasts?.notify(imageCopyNotice(copied));
    return copied;
  });
}
