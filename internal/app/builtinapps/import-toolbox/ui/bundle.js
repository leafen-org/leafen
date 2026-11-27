// A Leafen workspace bundle (docs/bundle.md): pages and blocks with their
// text as runs, files with their bytes, the descriptor and what the tool
// couldn't carry, in a ZIP of stored entries.

import { storedZip } from "./zip.js";

const K = new Uint32Array([
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5, 0xd807aa98, 0x12835b01,
  0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174, 0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc,
  0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da, 0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147,
  0x06ca6351, 0x14292967, 0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
  0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070, 0x19a4c116, 0x1e376c08,
  0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3, 0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208,
  0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
]);

/** SHA-256 over bytes that come in pieces. */
class Sha256 {
  h = new Uint32Array([0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19]);
  block = new Uint8Array(64);
  filled = 0;
  length = 0;
  w = new Uint32Array(64);

  update(bytes) {
    this.length += bytes.length;
    let at = 0;
    while (at < bytes.length) {
      const take = Math.min(64 - this.filled, bytes.length - at);
      this.block.set(bytes.subarray(at, at + take), this.filled);
      this.filled += take;
      at += take;
      if (this.filled === 64) { this.compress(); this.filled = 0; }
    }
  }

  compress() {
    const { w, h, block } = this;
    for (let i = 0; i < 16; i += 1) w[i] = (block[i * 4] << 24) | (block[i * 4 + 1] << 16) | (block[i * 4 + 2] << 8) | block[i * 4 + 3];
    for (let i = 16; i < 64; i += 1) {
      const s0 = ((w[i - 15] >>> 7) | (w[i - 15] << 25)) ^ ((w[i - 15] >>> 18) | (w[i - 15] << 14)) ^ (w[i - 15] >>> 3);
      const s1 = ((w[i - 2] >>> 17) | (w[i - 2] << 15)) ^ ((w[i - 2] >>> 19) | (w[i - 2] << 13)) ^ (w[i - 2] >>> 10);
      w[i] = (w[i - 16] + s0 + w[i - 7] + s1) >>> 0;
    }
    let [a, b, c, d, e, f, g, k] = h;
    for (let i = 0; i < 64; i += 1) {
      const S1 = ((e >>> 6) | (e << 26)) ^ ((e >>> 11) | (e << 21)) ^ ((e >>> 25) | (e << 7));
      const t1 = (k + S1 + ((e & f) ^ (~e & g)) + K[i] + w[i]) >>> 0;
      const S0 = ((a >>> 2) | (a << 30)) ^ ((a >>> 13) | (a << 19)) ^ ((a >>> 22) | (a << 10));
      const t2 = (S0 + ((a & b) ^ (a & c) ^ (b & c))) >>> 0;
      k = g; g = f; f = e; e = (d + t1) >>> 0; d = c; c = b; b = a; a = (t1 + t2) >>> 0;
    }
    h[0] += a; h[1] += b; h[2] += c; h[3] += d; h[4] += e; h[5] += f; h[6] += g; h[7] += k;
  }

  hex() {
    const bits = this.length * 8;
    this.update(new Uint8Array([0x80]));
    while (this.filled !== 56) this.update(new Uint8Array([0]));
    const tail = new Uint8Array(8);
    const high = Math.floor(bits / 2 ** 32);
    tail.set([high >>> 24, high >>> 16, high >>> 8, high, bits >>> 24, bits >>> 16, bits >>> 8, bits].map((value) => value & 0xff));
    this.update(tail);
    return [...this.h].map((word) => word.toString(16).padStart(8, "0")).join("");
  }
}

export async function sha256(blob) {
  const hash = new Sha256();
  const reader = blob.stream().getReader();
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    hash.update(value);
  }
  return hash.hex();
}

// What a file's name says it is, for the files people keep in other tools.
const FILE_TYPES = {
  png: "image/png", jpg: "image/jpeg", jpeg: "image/jpeg", gif: "image/gif", webp: "image/webp", bmp: "image/bmp", heic: "image/heic",
  tif: "image/tiff", tiff: "image/tiff", pdf: "application/pdf", txt: "text/plain", md: "text/markdown", csv: "text/csv",
  json: "application/json", canvas: "application/json", zip: "application/zip", doc: "application/msword",
  docx: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", xls: "application/vnd.ms-excel",
  xlsx: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", ppt: "application/vnd.ms-powerpoint",
  pptx: "application/vnd.openxmlformats-officedocument.presentationml.presentation", hwp: "application/x-hwp", hwpx: "application/hwp+zip",
  mp4: "video/mp4", mov: "video/quicktime", webm: "video/webm", mp3: "audio/mpeg", wav: "audio/wav", m4a: "audio/mp4", ogg: "audio/ogg",
};
const IMAGE_TYPES = new Set(["image/png", "image/jpeg", "image/gif", "image/webp", "image/bmp"]);
// Types and names Leafen doesn't keep, as a browser could run them (its file
// policy); a bundle carrying one is refused, so a tool leaves such files out.
const ACTIVE_TYPES = new Set(["application/ecmascript", "application/javascript", "application/xhtml+xml", "application/xml", "image/svg+xml",
  "text/css", "text/ecmascript", "text/html", "text/javascript", "text/xml"]);
const ACTIVE_EXTENSIONS = new Set(["cjs", "css", "hta", "htm", "html", "js", "mht", "mhtml", "mjs", "shtml", "svg", "svgz", "xht", "xhtml",
  "xml", "xsl", "xslt"]);
const extensionOf = (name) => /\.([^./]+)$/u.exec(name)?.[1]?.toLowerCase() ?? "";

/** The type of a file by its name, as Leafen keeps it. */
export function fileType(name) {
  return FILE_TYPES[extensionOf(name)] ?? "application/octet-stream";
}

/** Whether a file shows as a picture in Leafen. */
export function isPicture(name, contentType = fileType(name)) {
  return IMAGE_TYPES.has(contentType);
}

// A code block's language is one of the names Leafen's code block offers
// (Notion's); the migration leaves out a code block with any other.
const CODE_LANGUAGES = new Set(["abap", "arduino", "bash", "basic", "c", "clojure", "coffeescript", "c++", "c#", "css", "dart", "diff",
  "docker", "elixir", "elm", "erlang", "flow", "fortran", "f#", "gherkin", "glsl", "go", "graphql", "groovy", "haskell", "html", "java",
  "javascript", "json", "julia", "kotlin", "latex", "less", "lisp", "livescript", "lua", "makefile", "markdown", "markup", "matlab",
  "mermaid", "nix", "objective-c", "ocaml", "pascal", "perl", "php", "plain text", "powershell", "prolog", "protobuf", "python", "r",
  "reason", "ruby", "rust", "sass", "scala", "scheme", "scss", "shell", "sql", "swift", "typescript", "vb.net", "verilog", "vhdl",
  "visual basic", "webassembly", "xml", "yaml", "java/c/c++/c#"]);
const CODE_ALIASES = { js: "javascript", jsx: "javascript", mjs: "javascript", cjs: "javascript", ts: "typescript", tsx: "typescript",
  py: "python", rb: "ruby", sh: "shell", zsh: "shell", console: "shell", ps1: "powershell", yml: "yaml", md: "markdown", cpp: "c++",
  cxx: "c++", hpp: "c++", h: "c", cs: "c#", csharp: "c#", fs: "f#", fsharp: "f#", kt: "kotlin", rs: "rust", golang: "go",
  objc: "objective-c", objectivec: "objective-c", tex: "latex", vb: "visual basic", dockerfile: "docker", make: "makefile",
  proto: "protobuf", htm: "html", svg: "xml", wasm: "webassembly", hs: "haskell", ex: "elixir", exs: "elixir", erl: "erlang",
  clj: "clojure", pl: "perl", ml: "ocaml", jl: "julia", gql: "graphql", patch: "diff", text: "plain text", txt: "plain text",
  plain: "plain text", plaintext: "plain text" };

/** The Leafen name of a code language another tool names; plain text when Leafen has none. */
export function codeLanguage(name) {
  const key = String(name ?? "").trim().toLowerCase();
  if (CODE_LANGUAGES.has(key)) return key;
  return CODE_ALIASES[key] ?? "plain text";
}

const COUNT_KEYS = ["blockTrashTombstones", "blocks", "comments", "customEmojis", "dataSources", "databaseContainers", "databaseRows",
  "databases", "dbProperties", "dbTemplates", "dbViews", "pages", "rowBlockTrashTombstones", "rowPageBlocks", "teamspaces", "ygoDocuments"];

/** A bundle being written: pages, blocks, databases and files, then `build`. */
export function createBundle({ sourceId, sourceName }) {
  const lines = [];
  const files = new Map();
  const pagePositions = new Map();
  const blockPositions = new Map();
  const rowPositions = new Map();
  const counted = Object.fromEntries(COUNT_KEYS.map((key) => [key, 0]));
  const next = (map, key) => {
    const position = map.get(key) ?? 0;
    map.set(key, position + 1);
    return position;
  };
  const place = (map, key) => `p${String(next(map, key)).padStart(10, "0")}`;
  const add = (kind, value, count) => {
    lines.push({ kind, value });
    counted[count] += 1;
  };
  return {
    page({ id, parentId = "", title }) {
      add("page", { id, parentId, title, kind: "page", position: next(pagePositions, parentId) + 1 }, "pages");
    },
    /** A block of a page, after the blocks already there under the same parent. */
    block({ id, pageId, parentId = null, type, content = {}, text }) {
      const value = { id, pageId, parentId, position: place(blockPositions, `${pageId}\u0000${parentId ?? ""}`), type, content };
      if (text && text.length) value.text = text;
      add("block", value, "blocks");
      return id;
    },
    /**
     * A full-page database (docs/bundle.md "Databases"): its page, the block
     * that shows it there, its table with these columns (one `title`) and a
     * table view. `properties` are `{ id, name, type, config }`.
     */
    database({ id, parentId = "", title, dataSourceId, viewId, viewName = "", properties }) {
      add("page", { id, parentId, title, kind: "database", position: next(pagePositions, parentId) + 1 }, "pages");
      counted.databases += 1;
      const blockId = `${id}-table`;
      add("block", { id: blockId, pageId: id, parentId: null, position: place(blockPositions, `${id}\u0000`), type: "inline_database",
        content: { dataSourceId, viewId } }, "blocks");
      add("dataSource", { id: dataSourceId, pageId: id, title, icon: "", iconType: "none" }, "dataSources");
      for (const [index, property] of properties.entries()) {
        add("property", { id: property.id, dataSourceId, pageId: id, name: property.name, type: property.type, config: property.config ?? {},
          position: `p${String(index).padStart(10, "0")}`, deletedAt: null }, "dbProperties");
      }
      add("view", { id: viewId, dataSourceId, pageId: id, name: viewName, type: "table", config: {}, containerId: null, position: "a00000000" },
        "dbViews");
      add("databaseContainer", { id: blockId, blockId, kind: "source", pageId: id, parentBlockId: null, position: "p0000000000",
        sourceContainerId: null, dataSourceIds: [dataSourceId], viewIds: [viewId] }, "databaseContainers");
    },
    /**
     * A database inside a page where its block stands, as an inline
     * database: the block, its table with these columns (one `title`) and
     * its views (`{ id, name, type, config }`, the first the one the block
     * shows). Its rows go to `row` with the page as `databaseId`.
     */
    inlineDatabase({ id, pageId, parentId = null, title = "", dataSourceId, properties, views }) {
      const position = place(blockPositions, `${pageId}\u0000${parentId ?? ""}`);
      counted.databases += 1;
      add("block", { id, pageId, parentId, position, type: "inline_database", content: { dataSourceId, viewId: views[0].id } }, "blocks");
      add("dataSource", { id: dataSourceId, pageId, title, icon: "", iconType: "none" }, "dataSources");
      for (const [index, property] of properties.entries()) {
        add("property", { id: property.id, dataSourceId, pageId, name: property.name, type: property.type, config: property.config ?? {},
          position: `p${String(index).padStart(10, "0")}`, deletedAt: null }, "dbProperties");
      }
      for (const [index, view] of views.entries()) {
        add("view", { id: view.id, dataSourceId, pageId, name: view.name ?? "", type: view.type, config: view.config ?? {}, containerId: null,
          position: `a${String(index).padStart(8, "0")}` }, "dbViews");
      }
      add("databaseContainer", { id, blockId: id, kind: "source", pageId, parentBlockId: parentId, position, sourceContainerId: null,
        dataSourceIds: [dataSourceId], viewIds: views.map((view) => view.id) }, "databaseContainers");
      return id;
    },
    /** A row of a database's table: its values by property id, `{type, value}` as Leafen's database API takes them. */
    row({ id, databaseId, dataSourceId, values }) {
      add("row", { id, dataSourceId, pageId: databaseId, position: place(rowPositions, dataSourceId), values, icon: "", iconType: "none",
        cover: "", coverPosition: 0, font: "", fullWidth: false, smallText: false, inlineCommentsDisplay: "", tableOfContentsDisplay: "",
        archivedAt: null, trashedAt: null, trashRootId: null, subitemParentId: null }, "databaseRows");
      return id;
    },
    /**
     * A block of a row's page: the row, its database's page, "" at the top,
     * and when it was made and changed (milliseconds) — from the tool, so the
     * next bundle gives the same.
     */
    rowBlock({ id, rowId, databaseId, parentId = "", type, content = {}, text, createdAt = 1, updatedAt = createdAt }) {
      const value = { id, pageId: databaseId, rowId, parentId: parentId ?? "", position: place(blockPositions, `${rowId}\u0000${parentId ?? ""}`),
        type, content, createdAt, updatedAt: Math.max(updatedAt, createdAt) };
      if (text && text.length) value.text = text;
      add("rowPageBlock", value, "rowPageBlocks");
      return id;
    },
    /**
     * A comment on a page (docs/bundle.md "Comments"): `parentId` the comment
     * a reply answers ("" opens a discussion), `author` who wrote it in the
     * tool, `createdAt` an ISO time.
     */
    comment({ id, pageId, parentId = "", text, author = "", createdAt = "", resolved = false }) {
      add("comment", { id, pageId, targetKind: "page", targetId: pageId, blockId: "", parentId, body: { rich: [{ text }] }, resolved,
        createdAt, editedAt: "", author }, "comments");
      return id;
    },
    /**
     * A file the bundle carries, once per id (15 letters, digits, "_" or "-"):
     * its block content, or null for a file Leafen doesn't keep (count it).
     */
    async file({ id, name, contentType = fileType(name), data }) {
      if (!/^[A-Za-z0-9_-]{15}$/u.test(id)) throw new Error(`file id ${id} isn't 15 letters, digits, _ or -`);
      contentType = contentType.split(";")[0].trim().toLowerCase() || "application/octet-stream";
      if (ACTIVE_TYPES.has(contentType) || contentType.endsWith("+xml") || ACTIVE_EXTENSIONS.has(extensionOf(name)) || data.size === 0) return null;
      let file = files.get(id);
      if (!file) {
        file = { id, name, contentType, data, bytes: data.size, sha256: await sha256(data) };
        files.set(id, file);
      }
      return { fileId: `leafen-archive://files/${id}`, name: file.name, size: file.bytes, contentType: file.contentType, sha256: file.sha256 };
    },
    counts: () => ({ pages: counted.pages, blocks: counted.blocks, rows: counted.databaseRows, files: files.size }),
    async build({ rootIds, losses = [] }) {
      const encoder = new TextEncoder();
      const entities = new Blob([lines.map((line) => `${JSON.stringify(line)}\n`).join("")]);
      let offset = 0;
      const index = [];
      for (const file of files.values()) {
        index.push(`${JSON.stringify({ id: file.id, offset, name: file.name, contentType: file.contentType, scope: "uploads", bytes: file.bytes, sha256: file.sha256 })}\n`);
        offset += file.bytes;
      }
      const fileIndex = new Blob(index);
      const fileData = new Blob([...files.values()].map((file) => file.data));
      const stream = async (path, blob, records) => ({ path, records, bytes: blob.size, sha256: await sha256(blob) });
      const counts = { ...counted };
      const descriptor = {
        format: "leafen.archive", formatVersion: 10, generatedAt: new Date().toISOString(),
        scope: { kind: "workspace", rootIds }, source: { workspaceId: sourceId, workspaceName: sourceName }, counts,
        files: { included: true, count: files.size, bytes: offset, strippedReferences: 0 },
        streams: {
          entities: await stream("leafen/entities.ndjson", entities, lines.length),
          fileIndex: await stream("leafen/files.ndjson", fileIndex, files.size),
          fileData: await stream("leafen/files.bin", fileData, 0),
        },
        losses,
      };
      return storedZip([
        { name: "leafen/entities.ndjson", data: entities },
        { name: "leafen/files.ndjson", data: fileIndex },
        { name: "leafen/files.bin", data: fileData },
        { name: "leafen/archive.json", data: new Blob([encoder.encode(JSON.stringify(descriptor))]) },
      ]);
    },
  };
}
