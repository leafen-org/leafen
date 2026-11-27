/**
 * The host (name and port) Node's legacy `url.parse` finds in a URL, "" when
 * it finds none, or null where it fails (a sign-in it can't decode): the
 * host Notion's URL values read by (seen 2026-11-12 — its app carries that
 * parser, "slashed" protocols http, https, ftp, gopher and file wanting "//"
 * and "javascript:" holding no host, both by their lower-case names only;
 * urlDisplay.ts). Only the host is followed here.
 */
export function legacyUrlHost(input: string): string | null {
  // Back slashes before the query or fragment read as forward ones.
  const query = input.indexOf("?");
  const splitter = query !== -1 && query < input.indexOf("#") ? "?" : "#";
  const parts = input.split(splitter);
  parts[0] = parts[0].replace(/\\/gu, "/");
  const url = parts.join(splitter);
  let rest = url.trim();
  // A plain path holds no host.
  if (url.split("#").length === 1 && SIMPLE_PATH.test(rest)) return "";
  const proto = PROTOCOL.exec(rest)?.[0];
  if (proto) rest = rest.slice(proto.length);
  let slashes = false;
  if (proto || /^\/\/[^@/]+@[^@/]+/u.test(rest)) {
    slashes = rest.startsWith("//");
    if (slashes && !(proto && HOSTLESS.has(proto))) rest = rest.slice(2);
  }
  if ((proto && HOSTLESS.has(proto)) || !(slashes || (proto && !SLASHED.has(proto)))) return "";
  const hostEnd = firstOf(rest, HOST_ENDING);
  const at = hostEnd === -1 ? rest.lastIndexOf("@") : rest.lastIndexOf("@", hostEnd);
  if (at !== -1) {
    // The parser decodes what comes before "@" and fails where it can't.
    try { decodeURIComponent(rest.slice(0, at)); } catch { return null; }
    rest = rest.slice(at + 1);
  }
  const end = firstOf(rest, NON_HOST);
  let host = rest.slice(0, end === -1 ? rest.length : end);
  let port = "";
  const portMatch = /:[0-9]*$/u.exec(host);
  if (portMatch) {
    if (portMatch[0] !== ":") port = portMatch[0].slice(1);
    host = host.slice(0, host.length - portMatch[0].length);
  }
  let hostname = host;
  const ipv6 = hostname.startsWith("[") && hostname.endsWith("]");
  if (!ipv6) hostname = validHostname(hostname);
  hostname = hostname.length > 255 ? "" : hostname.toLowerCase();
  return hostname + (port ? `:${port}` : "");
}

/** A host name up to its first part that isn't a host name's, as the parser cuts it. */
function validHostname(hostname: string) {
  const labels = hostname.split(".");
  for (let index = 0; index < labels.length; index++) {
    const label = labels[index];
    // Letters past ASCII pass, each as one "x" per UTF-16 unit.
    const ascii = Array.from({ length: label.length }, (_, at) => (label.charCodeAt(at) > 127 ? "x" : label[at])).join("");
    if (!label || HOST_PART.test(ascii)) continue;
    const valid = labels.slice(0, index);
    const start = HOST_PART_START.exec(label);
    if (start) valid.push(start[1]);
    return valid.join(".");
  }
  return hostname;
}

function firstOf(text: string, characters: readonly string[]) {
  let first = -1;
  for (const character of characters) {
    const index = text.indexOf(character);
    if (index !== -1 && (first === -1 || index < first)) first = index;
  }
  return first;
}

const PROTOCOL = /^[a-z0-9.+-]+:/iu;
const SIMPLE_PATH = /^(\/\/?(?!\/)[^?\s]*)(\?\S*)?$/u;
const HOST_ENDING = ["/", "?", "#"];
const NON_HOST = ["%", "/", "?", ";", "#", "'", "{", "}", "|", "\\", "^", "`", "<", ">", "\"", " ", "\r", "\n", "\t"];
const HOST_PART = /^[+a-z0-9A-Z_-]{0,63}$/u;
const HOST_PART_START = /^([+a-z0-9A-Z_-]{0,63})(.*)$/u;
const HOSTLESS = new Set(["javascript", "javascript:"]);
const SLASHED = new Set(["http", "https", "ftp", "gopher", "file", "http:", "https:", "ftp:", "gopher:", "file:"]);
