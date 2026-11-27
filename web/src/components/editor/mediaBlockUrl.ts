import { type RefObject, useLayoutEffect, useRef, useState } from "react";
import { find as findLinks } from "linkifyjs";
import type { GraphBlock } from "@/contentGraph";
import { isManagedFileBlock } from "@/contentGraph";
import { type FilePreviewWidth, filePreviewWidth, useWorkspaceFile } from "@/lib/fileUrls";
import { nativeWorkspaceFileLocator } from "@/lib/workspaceFileTransport";
import { serverOrigin } from "@/lib/serverOrigin";
import { textLinkHref } from "./richtextUrlPolicy";

/**
 * Where a block's address opens, as a text link's does (textLinkHref): a web
 * address, one written without its scheme over https ("example.com/x",
 * "//e.com/x"), as Notion's API keeps one (notion_block_media_address.go).
 * Never a path or page of this app, nor another scheme ("mailto:", "ftp://");
 * "" for words that are no address ("not a url").
 */
export function blockWebAddress(value: unknown) {
  if (typeof value !== "string") return "";
  const text = value.trim();
  if (!text || text.startsWith("#") || /^[/\\](?![/\\])/u.test(text)) return "";
  // A scheme is letters before ":" that no port number follows ("localhost:3000/a").
  if (/^[a-z][a-z\d+.-]*:(?!\d)/iu.test(text) && !/^https?:/iu.test(text)) return "";
  try {
    const parsed = new URL(textLinkHref(text));
    // A host is names or an IP address: Chrome takes "https://not a url" as
    // the host not%20a%20url, which no browser opens.
    const host = /^[a-z\d._-]+$/iu.test(parsed.hostname) || /^\[[\da-f:.]+\]$/iu.test(parsed.hostname);
    return (parsed.protocol === "http:" || parsed.protocol === "https:") && host ? parsed.toString() : "";
  } catch { return ""; }
}

/**
 * The address a frame tries for text that is no web address, as Notion's
 * embed does (seen 2026-11-10: "not a url" framed as https://not%20a%20url/,
 * "mailto:…" framed as it is): every character that could make it one
 * encoded, so it names no host and opens nothing; "" for none.
 */
export function unopenedFrameAddress(value: unknown) {
  const text = typeof value === "string" ? value.trim() : "";
  if (!text || blockWebAddress(text)) return "";
  return `https://${encodeURIComponent(text)}/`;
}

/** A media block's address to load: a path of this server reads against it, any other as blockWebAddress. */
export function safeMediaUrl(value: unknown) {
  if (typeof value !== "string" || !value.trim()) return "";
  if (!/^\/(?![/\\])/u.test(value.trim())) return blockWebAddress(value);
  try {
    return new URL(value.trim(), serverOrigin()).toString();
  } catch { return ""; }
}

/**
 * A link as someone gave it in a block's link window, kept as written, as
 * Notion's (seen 2026-11-06): only trimmed, and without its scheme given
 * "http://" in front ("example.net/a/" → "http://example.net/a/", "//e.com/x"
 * → "http://e.com/x"), while "https://example.org" and "https://example.com/"
 * stay as typed. A path of this server ("/…") still reads against it. ""
 * unless it is a web link.
 *
 * Text is a link when an address can be found in it, as Notion's window
 * finds one with linkifyjs, as its URL property does (propertyLinks): a word
 * with no known top-level domain is refused with its "유효한 링크를
 * 입력하세요" — "abc", "abc.zz", "abc:8080", "1.2.3.4", "not a link" — while
 * "localhost", "http://abc" (its scheme given) and "a example.dev" (an
 * address among words, kept whole as "http://a example.dev") are taken
 * (seen 2026-11-16 in Chrome at 1440 in its embed and bookmark windows).
 */
export function givenWebLink(value: string) {
  const text = value.trim();
  if (!text) return "";
  if (/^\/(?![/\\])/u.test(text)) return safeMediaUrl(text);
  if (!findLinks(text).some((match) => match.type === "url")) return "";
  // A scheme is letters before ":" that no port number follows ("example.com:8080"); only the web's is kept.
  const scheme = /^([a-z][a-z\d+.-]*):(?!\d)/iu.exec(text)?.[1]?.toLowerCase();
  if (scheme) return scheme === "http" || scheme === "https" ? text : "";
  return text.startsWith("//") ? `http:${text}` : `http://${text}`;
}

/** Files uploaded from this tab, shown from their bytes here while it stays open. */
const uploadedHere = new Map<string, string>();
const UPLOADED_HERE_MOST = 16;

/** The block shows a file it just took at once, with no gap for its address to come back. */
export function keepUploadedFile(fileId: string, file: File) {
  uploadedHere.set(fileId, URL.createObjectURL(file));
  for (const [oldest, url] of uploadedHere) {
    if (uploadedHere.size <= UPLOADED_HERE_MOST) break;
    uploadedHere.delete(oldest);
    URL.revokeObjectURL(url);
  }
}

/**
 * A media block's address; an image shown asks for the width it is shown at,
 * a menu's open and download for none. `refused` is the server's error when
 * it gives no address for the block's file. While a shown file's next address
 * is on its way the last one stands (seen 2026-11-05: a tab left in the
 * background came back to every media block collapsed and loading again).
 */
export function useMediaBlockUrl(block: GraphBlock, width?: FilePreviewWidth) {
  const fileId = isManagedFileBlock(block) ? String(block.content.fileId ?? "") : "";
  const managed = useWorkspaceFile(fileId ? nativeWorkspaceFileLocator(fileId) : undefined, [], { width });
  const last = useRef({ fileId: "", url: "" });
  if (managed.url) last.current = { fileId, url: managed.url };
  const standing = last.current.fileId === fileId && !managed.failedStatus ? last.current.url : "";
  return {
    managed: Boolean(fileId),
    url: fileId ? managed.url || standing || (uploadedHere.get(fileId) ?? "") : safeMediaUrl(block.content.url),
    refused: fileId ? managed.failedStatus : 0,
  };
}

/**
 * The preview width for an image shown as a share (%) of the column holding
 * it, measured before the first paint, or undefined until then. It only grows,
 * so a narrower window or a smaller drag keeps the photo already loaded.
 */
export function useColumnPreviewWidth(ref: RefObject<HTMLElement | null>, share: number, mounted: boolean) {
  const [columnWidth, setColumnWidth] = useState(0);
  const widest = useRef<FilePreviewWidth | undefined>(undefined);
  useLayoutEffect(() => {
    const column = ref.current?.parentElement;
    if (!mounted || !column) return;
    const measure = () => setColumnWidth((seen) => Math.max(seen, column.getBoundingClientRect().width));
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(column);
    return () => observer.disconnect();
  }, [ref, mounted]);
  const width = columnWidth ? filePreviewWidth((columnWidth * share) / 100) : undefined;
  if (width && (!widest.current || width > widest.current)) widest.current = width;
  return widest.current;
}
