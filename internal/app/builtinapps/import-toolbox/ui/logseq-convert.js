// A Logseq graph folder (its ZIP) as a Leafen bundle: the graph as one top
// page, its pages under it (a namespace's pages under their parent) and its
// journals under a "Journals" page, newest first; each page's outline as
// nested bullets — headings, tasks, code, math, quotes, tables, pictures and
// files from its assets — with [[links]], #tags and aliases as page mentions,
// ((block references)) as the text they point at, page properties as a table
// at the top, and what has no place counted. Ids come from page names and
// blocks' content, so the graph's next state carries the same migration on.

import { codeLanguage, createBundle, isPicture } from "./bundle.js";
import { markdownRuns } from "./markdown.js";
import { readZip } from "./zip.js";

const TASKS = /^(TODO|DOING|NOW|LATER|WAITING|IN-PROGRESS|DONE|CANCELED|CANCELLED)\s+/u;
const PROPERTY = /^([A-Za-z][\w-]*)::\s?(.*)$/u;
const MONTHS = ["January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"];
const DAYS = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];

function stableId(text) {
  let hash = 0x811c9dc5;
  for (const char of text) {
    hash ^= char.codePointAt(0);
    hash = Math.imul(hash, 0x01000193) >>> 0;
  }
  return hash.toString(16).padStart(8, "0");
}
const key15 = (text) => `${stableId(text)}${stableId(`${text}\u0000`)}`.slice(0, 15);
const basename = (path) => path.split("/").pop() ?? path;

/** A journal's title in the graph's format (`:journal/page-title-format`, "MMM do, yyyy" by default). */
export function journalTitle(year, month, day, format = "MMM do, yyyy") {
  const date = new Date(Date.UTC(year, month - 1, day));
  const ordinal = (n) => `${n}${n % 100 >= 11 && n % 100 <= 13 ? "th" : ["th", "st", "nd", "rd"][n % 10] ?? "th"}`;
  const tokens = {
    yyyy: String(year), yy: String(year).slice(2), MMMM: MONTHS[month - 1], MMM: MONTHS[month - 1].slice(0, 3),
    MM: String(month).padStart(2, "0"), M: String(month), do: ordinal(day), dd: String(day).padStart(2, "0"), d: String(day),
    EEEE: DAYS[date.getUTCDay()], EEE: DAYS[date.getUTCDay()].slice(0, 3), E: DAYS[date.getUTCDay()].slice(0, 3),
  };
  return format.replace(/yyyy|yy|MMMM|MMM|MM|M|do|dd|d|EEEE|EEE|E/gu, (token) => tokens[token]);
}

/** The outline of a page's Markdown: its properties and nested blocks, each with its lines. */
export function outline(text) {
  const lines = text.replace(/\r\n?/gu, "\n").split("\n");
  const properties = [];
  let index = 0;
  while (index < lines.length && PROPERTY.test(lines[index].trim()) && !/^\s*-\s/u.test(lines[index])) {
    const [, key, value] = PROPERTY.exec(lines[index].trim());
    properties.push({ key, value });
    index += 1;
  }
  const root = { children: [], lines: [] };
  const stack = [{ indent: -1, block: root }];
  let current = null;
  for (; index < lines.length; index += 1) {
    const line = lines[index];
    const bullet = /^(\s*)-(?:\s(.*)|$)/u.exec(line);
    if (bullet) {
      const indent = bullet[1].replace(/\t/gu, "  ").length;
      while (stack.length > 1 && stack.at(-1).indent >= indent) stack.pop();
      current = { children: [], lines: [bullet[2] ?? ""], indent };
      stack.at(-1).block.children.push(current);
      stack.push({ indent, block: current });
    } else if (current) {
      current.lines.push(line.replace(new RegExp(`^\\s{0,${current.indent + 2}}`, "u"), ""));
    } else if (line.trim()) {
      root.children.push({ children: [], lines: [line], indent: 0 });
    }
  }
  return { properties, blocks: root.children };
}

/** Turns a graph's ZIP into a bundle; `progress(done, total)` follows the pages. */
export async function logseqBundle(file, { labels, progress = () => undefined }) {
  const zip = await readZip(file);
  const marker = zip.names.find((name) => /(^|\/)(pages|journals)\/[^/]+\.(md|org)$/iu.test(name) || /(^|\/)logseq\/config\.edn$/u.test(name));
  if (!marker) throw new Error(labels.noGraph);
  const root = marker.slice(0, marker.search(/(pages|journals|logseq)\//u));
  const graph = root.replace(/\/$/u, "").split("/").pop() || "Logseq";
  const losses = new Map();
  const lose = (what, why, count = 1) => {
    const loss = losses.get(what) ?? { what, why, count: 0 };
    loss.count += count;
    losses.set(what, loss);
  };
  const config = zip.has(`${root}logseq/config.edn`) ? await zip.text(`${root}logseq/config.edn`) : "";
  const format = /:journal\/page-title-format\s+"([^"]+)"/u.exec(config)?.[1] ?? "MMM do, yyyy";

  // The pages: their names from their files (a namespace's "/" written "___"
  // or %2F), a title:: property over that, and their aliases.
  const pages = [];
  for (const name of zip.names.filter((entry) => entry.startsWith(root))) {
    const local = name.slice(root.length);
    if (/^(pages|journals)\/[^/]+\.org$/iu.test(local)) { lose(labels.lossOrg, labels.lossOrgWhy); continue; }
    if (/^whiteboards\//u.test(local)) { lose(labels.lossWhiteboard, labels.lossWhiteboardWhy); continue; }
    const journal = /^journals\/(\d{4})_(\d{2})_(\d{2})\.md$/u.exec(local);
    if (!journal && !/^pages\/[^/]+\.md$/u.test(local)) continue;
    const text = await zip.text(name);
    const parsed = outline(text);
    const property = (key) => parsed.properties.find((item) => item.key.toLowerCase() === key)?.value?.trim() ?? "";
    let title;
    if (journal) title = journalTitle(Number(journal[1]), Number(journal[2]), Number(journal[3]), format);
    else {
      const stem = basename(local).replace(/\.md$/u, "");
      let decoded = stem.replaceAll("___", "/");
      try { decoded = decodeURIComponent(decoded); } catch { /* a name with a bare % stays */ }
      title = property("title") || decoded;
    }
    pages.push({
      title, journal: journal ? `${journal[1]}_${journal[2]}_${journal[3]}` : "", parsed, path: name,
      aliases: property("alias").split(",").map((alias) => alias.trim().replace(/^\[\[|\]\]$/gu, "")).filter(Boolean),
    });
  }
  if (pages.length === 0) throw new Error(labels.noGraph);
  const byName = new Map();
  for (const page of pages) {
    page.id = page.journal ? `j-${page.journal}` : `p-${key15(page.title.toLowerCase())}`;
    byName.set(page.title.toLowerCase(), page);
  }
  for (const page of pages) for (const alias of page.aliases) if (!byName.has(alias.toLowerCase())) byName.set(alias.toLowerCase(), page);
  // A namespace's parents, as pages of their own when the graph has none.
  const placeholders = new Map();
  const parentOf = (page) => {
    if (page.journal) return "journals";
    const parts = page.title.split("/");
    if (parts.length < 2) return "graph";
    const parentName = parts.slice(0, -1).join("/");
    let parent = byName.get(parentName.toLowerCase()) ?? placeholders.get(parentName.toLowerCase());
    if (!parent) {
      parent = { title: parentName, id: `p-${key15(parentName.toLowerCase())}`, placeholder: true, parsed: { properties: [], blocks: [] }, aliases: [] };
      placeholders.set(parentName.toLowerCase(), parent);
      parent.parent = parentOf(parent);
    }
    return parent.id;
  };
  for (const page of pages) page.parent = parentOf(page);

  // Blocks with an id:: property, for ((references)) to them.
  const referenced = new Map();
  const collect = (blocks) => {
    for (const block of blocks) {
      const id = block.lines.map((line) => /^id::\s*(\S+)/u.exec(line.trim())?.[1]).find(Boolean);
      if (id) referenced.set(id, block.lines.filter((line) => !PROPERTY.test(line.trim()))[0]?.replace(TASKS, "") ?? "");
      collect(block.children);
    }
  };
  for (const page of pages) collect(page.parsed.blocks);

  const assets = new Map(zip.names.filter((name) => name.startsWith(`${root}assets/`)).map((name) => [basename(name).toLowerCase(), name]));
  const usedAssets = new Set();
  const bundle = createBundle({ sourceId: `logseq:${graph}`, sourceName: graph });
  bundle.page({ id: "graph", title: graph });
  const journals = pages.filter((page) => page.journal).sort((left, right) => right.journal.localeCompare(left.journal));
  if (journals.length) bundle.page({ id: "journals", parentId: "graph", title: labels.journals });
  const ordered = [...placeholders.values(), ...pages.filter((page) => !page.journal)]
    .sort((left, right) => left.title.split("/").length - right.title.split("/").length || left.title.localeCompare(right.title));
  let done = 0;
  for (const page of [...ordered, ...journals]) {
    // A namespace's page shows its own name under its parent.
    bundle.page({ id: page.id, parentId: page.parent, title: page.journal ? page.title : page.title.split("/").pop() });
    if (!page.placeholder) {
      await new PageConverter({ bundle, page, byName, referenced, assets, usedAssets, zip, labels, lose }).convert();
      done += 1;
      progress(done, pages.length);
    }
  }
  const unused = [...assets.values()].filter((path) => !usedAssets.has(path)).length;
  if (unused) lose(labels.lossUnusedAsset, labels.lossUnusedAssetWhy, unused);
  const blob = await bundle.build({ rootIds: ["graph"], losses: [...losses.values()] });
  return { blob, sourceId: `logseq:${graph}`, graph, counts: bundle.counts(), losses: [...losses.values()] };
}

class PageConverter {
  constructor(options) {
    Object.assign(this, options);
    this.seen = new Map();
  }

  idFor(type, content, text) {
    const key = `${type}\u0000${(text ?? []).map((run) => run.insert).join("")}\u0000${JSON.stringify(content)}`;
    const count = this.seen.get(key) ?? 0;
    this.seen.set(key, count + 1);
    return `${this.page.id}-${key15(key)}-${count}`;
  }

  block(parentId, type, content, text) {
    return this.bundle.block({ id: this.idFor(type, content, text), pageId: this.page.id, parentId, type, content, text });
  }

  async convert() {
    const shown = this.page.parsed.properties.filter((item) => !["title", "alias"].includes(item.key.toLowerCase()));
    if (shown.length) {
      const table = this.block(null, "table", { tableWidth: 2, hasColumnHeader: false, hasRowHeader: true });
      for (const item of shown) this.block(table, "table_row", { cells: [[{ text: item.key }], [{ text: item.value }]] });
      this.lose(this.labels.lossProperties, this.labels.lossPropertiesWhy);
    }
    for (const block of this.page.parsed.blocks) await this.one(block, null);
  }

  async one(block, parentId) {
    const lines = [];
    for (const line of block.lines) {
      const property = PROPERTY.exec(line.trim());
      if (property) {
        if (!["id", "collapsed", "heading"].includes(property[1].toLowerCase())) this.lose(this.labels.lossBlockProperties, this.labels.lossBlockPropertiesWhy);
        continue;
      }
      if (/^(SCHEDULED|DEADLINE):\s*<[^>]+>/u.test(line.trim())) this.lose(this.labels.lossSchedule, this.labels.lossScheduleWhy);
      lines.push(line);
    }
    while (lines.length && !lines.at(-1).trim()) lines.pop();
    const text = lines.join("\n");
    const first = lines[0] ?? "";
    let id;
    const fence = /^```\s*([\w+#.-]*)\s*\n([\s\S]*?)\n?```\s*$/u.exec(text) ?? /^#\+BEGIN_SRC\s*(\S*)\n([\s\S]*?)\n#\+END_SRC\s*$/iu.exec(text);
    const query = /^#\+BEGIN_QUERY\n([\s\S]*?)\n#\+END_QUERY\s*$/iu.exec(text) ?? /^(\{\{query\s[\s\S]*\}\})$/u.exec(text.trim());
    const math = /^\$\$\n?([\s\S]*?)\n?\$\$\s*$/u.exec(text);
    const quote = /^#\+BEGIN_QUOTE\n([\s\S]*?)\n#\+END_QUOTE\s*$/iu.exec(text);
    if (query) {
      this.lose(this.labels.lossQuery, this.labels.lossQueryWhy);
      id = this.block(parentId, "code", { language: codeLanguage("") }, [{ insert: query[1] }]);
    } else if (fence) {
      id = this.block(parentId, "code", { language: codeLanguage(fence[1]) }, fence[2] ? [{ insert: fence[2] }] : []);
    } else if (math) {
      id = this.block(parentId, "equation", { expression: math[1].trim() });
    } else if (quote || /^>\s?/u.test(first)) {
      const body = quote ? quote[1] : lines.map((line) => line.replace(/^>\s?/u, "")).join("\n");
      id = this.block(parentId, "quote", {}, this.runs(body).runs);
    } else if (/^\|.*\|\s*$/u.test(first) && lines.length > 1 && /^\|?\s*:?-{3,}/u.test(lines[1])) {
      id = this.table(lines, parentId);
    } else {
      const heading = /^(#{1,6})\s+(.*)$/su.exec(text);
      const task = TASKS.exec(text);
      let body = heading ? heading[2] : task ? text.slice(task[0].length) : text;
      if (/^\[#[ABC]\]\s*/u.test(body)) {
        this.lose(this.labels.lossPriority, this.labels.lossPriorityWhy);
        body = body.replace(/^\[#[ABC]\]\s*/u, "");
      }
      const { runs, embeds } = this.runs(body);
      if (heading) {
        const level = Math.min(heading[1].length, 4);
        id = this.block(parentId, block.children.length ? `toggle_heading_${Math.min(level, 3)}` : `heading_${level}`, {}, runs);
      } else if (task) {
        const state = task[1];
        if (state === "CANCELED" || state === "CANCELLED") this.lose(this.labels.lossCanceled, this.labels.lossCanceledWhy);
        id = this.block(parentId, "to_do", { checked: ["DONE", "CANCELED", "CANCELLED"].includes(state) }, runs);
      } else if (runs.length === 0 && embeds.length && block.children.length === 0) {
        // A bullet that only shows a picture or a file is that picture or file.
        await this.embeds(embeds, parentId);
        return;
      } else {
        id = this.block(parentId, "bulleted_list_item", {}, runs);
      }
      await this.embeds(embeds, id);
    }
    for (const child of block.children) await this.one(child, id);
  }

  table(lines, parentId) {
    const rows = lines.filter((line, index) => index !== 1 && line.trim().startsWith("|"))
      .map((line) => line.trim().replace(/^\||\|$/gu, "").split("|").map((cell) => cell.trim()));
    const width = Math.max(...rows.map((row) => row.length));
    const id = this.block(parentId, "table", { tableWidth: width, hasColumnHeader: true, hasRowHeader: false });
    for (const row of rows) {
      const cells = row.map((cell) => this.runs(cell).runs.map((run) => {
        const { mention, pageId, ...marks } = run.attributes ?? {};
        return { text: run.insert, ...marks };
      }));
      while (cells.length < width) cells.push([]);
      this.block(id, "table_row", { cells });
    }
    return id;
  }

  async embeds(embeds, parentId) {
    for (const path of embeds) {
      this.usedAssets.add(path);
      const content = await this.bundle.file({ id: key15(`asset:${path}`), name: basename(path), data: new Blob([await this.zip.bytes(path)]) });
      if (!content) {
        this.lose(this.labels.lossKept, this.labels.lossKeptWhy);
        continue;
      }
      this.block(parentId, isPicture(basename(path)) ? "image" : "file", content);
    }
  }

  /** A block's text as runs, and the assets it shows. */
  runs(source) {
    const embeds = [];
    const asset = (target) => {
      const path = this.assets.get(basename(decodeURIComponent(target.split("?")[0])).toLowerCase());
      return /(^|\/)assets\//u.test(target) && path ? path : "";
    };
    const mention = (name, label, marks, at, tag) => {
      const page = this.byName.get(name.trim().toLowerCase());
      if (page) {
        at.add(label || (tag ? `#${name}` : name), { ...marks, mention: "page", pageId: page.id });
        return;
      }
      at.add(label || (tag ? `#${name}` : `[[${name}]]`), marks);
      this.lose(this.labels.lossPageNoFile, this.labels.lossPageNoFileWhy);
    };
    const runs = markdownRuns(source, {
      patterns: [
        ["math", /^\$([^$\s][^$]*?)\$/u], ["ref", /^\(\(([0-9a-f-]{36})\)\)/u], ["macro", /^\{\{([\s\S]*?)\}\}/u],
        ["wikiLabel", /^\[([^\]]+)\]\(\[\[([^\]]+)\]\]\)/u], ["wiki", /^\[\[([^\]]+)\]\]/u], ["tagLong", /^#\[\[([^\]]+)\]\]/u],
        ["tag", /^#([\p{L}\p{N}_/-]+)/u], ["highlight", /^(\^\^|==)(?=\S)([\s\S]+?\S)\1/u],
      ],
      handle: (name, found, marks, at) => {
        switch (name) {
          case "math": at.add(found[1], { ...marks, equation: found[1] }); return true;
          case "ref": {
            const text = this.referenced.get(found[1]);
            this.lose(this.labels.lossBlockRef, this.labels.lossBlockRefWhy);
            if (text !== undefined) at.walk(text, marks);
            else at.add(found[0], marks);
            return true;
          }
          case "macro": {
            const embed = /^embed\s+\[\[([^\]]+)\]\]$/u.exec(found[1].trim());
            const blockEmbed = /^embed\s+\(\(([0-9a-f-]{36})\)\)$/u.exec(found[1].trim());
            if (embed) mention(embed[1], "", marks, at, false);
            else if (blockEmbed && this.referenced.has(blockEmbed[1])) at.walk(this.referenced.get(blockEmbed[1]), marks);
            else at.add(found[0], marks);
            this.lose(embed || blockEmbed ? this.labels.lossEmbed : this.labels.lossLogseqMacro, embed || blockEmbed ? this.labels.lossEmbedWhy : this.labels.lossLogseqMacroWhy);
            return true;
          }
          case "wikiLabel": mention(found[2], found[1], marks, at, false); return true;
          case "wiki": mention(found[1], "", marks, at, false); return true;
          case "tagLong": mention(found[1], "", marks, at, true); return true;
          case "tag": mention(found[1], "", marks, at, true); return true;
          case "highlight": at.walk(found[2], { ...marks, color: "yellow_background" }); return true;
          case "image": case "link": {
            if (/^(https?|mailto):/iu.test(found[2])) return name === "link" ? false : (at.add(found[1] || found[2], { ...marks, link: found[2] }), true);
            const path = asset(found[2]);
            if (path) embeds.push(path);
            else this.lose(this.labels.lossMissingAsset, this.labels.lossMissingAssetWhy);
            if (name === "link" || !path) at.walk(found[1] || found[2], marks);
            return true;
          }
        }
        return false;
      },
    });
    return { runs, embeds };
  }
}
