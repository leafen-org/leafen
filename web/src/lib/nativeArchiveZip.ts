import { sha256 } from "@noble/hashes/sha2.js";
import { bytesToHex } from "@noble/hashes/utils.js";
import { NATIVE_ARCHIVE_LIMITS } from "./nativeArchiveFormat";
import { captureErrorFactoryCaller } from "./safeErrorLog";

const encoder = new TextEncoder();
const decoder = new TextDecoder("utf-8", { fatal: true });
const CONTROL_CHARS = /[\u0000-\u001f\u007f]/;
const MAX_ZIP_DIRECTORY_ENTRIES = 4096;

export interface ZipDirectoryEntry {
  name: string;
  flags: number;
  method: number;
  crc32: number;
  compressedBytes: number;
  bytes: number;
  localOffset: number;
  dataStart: number;
  dataEnd: number;
  recordEnd: number;
}

export interface ParsedZipDirectory {
  entries: ZipDirectoryEntry[];
  byName: Map<string, ZipDirectoryEntry>;
}

export function nativeArchiveError(message: string) {
  return captureErrorFactoryCaller(new Error(`Invalid Leafen archive: ${message}`), nativeArchiveError);
}

export function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === "object" && !Array.isArray(value);
}

export function exactInteger(value: unknown, label: string, minimum = 0) {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < minimum) {
    throw nativeArchiveError(`${label} must be an integer of at least ${minimum}.`);
  }
  return value;
}

export function boundedString(value: unknown, label: string, max: number) {
  if (typeof value !== "string" || !value.trim()) throw nativeArchiveError(`${label} must be a non-empty string.`);
  if (value.length > max || CONTROL_CHARS.test(value)) throw nativeArchiveError(`${label} is too long or invalid.`);
  return value;
}

export function canonicalJson(bytes: Uint8Array, label: string) {
  let text: string;
  let parsed: unknown;
  try {
    text = decoder.decode(bytes);
    parsed = JSON.parse(text);
  } catch {
    throw nativeArchiveError(`${label} is not valid UTF-8 JSON.`);
  }
  if (JSON.stringify(parsed) !== text) throw nativeArchiveError(`${label} must use canonical compact JSON.`);
  return parsed;
}

function u16(view: DataView, offset: number) {
  return view.getUint16(offset, true);
}

function u32(view: DataView, offset: number) {
  return view.getUint32(offset, true);
}

function safeU64(view: DataView, offset: number, label: string) {
  const value = view.getBigUint64(offset, true);
  if (value > BigInt(Number.MAX_SAFE_INTEGER)) throw nativeArchiveError(`${label} exceeds browser-safe addressing.`);
  return Number(value);
}

function validPath(name: string) {
  const bytes = encoder.encode(name).byteLength;
  if (!name || bytes > NATIVE_ARCHIVE_LIMITS.maxPathBytes || CONTROL_CHARS.test(name)) return false;
  if (name.startsWith("/") || name.includes("\\") || name.split("/").some((part) => !part || part === "." || part === "..")) return false;
  return name.split("/").length <= NATIVE_ARCHIVE_LIMITS.maxPathDepth;
}

function zip64Values(
  extra: Uint8Array,
  needs: { bytes: boolean; compressed: boolean; offset: boolean },
) {
  const view = new DataView(extra.buffer, extra.byteOffset, extra.byteLength);
  let cursor = 0;
  while (cursor + 4 <= extra.byteLength) {
    const id = u16(view, cursor);
    const size = u16(view, cursor + 2);
    const start = cursor + 4;
    const end = start + size;
    if (end > extra.byteLength) throw nativeArchiveError("ZIP extra field is truncated.");
    if (id === 0x0001) {
      let valueOffset = start;
      const read = (required: boolean, label: string) => {
        if (!required) return undefined;
        if (valueOffset + 8 > end) throw nativeArchiveError(`ZIP64 ${label} is missing.`);
        const value = safeU64(view, valueOffset, `ZIP64 ${label}`);
        valueOffset += 8;
        return value;
      };
      return {
        bytes: read(needs.bytes, "entry bytes"),
        compressed: read(needs.compressed, "compressed bytes"),
        offset: read(needs.offset, "local offset"),
      };
    }
    cursor = end;
  }
  if (needs.bytes || needs.compressed || needs.offset) throw nativeArchiveError("ZIP64 entry metadata is missing.");
  return {};
}

async function locateCentralDirectory(file: File, tail: Uint8Array, tailStart: number, relativeEocd: number) {
  const view = new DataView(tail.buffer, tail.byteOffset, tail.byteLength);
  const eocdOffset = tailStart + relativeEocd;
  if (u16(view, relativeEocd + 4) !== 0 || u16(view, relativeEocd + 6) !== 0) {
    throw nativeArchiveError("multi-disk ZIP files are not supported.");
  }
  const entries16 = u16(view, relativeEocd + 10);
  const centralBytes32 = u32(view, relativeEocd + 12);
  const centralOffset32 = u32(view, relativeEocd + 16);
  const needsZip64 = entries16 === 0xffff || centralBytes32 === 0xffffffff || centralOffset32 === 0xffffffff;
  if (!needsZip64) {
    return { totalEntries: entries16, centralBytes: centralBytes32, centralOffset: centralOffset32, eocdOffset };
  }
  if (eocdOffset < 20) throw nativeArchiveError("ZIP64 locator is missing.");
  const locator = new Uint8Array(await file.slice(eocdOffset - 20, eocdOffset).arrayBuffer());
  const locatorView = new DataView(locator.buffer, locator.byteOffset, locator.byteLength);
  if (locator.byteLength !== 20 || u32(locatorView, 0) !== 0x07064b50 || u32(locatorView, 4) !== 0 || u32(locatorView, 16) !== 1) {
    throw nativeArchiveError("ZIP64 locator is invalid.");
  }
  const zip64Offset = safeU64(locatorView, 8, "ZIP64 end record offset");
  const record = new Uint8Array(await file.slice(zip64Offset, zip64Offset + 56).arrayBuffer());
  const recordView = new DataView(record.buffer, record.byteOffset, record.byteLength);
  if (record.byteLength !== 56 || u32(recordView, 0) !== 0x06064b50 || safeU64(recordView, 4, "ZIP64 end record size") < 44) {
    throw nativeArchiveError("ZIP64 end record is invalid.");
  }
  if (u32(recordView, 16) !== 0 || u32(recordView, 20) !== 0) throw nativeArchiveError("multi-disk ZIP64 files are not supported.");
  const diskEntries = safeU64(recordView, 24, "ZIP64 disk entries");
  const totalEntries = safeU64(recordView, 32, "ZIP64 total entries");
  if (diskEntries !== totalEntries) throw nativeArchiveError("ZIP64 entry counts do not match.");
  return {
    totalEntries,
    centralBytes: safeU64(recordView, 40, "ZIP64 central bytes"),
    centralOffset: safeU64(recordView, 48, "ZIP64 central offset"),
    eocdOffset: zip64Offset,
  };
}

export async function parseZipDirectory(file: File): Promise<ParsedZipDirectory> {
  if (file.size <= 0 || !Number.isSafeInteger(file.size)) throw nativeArchiveError("archive size is invalid.");
  const tailStart = Math.max(0, file.size - 65_557);
  const tail = new Uint8Array(await file.slice(tailStart).arrayBuffer());
  const tailView = new DataView(tail.buffer, tail.byteOffset, tail.byteLength);
  let relativeEocd = -1;
  for (let offset = tail.byteLength - 22; offset >= 0; offset -= 1) {
    if (u32(tailView, offset) === 0x06054b50 && offset + 22 + u16(tailView, offset + 20) === tail.byteLength) {
      relativeEocd = offset;
      break;
    }
  }
  if (relativeEocd < 0) throw nativeArchiveError("ZIP end-of-directory record is missing.");
  const directory = await locateCentralDirectory(file, tail, tailStart, relativeEocd);
  if (directory.totalEntries < 2 || directory.totalEntries > MAX_ZIP_DIRECTORY_ENTRIES) {
    throw nativeArchiveError("ZIP entry count is invalid for a Leafen archive.");
  }
  if (directory.centralBytes > NATIVE_ARCHIVE_LIMITS.readerWindowBytes) {
    throw nativeArchiveError("ZIP control directory exceeds its bounded reader window.");
  }
  if (directory.centralOffset + directory.centralBytes !== directory.eocdOffset) {
    throw nativeArchiveError("ZIP central directory boundaries are invalid.");
  }
  const central = new Uint8Array(await file.slice(directory.centralOffset, directory.eocdOffset).arrayBuffer());
  const view = new DataView(central.buffer, central.byteOffset, central.byteLength);
  const entries: ZipDirectoryEntry[] = [];
  const names = new Set<string>();
  let offset = 0;
  for (let index = 0; index < directory.totalEntries; index += 1) {
    if (offset + 46 > central.byteLength || u32(view, offset) !== 0x02014b50) {
      throw nativeArchiveError("ZIP central directory is truncated or malformed.");
    }
    const madeBy = u16(view, offset + 4);
    const flags = u16(view, offset + 8);
    const method = u16(view, offset + 10);
    const crc32 = u32(view, offset + 16);
    let compressedBytes = u32(view, offset + 20);
    let bytes = u32(view, offset + 24);
    const nameBytes = u16(view, offset + 28);
    const extraBytes = u16(view, offset + 30);
    const commentBytes = u16(view, offset + 32);
    const disk = u16(view, offset + 34);
    const externalAttributes = u32(view, offset + 38);
    let localOffset = u32(view, offset + 42);
    const end = offset + 46 + nameBytes + extraBytes + commentBytes;
    if (end > central.byteLength) throw nativeArchiveError("ZIP central entry is truncated.");
    let name: string;
    try {
      name = decoder.decode(central.subarray(offset + 46, offset + 46 + nameBytes));
    } catch {
      throw nativeArchiveError("ZIP entry name is not valid UTF-8.");
    }
    if (!validPath(name) || names.has(name)) throw nativeArchiveError(`ZIP path ${name || "(empty)"} is invalid or duplicated.`);
    names.add(name);
    if (disk !== 0) throw nativeArchiveError("multi-disk ZIP entries are not supported.");
    const extra = central.subarray(offset + 46 + nameBytes, offset + 46 + nameBytes + extraBytes);
    const zip64 = zip64Values(extra, {
      bytes: bytes === 0xffffffff,
      compressed: compressedBytes === 0xffffffff,
      offset: localOffset === 0xffffffff,
    });
    bytes = zip64.bytes ?? bytes;
    compressedBytes = zip64.compressed ?? compressedBytes;
    localOffset = zip64.offset ?? localOffset;
    // Compression first: it sets flag bits of its own, and it is what a person can change.
    if (method !== 0 || compressedBytes !== bytes) throw nativeArchiveError("only store-only ZIP entries are supported.");
    // Without the UTF-8 mark a name must be plain ASCII, as a plain `zip` writes it; the server reads those too.
    if (flags & 1 || (flags & ~0x0808) !== 0 || ((flags & 0x0800) === 0 && !/^[\x00-\x7f]*$/u.test(name))) {
      throw nativeArchiveError("ZIP entry flags are unsupported.");
    }
    const unixMode = madeBy >>> 8 === 3 ? (externalAttributes >>> 16) & 0xffff : 0;
    if ((unixMode & 0xf000) === 0xa000) throw nativeArchiveError("ZIP symbolic-link entries are not supported.");
    entries.push({ name, flags, method, crc32, compressedBytes, bytes, localOffset, dataStart: 0, dataEnd: 0, recordEnd: 0 });
    offset = end;
  }
  if (offset !== central.byteLength) throw nativeArchiveError("ZIP central directory has unparsed bytes.");

  const ordered = [...entries].sort((left, right) => left.localOffset - right.localOffset);
  let expectedOffset = 0;
  for (const entry of ordered) {
    if (entry.localOffset !== expectedOffset || entry.localOffset + 30 > directory.centralOffset) {
      throw nativeArchiveError("ZIP local entries overlap or contain unexplained gaps.");
    }
    const head = new Uint8Array(await file.slice(entry.localOffset, entry.localOffset + 30).arrayBuffer());
    const headView = new DataView(head.buffer, head.byteOffset, head.byteLength);
    if (head.byteLength !== 30 || u32(headView, 0) !== 0x04034b50) throw nativeArchiveError("ZIP local header is invalid.");
    const flags = u16(headView, 6);
    const method = u16(headView, 8);
    const localCrc = u32(headView, 14);
    const localCompressed = u32(headView, 18);
    const localBytes = u32(headView, 22);
    const nameBytes = u16(headView, 26);
    const extraBytes = u16(headView, 28);
    if (flags !== entry.flags || method !== entry.method) throw nativeArchiveError("ZIP local and central metadata do not match.");
    const variable = new Uint8Array(await file.slice(entry.localOffset + 30, entry.localOffset + 30 + nameBytes + extraBytes).arrayBuffer());
    if (variable.byteLength !== nameBytes + extraBytes) throw nativeArchiveError("ZIP local header is truncated.");
    if (decoder.decode(variable.subarray(0, nameBytes)) !== entry.name) throw nativeArchiveError("ZIP local and central names do not match.");
    if ((flags & 0x08) === 0 && (localCrc !== entry.crc32 || ![entry.compressedBytes, 0xffffffff].includes(localCompressed)
      || ![entry.bytes, 0xffffffff].includes(localBytes))) {
      throw nativeArchiveError("ZIP local and central sizes do not match.");
    }
    entry.dataStart = entry.localOffset + 30 + nameBytes + extraBytes;
    entry.dataEnd = entry.dataStart + entry.bytes;
    entry.recordEnd = entry.dataEnd;
    if (flags & 0x08) {
      const zip64 = entry.bytes > 0xffffffff || entry.compressedBytes > 0xffffffff;
      const length = zip64 ? 24 : 16;
      const descriptor = new Uint8Array(await file.slice(entry.dataEnd, entry.dataEnd + length).arrayBuffer());
      const descriptorView = new DataView(descriptor.buffer, descriptor.byteOffset, descriptor.byteLength);
      if (descriptor.byteLength !== length || u32(descriptorView, 0) !== 0x08074b50 || u32(descriptorView, 4) !== entry.crc32) {
        throw nativeArchiveError("ZIP data descriptor is invalid.");
      }
      const compressed = zip64 ? safeU64(descriptorView, 8, "ZIP64 descriptor compressed bytes") : u32(descriptorView, 8);
      const bytes = zip64 ? safeU64(descriptorView, 16, "ZIP64 descriptor bytes") : u32(descriptorView, 12);
      if (compressed !== entry.compressedBytes || bytes !== entry.bytes) throw nativeArchiveError("ZIP data descriptor sizes do not match.");
      entry.recordEnd += length;
    }
    expectedOffset = entry.recordEnd;
  }
  if (expectedOffset !== directory.centralOffset) throw nativeArchiveError("ZIP payload and central directory boundaries do not match.");
  return { entries, byName: new Map(entries.map((entry) => [entry.name, entry])) };
}

let crcTable: Uint32Array | undefined;
function getCrcTable() {
  if (crcTable) return crcTable;
  crcTable = new Uint32Array(256);
  for (let index = 0; index < 256; index += 1) {
    let value = index;
    for (let bit = 0; bit < 8; bit += 1) value = (value >>> 1) ^ (0xedb88320 & -(value & 1));
    crcTable[index] = value >>> 0;
  }
  return crcTable;
}

export async function hashBlob(blob: Blob, expectedBytes: number) {
  if (blob.size !== expectedBytes) throw nativeArchiveError("an entry did not match its declared byte length.");
  const hash = sha256.create();
  const table = getCrcTable();
  let crc = 0xffffffff;
  let offset = 0;
  while (offset < blob.size) {
    const next = new Uint8Array(await blob.slice(offset, Math.min(blob.size, offset + NATIVE_ARCHIVE_LIMITS.readerWindowBytes)).arrayBuffer());
    offset += next.byteLength;
    hash.update(next);
    for (const byte of next) crc = (crc >>> 8) ^ table[(crc ^ byte) & 0xff];
  }
  return { sha256: bytesToHex(hash.digest()), crc32: (crc ^ 0xffffffff) >>> 0 };
}

export async function readEntry(file: File, entry: ZipDirectoryEntry, maxBytes: number, label: string) {
  if (entry.bytes > maxBytes) throw nativeArchiveError(`${label} exceeds its compatibility or control bound.`);
  const bytes = new Uint8Array(await file.slice(entry.dataStart, entry.dataEnd).arrayBuffer());
  const digest = await hashBlob(new Blob([bytes]), entry.bytes);
  if (digest.crc32 !== entry.crc32) throw nativeArchiveError(`${label} CRC does not match.`);
  return { bytes, sha256: digest.sha256 };
}
