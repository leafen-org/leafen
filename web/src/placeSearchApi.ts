import { placeReverseOperation, placeSearchOperation, type PlaceReverseResponse, type PlaceSearchResponse } from "./administratorWire.generated";
import type { request as apiRequest } from "./apiTransport";
import type { Session } from "./types";

export function createPlaceSearchApi(request: typeof apiRequest) {
 return { searchPlaces: (session: Session, pageId: string, query: string, signal?: AbortSignal) => request<PlaceSearchResponse>(
  placeSearchOperation.path.replace("{pageId}",encodeURIComponent(pageId)),
  { method: placeSearchOperation.method, body: JSON.stringify({query}), signal },session,
 ),
 // A point's name and address, from the provider an administrator chose.
 reversePlace: (session: Session, pageId: string, point: { lat: number; lon: number }, signal?: AbortSignal) => request<PlaceReverseResponse>(
  placeReverseOperation.path.replace("{pageId}",encodeURIComponent(pageId)),
  { method: placeReverseOperation.method, body: JSON.stringify(point), signal },session,
 ) };
}
