// A folder of Markdown notes as a Leafen bundle (decision "설계 우선 Notion
// 이사" 6) — an Obsidian vault, or what Bear, Joplin, Craft or Roam export:
// the folder as one top page, its folders as pages and its notes as pages
// under them, each note's Markdown as blocks — headings, lists and tasks,
// quotes, callouts, code, math, tables, rules, pictures and files — with its
// text's formatting, [[links]], aliases and links to other notes as page
// mentions, highlights as a yellow background, front matter as a table at
// the top; and what has no place counted as the bundle's losses. A tool's own
// syntax is made Markdown first (`rewrite`). Ids come from the paths and
// blocks' content, so the folder's next state carries the same migration on.

import { codeLanguage, createBundle, isPicture } from "./bundle.js";
import { markdownRuns } from "./markdown.js";

const CALLOUTS = {
  note: ["📝", "blue_background"], abstract: ["📋", "blue_background"], summary: ["📋", "blue_background"], info: ["ℹ️", "blue_background"],
  todo: ["☑️", "blue_background"], tip: ["💡", "green_background"], hint: ["💡", "green_background"], important: ["💡", "green_background"],
  success: ["✅", "green_background"], check: ["✅", "green_background"], done: ["✅", "green_background"], question: ["❓", "yellow_background"],
  help: ["❓", "yellow_background"], faq: ["❓", "yellow_background"], warning: ["⚠️", "orange_background"], caution: ["⚠️", "orange_background"],
  attention: ["⚠️", "orange_background"], failure: ["❌", "red_background"], fail: ["❌", "red_background"], missing: ["❌", "red_background"],
  danger: ["⛔", "red_background"], error: ["⛔", "red_background"], bug: ["🐞", "red_background"], example: ["📎", "purple_background"],
  quote: ["💬", "gray_background"], cite: ["💬", "gray_background"],
};
const QUERY_LANGUAGES = new Set(["dataview", "dataviewjs", "tasks", "query"]);

const extension = (path) => (/\.([^./]+)$/u.exec(path)?.[1] ?? "").toLowerCase();
const basename = (path) => path.split("/").pop() ?? path;
const stem = (path) => basename(path).replace(/\.[^.]+$/u, "");
// A file kept under an opaque ID (Joplin's _resources/<32 hex>.pdf) takes the
// name its link or picture gives it, "spec" becoming "spec.pdf"; any other
// keeps its own.
const fileName = (path, given) => {
  const label = (given ?? "").trim();
  if (!label || /[\\/]/u.test(label) || !/^[0-9a-f]{32}$/iu.test(stem(path))) return basename(path);
  return extension(path) && extension(label) !== extension(path) ? `${label}.${extension(path)}` : label;
};
const folderOf = (path) => path.includes("/") ? path.slice(0, path.lastIndexOf("/")) : "";

function stableId(text) {
  let hash = 0x811c9dc5;
  for (const char of text) {
    hash ^= char.codePointAt(0);
    hash = Math.imul(hash, 0x01000193) >>> 0;
  }
  return hash.toString(16).padStart(8, "0");
}
const key15 = (text) => `${stableId(text)}${stableId(`${text}\u0000`)}`.slice(0, 15);

/** A small YAML front matter: keys with a value, a [list] or "- item" lines. */
export function frontMatter(text) {
  const values = [];
  let current = null;
  for (const line of text.split("\n")) {
    const item = /^\s*-\s+(.*)$/u.exec(line);
    if (item && current) {
      current.value.push(item[1].replace(/^["']|["']$/gu, ""));
      continue;
    }
    const pair = /^([^:#\s][^:]*):\s*(.*)$/u.exec(line);
    if (!pair) continue;
    const raw = pair[2].trim();
    const list = /^\[(.*)\]$/u.exec(raw);
    current = { key: pair[1].trim(), value: list ? list[1].split(",").map((part) => part.trim().replace(/^["']|["']$/gu, "")).filter(Boolean)
      : raw ? [raw.replace(/^["']|["']$/gu, "")] : [] };
    values.push(current);
  }
  return values;
}

/** Markdown's blocks: a light parser for what Obsidian notes hold. */
export function parseBlocks(lines) {
  const blocks = [];
  let index = 0;
  const indentOf = (line) => (/^[ \t]*/u.exec(line)?.[0] ?? "").replace(/\t/gu, "    ").length;
  const isBlank = (line) => line.trim() === "";
  const listItem = /^(\s*)([-*+]|\d+[.)])\s+(?:\[([ xX/-])\]\s+)?(.*)$/u;
  const fence = /^\s*(```+|~~~+)\s*([\w#+-]*)/u;
  const tableRule = /^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)*\|?\s*$/u;
  const startsBlock = (line) => /^\s{0,3}(#{1,6}\s|>|```|~~~|\$\$|---\s*$|\*\*\*\s*$|___\s*$)/u.test(line) || listItem.test(line);
  while (index < lines.length) {
    const line = lines[index];
    if (isBlank(line)) { index += 1; continue; }
    const heading = /^\s{0,3}(#{1,6})\s+(.*?)\s*#*\s*$/u.exec(line);
    if (heading) { blocks.push({ type: "heading", level: heading[1].length, text: heading[2] }); index += 1; continue; }
    const opened = fence.exec(line);
    if (opened) {
      const marker = opened[1];
      const body = [];
      index += 1;
      while (index < lines.length && !lines[index].trim().startsWith(marker)) { body.push(lines[index]); index += 1; }
      index += 1;
      blocks.push({ type: "code", lang: opened[2].toLowerCase(), text: body.join("\n") });
      continue;
    }
    if (/^\s*\$\$/u.test(line)) {
      const body = [line.replace(/^\s*\$\$/u, "")];
      let closed = /\$\$\s*$/u.test(body[0]) && body[0].trim() !== "";
      if (closed) body[0] = body[0].replace(/\$\$\s*$/u, "");
      index += 1;
      while (!closed && index < lines.length) {
        if (/\$\$\s*$/u.test(lines[index])) { body.push(lines[index].replace(/\$\$\s*$/u, "")); closed = true; } else body.push(lines[index]);
        index += 1;
      }
      blocks.push({ type: "math", text: body.join("\n").trim() });
      continue;
    }
    if (/^\s{0,3}(---|\*\*\*|___)\s*$/u.test(line)) { blocks.push({ type: "hr" }); index += 1; continue; }
    if (/^\s{0,3}>/u.test(line)) {
      const inner = [];
      while (index < lines.length && /^\s{0,3}>/u.test(lines[index])) { inner.push(lines[index].replace(/^\s{0,3}>\s?/u, "")); index += 1; }
      const callout = /^\[!([\w-]+)\]([+-]?)\s*(.*)$/u.exec(inner[0] ?? "");
      if (callout) blocks.push({ type: "callout", kind: callout[1].toLowerCase(), fold: callout[2], title: callout[3], children: parseBlocks(inner.slice(1)) });
      else blocks.push({ type: "quote", children: parseBlocks(inner) });
      continue;
    }
    const item = listItem.exec(line);
    if (item) {
      const items = [];
      const base = indentOf(line);
      while (index < lines.length) {
        const current = listItem.exec(lines[index]);
        if (!current || indentOf(lines[index]) !== base) break;
        const sub = [];
        index += 1;
        while (index < lines.length && (isBlank(lines[index]) ? index + 1 < lines.length && indentOf(lines[index + 1]) > base && !isBlank(lines[index + 1])
          : indentOf(lines[index]) > base)) {
          sub.push(lines[index]);
          index += 1;
        }
        const shift = Math.min(...sub.filter((part) => !isBlank(part)).map(indentOf), Infinity);
        items.push({ ordered: /\d/u.test(current[2]), task: current[3] !== undefined, checked: current[3] !== undefined && current[3] !== " ",
          text: current[4], children: parseBlocks(sub.map((part) => part.replace(/\t/gu, "    ").slice(Number.isFinite(shift) ? shift : 0))) });
        while (index < lines.length && isBlank(lines[index]) && listItem.test(lines[index + 1] ?? "") && indentOf(lines[index + 1]) === base) index += 1;
      }
      blocks.push({ type: "list", items });
      continue;
    }
    if (line.includes("|") && tableRule.test(lines[index + 1] ?? "")) {
      const cells = (row) => row.trim().replace(/^\|/u, "").replace(/\|$/u, "").split(/(?<!\\)\|/u).map((cell) => cell.trim().replaceAll("\\|", "|"));
      const rows = [cells(line)];
      index += 2;
      while (index < lines.length && lines[index].includes("|") && !isBlank(lines[index])) { rows.push(cells(lines[index])); index += 1; }
      blocks.push({ type: "table", rows });
      continue;
    }
    if (/^\s*!\[\[[^\]]+\]\]\s*$/u.test(line) || /^\s*!\[[^\]]*\]\([^)]+\)\s*$/u.test(line)) {
      blocks.push({ type: "embed", text: line.trim() });
      index += 1;
      continue;
    }
    const paragraph = [];
    while (index < lines.length && !isBlank(lines[index]) && (paragraph.length === 0 || !startsBlock(lines[index]))) {
      paragraph.push(lines[index]);
      index += 1;
    }
    blocks.push({ type: "paragraph", text: paragraph.join("\n") });
  }
  return blocks;
}

/**
 * A folder, `{ name, files: [{ path, size, read() }] }`, as a bundle named by
 * `sourceId`; `rewrite(text, path, lose)` makes a note's text Markdown first,
 * counting with `lose(what, why)` what it can't carry.
 */
export async function notesBundle(vault, { labels, sourceId, rewrite = (text) => text, progress = () => undefined }) {
  const files = vault.files.filter((file) => !/(^|\/)\.(obsidian|trash|git)(\/|$)/u.test(file.path) && !basename(file.path).startsWith("."));
  const notes = files.filter((file) => extension(file.path) === "md" && !file.path.endsWith(".excalidraw.md"));
  const others = files.filter((file) => !notes.includes(file));
  const losses = new Map();
  const lose = (what, why, count = 1) => {
    const loss = losses.get(what) ?? { what, why, count: 0 };
    loss.count += count;
    losses.set(what, loss);
  };
  const bundle = createBundle({ sourceId, sourceName: vault.name });
  bundle.page({ id: "vault", title: vault.name });

  // Folders as pages, under the vault's page.
  const folderIds = new Map([["", "vault"]]);
  const folderPage = (folder) => {
    if (folderIds.has(folder)) return folderIds.get(folder);
    const parent = folderPage(folderOf(folder));
    const id = `dir-${key15(folder)}`;
    bundle.page({ id, parentId: parent, title: basename(folder) });
    folderIds.set(folder, id);
    return id;
  };
  // A folder becomes a page once a note or a file no note shows needs it.
  for (const note of [...notes].sort((a, b) => a.path.localeCompare(b.path))) folderPage(folderOf(note.path));

  // Notes by name and alias for [[links]], the nearest when two share a name.
  const noteIds = new Map();
  const byName = new Map();
  const texts = new Map();
  for (const note of notes) {
    noteIds.set(note.path, `note-${key15(note.path)}`);
    const name = stem(note.path).toLowerCase();
    byName.set(name, [...(byName.get(name) ?? []), note.path]);
  }
  let read = 0;
  for (const note of notes) {
    texts.set(note.path, rewrite(new TextDecoder().decode(await note.read()).replace(/\r\n?/gu, "\n"), note.path, lose));
    read += 1;
    progress(read, notes.length);
    const matter = /^---\n([\s\S]*?)\n---(\n|$)/u.exec(texts.get(note.path));
    for (const entry of matter ? frontMatter(matter[1]) : []) {
      if (entry.key.toLowerCase() !== "aliases" && entry.key.toLowerCase() !== "alias") continue;
      for (const alias of entry.value) byName.set(alias.toLowerCase(), [...(byName.get(alias.toLowerCase()) ?? []), note.path]);
    }
  }
  for (const [, paths] of byName) if (new Set(paths).size > 1) lose(labels.lossSameName, labels.lossSameNameWhy);
  const filesByName = new Map();
  for (const file of others) {
    const name = basename(file.path).toLowerCase();
    filesByName.set(name, [...(filesByName.get(name) ?? []), file]);
  }
  const nearest = (paths, from) => [...paths].sort((a, b) => (folderOf(a) === folderOf(from) ? -1 : 0) - (folderOf(b) === folderOf(from) ? -1 : 0)
    || a.split("/").length - b.split("/").length || a.localeCompare(b))[0];
  const resolveNote = (target, from) => {
    const clean = target.replace(/\.md$/iu, "").trim();
    const direct = notes.find((note) => note.path.replace(/\.md$/iu, "").toLowerCase() === clean.toLowerCase());
    if (direct) return direct.path;
    const paths = byName.get(basename(clean).toLowerCase());
    return paths ? nearest(paths, from) : null;
  };
  const resolveFile = (target, from) => {
    const clean = decodeURIComponent(target).replace(/^\.\//u, "");
    const relative = folderOf(from) ? `${folderOf(from)}/${clean}` : clean;
    const exact = others.find((file) => file.path === clean || file.path === relative);
    if (exact) return exact;
    const found = filesByName.get(basename(clean).toLowerCase());
    return found ? found.sort((a, b) => a.path.split("/").length - b.path.split("/").length)[0] : null;
  };

  const usedFiles = new Set();
  // A file Leafen doesn't keep (an SVG, an empty file) is counted, not moved.
  const fileContent = async (file, label) => {
    usedFiles.add(file.path);
    const content = await bundle.file({ id: key15(`file:${file.path}`), name: fileName(file.path, label), data: new Blob([await file.read()]) });
    if (!content) lose(labels.lossKept, labels.lossKeptWhy);
    return content;
  };

  for (const note of notes) {
    const id = noteIds.get(note.path);
    bundle.page({ id, parentId: folderPage(folderOf(note.path)), title: stem(note.path) });
    await new NoteConverter({ bundle, id, path: note.path, labels, lose, resolveNote, resolveFile, noteIds, fileContent })
      .convert(texts.get(note.path));
  }
  // Files no note shows stay in their folder's page; Canvas and Excalidraw
  // drawings among them, as files.
  for (const file of others) {
    if (usedFiles.has(file.path)) continue;
    if (extension(file.path) === "canvas") lose(labels.lossCanvas, labels.lossCanvasWhy);
    if (file.path.endsWith(".excalidraw.md") || extension(file.path) === "excalidraw") lose(labels.lossExcalidraw, labels.lossExcalidrawWhy);
    const parent = folderPage(folderOf(file.path));
    const content = await fileContent(file);
    if (content) bundle.block({ id: `${parent}-f-${key15(file.path)}`, pageId: parent, parentId: null, type: isPicture(file.path) ? "image" : "file", content });
  }
  const blob = await bundle.build({ rootIds: ["vault"], losses: [...losses.values()] });
  return { blob, sourceId, name: vault.name, counts: bundle.counts(), losses: [...losses.values()] };
}

class NoteConverter {
  constructor(options) {
    Object.assign(this, options);
    this.seen = new Map();
  }

  idFor(type, content, text) {
    const key = `${type}\u0000${(text ?? []).map((run) => run.insert).join("")}\u0000${JSON.stringify(content)}`;
    const count = this.seen.get(key) ?? 0;
    this.seen.set(key, count + 1);
    return `${this.id}-${key15(key)}-${count}`;
  }

  block(parentId, type, content, text) {
    return this.bundle.block({ id: this.idFor(type, content, text), pageId: this.id, parentId, type, content, text });
  }

  async convert(text) {
    const matter = /^---\n([\s\S]*?)\n---(\n|$)/u.exec(text);
    let body = text;
    if (matter) {
      body = text.slice(matter[0].length);
      const entries = frontMatter(matter[1]);
      if (entries.length) {
        const table = this.block(null, "table", { tableWidth: 2, hasColumnHeader: false, hasRowHeader: true });
        for (const entry of entries) this.block(table, "table_row", { cells: [[{ text: entry.key }], [{ text: entry.value.join(", ") }]] });
        this.lose(this.labels.lossFrontMatter, this.labels.lossFrontMatterWhy);
      }
    }
    // A note that opens with its own name as a heading: the page's title says it.
    const lead = /^\s*#[ \t]+(.+?)(?:[ \t]+#+)?[ \t]*(?:\n|$)/u.exec(body);
    if (lead && lead[1].trim() === stem(this.path)) body = body.slice(lead[0].length);
    if (/%%[\s\S]*?%%/u.test(body)) {
      this.lose(this.labels.lossComment, this.labels.lossCommentWhy);
      body = body.replace(/%%[\s\S]*?%%/gu, "");
    }
    await this.blocks(parseBlocks(body.split("\n")), null);
  }

  async blocks(list, parentId) {
    for (const block of list) await this.one(block, parentId);
  }

  async one(block, parentId) {
    switch (block.type) {
      case "heading": {
        const { runs, embeds } = this.runs(block.text);
        this.block(parentId, `heading_${Math.min(block.level, 4)}`, {}, runs);
        return this.embeds(embeds, parentId);
      }
      case "paragraph": {
        const { runs, embeds } = this.runs(block.text);
        if (runs.length) this.block(parentId, "paragraph", {}, runs);
        return this.embeds(embeds, parentId);
      }
      case "embed": {
        // A note embedded on its own line stays as a mention of the note.
        const { runs, embeds } = this.runs(block.text);
        if (runs.length) this.block(parentId, "paragraph", {}, runs);
        return this.embeds(embeds, parentId);
      }
      case "code": {
        if (QUERY_LANGUAGES.has(block.lang)) this.lose(this.labels.lossQuery, this.labels.lossQueryWhy);
        this.block(parentId, "code", { language: codeLanguage(block.lang) }, block.text ? [{ insert: block.text }] : []);
        return undefined;
      }
      case "math": this.block(parentId, "equation", { expression: block.text }); return undefined;
      case "hr": this.block(parentId, "divider", {}); return undefined;
      case "quote": {
        const [first, ...rest] = block.children;
        const { runs, embeds } = first?.type === "paragraph" ? this.runs(first.text) : { runs: [], embeds: [] };
        const id = this.block(parentId, "quote", {}, runs);
        await this.embeds(embeds, id);
        return this.blocks(first?.type === "paragraph" ? rest : block.children, id);
      }
      case "callout": {
        const [icon, color] = CALLOUTS[block.kind] ?? ["📌", "gray_background"];
        if (block.fold) this.lose(this.labels.lossFold, this.labels.lossFoldWhy);
        const title = block.title || block.kind[0].toUpperCase() + block.kind.slice(1);
        const { runs } = this.runs(title);
        const id = this.block(parentId, "callout", { icon, iconType: "emoji", color }, runs.map((run) => ({ ...run, attributes: { ...(run.attributes ?? {}), bold: true } })));
        return this.blocks(block.children, id);
      }
      case "list":
        for (const item of block.items) {
          const { runs, embeds } = this.runs(item.text);
          const id = item.task ? this.block(parentId, "to_do", { checked: item.checked }, runs)
            : this.block(parentId, item.ordered ? "numbered_list_item" : "bulleted_list_item", {}, runs);
          await this.embeds(embeds, id);
          await this.blocks(item.children, id);
        }
        return undefined;
      case "table": {
        const width = Math.max(...block.rows.map((row) => row.length));
        const id = this.block(parentId, "table", { tableWidth: width, hasColumnHeader: true, hasRowHeader: false });
        for (const row of block.rows) {
          const cells = row.map((cell) => this.runs(cell).runs.map((run) => {
            const { mention, pageId, ...marks } = run.attributes ?? {};
            return { text: run.insert, ...marks };
          }));
          while (cells.length < width) cells.push([]);
          this.block(id, "table_row", { cells });
        }
        return undefined;
      }
    }
    return undefined;
  }

  async embeds(embeds, parentId) {
    for (const embed of embeds) {
      if (embed.file) {
        const content = await this.fileContent(embed.file, embed.label);
        if (content) this.block(parentId, isPicture(embed.file.path) ? "image" : "file", content);
      } else if (embed.url) {
        this.block(parentId, "bookmark", { url: embed.url });
      }
    }
  }

  /** A line's runs, each with its formatting, and the pictures and files it embeds. */
  runs(source) {
    const embeds = [];
    const link = (target, label, marks, at) => {
      const [noteName, anchor] = target.split("#");
      if (anchor !== undefined) this.lose(this.labels.lossAnchor, this.labels.lossAnchorWhy);
      const path = noteName.trim() ? this.resolveNote(noteName, this.path) : this.path;
      if (path) at.add(label || noteName.trim() || stem(path), { ...marks, mention: "page", pageId: this.noteIds.get(path) });
      else {
        at.add(label || target, marks);
        this.lose(this.labels.lossBrokenLink, this.labels.lossBrokenLinkWhy);
      }
    };
    const embed = (target, marks, at) => {
      const [name, anchor] = target.split("|")[0].split("#");
      const file = this.resolveFile(name, this.path);
      if (file && extension(file.path) !== "md") { embeds.push({ file }); return; }
      const note = this.resolveNote(name, this.path);
      if (note) {
        this.lose(this.labels.lossNoteEmbed, this.labels.lossNoteEmbedWhy);
        if (anchor !== undefined) this.lose(this.labels.lossAnchor, this.labels.lossAnchorWhy);
        at.add(stem(note), { ...marks, mention: "page", pageId: this.noteIds.get(note) });
        return;
      }
      at.add(`![[${target}]]`, marks);
      this.lose(this.labels.lossBrokenLink, this.labels.lossBrokenLinkWhy);
    };
    const runs = markdownRuns(source, {
      patterns: [
        ["math", /^\$([^$\s][^$]*?)\$/u], ["wikiEmbed", /^!\[\[([^\]]+)\]\]/u], ["wiki", /^\[\[([^\]]+)\]\]/u],
        ["highlight", /^==(?=\S)([\s\S]+?\S)==/u], ["footnote", /^\[\^[^\]]+\]/u], ["blockId", /^\s\^[\w-]+$/u],
      ],
      handle: (name, found, marks, at) => {
        switch (name) {
          case "math": at.add(found[1], { ...marks, equation: found[1] }); return true;
          case "wikiEmbed": embed(found[1], marks, at); return true;
          case "wiki": { const [target, label] = found[1].split("|"); link(target, label, marks, at); return true; }
          case "highlight": at.walk(found[1], { ...marks, color: "yellow_background" }); return true;
          case "footnote": at.add(found[0], marks); this.lose(this.labels.lossFootnote, this.labels.lossFootnoteWhy); return true;
          case "blockId": this.lose(this.labels.lossBlockId, this.labels.lossBlockIdWhy); return true;
          case "image": {
            if (/^https?:/iu.test(found[2])) embeds.push({ url: found[2] });
            else {
              const file = this.resolveFile(found[2], this.path);
              if (file) embeds.push({ file, label: found[1] });
              else { at.add(found[1] || found[2], marks); this.lose(this.labels.lossBrokenLink, this.labels.lossBrokenLinkWhy); }
            }
            return true;
          }
          case "link": {
            if (/^(https?|mailto):/iu.test(found[2])) return false;
            if (/\.md(#.*)?$/iu.test(decodeURIComponent(found[2]))) link(decodeURIComponent(found[2]).replace(/\.md(?=#|$)/iu, ""), found[1], marks, at);
            else {
              const file = this.resolveFile(found[2].split("#")[0], this.path);
              if (file) embeds.push({ file, label: found[1] });
              at.walk(found[1], marks);
            }
            return true;
          }
        }
        return false;
      },
    });
    if (/(^|\s)#[\p{L}\p{N}_/-]+/u.test(source)) this.lose(this.labels.lossTags, this.labels.lossTagsWhy);
    return { runs, embeds };
  }
}
