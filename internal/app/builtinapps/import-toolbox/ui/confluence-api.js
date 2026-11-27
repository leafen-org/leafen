// Confluence Cloud through its REST API (v2), as the person: their email and
// API token are a credential Leafen seals ("confluence") and fills into each
// relayed request as HTTP Basic, so the app never holds them. The site's
// address gives its cloud id; a scoped token reaches the site through
// Atlassian's gateway, a classic one at the site itself. One space's pages
// come in their storage format with their attachments and become the same
// bundle the HTML export makes, so either way carries the same migration on.

import { spaceBundle } from "./confluence-convert.js";
import { mentionedAccounts, storageToExportHtml } from "./confluence-storage.js";

const AUTHORIZATION = "Basic {{credential:confluence.email:token|base64}}";
// The relay answers with up to 5 MB.
const LARGEST_FILE = 5 * 1024 * 1024;
const decoder = new TextDecoder();

export class ConfluenceError extends Error {
  constructor(code, detail = "") {
    super(detail ? `${code}: ${detail}` : code);
    this.code = code;
  }
}

/** The site's address from what the person types: "acme", "acme.atlassian.net" or a link into it. */
export function siteOrigin(text) {
  const value = text.trim();
  if (!value) return "";
  if (/^https?:\/\//iu.test(value)) {
    try {
      return new URL(value).origin;
    } catch {
      return "";
    }
  }
  const host = value.split("/")[0].toLowerCase();
  if (!/^[a-z0-9.-]+$/u.test(host)) return "";
  return `https://${host.includes(".") ? host : `${host}.atlassian.net`}`;
}

const bytesOf = (answer) => answer.bodyBase64
  ? Uint8Array.from(atob(answer.body), (char) => char.charCodeAt(0))
  : new TextEncoder().encode(answer.body);
const wait = (ms) => new Promise((resolve) => { setTimeout(resolve, ms); });
const escapeHtml = (text) => text.replace(/&/gu, "&amp;").replace(/</gu, "&lt;").replace(/>/gu, "&gt;").replace(/"/gu, "&quot;");

/** A Confluence site, read through Leafen's relay. */
export class ConfluenceSite {
  constructor(app, origin) {
    this.app = app;
    this.origin = origin;
    this.base = "";
  }

  /** One relayed GET; the relay's and Confluence's own limits are waited out. */
  async get(url, { signed = true } = {}) {
    for (let attempt = 0; ; attempt += 1) {
      let answer;
      try {
        answer = await this.app.relay({ method: "GET", url, headers: signed ? { Authorization: AUTHORIZATION } : {} });
      } catch (error) {
        if (/too many|limited/iu.test(String(error?.message ?? "")) && attempt < 30) {
          await wait(5000);
          continue;
        }
        throw error;
      }
      if (answer.status === 429 && attempt < 30) {
        await wait(Math.min(60, Number(answer.headers?.["retry-after"]) || 5) * 1000);
        continue;
      }
      return answer;
    }
  }

  async json(path) {
    const answer = await this.get(/^https?:/iu.test(path) ? path : this.base + path);
    if (answer.status === 401 || answer.status === 403) throw new ConfluenceError("auth");
    if (answer.status !== 200) throw new ConfluenceError("status", `${answer.status} ${path.split("?")[0]}`);
    return JSON.parse(decoder.decode(bytesOf(answer)));
  }

  /** Every item of a list, page after page (`_links.next`). */
  async all(path) {
    const items = [];
    for (let next = path; next;) {
      const page = await this.json(next);
      items.push(...(page.results ?? []));
      next = page._links?.next ?? "";
    }
    return items;
  }

  /** Finds how the person's token reaches the site: the gateway first, then the site. */
  async connect() {
    let cloudId = "";
    try {
      const info = await this.get(`${this.origin}/_edge/tenant_info`, { signed: false });
      if (info.status === 200) cloudId = String(JSON.parse(decoder.decode(bytesOf(info))).cloudId ?? "");
    } catch {
      // A site without it is read at its own address.
    }
    let refused = false;
    let reason = "";
    for (const base of [cloudId ? `https://api.atlassian.com/ex/confluence/${encodeURIComponent(cloudId)}` : "", this.origin]) {
      if (!base) continue;
      let answer;
      try {
        answer = await this.get(`${base}/wiki/api/v2/spaces?limit=1`);
      } catch (error) {
        // The person hasn't given the credential, or the administrator hasn't let them.
        if (/credential/iu.test(String(error?.message ?? ""))) throw new ConfluenceError("credential", error.message);
        reason = String(error?.message ?? error);
        continue;
      }
      if (answer.status === 200) {
        this.base = base;
        return;
      }
      if (answer.status === 401 || answer.status === 403) refused = true;
      else reason = `HTTP ${answer.status}`;
    }
    throw new ConfluenceError(refused ? "auth" : "site", reason);
  }

  /** The spaces the person can read, by name. */
  async spaces() {
    const spaces = await this.all("/wiki/api/v2/spaces?limit=250&status=current");
    return spaces.map((space) => ({ id: String(space.id), key: space.key, name: space.name || space.key }))
      .sort((left, right) => left.name.localeCompare(right.name));
  }

  /** A person's name by their account id, or "" when the site won't say. */
  async person(accountId) {
    this.people ??= new Map();
    if (!this.people.has(accountId)) {
      const name = await this.json(`/wiki/rest/api/user?accountId=${encodeURIComponent(accountId)}`)
        .then((user) => String(user.displayName ?? user.publicName ?? "")).catch(() => "");
      this.people.set(accountId, name);
    }
    return this.people.get(accountId);
  }

  async download(attachment) {
    const answer = await this.get(`${this.base}/wiki/rest/api/content/${encodeURIComponent(attachment.pageId)}/child/attachment/${encodeURIComponent(attachment.id)}/download`);
    if (answer.status !== 200) throw new ConfluenceError("status", `${answer.status} attachment`);
    return bytesOf(answer);
  }
}

/** A space as a bundle; `progress(done, total)` follows the pages read, then the pages made. */
export async function confluenceSpaceBundle(site, space, { labels, progress = () => undefined }) {
  const losses = new Map();
  const lose = (what, why, count = 1) => {
    const loss = losses.get(what) ?? { what, why, count: 0 };
    loss.count += count;
    losses.set(what, loss);
  };
  const pages = (await site.all(`/wiki/api/v2/spaces/${encodeURIComponent(space.id)}/pages?limit=250&status=current&body-format=storage`))
    .sort((left, right) => (left.position ?? 0) - (right.position ?? 0) || String(left.title).localeCompare(String(right.title)));
  const ids = new Set(pages.map((page) => String(page.id)));
  const byTitle = new Map(pages.map((page) => [page.title, page]));
  const attachments = new Map();
  const records = [];
  for (const [index, page] of pages.entries()) {
    const id = String(page.id);
    const files = new Map();
    const listed = [];
    for (const item of await site.all(`/wiki/api/v2/pages/${encodeURIComponent(id)}/attachments?limit=250`)) {
      // Named as the export names it, so either way gives the same file.
      const path = `attachments/${id}/${String(item.id).replace(/^att/u, "")}${/\.[^./]+$/u.exec(item.title ?? "")?.[0] ?? ""}`;
      files.set(item.title, path);
      attachments.set(path, { pageId: id, id: String(item.id), size: Number(item.fileSize) || 0 });
      listed.push(`<a href="${escapeHtml(path)}">${escapeHtml(item.title ?? "")}</a>`);
    }
    const { comments, resolved } = await pageComments(site, id);
    if (resolved) lose(labels.lossResolvedComments, labels.lossResolvedCommentsWhy, resolved);
    const storage = page.body?.storage?.value ?? "";
    for (const accountId of mentionedAccounts(storage)) await site.person(accountId);
    const html = storageToExportHtml(storage, {
      page: (title, key) => (!key || key === space.key) && byTitle.has(title) ? `${byTitle.get(title).id}.html` : "",
      attachment: (filename) => files.get(filename) ?? "",
      user: (accountId) => site.people?.get(accountId) ?? "",
    });
    records.push({
      fileName: `${id}.html`, id: `page-${id}`, title: page.title ?? "",
      parent: page.parentId && ids.has(String(page.parentId)) ? `page-${page.parentId}` : "space",
      html: async () => `<div id="main-content">${html}</div><div class="pageSection group">${listed.join("")}</div>`,
      comments: async () => comments,
    });
    progress(index + 1, pages.length);
  }
  return spaceBundle({
    spaceKey: space.key, spaceName: space.name, pages: records, root: "", lose, losses,
    labels: { ...labels, lossMissingPage: labels.lossOtherPage, lossMissingPageWhy: labels.lossOtherPageWhy,
      lossMissingFile: labels.lossUnreadFile, lossMissingFileWhy: labels.lossUnreadFileWhy },
    files: {
      has: (path) => (attachments.get(path)?.size ?? Infinity) <= LARGEST_FILE,
      bytes: (path) => site.download(attachments.get(path)),
    },
    progress: (done, total) => progress(done, total),
  });
}

/**
 * A page's comments, footer and inline, each discussion's first comment with
 * its replies after it — a reply to a reply answers the discussion, as
 * Leafen's do. Each names who wrote it and when; an inline one opens with
 * the words it was made on, since it comes as the page's. A resolved
 * discussion is only counted: a migration moves open discussions, as
 * Notion's API gives them.
 */
async function pageComments(site, pageId) {
  const said = [];
  let resolved = 0;
  const one = async (comment, parentId, opening = "") => {
    const authorId = comment.version?.authorId;
    said.push({
      id: `comment-${comment.id}`, parentId, author: (authorId ? await site.person(authorId) : "") || "Confluence",
      createdAt: String(comment.version?.createdAt ?? ""), text: opening + storageText(comment.body?.storage?.value ?? ""),
    });
  };
  for (const kind of ["footer-comments", "inline-comments"]) {
    for (const comment of await site.all(`/wiki/api/v2/pages/${encodeURIComponent(pageId)}/${kind}?limit=250&body-format=storage`)) {
      const open = comment.resolutionStatus !== "resolved";
      const selection = String(comment.properties?.inlineOriginalSelection ?? "").trim();
      if (open) await one(comment, "", selection ? `“${selection}”\n` : "");
      else resolved += 1;
      const waiting = [comment.id];
      while (waiting.length) {
        const parent = waiting.shift();
        for (const reply of await site.all(`/wiki/api/v2/${kind}/${encodeURIComponent(parent)}/children?limit=250&body-format=storage`)) {
          if (open) await one(reply, `comment-${comment.id}`);
          else resolved += 1;
          waiting.push(reply.id);
        }
      }
    }
  }
  return { comments: said, resolved };
}

/** A comment's storage format as plain lines. */
function storageText(storage) {
  const doc = new DOMParser().parseFromString(`<div>${storage}</div>`, "text/html");
  for (const br of doc.querySelectorAll("br")) br.replaceWith("\n");
  for (const block of doc.querySelectorAll("p, li, h1, h2, h3, h4, h5, h6, pre, blockquote")) block.append("\n");
  return (doc.body.textContent ?? "").replace(/\n{3,}/gu, "\n\n").trim();
}
