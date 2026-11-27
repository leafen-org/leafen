# Hangul Documents

A built-in app that opens Hangul documents (HWP 5 and HWPX) from a file
block's menu ("Open with"), with rhwp (MIT, <https://github.com/edwardkim/rhwp>).
It has no worker; its package lives in `internal/app/builtinapps/hwp-viewer`
(the folder keeps its first name), and `ui/app.js` is the whole app.

- Reading: `@rhwp/core` draws the pages as SVG. It runs in any frame — one
  with no origin, a phone, or a person who may only read.
- Editing: a person who may change the page edits in rhwp-studio, embedded
  from the app's own files with `@rhwp/editor`'s protocol in the frame with
  no origin apps open in. The studio's frame has no origin either: the app
  sends the handshake without naming an origin, and `studio-bridge.js`, the
  studio page's first script (`leafen-bridge.js`), hands it on with the page's
  address and hides IndexedDB; everything after travels on the handshake's
  ports. The bridge also tells the app each press that may change the
  document as it comes, so closing right after typing asks about it, and
  after the app's Save it focuses again what was typed in when the app's bar
  took the focus, so the next key lands at the caret. Saving
  puts the file back in its own format: an HWPX as HWPX, an HWP as HWP when
  rhwp reads its own writing back whole (`exportHwpVerify`), else as HWPX
  with a notice. A phone (a touch screen under 600px) only reads, and so
  does a frame where the studio didn't start within 30 seconds; a wider touch
  screen reads first, with "Open in the editor".
- New documents: the library's "New document" starts an HWPX from
  `templates/blank.hwpx`, rhwp's own blank document written as HWPX, which
  `build-rhwp.mjs` makes along with the rest.

`ui/vendor` holds rhwp 0.8.6, put there by
`node builtin-apps/hwp-viewer/build-rhwp.mjs <work folder>`:

- `rhwp-core-0.8.6/rhwp.js` and `rhwp-editor-0.8.6/` are the npm packages as
  published (tarball SHA-256 `4e1f5aeceed38f72cade226b291977f35a9bf76da0f4f4138d1ae5113f6b1933`
  and `c0b11162c7849152c0f20d13698dbb081177fb386c9527ace17f4d8f6198743c`).
- `rhwp-studio-0.8.6/` is rhwp-studio built from the tag `v0.8.6` (commit
  `f1f9c6ae58344ee9368996d3543f76b9345cf227`) with `@rhwp/core`'s WebAssembly,
  without hwpctrl, external web fonts, the service worker, the sample
  documents or the CanvasKit renderer, with stable file names (so the reader
  and the editor share `assets/rhwp_bg.wasm`) and the SIL OFL 1.1 fonts the
  HWP fallbacks name, and the bridge first in `index.html`. Its licenses are `LICENSE`, `THIRD_PARTY_LICENSES.md`
  and `fonts/LICENSE-OFL-1.1.txt`, with the fonts' list in `fonts/FONTS.md`.
