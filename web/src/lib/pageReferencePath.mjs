function decodePathPart(value) {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}

// A Leafen page's ID as its links carry it: the server's own ID, a client's
// UUID or a page's public ID, the shape the server reads a page link's ID in
// (workspaceImportReferenceIDPattern). A Notion address's "Title-<ID>" fits
// it too, so the address it stands at decides first.
const PAGE_ID = /^[A-Za-z0-9_-]{1,64}$/u;

function pageIdIn(segment) {
  const id = decodePathPart(segment);
  return PAGE_ID.test(id) ? id : null;
}

/**
 * The page of this server a link names, without depending on a browser
 * runtime: leafen://page/<ID>, a path in the app (/p/<ID>), or a web address
 * at /p/<ID> whose origin is this server's, as ownOrigin says; null for any
 * other link. Another site's /p/ (a Notion page's address, another Leafen
 * server's page) names no page here.
 */
export function pageIdFromPageHref(href, ownOrigin) {
  const raw = typeof href === 'string' ? href.trim() : '';
  if (!raw) return null;

  const schemeMatch = raw.match(/^leafen:\/\/page\/([^/?#]+)(?:[?#].*)?$/i);
  if (schemeMatch) return pageIdIn(schemeMatch[1]);

  // A path in the app; "//host/…" and "/\host/…" name another host.
  if (/^\/(?![/\\])/u.test(raw)) {
    const match = raw.split(/[?#]/u, 1)[0].match(/^\/p\/([^/]+)\/?$/u);
    return match ? pageIdIn(match[1]) : null;
  }

  let url;
  try {
    url = new URL(raw);
  } catch {
    return null;
  }
  if ((url.protocol !== 'http:' && url.protocol !== 'https:') || typeof ownOrigin !== 'function' || !ownOrigin(url.origin)) {
    return null;
  }
  const match = url.pathname.match(/^\/p\/([^/]+)\/?$/u);
  return match ? pageIdIn(match[1]) : null;
}
