// A Confluence space's HTML export (Space settings › Export › HTML) as a
// Leafen bundle: the space as one top page with its page tree, each page's
// body as blocks with formatted text, embedded pictures and attachments as
// files, and what Leafen has no place for counted as the bundle's losses.
// Ids come from Confluence's page ids and attachment paths, so the next
// export of the same space makes the same ones.

import { codeLanguage, createBundle, isPicture } from "./bundle.js";
import { readZip } from "./zip.js";


const PANELS = {
  information: { icon: "ℹ️", color: "blue_background" },
  tip: { icon: "✅", color: "green_background" },
  note: { icon: "📝", color: "yellow_background" },
  warning: { icon: "⚠️", color: "red_background" },
};

const LAYOUT = /(^|\s)(contentLayout2|columnLayout|sectionColumnWrapper|sectionMacro|cell|innerCell|columnMacro|table-wrap)(\s|$)/u;

function stableId(text) {
  let hash = 0x811c9dc5;
  for (const char of text) {
    hash ^= char.codePointAt(0);
    hash = Math.imul(hash, 0x01000193) >>> 0;
  }
  return hash.toString(16).padStart(8, "0");
}

// A bundle's file ids are 15 letters, digits, "_" or "-".
function fileKey(path) {
  return `${stableId(path)}${stableId(`${path}\u0000`)}`.slice(0, 15);
}

/** Turns an export into a bundle; `progress(done, total)` follows the pages. */
export async function confluenceBundle(file, { labels, progress = () => undefined }) {
  const zip = await readZip(file);
  const indexPath = zip.names.filter((name) => /(^|\/)index\.html$/iu.test(name)).sort((a, b) => a.length - b.length)[0];
  if (!indexPath) throw new Error(labels.noIndex);
  const root = indexPath.slice(0, indexPath.length - "index.html".length);
  const parser = new DOMParser();
  const indexDoc = parser.parseFromString(await zip.text(indexPath), "text/html");
  const spaceName = (indexDoc.querySelector("#title-text")?.textContent ?? indexDoc.title ?? "").replace(/\s*:\s*.*$/u, "").trim()
    || labels.space;
  const losses = new Map();
  const lose = (what, why, count = 1) => {
    const loss = losses.get(what) ?? { what, why, count: 0 };
    loss.count += count;
    losses.set(what, loss);
  };

  // The page tree: each page file and the page it sits under.
  const pageFiles = zip.names.filter((name) => name.startsWith(root) && name.endsWith(".html") && name !== indexPath
    && !name.slice(root.length).includes("/"));
  const pages = new Map();
  const idOf = (fileName) => {
    const match = /_(\d+)\.html$/u.exec(fileName) ?? /^(\d+)\.html$/u.exec(fileName);
    return `page-${match ? match[1] : stableId(fileName)}`;
  };
  for (const path of pageFiles) {
    const fileName = path.slice(root.length);
    pages.set(fileName, { path, fileName, id: idOf(fileName), parent: "space", title: "" });
  }
  // The pages in the tree's order, as the space lists them: the archive's
  // own order is its files' (2026-11-18: a next export's new "Changelog",
  // after "Design" in the tree, came first among Home's pages, its file
  // standing before Design's in the ZIP). Pages the tree doesn't name follow.
  const treeOrder = new Set();
  for (const link of indexDoc.querySelectorAll("li > a[href$='.html']")) {
    const page = pages.get(decodeURIComponent(link.getAttribute("href") ?? ""));
    if (!page) continue;
    page.title = link.textContent?.trim() ?? "";
    const parentLink = link.parentElement?.parentElement?.closest("li")?.querySelector(":scope > a[href$='.html']");
    const parent = parentLink ? pages.get(decodeURIComponent(parentLink.getAttribute("href") ?? "")) : null;
    if (parent) page.parent = parent.id;
    treeOrder.add(page);
  }
  for (const page of pages.values()) treeOrder.add(page);

  // The space's key, which a renamed space keeps, names the source.
  const keyCell = [...indexDoc.querySelectorAll("th")].find((cell) => cell.textContent?.trim() === "Key")?.nextElementSibling;
  const spaceKey = keyCell?.textContent?.trim() || root.replace(/\/$/u, "") || spaceName;
  return spaceBundle({
    spaceKey, spaceName, files: zip, root, labels, lose, losses, progress,
    pages: [...treeOrder].map((page) => ({
      ...page,
      html: () => zip.text(page.path),
      // A page the tree didn't name is titled by its own heading.
      titleOf: (doc) => {
        const heading = (doc.querySelector("#title-text")?.textContent ?? doc.title ?? "").trim();
        return (heading.startsWith(`${spaceName} : `) ? heading.slice(spaceName.length + 3) : heading) || page.fileName.replace(/\.html$/u, "");
      },
    })),
  });
}

/**
 * A space as a bundle, however it was read: the space as one top page and
 * its pages under their parents in the tree's order, each page's HTML — an
 * export's, or the API's storage format made into it — as blocks, and its
 * comments when the API gave them (`comments`). `files` gives the
 * attachments by their path (`has`, `bytes`).
 */
export async function spaceBundle({ spaceKey, spaceName, pages, files, root, labels, lose, losses, progress = () => undefined }) {
  const sourceId = `confluence:${spaceKey}`;
  const bundle = createBundle({ sourceId, sourceName: spaceName });
  bundle.page({ id: "space", title: spaceName });
  const byFileName = new Map(pages.map((page) => [page.fileName, page]));
  const ids = new Set(pages.map((page) => page.id));
  for (const page of pages) if (page.parent !== "space" && !ids.has(page.parent)) page.parent = "space";
  // Pages under their parents, in the tree's order.
  const ordered = [];
  const placed = new Set();
  const place = (parentId) => {
    for (const page of pages) {
      if (page.parent === parentId && !placed.has(page.id)) {
        placed.add(page.id);
        ordered.push(page);
        place(page.id);
      }
    }
  };
  place("space");
  for (const page of pages) if (!placed.has(page.id)) { page.parent = "space"; ordered.push(page); }

  const parser = new DOMParser();
  let done = 0;
  for (const page of ordered) {
    const doc = parser.parseFromString(await page.html(), "text/html");
    bundle.page({ id: page.id, parentId: page.parent, title: page.title || page.titleOf?.(doc) || page.fileName.replace(/\.html$/u, "") });
    await new PageConverter({ zip: files, root, bundle, pages: byFileName, page, lose, labels }).convert(doc);
    for (const comment of await page.comments?.() ?? []) bundle.comment({ ...comment, pageId: page.id });
    done += 1;
    progress(done, ordered.length);
  }
  const blob = await bundle.build({ rootIds: ["space"], losses: [...losses.values()] });
  return { blob, sourceId, space: spaceName, counts: bundle.counts(), losses: [...losses.values()] };
}

class PageConverter {
  constructor({ zip, root, bundle, pages, page, lose, labels }) {
    Object.assign(this, { zip, root, bundle, pages, page, lose, labels });
    this.seen = new Map();
    this.usedFiles = new Set();
  }

  // A block's id comes from what it is — its kind, text and content — and
  // how many like it came before on the page, not from where it stands: a
  // line added above in the next export leaves the others' ids as they were.
  idFor(type, content, text) {
    const key = `${type}\u0000${(text ?? []).map((run) => run.insert).join("")}\u0000${JSON.stringify(content)}`;
    const count = this.seen.get(key) ?? 0;
    this.seen.set(key, count + 1);
    return `${this.page.id}-${stableId(key)}${stableId(`${key}\u0000`)}-${count}`;
  }

  block(parentId, type, content, text) {
    return this.bundle.block({ id: this.idFor(type, content, text), pageId: this.page.id, parentId, type, content, text });
  }

  async convert(doc) {
    const body = doc.querySelector("#main-content") ?? doc.body;
    await this.nodes(body.childNodes, null);
    // Attachments the page lists but doesn't show, as file blocks at its end.
    for (const link of doc.querySelectorAll(".pageSection.group a[href*='attachments/']")) {
      const path = this.resolve(link.getAttribute("href") ?? "");
      if (!path || this.usedFiles.has(path.replace(/^missing:/u, ""))) continue;
      await this.fileBlock(null, "file", path, link.textContent?.trim() || path.split("/").pop());
    }
  }

  resolve(href) {
    if (!href || /^[a-z]+:/iu.test(href) || href.startsWith("#")) return "";
    const path = `${this.root}${decodeURIComponent(href.split(/[?#]/u)[0])}`.replace(/\/\.\//gu, "/");
    return this.zip.has(path) ? path : `missing:${path}`;
  }

  async fileBlock(parentId, type, path, name) {
    if (path.startsWith("missing:")) {
      this.usedFiles.add(path.slice("missing:".length));
      this.lose(this.labels.lossMissingFile, this.labels.lossMissingFileWhy);
      return;
    }
    this.usedFiles.add(path);
    let data;
    try {
      data = new Blob([await this.zip.bytes(path)]);
    } catch {
      this.lose(this.labels.lossMissingFile, this.labels.lossMissingFileWhy);
      return;
    }
    const content = await this.bundle.file({ id: fileKey(path), name, data });
    if (!content) {
      this.lose(this.labels.lossKept, this.labels.lossKeptWhy);
      return;
    }
    this.block(parentId, type, content);
  }

  async nodes(list, parentId) {
    for (const node of [...list]) await this.node(node, parentId);
  }

  async node(node, parentId) {
    if (node.nodeType === Node.TEXT_NODE) {
      const text = node.textContent?.trim();
      if (text) this.block(parentId, "paragraph", {}, [{ insert: text }]);
      return;
    }
    if (node.nodeType !== Node.ELEMENT_NODE) return;
    const element = node;
    const tag = element.tagName.toLowerCase();
    const classes = element.className && typeof element.className === "string" ? element.className : "";
    if (/^h[1-6]$/u.test(tag)) {
      const level = Number(tag[1]);
      this.block(parentId, `heading_${Math.min(level, 4)}`, {}, this.runs(element));
      return;
    }
    switch (tag) {
      case "p": return this.paragraph(element, parentId);
      case "ul": case "ol": return this.list(element, parentId, tag === "ol");
      case "pre": return this.code(element, parentId);
      case "blockquote": return this.quote(element, parentId);
      case "hr": return void this.block(parentId, "divider", {});
      case "table": return this.table(element, parentId);
      case "img": return this.image(element, parentId);
      case "br": case "style": case "script": return;
    }
    if (tag === "div" || tag === "section") {
      if (/(^|\s)(code|preformatted)(\s|$)/u.test(classes) && element.querySelector("pre")) return this.code(element.querySelector("pre"), parentId);
      const panel = /confluence-information-macro-(information|tip|note|warning)/u.exec(classes);
      if (panel) return this.callout(element, parentId, PANELS[panel[1]]);
      if (/(^|\s)expand-container(\s|$)/u.test(classes)) return this.toggle(element, parentId);
      if (/(^|\s)panel(\s|$)/u.test(classes)) return this.callout(element, parentId, { icon: "📌", color: "gray_background" });
      if (/(^|\s)columnLayout(\s|$)/u.test(classes) && element.querySelectorAll(":scope > .cell").length > 1) {
        this.lose(this.labels.lossColumns, this.labels.lossColumnsWhy);
      }
      const macro = element.getAttribute("data-macro-name") ?? /(?:^|\s)([\w-]+)-macro(?:\s|$)/u.exec(classes)?.[1];
      if (macro && !LAYOUT.test(classes)) {
        this.lose(this.labels.lossMacro.replace("{name}", macro), this.labels.lossMacroWhy);
      }
      return this.nodes(element.childNodes, parentId);
    }
    // Inline content outside a paragraph is a paragraph of its own.
    const text = this.runs(element);
    if (text.length) this.block(parentId, "paragraph", {}, text);
    await this.images(element, parentId);
  }

  async paragraph(element, parentId) {
    const text = this.runs(element);
    if (text.length) this.block(parentId, "paragraph", {}, text);
    await this.images(element, parentId);
  }

  /** Pictures inside a paragraph or a cell's text, as picture blocks after it. */
  async images(element, parentId) {
    for (const image of element.querySelectorAll("img")) await this.image(image, parentId);
  }

  async image(element, parentId) {
    const classes = element.className || "";
    if (/(^|\s)(emoticon|icon)(\s|$)/u.test(classes)) return;
    const src = element.getAttribute("data-image-src") ?? element.getAttribute("src") ?? "";
    if (/^https?:/iu.test(src)) {
      this.block(parentId, "bookmark", { url: src });
      this.lose(this.labels.lossOutsideImage, this.labels.lossOutsideImageWhy);
      return;
    }
    const path = this.resolve(src);
    if (!path) return;
    const name = element.getAttribute("data-linked-resource-default-alias") ?? element.getAttribute("alt") ?? path.split("/").pop();
    // An attachment Leafen can't show as a picture comes as a file.
    const shown = /\.[^./]+$/u.test(name) ? name : `${name || "image"}${/\.[^./]+$/u.exec(path)?.[0] ?? ""}`;
    await this.fileBlock(parentId, isPicture(shown) ? "image" : "file", path, shown);
  }

  async list(element, parentId, numbered) {
    const tasks = /(^|\s)inline-task-list(\s|$)/u.test(element.className || "");
    for (const item of element.children) {
      if (item.tagName.toLowerCase() !== "li") continue;
      const own = document.createElement("div");
      const nested = [];
      for (const child of item.childNodes) {
        if (child.nodeType === Node.ELEMENT_NODE && /^(ul|ol|div|table|pre|p)$/u.test(child.tagName.toLowerCase()) && child.tagName.toLowerCase() !== "p") nested.push(child);
        else own.appendChild(child.cloneNode(true));
      }
      const id = tasks
        ? this.block(parentId, "to_do", { checked: /(^|\s)checked(\s|$)/u.test(item.className || "") }, this.runs(own))
        : this.block(parentId, numbered ? "numbered_list_item" : "bulleted_list_item", {}, this.runs(own));
      await this.images(own, id);
      for (const child of nested) await this.node(child, id);
    }
  }

  code(element, parentId) {
    const params = element.getAttribute("data-syntaxhighlighter-params") ?? "";
    const brush = /brush:\s*([\w#+-]+)/u.exec(params)?.[1]?.toLowerCase() ?? "plain";
    const text = (element.textContent ?? "").replace(/\n$/u, "");
    this.block(parentId, "code", { language: codeLanguage(brush) }, text ? [{ insert: text }] : []);
  }

  async quote(element, parentId) {
    const paragraphs = element.querySelectorAll(":scope > p");
    if (paragraphs.length <= 1) {
      this.block(parentId, "quote", {}, this.runs(paragraphs[0] ?? element));
      return;
    }
    const id = this.block(parentId, "quote", {}, this.runs(paragraphs[0]));
    for (const paragraph of [...paragraphs].slice(1)) await this.paragraph(paragraph, id);
  }

  async callout(element, parentId, style) {
    const title = element.querySelector(":scope > .title, :scope > .panelHeader, :scope > p.title")?.textContent?.trim();
    const body = element.querySelector(".confluence-information-macro-body, .panelContent") ?? element;
    const content = { icon: style.icon, iconType: "emoji", color: style.color };
    if (title) {
      const id = this.block(parentId, "callout", content, [{ insert: title, attributes: { bold: true } }]);
      await this.nodes(body.childNodes, id);
      return;
    }
    const first = body.querySelector(":scope > p");
    const id = this.block(parentId, "callout", content, this.runs(first ?? body));
    if (first) await this.nodes([...body.childNodes].filter((child) => child !== first), id);
  }

  async toggle(element, parentId) {
    const title = element.querySelector(".expand-control-text")?.textContent?.trim() || this.labels.expand;
    const id = this.block(parentId, "toggle", {}, [{ insert: title }]);
    const body = element.querySelector(".expand-content");
    if (body) await this.nodes(body.childNodes, id);
  }

  async table(element, parentId) {
    const rows = [...element.querySelectorAll(":scope > tbody > tr, :scope > thead > tr, :scope > tr")];
    if (rows.length === 0) return;
    const width = Math.max(...rows.map((row) => row.children.length));
    const header = [...rows[0].children].every((cell) => cell.tagName.toLowerCase() === "th");
    const sideHeader = rows.length > 1 && rows.slice(1).every((row) => row.children[0]?.tagName.toLowerCase() === "th");
    const id = this.block(parentId, "table", { tableWidth: width, hasColumnHeader: header, hasRowHeader: sideHeader });
    let nestedBlocks = false;
    for (const row of rows) {
      // A cell keeps a page link's words; its mention lives only in blocks' text.
      const cells = [...row.children].map((cell) => this.runs(cell).map((run) => {
        const { mention, pageId, ...marks } = run.attributes ?? {};
        return { text: run.insert, ...marks };
      }));
      while (cells.length < width) cells.push([]);
      this.block(id, "table_row", { cells });
      for (const cell of row.children) {
        if (cell.querySelector("img, ul, ol, table, pre")) nestedBlocks = true;
      }
    }
    if (nestedBlocks) this.lose(this.labels.lossCellBlocks, this.labels.lossCellBlocksWhy);
  }

  /** An element's text as runs, each with its formatting. */
  runs(element) {
    const runs = [];
    const add = (insert, attributes) => {
      if (!insert) return;
      const last = runs.at(-1);
      if (last && JSON.stringify(last.attributes ?? {}) === JSON.stringify(attributes ?? {})) last.insert += insert;
      else runs.push(attributes && Object.keys(attributes).length ? { insert, attributes } : { insert });
    };
    const walk = (node, marks) => {
      if (node.nodeType === Node.TEXT_NODE) {
        add((node.textContent ?? "").replace(/\s+/gu, " "), marks);
        return;
      }
      if (node.nodeType !== Node.ELEMENT_NODE) return;
      const tag = node.tagName.toLowerCase();
      const classes = node.className && typeof node.className === "string" ? node.className : "";
      if (tag === "br") { add("\n", marks); return; }
      if (tag === "img" || tag === "style" || tag === "script" || /^(ul|ol|table|pre)$/u.test(tag)) return;
      if (/(^|\s)(user-mention|confluence-userlink)(\s|$)/u.test(classes)) {
        add(`@${(node.textContent ?? "").trim().replace(/^@/u, "")}`, marks);
        this.lose(this.labels.lossMention, this.labels.lossMentionWhy);
        return;
      }
      // A macro inside text — the API's storage format puts some there — keeps its words.
      const macro = node.getAttribute("data-macro-name");
      if (macro) {
        this.lose(this.labels.lossMacro.replace("{name}", macro), this.labels.lossMacroWhy);
        for (const child of node.childNodes) walk(child, marks);
        return;
      }
      if (/(^|\s)status-macro(\s|$)/u.test(classes)) {
        add(`[${(node.textContent ?? "").trim()}]`, { ...marks, bold: true });
        this.lose(this.labels.lossStatus, this.labels.lossStatusWhy);
        return;
      }
      const next = { ...marks };
      if (tag === "strong" || tag === "b") next.bold = true;
      if (tag === "em" || tag === "i") next.italic = true;
      if (tag === "u") next.underline = true;
      if (tag === "s" || tag === "del" || tag === "strike") next.strikethrough = true;
      if (tag === "code" || tag === "tt" || tag === "kbd") next.code = true;
      if (tag === "a") {
        const href = node.getAttribute("href") ?? "";
        const target = this.pages.get(decodeURIComponent(href.split("#")[0]));
        if (target) {
          add((node.textContent ?? "").trim() || target.title, { mention: "page", pageId: target.id });
          return;
        }
        if (/(^|\s)createlink(\s|$)/u.test(classes)) this.lose(this.labels.lossMissingPage, this.labels.lossMissingPageWhy);
        else if (/^(https?|mailto):/iu.test(href)) next.link = href;
      }
      for (const child of node.childNodes) walk(child, next);
    };
    walk(element, {});
    if (runs.length) {
      runs[0].insert = runs[0].insert.replace(/^\s+/u, "");
      runs[runs.length - 1].insert = runs[runs.length - 1].insert.replace(/\s+$/u, "");
    }
    return runs.filter((run) => run.insert.length > 0);
  }
}
