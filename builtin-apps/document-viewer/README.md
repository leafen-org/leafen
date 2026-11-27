# PDF Documents

A built-in app that opens PDF files from a file block's menu ("Open with") in
pdf.js's viewer. Someone who may change the page fills the PDF's forms and adds
annotations — text, drawings and highlights — and **Save** puts the PDF back
through Leafen's save (a save over a newer version offers overwrite, save as a
copy or open the latest). Others read it with its forms and annotations as they
are. The PDF's own text isn't edited. Word, Excel and PowerPoint files open in
the Office Documents app (`catalog-apps/office`), one app for each format.

The app keeps its first id and slug (`app.leafen.docviewer`, `document-viewer`),
from when it also showed Office files read-only. It has no worker; its package
lives in `internal/app/builtinapps/document-viewer`, where `ui/app.js` drives
the viewer from `ui/vendor`, a published build, unchanged, with its license:

| Folder | From npm (tarball SHA-256) | Files |
| --- | --- | --- |
| `pdfjs-dist-6.5.438` | `pdfjs-dist@6.5.438` (`7461aefcd27c4ef0dd76f9ca5760f09466eb6b3f934432670c12132e7b3d6faa`) | `build/pdf.min.mjs`, `build/pdf.worker.min.mjs`, `web/pdf_viewer.mjs`, `web/pdf_viewer.css`, `web/images/`, `cmaps/`, `wasm/` (the `.wasm` modules and their licenses), `iccs/` |
| `pdfjs-locale-6.5.438` | pdf.js's release asset `pdfjs-6.5.438-dist.zip` (`fcbe9ba21d9911843e3502adc178a7f549a524a4055f887bedc942e5490c6ba1`), build `0a4153349` as the npm package's | `web/locale/<language>/viewer.ftl` but `en-US` (pdf.js's viewer carries its English), each under MPL-2.0 as its header says, with that license's text as `LICENSE` |

pdf.js's own words — a text box's "Start typing…", its editors' and buttons'
names — follow Leafen's screen language through `ui/l10n/locale.json`, which
maps each of pdf.js's languages to its file; Korean takes this app's names for
its tools (`ui/l10n/ko.ftl`) before pdf.js's Korean (`ui/l10n/language.js`
picks the file). The bar's tool names and **Save** are pdf.js's editor
buttons' words, so they come in every language pdf.js has. The app's other
words (opening, saving, the page count, **Select**) are in
`ui/l10n/words/<language>.json` by Leafen's language codes: Korean and English
are written with the app, the other languages come from Leafen's
translations, and a word a language doesn't have yet is English. Leafen's file
preview reads pdf.js's words the same way, from this app's files.

pdf.js runs on the frame's own thread: its worker is a module, and a frame with
no origin starts only classic workers. It reads its CMaps,
WebAssembly decoders and color profiles from the app's own files. A link to an
outside address opens in a new tab once the person agrees, never in the frame.
