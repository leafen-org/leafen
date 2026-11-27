# Office Documents

An official Leafen app that opens Word, Excel and PowerPoint files (docx, xlsx,
pptx) from their file blocks in ONLYOFFICE's editor, as CryptPad builds it to run
in the browser without a document server, and saves them back in their own
format through Leafen's save (a save over a newer version offers overwrite,
save as a copy or open the latest). At about 240 MB it ships through an app
catalog rather than inside Leafen.

The editor runs scripts it makes (`scriptEval`). It opens in the frame with no
origin apps open in — the editor's own pages in frames inside it, with no
origin either, joined by `ui/bridge.js` and messages — with nothing for an
administrator to set up. It reaches only the file a person opened with it
(`personAccess: openedFile`).

Phones (touch screens narrower than 600px) and a file the editor couldn't open
get the app's light view instead, to read: docx-preview, SheetJS and
pptx-renderer (`ui/light.js`), which Leafen's built-in Document Viewer used
before it became the PDF app. Wider touch screens, devices with less than 4 GB
of memory (where the browser says so) and narrow windows get the light view
first, with "Open in the editor". Each engine is a published build, unchanged, in
`ui/vendor/<name>-<version>/` with its license and the licenses of what it
bundles:

| Folder | From npm (tarball SHA-256) | Files |
| --- | --- | --- |
| `docx-preview-0.4.1` | `docx-preview@0.4.1` (`b5c34d9fb04b3d70ba8cde8a71e2d361325993652ea917817654ed9431750432`) | `dist/docx-preview.min.js` |
| `jszip-3.10.1` | `jszip@3.10.1` (`5117f4a2a645aeb307bf3b829c575ad58135cc97e75291e594532ab5b5b21b23`) | `dist/jszip.min.js` (bundles pako 1.0.11, lie 3.3.0, immediate 3.0.6, setimmediate 1.0.5) |
| `xlsx-0.20.3` | SheetJS `https://cdn.sheetjs.com/xlsx-0.20.3/package/dist/xlsx.full.min.js` (the file's own SHA-256: `cc015130aa8521e7f088f88898eba949ccdcbfb38df0bd129b44b7273c3a6f41`) | `dist/xlsx.full.min.js`, the full build, which also reads Excel 97–2003 (.xls) and Numbers files (bundles SheetJS's codepage 1.15.0 for the text of older files) |
| `pptx-renderer-1.3.0` | `@aiden0z/pptx-renderer@1.3.0` (`f867ce7e814454ac048c3857b0db689f884dea633a66e73648860a44cacf0422`) | `dist/aiden0z-pptx-renderer.browser.es.js` (bundles ECharts 6.0.0, ZRender 6.0.0, tslib 2.3.0, JSZip 3.10.1 and its helpers, mtx-decompressor 1.4.2 under MPL-2.0 — its source: <https://github.com/ChristopherVR/mtx-decompressor/archive/refs/tags/v1.4.2.tar.gz>) |

## Build

    node catalog-apps/office/build-onlyoffice.mjs <work folder> [<package.leafenapp>]

The script downloads, into the work folder, and checks against hashes:

- CryptPad's `onlyoffice-editor` and `onlyoffice-x2t-wasm` v9.3.2+2 (GNU AGPL v3);
- Noto Sans KR and Noto Serif KR, Regular and Bold (SIL OFL 1.1), from
  `notofonts/noto-cjk` at Sans2.004 and Serif2.003, which it gives TrueType
  outlines with `otf2ttf.py` (Python with fontTools).

It then keeps the Word, Excel and PowerPoint editors in English and Korean —
leaving out help, other languages, source maps, the PDF, Visio and macro
editors — puts `ui/bridge.js` first in the editor pages, and rebuilds the font
list (`sdkjs/common/AllFonts.js`, with `fontlist.mjs`): Monotype's Arial and
Microsoft's Calibri are left out and their names open the metric-compatible
Arimo and Carlito, WenQuanYi (GPL) and Ubuntu are left out, and Hangul — and
CJK characters the left-out fonts had — open Noto Sans KR. The package's
`ui/vendor/onlyoffice-9.3.2/` carries the AGPL text, `SOURCE.md` (where the
sources are) and `fonts/FONTS.md` (each font's own copyright and license, with
the license texts in `fonts/licenses/`); `ui/vendor/x2t-9.3.2/` likewise.

## How it works

`ui/app.js` reads the file through the app SDK, turns it into the editor's own
form with x2t (WebAssembly, `ui/x2t.js`), starts ONLYOFFICE's editor in a frame with a
stand-in document server that answers as for one person editing alone, and on
**Save** asks the editor for its document, turns it back into the Office
format with x2t and hands it to Leafen's save. The editor's own Save (Ctrl+S,
its menu and button) asks the stand-in server, which saves the same way; the
editor's status line, which would report changes the stand-in server holds as
saved, is hidden, and the app's bar says whether the file is saved. After the
bar's Save the editor's caret has the focus again, so the next key lands where
the person was typing: the editor's frame is focused, and the document and
presentation editors also take it through their own `grabFocus` (the
spreadsheet editor's throws in its frame, and its cell takes keys with the
frame focused alone).
`ui/bridge.js` answers the request for the document inside the editor's frame
— a spreadsheet cell still being typed in is put in first, ending its editing
as the editor's own Save does — tells the app of each change the editor makes
(through its history, or a cell's typing before Enter), which the bar and
Leafen's question before closing go by, keeps a spreadsheet cell being typed
in within sight when the frame gets shorter (a tablet's keyboard rising), has
the editor draw everything again when the browser hands its canvases back
blank (a lost graphics context restored, as Chrome on an Android tablet did at
a fresh start), and hands pictures put in while
editing (from a file, or dropped in) to the app, which keeps them in x2t's
media folder so the saved file has them; a pasted picture comes as data and is kept the same way. One by address
— from a URL, or in a pasted web page — stays out, as the app reaches no
address (for a URL the editor says it is incorrect). When the app's frame has no origin of its own,
the bridge also stands in for what the editor reads from its parent, and
reads a formatted paste from a shadow root of the editor's page rather than
from a frame the editor makes for it, which can't be reached there — so the
editor can run there too. (The spreadsheet editor leaves out pasted HTML that
holds pictures.)
The editor's Print (Ctrl+P, its menu and button) hands the page layout it
draws to the bridge, with the document and the fonts it uses; the app has a
fresh x2t in a worker turn them, with the document's pictures, into a PDF —
stopped after a minute, so a conversion that never ends can't stop the page
— and gives it to Leafen's `printDocument`, which prints it from its own page,
as the app's frame may not open the browser's print dialog. The Korean fonts
are TrueType for this: in their published CFF form x2t's PDF left out their
text and could run without end.

## New documents

`templates/blank.docx`, `blank.xlsx` and `blank.pptx` are the blank files the
library's "New document" starts from (`newFiles` in the manifest). They were
made once with LibreOffice from empty input, with no author or other
personal details in their properties:

    soffice --headless --convert-to docx --outdir templates empty.txt
    soffice --headless --convert-to xlsx --outdir templates empty.csv
    soffice --headless --convert-to pptx --outdir templates empty.fodp

(`empty.fodp` being a flat ODF presentation with one empty 16:9 slide.)
