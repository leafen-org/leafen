import { find } from "linkifyjs";
import type { DbProperty } from "@/lib/types";

/** The protocols a URL value opens; another (ftp:, file:) is a link that opens nothing, as Notion's. */
const OPENED_PROTOCOLS = new Set(["http:", "https:", "mailto:"]);

/** A value's link: where it opens, null for a link that opens nothing. */
export type PropertyLink = { href: string | null };

/**
 * A URL, email or phone value's link, as Notion's (seen 2026-11-12 on 64
 * URL, 13 email and 13 phone values), or null for a URL value that reads as
 * plain text. Notion finds the value's first link with linkifyjs, as this
 * does:
 * - a URL opens that link as a browser reads it ("example.com/a" →
 *   "http://example.com/a", "www.example.com" → "http://www.example.com/",
 *   "a@example.com" → "mailto:a@example.com", "a b.com" → "http://b.com/");
 *   one that holds none is no link ("plain words here", "tel:0101234",
 *   "foo:bar", "127.0.0.1:3000/p"), and one whose link is neither web nor
 *   mail ("ftp://…", "file:///…") a link that opens nothing;
 * - an email opens "mailto:" with its first link, or with the value itself
 *   when it holds none ("a b@example.com" → "mailto:b@example.com",
 *   "plain words" → "mailto:plain words");
 * - a phone opens "tel:" with the value less its spaces, dots, dashes and
 *   brackets ("(02) 123-4567 ext 9" → "tel:021234567ext9").
 */
export function propertyLink(type: DbProperty["type"], value: string): PropertyLink | null {
  const normalized = value.trim();
  if (!normalized) return null;
  if (type === "phone") {
    const number = normalized.replace(/[\s().-]/gu, "");
    if (!number || /^tel:$/iu.test(number)) return null;
    return { href: /^tel:/iu.test(number) ? number : `tel:${number}` };
  }
  const found = find(normalized)[0]?.href;
  if (type === "email") {
    const to = found ?? normalized;
    return { href: /^mailto:/iu.test(to) ? to : `mailto:${to}` };
  }
  return found ? { href: openedLink(found) } : null;
}

/** A found link as a browser reads it, when it is web or mail. */
function openedLink(found: string) {
  try {
    const url = new URL(found);
    return OPENED_PROTOCOLS.has(url.protocol) ? url.href : null;
  } catch {
    return null;
  }
}

/**
 * Where a URL, email or phone value made text links, as Notion's change
 * (seen 2026-11-12 on 16 URL, 13 email and 13 phone values; the server's
 * twin is databasePropertyTextLink): its first link as a browser reads it,
 * else a "tel:" value as written. Notion links any other value to itself, a
 * link that opens nothing; such a value stays plain text here.
 */
export function propertyTextLink(value: string): string | null {
  const normalized = value.trim();
  if (!normalized) return null;
  const found = find(normalized)[0]?.href;
  if (found) return openedLink(found);
  return /^tel:/iu.test(normalized) ? normalized : null;
}
