export type ImageCrop = { x: number; y: number; width: number; height: number; circle?: boolean };
export const FULL_IMAGE_CROP: ImageCrop = { x: 0, y: 0, width: 1, height: 1 };
export function imageCrop(value: unknown): ImageCrop | null {
  if (!value || typeof value !== "object") return null;
  const crop = value as ImageCrop;
  return [crop.x, crop.y, crop.width, crop.height].every(v => typeof v === "number" && Number.isFinite(v))
    && crop.x >= 0 && crop.y >= 0 && crop.width >= .01 && crop.height >= .01
    && crop.x + crop.width <= 1.000001 && crop.y + crop.height <= 1.000001
    && (crop.circle === undefined || typeof crop.circle === "boolean") ? crop : null;
}
export function cropToAspect(crop: ImageCrop, ratio: number, naturalRatio: number, circle = false): ImageCrop {
  const desired = ratio / naturalRatio;
  const width = Math.min(crop.width, crop.height * desired), height = width / desired;
  return { x: crop.x, y: crop.y, width, height, ...(circle ? { circle: true } : {}) };
}
