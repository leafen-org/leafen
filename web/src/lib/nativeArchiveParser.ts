import {
  NATIVE_ARCHIVE_DESCRIPTOR_PATH,
  type ParsedNativeArchive,
} from "./nativeArchiveFormat";
import { parseNativeArchiveV2 } from "./nativeArchiveV2";
import { nativeArchiveError, parseZipDirectory } from "./nativeArchiveZip";

/** A file named as Leafen names its archive. The name is only a hint: a
 * browser saving a name it already saved writes "… (1).zip", so an archive
 * is known by its contents, the descriptor it holds. */
export function isNativeArchiveFile(file: File) {
  return /\.leafen\.zip$/i.test(file.name);
}

export async function hasNativeArchiveDescriptor(file: File) {
  try {
    return (await parseZipDirectory(file)).byName.has(NATIVE_ARCHIVE_DESCRIPTOR_PATH);
  } catch {
    return false;
  }
}

export async function parseNativeArchive(file: File): Promise<ParsedNativeArchive> {
  const directory = await parseZipDirectory(file);
  if (directory.byName.has(NATIVE_ARCHIVE_DESCRIPTOR_PATH)) {
    return parseNativeArchiveV2(file, directory);
  }
  throw nativeArchiveError("archive format version is unsupported.");
}
