import type { WorkspaceDocumentKind } from "@/workspaceMembershipWire.generated";

const BY_TYPE: Readonly<Record<string, WorkspaceDocumentKind>> = {
  "application/vnd.openxmlformats-officedocument.wordprocessingml.document": "docx",
  "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": "xlsx",
  "application/vnd.openxmlformats-officedocument.presentationml.presentation": "pptx",
  "application/pdf": "pdf",
  "application/hwp+zip": "hwpx", "application/vnd.hancom.hwpx": "hwpx", "application/haansofthwpx": "hwpx",
  "application/x-hwp": "hwp", "application/haansofthwp": "hwp", "application/vnd.hancom.hwp": "hwp",
};
const BY_EXTENSION: readonly WorkspaceDocumentKind[] = ["docx", "xlsx", "pptx", "pdf", "hwpx", "hwp"];

/**
 * The kind of document a file is, as the library's "문서" lists it (the
 * server reads it the same way): by its type, else its name's extension;
 * other files are none.
 */
export function documentKindOf(name: string, contentType: string): WorkspaceDocumentKind | null {
  const byType = BY_TYPE[contentType.split(";")[0].trim().toLowerCase()];
  if (byType) return byType;
  const lower = name.toLowerCase();
  return BY_EXTENSION.find((kind) => lower.endsWith(`.${kind}`)) ?? null;
}

/**
 * A Word, Excel or PowerPoint file, which Leafen's document preview draws in
 * an embed: by its type, or, stored without one, its name's extension (the
 * server's officeDocumentPreviewable).
 */
export function officeDocumentPreviewable(name: string, contentType: string) {
  const type = contentType.split(";")[0].trim().toLowerCase();
  const kind = BY_TYPE[type];
  if (kind === "docx" || kind === "xlsx" || kind === "pptx") return true;
  return (!type || type === "application/octet-stream") && /\.(?:docx|xlsx|pptx)$/iu.test(name.trim());
}

// The kin of Word and Excel files the preview's engines read as they are:
// docx-preview a macro-enabled or template Word file, SheetJS (its full
// build) a macro-enabled, binary, Excel 97–2003, OpenDocument or Numbers
// sheet, which the preview draws as a plain table ("sheet", its table.js), as
// Notion's preview and embed draw any sheet but an .xlsx file (seen
// 2026-11-15: .xls and .ods twice, .xlsm and .xlsb once; Numbers unmeasured).
// By type, else by name.
const DRAWN_AS: Readonly<Record<string, "docx" | "sheet">> = {
  "application/vnd.ms-word.document.macroenabled.12": "docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.template": "docx",
  "application/vnd.ms-excel.sheet.macroenabled.12": "sheet", "application/vnd.ms-excel.sheet.binary.macroenabled.12": "sheet",
  "application/vnd.ms-excel": "sheet", "application/vnd.oasis.opendocument.spreadsheet": "sheet",
  "application/vnd.apple.numbers": "sheet", "application/x-iwork-numbers-sffnumbers": "sheet",
};
const DRAWN_AS_EXTENSION: Readonly<Record<string, "docx" | "sheet">> = {
  docm: "docx", dotx: "docx", xlsm: "sheet", xlsb: "sheet", xls: "sheet", ods: "sheet", numbers: "sheet",
};

/** What Leafen's document preview draws: a Word, Excel (.xlsx) or PowerPoint file, a CSV file or another sheet as a plain table. */
export type EmbedDocumentKind = "docx" | "xlsx" | "pptx" | "csv" | "sheet";

/**
 * The document an uploaded embed draws through Leafen's document preview: a
 * Word, Excel or PowerPoint file and those its engines read alike (DRAWN_AS),
 * or a CSV file as a sheet (Notion's, seen 2026-11-12), by its type, else its
 * name; a PDF or a medium is none.
 */
export function embedDocumentKind(name: string, contentType: string): EmbedDocumentKind | null {
  const type = contentType.split(";")[0].trim().toLowerCase();
  if (type === "text/csv") return "csv";
  if (type === "application/pdf" || /^(?:image|video|audio)\//u.test(type)) return null;
  const kind = documentKindOf(name, type);
  if (kind === "docx" || kind === "xlsx" || kind === "pptx") return kind;
  const drawn = DRAWN_AS[type] ?? DRAWN_AS_EXTENSION[/\.([a-z0-9]+)$/iu.exec(name.trim())?.[1]?.toLowerCase() ?? ""];
  if (drawn) return drawn;
  return /\.csv$/iu.test(name.trim()) ? "csv" : null;
}
