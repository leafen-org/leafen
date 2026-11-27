// ZIP files in the browser: reading the export a person chose — entries
// stored or deflated, without ZIP64 — and writing one of stored entries, as a
// Leafen bundle is (docs/bundle.md).

const utf8 = new TextDecoder();
const strictUtf8 = new TextDecoder("utf-8", { fatal: true });

function entryName(bytes, flags) {
  if (flags & 0x800) return utf8.decode(bytes);
  try {
    return strictUtf8.decode(bytes);
  } catch {
    return String.fromCharCode(...bytes);
  }
}

export class ZipError extends Error {}

/** The entries of a ZIP file, read on demand. */
export async function readZip(file) {
  const tailSize = Math.min(file.size, 65_557);
  const tail = new DataView(await file.slice(file.size - tailSize).arrayBuffer());
  let end = -1;
  for (let index = tail.byteLength - 22; index >= 0; index -= 1) {
    if (tail.getUint32(index, true) === 0x06054b50) { end = index; break; }
  }
  if (end < 0) throw new ZipError("not-zip");
  const count = tail.getUint16(end + 10, true);
  const directorySize = tail.getUint32(end + 12, true);
  const directoryOffset = tail.getUint32(end + 16, true);
  if (count === 0xffff || directoryOffset === 0xffffffff) throw new ZipError("zip64");
  const directory = new DataView(await file.slice(directoryOffset, directoryOffset + directorySize).arrayBuffer());
  const entries = new Map();
  let at = 0;
  for (let n = 0; n < count; n += 1) {
    if (directory.getUint32(at, true) !== 0x02014b50) throw new ZipError("bad-zip");
    const flags = directory.getUint16(at + 8, true);
    const method = directory.getUint16(at + 10, true);
    const compressed = directory.getUint32(at + 20, true);
    const size = directory.getUint32(at + 24, true);
    const nameLength = directory.getUint16(at + 28, true);
    const extraLength = directory.getUint16(at + 30, true);
    const commentLength = directory.getUint16(at + 32, true);
    const local = directory.getUint32(at + 42, true);
    const name = entryName(new Uint8Array(directory.buffer, directory.byteOffset + at + 46, nameLength), flags)
      .replaceAll("\\", "/");
    at += 46 + nameLength + extraLength + commentLength;
    if (!name.endsWith("/")) entries.set(name, { method, compressed, size, local, flags });
  }
  async function bytes(name) {
    const entry = entries.get(name);
    if (!entry) throw new ZipError(`missing:${name}`);
    if (entry.flags & 1) throw new ZipError("encrypted");
    const header = new DataView(await file.slice(entry.local, entry.local + 30).arrayBuffer());
    if (header.getUint32(0, true) !== 0x04034b50) throw new ZipError("bad-zip");
    const start = entry.local + 30 + header.getUint16(26, true) + header.getUint16(28, true);
    const data = file.slice(start, start + entry.compressed);
    if (entry.method === 0) return new Uint8Array(await data.arrayBuffer());
    if (entry.method !== 8) throw new ZipError("method");
    const inflated = data.stream().pipeThrough(new DecompressionStream("deflate-raw"));
    return new Uint8Array(await new Response(inflated).arrayBuffer());
  }
  return {
    names: [...entries.keys()],
    has: (name) => entries.has(name),
    size: (name) => entries.get(name)?.size ?? 0,
    bytes,
    text: async (name) => utf8.decode(await bytes(name)),
  };
}

const crcTable = (() => {
  const table = new Uint32Array(256);
  for (let n = 0; n < 256; n += 1) {
    let c = n;
    for (let k = 0; k < 8; k += 1) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    table[n] = c >>> 0;
  }
  return table;
})();

async function crc32(blob) {
  let crc = 0xffffffff;
  const reader = blob.stream().getReader();
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    for (let index = 0; index < value.length; index += 1) crc = crcTable[(crc ^ value[index]) & 0xff] ^ (crc >>> 8);
  }
  return (crc ^ 0xffffffff) >>> 0;
}

/** A ZIP of stored entries, in the order given; each entry's data is a Blob. */
export async function storedZip(entries) {
  const encoder = new TextEncoder();
  const parts = [];
  const central = [];
  let offset = 0;
  for (const entry of entries) {
    const name = encoder.encode(entry.name);
    const size = entry.data.size;
    if (size >= 0xffffffff || offset >= 0xffffffff) throw new ZipError("too-large");
    const crc = await crc32(entry.data);
    const local = new DataView(new ArrayBuffer(30));
    local.setUint32(0, 0x04034b50, true);
    local.setUint16(4, 20, true);
    local.setUint16(6, 0x800, true);
    local.setUint16(8, 0, true);
    local.setUint32(14, crc, true);
    local.setUint32(18, size, true);
    local.setUint32(22, size, true);
    local.setUint16(26, name.length, true);
    parts.push(local.buffer, name, entry.data);
    const record = new DataView(new ArrayBuffer(46));
    record.setUint32(0, 0x02014b50, true);
    record.setUint16(4, 20, true);
    record.setUint16(6, 20, true);
    record.setUint16(8, 0x800, true);
    record.setUint16(10, 0, true);
    record.setUint32(16, crc, true);
    record.setUint32(20, size, true);
    record.setUint32(24, size, true);
    record.setUint16(28, name.length, true);
    record.setUint32(42, offset, true);
    central.push(record.buffer, name);
    offset += 30 + name.length + size;
  }
  const centralSize = central.reduce((total, part) => total + (part.byteLength ?? part.length), 0);
  const end = new DataView(new ArrayBuffer(22));
  end.setUint32(0, 0x06054b50, true);
  end.setUint16(8, entries.length, true);
  end.setUint16(10, entries.length, true);
  end.setUint32(12, centralSize, true);
  end.setUint32(16, offset, true);
  return new Blob([...parts, ...central, end.buffer], { type: "application/zip" });
}
