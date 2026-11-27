import type { GraphBlock } from "@/contentGraph";
import type { IconType } from "@/lib/types";
import { nativeWorkspaceFileLocator } from "@/lib/workspaceFileTransport";
import { serverUrl } from "@/lib/serverOrigin";

// A stored file's ID, as an uploaded icon keeps one (managedFileValueLocator).
const STORED_FILE_ID = /^[A-Za-z0-9_-]{15}$/u;

/**
 * A callout's stored icon kind as an icon's: a linked picture ("external")
 * and a picture uploaded for it ("image", its file's ID, which the block
 * keeps as a file block keeps its file) both show as a picture.
 */
export function calloutIconType(stored: unknown): IconType {
  if (stored === "external" || stored === "image") return "image";
  return stored === "icon" || stored === "custom_emoji" ? stored : "emoji";
}

/** What a callout stores for a picture picked as its icon: an uploaded file's ID or a link. */
export function calloutPictureIcon(value: string): { icon: string; iconType: "image" | "external" } {
  const trimmed = value.trim();
  return STORED_FILE_ID.test(trimmed) ? { icon: trimmed, iconType: "image" } : { icon: trimmed, iconType: "external" };
}

/**
 * How big a callout draws its icon in its 24px place, in the editor and in a
 * version's preview alike, as Notion's both (measured 2026-11-12): a built-in
 * icon 23.04 (0.96 of the place, again 2026-11-15), a picture 21.31 (0.888 of
 * it, as a page's picture of its box); an emoji its 21.6px line (the place's CSS). A
 * workspace emoji keeps 21.6 and its box 2 more, not yet measured there.
 */
export function calloutGlyphSize(iconType: IconType | undefined): { size: number; box?: number } {
  if (iconType === "icon") return { size: 23.04 };
  if (iconType === "image") return { size: 21.6, box: 24 };
  return { size: 21.6 };
}

// A callout shows its stored icon; an unset icon keeps the default bulb.
export function storedBlockIcon(block: Pick<GraphBlock, "content">): { icon: string; iconType: IconType } | null {
  const content = block.content;
  if (content.iconType === "custom_emoji" && typeof content.customEmojiId === "string") {
    return { icon: content.customEmojiId, iconType: "custom_emoji" };
  }
  if (typeof content.icon !== "string" || !content.icon) return null;
  const iconType = calloutIconType(content.iconType);
  return { icon: content.icon, iconType: iconType === "custom_emoji" ? "emoji" : iconType };
}

/**
 * A callout's icon as a copy for other apps writes it: its emoji, or a
 * picture's address for an <img> tag, as Notion's copy writes an uploaded
 * icon (seen 2026-11-07) — never an uploaded file's bare ID; none when it has
 * none.
 */
export function calloutCopyIcon(content: Record<string, unknown> | undefined): { text: string } | { src: string } | null {
  if (content?.iconType === "none") return null;
  const icon = typeof content?.icon === "string" ? content.icon.trim() : "";
  if (content?.iconType === "image" && STORED_FILE_ID.test(icon)) {
    const locator = nativeWorkspaceFileLocator(icon);
    return { src: typeof window === "undefined" ? locator : serverUrl(locator) };
  }
  if (content?.iconType === "external" && /^https?:\/\//iu.test(icon)) return { src: icon };
  // A workspace emoji keeps its picture's address, as the server names it.
  if (content?.iconType === "custom_emoji" && icon.startsWith("/")) {
    return { src: typeof window === "undefined" ? icon : serverUrl(icon) };
  }
  return { text: icon || "💡" };
}
