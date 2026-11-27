import { request, requestBinaryResponse } from "./apiTransport";
import type { Session } from "./types";
import {
  urlMetadataPaths,
  type UrlMetadataRequest,
  type UrlMetadataResponse,
} from "./urlMetadataWire.generated";

export function createUrlMetadataApi() {
  return {
    /** A link's page facts; `videoSize` also reads its video's shape (two more reads, for a video's frame only). */
    readUrlMetadata(
      session: Session,
      url: string,
      signal?: AbortSignal,
      options: { videoSize?: boolean } = {},
    ): Promise<UrlMetadataResponse> {
      const body: UrlMetadataRequest = options.videoSize ? { url, videoSize: true } : { url };
      return request<UrlMetadataResponse>(
        urlMetadataPaths.readUrlMetadata,
        { method: "POST", body: JSON.stringify(body), signal },
        session,
      );
    },
    /** A PDF a page links to, read through the server and checked to be a PDF. */
    readLinkedPdf(session: Session, url: string, signal?: AbortSignal): Promise<Response> {
      return requestBinaryResponse(urlMetadataPaths.readLinkedPdf, session, {
        method: "POST", body: JSON.stringify({ url }), headers: { "content-type": "application/json" }, signal,
      });
    },
  };
}
