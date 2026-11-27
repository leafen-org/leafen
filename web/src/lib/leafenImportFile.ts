import type { LeafenExportDocument } from "./leafenExportContract";
import {
  parseLeafenExportDocument,
  type LeafenExportParserBounds,
} from "./leafenExportParser";
import {
  hasNativeArchiveDescriptor,
  isNativeArchiveFile,
  parseNativeArchive,
  type ParsedNativeArchive,
} from "./nativeArchive";

export type LeafenImportFileBounds = {
  maxJsonBytes: number;
  parser: LeafenExportParserBounds;
};

export type LeafenImportFileSelection =
  | { kind: "json"; document: LeafenExportDocument }
  | { kind: "archive"; archive: ParsedNativeArchive }
  | { kind: "workspace_archive"; file: File };

export type LeafenImportFileError = Error & {
  code: "LEAFEN_IMPORT_FILE_BOUND_EXCEEDED" | "LEAFEN_IMPORT_FILE_INVALID" | "LEAFEN_IMPORT_FILE_FROM_HANJI";
  stage: "bounds" | "file-name" | "json-size";
};

function importFileError(
  code: LeafenImportFileError["code"],
  stage: LeafenImportFileError["stage"],
  message: string,
): LeafenImportFileError {
  const error = Object.assign(new Error(message), { code, stage });
  error.name = "LeafenImportFileError";
  return error;
}

function assertImportFileBounds(bounds: LeafenImportFileBounds) {
  if (!Number.isSafeInteger(bounds.maxJsonBytes) || bounds.maxJsonBytes <= 0) {
    throw importFileError(
      "LEAFEN_IMPORT_FILE_BOUND_EXCEEDED",
      "bounds",
      "The Leafen import JSON byte bound must be a positive safe integer.",
    );
  }
}

/** A file Hanji, the product Leafen was before, exported. Leafen does not
 * import it; the person is told so rather than that the file is broken. */
export function isHanjiExportFile(file: File) {
  return /\.hanji(\.zip|\.json)?$/i.test(file.name);
}

export function isLeafenImportFile(file: File) {
  return (
    isNativeArchiveFile(file)
    || /\.leafen\.json$/i.test(file.name)
    || /\.leafen$/i.test(file.name)
  );
}

// A Leafen archive is known by its name or by the descriptor a zip holds,
// whatever its name ("… (1).zip"): every way in that takes files asks this,
// so an archive reaches the archive import from each of them.
export async function holdsLeafenArchive(file: File) {
  if (isLeafenImportFile(file)) return true;
  return await hasZipSignature(file) && await hasNativeArchiveDescriptor(file);
}

async function hasZipSignature(file: File) {
  const bytes = new Uint8Array(await file.slice(0, 4).arrayBuffer());
  if (bytes.length !== 4 || bytes[0] !== 0x50 || bytes[1] !== 0x4b) return false;
  return (
    (bytes[2] === 0x03 && bytes[3] === 0x04)
    || (bytes[2] === 0x05 && bytes[3] === 0x06)
    || (bytes[2] === 0x07 && bytes[3] === 0x08)
  );
}

async function readLeafenJsonFile(file: File, bounds: LeafenImportFileBounds) {
  if (file.size > bounds.maxJsonBytes) {
    throw importFileError(
      "LEAFEN_IMPORT_FILE_BOUND_EXCEEDED",
      "json-size",
      "The Leafen import file exceeds its caller-owned JSON byte bound.",
    );
  }
  return parseLeafenExportDocument(await file.text(), bounds.parser);
}

export async function readLeafenImportFile(
  file: File,
  bounds: LeafenImportFileBounds,
): Promise<LeafenImportFileSelection> {
  assertImportFileBounds(bounds);
  if (isHanjiExportFile(file)) {
    throw importFileError(
      "LEAFEN_IMPORT_FILE_FROM_HANJI",
      "file-name",
      "The selected file was exported from Hanji, which Leafen does not import.",
    );
  }
  const notLeafen = () => importFileError(
    "LEAFEN_IMPORT_FILE_INVALID",
    "file-name",
    "The selected file is not a supported Leafen import format.",
  );
  // A file is known by what it holds, as a browser saving a name twice
  // writes "… (1).zip": a zip holding Leafen's archive descriptor is an
  // archive whatever its name.
  if (await hasZipSignature(file)) {
    try {
      return { kind: "archive", archive: await parseNativeArchive(file) };
    } catch {
      // One the browser cannot read whole goes to the server to read.
      if (isNativeArchiveFile(file) || await hasNativeArchiveDescriptor(file)) return { kind: "workspace_archive", file };
      throw notLeafen();
    }
  }
  if (!isLeafenImportFile(file) && !/\.json$/i.test(file.name)) throw notLeafen();
  return { kind: "json", document: await readLeafenJsonFile(file, bounds) };
}
