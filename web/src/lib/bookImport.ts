import { ImportOutputBudget } from "./importOutputBudget";
import { htmlReferenceHrefs, htmlToImportMarkdown } from "./importDocumentConverters";
import { isRelativeImportHref, resolveArchivePath, type ImportAssetUploader, type ImportLeftOut, type ImportReference } from "./importReferences";
import { listZipEntries } from "./zipArchiveReader";
import { importParagraphText } from "./importMarkdownText";

/**
 * Reads the outline and book files Notion's ZIP import turns into pages:
 * an OPML outline becomes nested bulleted lists (an outline's note follows
 * its item), and an EPUB book becomes one page with its chapters in reading
 * order, each read by the HTML importer. A book's pictures are uploaded
 * through `uploadAsset` when one is given.
 */

export class BookReadError extends Error {}


function parseXml(text: string, type: DOMParserSupportedType = "application/xml") {
  const document = new DOMParser().parseFromString(text, type);
  if (type === "application/xml" && document.getElementsByTagName("parsererror").length > 0) {
    throw new BookReadError("The file is damaged.");
  }
  return document;
}

/** An outline's words on one line, read as written (importMarkdownText.ts). */
function escapeText(text: string) {
  return importParagraphText(text.replace(/\s+/g, " ").trim());
}

/** Converts an OPML outline into nested bulleted lists, all of it, however
 * deep — or FileImportTooLargeError once larger than Leafen imports. */
export function opmlToImportMarkdown(text: string) {
  const document = parseXml(text);
  const body = Array.from(document.getElementsByTagName("*")).find((node) => node.localName === "body");
  if (!body) throw new BookReadError("The file is not an OPML outline.");
  const lines: string[] = [];
  const budget = new ImportOutputBudget();
  const visit = (node: Element, depth: number) => {
    for (const outline of Array.from(node.children).filter((child) => child.localName === "outline")) {
      const title = escapeText(outline.getAttribute("text") ?? outline.getAttribute("title") ?? "");
      const indent = "  ".repeat(depth);
      lines.push(`${indent}- ${title}`);
      const note = escapeText(outline.getAttribute("_note") ?? "");
      if (note) lines.push(`${indent}  ${note}`);
      budget.add(indent.length * 2 + title.length + note.length + 6);
      visit(outline, depth + 1);
    }
  };
  visit(body, 0);
  return lines.join("\n");
}

/** Converts an EPUB book's chapters, in reading order, into one page's
 * Markdown — every chapter, or FileImportTooLargeError once larger than
 * Leafen imports. */
export async function epubToImportMarkdown(blob: Blob, uploadAsset?: ImportAssetUploader, leftOut?: ImportLeftOut) {
  const entries = await listZipEntries(blob);
  const read = async (name: string) => {
    const entry = entries.find((candidate) => candidate.name === name);
    return entry ? new TextDecoder("utf-8").decode(await entry.read()) : "";
  };
  const container = await read("META-INF/container.xml");
  const packagePath = container
    ? Array.from(parseXml(container).getElementsByTagName("*")).find((node) => node.localName === "rootfile")?.getAttribute("full-path")
    : undefined;
  if (!packagePath) throw new BookReadError("The file is not an EPUB book.");
  const packageDocument = parseXml(await read(packagePath));
  const packageDirectory = packagePath.includes("/") ? packagePath.slice(0, packagePath.lastIndexOf("/")) : "";
  const all = Array.from(packageDocument.getElementsByTagName("*"));
  const manifest = new Map(all.filter((node) => node.localName === "item")
    .map((item) => [item.getAttribute("id") ?? "", item.getAttribute("href") ?? ""]));
  const chapters = all.filter((node) => node.localName === "itemref")
    .map((itemref) => manifest.get(itemref.getAttribute("idref") ?? "") ?? "")
    .map((href) => resolveArchivePath(packageDirectory, href))
    .filter((path): path is string => Boolean(path));
  const budget = new ImportOutputBudget();
  const uploads = new Map<string, Promise<ImportReference | null>>();
  const parts: string[] = [];
  for (const chapter of chapters) {
    const html = await read(chapter);
    if (!html) continue;
    const directory = chapter.includes("/") ? chapter.slice(0, chapter.lastIndexOf("/")) : "";
    const references = new Map<string, ImportReference>();
    for (const href of uploadAsset ? htmlReferenceHrefs(html) : []) {
      if (!isRelativeImportHref(href)) continue;
      const path = resolveArchivePath(directory, href);
      const entry = path ? entries.find((candidate) => candidate.name === path) : undefined;
      // Pictures are uploaded; links to other chapters keep their text.
      if (!entry || /\.x?html?$/i.test(entry.name)) continue;
      let upload = uploads.get(entry.name);
      if (!upload) {
        upload = entry.blob().then((file) => uploadAsset!(entry.name.split("/").pop() ?? entry.name, file)).catch(() => null);
        uploads.set(entry.name, upload);
      }
      const reference = await upload;
      if (reference) references.set(href, reference);
      else leftOut?.add(entry.name.split("/").pop() ?? entry.name);
    }
    const markdown = htmlToImportMarkdown(html, references);
    budget.add(markdown.length + 2);
    if (markdown.trim()) parts.push(markdown);
  }
  if (parts.length === 0) throw new BookReadError("The book has no chapters to import.");
  return parts.join("\n\n");
}
