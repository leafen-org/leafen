import { createContext, useContext } from "react";
import { legacyUrlHost } from "./legacyUrlHost";

/**
 * How a URL value reads where it is shown: short (shortUrlParts) in a
 * table, on a card, in a list row and in a page's properties alike, as
 * Notion's (seen 2026-11-12 at 1440), or in full where its property's "전체
 * URL 표시" is on. The switch is the property's, as Notion's (seen
 * 2026-11-14: turned on in a table's column menu, the row's page shows the
 * address whole, wrapped, in one color, and so does every other view); a
 * table whose view held it before (fullUrlColumns, read only) says so here,
 * and the property's own setting, once made, goes before it.
 */
export type UrlDisplay = { full: boolean };

export const UrlDisplayContext = createContext<UrlDisplay>({ full: false });

/** Whether a URL property reads whole: its own switch, else what the table's view held. */
export function urlShownFull(property: { config?: { showFullUrl?: unknown } | null }, viewHeld = false) {
  const own = property.config?.showFullUrl;
  return typeof own === "boolean" ? own : viewHeld;
}

export function useUrlDisplay(property: { config?: { showFullUrl?: unknown } | null }): UrlDisplay {
  return { full: urlShownFull(property, useContext(UrlDisplayContext).full) };
}

/**
 * A URL's short parts as Notion's shows them, its host in the text's color
 * and what follows dimmed (seen 2026-11-12, 102 values): the host Node's
 * legacy url.parse finds in it (legacyUrlHost), or else in "http://" and it,
 * as written in the value, without a leading "www."; what follows it, past 12
 * characters only its first 4 and last 6 around "…". A value with no such
 * host, one the parser fails on, one whose host isn't written in it (a name
 * in other letters, which the parser spells in punycode) and one with a
 * space inside read whole, all without the spaces around them.
 * "https://www.example.com/path/page?x=1" reads "example.com" and
 * "/pat…ge?x=1", "mailto:a@example.com" "example.com", "example.com:8080/x"
 * "8080" and "/x" (the scheme "example.com:"), "javascript:void(0)"
 * "javascript" and ":void(0)", "https:example.com" "https" and
 * ":example.com" (a scheme that wants "//" without them), "foo:foo/bar"
 * "foo" and ":foo/bar" (the host where it is first written).
 */
export function shortUrlParts(url: string) {
  const value = url.trim();
  const whole = { host: value, path: "" };
  if (/\s/u.test(value)) return whole;
  const first = legacyUrlHost(value);
  const found = first === "" ? legacyUrlHost(`http://${value}`) : first;
  // The parser writes a host in other letters in punycode, which the value doesn't hold.
  if (!found || [...found].some((character) => character.charCodeAt(0) > 127)) return whole;
  const at = value.replace(/[A-Z]+/gu, (letters) => letters.toLowerCase()).indexOf(found);
  if (at < 0) return whole;
  const host = value.slice(at, at + found.length).replace(/^www\./iu, "");
  if (!host) return whole;
  const path = value.slice(at + found.length);
  return { host, path: path.length > 12 ? `${path.slice(0, 4)}…${path.slice(-6)}` : path };
}
