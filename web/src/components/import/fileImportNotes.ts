import type { TFunction } from "i18next";
import { BookReadError } from "@/lib/bookImport";
import { isActiveContentFile } from "@/lib/fileSecurity";
import { FileImportTooLargeError, type FileImportConversion } from "@/lib/fileImportSources";
import { PdfReadError } from "@/lib/pdfDocument";
import { productErrorMessage } from "@/lib/productErrorPresentation";
import { SpreadsheetReadError } from "@/lib/spreadsheetImport";
import { XlsReadError } from "@/lib/xlsWorkbook";
import { ZipEntryTooLargeError, ZipReadError } from "@/lib/zipArchiveReader";

// What a converted file left out: parts of a PDF, and files it uses that
// weren't brought in — named, the first few in the note and all in `details`.
// A file of a type the file security policy refuses (isActiveContentFile) is
// named apart with that reason, as a Notion import's report names it.
const NAMES_SHOWN = 5;
export function fileImportNotes(converted: FileImportConversion, t: TFunction): { message?: string; details?: string } {
  const notes: string[] = [];
  if (converted.omitted) notes.push(t("importDialog:batch.pdfOmitted", { count: converted.omitted }));
  const names = converted.leftOut ?? [];
  const refused = names.filter((name) => isActiveContentFile({ name }));
  for (const [key, group] of [["importDialog:batch.filesRefused", refused], ["importDialog:batch.filesLeftOut", names.filter((name) => !refused.includes(name))]] as const) {
    if (!group.length) continue;
    const shown = group.slice(0, NAMES_SHOWN).join(", ");
    notes.push(t(key, {
      count: group.length,
      names: group.length > NAMES_SHOWN ? t("importDialog:batch.moreNames", { names: shown, count: group.length - NAMES_SHOWN }) : shown,
    }));
  }
  return notes.length ? { message: notes.join(" · "), ...(names.length > NAMES_SHOWN ? { details: names.join("\n") } : {}) } : {};
}

/** Why a file couldn't be converted or imported, in the reader's words. */
export function fileImportFailure(caught: unknown, t: TFunction) {
  if (caught instanceof FileImportTooLargeError || caught instanceof ZipEntryTooLargeError) return t("importDialog:batch.tooLarge");
  if (caught instanceof PdfReadError) {
    return t(caught.reason === "encrypted" ? "importDialog:batch.pdfEncrypted"
      : caught.reason === "no_text" ? "importDialog:batch.pdfNoText" : "importDialog:batch.pdfUnreadable");
  }
  if (caught instanceof SpreadsheetReadError || caught instanceof BookReadError || caught instanceof XlsReadError || caught instanceof ZipReadError
    || (caught instanceof Error && /Word document/.test(caught.message))) return t("importDialog:batch.unreadable");
  return productErrorMessage(caught, "workspaceImport.start");
}
