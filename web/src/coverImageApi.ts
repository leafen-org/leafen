import { request } from "./apiTransport";
import type { Session } from "./types";
import { coverImagePaths, type CoverImageSearchResponse, type CoverImageSelectionResponse } from "./coverImageWire.generated";

export function searchCoverImages(session: Session, query: string, page: number, signal?: AbortSignal) {
  const search = new URLSearchParams({ query, page: String(page) });
  return request<CoverImageSearchResponse>(`${coverImagePaths.searchCoverImages}?${search}`, { signal }, session);
}

export function selectCoverImage(session: Session, photoId: string, signal?: AbortSignal) {
  return request<CoverImageSelectionResponse>(coverImagePaths.selectCoverImage.replace("{photoId}", encodeURIComponent(photoId)),
    { method: "POST", signal }, session);
}
