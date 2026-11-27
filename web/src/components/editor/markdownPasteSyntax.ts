import { leafenPagePath } from "@/lib/pageLinks";
import { safeUrl } from "./richtext";

export function isEscaped(text: string, index: number) {
  let slashCount = 0;
  for (let i = index - 1; i >= 0 && text[i] === "\\"; i--) slashCount++;
  return slashCount % 2 === 1;
}

export function findUnescaped(text: string, needle: string, from: number) {
  for (
    let index = text.indexOf(needle, from);
    index >= 0;
    index = text.indexOf(needle, index + 1)
  ) {
    if (!isEscaped(text, index)) return index;
  }
  return -1;
}

function escapedMarkdownLiteral(text: string, offset: number) {
  if (text[offset] !== "\\") return null;
  const next = text[offset + 1] ?? "";
  return /[\\`*_~[\]|]/.test(next) ? next : null;
}

export function unescapeMarkdownLiteral(text: string) {
  let out = "";
  for (let index = 0; index < text.length; index++) {
    const escapedLiteral = escapedMarkdownLiteral(text, index);
    if (escapedLiteral) {
      out += escapedLiteral;
      index++;
    } else {
      out += text[index];
    }
  }
  return out;
}

// "&name;", "&#…;" or "&#x…;", as CommonMark reads a character reference.
const CHARACTER_REFERENCE = /&(?:#[xX]([0-9a-fA-F]{1,6})|#([0-9]{1,7})|([A-Za-z][A-Za-z0-9]{1,31}));/gu;
let referenceReader: HTMLTextAreaElement | null = null;

/**
 * Words with their character references read, as CommonMark and Notion's
 * paste read them (seen 2026-11-11): "&copy;" is "©" and "&nbsp;" a
 * no-break space — any of HTML's names, by the browser's own table — and
 * "&#169;" or "&#xA9;" its number's character (U+FFFD for none); a name
 * HTML lacks ("&foo;") or one without its ";" ("&copy") stays as written.
 * Code and an equation are not read so, as Notion's keeps them.
 */
export function decodeCharacterReferences(text: string) {
  if (!text.includes("&")) return text;
  return text.replace(CHARACTER_REFERENCE, (raw: string, hex?: string, decimal?: string) => {
    if (hex || decimal) {
      const code = Number.parseInt(hex ?? decimal ?? "", hex ? 16 : 10);
      return code === 0 || code > 0x10ffff || (code >= 0xd800 && code <= 0xdfff) ? "�" : String.fromCodePoint(code);
    }
    // A textarea's markup is its text, never elements, so nothing in it runs.
    referenceReader ??= document.createElement("textarea");
    referenceReader.innerHTML = raw;
    const read = referenceReader.value;
    // HTML also reads a known name at the start of a longer one ("&copyx;"
    // as "©x;"); only the whole name counts, "&semi;" being ";".
    return read !== raw && (read === ";" || !read.endsWith(";")) ? read : raw;
  });
}

export function normalizeInlineAutoLink(raw: string) {
  if (!raw || /\s/.test(raw)) return "";
  let candidate = raw;
  const bareDomain = /^(?:[a-z0-9-]+\.)+[a-z]{2,}(?::\d+)?(?:[/?#].*)?$/i;
  const localhost = /^localhost(?::\d+)?(?:[/?#].*)?$/i;
  if (bareDomain.test(candidate) || localhost.test(candidate)) {
    candidate = `https://${candidate}`;
  } else if (!/^([a-z][a-z0-9+.-]*:|\/|#)/i.test(candidate)) {
    return "";
  }
  return safeUrl(candidate);
}

/**
 * A pasted Markdown link's address as a text link keeps it: a page here
 * named by the app's own leafen://page/<ID> scheme is its path in the app
 * (leafenPagePath: /p/<ID>, its block anchor kept), which a text link opens;
 * any other stays as it is.
 */
export function pastedLinkAddress(raw: string) {
  const value = raw.trim();
  return leafenPagePath(value) ?? value;
}

export function safePasteAssetUrl(raw: string) {
  const url = safeUrl(raw.trim());
  if (!url || url.startsWith("#") || /^mailto:/i.test(url)) return "";
  return url;
}
