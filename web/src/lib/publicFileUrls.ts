import { createContext, useContext } from "react";
import { API_ROOT } from "@/apiTransport";
import { serverPath } from "@/lib/serverOrigin";

// A published page shows its managed files through the anonymous public file
// route of the share link, site or view link it was opened from. The server serves a
// file only while a page that link shows displays it, and a workspace emoji's
// picture for the token the page's text carries for it.

export type PublicFileSource = { kind: "share" | "site" | "view"; key: string };

export type PublicFileUrl = ((fileId: string) => string) & {
  /** The picture of a workspace emoji the page shows, by the token its text carries. */
  emoji?: (token: string) => string;
};

export const PublicFileUrlContext = createContext<PublicFileUrl | null>(null);

// A window shows one published page at a time: markup drawn outside React
// (a title's field, a cell, a mention's words) shows its emoji pictures
// through the link that page was opened from, the last one set up here.
let shownEmojiUrl: ((token: string) => string) | null = null;

/** The picture of a workspace emoji the published page shown has, by its token; "" outside published pages. */
export function publicEmojiPictureUrl(token: string) {
  return token && shownEmojiUrl ? shownEmojiUrl(token) : "";
}

function publicContentFileUrl(source: PublicFileSource, fileId: string) {
  return serverPath(`${API_ROOT}/public-content/${source.kind}/${encodeURIComponent(source.key)}/files/${encodeURIComponent(fileId)}`);
}

/** The addresses a published page's files and emoji pictures are read from. */
export function publicFileUrlsFor(source: PublicFileSource): PublicFileUrl {
  const fileUrl: PublicFileUrl = (fileId) => publicContentFileUrl(source, fileId);
  fileUrl.emoji = (token) => serverPath(
    `${API_ROOT}/public-content/${source.kind}/${encodeURIComponent(source.key)}/emoji/${encodeURIComponent(token)}`,
  );
  shownEmojiUrl = fileUrl.emoji;
  return fileUrl;
}

/** The public page's file address builder, or null outside published pages. */
export function usePublicFileUrl() {
  return useContext(PublicFileUrlContext);
}
