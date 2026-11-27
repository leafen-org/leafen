import type { GraphReadItem } from "@/contentGraphWire.generated";
import type { PublicContentPageSummary } from "@/publicContentWire.generated";
import type { Page } from "@/types";
import { isPagePictureIcon } from "@/components/page/pagePictureIcon";
import { viewLinkFrameScreen, type ViewLinkScreen } from "@/lib/viewLinkScreen";

/**
 * The shape the page a public address opens is drawn in, so the screen shown
 * while it comes (SharedPageLoading) has that shape and nothing moves when the
 * page is drawn: a full-page database on the whole canvas, a full-width page
 * across the window, any other page in its column (PageView .doc).
 */
export type PublicOpeningShape = "page" | "full-width" | "database";

/**
 * The page about to open: its shape and what heads it — an icon, a picture's
 * box or an emoji's; a cover; the small text's title — each of which moves
 * its title down or changes its size by its head's own rules (PageHeader,
 * PageCover), which the screen shown while it comes follows.
 */
export type PublicOpening = {
  shape: PublicOpeningShape;
  icon: "none" | "emoji" | "picture";
  cover: boolean;
  smallText: boolean;
};

const SHAPES: ReadonlySet<string> = new Set<PublicOpeningShape>(["page", "full-width", "database"]);

/** The head line a share link's or site's document says it in (public_opening_shape.go). */
const SHAPE_META = 'meta[name="leafen-opening-shape"]';
/** What a view link's address says it in, after "#" beside its token. */
const VIEW_LINK_SHAPE = "shape";

/** What public_opening_shape.go says, as words with spaces between them, the shape first: "page icon cover". */
function readOpening(said: string | null | undefined): PublicOpening | null {
  const [shape, ...head] = (said ?? "").trim().split(/\s+/u);
  if (!shape || !SHAPES.has(shape)) return null;
  return {
    shape: shape as PublicOpeningShape,
    icon: head.includes("picture") ? "picture" : head.includes("icon") ? "emoji" : "none",
    cover: head.includes("cover"),
    smallText: head.includes("small-text"),
  };
}

type SaidOpening = { pathname: string; page: string | null; opening: PublicOpening };

/** What the address the app was opened at said of its page. */
function saidOpening(): SaidOpening | null {
  if (typeof window === "undefined") return null;
  const { pathname, search, hash } = window.location;
  const opening = readOpening(pathname === "/view"
    ? new URLSearchParams(hash.slice(1)).get(VIEW_LINK_SHAPE)
    : document.querySelector<HTMLMetaElement>(SHAPE_META)?.content);
  if (!opening) return null;
  return { pathname, page: new URLSearchParams(search).get("page"), opening };
}

// Read as the app starts, while the address is still the one the document was served for.
const said = saidOpening();

const PAGE: PublicOpening = { shape: "page", icon: "none", cover: false, smallText: false };

/** What a public read has read so far: the pages it lists, and each page's graph. */
export type PublicOpeningRead = {
  pages?: readonly PublicContentPageSummary[];
  documents?: Readonly<Record<string, { graph: readonly GraphReadItem[] }>>;
};

/**
 * The page about to open: what its document or view link said while the
 * address is still the one it was opened at, else — a page opened from it —
 * what the pages read so far say of pageId, in the same words
 * (public_opening_shape.go publicOpeningWords), or for a database's page
 * its row read with its database (its icon, cover, full width and small
 * text, content_graph_storage.go), else a page's.
 */
export function publicOpening(pageId?: string, read?: PublicOpeningRead): PublicOpening {
  if (said && typeof window !== "undefined" && window.location.pathname === said.pathname
    && new URLSearchParams(window.location.search).get("page") === said.page) return said.opening;
  if (!pageId || !read) return PAGE;
  const summary = read.pages?.find((candidate) => candidate.id === pageId);
  if (summary?.opening) return readOpening(summary.opening) ?? PAGE;
  for (const document of Object.values(read.documents ?? {})) {
    const row = document.graph.find((item) => item.kind === "row" && item.value.id === pageId);
    if (row?.kind === "row") return readOpening(publicPageOpening(row.value)) ?? PAGE;
  }
  // A page the read listed as it read it (its kind and icon alone).
  return (summary ? readOpening(publicPageOpening(summary)) : null) ?? PAGE;
}

/**
 * What else the screen around the page shows as it opens: a site's page in
 * another site's frame ("이 페이지 임베드") has no bar over it, and without
 * its title no title (PublicSiteView); a view link made for a tablet or a
 * phone, in a wider window, a frame of that screen's size (ViewLinkView).
 */
export function publicOpeningFrame(): { bar: boolean; title: boolean; viewLink: ViewLinkScreen | null } {
  if (typeof window === "undefined") return { bar: true, title: true, viewLink: null };
  if (window.location.pathname === "/view") return { bar: true, title: true, viewLink: viewLinkFrameScreen() };
  if (!window.location.pathname.startsWith("/site/")) return { bar: true, title: true, viewLink: null };
  const params = new URLSearchParams(window.location.search);
  const embedded = params.get("embed") === "1";
  return { bar: !embedded, title: !embedded || params.get("title") !== "0", viewLink: null };
}

/** What a page already read says of itself, for an address that opens it again (a view link's frame) or a page opened from another. */
export function publicPageOpening(page: {
  kind?: string; fullWidth?: boolean; icon?: string; iconType?: string; cover?: string; smallText?: boolean;
}): string {
  const said: string[] = [page.kind === "database" ? "database" : page.fullWidth ? "full-width" : "page"];
  // An icon as the page's head draws one (PageHeader): a picture in its own box, any other in an emoji's.
  if (page.icon && page.iconType && page.iconType !== "none") {
    said.push(isPagePictureIcon({ icon: page.icon, iconType: page.iconType as Page["iconType"] }) ? "picture" : "icon");
  }
  if (page.cover) said.push("cover");
  if (page.smallText) said.push("small-text");
  return said.join(" ");
}

/** What a view link's address carries after its token for a page of this opening. */
export function viewLinkOpeningSuffix(opening: string) {
  return `&${VIEW_LINK_SHAPE}=${encodeURIComponent(opening)}`;
}
