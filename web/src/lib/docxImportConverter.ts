import { importFileLine, resolveArchivePath, type ImportAssetUploader, type ImportLeftOut, type ImportReference } from "./importReferences";
import { listZipEntries, ZipReadError } from "./zipArchiveReader";
import { escapeImportMarkdown } from "./importMarkdownText";
import { blocksToMarkdown, HEADER_ROW, wrapInline, type Block } from "./importMarkdownBlocks";
import { linkText } from "./htmlImportText";

/**
 * Turns a Word document into the block Markdown the workspace importer
 * reads (importMarkdownBlocks.ts): its title and headings (a fifth level and
 * below the fourth), paragraphs, bulleted and numbered lists by its
 * numbering, quotes, tables, and embedded pictures after their paragraph;
 * bold, italic, strikethrough and web links as inline Markdown.
 */

function children(node: Element, localName: string) {
  return Array.from(node.children).filter((child) => child.localName === localName);
}

function child(node: Element | undefined, localName: string) {
  return node ? children(node, localName)[0] : undefined;
}

function attribute(node: Element | undefined, name: string) {
  return node?.getAttribute(`w:${name}`) ?? node?.getAttribute(name) ?? "";
}

type DocxContext = {
  bulletLists: Set<string>;
  links: Map<string, string>;
  images: Map<string, ImportReference>;
  trailing: string[];
};

function docxToggle(properties: Element | undefined, name: string) {
  const flag = child(properties, name);
  if (!flag) return false;
  return !/^(false|0|off|none)$/i.test(attribute(flag, "val"));
}

function docxRunText(node: Element, context: DocxContext): string {
  switch (node.localName) {
    case "t":
      return escapeImportMarkdown(node.textContent ?? "");
    case "r": {
      const properties = child(node, "rPr");
      let text = Array.from(node.children).filter((part) => part.localName !== "rPr")
        .map((part) => docxRunText(part, context)).join("");
      if (docxToggle(properties, "strike") || docxToggle(properties, "dstrike")) text = wrapInline(text, "~~");
      if (docxToggle(properties, "i")) text = wrapInline(text, "*");
      if (docxToggle(properties, "b")) text = wrapInline(text, "**");
      return text;
    }
    case "tab":
      return "\t";
    case "br":
    case "cr":
      return "\n";
    case "hyperlink": {
      const inner = Array.from(node.children).map((part) => docxRunText(part, context)).join("");
      return linkText(inner, context.links.get(node.getAttribute("r:id") ?? "") ?? null);
    }
    case "drawing":
    case "pict": {
      // An embedded picture follows its paragraph as an uploaded image.
      for (const blip of Array.from(node.getElementsByTagName("*")).filter((part) => part.localName === "blip")) {
        const image = context.images.get(blip.getAttribute("r:embed") ?? "");
        if (image && image.kind !== "page") context.trailing.push(importFileLine(image, ""));
      }
      return "";
    }
    case "delText":
    case "instrText":
      return "";
    default:
      return Array.from(node.children).map((part) => docxRunText(part, context)).join("");
  }
}

function docxParagraph(paragraph: Element, context: DocxContext): Block | null {
  const properties = child(paragraph, "pPr");
  const style = attribute(child(properties, "pStyle"), "val").toLowerCase();
  const text = Array.from(paragraph.children)
    .filter((part) => part.localName !== "pPr")
    .map((part) => docxRunText(part, context)).join("")
    .split("\n").map((line) => line.replace(/[ \t ]+/g, " ").trim()).filter(Boolean).join("\n");
  const numbering = child(properties, "numPr");
  if (numbering) {
    const level = Number(attribute(child(numbering, "ilvl"), "val") || 0);
    const numberId = attribute(child(numbering, "numId"), "val");
    const bullet = context.bulletLists.has(`${numberId}:${level}`);
    return text ? { prefix: bullet ? "- " : "1. ", text: text.replace(/\n/g, " "), indent: Math.min(level, 8) } : null;
  }
  const heading = /^(title|heading\s?(\d))$/.exec(style);
  if (heading && text) {
    const level = heading[1] === "title" ? 1 : Math.min(Number(heading[2]) || 1, 4);
    return { prefix: `${"#".repeat(level)} `, text: text.replace(/\n/g, " "), indent: 0 };
  }
  if ((style === "quote" || style === "intensequote") && text) {
    return { prefix: "> ", text: text.replace(/\n/g, " "), indent: 0 };
  }
  return text ? { prefix: "", text, indent: 0 } : null;
}

function docxBody(body: Element, context: DocxContext, blocks: Block[]) {
  for (const part of Array.from(body.children)) {
    if (part.localName === "p") {
      const block = docxParagraph(part, context);
      if (block) blocks.push(block);
    } else if (part.localName === "tbl" && children(part, "tr").every((row) => children(row, "tc").length <= 1)) {
      docxColumnTable(children(part, "tr"), context, blocks);
    } else if (part.localName === "tbl") {
      const rows = children(part, "tr").map((row) => children(row, "tc").map((cell) => children(cell, "p")
        .map((paragraph) => docxParagraph(paragraph, context))
        .map((block) => (block && "text" in block ? block.text.replace(/\n/g, " ") : ""))
        .filter(Boolean).join(" "))).filter((cells) => cells.some(Boolean));
      if (rows.length) blocks.push({ rows, headers: HEADER_ROW, indent: 0 });
    } else if (part.localName === "sdt") {
      const content = child(part, "sdtContent");
      if (content) docxBody(content, context, blocks);
    }
    for (const line of context.trailing.splice(0)) blocks.push({ prefix: line, text: "", indent: 0 });
  }
}

/**
 * A table of one column, as Notion's Word import reads one (seen
 * 2026-11-13, as its HTML import does, importDocumentConverters.ts): each
 * row is its cell's paragraphs and a divider after them; the runs of a
 * paragraph in one formatting, and each link, are a paragraph of their own,
 * a line break between them none.
 */
function docxColumnTable(rows: Element[], context: DocxContext, blocks: Block[]) {
  const key = (part: Element) => {
    if (part.localName === "hyperlink") return part;
    const properties = child(part, "rPr");
    return part.localName === "r" ? ["b", "i", "strike", "dstrike"].map((name) => (docxToggle(properties, name) ? name : "")).join() : "";
  };
  for (const row of rows) {
    for (const paragraph of children(children(row, "tc")[0] ?? row, "p")) {
      const parts: Element[][] = [];
      for (const part of Array.from(paragraph.children).filter((node) => node.localName !== "pPr")) {
        const last = parts[parts.length - 1];
        if (last && key(last[0]!) === key(part)) last.push(part);
        else parts.push([part]);
      }
      for (const part of parts) {
        const lines = part.map((run) => docxRunText(run, context)).join("").split("\n");
        for (const line of lines.map((words) => words.replace(/[ \t ]+/g, " ").trim()).filter(Boolean)) blocks.push({ prefix: "", text: line, indent: 0 });
        for (const line of context.trailing.splice(0)) blocks.push({ prefix: line, text: "", indent: 0 });
      }
    }
    blocks.push({ prefix: "---", text: "", indent: 0 });
  }
}

function parseXml(bytes: Uint8Array | undefined) {
  if (!bytes) return null;
  const parsed = new DOMParser().parseFromString(new TextDecoder("utf-8").decode(bytes), "application/xml");
  return parsed.getElementsByTagName("parsererror").length > 0 ? null : parsed;
}

/**
 * Converts a Word .docx file; throws when it is not a Word document. Its
 * embedded pictures are uploaded through `uploadAsset` when one is given.
 */
export async function docxToImportMarkdown(file: Blob, uploadAsset?: ImportAssetUploader, leftOut?: ImportLeftOut) {
  let entries;
  try {
    entries = await listZipEntries(file);
  } catch (error) {
    if (error instanceof ZipReadError) throw new Error("The file is not a Word document.");
    throw error;
  }
  const read = async (name: string) => entries.find((entry) => entry.name === name)?.read();
  const document = parseXml(await read("word/document.xml"));
  const body = document ? Array.from(document.getElementsByTagName("*")).find((node) => node.localName === "body") : undefined;
  if (!document || !body) throw new Error("The file is not a Word document.");
  const context: DocxContext = { bulletLists: new Set(), links: new Map(), images: new Map(), trailing: [] };
  const numbering = parseXml(await read("word/numbering.xml"));
  if (numbering) {
    const bulletLevels = new Map<string, Set<number>>();
    for (const abstract of Array.from(numbering.getElementsByTagName("*")).filter((node) => node.localName === "abstractNum")) {
      const levels = new Set<number>();
      for (const level of children(abstract, "lvl")) {
        if (attribute(child(level, "numFmt"), "val") === "bullet") levels.add(Number(attribute(level, "ilvl") || 0));
      }
      bulletLevels.set(attribute(abstract, "abstractNumId"), levels);
    }
    for (const number of Array.from(numbering.getElementsByTagName("*")).filter((node) => node.localName === "num")) {
      const levels = bulletLevels.get(attribute(child(number, "abstractNumId"), "val")) ?? new Set();
      for (const level of levels) context.bulletLists.add(`${attribute(number, "numId")}:${level}`);
    }
  }
  const relationships = parseXml(await read("word/_rels/document.xml.rels"));
  for (const relationship of Array.from(relationships?.getElementsByTagName("*") ?? [])) {
    if (relationship.localName !== "Relationship") continue;
    const id = relationship.getAttribute("Id") ?? "";
    const target = relationship.getAttribute("Target") ?? "";
    if (relationship.getAttribute("TargetMode") === "External") {
      context.links.set(id, target);
    } else if (uploadAsset && /\/image$/.test(relationship.getAttribute("Type") ?? "")) {
      // Every picture, one after another; one that can't be read or
      // uploaded is named in the import's report.
      const path = resolveArchivePath("word", target);
      const entry = path ? entries.find((candidate) => candidate.name === path) : undefined;
      if (!entry) continue;
      const name = entry.name.split("/").pop() ?? "image";
      const reference = await entry.blob().then((picture) => uploadAsset(name, picture), () => null);
      if (reference) context.images.set(id, reference);
      else leftOut?.add(name);
    }
  }
  const blocks: Block[] = [];
  docxBody(body, context, blocks);
  return blocksToMarkdown(blocks);
}
