// SiYuan notebooks as a Leafen bundle, from a ZIP of the workspace's data
// folder or of a notebook's ".sy.zip" export: each notebook a page with its
// documents under it (a document's sub-documents, kept in a folder named by
// its id, under it), each document's block tree — the .sy JSON SiYuan's Lute
// engine writes — as blocks: headings, paragraphs, lists and task lists,
// quotes, code, math, tables, rules, pictures and files from its assets, a
// super block's columns one under another, databases (siyuan-database.js);
// references to documents as page mentions, to blocks as the text they show,
// and what has no place counted.
// Ids come from SiYuan's own block ids, so the next ZIP carries the same
// migration on.

import { codeLanguage, createBundle, isPicture } from "./bundle.js";
import { readAttributeView, writeAttributeView } from "./siyuan-database.js";
import { readZip } from "./zip.js";

const DOC_ID = /^\d{14}-[a-z0-9]{7}$/u;
const MARKS = { strong: "bold", em: "italic", u: "underline", s: "strikethrough", code: "code", kbd: "code" };
const decodeBase64 = (text) => {
  try { return new TextDecoder().decode(Uint8Array.from(atob(text ?? ""), (char) => char.charCodeAt(0))); } catch { return ""; }
};
const basename = (path) => path.split("/").pop() ?? path;
const dirname = (path) => path.includes("/") ? path.slice(0, path.lastIndexOf("/")) : "";

function stableId(text) {
  let hash = 0x811c9dc5;
  for (const char of text) {
    hash ^= char.codePointAt(0);
    hash = Math.imul(hash, 0x01000193) >>> 0;
  }
  return hash.toString(16).padStart(8, "0");
}
const key15 = (text) => `${stableId(text)}${stableId(`${text}\u0000`)}`.slice(0, 15);

/** The notebooks in a ZIP: folders of .sy documents, each with its name and its documents' tree. */
async function notebooksIn(zip) {
  const documents = zip.names.filter((name) => /\.sy$/u.test(name) && !basename(name).startsWith("._"));
  // A notebook is the folder holding .siyuan/conf.json; an export without it, the folder of its top documents.
  let roots = zip.names.filter((name) => /(^|\/)\.siyuan\/conf\.json$/u.test(name)).map((name) => dirname(dirname(name)));
  if (roots.length === 0 && documents.length) {
    const depth = Math.min(...documents.map((name) => name.split("/").length));
    roots = [...new Set(documents.filter((name) => name.split("/").length === depth).map(dirname))];
  }
  const notebooks = new Map();
  for (const path of documents) {
    const folder = roots.filter((root) => root === "" || path.startsWith(`${root}/`)).sort((left, right) => right.length - left.length)[0];
    if (folder === undefined) continue;
    const chain = (folder ? path.slice(folder.length + 1) : path).replace(/\.sy$/u, "").split("/");
    if (!chain.every((part) => DOC_ID.test(part))) continue;
    const notebook = notebooks.get(folder) ?? { folder, documents: [] };
    notebook.documents.push({ path, chain });
    notebooks.set(folder, notebook);
  }
  for (const notebook of notebooks.values()) {
    const conf = `${notebook.folder ? `${notebook.folder}/` : ""}.siyuan/conf.json`;
    let name = "";
    if (zip.has(conf)) {
      try { name = JSON.parse(await zip.text(conf)).name ?? ""; } catch { name = ""; }
    }
    notebook.name = name || basename(notebook.folder) || "SiYuan";
    notebook.id = DOC_ID.test(basename(notebook.folder)) ? basename(notebook.folder) : key15(`notebook:${notebook.folder}`);
    const sort = `${notebook.folder ? `${notebook.folder}/` : ""}.siyuan/sort.json`;
    notebook.sort = {};
    if (zip.has(sort)) {
      try { notebook.sort = JSON.parse(await zip.text(sort)); } catch { notebook.sort = {}; }
    }
  }
  return [...notebooks.values()];
}

/** Turns a ZIP of SiYuan notebooks into a bundle; `progress(done, total)` follows the documents. */
export async function siyuanBundle(file, { labels, progress = () => undefined }) {
  const zip = await readZip(file);
  const notebooks = await notebooksIn(zip);
  if (notebooks.length === 0) throw new Error(labels.noSiyuan);
  const losses = new Map();
  const lose = (what, why, count = 1) => {
    const loss = losses.get(what) ?? { what, why, count: 0 };
    loss.count += count;
    losses.set(what, loss);
  };
  // Pictures and files, by the path a document gives ("assets/…").
  const assets = new Map(zip.names.filter((name) => /(^|\/)assets\/[^/]+$/u.test(name)).map((name) => [basename(name), name]));
  // Every document and block, for references to them.
  const trees = new Map();
  const blocks = new Map();
  const documentOf = new Map();
  const walkIds = (node, documentId) => {
    if (node.ID) {
      blocks.set(node.ID, node);
      documentOf.set(node.ID, documentId);
    }
    for (const child of node.Children ?? []) walkIds(child, documentId);
  };
  for (const notebook of notebooks) {
    for (const document of notebook.documents) {
      const tree = JSON.parse(await zip.text(document.path));
      document.id = tree.ID ?? document.chain.at(-1);
      document.title = tree.Properties?.title || labels.untitled;
      trees.set(document.id, tree);
      walkIds(tree, document.id);
    }
  }
  const sourceId = `siyuan:${notebooks.map((notebook) => notebook.id).sort().join(",")}`;
  const bundle = createBundle({ sourceId, sourceName: "SiYuan" });
  const single = notebooks.length === 1;
  const top = single ? `nb-${notebooks[0].id}` : "siyuan";
  if (!single) bundle.page({ id: "siyuan", title: "SiYuan" });
  let done = 0;
  const total = notebooks.reduce((sum, notebook) => sum + notebook.documents.length, 0);
  // Databases written so far, by their attribute view: a view's next block mentions it.
  const written = new Map();
  const docOf = (blockId) => documentOf.get(blockId) ?? "";
  for (const notebook of notebooks) {
    bundle.page({ id: `nb-${notebook.id}`, parentId: single ? "" : "siyuan", title: notebook.name });
    // Parents before their children, in SiYuan's order.
    const ordered = [...notebook.documents].sort((left, right) => left.chain.length - right.chain.length
      || (notebook.sort[left.id] ?? 0) - (notebook.sort[right.id] ?? 0) || left.title.localeCompare(right.title));
    for (const document of ordered) {
      const parent = document.chain.length > 1 ? `doc-${document.chain.at(-2)}` : `nb-${notebook.id}`;
      bundle.page({ id: `doc-${document.id}`, parentId: parent, title: document.title });
      await new DocumentConverter({ bundle, pageId: `doc-${document.id}`, trees, blocks, assets, zip, labels, lose, written, docOf })
        .convert(trees.get(document.id));
      done += 1;
      progress(done, total);
    }
  }
  const blob = await bundle.build({ rootIds: [top], losses: [...losses.values()] });
  return { blob, sourceId, notebooks: notebooks.length, counts: bundle.counts(), losses: [...losses.values()] };
}

class DocumentConverter {
  constructor(options) {
    Object.assign(this, options);
  }

  block(parentId, node, type, content, text) {
    // SiYuan's block ids stay the same as the document changes.
    const id = `${this.pageId}-${node.ID ?? `${type}-${this.bundleCount = (this.bundleCount ?? 0) + 1}`}`;
    return this.bundle.block({ id, pageId: this.pageId, parentId, type, content, text });
  }

  async convert(tree) {
    for (const child of tree.Children ?? []) await this.node(child, null);
  }

  async node(node, parentId) {
    switch (node.Type) {
      case "NodeParagraph": return this.paragraph(node, parentId, "paragraph");
      case "NodeHeading": return void this.block(parentId, node, `heading_${Math.min(node.HeadingLevel ?? 1, 4)}`, {}, this.runs(node));
      case "NodeList": {
        for (const item of node.Children ?? []) if (item.Type === "NodeListItem") await this.listItem(item, node.ListData?.Typ ?? 0, parentId);
        return undefined;
      }
      case "NodeBlockquote": {
        const [first, ...rest] = (node.Children ?? []).filter((child) => child.Type !== "NodeBlockquoteMarker");
        const id = this.block(parentId, node, "quote", {}, first?.Type === "NodeParagraph" ? this.runs(first) : []);
        for (const child of first?.Type === "NodeParagraph" ? rest : [first, ...rest].filter(Boolean)) await this.node(child, id);
        return undefined;
      }
      case "NodeCodeBlock": {
        const info = (node.Children ?? []).find((child) => child.Type === "NodeCodeBlockFenceInfoMarker");
        const code = (node.Children ?? []).find((child) => child.Type === "NodeCodeBlockCode")?.Data ?? "";
        const language = decodeBase64(info?.CodeBlockInfo).trim().split(/\s/u)[0];
        const text = code.replace(/\n$/u, "");
        return void this.block(parentId, node, "code", { language: codeLanguage(language) }, text ? [{ insert: text }] : []);
      }
      case "NodeMathBlock": {
        const expression = (node.Children ?? []).find((child) => child.Type === "NodeMathBlockContent")?.Data ?? "";
        return void this.block(parentId, node, "equation", { expression: expression.trim() });
      }
      case "NodeThematicBreak": return void this.block(parentId, node, "divider", {});
      case "NodeTable": return this.table(node, parentId);
      case "NodeSuperBlock": {
        if ((node.Children ?? []).some((child) => child.Type === "NodeSuperBlockLayoutMarker" && child.Data === "col")) {
          this.lose(this.labels.lossColumns, this.labels.lossColumnsWhy);
        }
        for (const child of node.Children ?? []) await this.node(child, parentId);
        return undefined;
      }
      case "NodeAttributeView": {
        const av = await readAttributeView(this.zip, node.AttributeViewID ?? "");
        if (!av) return this.lose(this.labels.lossAttributeView, this.labels.lossAttributeViewWhy);
        return void writeAttributeView({ bundle: this.bundle, pageId: this.pageId, parentId, node, av, written: this.written,
          docOf: this.docOf, labels: this.labels, lose: this.lose });
      }
      case "NodeBlockQueryEmbed": {
        const script = (node.Children ?? []).find((child) => child.Type === "NodeBlockQueryEmbedScript")?.Data ?? "";
        this.lose(this.labels.lossEmbedQuery, this.labels.lossEmbedQueryWhy);
        return void this.block(parentId, node, "code", { language: "sql" }, script ? [{ insert: script }] : []);
      }
      case "NodeIFrame": case "NodeVideo": case "NodeAudio": case "NodeWidget": case "NodeHTMLBlock": {
        const source = /src="([^"]+)"/u.exec(node.Data ?? "")?.[1] ?? "";
        if (/^https?:/iu.test(source)) this.block(parentId, node, "bookmark", { url: source });
        else if (source && this.assets.has(basename(source))) await this.fileBlock(parentId, node, source);
        return this.lose(this.labels.lossHtml, this.labels.lossHtmlWhy);
      }
      default:
        if (/Marker$/u.test(node.Type ?? "")) return undefined;
        if (node.Children?.length) for (const child of node.Children) await this.node(child, parentId);
        return undefined;
    }
  }

  async paragraph(node, parentId, type) {
    const text = this.runs(node);
    const id = text.length ? this.block(parentId, node, type, {}, text) : null;
    for (const image of this.images(node)) await this.fileBlock(id && type !== "paragraph" ? id : parentId, image.node, image.path);
    return id;
  }

  async listItem(item, kind, parentId) {
    const children = (item.Children ?? []).filter((child) => child.Type !== "NodeTaskListItemMarker");
    const marker = (item.Children ?? []).find((child) => child.Type === "NodeTaskListItemMarker");
    const [first, ...rest] = children;
    const runs = first?.Type === "NodeParagraph" ? this.runs(first) : [];
    const type = kind === 3 || marker ? "to_do" : kind === 1 ? "numbered_list_item" : "bulleted_list_item";
    const id = this.block(parentId, item, type, type === "to_do" ? { checked: Boolean(marker?.TaskListItemChecked) } : {}, runs);
    if (first?.Type === "NodeParagraph") for (const image of this.images(first)) await this.fileBlock(id, image.node, image.path);
    for (const child of first?.Type === "NodeParagraph" ? rest : children) await this.node(child, id);
  }

  table(node, parentId) {
    const rows = [];
    const collect = (children) => {
      for (const child of children ?? []) {
        if (child.Type === "NodeTableRow") rows.push(child);
        else if (child.Type === "NodeTableHead") collect(child.Children);
      }
    };
    collect(node.Children);
    if (rows.length === 0) return;
    const width = Math.max(...rows.map((row) => (row.Children ?? []).length));
    const head = (node.Children ?? []).some((child) => child.Type === "NodeTableHead");
    const id = this.block(parentId, node, "table", { tableWidth: width, hasColumnHeader: head, hasRowHeader: false });
    rows.forEach((row, index) => {
      const cells = (row.Children ?? []).map((cell) => this.runs(cell).map((run) => {
        const { mention, pageId, ...marks } = run.attributes ?? {};
        return { text: run.insert, ...marks };
      }));
      while (cells.length < width) cells.push([]);
      this.bundle.block({ id: `${id}-row-${row.ID ?? index}`, pageId: this.pageId, parentId: id, type: "table_row", content: { cells } });
    });
  }

  /** The pictures a paragraph shows, by their asset paths. */
  images(node) {
    const found = [];
    const walk = (child) => {
      if (child.Type === "NodeImage") {
        const path = (child.Children ?? []).find((part) => part.Type === "NodeLinkDest")?.Data ?? "";
        found.push({ node: child, path });
        return;
      }
      for (const part of child.Children ?? []) walk(part);
    };
    for (const child of node.Children ?? []) walk(child);
    return found;
  }

  async fileBlock(parentId, node, path) {
    if (/^https?:/iu.test(path)) {
      this.block(parentId, node, "bookmark", { url: path });
      return;
    }
    const asset = this.assets.get(basename(decodeURIComponent(path)));
    if (!asset) {
      this.lose(this.labels.lossMissingMedia, this.labels.lossMissingMediaWhy);
      return;
    }
    const name = basename(asset).replace(/-\d{14}-[a-z0-9]{7}(?=\.[^.]+$)/u, "");
    const content = await this.bundle.file({ id: key15(`asset:${basename(asset)}`), name, data: new Blob([await this.zip.bytes(asset)]) });
    if (!content) {
      this.lose(this.labels.lossKept, this.labels.lossKeptWhy);
      return;
    }
    this.block(parentId, node, isPicture(name) ? "image" : "file", content);
  }

  /** A node's inline children as runs. */
  runs(node) {
    const runs = [];
    const add = (insert, attributes) => {
      if (!insert) return;
      const last = runs.at(-1);
      if (last && JSON.stringify(last.attributes ?? {}) === JSON.stringify(attributes ?? {})) last.insert += insert;
      else runs.push(attributes && Object.keys(attributes).length ? { insert, attributes } : { insert });
    };
    const walk = (child) => {
      switch (child.Type) {
        case "NodeText": add(child.Data ?? "", {}); return;
        case "NodeBr": case "NodeHardBreak": case "NodeSoftBreak": add("\n", {}); return;
        case "NodeImage": case "NodeKramdownSpanIAL": return;
        case "NodeTextMark": {
          const types = (child.TextMarkType ?? "").split(/\s+/u);
          const marks = {};
          for (const type of types) if (MARKS[type]) marks[MARKS[type]] = true;
          const text = child.TextMarkTextContent ?? "";
          if (types.includes("mark")) marks.color = "yellow_background";
          if (types.includes("a") && /^(https?|mailto):/iu.test(child.TextMarkAHref ?? "")) marks.link = child.TextMarkAHref;
          if (types.includes("inline-math")) {
            const expression = child.TextMarkInlineMathContent ?? "";
            add(expression, { ...marks, equation: expression });
            return;
          }
          if (types.includes("block-ref")) {
            const target = child.TextMarkBlockRefID ?? "";
            if (this.trees.has(target)) {
              add(text || this.trees.get(target).Properties?.title || "", { ...marks, mention: "page", pageId: `doc-${target}` });
              return;
            }
            this.lose(this.labels.lossBlockRef, this.labels.lossBlockRefWhy);
          }
          if (types.includes("tag")) {
            this.lose(this.labels.lossSiyuanTag, this.labels.lossSiyuanTagWhy);
            add(`#${text}#`, marks);
            return;
          }
          if (types.some((type) => ["sup", "sub", "file-annotation-ref"].includes(type))) this.lose(this.labels.lossTextStyle, this.labels.lossTextStyleWhy);
          add(text, marks);
          return;
        }
        default:
          for (const part of child.Children ?? []) walk(part);
      }
    };
    for (const child of node.Children ?? []) walk(child);
    if (runs.length) {
      runs[0].insert = runs[0].insert.replace(/^\s+/u, "");
      runs[runs.length - 1].insert = runs[runs.length - 1].insert.replace(/\s+$/u, "");
    }
    return runs.filter((run) => run.insert.length > 0);
  }
}
