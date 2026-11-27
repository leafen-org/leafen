import { fileImportKind, isZipImportFile, type FileImportKind } from "./fileImportSources";

/**
 * The files the box's "파일을 선택하세요" offers, Notion's list (seen
 * 2026-11-19): text and Markdown, CSV, HTML, PDF, Word and ZIP. An EPUB or
 * a TSV is dropped on the box, as in Notion, whose chooser offers neither;
 * a name it offers that the box doesn't take (.mkdn, .Rmd, .dotx …) is
 * refused once chosen, as Notion's is.
 */
export const CONTENT_BOX_ACCEPT = [
  "text/plain", ".csv", "text/markdown", "text/x-markdown", ".md", ".mdown", ".markdown", ".mkdn", ".mkd", ".Rmd",
  ".md.txt", ".txt", "text/html", ".html", ".htm", "application/pdf", ".pdf",
  "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
  "application/vnd.openxmlformats-officedocument.wordprocessingml.template",
  "application/vnd.ms-word.document.macroEnabled.12", "application/vnd.ms-word.template.macroEnabled.12",
  ".docx", ".dotx", ".docm", ".dotm", "application/zip", ".zip",
].join(",");

/**
 * What Notion's "콘텐츠 가져오기" box takes one file as (seen 2026-11-19,
 * dropping each kind): a ZIP, a CSV or TSV (the CSV window), or a document
 * fileImportKind names. It refuses anything else (null): an image, a
 * spreadsheet other than CSV, JSON, RTF, a file with no extension.
 */
export function contentBoxFileKind(file: Pick<File, "name" | "type">): FileImportKind | "zip" | null {
  if (isZipImportFile(file)) return "zip";
  if (/\.tsv$/i.test(file.name)) return "csv";
  return fileImportKind(file.name, file.type);
}
