import { blockItemText } from "./blockItemText";
import { safeUrl } from "./richtextUrlPolicy";

/**
 * The services the "/ 메뉴" embed group offers, in Notion's order (seen
 * 2026-10-01): of the 51 Notion lists there, the ones that draw from a link
 * alone, each with its links' shape and the frame address it draws in.
 * Services Notion draws through an account connection are left out, as
 * decided for Leafen. PDF, which Notion lists among them, is its own block.
 */
export type EmbedService = {
  id: string;
  /** The service's name, the same in every language. */
  name: string;
  /** Where Notion names it otherwise on its empty block ("Google Map 임베드", "Repl 임베드" — seen 2026-11-01). */
  rowName?: string;
  /**
   * Its link pasted in a line of text, as Notion's "붙여넣기 형식" (seen
   * 2026-11-15, one link each, Chrome 1440): a service it names has
   * "<name> 임베드" before "북마크" ("miroBoard" is the "Miro 보드" its
   * words name); one it doesn't, the plain "임베드" after it. Figma's and
   * Miro's keep "URL" last, as YouTube's.
   */
  paste?: { name?: string | "miroBoard"; urlLast?: true };
  /**
   * Notion's is a link preview, an account's (Google Drive, Figma): its "/"
   * search names it "link preview" and finds the service's name as a keyword
   * ("/li" lists the link previews first; "/fi": 파일, 5개의 열, Figma; "/go":
   * Google Maps, then Google Drive — seen 2026-11-16).
   */
  linkPreview?: true;
  keywords: string[];
  /**
   * Its empty block's window as Notion's (seen 2026-11-06): the button "링크
   * 임베드", "지도 임베드" or "<name> 임베드" and a line naming the links it
   * takes; without it, the plain "링크" and no line (Google Drive, Figma).
   * Its link field shows the service's link shape where Notion's does
   * ("https://www.google.com/maps…"), the plain "URL을 붙여넣으세요" otherwise.
   */
  window?: { submit: "embedLink" | "map" | "named"; hint: true; placeholder?: string };
  /**
   * Notion keeps it a block of its own once its link is in (seen 2026-11-06:
   * Google Maps, Miro and Typeform): its block menu is headed with its name, and
   * "바꾸기" opens its own window. Excalidraw and the rest become a plain embed
   * once filled: "임베드", and the embed window with its upload tab. A test of
   * the link keeps only some of its links so (CodePen's, ownBlockService).
   */
  ownBlock?: true | ((url: URL) => boolean);
  /** The frame address for one of its links, or null when the link is not its. */
  frame(url: URL): string | null;
};

const host = (url: URL) => url.hostname.replace(/^www\./, "").toLowerCase();
const segments = (url: URL) => url.pathname.split("/").filter(Boolean);
const decodedSegment = (part: string) => {
  try {
    return decodeURIComponent(part);
  } catch {
    return part;
  }
};

export const EMBED_SERVICES: readonly EmbedService[] = [
  {
    id: "google_drive",
    name: "Google Drive",
    linkPreview: true,
    keywords: ["google", "drive", "link preview"],
    frame(url) {
      const parts = segments(url);
      if (host(url) === "drive.google.com" && parts[0] === "file" && parts[1] === "d" && parts[2]) {
        return `https://drive.google.com/file/d/${parts[2]}/preview`;
      }
      if (host(url) === "docs.google.com" && ["document", "spreadsheets", "presentation"].includes(parts[0]) && parts[1] === "d" && parts[2]) {
        return `https://docs.google.com/${parts[0]}/d/${parts[2]}/preview`;
      }
      return null;
    },
  },
  {
    id: "google_maps",
    name: "Google Maps",
    rowName: "Google Map",
    paste: { name: "Google Maps" },
    keywords: ["google", "maps", "map"],
    window: { submit: "map", hint: true, placeholder: "https://www.google.com/maps…" },
    ownBlock: true,
    frame(url) {
      if ((host(url) === "google.com" || host(url) === "maps.google.com") && url.pathname.startsWith("/maps")) {
        if (url.pathname.includes("/embed")) return url.toString();
        // The place by its name, close up, as Notion's frame asks (seen 2026-11-06:
        // /maps/place/Busan drawn as "Busan" at zoom 17, its streets).
        const parts = segments(url);
        const place = parts[1] === "place" && parts[2] ? decodedSegment(parts[2].replace(/\+/g, " ")) : null;
        return `https://maps.google.com/maps?q=${encodeURIComponent(place ?? url.searchParams.get("q") ?? url.pathname)}&z=17&output=embed`;
      }
      return null;
    },
  },
  {
    id: "figma",
    name: "Figma",
    paste: { urlLast: true },
    linkPreview: true,
    keywords: ["figma", "link preview"],
    frame(url) {
      if (host(url) !== "figma.com") return null;
      if (url.pathname.startsWith("/embed")) return url.toString();
      return ["file", "design", "proto", "board", "slides"].includes(segments(url)[0] ?? "")
        ? `https://www.figma.com/embed?embed_host=leafen&url=${encodeURIComponent(url.toString())}`
        : null;
    },
  },
  {
    id: "miro",
    name: "Miro",
    paste: { name: "miroBoard", urlLast: true },
    keywords: ["miro"],
    window: { submit: "named", hint: true, placeholder: "https://miro.com/app/board/…" },
    ownBlock: true,
    frame(url) {
      const parts = segments(url);
      if (host(url) !== "miro.com" || parts[0] !== "app" || !parts[2]) return null;
      if (parts[1] === "live-embed") return url.toString();
      // The board itself at once, without Miro's tools, as Notion's frame asks (seen 2026-11-05).
      return parts[1] === "board" ? `https://miro.com/app/live-embed/${parts[2]}/?embedMode=view_only_without_ui&autoplay=true` : null;
    },
  },
  {
    id: "excalidraw",
    name: "Excalidraw",
    paste: { name: "Excalidraw" },
    keywords: ["excalidraw"],
    window: { submit: "named", hint: true, placeholder: "https://excalidraw.com/…" },
    frame(url) {
      return host(url) === "excalidraw.com" ? url.toString() : null;
    },
  },
  {
    id: "loom",
    name: "Loom",
    paste: { name: "Loom" },
    keywords: ["loom"],
    window: { submit: "embedLink", hint: true },
    frame(url) {
      if (host(url) !== "loom.com") return null;
      const id = url.pathname.replace(/^\/(share|embed)\//, "").split("/")[0];
      return id ? `https://www.loom.com/embed/${id}` : null;
    },
  },
  {
    id: "typeform",
    name: "Typeform",
    paste: { name: "Typeform" },
    keywords: ["typeform"],
    window: { submit: "embedLink", hint: true },
    ownBlock: true,
    frame(url) {
      const parts = segments(url);
      // Typeform's oEmbed player, as Notion's frame (seen 2026-11-06): it takes no focus from the page.
      return host(url).endsWith("typeform.com") && parts[0] === "to" && parts[1]
        ? `https://tfcollect.typeform.com/to/${encodeURIComponent(parts[1])}?typeform-embed=oembed&typeform-medium=embed-oembed&format=json&disable-auto-focus=true`
        : null;
    },
  },
  {
    id: "codepen",
    name: "CodePen",
    keywords: ["codepen", "code"],
    window: { submit: "embedLink", hint: true },
    // A person's own pen (codepen.io/<name>/pen/…) stays a CodePen block, made
    // or given in "바꾸기", 300 high, where a team's (/team/…/pen/…) becomes a
    // plain embed, as Notion's (seen 2026-11-15: its menu headed "CodePen" and
    // its "바꾸기" window the service's, two pens; a team's pen "임베드").
    ownBlock: (url) => {
      const parts = segments(url);
      return parts[0] !== "team" && parts[1] === "pen" && Boolean(parts[2]);
    },
    frame(url) {
      // Its owner's address may be a team's: /team/codepen/pen/…
      const parts = segments(url);
      const kind = parts.findIndex(part => part === "pen" || part === "embed");
      if (host(url) !== "codepen.io" || kind < 1 || !parts[kind + 1]) return null;
      if (parts[kind] === "embed") return url.toString();
      // Its preview, JS and result tabs, 300 high, as Notion's frame asks (seen 2026-11-05).
      const pen = parts[kind + 1];
      const query = new URLSearchParams({ "default-tabs": "js,result", height: "300", host: "https://codepen.io", "slug-hash": pen });
      return `https://codepen.io/${parts.slice(0, kind).join("/")}/embed/preview/${pen}?${query}`;
    },
  },
  {
    id: "replit",
    name: "Replit",
    rowName: "Repl",
    paste: { name: "Repl" },
    keywords: ["replit", "repl"],
    window: { submit: "named", hint: true, placeholder: "https://replit.com/…" },
    frame(url) {
      const parts = segments(url);
      if (host(url) !== "replit.com" || !parts[0]?.startsWith("@") || !parts[1]) return null;
      return `https://replit.com/${parts[0]}/${parts[1]}?embed=true`;
    },
  },
];

export function embedService(id: unknown): EmbedService | undefined {
  return EMBED_SERVICES.find((service) => service.id === id);
}

/** The service a link is one of, whose frame draws it; none for another link. */
export function embedServiceForLink(raw: unknown): EmbedService | undefined {
  const safe = typeof raw === "string" ? safeUrl(raw.trim()) : null;
  if (!safe || !/^https?:/i.test(safe)) return undefined;
  try {
    const url = new URL(safe);
    return EMBED_SERVICES.find((service) => service.frame(url) !== null);
  } catch {
    return undefined;
  }
}

/**
 * An embed's service: the one it was made for ("/ 메뉴"), else the one its
 * link is. A service's link given to a plain embed — typed, pasted, by an
 * agent or through the API — draws as that service's block, as Notion's does
 * (seen 2026-11-07: a Google Maps link in a plain "/임베드" became its 916px
 * map, a Miro board 416).
 */
export function embedServiceOf(block: { content: Record<string, unknown> }): EmbedService | undefined {
  return embedService(block.content.service) ?? embedServiceForLink(block.content.url);
}

/**
 * The service whose own block an embed's link keeps it as (EmbedService.ownBlock):
 * it spans the column at its service's height, and its "바꾸기" is the service's.
 */
export function ownBlockService(block: { content: Record<string, unknown> }): EmbedService | undefined {
  const service = embedServiceOf(block);
  if (typeof service?.ownBlock !== "function") return service?.ownBlock ? service : undefined;
  const safe = typeof block.content.url === "string" ? safeUrl(block.content.url.trim()) : null;
  try {
    return safe && service.ownBlock(new URL(safe)) ? service : undefined;
  } catch {
    return undefined;
  }
}

/**
 * The service whose own block an embed stands as, its block menu and phone
 * sheet headed with the service's name: none for one the block API made
 * (its "blockWidth", notion_block_file_request.go), which Notion keeps a
 * plain embed headed "임베드" while its frame is still the service's (seen
 * 2026-11-15: a map made through the API, its menu at 1440 and its sheet on
 * an A90 "임베드", 704×900; the same link given to "/임베드" in the editor
 * became its map block).
 */
export function embedOwnService(block: { content: Record<string, unknown> }): EmbedService | undefined {
  return typeof block.content.blockWidth !== "number" ? ownBlockService(block) : undefined;
}

/** A link embed's frame height until it is resized, as Notion's (its code and its "/임베드", 2026-10-31). */
export const EMBED_DEFAULT_HEIGHT = 320;
// Where Notion makes a service's frame another height (seen 2026-11-05): its map block 900, a pen 300, a board 400,
// a form 600, each the column wide (seen 2026-11-06: a form from "/typeform" and from a pasted link, 704×600).
const SERVICE_DEFAULT_HEIGHT: Record<string, number> = { google_maps: 900, codepen: 300, miro: 400, typeform: 600 };

/**
 * The height an embed's frame has until it is resized: its service's where
 * the embed is made or first takes its link, else 320. A link given in its
 * block menu's "바꾸기" makes a plain embed's 320 unless its service keeps a
 * block of its own (MediaReplacementDialog), as Notion's (seen 2026-11-12: a
 * pen 704×300 replaced by another pen, 704×320; a map 704×900 stays 900).
 */
export function embedDefaultHeight(service?: EmbedService) {
  return SERVICE_DEFAULT_HEIGHT[service?.id ?? ""] ?? EMBED_DEFAULT_HEIGHT;
}

/** What a service's window says (EmbedService.window): its empty block's, and its own block's "바꾸기". */
export function serviceWindowText(service: EmbedService) {
  const submit = service.window?.submit;
  return {
    placeholder: service.window?.placeholder ?? blockItemText("blockEmpty.bookmark.placeholder"),
    submit: submit === "embedLink" ? blockItemText("blockEmpty.service.embedLink")
      : submit === "map" ? blockItemText("blockEmpty.service.embedMap")
        : submit === "named" ? blockItemText("blockEmpty.service.embedNamed", { service: service.rowName ?? service.name })
          : blockItemText("blockEmpty.service.submit"),
    hint: service.window ? blockItemText(`blockEmpty.serviceHint.${service.id}`) : undefined,
  };
}

/**
 * The frame address for a link: the named service's when it is that
 * service's link, else any service's that knows it; null when none does.
 */
export function embedServiceFrame(raw: string, serviceId?: unknown): string | null {
  const safe = safeUrl(raw.trim());
  if (!safe || !/^https?:/i.test(safe)) return null;
  let url: URL;
  try {
    url = new URL(safe);
  } catch {
    return null;
  }
  const named = embedService(serviceId);
  const first = named ? [named, ...EMBED_SERVICES.filter((service) => service !== named)] : EMBED_SERVICES;
  for (const service of first) {
    const frame = service.frame(url);
    if (frame) return frame;
  }
  return null;
}

/**
 * A link a site refuses to be framed by, which Notion makes a bookmark of
 * however it is given as an embed — Replit's (seen 2026-11-05:
 * replit.com/@replit/Python; 2026-11-15: from the "/replit" block, a plain
 * embed's link field and the paste menu's "Repl 임베드", each a bookmark card
 * with no notice).
 */
export function embedLinkMakesBookmark(raw: string, serviceId?: unknown) {
  const target = embedService(serviceId) ?? embedServiceForLink(raw);
  return target?.id === "replit" && Boolean(embedServiceFrame(raw, target.id));
}
