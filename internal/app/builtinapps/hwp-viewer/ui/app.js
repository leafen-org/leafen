// Opens a Hangul (HWP or HWPX) document a person opened from its file block,
// with rhwp (ui/vendor, MIT). Someone who may change the page edits it in
// rhwp-studio on a screen wider than a phone's — on a touch screen once they
// choose to, after reading — in the frame with no origin every app opens in,
// where the studio's bridge takes the handshake, and saving puts it back in
// its own format: an HWP that rhwp can't write back whole goes as HWPX, with
// a notice (document apps decision of 2026-10-10, 11, and 2026-11-14).
// Otherwise, or when the studio doesn't start, @rhwp/core draws its pages to
// read.
import { connect } from "./leafen-app-sdk.js";

const VERSION = "0.8.6";
const STUDIO_WAIT_MS = 30_000;
const vendor = new URL("./vendor/", import.meta.url);
const studioUrl = new URL(`rhwp-studio-${VERSION}/index.html`, vendor).href;
const wasmUrl = new URL(`rhwp-studio-${VERSION}/assets/rhwp_bg.wasm`, vendor).href;
const root = document.getElementById("document");
const state = document.getElementById("state");
const bar = document.getElementById("bar");
const status = document.getElementById("status");
const saveButton = document.getElementById("save");
const editInstead = document.getElementById("editInstead");
const words = {
  en: {
    loading: "Opening the document…", failed: "This document couldn't be opened.", none: "Press an HWP or HWPX file on a page to open it here.",
    page: (at, of) => `Page ${at} of ${of}`, save: "Save", saving: "Saving…", saved: "Saved", unsaved: "Not saved yet",
    notSaved: "Couldn't save", notSavedNotice: "Couldn't save the file. Try again.", savedAsHwpx: "This HWP couldn't be written back whole, so it was saved as HWPX.",
    noEditor: "The editor couldn't start here, so the document is shown to read.",
    touch: "Shown to read first on a touch screen.", edit: "Open in the editor",
  },
  ko: {
    loading: "문서를 여는 중…", failed: "이 문서를 열지 못했습니다.", none: "페이지의 HWP·HWPX 파일을 누르면 여기서 열립니다.",
    page: (at, of) => `${at} / ${of}쪽`, save: "저장", saving: "저장 중…", saved: "저장됨", unsaved: "아직 저장하지 않음",
    notSaved: "저장하지 못함", notSavedNotice: "파일을 저장하지 못했습니다. 다시 시도하세요.", savedAsHwpx: "이 HWP는 그대로 다시 쓸 수 없어 HWPX로 저장했습니다.",
    noEditor: "편집기를 열지 못해 읽기만 할 수 있게 보여 줍니다.",
    touch: "터치 화면이라 먼저 읽기만 할 수 있게 보여 줍니다.", edit: "편집기로 열기",
  },
};
let say = words.en;
// After the bar's Save pressed with a mouse (the press takes the focus, ending what is being typed),
// or once the editor "편집기로 열기" opened is ready, the editor's caret has the
// focus again — its frame, and in it the input the studio types through
// (leafen-bridge.js) — so the next key lands there (2026-11-18: it went
// nowhere until the document was pressed). Focus elsewhere in Leafen stays.
const focusEditor = () => {
  const away = document.activeElement === document.body || bar.contains(document.activeElement);
  const frame = document.querySelector("#editor iframe");
  if (!document.hasFocus() || !away || !frame) return;
  frame.focus();
  frame.contentWindow?.postMessage({ leafenStudio: "focus" }, "*");
};
// A finger or a pen on the bar's Save leaves the studio its focus: Safari on an iPad puts the soft
// keyboard down as the focus leaves the studio's frame and raises it again only for a focus a tap
// makes, so the next letter after the save waited for a tap on the document (2026-11-19). The
// studio keeps what is being typed in its document, so the save holds it as shown and typing goes
// on. A mouse press still takes the focus and gets the caret back after the save.
let touchPress = false;
saveButton.addEventListener("pointerdown", (event) => { touchPress = event.pointerType !== "mouse"; });
saveButton.addEventListener("mousedown", (event) => { if (touchPress) event.preventDefault(); });
// Leafen gives the app's frame the focus back once its question about a newer version is
// answered (2026-11-19: the next key went to the window's close button). The focus that comes to
// the frame as a whole — nothing of the app's own has it, Save is busy saving, or the studio's
// frame still is the focused element, as a finger's Save leaves it — goes on to the studio's
// caret at once, so the next key lands there even before the save ends.
addEventListener("focus", (event) => {
  if (event.target !== window) return;
  setTimeout(() => {
    const frame = document.querySelector("#editor iframe"), now = document.activeElement;
    if (!frame || !(now === document.body || now === frame || now === saveButton && saveButton.disabled)) return;
    if (now === frame) frame.blur();
    focusEditor();
  });
});

try {
  const leafen = await connect();
  say = String(leafen.context.locale).startsWith("ko") ? words.ko : words.en;
  document.documentElement.dataset.theme = leafen.context.theme;
  leafen.onTheme((theme) => { document.documentElement.dataset.theme = theme; });
  const file = leafen.context.file;
  if (!file) {
    state.textContent = say.none;
  } else {
    state.textContent = say.loading;
    const read = await leafen.api("GET", `/files/${encodeURIComponent(file.fileId)}`);
    if (read.status !== 200 || !(read.body instanceof ArrayBuffer)) throw new Error(`read ${read.status}`);
    const bytes = new Uint8Array(read.body);
    // A phone reads, by the screen Leafen says it is on: the frame's own width
    // doesn't say, as it may stand beside the sidebar (2026-11-14: 534px of an
    // 834px iPad). A wider touch screen reads first and offers the editor
    // (document apps decision, 2026-11-14: only a phone reads alone); a narrow
    // window on a computer edits as a wide one does (a 768px window with the
    // sidebar left the frame 498px, read only, 2026-11-06). A Leafen that
    // doesn't say has the device's screen tell a phone (under 600px).
    const touch = leafen.context.screen ? leafen.context.screen !== "desktop" : matchMedia("(hover: none) and (pointer: coarse)").matches;
    const phone = leafen.context.screen ? leafen.context.screen === "phone" : touch && Math.min(screen.width, screen.height) < 600;
    // The editor, or reading again with why when it doesn't start.
    const editing = async () => {
      try {
        await edit(leafen, file, bytes);
        return true;
      } catch (error) {
        state.dataset.error = String(error?.message ?? error);
        document.getElementById("editor").hidden = true;
        document.getElementById("editor").replaceChildren();
        root.hidden = false;
        return false;
      }
    };
    if (file.canEdit && touch && !phone) {
      await view(bytes, say.touch, async () => {
        if (!await editing()) await view(bytes, say.noEditor);
      });
    } else if (!(file.canEdit && !touch && await editing())) {
      await view(bytes, file.canEdit && !phone ? say.noEditor : "");
    }
  }
} catch (error) {
  bar.hidden = true;
  document.getElementById("editor").hidden = true;
  root.hidden = false;
  root.replaceChildren(state);
  state.textContent = say.failed;
  state.dataset.error = String(error?.message ?? error);
}

// --- Reading -------------------------------------------------------------

// Draws the pages to read, with a note on why in the bar and, given
// `editLater`, "편집기로 열기", which puts the pages away for the editor.
async function view(bytes, why, editLater) {
  const rhwp = await import(new URL(`rhwp-core-${VERSION}/rhwp.js`, vendor).href);
  // rhwp lays text out by widths the page measures.
  const context = document.createElement("canvas").getContext("2d");
  globalThis.measureTextWidth = (font, text) => {
    if (context.font !== font) context.font = font;
    return context.measureText(text).width;
  };
  await rhwp.default({ module_or_path: wasmUrl });
  const doc = new rhwp.HwpDocument(bytes);
  const count = doc.pageCount();
  state.remove();
  root.replaceChildren();
  root.classList.add("pages");
  bar.hidden = false;
  status.textContent = say.page(1, count);
  bar.querySelector(".note")?.remove();
  if (why) {
    const note = document.createElement("span");
    note.className = "note";
    note.textContent = why;
    editInstead.before(note);
  }
  editInstead.hidden = !editLater;
  editInstead.textContent = say.edit;
  editInstead.onclick = editLater ? () => {
    editInstead.hidden = true;
    bar.querySelector(".note")?.remove();
    removeEventListener("scroll", onScroll);
    near.disconnect();
    root.replaceChildren();
    void editLater().then(focusEditor);
  } : null;
  const pages = [];
  for (let index = 0; index < count; index += 1) {
    const page = document.createElement("div");
    page.className = "page";
    page.dataset.page = String(index + 1);
    pages.push(page);
  }
  root.append(...pages);
  // Pages draw as they come near.
  const near = new IntersectionObserver((entries) => {
    for (const entry of entries) {
      if (!entry.isIntersecting || entry.target.dataset.drawn) continue;
      entry.target.dataset.drawn = "1";
      entry.target.replaceChildren(svgOf(doc.renderPageSvg(Number(entry.target.dataset.page) - 1)));
    }
  }, { rootMargin: "1200px 0px" });
  for (const page of pages) near.observe(page);
  let pending = false;
  function onScroll() {
    if (pending) return;
    pending = true;
    requestAnimationFrame(() => {
      pending = false;
      if (!root.contains(pages[0])) return;
      const top = bar.getBoundingClientRect().bottom + 8;
      const at = pages.find((page) => page.getBoundingClientRect().bottom > top);
      status.textContent = say.page(Number(at?.dataset.page ?? count), count);
    });
  }
  addEventListener("scroll", onScroll, { passive: true });
}

// svgOf takes rhwp's drawing of a page as SVG elements, without anything that
// could run.
function svgOf(markup) {
  const parsed = new DOMParser().parseFromString(markup, "image/svg+xml");
  for (const element of parsed.querySelectorAll("script, foreignObject")) element.remove();
  for (const element of parsed.querySelectorAll("*")) {
    for (const attribute of [...element.attributes]) {
      if (/^on/iu.test(attribute.name) || /^\s*javascript:/iu.test(attribute.value)) element.removeAttribute(attribute.name);
    }
  }
  const svg = document.importNode(parsed.documentElement, true);
  svg.removeAttribute("height");
  svg.setAttribute("width", "100%");
  return svg;
}

// --- Editing -------------------------------------------------------------

// openStudio is @rhwp/editor's createEditor, but for a frame with no origin:
// the studio's frame has none either, so the handshake goes to it without
// naming an origin (its bridge takes it), and the rest travels on the
// handshake's ports.
async function openStudio(host) {
  const { RhwpEditor } = await import(new URL(`rhwp-editor-${VERSION}/index.js`, vendor).href);
  const { EditorTransport } = await import(new URL(`rhwp-editor-${VERSION}/transport.js`, vendor).href);
  const address = new URL(studioUrl);
  address.searchParams.set("renderer", "canvas2d");
  const frame = document.createElement("iframe");
  frame.src = address.href;
  frame.style.cssText = "width: 100%; height: 100%; border: none;";
  frame.allow = "clipboard-read; clipboard-write";
  host.append(frame);
  await new Promise((resolve) => frame.addEventListener("load", resolve, { once: true }));
  const transport = new EditorTransport(frame, address.href, {});
  transport._targetOrigin = "*";
  try {
    // A studio that doesn't answer gives way to reading before long.
    return await Promise.race([
      (async () => {
        await transport.connect();
        const editor = new RhwpEditor(frame, transport);
        await editor._waitReady();
        return editor;
      })(),
      new Promise((_, reject) => setTimeout(() => reject(new Error("the editor didn't start in time")), STUDIO_WAIT_MS)),
    ]);
  } catch (error) {
    transport.destroy();
    frame.remove();
    throw error;
  }
}

async function edit(leafen, file, bytes) {
  const host = document.getElementById("editor");
  host.hidden = false;
  root.hidden = true;
  // The bar takes its room first: appearing after the page is laid out, it
  // pushed the page's first line under the ruler.
  bar.hidden = false;
  saveButton.hidden = true;
  status.textContent = say.loading;
  const editor = await openStudio(host);
  await editor.loadFile(bytes.slice().buffer, file.name);
  // An HWPX file, or an HWP once saved as HWPX, stays HWPX.
  let hwpx = bytes[0] === 0x50 && bytes[1] === 0x4b;
  saveButton.hidden = false;
  saveButton.textContent = say.save;
  status.textContent = say.saved;
  // Whether there is anything unsaved: the studio counts every change.
  const changes = async () => (await editor.getDocumentState().catch(() => null))?.changeSeq;
  let savedAt = await changes();
  let failedAt;
  // Leafen asks before closing while there are unsaved changes.
  // From the first, "nothing": Leafen then keeps the version for a save with
  // nothing changed (2026-11-07).
  let toldUnsaved = null;
  const tellUnsaved = (unsaved) => {
    if (unsaved === toldUnsaved) return;
    toldUnsaved = unsaved;
    leafen.setUnsaved(unsaved).catch(() => undefined);
  };
  tellUnsaved(false);
  const look = async () => {
    const now = await changes();
    const unsaved = now !== undefined && now !== savedAt;
    if (saveButton.disabled) {
      // While saving only a change is told; the save says the rest.
      if (unsaved && now !== saving) tellUnsaved(true);
      return;
    }
    // A save that failed says so until something changes again.
    if (unsaved && now !== failedAt) status.textContent = say.unsaved;
    tellUnsaved(unsaved);
  };
  setInterval(() => void look(), 700);
  // The studio tells each press that may change the document as it comes
  // (leafen-bridge.js): typing or taking text out counts as unsaved at once,
  // and a look follows to see what it did; another press is looked at now,
  // unless something is unsaved already (2026-11-12: closing right after
  // typing lost the typing without a question).
  let soon = 0;
  addEventListener("message", (event) => {
    if (event.source !== editor.element.contentWindow || event.data?.leafenStudio !== "input") return;
    if (event.data.edit === true) tellUnsaved(true);
    else if (toldUnsaved === true) return;
    clearTimeout(soon);
    soon = setTimeout(() => void look(), event.data.edit === true ? 250 : 0);
  });
  // What the save in progress holds.
  let saving;
  saveButton.addEventListener("click", async () => {
    saveButton.disabled = true;
    status.textContent = say.saving;
    saving = await changes();
    // Leafen keeps the version when the app says nothing is unsaved, so the
    // app says so now rather than at its next look (2026-11-07).
    tellUnsaved(saving !== undefined && saving !== savedAt);
    try {
      let out, type = hwpx ? "application/hwp+zip" : "application/x-hwp", asHwpx = false;
      if (hwpx) {
        out = await editor.exportHwpx();
      } else {
        // An HWP goes back as HWP only when rhwp reads its own writing back whole.
        const verify = await editor.exportHwpVerify().catch(() => null);
        if (verify?.recovered && verify.pageCountBefore === verify.pageCountAfter) {
          out = await editor.exportHwp();
        } else {
          out = await editor.exportHwpx();
          type = "application/hwp+zip";
          asHwpx = true;
        }
      }
      // The file keeps the block's name, but for the extension a change of format takes.
      const renamed = asHwpx ? `${file.name.replace(/\.hwp$/iu, "")}.hwpx` : undefined;
      const result = await leafen.saveFile(out, renamed ? { name: renamed, contentType: type } : { contentType: type });
      if (result.ok) {
        savedAt = saving;
        // A change made while saving stays unsaved.
        const now = await changes();
        tellUnsaved(now !== undefined && now !== savedAt);
        if (asHwpx) hwpx = true;
        // A copy kept is the file in view from now on, by its own name.
        await editor.notifySaved(renamed ?? result.copied?.name).catch(() => undefined);
        status.textContent = toldUnsaved ? say.unsaved : say.saved;
        if (asHwpx) leafen.notice(say.savedAsHwpx);
      } else if (result.conflict) {
        // The person settled a newer version in Leafen's own question.
        status.textContent = say.unsaved;
      } else {
        // A save that failed says so, not "not saved yet" (2026-10-16).
        failedAt = saving;
        status.textContent = say.notSaved;
        leafen.notice(say.notSavedNotice);
      }
    } catch {
      failedAt = saving;
      status.textContent = say.notSaved;
      leafen.notice(say.notSavedNotice);
    } finally {
      saveButton.disabled = false;
      focusEditor();
    }
  });
}
