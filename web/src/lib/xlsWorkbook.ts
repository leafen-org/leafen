/**
 * Reads legacy Excel workbooks (.xls): the OLE compound file that holds
 * them ([MS-CFB]) and the BIFF8 records of their Workbook stream ([MS-XLS]).
 * Each worksheet's cells come back as rows of text: shared, inline and
 * formula strings, numbers (dates as ISO dates by their cell format),
 * booleans and errors. Encrypted workbooks and older BIFF versions are
 * refused. Sector chains, stream sizes and cell ranges are bounded so a
 * damaged file cannot loop or expand without limit.
 */

export class XlsReadError extends Error {}

const END_OF_CHAIN = 0xfffffffe;
const MAX_STREAM_BYTES = 64 * 1024 * 1024;
const BUILTIN_DATE_FORMATS = new Set([14, 15, 16, 17, 18, 19, 20, 21, 22, 45, 46, 47]);

function compoundStream(bytes: Uint8Array, names: readonly string[]) {
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  const signature = [0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1];
  if (bytes.length < 512 || signature.some((byte, index) => bytes[index] !== byte)) {
    throw new XlsReadError("The file is not an Excel workbook.");
  }
  const sectorSize = 1 << view.getUint16(0x1e, true);
  const miniSectorSize = 1 << view.getUint16(0x20, true);
  const miniCutoff = view.getUint32(0x38, true);
  const sectorCount = Math.floor((bytes.length - sectorSize) / sectorSize) + 1;
  const sectorAt = (sector: number) => {
    const offset = (sector + 1) * sectorSize;
    if (sector >= sectorCount || offset >= bytes.length) throw new XlsReadError("The workbook is damaged.");
    return bytes.subarray(offset, Math.min(offset + sectorSize, bytes.length));
  };
  // The FAT's sectors: 109 listed in the header, the rest in DIFAT sectors.
  const fatSectors: number[] = [];
  for (let index = 0; index < 109; index += 1) {
    const sector = view.getUint32(0x4c + index * 4, true);
    if (sector < END_OF_CHAIN) fatSectors.push(sector);
  }
  let difat = view.getUint32(0x44, true);
  for (let guard = 0; difat < END_OF_CHAIN && guard < sectorCount; guard += 1) {
    const block = sectorAt(difat);
    const blockView = new DataView(block.buffer, block.byteOffset, block.byteLength);
    for (let index = 0; index < sectorSize / 4 - 1; index += 1) {
      const sector = blockView.getUint32(index * 4, true);
      if (sector < END_OF_CHAIN) fatSectors.push(sector);
    }
    difat = blockView.getUint32(sectorSize - 4, true);
  }
  const fat: number[] = [];
  for (const sector of fatSectors) {
    const block = sectorAt(sector);
    const blockView = new DataView(block.buffer, block.byteOffset, block.byteLength);
    for (let index = 0; index + 4 <= block.length; index += 4) fat.push(blockView.getUint32(index, true));
  }
  const chain = (start: number, table: readonly number[], read: (sector: number) => Uint8Array, limit: number) => {
    const parts: Uint8Array[] = [];
    let size = 0;
    for (let sector = start, steps = 0; sector < END_OF_CHAIN; sector = table[sector] ?? END_OF_CHAIN, steps += 1) {
      if (steps > table.length) throw new XlsReadError("The workbook is damaged.");
      const part = read(sector);
      parts.push(part);
      size += part.length;
      if (size > limit) break;
    }
    const out = new Uint8Array(size);
    let offset = 0;
    for (const part of parts) {
      out.set(part, offset);
      offset += part.length;
    }
    return out;
  };
  const directory = chain(view.getUint32(0x30, true), fat, sectorAt, MAX_STREAM_BYTES);
  const entries = [];
  for (let offset = 0; offset + 128 <= directory.length; offset += 128) {
    const entryView = new DataView(directory.buffer, directory.byteOffset + offset, 128);
    const nameLength = Math.min(entryView.getUint16(0x40, true), 64);
    const name = String.fromCharCode(...Array.from({ length: Math.max(0, nameLength / 2 - 1) },
      (_, index) => entryView.getUint16(index * 2, true)));
    entries.push({ name, type: entryView.getUint8(0x42), start: entryView.getUint32(0x74, true), size: entryView.getUint32(0x78, true) });
  }
  const root = entries.find((entry) => entry.type === 5);
  const stream = entries.find((entry) => entry.type === 2 && names.includes(entry.name));
  if (!root || !stream) throw new XlsReadError("The workbook has no Workbook stream.");
  if (stream.size > MAX_STREAM_BYTES) throw new XlsReadError("The workbook is too large.");
  if (stream.size >= miniCutoff) return chain(stream.start, fat, sectorAt, stream.size).subarray(0, stream.size);
  // A small stream lives in the mini stream, in 64-byte sectors.
  const miniStream = chain(root.start, fat, sectorAt, MAX_STREAM_BYTES);
  const miniFat: number[] = [];
  const miniFatBytes = chain(view.getUint32(0x3c, true), fat, sectorAt, MAX_STREAM_BYTES);
  const miniFatView = new DataView(miniFatBytes.buffer, miniFatBytes.byteOffset, miniFatBytes.byteLength);
  for (let index = 0; index + 4 <= miniFatBytes.length; index += 4) miniFat.push(miniFatView.getUint32(index, true));
  const miniSector = (sector: number) => {
    const offset = sector * miniSectorSize;
    if (offset + miniSectorSize > miniStream.length) throw new XlsReadError("The workbook is damaged.");
    return miniStream.subarray(offset, offset + miniSectorSize);
  };
  return chain(stream.start, miniFat, miniSector, stream.size).subarray(0, stream.size);
}

type XlsRecord = { type: number; data: Uint8Array; continues: Uint8Array[] };

function records(stream: Uint8Array, start: number) {
  const view = new DataView(stream.buffer, stream.byteOffset, stream.byteLength);
  const result: XlsRecord[] = [];
  for (let offset = start; offset + 4 <= stream.length;) {
    const type = view.getUint16(offset, true);
    const length = view.getUint16(offset + 2, true);
    const data = stream.subarray(offset + 4, Math.min(offset + 4 + length, stream.length));
    offset += 4 + length;
    if (type === 0x003c && result.length > 0) result.at(-1)!.continues.push(data);
    else result.push({ type, data, continues: [] });
    if (type === 0x000a) break;
  }
  return result;
}

/** A reader over a record's data and its CONTINUE records, for strings that span them. */
class RecordReader {
  private part = 0;
  private offset = 0;
  constructor(private readonly parts: Uint8Array[]) {}

  private current() {
    while (this.part < this.parts.length && this.offset >= this.parts[this.part]!.length) {
      this.part += 1;
      this.offset = 0;
    }
    return this.parts[this.part];
  }
  atEnd() {
    return !this.current();
  }
  byte() {
    const part = this.current();
    if (!part) throw new XlsReadError("The workbook is damaged.");
    return part[this.offset++]!;
  }
  uint16() {
    return this.byte() | (this.byte() << 8);
  }
  uint32() {
    return (this.uint16() | (this.uint16() << 16)) >>> 0;
  }
  skip(count: number) {
    for (let index = 0; index < count; index += 1) this.byte();
  }
  /** Characters of a string; a CONTINUE boundary restarts with its own width flag. */
  characters(count: number, highByte: boolean) {
    let text = "";
    let wide = highByte;
    for (let index = 0; index < count; index += 1) {
      if (this.offset >= (this.parts[this.part]?.length ?? 0) && this.part + 1 < this.parts.length) {
        this.part += 1;
        this.offset = 0;
        wide = (this.byte() & 1) === 1;
      }
      text += String.fromCharCode(wide ? this.uint16() : this.byte());
    }
    return text;
  }
  unicodeString(shortLength = false) {
    const count = shortLength ? this.byte() : this.uint16();
    const flags = this.byte();
    const runs = flags & 0x08 ? this.uint16() : 0;
    const extended = flags & 0x04 ? this.uint32() : 0;
    const text = this.characters(count, (flags & 1) === 1);
    this.skip(runs * 4 + extended);
    return text;
  }
}

function numberAt(data: Uint8Array, offset: number) {
  return new DataView(data.buffer, data.byteOffset, data.byteLength).getFloat64(offset, true);
}

function rkNumber(value: number) {
  let number: number;
  if (value & 0x02) number = value >> 2;
  else {
    const buffer = new DataView(new ArrayBuffer(8));
    buffer.setUint32(4, (value & 0xfffffffc) >>> 0, true);
    number = buffer.getFloat64(0, true);
  }
  return value & 0x01 ? number / 100 : number;
}

function serialDate(serial: number, from1904: boolean) {
  const days = serial + (from1904 ? 1462 : 0);
  const date = new Date(Math.round((days - 25569) * 86_400_000));
  if (Number.isNaN(date.getTime())) return String(serial);
  const iso = date.toISOString();
  return days % 1 === 0 ? iso.slice(0, 10) : iso.slice(0, 16).replace("T", " ");
}

export type XlsSheet = { name: string; rows: string[][] };

/** Reads an .xls workbook's worksheets and their cells. */
export function readXlsWorkbook(bytes: Uint8Array): XlsSheet[] {
  const stream = compoundStream(bytes, ["Workbook", "Book"]);
  const globals = records(stream, 0);
  const bof = globals[0];
  if (!bof || bof.type !== 0x0809 || new DataView(bof.data.buffer, bof.data.byteOffset).getUint16(0, true) !== 0x0600) {
    throw new XlsReadError("Only Excel 97 and later workbooks are supported.");
  }
  if (globals.some((record) => record.type === 0x002f)) throw new XlsReadError("The workbook is encrypted.");
  let from1904 = false;
  const strings: string[] = [];
  const formats = new Map<number, string>();
  const cellFormats: number[] = [];
  const sheets: Array<{ name: string; offset: number }> = [];
  for (const record of globals) {
    const reader = new RecordReader([record.data, ...record.continues]);
    if (record.type === 0x0022) from1904 = reader.uint16() === 1;
    else if (record.type === 0x041e) formats.set(reader.uint16(), reader.unicodeString());
    else if (record.type === 0x00e0) {
      reader.uint16();
      cellFormats.push(reader.uint16());
    } else if (record.type === 0x0085) {
      const offset = reader.uint32();
      reader.byte();
      const kind = reader.byte();
      const name = reader.unicodeString(true);
      if (kind === 0) sheets.push({ name, offset });
    } else if (record.type === 0x00fc) {
      reader.uint32();
      const unique = reader.uint32();
      for (let index = 0; index < unique && !reader.atEnd(); index += 1) strings.push(reader.unicodeString());
    }
  }
  const isDate = (xf: number) => {
    const format = cellFormats[xf];
    if (format === undefined) return false;
    const code = (formats.get(format) ?? "").replace(/"[^"]*"|\[[^\]]*\]|\\./g, "");
    return BUILTIN_DATE_FORMATS.has(format) || /[dy]/i.test(code) || /h.*m|m.*s/i.test(code);
  };
  const numberText = (value: number, xf: number) => (isDate(xf) ? serialDate(value, from1904) : String(value));
  return sheets.map(({ name, offset }) => {
    const rows: string[][] = [];
    // Only cells with something in them, so a row ends at its last value.
    const put = (row: number, column: number, value: string) => {
      if (!value) return;
      while (rows.length <= row) rows.push([]);
      rows[row]![column] = value;
    };
    let pendingFormula: { row: number; column: number } | null = null;
    for (const record of records(stream, offset).slice(1)) {
      const { data } = record;
      const view = new DataView(data.buffer, data.byteOffset, data.byteLength);
      if (data.length < 6 && record.type !== 0x0207) continue;
      const row = data.length >= 2 ? view.getUint16(0, true) : 0;
      const column = data.length >= 4 ? view.getUint16(2, true) : 0;
      const xf = data.length >= 6 ? view.getUint16(4, true) : 0;
      switch (record.type) {
        case 0x00fd: put(row, column, strings[view.getUint32(6, true)] ?? ""); break;
        case 0x0204: {
          const reader = new RecordReader([data.subarray(6), ...record.continues]);
          put(row, column, reader.unicodeString());
          break;
        }
        case 0x0203: put(row, column, numberText(numberAt(data, 6), xf)); break;
        case 0x027e: put(row, column, numberText(rkNumber(view.getInt32(6, true)), xf)); break;
        case 0x00bd: {
          const last = view.getUint16(data.length - 2, true);
          for (let index = 0; column + index <= last && 4 + index * 6 + 6 <= data.length - 2; index += 1) {
            const cellXf = view.getUint16(4 + index * 6, true);
            put(row, column + index, numberText(rkNumber(view.getInt32(6 + index * 6, true)), cellXf));
          }
          break;
        }
        case 0x0205: {
          const value = data[6]!;
          put(row, column, data[7] ? "#ERROR" : value ? "TRUE" : "FALSE");
          break;
        }
        case 0x0006: {
          pendingFormula = null;
          if (data[12] === 0xff && data[13] === 0xff) {
            const kind = data[6];
            if (kind === 0) pendingFormula = { row, column };
            else if (kind === 1) put(row, column, data[8] ? "TRUE" : "FALSE");
            else if (kind === 2) put(row, column, "#ERROR");
            else put(row, column, "");
          } else put(row, column, numberText(numberAt(data, 6), xf));
          break;
        }
        case 0x0207:
          if (pendingFormula) {
            const reader = new RecordReader([data, ...record.continues]);
            put(pendingFormula.row, pendingFormula.column, reader.unicodeString());
            pendingFormula = null;
          }
          break;
        default: break;
      }
    }
    return { name, rows: rows.map((cells) => Array.from(cells, (cell) => cell ?? "")) };
  });
}
