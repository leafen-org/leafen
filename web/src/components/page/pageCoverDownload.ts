import { triggerBrowserFileDownload } from "@/lib/browserFileDownload";
import { coverGradient, GRADIENT_HEIGHT, GRADIENT_WIDTH } from "./coverGradient";

const IMAGE_EXTENSIONS: Record<string, string> = {
  "image/avif": "avif",
  "image/gif": "gif",
  "image/jpeg": "jpg",
  "image/png": "png",
  "image/svg+xml": "svg",
  "image/webp": "webp",
};

/** Whether the download can make a file of this cover: an image address, or
 * a gallery color or gradient, which it paints. */
export function coverDownloadable(cover: string, resolvedUrl: string) {
  return cover.startsWith("linear-gradient") ? coverGradient(cover) !== null : Boolean(resolvedUrl);
}

// Paints the gradient's 1500×600 image (coverGradient.ts).
function paintGradient(cover: string): Promise<Blob | null> {
  const line = coverGradient(cover);
  if (!line) return Promise.resolve(null);
  const canvas = document.createElement("canvas");
  canvas.width = GRADIENT_WIDTH;
  canvas.height = GRADIENT_HEIGHT;
  const context = canvas.getContext("2d");
  if (!context) return Promise.resolve(null);
  const gradient = context.createLinearGradient(line.from.x, line.from.y, line.to.x, line.to.y);
  gradient.addColorStop(0, line.colors[0]);
  gradient.addColorStop(1, line.colors[1]);
  context.fillStyle = gradient;
  context.fillRect(0, 0, GRADIENT_WIDTH, GRADIENT_HEIGHT);
  return new Promise(resolve => canvas.toBlob(resolve, "image/png"));
}

async function coverImageFile(cover: string, resolvedUrl: string) {
  if (cover.startsWith("linear-gradient")) return paintGradient(cover);
  const response = await fetch(resolvedUrl, { credentials: "same-origin" });
  if (!response.ok) return null;
  const file = await response.blob();
  return file.type.startsWith("image/") ? file : null;
}

function downloadName(title: string, type: string) {
  const stem = title.replace(/[\\/:*?"<>|\u0000-\u001f]+/gu, " ").trim() || "cover";
  const extension = IMAGE_EXTENSIONS[type.split(";")[0].trim().toLowerCase()];
  return extension ? `${stem}.${extension}` : stem;
}

/** Saves the cover as an image named after its page, as Notion does
 * ("<page title>.png"). False when it couldn't read the image, such as an
 * outside host that doesn't let other sites read it. */
export async function downloadPageCover(cover: string, resolvedUrl: string, title: string) {
  try {
    const file = await coverImageFile(cover, resolvedUrl);
    if (!file) return false;
    triggerBrowserFileDownload(file, downloadName(title, file.type), { revokeAfterMs: 0 });
    return true;
  } catch {
    return false;
  }
}
