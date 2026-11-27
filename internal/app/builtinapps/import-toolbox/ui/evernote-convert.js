// Evernote notebooks from their ENEX export (a notebook's "Export notebook…"
// as ENEX) as a Leafen bundle, as Notion's Evernote import makes them: each
// notebook a database — its notes rows with their tags, when each was made
// and changed and where a clipped one came from — under one "Evernote" page,
// each note's ENML its row's page, its pictures and files from the export,
// and what has no place counted as losses. Ids come from the notebook, the
// note's title and when it was made, so a notebook's next export carries the
// same migration on.

import { codeLanguage, createBundle, isPicture } from "./bundle.js";
import { md5 } from "./md5.js";
import { readZip } from "./zip.js";

const VOID = new Set(["area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "param", "source", "track", "wbr"]);
const OPTION_COLORS = ["blue", "green", "orange", "purple", "pink", "yellow", "red", "brown", "gray"];
const HIGHLIGHTS = { yellow: "yellow", green: "green", blue: "blue", purple: "purple", pink: "pink", red: "red", orange: "orange" };
const MEDIA_EXTENSIONS = { "image/png": "png", "image/jpeg": "jpg", "image/gif": "gif", "image/webp": "webp", "application/pdf": "pdf",
  "audio/mpeg": "mp3", "audio/wav": "wav", "video/mp4": "mp4", "text/plain": "txt" };

function stableId(text) {
  let hash = 0x811c9dc5;
  for (const char of text) {
    hash ^= char.codePointAt(0);
    hash = Math.imul(hash, 0x01000193) >>> 0;
  }
  return hash.toString(16).padStart(8, "0");
}
const key15 = (text) => `${stableId(text)}${stableId(`${text}\u0000`)}`.slice(0, 15);
const childText = (element, name) => [...element.children].find((child) => child.nodeName === name)?.textContent?.trim() ?? "";
const enexDate = (text) => {
  const match = /^(\d{4})(\d{2})(\d{2})T(\d{2})(\d{2})(\d{2})Z$/u.exec(text);
  return match ? `${match[1]}-${match[2]}-${match[3]}T${match[4]}:${match[5]}:${match[6]}.000Z` : "";
};

/** The notebooks in what the person chose: .enex files, or ZIPs of them. */
async function notebooksIn(chosen) {
  const found = [];
  for (const file of chosen) {
    if (/\.zip$/iu.test(file.name)) {
      const zip = await readZip(file);
      for (const name of zip.names.filter((entry) => /\.enex$/iu.test(entry) && !entry.split("/").pop().startsWith("._"))) {
        found.push({ name: name.split("/").pop(), text: () => zip.text(name) });
      }
    } else if (/\.enex$/iu.test(file.name)) {
      found.push({ name: file.name, text: () => file.text() });
    }
  }
  return found;
}

/** Turns ENEX files into a bundle; `progress(done)` counts the notes. */
export async function evernoteBundle(chosen, { labels, progress = () => undefined }) {
  const notebooks = await notebooksIn(chosen);
  if (notebooks.length === 0) throw new Error(labels.noEnex);
  const losses = new Map();
  const lose = (what, why, count = 1) => {
    const loss = losses.get(what) ?? { what, why, count: 0 };
    loss.count += count;
    losses.set(what, loss);
  };
  // Every Evernote move into a workspace is one source: a notebook moved again carries on.
  const bundle = createBundle({ sourceId: "evernote", sourceName: "Evernote" });
  bundle.page({ id: "evernote", title: "Evernote" });
  const parser = new DOMParser();
  let done = 0;
  for (const notebook of notebooks) {
    const title = notebook.name.replace(/\.enex$/iu, "");
    const doc = parser.parseFromString(await notebook.text(), "text/xml");
    if (doc.querySelector("parsererror") || doc.documentElement.nodeName !== "en-export") {
      throw new Error(labels.notEnex.replace("{name}", notebook.name));
    }
    const notes = [...doc.documentElement.children].filter((child) => child.nodeName === "note");
    const key = key15(`notebook:${title}`);
    const tags = [...new Set(notes.flatMap((note) => [...note.children].filter((child) => child.nodeName === "tag")
      .map((tag) => tag.textContent?.trim() ?? "").filter(Boolean)))];
    const options = tags.map((tag, index) => ({ id: `t${key15(`${title}\u0000${tag}`)}`, name: tag, color: OPTION_COLORS[index % OPTION_COLORS.length] }));
    const property = (name) => `${name}-${key}`;
    const database = `nb-${key}`;
    const dataSourceId = `ds-${key}`;
    bundle.database({
      id: database, parentId: "evernote", title, dataSourceId, viewId: `vw-${key}`, viewName: labels.viewName,
      properties: [
        { id: property("title"), name: labels.propTitle, type: "title" },
        { id: property("tags"), name: labels.propTags, type: "multi_select", config: { options } },
        { id: property("created"), name: labels.propCreated, type: "date" },
        { id: property("updated"), name: labels.propUpdated, type: "date" },
        { id: property("source"), name: labels.propSource, type: "url" },
      ],
    });
    const seen = new Map();
    for (const note of notes) {
      const name = childText(note, "title") || labels.untitled;
      const created = enexDate(childText(note, "created"));
      const updated = enexDate(childText(note, "updated"));
      let rowId = `note-${key15(`${title}\u0000${name}\u0000${created}`)}`;
      const repeat = seen.get(rowId) ?? 0;
      seen.set(rowId, repeat + 1);
      if (repeat) rowId = `${rowId}-${repeat}`;
      const attributes = [...note.children].find((child) => child.nodeName === "note-attributes");
      const source = attributes ? childText(attributes, "source-url") : "";
      const own = new Set([...note.children].filter((child) => child.nodeName === "tag").map((tag) => tag.textContent?.trim()));
      const values = { [property("title")]: { type: "title", value: name } };
      if (own.size) values[property("tags")] = { type: "multi_select", value: options.filter((option) => own.has(option.name)).map((option) => option.id) };
      if (created) values[property("created")] = { type: "date", value: { start: created } };
      if (updated) values[property("updated")] = { type: "date", value: { start: updated } };
      if (/^https?:\/\//iu.test(source)) values[property("source")] = { type: "url", value: source };
      bundle.row({ id: rowId, databaseId: database, dataSourceId, values });
      if (attributes && childText(attributes, "reminder-time")) lose(labels.lossReminder, labels.lossReminderWhy);
      if (attributes && childText(attributes, "latitude")) lose(labels.lossLocation, labels.lossLocationWhy);
      await new NoteConverter({ bundle, rowId, databaseId: database, note, labels, lose,
        createdAt: Date.parse(created) || 1, updatedAt: Date.parse(updated) || Date.parse(created) || 1 }).convert();
      done += 1;
      progress(done);
    }
  }
  const blob = await bundle.build({ rootIds: ["evernote"], losses: [...losses.values()] });
  return { blob, sourceId: "evernote", notebooks: notebooks.length, counts: bundle.counts(), losses: [...losses.values()] };
}

class NoteConverter {
  constructor(options) {
    Object.assign(this, options);
    this.seen = new Map();
    this.media = new Map();
    this.used = new Set();
    this.styled = false;
  }

  idFor(type, content, text) {
    const key = `${type}\u0000${(text ?? []).map((run) => run.insert).join("")}\u0000${JSON.stringify(content)}`;
    const count = this.seen.get(key) ?? 0;
    this.seen.set(key, count + 1);
    return `${this.rowId}-${key15(key)}-${count}`;
  }

  block(parentId, type, content, text) {
    return this.bundle.rowBlock({ id: this.idFor(type, content, text), rowId: this.rowId, databaseId: this.databaseId, parentId: parentId ?? "",
      type, content, text, createdAt: this.createdAt, updatedAt: this.updatedAt });
  }

  async convert() {
    // The note's files, by the MD5 of their bytes as <en-media hash> names them.
    for (const resource of [...this.note.children].filter((child) => child.nodeName === "resource")) {
      const data = [...resource.children].find((child) => child.nodeName === "data");
      const bytes = Uint8Array.from(atob((data?.textContent ?? "").replace(/\s+/gu, "")), (char) => char.charCodeAt(0));
      const mime = childText(resource, "mime") || "application/octet-stream";
      const attributes = [...resource.children].find((child) => child.nodeName === "resource-attributes");
      const hash = md5(bytes);
      const named = attributes ? childText(attributes, "file-name") : "";
      this.media.set(hash, { bytes, mime, name: named || `${hash.slice(0, 12)}.${MEDIA_EXTENSIONS[mime] ?? "bin"}` });
    }
    const enml = (childText(this.note, "content"))
      .replace(/<\?xml[^>]*\?>/u, "").replace(/<!DOCTYPE[^>]*>/iu, "")
      .replace(/<([a-zA-Z][\w:-]*)((?:\s+[^<>]*?)?)\s*\/>/gu, (_, tag, attributes) =>
        VOID.has(tag.toLowerCase()) ? `<${tag}${attributes}>` : `<${tag}${attributes}></${tag}>`);
    const doc = new DOMParser().parseFromString(`<!doctype html><html><body>${enml}</body></html>`, "text/html");
    const root = doc.querySelector("en-note") ?? doc.body;
    await this.nodes(root.childNodes, null);
    // Evernote's tasks, as to-dos at the note's end.
    for (const task of [...this.note.children].filter((child) => child.nodeName === "task")) {
      const text = childText(task, "title");
      if (text) this.block(null, "to_do", { checked: childText(task, "taskStatus") === "completed" }, [{ insert: text }]);
    }
    // Files the note keeps without showing them.
    for (const [hash, media] of this.media) if (!this.used.has(hash)) await this.mediaBlock(null, hash);
    if (this.styled) this.lose(this.labels.lossTextStyle, this.labels.lossTextStyleWhy);
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
    const tag = node.tagName.toLowerCase();
    const style = node.getAttribute("style") ?? "";
    if (/^h[1-6]$/u.test(tag)) {
      this.block(parentId, `heading_${Math.min(Number(tag[1]), 4)}`, {}, this.runs(node));
      return;
    }
    switch (tag) {
      case "ul": case "ol": return this.list(node, parentId, tag === "ol", /--en-todo:\s*true/u.test(style));
      case "table": return this.table(node, parentId);
      case "blockquote": {
        // The first line is the quote's own text; what follows sits in it.
        const lines = [...node.childNodes].filter((child) => child.nodeType === Node.ELEMENT_NODE || child.textContent?.trim());
        const first = lines[0]?.nodeType === Node.ELEMENT_NODE && /^(div|p)$/u.test(lines[0].tagName.toLowerCase()) ? lines[0] : null;
        const id = this.block(parentId, "quote", {}, first ? this.runs(first) : this.runs(node));
        return first ? this.nodes(lines.slice(1), id) : undefined;
      }
      case "pre": return void this.block(parentId, "code", { language: codeLanguage("") }, [{ insert: node.textContent ?? "" }]);
      case "hr": return void this.block(parentId, "divider", {});
      case "en-media": {
        const hash = node.getAttribute("hash") ?? "";
        return this.media.has(hash) ? this.mediaBlock(parentId, hash) : this.lose(this.labels.lossMissingMedia, this.labels.lossMissingMediaWhy);
      }
      case "en-crypt":
        this.lose(this.labels.lossCrypt, this.labels.lossCryptWhy);
        return void this.block(parentId, "paragraph", {}, [{ insert: this.labels.encrypted, attributes: { italic: true } }]);
      case "br": case "style": case "script": return;
    }
    if (/--en-codeblock:\s*true/u.test(style)) {
      const lines = [...node.children].length ? [...node.children].map((line) => line.textContent ?? "") : [node.textContent ?? ""];
      return void this.block(parentId, "code", { language: codeLanguage("") }, [{ insert: lines.join("\n") }]);
    }
    // A line: a div or p whose content is text; one holding blocks is gone through.
    if (node.querySelector("div, p, ul, ol, table, blockquote, pre, hr, h1, h2, h3, h4, h5, h6, en-crypt")) return this.nodes(node.childNodes, parentId);
    const todo = node.querySelector("en-todo");
    const text = this.runs(node);
    if (todo) this.block(parentId, "to_do", { checked: todo.getAttribute("checked") === "true" }, text);
    else if (text.length) this.block(parentId, "paragraph", {}, text);
    for (const media of node.querySelectorAll("en-media")) await this.node(media, parentId);
  }

  async list(element, parentId, numbered, todo) {
    for (const item of [...element.children].filter((child) => child.tagName.toLowerCase() === "li")) {
      const own = document.createElement("div");
      const nested = [];
      for (const child of item.childNodes) {
        if (child.nodeType === Node.ELEMENT_NODE && /^(ul|ol|table)$/u.test(child.tagName.toLowerCase())) nested.push(child);
        else own.appendChild(child.cloneNode(true));
      }
      const checked = /--en-checked:\s*true/u.test(item.getAttribute("style") ?? "");
      const id = todo ? this.block(parentId, "to_do", { checked }, this.runs(own))
        : this.block(parentId, numbered ? "numbered_list_item" : "bulleted_list_item", {}, this.runs(own));
      for (const media of own.querySelectorAll("en-media")) await this.node(media, id);
      for (const child of nested) await this.node(child, id);
    }
  }

  table(element, parentId) {
    const rows = [...element.querySelectorAll(":scope > tbody > tr, :scope > thead > tr, :scope > tr")];
    if (rows.length === 0) return;
    const width = Math.max(...rows.map((row) => row.children.length));
    const id = this.block(parentId, "table", { tableWidth: width, hasColumnHeader: false, hasRowHeader: false });
    let nested = false;
    for (const row of rows) {
      const cells = [...row.children].map((cell) => {
        if (cell.querySelector("en-media, ul, ol, table")) nested = true;
        return this.runs(cell).map((run) => ({ text: run.insert, ...(run.attributes ?? {}) }));
      });
      while (cells.length < width) cells.push([]);
      this.block(id, "table_row", { cells });
    }
    if (nested) this.lose(this.labels.lossCellBlocks, this.labels.lossCellBlocksWhy);
  }

  async mediaBlock(parentId, hash) {
    const media = this.media.get(hash);
    this.used.add(hash);
    const content = await this.bundle.file({ id: key15(`media:${hash}`), name: media.name, contentType: media.mime, data: new Blob([media.bytes]) });
    if (!content) {
      this.lose(this.labels.lossKept, this.labels.lossKeptWhy);
      return;
    }
    this.block(parentId, isPicture(media.name, media.mime) ? "image" : "file", content);
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
      if (tag === "br") { add("\n", marks); return; }
      if (/^(en-media|en-todo|ul|ol|table|style|script)$/u.test(tag)) return;
      const next = { ...marks };
      const style = node.getAttribute("style") ?? "";
      if (tag === "b" || tag === "strong" || /font-weight:\s*(bold|[6-9]00)/u.test(style)) next.bold = true;
      if (tag === "i" || tag === "em" || /font-style:\s*italic/u.test(style)) next.italic = true;
      if (tag === "u" || /text-decoration:[^;]*underline/u.test(style)) next.underline = true;
      if (tag === "s" || tag === "strike" || tag === "del" || /text-decoration:[^;]*line-through/u.test(style)) next.strikethrough = true;
      if (tag === "code" || tag === "tt") next.code = true;
      const highlight = /--en-highlight:\s*(\w+)/u.exec(style)?.[1];
      if (highlight) next.color = `${HIGHLIGHTS[highlight] ?? "yellow"}_background`;
      else if (/(^|;)\s*(color|background-color)\s*:/u.test(style) || tag === "font") this.styled = true;
      if (tag === "a") {
        const href = node.getAttribute("href") ?? "";
        if (/^evernote:/iu.test(href)) this.lose(this.labels.lossNoteLink, this.labels.lossNoteLinkWhy);
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
