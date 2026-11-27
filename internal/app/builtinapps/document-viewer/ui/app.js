// Opens a PDF a person opened from its file block in pdf.js's viewer (ui/vendor,
// Apache-2.0). Someone who may change the page fills its forms and adds
// annotations — text, drawing, highlights — and saving puts the PDF back
// through Leafen's save, which asks how to keep it when someone saved first
// (document apps decision of 2026-10-10, 11). The page's own text isn't
// edited. Others read it with its forms and annotations as they are.
import { connect } from "./leafen-app-sdk.js";
import { pdfjsLanguage } from "./l10n/language.js";
import { appWords, fill } from "./words.js";

const base = new URL("./vendor/pdfjs-dist-6.5.438/", import.meta.url).href;
const state = document.getElementById("state");
const bar = document.getElementById("bar");
const status = document.getElementById("status");
const pageAt = document.getElementById("page");
const tools = document.getElementById("tools");
const saveButton = document.getElementById("save");
const container = document.getElementById("viewerContainer");
// The app's own words, English until the person's language is known.
let say = {};

try {
  const leafen = await connect();
  say = await appWords(leafen.context.locale);
  document.documentElement.dataset.theme = leafen.context.theme;
  leafen.onTheme((theme) => { document.documentElement.dataset.theme = theme; });
  const file = leafen.context.file;
  if (!file) {
    state.textContent = say.none;
  } else if (!/\.pdf$/iu.test(file.name ?? "") && String(file.contentType ?? "").toLowerCase() !== "application/pdf") {
    state.textContent = say.kind;
  } else {
    state.textContent = say.loading;
    const read = await leafen.api("GET", `/files/${encodeURIComponent(file.fileId)}`);
    if (read.status !== 200 || !(read.body instanceof ArrayBuffer)) throw new Error(`read ${read.status}`);
    await open(leafen, file, read.body);
  }
} catch (error) {
  if (!say.failed) say = await appWords("en");
  bar.hidden = true;
  container.hidden = true;
  state.hidden = false;
  state.textContent = error?.name === "PasswordException" ? say.locked : say.failed;
  state.dataset.error = String(error?.message ?? error);
}

async function open(leafen, file, data) {
  const pdfjs = await import(`${base}pdf.min.mjs`);
  // pdf.js's worker is a module, and a frame with no origin starts only
  // classic workers, so the document is read on this thread.
  pdfjs.GlobalWorkerOptions.workerSrc = `${base}pdf.worker.min.mjs`;
  globalThis.pdfjsWorker = await import(`${base}pdf.worker.min.mjs`);
  globalThis.pdfjsLib = pdfjs;
  const { EventBus, GenericL10n, PDFLinkService, PDFViewer } = await import(`${base}web/pdf_viewer.mjs`);
  const document_ = await pdfjs.getDocument({
    data: new Uint8Array(data), isEvalSupported: false, cMapUrl: `${base}cmaps/`, cMapPacked: true,
    wasmUrl: `${base}wasm/`, iccUrl: `${base}iccs/`,
  }).promise;
  // A phone reads, as in the other document apps, by the screen Leafen says
  // it is on (the frame may stand beside the sidebar, narrower than the
  // window; a Leafen that doesn't say has the device's screen tell a phone,
  // under 600px). A wider touch screen and a narrow window on a computer
  // edit, the viewer being the same either way (document apps decision,
  // 2026-11-14: only a phone reads alone).
  const phone = leafen.context.screen ? leafen.context.screen === "phone"
    : matchMedia("(hover: none) and (pointer: coarse)").matches && Math.min(screen.width, screen.height) < 600;
  const editing = Boolean(file.canEdit) && !phone;
  const eventBus = new EventBus();
  const linkService = new PDFLinkService({ eventBus });
  // pdf.js's own words (a text box's "Start typing…", its editors' and
  // buttons' names) in the person's language (2026-11-14: a Korean screen
  // showed "Start typing…"), from l10n/locale.json, which index.html names.
  const l10n = new GenericL10n(pdfjsLanguage(leafen.context.locale));
  // A note's mark is one of pdf.js's own pictures, next to its viewer
  // (2026-11-15: without the place, every note drew as a broken picture).
  const viewer = new PDFViewer({
    container, eventBus, linkService, l10n, textLayerMode: 1, imageResourcesPath: `${base}web/images/`,
    annotationMode: editing ? pdfjs.AnnotationMode.ENABLE_FORMS : pdfjs.AnnotationMode.ENABLE,
    annotationEditorMode: editing ? pdfjs.AnnotationEditorType.NONE : pdfjs.AnnotationEditorType.DISABLE,
  });
  // Given its words, the viewer leaves translating its container to the
  // page, as pdf.js's own viewer does for the whole document.
  void l10n.translate(container);
  linkService.setViewer(viewer);
  state.hidden = true;
  bar.hidden = false;
  container.hidden = false;
  // The viewer starts under the bar, which wraps on a narrow screen.
  new ResizeObserver(() => requestAnimationFrame(() => {
    document.documentElement.style.setProperty("--bar-height", `${bar.offsetHeight}px`);
  })).observe(bar);
  eventBus.on("pagesinit", () => { viewer.currentScaleValue = innerWidth <= 640 ? "page-width" : "auto"; });
  // The page fits the frame again as it resizes, but on a touch screen not
  // when only its height changed: that is the keyboard, Leafen keeping the
  // frame to what shows above it, and pdf.js commits its editors on every
  // fit — the text box just placed, still empty, went with the keyboard
  // (2026-11-14, iPad Safari).
  const touchScreen = leafen.context.screen ? leafen.context.screen !== "desktop" : matchMedia("(hover: none) and (pointer: coarse)").matches;
  let fittedWidth = innerWidth;
  addEventListener("resize", () => {
    const widthChanged = innerWidth !== fittedWidth;
    fittedWidth = innerWidth;
    if (touchScreen && !widthChanged) return;
    if (viewer.currentScaleValue === "page-width" || viewer.currentScaleValue === "auto") viewer.currentScaleValue = viewer.currentScaleValue;
  });
  const count = document_.numPages;
  // Reading, the status is the page position; editing, the page position has
  // its own place and the status says the save from the first, "저장됨" until
  // something changes (2026-11-07: only PDF said nothing of it on opening).
  const where = (at) => { (editing ? pageAt : status).textContent = fill(say.page, { at, of: count }); };
  eventBus.on("pagechanging", (event) => where(event.pageNumber));
  viewer.setDocument(document_);
  linkService.setDocument(document_, null);
  where(1);
  if (editing) { pageAt.hidden = false; status.textContent = say.saved; }
  // A link outside the document opens in a new tab once the person agrees, not in this frame.
  container.addEventListener("click", (event) => {
    const link = event.target.closest?.("a[href]");
    if (!link || !/^https?:/iu.test(link.href)) return;
    event.preventDefault();
    void leafen.openLink(link.href);
  }, true);
  if (!editing) return;
  // From the first, "nothing unsaved": Leafen then keeps the version for a
  // save with nothing changed (2026-11-07).
  leafen.setUnsaved(false).catch(() => undefined);

  // After the bar's Save pressed with a mouse or entered from the keyboard, the caret is back where
  // it was — in a form field, or in the text box being typed in, which pdf.js puts down as it loses
  // the focus (keeping its own outline focused) and the save opens again — so the next key lands
  // there (2026-11-18: it went nowhere until the page was pressed). Such a press still takes the
  // focus: a text box still being typed in when the save put the tool down was saved with no size.
  // Focus elsewhere stays, but not on pdf.js's own layer, which takes it as the tool goes down and
  // up (2026-11-18: that kept a page's first text box shut).
  // A finger or a pen on Save leaves the focus where it is: Safari on an iPad puts the soft keyboard
  // down as the focus leaves a field and raises it again only for a focus a tap makes, so it went
  // down and up with every save (2026-11-19). A form field keeps the focus, its value saved as
  // typed; a text box being typed in is put down, taken into the file and opened again within the
  // same press (the save below).
  let before = null, touchPress = false;
  bar.addEventListener("pointerdown", () => { if (!bar.contains(document.activeElement)) before = document.activeElement; }, true);
  bar.addEventListener("focusin", (event) => { if (!bar.contains(event.relatedTarget)) before = event.relatedTarget; });
  saveButton.addEventListener("pointerdown", (event) => { touchPress = event.pointerType !== "mouse"; });
  saveButton.addEventListener("mousedown", (event) => { if (touchPress) event.preventDefault(); });
  // The words each text box the save opened again held then, which the save has (typing, below).
  const reopened = new WeakMap();
  const openBox = (box) => {
    // pdf.js opens a text box on a double click; the caret goes after its words.
    box.dispatchEvent(new MouseEvent("dblclick", { bubbles: true }));
    const field = document.activeElement;
    if (!field?.isContentEditable || !box.contains(field)) return;
    reopened.set(field, field.textContent);
    getSelection().selectAllChildren(field);
    getSelection().collapseToEnd();
  };
  // Leafen gives the app's frame the focus back once its question about a newer version is
  // answered (2026-11-19: the next key went to the window's close button). A field that kept the
  // focus through a finger's Save takes it again, which raises Safari's keyboard; otherwise the
  // caret comes back as after any save, once this one ends.
  addEventListener("focus", (event) => {
    if (event.target !== window) return;
    setTimeout(() => {
      const now = document.activeElement;
      if (now !== document.body && container.contains(now) && (now.matches("input, textarea") || now.isContentEditable)) now.focus({ preventScroll: true });
    });
  });
  const caretBack = () => {
    const at = before, now = document.activeElement;
    if (!document.hasFocus() || !at?.isConnected || !container.contains(at)) return;
    const box = at.classList.contains("internal") ? at.closest(".freeTextEditor") : null;
    if (now !== document.body && !bar.contains(now) && !now?.matches(".annotationEditorLayer") && !box?.contains(now)) return;
    if (box) openBox(box);
    else if (at.matches("input, textarea, select")) at.focus({ preventScroll: true });
  };

  // Leafen asks before closing while the file differs from its last save:
  // an edit through the editors' history since then, or forms and
  // annotations other than the save held. Nothing else counts (2026-11-15: a
  // tool coming up turns the page's own text, drawings and highlights into
  // editors, which marked a file with any of them unsaved and asked before
  // closing with nothing to save).
  let edits = 0, savedEdits = 0, storageDiffers = false, unsaved = false;
  // A text box being typed in reaches the document only once it is done, but
  // what is typed is a change from its first letter (2026-11-15: the status
  // said "저장됨" until the box was left, and closing lost the words without asking) — but not the
  // words a box the save opened again still holds as they were.
  const typing = () => [...container.querySelectorAll(".freeTextEditor .internal[contenteditable='true']")]
    .some((box) => box.textContent.trim() !== "" && box.textContent !== reopened.get(box));
  const refresh = () => {
    const now = edits !== savedEdits || storageDiffers || typing();
    const word = now ? say.unsaved : say.saved;
    if (!saveButton.disabled && status.textContent !== word) status.textContent = word;
    if (now !== unsaved) { unsaved = now; leafen.setUnsaved(now).catch(() => undefined); }
  };
  container.addEventListener("input", () => refresh());
  // What the last save held, the file as opened at first: storage touched
  // without a change tells again at the next one (2026-11-05: a tool coming
  // back up touches it).
  let savedHash = document_.annotationStorage.serializable.hash;
  document_.annotationStorage.onSetModified = () => {
    storageDiffers = document_.annotationStorage.serializable.hash !== savedHash;
    if (!storageDiffers) document_.annotationStorage.resetModified();
    refresh();
  };
  // A drawing in progress reaches the document only once its tool is put
  // down, but every stroke, word or move goes through the editors' undo
  // history first: that counts as a change too (2026-10-16: closing right
  // after drawing lost the stroke without asking). An undo or redo with
  // nothing to take back or do again changes nothing.
  let watched = null, states = {};
  eventBus.on("editingstateschanged", ({ source, details }) => {
    states = details ?? states;
    if (!source || source === watched) return;
    watched = source;
    for (const name of ["addCommands", "undo", "redo"]) {
      const own = source[name];
      if (typeof own !== "function") continue;
      source[name] = (...args) => {
        const counts = name === "addCommands" || (name === "undo" ? states.hasSomethingToUndo : states.hasSomethingToRedo);
        const out = own.apply(source, args);
        if (counts) { edits += 1; refresh(); }
        return out;
      };
    }
  });
  // Puts a tool down or up, done once pdf.js says so: down so a drawing in
  // progress is part of what is saved, up again as it was after the save.
  const useTool = (mode) => new Promise((resolve) => {
    if (viewer.annotationEditorMode === mode) return resolve();
    eventBus.on("annotationeditormodechanged", resolve, { once: true });
    viewer.annotationEditorMode = { mode };
  });
  // The tools' names and "Save" are pdf.js's own editor buttons' words, so
  // they come in every language pdf.js has, as its words beside them do
  // (2026-11-14: a German screen had English tool names beside pdf.js's
  // German); Korean takes this app's names (l10n/ko.ftl). "Select" is the app's.
  const [textName, drawName, highlightName, saveName] = await l10n.get(["pdfjs-editor-free-text-button-label",
    "pdfjs-editor-ink-button-label", "pdfjs-editor-highlight-button-label", "pdfjs-save-button-label"]);
  const names = { select: say.select, text: textName, draw: drawName, highlight: highlightName };
  const modes = [["select", pdfjs.AnnotationEditorType.NONE], ["text", pdfjs.AnnotationEditorType.FREETEXT],
    ["draw", pdfjs.AnnotationEditorType.INK], ["highlight", pdfjs.AnnotationEditorType.HIGHLIGHT]];
  tools.replaceChildren(...modes.map(([name, mode]) => {
    const button = document.createElement("button");
    button.type = "button";
    button.dataset.mode = name;
    button.textContent = names[name];
    button.setAttribute("aria-pressed", String(mode === pdfjs.AnnotationEditorType.NONE));
    button.addEventListener("click", () => {
      viewer.annotationEditorMode = { mode };
      for (const other of tools.children) other.setAttribute("aria-pressed", String(other === button));
    });
    return button;
  }));
  tools.hidden = false;
  // iPad Safari raises its keyboard only for a field focused while it handles
  // the touch itself, and pdf.js focuses a new text box on pointerup, which it
  // didn't always take (2026-11-14: the box came, the keyboard didn't). The
  // touch that made a box gives it the focus again as it ends, by way of the
  // box, so pdf.js doesn't take the move for leaving it.
  let focusedAtTouch = null;
  container.addEventListener("touchstart", () => { focusedAtTouch = document.activeElement; }, { passive: true });
  container.addEventListener("touchend", () => {
    const field = document.activeElement;
    const box = field?.isContentEditable ? field.closest(".freeTextEditor") : null;
    if (!box || field === focusedAtTouch) return;
    box.focus({ preventScroll: true });
    field.focus({ preventScroll: true });
  });
  // The keyboard comes up with the tap's own click on the new box, and
  // pdf.js puts a new box in reading order among the page's other boxes a
  // moment after making it: taking it out of the page and back, focus given
  // back later. Safari then drops the tap's click, and on a page that already
  // had text or drawings the box came without the keyboard every time
  // (2026-11-15, iPad Safari). On a touch screen a box being typed in keeps
  // its place until it is done; pdf.js orders it then, as it places it again.
  const ordered = new WeakSet();
  eventBus.on("annotationeditorlayerrendered", ({ source }) => {
    const layer = source.annotationEditorLayer?.annotationEditorLayer;
    if (!touchScreen || !layer || ordered.has(layer)) return;
    ordered.add(layer);
    const order = layer.moveEditorInDOM.bind(layer);
    layer.moveEditorInDOM = (editor) => {
      if (editor.isInEditMode?.() && editor.div?.contains(document.activeElement)) return;
      order(editor);
    };
  });
  saveButton.hidden = false;
  saveButton.textContent = saveName;
  saveButton.addEventListener("click", async () => {
    // The text box a finger or a pen pressed Save over, being typed in.
    const field = touchPress ? document.activeElement : null;
    touchPress = false;
    const typedBox = field?.isContentEditable ? field.closest(".freeTextEditor") : null;
    saveButton.disabled = true;
    status.textContent = say.saving;
    const using = viewer.annotationEditorMode;
    // What the status says after a save that didn't go through.
    let word = null;
    try {
      // pdf.js puts that box down as it loses the focus; the file is taken at once (saveDocument
      // reads the annotations as it is called) and the box opened again in the same press.
      if (typedBox) field.blur();
      else await useTool(pdfjs.AnnotationEditorType.NONE);
      const at = edits;
      const hash = document_.annotationStorage.serializable.hash;
      const saving = document_.saveDocument();
      if (typedBox?.isConnected) openBox(typedBox);
      const bytes = await saving;
      const result = await leafen.saveFile(bytes, { contentType: "application/pdf" });
      if (result.ok) {
        // A change made while saving stays unsaved.
        savedEdits = at;
        savedHash = hash;
        document_.annotationStorage.resetModified();
        storageDiffers = document_.annotationStorage.serializable.hash !== savedHash;
      } else if (result.conflict) {
        // The person settled a newer version in Leafen's own question.
        word = say.unsaved;
      } else {
        // A save that failed says so, not "not saved yet" (2026-10-16).
        word = say.notSaved;
        leafen.notice(say.notSavedNotice);
      }
    } catch {
      word = say.notSaved;
      leafen.notice(say.notSavedNotice);
    } finally {
      saveButton.disabled = false;
      refresh();
      if (word) status.textContent = word;
      // The tool comes back up as it was, and the caret where it was.
      await useTool(using).catch(() => undefined);
      if (!typedBox) caretBack();
    }
  });
}
