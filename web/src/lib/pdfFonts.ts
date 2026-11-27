import {
  isPdfStream,
  PdfDocument,
  PdfLexer,
  PdfName,
  PdfOperator,
  PdfString,
  pdfName,
  type PdfDict,
  type PdfValue,
} from "./pdfDocument";

/**
 * Turns the bytes a PDF font shows into text and glyph widths: through the
 * font's ToUnicode map when it has one, else its encoding (WinAnsi, MacRoman
 * or Standard, with /Differences glyph names), and for composite fonts the
 * UCS-2 CMaps. Glyph widths come from /Widths or /W in 1/1000 of the font
 * size, so the reader can tell a gap between words from the next letter.
 */

export type PdfGlyph = { text: string; width: number; space: boolean };

export type PdfFont = {
  decode(bytes: Uint8Array): PdfGlyph[];
};

const MAX_CMAP_ENTRIES = 200_000;

const ACCENTS: Record<string, string> = {
  acute: "́", grave: "̀", circumflex: "̂", dieresis: "̈", tilde: "̃", ring: "̊",
  cedilla: "̧", caron: "̌", macron: "̄", breve: "̆", dotaccent: "̇", ogonek: "̨",
  hungarumlaut: "̋",
};

const GLYPHS: Record<string, string> = {
  space: " ", exclam: "!", quotedbl: "\"", numbersign: "#", dollar: "$", percent: "%", ampersand: "&",
  quotesingle: "'", quoteright: "’", quoteleft: "‘", parenleft: "(", parenright: ")", asterisk: "*",
  plus: "+", comma: ",", hyphen: "-", period: ".", slash: "/", colon: ":", semicolon: ";", less: "<", equal: "=",
  greater: ">", question: "?", at: "@", bracketleft: "[", backslash: "\\", bracketright: "]", asciicircum: "^",
  underscore: "_", grave: "`", braceleft: "{", bar: "|", braceright: "}", asciitilde: "~", bullet: "•",
  endash: "–", emdash: "—", quotedblleft: "“", quotedblright: "”", quotesinglbase: "‚",
  quotedblbase: "„", ellipsis: "…", dagger: "†", daggerdbl: "‡", trademark: "™",
  copyright: "©", registered: "®", degree: "°", minus: "−", multiply: "×",
  divide: "÷", section: "§", paragraph: "¶", periodcentered: "·", germandbls: "ß",
  ae: "æ", AE: "Æ", oe: "œ", OE: "Œ", oslash: "ø", Oslash: "Ø", eth: "ð",
  Eth: "Ð", thorn: "þ", Thorn: "Þ", dotlessi: "ı", lslash: "ł", Lslash: "Ł",
  fi: "fi", fl: "fl", ff: "ff", ffi: "ffi", ffl: "ffl", nbspace: " ", sfthyphen: "-", exclamdown: "¡",
  questiondown: "¿", guillemotleft: "«", guillemotright: "»", guilsinglleft: "‹",
  guilsinglright: "›", cent: "¢", sterling: "£", yen: "¥", Euro: "€", euro: "€",
  florin: "ƒ", perthousand: "‰", plusminus: "±", mu: "µ", onehalf: "½",
  onequarter: "¼", threequarters: "¾", ordfeminine: "ª", ordmasculine: "º",
};
const DIGITS = ["zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine"];

/** The Unicode text of an Adobe glyph name, or "" when it names no character. */
function glyphNameText(raw: string): string {
  const name = raw.split(".")[0] ?? "";
  if (!name) return "";
  if (name.includes("_")) return name.split("_").map(glyphNameText).join("");
  if (GLYPHS[name] !== undefined) return GLYPHS[name]!;
  const digit = DIGITS.indexOf(name);
  if (digit >= 0) return String(digit);
  if (/^[A-Za-z]$/.test(name)) return name;
  const uni = /^uni((?:[0-9A-F]{4})+)$/.exec(name);
  if (uni) return String.fromCharCode(...(uni[1]!.match(/.{4}/g) ?? []).map((hex) => parseInt(hex, 16)));
  const u = /^u([0-9A-F]{4,6})$/.exec(name);
  if (u) return String.fromCodePoint(parseInt(u[1]!, 16));
  const accented = /^([A-Za-z])(acute|grave|circumflex|dieresis|tilde|ring|cedilla|caron|macron|breve|dotaccent|ogonek|hungarumlaut)$/.exec(name);
  if (accented) return `${accented[1]}${ACCENTS[accented[2]!]}`.normalize("NFC");
  return "";
}

function singleByteTable(encoding: string) {
  const label = encoding === "MacRomanEncoding" ? "macintosh" : "windows-1252";
  let decoder: TextDecoder;
  try {
    decoder = new TextDecoder(label);
  } catch {
    decoder = new TextDecoder("windows-1252");
  }
  const table: string[] = [];
  for (let code = 0; code < 256; code += 1) table.push(code < 32 ? "" : decoder.decode(Uint8Array.of(code)));
  if (encoding === "StandardEncoding") {
    table[0x27] = "’";
    table[0x60] = "‘";
  }
  return table;
}

function utf16(bytes: Uint8Array) {
  let text = "";
  for (let index = 0; index + 1 < bytes.length; index += 2) text += String.fromCharCode((bytes[index]! << 8) | bytes[index + 1]!);
  return text;
}

function codeKey(bytes: Uint8Array) {
  let key = 0;
  for (const byte of bytes) key = key * 256 + byte;
  return `${bytes.length}:${key}`;
}

type ToUnicode = { map: Map<string, string>; lengths: number[] };

/** Reads a ToUnicode CMap's code space lengths and code-to-text entries. */
function parseToUnicode(data: Uint8Array): ToUnicode {
  const lexer = new PdfLexer(data);
  const map = new Map<string, string>();
  const lengths = new Set<number>();
  const operands: PdfValue[] = [];
  let mode = "";
  while (!lexer.atEnd() && map.size < MAX_CMAP_ENTRIES) {
    const token = lexer.read();
    if (token === undefined) break;
    if (!(token instanceof PdfOperator)) {
      operands.push(token);
      continue;
    }
    if (token.op.startsWith("begin")) {
      mode = token.op;
      operands.length = 0;
      continue;
    }
    if (token.op === "endcodespacerange") {
      for (const value of operands) if (value instanceof PdfString && value.bytes.length) lengths.add(value.bytes.length);
    } else if (token.op === "endbfchar") {
      for (let index = 0; index + 1 < operands.length; index += 2) {
        const source = operands[index];
        const target = operands[index + 1];
        if (source instanceof PdfString && target instanceof PdfString) map.set(codeKey(source.bytes), utf16(target.bytes));
      }
    } else if (token.op === "endbfrange") {
      for (let index = 0; index + 2 < operands.length; index += 3) {
        const low = operands[index];
        const high = operands[index + 1];
        const target = operands[index + 2];
        if (!(low instanceof PdfString) || !(high instanceof PdfString)) continue;
        const from = Number.parseInt(codeKey(low.bytes).split(":")[1]!, 10);
        const to = Number.parseInt(codeKey(high.bytes).split(":")[1]!, 10);
        if (!(to >= from) || to - from > 65_535) continue;
        for (let code = from; code <= to && map.size < MAX_CMAP_ENTRIES; code += 1) {
          const key = `${low.bytes.length}:${code}`;
          if (target instanceof PdfString) {
            const bytes = Uint8Array.from(target.bytes);
            let carry = code - from;
            for (let byte = bytes.length - 1; byte >= 0 && carry > 0; byte -= 1) {
              const sum = bytes[byte]! + carry;
              bytes[byte] = sum & 255;
              carry = sum >> 8;
            }
            map.set(key, utf16(bytes));
          } else if (Array.isArray(target)) {
            const entry = target[code - from];
            if (entry instanceof PdfString) map.set(key, utf16(entry.bytes));
          }
        }
      }
    }
    if (!token.op.startsWith("begin")) {
      if (token.op.startsWith("end")) mode = "";
      if (!mode) operands.length = 0;
    }
  }
  return { map, lengths: [...lengths].sort((left, right) => left - right) };
}

function numbers(document: PdfDocument, value: PdfValue | undefined) {
  const resolved = document.resolve(value);
  return Array.isArray(resolved) ? resolved.map((item) => document.resolve(item)) : [];
}

/** Builds the decoder for one font dictionary. */
export async function loadPdfFont(document: PdfDocument, fontDict: PdfDict): Promise<PdfFont> {
  const subtype = pdfName(document.resolve(fontDict.get("Subtype")));
  const composite = subtype === "Type0";
  let toUnicode: ToUnicode | null = null;
  const unicodeStream = document.resolve(fontDict.get("ToUnicode"));
  if (isPdfStream(unicodeStream)) toUnicode = parseToUnicode(await document.decode(unicodeStream));
  const encodingValue = document.resolve(fontDict.get("Encoding"));
  const encodingName = encodingValue instanceof PdfName ? encodingValue.name
    : isPdfStream(encodingValue) ? pdfName(document.resolve(encodingValue.dict.get("CMapName"))) : "";

  if (composite) {
    const descendant = document.dict(numbers(document, fontDict.get("DescendantFonts"))[0]);
    const defaultWidth = document.resolve(descendant?.get("DW"));
    const widths = new Map<number, number>();
    const w = numbers(document, descendant?.get("W"));
    for (let index = 0; index < w.length && widths.size < MAX_CMAP_ENTRIES;) {
      const first = w[index];
      const next = w[index + 1];
      if (typeof first !== "number") break;
      if (Array.isArray(next)) {
        next.forEach((width, offset) => {
          const value = document.resolve(width);
          if (typeof value === "number") widths.set(first + offset, value);
        });
        index += 2;
      } else {
        const last = next;
        const width = w[index + 2];
        if (typeof last !== "number" || typeof width !== "number" || last - first > 65_535) break;
        for (let code = first; code <= last; code += 1) widths.set(code, width);
        index += 3;
      }
    }
    const ucs2 = /UCS2|UTF16/.test(encodingName);
    const lengths = toUnicode?.lengths.length ? toUnicode.lengths : [2];
    return {
      decode(bytes) {
        const glyphs: PdfGlyph[] = [];
        for (let index = 0; index < bytes.length;) {
          let length = lengths.find((candidate) => toUnicode?.map.has(codeKey(bytes.subarray(index, index + candidate))))
            ?? lengths[lengths.length - 1] ?? 2;
          length = Math.max(1, Math.min(length, bytes.length - index));
          const code = bytes.subarray(index, index + length);
          const text = toUnicode?.map.get(codeKey(code)) ?? (ucs2 ? utf16(code) : "");
          const cid = Number.parseInt(codeKey(code).split(":")[1]!, 10);
          glyphs.push({
            text, width: widths.get(cid) ?? (typeof defaultWidth === "number" ? defaultWidth : 1000),
            space: text === " ",
          });
          index += length;
        }
        return glyphs;
      },
    };
  }

  const baseName = encodingValue instanceof Map ? pdfName(document.resolve(encodingValue.get("BaseEncoding"))) : encodingName;
  // A standard Latin font named without an encoding uses its own, the
  // standard one, as Notion's import reads it (seen 2026-11-05: its ` and '
  // as ‘ and ’).
  const standardFont = subtype === "Type1"
    && /^(?:Courier|Helvetica|Times)/.test(pdfName(document.resolve(fontDict.get("BaseFont"))));
  const table = singleByteTable(baseName || (standardFont && !encodingValue ? "StandardEncoding" : "WinAnsiEncoding"));
  if (encodingValue instanceof Map) {
    let code = 0;
    for (const entry of numbers(document, encodingValue.get("Differences"))) {
      if (typeof entry === "number") code = entry;
      else if (entry instanceof PdfName && code >= 0 && code < 256) table[code++] = glyphNameText(entry.name);
    }
  }
  const firstChar = document.resolve(fontDict.get("FirstChar"));
  const widthList = numbers(document, fontDict.get("Widths"));
  const descriptor = document.dict(fontDict.get("FontDescriptor"));
  const missing = document.resolve(descriptor?.get("MissingWidth"));
  const matrix = numbers(document, fontDict.get("FontMatrix"));
  const unitsPerEm = subtype === "Type3" && typeof matrix[0] === "number" ? matrix[0] * 1000 : 1;
  const fallbackWidth = /Courier/.test(pdfName(document.resolve(fontDict.get("BaseFont")))) ? 600 : 500;
  return {
    decode(bytes) {
      return Array.from(bytes, (code) => {
        const mapped = toUnicode?.map.get(`1:${code}`);
        const text = mapped ?? table[code] ?? "";
        const listed = typeof firstChar === "number" ? widthList[code - firstChar] : undefined;
        const width = typeof listed === "number" ? listed * unitsPerEm
          : typeof missing === "number" && missing > 0 ? missing : fallbackWidth;
        return { text, width, space: code === 32 };
      });
    },
  };
}
