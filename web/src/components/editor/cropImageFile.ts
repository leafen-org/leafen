import { imageCrop, type ImageCrop } from "./imageCropModel";

/** Cropping creates real lossless image bytes, like the observed Notion
 * download. The existing upload owner attaches them only after success. */
export async function cropImageFile(url: string, crop: ImageCrop, name: string, signal: AbortSignal) {
  if (!url || !imageCrop(crop)) throw new Error("Invalid image crop");
  const response = await fetch(url, { signal });
  if (!response.ok) throw new Error("Couldn't read the image");
  const original = await response.blob();
  const objectUrl = URL.createObjectURL(original);
  const image = new Image();
  try {
    image.src = objectUrl;
    await image.decode();
    if (signal.aborted) throw new DOMException("Cancelled", "AbortError");
    const canvas = document.createElement("canvas");
    canvas.width = Math.max(1, Math.round(image.naturalWidth * crop.width));
    canvas.height = Math.max(1, Math.round(image.naturalHeight * crop.height));
    const context = canvas.getContext("2d");
    if (!context) throw new Error("Couldn't prepare the image");
    if (crop.circle) {
      context.beginPath(); context.ellipse(canvas.width / 2, canvas.height / 2, canvas.width / 2, canvas.height / 2, 0, 0, 2 * Math.PI);
      context.clip();
    }
    context.drawImage(image, crop.x * image.naturalWidth, crop.y * image.naturalHeight,
      crop.width * image.naturalWidth, crop.height * image.naturalHeight, 0, 0, canvas.width, canvas.height);
    const blob = await new Promise<Blob | null>(resolve => canvas.toBlob(resolve, "image/png"));
    if (!blob || signal.aborted) throw new Error("Couldn't prepare the image");
    const base = name.replace(/\.[^./]+$/u, "") || "image";
    return new File([blob], `${Array.from(base).slice(0, 251).join("")}.png`, { type: "image/png" });
  } finally { URL.revokeObjectURL(objectUrl); }
}
