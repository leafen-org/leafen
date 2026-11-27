import { fileTransferBounds } from "@/fileTransferWire.generated";

/**
 * Reads the files of an ordinary ZIP (stored or deflated, ZIP64 included),
 * such as a .docx or a folder exported from another app. Every file is
 * listed, however many and however large (2026-10-14: past 2,000 files or
 * 256 MiB in all, another tool's export was refused whole); entries are
 * read lazily, one at a time, and none inflates past the size it declares.
 * What an entry declares is checked before it is inflated: one read into
 * memory is at most the longest text a browser holds, one taken as a file
 * at most the largest file Leafen keeps, so a small archive cannot inflate
 * into an unbounded one. What reads a document out of it bounds that
 * document.
 */

/** The most one entry read into memory may be: about the longest string a
 * browser holds (Chrome's is just over 512 Mi characters). */
export const ZIP_ENTRY_READ_MAX_BYTES = 512 * 1024 * 1024;

export type ZipFileEntry = {
  /** The entry's path inside the archive, with "/" separators. */
  name: string;
  bytes: number;
  /** The file's bytes, in memory. */
  read(): Promise<Uint8Array>;
  /** The file as a Blob, streamed out of the archive (a browser keeps a
   * large one on disk): for uploading it as it is. */
  blob(): Promise<Blob>;
};

const utf8 = new TextDecoder("utf-8");

export class ZipReadError extends Error {}

/** An entry larger than it may be read as. */
export class ZipEntryTooLargeError extends ZipReadError {}

function u16(view: DataView, offset: number) {
  return view.getUint16(offset, true);
}

function u32(view: DataView, offset: number) {
  return view.getUint32(offset, true);
}

function u64(view: DataView, offset: number) {
  const value = u32(view, offset) + u32(view, offset + 4) * 0x1_0000_0000;
  if (!Number.isSafeInteger(value)) throw new ZipReadError("The ZIP directory is damaged.");
  return value;
}

async function sliceBytes(blob: Blob, start: number, end: number) {
  return new Uint8Array(await blob.slice(start, end).arrayBuffer());
}

/** Hidden files, folders and macOS resource forks are not content. */
function zipEntryIsContent(name: string) {
  if (!name || name.endsWith("/") || name.startsWith("__MACOSX/")) return false;
  return !name.split("/").some((part) => part.startsWith("."));
}

// Inflates a deflated entry into a Blob, failing once it passes the size it
// declared.
async function inflate(data: Blob, limit: number) {
  let total = 0;
  const bounded = new TransformStream<Uint8Array, Uint8Array>({
    transform(chunk, controller) {
      total += chunk.byteLength;
      if (total > limit) controller.error(new ZipReadError("A ZIP entry is larger than it declared."));
      else controller.enqueue(chunk);
    },
  });
  try {
    return await new Response(data.stream().pipeThrough(new DecompressionStream("deflate-raw")).pipeThrough(bounded)).blob();
  } catch (error) {
    throw error instanceof ZipReadError ? error : new ZipReadError("A ZIP entry is damaged.");
  }
}

export async function listZipEntries(blob: Blob): Promise<ZipFileEntry[]> {
  return (await readZipDirectory(blob)).entries;
}

/** A folder an entry names on its own ("Folder/"), such as an empty one: content as `zipEntryIsContent` says. */
function zipFolderIsContent(name: string) {
  return name.endsWith("/") && zipEntryIsContent(name.slice(0, -1));
}

/**
 * The archive's files, and the folders it names on their own (without the
 * trailing "/"), which an empty folder has only there.
 */
export async function readZipDirectory(blob: Blob): Promise<{ entries: ZipFileEntry[]; folders: string[] }> {
  const tailStart = Math.max(0, blob.size - 65_557);
  const tail = await sliceBytes(blob, tailStart, blob.size);
  const tailView = new DataView(tail.buffer, tail.byteOffset, tail.byteLength);
  let eocd = -1;
  for (let offset = tail.byteLength - 22; offset >= 0; offset -= 1) {
    if (u32(tailView, offset) === 0x06054b50) {
      eocd = offset;
      break;
    }
  }
  if (eocd < 0) throw new ZipReadError("The file is not a ZIP archive.");
  let count = u16(tailView, eocd + 10);
  let centralBytes = u32(tailView, eocd + 12);
  let centralOffset = u32(tailView, eocd + 16);
  if (count === 0xffff || centralBytes === 0xffffffff || centralOffset === 0xffffffff) {
    // A ZIP64 archive's end record is named by the locator just before the
    // ordinary one.
    const locatorStart = tailStart + eocd - 20;
    const locator = locatorStart >= 0 ? await sliceBytes(blob, locatorStart, locatorStart + 20) : new Uint8Array();
    const locatorView = new DataView(locator.buffer, locator.byteOffset, locator.byteLength);
    if (locator.byteLength !== 20 || u32(locatorView, 0) !== 0x07064b50) throw new ZipReadError("The ZIP directory is damaged.");
    const recordStart = u64(locatorView, 8);
    const record = await sliceBytes(blob, recordStart, recordStart + 56);
    const recordView = new DataView(record.buffer, record.byteOffset, record.byteLength);
    if (record.byteLength !== 56 || u32(recordView, 0) !== 0x06064b50) throw new ZipReadError("The ZIP directory is damaged.");
    count = u64(recordView, 32);
    centralBytes = u64(recordView, 40);
    centralOffset = u64(recordView, 48);
  }
  if (centralOffset + centralBytes > blob.size) throw new ZipReadError("The ZIP directory is damaged.");
  const central = await sliceBytes(blob, centralOffset, centralOffset + centralBytes);
  const view = new DataView(central.buffer, central.byteOffset, central.byteLength);
  const entries: ZipFileEntry[] = [];
  const folders: string[] = [];
  let offset = 0;
  for (let index = 0; index < count; index += 1) {
    if (offset + 46 > central.byteLength || u32(view, offset) !== 0x02014b50) {
      throw new ZipReadError("The ZIP directory is damaged.");
    }
    const flags = u16(view, offset + 8);
    const method = u16(view, offset + 10);
    let compressedBytes = u32(view, offset + 20);
    let bytes = u32(view, offset + 24);
    const nameLength = u16(view, offset + 28);
    const extraLength = u16(view, offset + 30);
    const commentLength = u16(view, offset + 32);
    let localOffset = u32(view, offset + 42);
    const name = utf8.decode(central.subarray(offset + 46, offset + 46 + nameLength)).replace(/\\/g, "/");
    if (bytes === 0xffffffff || compressedBytes === 0xffffffff || localOffset === 0xffffffff) {
      // A ZIP64 entry's sizes and place are in its extra field, those its
      // record leaves at their largest, in order.
      let field = offset + 46 + nameLength;
      const fieldsEnd = field + extraLength;
      while (field + 4 <= fieldsEnd && u16(view, field) !== 0x0001) field += 4 + u16(view, field + 2);
      if (field + 4 > fieldsEnd) throw new ZipReadError("The ZIP directory is damaged.");
      let value = field + 4;
      const next = () => {
        if (value + 8 > fieldsEnd) throw new ZipReadError("The ZIP directory is damaged.");
        const read = u64(view, value);
        value += 8;
        return read;
      };
      if (bytes === 0xffffffff) bytes = next();
      if (compressedBytes === 0xffffffff) compressedBytes = next();
      if (localOffset === 0xffffffff) localOffset = next();
    }
    offset += 46 + nameLength + extraLength + commentLength;
    if (zipFolderIsContent(name)) folders.push(name.slice(0, -1));
    if (!zipEntryIsContent(name)) continue;
    if (flags & 1) throw new ZipReadError("Encrypted ZIP archives are not supported.");
    if (method !== 0 && method !== 8) throw new ZipReadError("The ZIP archive uses an unsupported compression.");
    const file = async () => {
      const head = await sliceBytes(blob, localOffset, localOffset + 30);
      const headView = new DataView(head.buffer, head.byteOffset, head.byteLength);
      if (head.byteLength !== 30 || u32(headView, 0) !== 0x04034b50) {
        throw new ZipReadError("A ZIP entry is damaged.");
      }
      const dataStart = localOffset + 30 + u16(headView, 26) + u16(headView, 28);
      const data = blob.slice(dataStart, dataStart + compressedBytes);
      const content = method === 0 ? data : await inflate(data, bytes);
      if (content.size !== bytes) throw new ZipReadError("A ZIP entry is damaged.");
      return content;
    };
    entries.push({
      name,
      bytes,
      blob: async () => {
        if (bytes > fileTransferBounds.maxFileBytes) throw new ZipEntryTooLargeError("A ZIP entry is larger than Leafen keeps.");
        return file();
      },
      read: async () => {
        if (bytes > ZIP_ENTRY_READ_MAX_BYTES) throw new ZipEntryTooLargeError("A ZIP entry is too large to read.");
        return new Uint8Array(await (await file()).arrayBuffer());
      },
    });
  }
  return { entries, folders };
}
