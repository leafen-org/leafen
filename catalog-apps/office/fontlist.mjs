// Reads a TrueType/OpenType font's tables, and ONLYOFFICE's font selection
// records (AllFonts.js, version 2), for building the Office app's font list.
import { readFileSync } from 'node:fs'

export function readFont(path) {
  const data = readFileSync(path)
  const u16 = (o) => data.readUInt16BE(o), s16 = (o) => data.readInt16BE(o), u32 = (o) => data.readUInt32BE(o)
  const tables = {}
  const count = u16(4)
  for (let i = 0; i < count; i++) {
    const at = 12 + i * 16
    tables[data.toString('latin1', at, at + 4)] = { offset: u32(at + 8), length: u32(at + 12) }
  }
  const names = {}
  if (tables.name) {
    const base = tables.name.offset, n = u16(base + 2), strings = base + u16(base + 4)
    for (let i = 0; i < n; i++) {
      const at = base + 6 + i * 12
      const [platform, encoding, language, id, length, offset] = [u16(at), u16(at + 2), u16(at + 4), u16(at + 6), u16(at + 8), u16(at + 10)]
      const raw = data.subarray(strings + offset, strings + offset + length)
      let text
      if (platform === 3 || platform === 0) { text = ''; for (let k = 0; k + 1 < raw.length; k += 2) text += String.fromCharCode(raw.readUInt16BE(k)) }
      else if (platform === 1 && encoding === 0) text = raw.toString('latin1')
      else continue
      const english = platform === 3 ? language === 0x409 : platform === 1 ? language === 0 : true
      ;(names[id] ??= {})[english ? 'en' : `${platform}-${language}`] = text
    }
  }
  const name = (id) => names[id]?.en ?? Object.values(names[id] ?? {})[0] ?? ''
  const os2 = tables['OS/2']?.offset
  const head = tables.head.offset, post = tables.post?.offset, hhea = tables.hhea.offset
  const version = os2 !== undefined ? u16(os2) : 0
  const font = {
    path, cff: Boolean(tables['CFF '] || tables.CFF2),
    family: name(16) || name(1), subfamily: name(17) || name(2), copyright: name(0), license: name(13), licenseUrl: name(14),
    unitsPerEm: u16(head + 18), macStyle: u16(head + 44),
    fixed: post !== undefined ? u32(post + 12) !== 0 : false,
    hheaAscent: s16(hhea + 4), hheaDescent: s16(hhea + 6), hheaLineGap: s16(hhea + 8),
  }
  if (os2 !== undefined) Object.assign(font, {
    avgCharWidth: s16(os2 + 2), weight: u16(os2 + 4), width: u16(os2 + 6), fsType: u16(os2 + 8), familyClass: s16(os2 + 30),
    panose: [...data.subarray(os2 + 32, os2 + 42)], unicodeRanges: [u32(os2 + 42), u32(os2 + 46), u32(os2 + 50), u32(os2 + 54)],
    fsSelection: u16(os2 + 62), typoAscent: s16(os2 + 68), typoDescent: s16(os2 + 70), typoLineGap: s16(os2 + 72),
    winAscent: u16(os2 + 74), winDescent: u16(os2 + 76),
    codePages: version >= 1 ? [u32(os2 + 78), u32(os2 + 82)] : [0, 0],
    xHeight: version >= 2 ? s16(os2 + 86) : 0, capHeight: version >= 2 ? s16(os2 + 88) : 0,
  })
  font.coverage = () => readCoverage(data, tables.cmap.offset)
  return font
}

// The code points a font maps, as [start, end] runs.
function readCoverage(data, base) {
  const u16 = (o) => data.readUInt16BE(o), u32 = (o) => data.readUInt32BE(o)
  const subtables = []
  for (let i = 0; i < u16(base + 2); i++) {
    const at = base + 4 + i * 8
    subtables.push({ platform: u16(at), encoding: u16(at + 2), offset: base + u32(at + 4) })
  }
  const pick = subtables.find((t) => t.platform === 3 && t.encoding === 10 && u16(t.offset) === 12)
    ?? subtables.find((t) => t.platform === 0 && u16(t.offset) === 12)
    ?? subtables.find((t) => t.platform === 3 && t.encoding === 1 && u16(t.offset) === 4)
    ?? subtables.find((t) => t.platform === 0 && u16(t.offset) === 4)
    ?? subtables.find((t) => t.platform === 3 && t.encoding === 0 && u16(t.offset) === 4)
  if (!pick) return []
  const runs = []
  const add = (start, end) => { const last = runs.at(-1); if (last && last[1] + 1 >= start) last[1] = Math.max(last[1], end); else runs.push([start, end]) }
  const at = pick.offset
  if (u16(at) === 12) {
    const groups = u32(at + 12)
    for (let g = 0; g < groups; g++) { const o = at + 16 + g * 12; add(u32(o), u32(o + 4)) }
  } else {
    const segments = u16(at + 6) / 2
    const ends = at + 14, starts = ends + segments * 2 + 2, deltas = starts + segments * 2, offsets = deltas + segments * 2
    for (let s = 0; s < segments; s++) {
      const end = u16(ends + s * 2), start = u16(starts + s * 2), delta = data.readInt16BE(deltas + s * 2), rangeOffset = u16(offsets + s * 2)
      if (start === 0xffff) continue
      if (rangeOffset === 0) { if (((start + delta) & 0xffff) !== 0 || start !== end) add(start, end); continue }
      for (let c = start; c <= end; c++) {
        const glyphAt = offsets + s * 2 + rangeOffset + (c - start) * 2
        if (glyphAt + 1 < data.length && u16(glyphAt) !== 0) add(c, c)
      }
    }
  }
  runs.sort((a, b) => a[0] - b[0])
  return runs
}

// ONLYOFFICE's selection records (sdkjs CFontSelect.fromStream, version 2).
export function decodeSelection(base64) {
  const data = Buffer.from(base64, 'base64')
  let at = 0
  const i32 = () => { const v = data.readInt32LE(at); at += 4; return v }
  const u32 = () => { const v = data.readUInt32LE(at); at += 4; return v }
  const u16 = () => { const v = data.readUInt16LE(at); at += 2; return v }
  const s16 = () => { const v = data.readInt16LE(at); at += 2; return v }
  const str = () => { const n = i32(); const v = data.toString('utf8', at, at + n); at += n; return v }
  const count = i32(), records = []
  for (let r = 0; r < count; r++) {
    const start = at, length = i32()
    const record = { name: str(), names: [] }
    for (let n = i32(); n > 0; n--) record.names.push(str())
    record.path = str()
    Object.assign(record, { index: i32(), italic: i32(), bold: i32(), fixed: i32() })
    const panoseLength = i32(); record.panose = [...data.subarray(at, at + panoseLength)]; at += panoseLength
    record.unicodeRanges = [u32(), u32(), u32(), u32()]; record.codePages = [u32(), u32()]
    Object.assign(record, { weight: u16(), width: u16(), familyClass: s16(), format: s16(), avgCharWidth: s16(), ascent: s16(), descent: s16(),
      lineGap: s16(), xHeight: s16(), capHeight: s16(), fsType: u16() })
    at = start + length
    records.push(record)
  }
  return records
}

export function encodeSelection(records) {
  const parts = []
  const i32 = (v) => { const b = Buffer.alloc(4); b.writeInt32LE(v); return b }
  const u32 = (v) => { const b = Buffer.alloc(4); b.writeUInt32LE(v >>> 0); return b }
  const u16 = (v) => { const b = Buffer.alloc(2); b.writeUInt16LE(v & 0xffff); return b }
  const s16 = (v) => { const b = Buffer.alloc(2); b.writeInt16LE(v); return b }
  const str = (v) => { const b = Buffer.from(v, 'utf8'); return [i32(b.length), b] }
  parts.push(i32(records.length))
  for (const r of records) {
    const body = Buffer.concat([...str(r.name), i32(r.names.length), ...r.names.flatMap(str), ...str(r.path), i32(r.index), i32(r.italic), i32(r.bold), i32(r.fixed),
      i32(r.panose.length), Buffer.from(r.panose), ...r.unicodeRanges.map(u32), ...r.codePages.map(u32),
      u16(r.weight), u16(r.width), s16(r.familyClass), s16(r.format), s16(r.avgCharWidth), s16(r.ascent), s16(r.descent), s16(r.lineGap), s16(r.xHeight), s16(r.capHeight), u16(r.fsType)])
    parts.push(i32(body.length + 4), body)
  }
  return Buffer.concat(parts).toString('base64')
}
