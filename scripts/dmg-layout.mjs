// The macOS disk image's window (scripts/package-desktop.mjs), laid out as
// Notion's disk image is (its .DS_Store and background picture, read
// 2026-11-16 from Notion-7.43.2-arm64.dmg): the volume named "<app>
// Installer" with the app's own icon as its icon; a window 540 wide and 402
// tall at (100, 400) — the 540×380 background picture plus Finder's 22-point
// title bar — with no toolbar, path bar, status bar or tabs (the sidebar set
// as shown at no width, which Finder hides with the toolbar); 80-point icons
// with 12-point names below them, the app at (130, 180) on the left and the
// Applications folder at (410, 180) on the right; behind them an opaque light
// picture with a dark arrow from one to the other. The picture is Leafen's
// own drawing in its own light page colours, not Notion's.
//
// Finder keeps a folder's window in its .DS_Store, and the background picture
// there as an alias to the file inside the image. Both are written here
// directly, on the image mounted read-write, rather than by telling Finder to
// arrange the window over AppleScript, which needs the Automation permission
// on the machine that packages and the window on its screen. The formats are
// the ones Finder writes (Mark Mentovai's and Wim Lewis's notes, as the
// ds_store and mac_alias Python packages read them).

import { spawnSync } from 'node:child_process'
import { copyFileSync, mkdirSync, readFileSync, rmSync, statSync, symlinkSync, writeFileSync } from 'node:fs'
import { basename, join } from 'node:path'
import { deflateSync } from 'node:zlib'

export const diskImageWindow = Object.freeze({
  x: 100, y: 400, width: 540, height: 380, titleBar: 22, iconSize: 80, textSize: 12,
  app: { x: 130, y: 180 }, applications: { x: 410, y: 180 },
})

const backgroundFolder = '.background'
const backgroundFile = 'background.tiff'
// Seconds from 1904 (the Mac's dates) to 1970.
const macEpoch = 2082844800

function command(name, args) {
  const result = spawnSync(name, args, { encoding: 'utf8' })
  if (result.status !== 0) throw new Error(`${name} ${args.join(' ')} failed: ${(result.stderr || '').trim()}`)
  return result
}

// ---- the background: an arrow from the app's place to the Applications folder's

const crcTable = Array.from({ length: 256 }, (_, n) => {
  let c = n
  for (let k = 0; k < 8; k += 1) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1
  return c >>> 0
})

function crc32(bytes) {
  let crc = 0xffffffff
  for (const byte of bytes) crc = crcTable[(crc ^ byte) & 0xff] ^ (crc >>> 8)
  return (crc ^ 0xffffffff) >>> 0
}

function png(width, height, rgba) {
  const chunk = (type, data) => {
    const head = Buffer.alloc(8)
    head.writeUInt32BE(data.length, 0)
    head.write(type, 4, 'latin1')
    const tail = Buffer.alloc(4)
    tail.writeUInt32BE(crc32(Buffer.concat([head.subarray(4), data])), 0)
    return Buffer.concat([head, data, tail])
  }
  const header = Buffer.alloc(13)
  header.writeUInt32BE(width, 0)
  header.writeUInt32BE(height, 4)
  header.set([8, 6, 0, 0, 0], 8)
  const rows = Buffer.alloc((width * 4 + 1) * height)
  for (let y = 0; y < height; y += 1) rgba.copy(rows, y * (width * 4 + 1) + 1, y * width * 4, (y + 1) * width * 4)
  return Buffer.concat([Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]), chunk('IHDR', header),
    chunk('IDAT', deflateSync(rows)), chunk('IEND', Buffer.alloc(0))])
}

// Leafen's light page colours (web/src/app/globals.css --bg-app and
// --text-default): the picture is opaque, so the window reads light in the
// dark appearance too, as Notion's does.
const paper = [255, 255, 255]
const ink = [44, 44, 43]

function distanceToSegment(x, y, [ax, ay], [bx, by]) {
  const dx = bx - ax
  const dy = by - ay
  const along = Math.max(0, Math.min(1, ((x - ax) * dx + (y - ay) * dy) / (dx * dx + dy * dy)))
  return Math.hypot(x - (ax + along * dx), y - (ay + along * dy))
}

// The arrow in points, where Notion's picture has its arrow: round-capped
// strokes 9 points wide, the ink running from 42 points right of the app's
// icon to 38 points left of the Applications folder's, 10 points below the
// icons' places (the middle of an icon with its name), and an open head of
// two strokes whose ink reaches 32 points back and 24 up and down from the
// tip.
function arrowCovers(x, y) {
  const { app, applications, iconSize } = diskImageWindow
  const half = 4.5
  const middle = app.y + 10
  const tail = [app.x + iconSize / 2 + 42 + half, middle]
  const tip = [applications.x - iconSize / 2 - 38 - half, middle]
  const strokes = [[tail, tip], [tip, [tip[0] - 23, middle - 20]], [tip, [tip[0] - 23, middle + 20]]]
  return strokes.some(([from, to]) => distanceToSegment(x, y, from, to) <= half)
}

function drawBackground(scale) {
  const width = diskImageWindow.width * scale
  const height = diskImageWindow.height * scale
  const rgba = Buffer.alloc(width * height * 4)
  const samples = 4
  for (let py = 0; py < height; py += 1) {
    for (let px = 0; px < width; px += 1) {
      let covered = 0
      for (let sy = 0; sy < samples; sy += 1) {
        for (let sx = 0; sx < samples; sx += 1) {
          if (arrowCovers((px + (sx + 0.5) / samples) / scale, (py + (sy + 0.5) / samples) / scale)) covered += 1
        }
      }
      const share = covered / (samples * samples)
      const at = (py * width + px) * 4
      rgba.set([...paper.map((channel, index) => Math.round(channel + (ink[index] - channel) * share)), 255], at)
    }
  }
  return png(width, height, rgba)
}

// The background as one TIFF holding the picture at 1× and 2×, as Finder
// draws a disk image's background sharp on a Retina screen.
function writeDiskImageBackground(folder, work) {
  const one = join(work, 'background.png')
  const two = join(work, 'background@2x.png')
  writeFileSync(one, drawBackground(1))
  writeFileSync(two, drawBackground(2))
  mkdirSync(join(folder, backgroundFolder), { recursive: true })
  command('tiffutil', ['-cathidpicheck', one, two, '-out', join(folder, backgroundFolder, backgroundFile)])
}

// ---- the alias Finder keeps to the background picture

function pascal(text, size) {
  const bytes = Buffer.from(text, 'utf8').subarray(0, size - 1)
  const field = Buffer.alloc(size)
  field[0] = bytes.length
  bytes.copy(field, 1)
  return field
}

function tagged(tag, data) {
  const head = Buffer.alloc(4)
  head.writeInt16BE(tag, 0)
  head.writeInt16BE(data.length, 2)
  return Buffer.concat([head, data, Buffer.alloc(data.length & 1)])
}

function highResolutionDate(seconds) {
  const field = Buffer.alloc(8)
  field.writeBigUInt64BE(BigInt(seconds) * 65536n, 0)
  return field
}

// A version 2 alias record to a file on the mounted image, which Finder
// finds again by the volume's name and creation date and the path in it.
function alias(mount, volumeName) {
  const folderPath = join(mount, backgroundFolder)
  const target = statSync(join(folderPath, backgroundFile))
  const folder = statSync(folderPath)
  const volumeDate = Math.floor(statSync(mount).birthtimeMs / 1000) + macEpoch
  const created = Math.floor(target.birthtimeMs / 1000) + macEpoch
  const fixed = Buffer.alloc(150)
  let at = 4
  at = fixed.writeInt16BE(150, at)
  at = fixed.writeInt16BE(2, at)
  at = fixed.writeInt16BE(0, at)
  pascal(volumeName, 28).copy(fixed, at)
  at += 28
  at = fixed.writeUInt32BE(volumeDate, at)
  at += fixed.write('H+', at, 'latin1')
  at = fixed.writeInt16BE(0, at)
  at = fixed.writeUInt32BE(folder.ino, at)
  pascal(backgroundFile, 64).copy(fixed, at)
  at += 64
  at = fixed.writeUInt32BE(target.ino, at)
  at = fixed.writeUInt32BE(created, at)
  at += 8
  at = fixed.writeInt16BE(-1, at)
  fixed.writeInt16BE(-1, at)
  const cnids = Buffer.alloc(4)
  cnids.writeUInt32BE(folder.ino, 0)
  const unicode = (text) => {
    const utf16 = Buffer.from(text, 'utf16le').swap16()
    const length = Buffer.alloc(2)
    length.writeUInt16BE(text.length, 0)
    return Buffer.concat([length, utf16])
  }
  const record = Buffer.concat([
    fixed,
    tagged(0, Buffer.from(backgroundFolder, 'utf8')),
    tagged(16, highResolutionDate(volumeDate)),
    tagged(17, highResolutionDate(created)),
    tagged(1, cnids),
    tagged(2, Buffer.from(`${volumeName}:${backgroundFolder}:${backgroundFile}`, 'utf8')),
    tagged(14, unicode(backgroundFile)),
    tagged(15, unicode(volumeName)),
    tagged(18, Buffer.from(`/${backgroundFolder}/${backgroundFile}`, 'utf8')),
    tagged(19, Buffer.from(`/Volumes/${volumeName}`, 'utf8')),
    Buffer.from([0xff, 0xff, 0x00, 0x00]),
  ])
  record.writeInt16BE(record.length, 4)
  return record
}

// ---- the .DS_Store

const escapeXML = (text) => text.replace(/&/gu, '&amp;').replace(/</gu, '&lt;').replace(/>/gu, '&gt;')

function plistValue(value) {
  if (typeof value === 'boolean') return value ? '<true/>' : '<false/>'
  if (typeof value === 'number') return Number.isInteger(value) ? `<integer>${value}</integer>` : `<real>${value}</real>`
  if (Buffer.isBuffer(value)) return `<data>${value.toString('base64')}</data>`
  if (value && typeof value === 'object' && 'real' in value) return `<real>${value.real}</real>`
  return `<string>${escapeXML(String(value))}</string>`
}

// A binary property list, as Finder keeps its window settings.
function binaryPlist(work, name, dict) {
  const path = join(work, `${name}.plist`)
  writeFileSync(path, `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>${Object.entries(dict).map(([key, value]) => `<key>${key}</key>${plistValue(value)}`).join('')}</dict></plist>
`)
  command('plutil', ['-convert', 'binary1', path])
  return readFileSync(path)
}

function entry(name, code, type, value) {
  const utf16 = Buffer.from(name, 'utf16le').swap16()
  const head = Buffer.alloc(4)
  head.writeUInt32BE(name.length, 0)
  let data
  if (type === 'blob') {
    data = Buffer.alloc(4 + value.length)
    data.writeUInt32BE(value.length, 0)
    value.copy(data, 4)
  } else if (type === 'long') {
    data = Buffer.alloc(4)
    data.writeUInt32BE(value, 0)
  } else {
    data = Buffer.from(value, 'latin1')
  }
  return { name, code, bytes: Buffer.concat([head, utf16, Buffer.from(code + type, 'latin1'), data]) }
}

function iconPlace({ x, y }) {
  const place = Buffer.alloc(16)
  place.writeUInt32BE(x, 0)
  place.writeUInt32BE(y, 4)
  place.writeUInt32BE(0xffffffff, 8)
  place.writeUInt32BE(0xffff0000, 12)
  return place
}

// The store as one leaf of records in a buddy-allocated space of 8 KB: the
// header block at 0, the tree's header at 32, the allocator's own block at
// 2048 (its block addresses, the table naming the tree "DSDB", the free
// lists) and the leaf at 4096, the rest of the space on the free lists.
function dsStore(records) {
  records.sort((a, b) => {
    const left = a.name.toLowerCase()
    const right = b.name.toLowerCase()
    return left < right ? -1 : left > right ? 1 : a.code < b.code ? -1 : a.code > b.code ? 1 : 0
  })
  const space = Buffer.alloc(8192)
  const leaf = Buffer.concat([Buffer.alloc(8), ...records.map((record) => record.bytes)])
  if (leaf.length > 4096) throw new Error('the disk image window settings do not fit one page')
  leaf.writeUInt32BE(0, 0)
  leaf.writeUInt32BE(records.length, 4)
  leaf.copy(space, 4096)
  const tree = Buffer.alloc(20)
  ;[2, 0, records.length, 1, 0x1000].forEach((value, index) => tree.writeUInt32BE(value, index * 4))
  tree.copy(space, 32)
  const blocks = [2048 | 11, 32 | 5, 4096 | 12]
  const free = new Map([[6, [64]], [7, [128]], [8, [256]], [9, [512]], [10, [1024]]])
  const words = [blocks.length, 0, ...blocks, ...Array(256 - blocks.length).fill(0)]
  const root = Buffer.alloc(2048)
  let at = 0
  for (const word of words) at = root.writeUInt32BE(word, at)
  at = root.writeUInt32BE(1, at)
  at = root.writeUInt8(4, at)
  at += root.write('DSDB', at, 'latin1')
  at = root.writeUInt32BE(1, at)
  for (let size = 0; size < 32; size += 1) {
    const offsets = free.get(size) ?? []
    at = root.writeUInt32BE(offsets.length, at)
    for (const offset of offsets) at = root.writeUInt32BE(offset, at)
  }
  root.copy(space, 2048)
  // The header block: the magic, where the allocator's block is and how much
  // of it is used (twice), and the bytes Finder writes after them.
  space.write('Bud1', 0, 'latin1')
  space.writeUInt32BE(2048, 4)
  space.writeUInt32BE(at, 8)
  space.writeUInt32BE(2048, 12)
  Buffer.from([0x00, 0x00, 0x10, 0x0c, 0x00, 0x00, 0x00, 0x87, 0x00, 0x00, 0x20, 0x0b, 0x00, 0x00, 0x00, 0x00]).copy(space, 16)
  // Every address counts from after the file's first four bytes.
  return Buffer.concat([Buffer.from([0, 0, 0, 1]), space])
}

// Writes the window's settings into the image mounted at `mount`, whose
// background picture writeDiskImageBackground put in place before the image
// was made.
function writeDiskImageLayout(mount, volumeName, appName, work) {
  const view = diskImageWindow
  const records = [
    entry('.', 'bwsp', 'blob', binaryPlist(work, 'bwsp', {
      ContainerShowSidebar: true, ShowPathbar: false, ShowSidebar: true, ShowStatusBar: false, ShowTabView: false,
      ShowToolbar: false, SidebarWidth: 0,
      WindowBounds: `{{${view.x}, ${view.y}}, {${view.width}, ${view.height + view.titleBar}}}`,
    })),
    entry('.', 'icvp', 'blob', binaryPlist(work, 'icvp', {
      arrangeBy: 'none', backgroundColorBlue: { real: 1 }, backgroundColorGreen: { real: 1 }, backgroundColorRed: { real: 1 },
      backgroundImageAlias: alias(mount, volumeName), backgroundType: 2, gridOffsetX: { real: 0 }, gridOffsetY: { real: 0 },
      gridSpacing: { real: 100 }, iconSize: { real: view.iconSize }, labelOnBottom: true, showIconPreview: true,
      showItemInfo: false, textSize: { real: view.textSize }, viewOptionsVersion: 1,
    })),
    entry('.', 'vSrn', 'long', 1),
    entry('.', 'vstl', 'type', 'icnv'),
    entry(appName, 'Iloc', 'blob', iconPlace(view.app)),
    entry('Applications', 'Iloc', 'blob', iconPlace(view.applications)),
  ]
  writeFileSync(join(mount, '.DS_Store'), dsStore(records))
}

// The app's own icon file (its Info.plist's CFBundleIconFile), which the
// volume shows as its icon.
function appIcon(app) {
  const name = command('plutil', ['-extract', 'CFBundleIconFile', 'raw', join(app, 'Contents', 'Info.plist')]).stdout.trim()
  return join(app, 'Contents', 'Resources', name.endsWith('.icns') ? name : `${name}.icns`)
}

// Marks the mounted volume's root as having a custom icon (.VolumeIcon.icns):
// the kHasCustomIcon flag in its Finder information.
function markVolumeIcon(mount) {
  const info = Buffer.alloc(32)
  info.writeUInt16BE(0x0400, 8)
  command('xattr', ['-wx', 'com.apple.FinderInfo', info.toString('hex'), mount])
}

// Makes the compressed disk image `image` holding the app and a link to
// Applications, its window laid out as above; `work` is a folder of the
// packaging's own to work in. It is compressed with LZFSE (ULFO), as
// Notion's is (read 2026-11-16), which opens on macOS 10.11 and later — the
// app asks for 12 — and is smaller than zlib (UDZO): 165.9 MB rather than
// 174.7 MB for a 289 MB app (measured 2026-11-16).
export function makeDiskImage(app, image, work) {
  const folder = join(work, 'disk-image')
  const appName = basename(app)
  const volumeName = `${appName.replace(/\.app$/u, '')} Installer`
  mkdirSync(folder, { recursive: true })
  command('ditto', [app, join(folder, appName)])
  symlinkSync('/Applications', join(folder, 'Applications'))
  copyFileSync(appIcon(app), join(folder, '.VolumeIcon.icns'))
  writeDiskImageBackground(folder, work)
  // Room for the window's settings, written once the image is mounted and
  // the background picture has its place there.
  writeFileSync(join(folder, '.DS_Store'), Buffer.alloc(8196))
  const writable = join(work, 'disk-image-rw.dmg')
  command('hdiutil', ['create', '-volname', volumeName, '-srcfolder', folder, '-fs', 'HFS+', '-format', 'UDRW', '-ov', writable])
  const mount = join(work, 'disk-image-mount')
  mkdirSync(mount, { recursive: true })
  command('hdiutil', ['attach', writable, '-nobrowse', '-noautoopen', '-noverify', '-mountpoint', mount])
  try {
    writeDiskImageLayout(mount, volumeName, appName, work)
    markVolumeIcon(mount)
  } finally {
    command('hdiutil', ['detach', mount])
  }
  rmSync(image, { force: true })
  command('hdiutil', ['convert', writable, '-format', 'ULFO', '-o', image])
}
