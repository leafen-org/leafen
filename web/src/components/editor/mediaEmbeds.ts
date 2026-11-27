import { isActiveServerOrigin, serverUrl } from "@/lib/serverOrigin";
import { embedServiceFrame } from "./embedServices";
import { safeUrl } from "./richtextUrlPolicy";

export type PastedAssetBlockType = "image" | "video" | "audio" | "pdf";

/** Provider URL normalization and pasted-asset classification for editor media blocks. */
export function blockTypeForPastedAssetUrl(url: string): PastedAssetBlockType | null {
  const lower = url.trim().toLowerCase();
  let pathname = lower;
  try {
    pathname = new URL(url, "https://leafen.local").pathname.toLowerCase();
  } catch {
    /* keep the lower-cased input */
  }

  if (/\.(?:apng|avif|gif|jpe?g|png|svg|webp)$/i.test(pathname)) return "image";
  if (streamingVideoEmbed(url) || /\.(?:m4v|mov|mp4|ogv|webm)$/i.test(pathname)) return "video";
  if (/\.(?:aac|flac|m4a|mp3|oga|ogg|opus|wav|weba)$/i.test(pathname)) return "audio";
  if (/\.pdf$/i.test(pathname)) return "pdf";
  return null;
}

/**
 * Convert a common provider share/watch URL into its embeddable iframe URL.
 * Returns null when the URL isn't a recognized provider (caller falls back to
 * the raw URL for already-embeddable links).
 */
export function providerEmbedUrl(raw: string): string | null {
  const safe = safeUrl(raw.trim());
  if (!safe || !/^https?:/i.test(safe)) return null;
  let u: URL;
  try {
    u = new URL(safe);
  } catch {
    return null;
  }
  const host = u.hostname.replace(/^www\./, "").toLowerCase();
  const youTube = youTubeEmbed(u, host, safe);
  // Its player suggests the channel's own videos at the end, as Notion's frame asks (rel=0, seen 2026-11-05).
  if (youTube) {
    const embed = new URL(youTube);
    embed.searchParams.set("rel", "0");
    return embed.toString();
  }
  return otherProviderEmbedUrl(u, host, safe);
}

function youTubeEmbed(u: URL, host: string, safe: string): string | null {
  if (host === "youtu.be") {
    const id = u.pathname.slice(1).split("/")[0];
    if (id) return `https://www.youtube.com/embed/${id}`;
  }
  if (host === "youtube.com" || host === "m.youtube.com" || host === "youtube-nocookie.com") {
    if (u.pathname === "/watch") {
      const id = u.searchParams.get("v");
      if (id) return `https://www.youtube.com/embed/${id}`;
    }
    if (u.pathname.startsWith("/embed/")) return safe;
    if (u.pathname.startsWith("/shorts/")) {
      const id = u.pathname.split("/")[2];
      if (id) return `https://www.youtube.com/embed/${id}`;
    }
  }

  return null;
}

function otherProviderEmbedUrl(u: URL, host: string, safe: string): string | null {
  // Vimeo
  if (host === "vimeo.com") {
    const id = u.pathname.split("/").filter(Boolean)[0];
    if (id && /^\d+$/.test(id)) return `https://player.vimeo.com/video/${id}`;
  }
  if (host === "player.vimeo.com") return safe;

  // Loom
  if (host === "loom.com") {
    const id = u.pathname.replace(/^\/(share|embed)\//, "").split("/")[0];
    if (id) return `https://www.loom.com/embed/${id}`;
  }

  // Google Maps, drawn as its service's embed draws it.
  if ((host === "google.com" || host === "maps.google.com") && u.pathname.startsWith("/maps")) {
    return embedServiceFrame(safe, "google_maps");
  }

  // SoundCloud (audio) — uses its player widget.
  if (host === "soundcloud.com") {
    return `https://w.soundcloud.com/player/?url=${encodeURIComponent(safe)}`;
  }

  // Figma / CodePen / Twitter use embed wrappers; pass through their embed forms.
  if (host === "figma.com" && u.pathname.startsWith("/embed")) return safe;
  if (host === "codepen.io" && u.pathname.includes("/embed/")) return safe;

  return null;
}

/** Streaming providers whose video can't play in a native <video> element. */
export function streamingVideoEmbed(raw: string): string | null {
  const embed = providerEmbedUrl(raw);
  if (!embed) return null;
  return /youtube\.com\/embed|player\.vimeo\.com|loom\.com\/embed/.test(embed)
    ? embed
    : null;
}

/** What a streaming provider's player may do in its frame. */
export const PROVIDER_FRAME_ALLOW = "accelerometer; autoplay; clipboard-write; encrypted-media; fullscreen; gyroscope; picture-in-picture";

/**
 * Where a frame opens a streaming provider's player. A page whose address isn't http(s),
 * the Mac and iPhone apps' wails://, sends no Referer, and YouTube answers
 * one without it with "error 153" (seen 2026-11-05); the server's own small page
 * frames the player for it. A server on this computer by its number (127.0.0.1,
 * [::1]) is named localhost there: YouTube refuses some videos to a page named by
 * a number ("이 동영상은 볼 수 없습니다", seen 2026-11-07: a music video
 * played framed from localhost and not from 127.0.0.1).
 */
export function providerFrameSrc(player: string) {
  if (/^https?:$/u.test(window.location.protocol)) return player;
  const frame = new URL(serverUrl(`/api/leafen/v1/provider-frame?src=${encodeURIComponent(player)}`), window.location.href);
  if (frame.hostname === "127.0.0.1" || frame.hostname === "[::1]") frame.hostname = "localhost";
  return frame.toString();
}

/** A web page's frame sends the page's origin, as Notion's: many a site draws nothing without it (CodePen, seen 2026-11-05). */
export const WEB_FRAME_REFERRER = "strict-origin-when-cross-origin";

/**
 * What a web page may do in its frame, as Notion's (read 2026-11-05): another
 * site keeps its own storage, without which many a site fails. A page of this
 * very server never does: with its scripts it could reach out of the frame.
 */
export function webFrameSandbox(url: string) {
  const base = "allow-forms allow-popups allow-popups-to-escape-sandbox allow-scripts allow-top-navigation-by-user-activation";
  try {
    const { origin } = new URL(url);
    if (origin === window.location.origin || isActiveServerOrigin(origin)) return base;
  } catch {
    return base;
  }
  return `${base} allow-same-origin allow-storage-access-by-user-activation`;
}
