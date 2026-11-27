// Draws the Word, Excel, PowerPoint or PDF file the Leafen page that frames this
// one posts, to read: { type: "leafen:document-preview", ext, data, language, words, theme, name, present, embed }.
// Its own words (a zoom bar's, a sheet's) are the ones the page sends, in the
// person's language from Leafen's translations; a PDF's pdf.js words are pdf.js's own for that language.
// It answers "ready" once it listens, then "shown" or "failed"; "escape" when Escape is pressed in it.
// A sheet posted as "sheet" (one that isn't an .xlsx file) is drawn as a plain table (table.js): this one tells
// the page its sheets ({ type: "leafen:document-preview:sheets", names }) and the page says which to draw
// ({ type: "leafen:document-preview:sheet", at }), as its head shows their tabs.
// A PowerPoint file posted with present is drawn a slide at a time (showPresentation); one posted with
// embed stands in a page's document embed, its pages or slides laid out as Notion's embed (style.css).
import { lightWords, showLight } from "./light.js";
import { showTable } from "./table.js";

const light = document.getElementById("light");
const sheets = document.getElementById("sheets");
const answer = (state) => parent.postMessage({ type: "leafen:document-preview:" + state }, "*");
let shown = false;

addEventListener("message", async (event) => {
  const message = event.data;
  if (event.source !== parent || shown || !message || message.type !== "leafen:document-preview") return;
  if (!["docx", "xlsx", "csv", "sheet", "pptx", "pdf"].includes(message.ext) || !(message.data instanceof ArrayBuffer)) return answer("failed");
  shown = true;
  if (message.theme === "dark" || message.theme === "light") document.documentElement.dataset.theme = message.theme;
  const language = String(message.language || "en");
  document.documentElement.lang = language;
  document.documentElement.classList.toggle("embed", message.embed === true);
  // A word the page doesn't send (an older Leafen's) is this file's English, or Korean.
  const korean = language.toLowerCase().startsWith("ko");
  const sent = Object.entries(message.words && typeof message.words === "object" ? message.words : {})
    .filter(([, word]) => typeof word === "string" && word !== "");
  const say = { ...lightWords.en, ...pdfWords.en, ...(korean ? { ...lightWords.ko, ...pdfWords.ko } : {}), ...Object.fromEntries(sent) };
  try {
    if (message.ext === "pdf") await showPDF(message.data, light, say, language);
    else if (message.ext === "sheet") {
      light.hidden = false;
      const pick = await showTable(message.data, light, (names) => parent.postMessage({ type: "leafen:document-preview:sheets", names }, "*"));
      addEventListener("message", (event) => {
        if (event.source === parent && event.data?.type === "leafen:document-preview:sheet") pick(Number(event.data.at) || 0);
      });
    }
    else if (message.ext === "pptx" && message.present === true) await showPresentation(message.data, light, say);
    else await showLight(message.ext, message.data, say, {
      root: light, tabs: sheets, name: typeof message.name === "string" ? message.name : "",
      // No slide count under a PowerPoint file, as Notion's embed has none (seen 2026-11-12); the Office app says it in its bar.
      place: () => {},
    });
    answer("shown");
  } catch {
    light.hidden = true;
    answer("failed");
  }
});
// The PDF's zoom bar, in Notion's words (2026-11-06), with lightWords' zoomIn and zoomOut.
const pdfWords = {
  en: { fit: "Fit to page", reset: "Reset zoom to 100%" },
  ko: { fit: "페이지에 맞춤", reset: "확대/축소를 100%로 초기화" },
};

// A PDF point drawn at 100% is one CSS pixel, as Notion counts it; pdf.js's
// scale 1 draws it at 96/72.
const PDF_POINT = 72 / 96;
const ZOOM_STEP = 25, ZOOM_MIN = 25, ZOOM_MAX = 200;

// showPDF draws a PDF a whole page in view, with a bar to zoom by quarters
// from 25% to 200% and back to fitting the page, as Notion's preview does
// (2026-11-06), with the PDF app's pdf.js (served at ./pdfjs/), its text
// selectable and nothing to edit. pdf.js's own words — a page's name to a
// screen reader, a note's mark — are in the person's language as in the PDF
// app, from its translations (./pdfjs-words/, which index.html names):
// 2026-11-14, a Korean screen read "Page 1".
async function showPDF(data, root, say, language) {
  const base = new URL("./pdfjs/", import.meta.url).href;
  document.head.append(Object.assign(document.createElement("link"), { rel: "stylesheet", href: `${base}web/pdf_viewer.css` }));
  const pdfjs = await import(`${base}pdf.min.mjs`);
  // A frame with no origin starts only classic workers, so pdf.js reads the file on this thread, as in the PDF app.
  pdfjs.GlobalWorkerOptions.workerSrc = `${base}pdf.worker.min.mjs`;
  globalThis.pdfjsWorker = await import(`${base}pdf.worker.min.mjs`);
  globalThis.pdfjsLib = pdfjs;
  const { EventBus, GenericL10n, PDFLinkService, PDFViewer } = await import(`${base}web/pdf_viewer.mjs`);
  const { pdfjsLanguage } = await import(new URL("./pdfjs-words/l10n/language.js", import.meta.url).href);
  const pdf = await pdfjs.getDocument({
    data: new Uint8Array(data), isEvalSupported: false, cMapUrl: `${base}cmaps/`, cMapPacked: true,
    wasmUrl: `${base}wasm/`, iccUrl: `${base}iccs/`,
  }).promise;
  const first = (await pdf.getPage(1)).getViewport({ scale: 1 });
  const container = Object.assign(document.createElement("div"), { className: "pdfScroll" });
  const pages = Object.assign(document.createElement("div"), { className: "pdfViewer" });
  container.append(pages);
  root.classList.add("pdf");
  root.replaceChildren(container);
  root.hidden = false;
  const eventBus = new EventBus();
  const linkService = new PDFLinkService({ eventBus });
  const l10n = new GenericL10n(pdfjsLanguage(language));
  // A note's mark is one of pdf.js's own pictures, served beside it (2026-11-15: broken without).
  const viewer = new PDFViewer({
    container, viewer: pages, eventBus, linkService, l10n, textLayerMode: 1, imageResourcesPath: `${base}web/images/`,
    annotationMode: pdfjs.AnnotationMode.ENABLE, annotationEditorMode: pdfjs.AnnotationEditorType.DISABLE,
  });
  // Given its words, the viewer leaves translating its container to the page.
  void l10n.translate(container);
  linkService.setViewer(viewer);

  const zoom = document.getElementById("zoom");
  const fitButton = document.getElementById("zoomFit"), reset = document.getElementById("zoomReset");
  const zoomOut = document.getElementById("zoomOut"), zoomIn = document.getElementById("zoomIn");
  fitButton.textContent = say.fit;
  zoomOut.setAttribute("aria-label", say.zoomOut); zoomOut.title = say.zoomOut;
  zoomIn.setAttribute("aria-label", say.zoomIn); zoomIn.title = say.zoomIn;
  reset.setAttribute("aria-label", say.reset); reset.title = say.reset;
  zoom.hidden = false;
  let fitting = true;
  const percent = () => Math.round(viewer.currentScale / PDF_POINT * 100);
  // The first page whole in view with the 8px above it and its 8px of white
  // below, as in Notion's (2026-11-06, in 781px: an A4 page at 91%, a Letter one at 97%).
  const fit = () => {
    const room = Math.min((container.clientWidth - 32) / first.width, (container.clientHeight - 16) / first.height);
    viewer.currentScale = Math.max(0.05, room) * PDF_POINT;
  };
  const show = () => {
    fitButton.setAttribute("aria-pressed", String(fitting));
    reset.textContent = `${percent()}%`;
  };
  const zoomTo = (value) => {
    fitting = false;
    viewer.currentScale = Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, value)) / 100 * PDF_POINT;
    show();
  };
  fitButton.onclick = () => { fitting = true; fit(); show(); };
  zoomOut.onclick = () => zoomTo(Math.round(percent() / ZOOM_STEP) * ZOOM_STEP - ZOOM_STEP);
  zoomIn.onclick = () => zoomTo(Math.round(percent() / ZOOM_STEP) * ZOOM_STEP + ZOOM_STEP);
  reset.onclick = () => zoomTo(100);
  // Opened at its very top, the 8px above the first page in view: pdf.js
  // brings a page's own top to the view's edge as the first scale is set.
  eventBus.on("pagesinit", () => { fit(); show(); container.scrollTop = 0; });
  addEventListener("resize", () => { if (fitting) { fit(); show(); } });
  viewer.setDocument(pdf);
  linkService.setDocument(pdf, null);
}

// showPresentation draws one slide at a time, whole in the frame on black, as
// Notion's "프레젠테이션 모드" (seen 2026-11-12): the page that frames this one
// draws the bar under it and says which slide to show
// ({ type: "leafen:document-preview:slide", at }); this one tells it how many
// there are ({ type: "leafen:document-preview:slides", count }), the keys that
// turn them and the pointer's moves, which its bar shows on.
let presenting = false;
async function showPresentation(data, root, say) {
  const { PptxViewer, RECOMMENDED_ZIP_LIMITS } = await import(new URL("./vendor/pptx-renderer-1.3.0/aiden0z-pptx-renderer.browser.es.js", import.meta.url).href);
  document.documentElement.classList.add("presenting");
  root.classList.add("present");
  const stage = Object.assign(document.createElement("div"), { className: "slideStage" });
  root.replaceChildren(stage);
  root.hidden = false;
  const viewer = await PptxViewer.open(data, stage, {
    zipLimits: RECOMMENDED_ZIP_LIMITS, lazyMedia: true, lazySlides: true, pdfjs: false, fitMode: "contain", renderMode: "slide",
  });
  if (!viewer.slideCount) {
    root.replaceChildren(Object.assign(document.createElement("p"), { className: "note", textContent: say.noSlides }));
    return;
  }
  // As wide as the frame or as its height lets the slide be, whichever is less; the renderer fits the slide to that width.
  stage.style.setProperty("--slide-ratio", String(viewer.slideWidth / viewer.slideHeight));
  addEventListener("message", (event) => {
    if (event.source === parent && event.data?.type === "leafen:document-preview:slide") void viewer.goToSlide(Number(event.data.at) || 0);
  });
  presenting = true;
  parent.postMessage({ type: "leafen:document-preview:slides", count: viewer.slideCount }, "*");
}
// The keys that turn a presented slide, as Notion's: → Space PageDown on, ← PageUp back.
const SLIDE_KEYS = new Set(["ArrowRight", "ArrowLeft", " ", "PageDown", "PageUp"]);
let moved = 0;
addEventListener("pointermove", () => {
  const now = Date.now();
  if (!presenting || now - moved < 100) return;
  moved = now;
  parent.postMessage({ type: "leafen:document-preview:pointer" }, "*");
});

// Escape pressed in here closes the preview, as anywhere in Notion's: a press
// in the document or its zoom bar leaves the keys with this frame (2026-11-06).
addEventListener("keydown", (event) => {
  if (event.isComposing) return;
  if (event.key === "Escape") answer("escape");
  if (presenting && SLIDE_KEYS.has(event.key) && !event.altKey && !event.ctrlKey && !event.metaKey) {
    event.preventDefault();
    parent.postMessage({ type: "leafen:document-preview:key", key: event.key }, "*");
  }
});

answer("ready");
