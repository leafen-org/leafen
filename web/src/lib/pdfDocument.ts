/**
 * Reads the objects of a PDF file for text import: every indirect object,
 * including those packed in object streams, with its stream data decoded
 * (Flate, ASCII hex and ASCII85). It does not trust the cross-reference
 * table; objects are found by scanning, and a later definition of an object
 * replaces an earlier one, as incremental updates do. Sizes, counts and
 * nesting are bounded so a hostile file cannot exhaust the page. A file
 * encrypted with an empty user password is read through its decryptor.
 */

import { createPdfStreamDecryptor, type PdfCryptMethod, type PdfStreamDecryptor } from "./pdfSecurity";

export class PdfReadError extends Error {
  constructor(readonly reason: "encrypted" | "malformed" | "too_large" | "no_text") {
    super(`PDF import failed: ${reason}`);
  }
}

export class PdfName {
  constructor(readonly name: string) {}
}

class PdfRef {
  constructor(readonly num: number, readonly gen: number) {}
}

export class PdfString {
  constructor(readonly bytes: Uint8Array) {}
}

export class PdfOperator {
  constructor(readonly op: string) {}
}

export type PdfDict = Map<string, PdfValue>;
export type PdfStream = { dict: PdfDict; raw: Uint8Array; ref?: { num: number; gen: number } };
export type PdfValue =
  | number | boolean | null | PdfName | PdfRef | PdfString | PdfOperator | PdfValue[] | PdfDict | PdfStream;

const MAX_OBJECTS = 200_000;
const MAX_NESTING = 64;
const MAX_STREAM_BYTES = 32 * 1024 * 1024;
const MAX_DECODED_TOTAL_BYTES = 128 * 1024 * 1024;

const WHITESPACE = new Set([0, 9, 10, 12, 13, 32]);
const DELIMITERS = new Set([40, 41, 60, 62, 91, 93, 123, 125, 47, 37]);

export function isPdfStream(value: PdfValue | undefined): value is PdfStream {
  return typeof value === "object" && value !== null && !(value instanceof Map) && !Array.isArray(value)
    && "raw" in value && "dict" in value;
}

export function pdfName(value: PdfValue | undefined) {
  return value instanceof PdfName ? value.name : "";
}

/** A tokenizer over PDF bytes that reads objects and content operators. */
export class PdfLexer {
  position = 0;

  constructor(readonly bytes: Uint8Array, start = 0) {
    this.position = start;
  }

  private skipSpace() {
    const { bytes } = this;
    while (this.position < bytes.length) {
      const byte = bytes[this.position]!;
      if (WHITESPACE.has(byte)) this.position += 1;
      else if (byte === 37) {
        while (this.position < bytes.length && bytes[this.position] !== 10 && bytes[this.position] !== 13) this.position += 1;
      } else break;
    }
  }

  private regular() {
    const start = this.position;
    while (this.position < this.bytes.length) {
      const byte = this.bytes[this.position]!;
      if (WHITESPACE.has(byte) || DELIMITERS.has(byte)) break;
      this.position += 1;
    }
    return latin1(this.bytes.subarray(start, this.position));
  }

  atEnd() {
    this.skipSpace();
    return this.position >= this.bytes.length;
  }

  /** Reads the next value; content-stream operators come back as PdfOperator. */
  read(depth = 0): PdfValue | undefined {
    if (depth > MAX_NESTING) throw new PdfReadError("malformed");
    this.skipSpace();
    const { bytes } = this;
    if (this.position >= bytes.length) return undefined;
    const byte = bytes[this.position]!;
    if (byte === 60 && bytes[this.position + 1] === 60) {
      this.position += 2;
      const dict: PdfDict = new Map();
      for (;;) {
        this.skipSpace();
        if (this.position >= bytes.length) return dict;
        if (bytes[this.position] === 62 && bytes[this.position + 1] === 62) {
          this.position += 2;
          return dict;
        }
        const key = this.read(depth + 1);
        if (!(key instanceof PdfName)) {
          if (key === undefined) return dict;
          continue;
        }
        const value = this.read(depth + 1);
        if (value === undefined) return dict;
        dict.set(key.name, value);
      }
    }
    if (byte === 60) return this.hexString();
    if (byte === 40) return this.literalString();
    if (byte === 91) {
      this.position += 1;
      const items: PdfValue[] = [];
      for (;;) {
        this.skipSpace();
        if (this.position >= bytes.length) return items;
        if (bytes[this.position] === 93) {
          this.position += 1;
          return items;
        }
        const item = this.read(depth + 1);
        if (item === undefined) return items;
        items.push(item);
      }
    }
    if (byte === 47) {
      this.position += 1;
      return new PdfName(this.regular().replace(/#([0-9a-fA-F]{2})/g, (_, hex: string) => String.fromCharCode(parseInt(hex, 16))));
    }
    if (byte === 93 || byte === 62 || byte === 41 || byte === 123 || byte === 125) {
      this.position += 1;
      return new PdfOperator(String.fromCharCode(byte));
    }
    const word = this.regular();
    if (/^[+-]?(\d+\.?\d*|\.\d+)$/.test(word)) {
      const number = Number(word);
      if (/^\d+$/.test(word)) {
        const saved = this.position;
        this.skipSpace();
        const generation = this.regular();
        this.skipSpace();
        if (/^\d+$/.test(generation) && this.bytes[this.position] === 82) {
          const after = this.bytes[this.position + 1];
          if (after === undefined || WHITESPACE.has(after) || DELIMITERS.has(after)) {
            this.position += 1;
            return new PdfRef(number, Number(generation));
          }
        }
        this.position = saved;
      }
      return number;
    }
    if (word === "true") return true;
    if (word === "false") return false;
    if (word === "null") return null;
    if (!word) {
      this.position += 1;
      return new PdfOperator("");
    }
    return new PdfOperator(word);
  }

  private hexString() {
    this.position += 1;
    const digits: number[] = [];
    while (this.position < this.bytes.length && this.bytes[this.position] !== 62) {
      const byte = this.bytes[this.position]!;
      const value = byte >= 48 && byte <= 57 ? byte - 48 : byte >= 65 && byte <= 70 ? byte - 55
        : byte >= 97 && byte <= 102 ? byte - 87 : -1;
      if (value >= 0) digits.push(value);
      this.position += 1;
    }
    this.position += 1;
    if (digits.length % 2) digits.push(0);
    const out = new Uint8Array(digits.length / 2);
    for (let index = 0; index < out.length; index += 1) out[index] = digits[index * 2]! * 16 + digits[index * 2 + 1]!;
    return new PdfString(out);
  }

  private literalString() {
    this.position += 1;
    const out: number[] = [];
    let nesting = 1;
    const { bytes } = this;
    while (this.position < bytes.length) {
      const byte = bytes[this.position++]!;
      if (byte === 92) {
        const next = bytes[this.position++];
        if (next === undefined) break;
        const escaped: Record<number, number> = { 110: 10, 114: 13, 116: 9, 98: 8, 102: 12, 40: 40, 41: 41, 92: 92 };
        if (next in escaped) out.push(escaped[next]!);
        else if (next >= 48 && next <= 55) {
          let value = next - 48;
          for (let count = 0; count < 2 && bytes[this.position]! >= 48 && bytes[this.position]! <= 55; count += 1) {
            value = value * 8 + bytes[this.position++]! - 48;
          }
          out.push(value & 255);
        } else if (next === 13) {
          if (bytes[this.position] === 10) this.position += 1;
        } else if (next !== 10) out.push(next);
        continue;
      }
      if (byte === 40) nesting += 1;
      if (byte === 41 && --nesting === 0) break;
      out.push(byte);
    }
    return new PdfString(Uint8Array.from(out));
  }
}

function latin1(bytes: Uint8Array) {
  return new TextDecoder("latin1").decode(bytes);
}

async function inflate(raw: Uint8Array, budget: { left: number }) {
  const chunks: Uint8Array[] = [];
  let size = 0;
  const attempt = async (format: CompressionFormat, input: Uint8Array) => {
    const reader = new Blob([input as BlobPart]).stream().pipeThrough(new DecompressionStream(format)).getReader();
    try {
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        size += value.length;
        if (size > MAX_STREAM_BYTES || size > budget.left) throw new PdfReadError("too_large");
        chunks.push(value);
      }
    } catch (error) {
      // A damaged tail still leaves the text decoded before it.
      if (error instanceof PdfReadError) throw error;
    }
  };
  await attempt("deflate", raw);
  if (size === 0 && raw.length > 2) await attempt("deflate-raw", raw.subarray(2));
  budget.left -= size;
  const out = new Uint8Array(size);
  let offset = 0;
  for (const chunk of chunks) {
    out.set(chunk, offset);
    offset += chunk.length;
  }
  return out;
}

function asciiHex(raw: Uint8Array) {
  const text = latin1(raw).replace(/>.*$/s, "").replace(/[^0-9a-fA-F]/g, "");
  const padded = text.length % 2 ? `${text}0` : text;
  const out = new Uint8Array(padded.length / 2);
  for (let index = 0; index < out.length; index += 1) out[index] = parseInt(padded.slice(index * 2, index * 2 + 2), 16);
  return out;
}

function ascii85(raw: Uint8Array) {
  const text = latin1(raw).replace(/^<~/, "").replace(/~>.*$/s, "").replace(/\s/g, "");
  const out: number[] = [];
  let group: number[] = [];
  for (const char of text) {
    if (char === "z" && group.length === 0) {
      out.push(0, 0, 0, 0);
      continue;
    }
    const value = char.charCodeAt(0) - 33;
    if (value < 0 || value > 84) continue;
    group.push(value);
    if (group.length === 5) {
      let number = 0;
      for (const digit of group) number = number * 85 + digit;
      out.push((number >>> 24) & 255, (number >>> 16) & 255, (number >>> 8) & 255, number & 255);
      group = [];
    }
  }
  if (group.length > 1) {
    const count = group.length - 1;
    while (group.length < 5) group.push(84);
    let number = 0;
    for (const digit of group) number = number * 85 + digit;
    out.push(...[(number >>> 24) & 255, (number >>> 16) & 255, (number >>> 8) & 255, number & 255].slice(0, count));
  }
  return Uint8Array.from(out);
}

/** The objects of one PDF file. */
export class PdfDocument {
  private readonly objects = new Map<number, PdfValue>();
  private readonly budget = { left: MAX_DECODED_TOTAL_BYTES };
  private decryptor: PdfStreamDecryptor | null = null;
  trailer: PdfDict = new Map();

  private constructor(private readonly bytes: Uint8Array) {}

  static async open(bytes: Uint8Array) {
    const document = new PdfDocument(bytes);
    if (!latin1(bytes.subarray(0, 1024)).includes("%PDF-")) throw new PdfReadError("malformed");
    await document.scan();
    return document;
  }

  private async scan() {
    const text = latin1(this.bytes);
    const pattern = /(\d+)\s+(\d+)\s+obj\b/g;
    const packed: PdfStream[] = [];
    for (let match = pattern.exec(text); match; match = pattern.exec(text)) {
      if (this.objects.size >= MAX_OBJECTS) throw new PdfReadError("too_large");
      const lexer = new PdfLexer(this.bytes, match.index + match[0].length);
      let value: PdfValue | undefined;
      try {
        value = lexer.read();
      } catch {
        continue;
      }
      if (value instanceof Map) {
        const stream = this.streamAfter(value, lexer.position, text);
        if (stream) {
          stream.ref = { num: Number(match[1]), gen: Number(match[2]) };
          value = stream;
          pattern.lastIndex = Math.max(pattern.lastIndex, lexer.position + stream.raw.length);
          if (pdfName(stream.dict.get("Type")) === "ObjStm") packed.push(stream);
          if (pdfName(stream.dict.get("Type")) === "XRef") this.trailer = stream.dict;
        }
      }
      if (value !== undefined) this.objects.set(Number(match[1]), value);
    }
    const trailerAt = text.lastIndexOf("trailer");
    if (trailerAt >= 0) {
      const trailer = new PdfLexer(this.bytes, trailerAt + 7).read();
      if (trailer instanceof Map) this.trailer = trailer;
    }
    if (this.trailer.has("Encrypt")) await this.openEncryption();
    for (const stream of packed) await this.unpack(stream);
  }

  /** Reads the Standard security handler with an empty user password. */
  private async openEncryption() {
    const encrypt = this.dict(this.trailer.get("Encrypt"));
    const ids = this.resolve(this.trailer.get("ID"));
    const bytes = (value: PdfValue | undefined) => {
      const resolved = this.resolve(value);
      return resolved instanceof PdfString ? resolved.bytes : new Uint8Array();
    };
    const number = (value: PdfValue | undefined, fallback: number) => {
      const resolved = this.resolve(value);
      return typeof resolved === "number" ? resolved : fallback;
    };
    if (!encrypt || pdfName(this.resolve(encrypt.get("Filter"))) !== "Standard") throw new PdfReadError("encrypted");
    const version = number(encrypt.get("V"), 0);
    const method = (): PdfCryptMethod => {
      if (version === 5) return "aes256";
      if (version !== 4) return "rc4";
      const name = pdfName(this.resolve(encrypt.get("StmF"))) || "Identity";
      if (name === "Identity") return "none";
      const filter = pdfName(this.resolve(this.dict(this.dict(encrypt.get("CF"))?.get(name))?.get("CFM")));
      return filter === "AESV2" ? "aes128" : filter === "AESV3" ? "aes256" : filter === "None" ? "none" : "rc4";
    };
    this.decryptor = await createPdfStreamDecryptor({
      version,
      revision: number(encrypt.get("R"), 0),
      keyBits: number(encrypt.get("Length"), version >= 4 ? 128 : 40),
      owner: bytes(encrypt.get("O")),
      user: bytes(encrypt.get("U")),
      userEncryptedKey: bytes(encrypt.get("UE")),
      permissions: number(encrypt.get("P"), 0),
      encryptMetadata: this.resolve(encrypt.get("EncryptMetadata")) !== false,
      streamMethod: method(),
      firstId: Array.isArray(ids) ? bytes(ids[0]) : new Uint8Array(),
    });
    if (!this.decryptor) throw new PdfReadError("encrypted");
  }

  private streamAfter(dict: PdfDict, position: number, text: string): PdfStream | null {
    const keyword = /^\s*stream(\r\n|\n|\r)/.exec(text.slice(position, position + 32));
    if (!keyword) return null;
    const start = position + keyword[0].length;
    const declared = dict.get("Length");
    let end = -1;
    if (typeof declared === "number" && declared >= 0 && /^\s*endstream/.test(text.slice(start + declared, start + declared + 32))) {
      end = start + declared;
    } else {
      end = text.indexOf("endstream", start);
      if (end < 0) return null;
      while (end > start && (this.bytes[end - 1] === 10 || this.bytes[end - 1] === 13)) end -= 1;
    }
    if (end - start > MAX_STREAM_BYTES) throw new PdfReadError("too_large");
    return { dict, raw: this.bytes.subarray(start, end) };
  }

  private async unpack(stream: PdfStream) {
    const data = await this.decode(stream);
    const count = this.resolve(stream.dict.get("N"));
    const first = this.resolve(stream.dict.get("First"));
    if (typeof count !== "number" || typeof first !== "number") return;
    const header = new PdfLexer(data);
    const entries: Array<[number, number]> = [];
    for (let index = 0; index < count && index < MAX_OBJECTS; index += 1) {
      const num = header.read();
      const offset = header.read();
      if (typeof num !== "number" || typeof offset !== "number") break;
      entries.push([num, offset]);
    }
    for (const [num, offset] of entries) {
      if (this.objects.has(num)) continue;
      if (this.objects.size >= MAX_OBJECTS) throw new PdfReadError("too_large");
      try {
        const value = new PdfLexer(data, first + offset).read();
        if (value !== undefined) this.objects.set(num, value);
      } catch {
        // A damaged packed object is left out.
      }
    }
  }

  resolve(value: PdfValue | undefined, depth = 0): PdfValue | undefined {
    let current = value;
    for (let hops = depth; current instanceof PdfRef; hops += 1) {
      if (hops > MAX_NESTING) return undefined;
      current = this.objects.get(current.num);
    }
    return current;
  }

  dict(value: PdfValue | undefined): PdfDict | undefined {
    const resolved = this.resolve(value);
    if (resolved instanceof Map) return resolved;
    return isPdfStream(resolved) ? resolved.dict : undefined;
  }

  /** The indirect objects whose dictionary has the given /Type, in object number order. */
  objectsOfType(type: string) {
    return [...this.objects.entries()].sort(([left], [right]) => left - right)
      .map(([, value]) => value).filter((value) => {
        const dict = value instanceof Map ? value : isPdfStream(value) ? value.dict : undefined;
        return pdfName(dict?.get("Type")) === type;
      });
  }

  /** The stream's data with its filters undone; unsupported filters give no data. */
  async decode(stream: PdfStream) {
    const filterValue = this.resolve(stream.dict.get("Filter"));
    const filters = (Array.isArray(filterValue) ? filterValue : filterValue ? [filterValue] : [])
      .map((filter) => pdfName(this.resolve(filter)));
    let data = stream.raw;
    if (this.decryptor && stream.ref && pdfName(stream.dict.get("Type")) !== "XRef") {
      data = await this.decryptor.decrypt(data, stream.ref.num, stream.ref.gen);
    }
    for (const filter of filters) {
      if (filter === "Crypt") continue;
      if (filter === "FlateDecode" || filter === "Fl") data = await inflate(data, this.budget);
      else if (filter === "ASCIIHexDecode" || filter === "AHx") data = asciiHex(data);
      else if (filter === "ASCII85Decode" || filter === "A85") data = ascii85(data);
      else return new Uint8Array();
    }
    return data;
  }
}
