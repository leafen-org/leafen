// The first script of ONLYOFFICE's editor pages (the build puts it there).
// It answers the app's request for the document as the editor has it now,
// tells the app of changes to it, and hands pictures put in while editing
// and documents to print to the app. When the editor's
// frame can't reach the app's — both frames without an origin — it also
// stands in for what the editor reads from its parent: a parent that
// forwards messages and answers images by message, no service workers,
// storage kept in memory, and workers started from blob: copies.
(() => {
  const real = window.parent;
  // The editor's status line says changes are saved when the stand-in server
  // has them, not Leafen; the app's bar says whether the file is saved.
  document.head.append(Object.assign(document.createElement("style"), {
    textContent: "#label-action, #status-label-action { visibility: hidden !important; }",
  }));
  // Each change to the document is told to the app at once ({ leafenBridge: "changed" }), once
  // until the document is next taken for a save, from when the app asks ({ leafenBridge: "watch" },
  // once it is open): every change goes through the editor's history — typing, a Korean letter
  // being composed, a paste, a format, undo and redo — and a spreadsheet cell's typing goes
  // through its cell editor until Enter. The editor's own "modified" note, which the app went by
  // before, only says changes are waiting for its stand-in server: it said nothing of a cell typed
  // in before Enter, nor of a change made while others still waited or a letter was being
  // composed — the next letter of a syllable composed on after a save (2026-11-19). A cell's
  // typing left with Escape, or undone in its editor, with nothing through the history since,
  // leaves the document as it was taken last ({ leafenBridge: "unchanged" }).
  let told = false, inHistory = false, watching = false;
  const changed = (throughHistory) => {
    inHistory ||= throughHistory;
    if (told) return;
    told = true;
    real.postMessage({ leafenBridge: "changed" }, "*");
  };
  const unchanged = () => {
    if (!told || inHistory) return;
    told = false;
    real.postMessage({ leafenBridge: "unchanged" }, "*");
  };
  const watch = () => {
    const history = window.AscCommon?.History;
    if (watching || !history || !["Add", "Undo", "Redo"].every((name) => typeof history[name] === "function")) return watching;
    for (const name of ["Add", "Undo", "Redo"]) {
      const own = history[name];
      history[name] = function (...args) {
        const index = this.Index, point = this.Points?.[index], items = point?.Items?.length;
        const out = own.apply(this, args);
        if (this.Index !== index || this.Points?.[this.Index] !== point || point?.Items?.length !== items) changed(true);
        return out;
      };
    }
    const cell = window.editorCell?.wb?.cellEditor;
    for (const name of ["_updateUndoRedoChanged", "close"]) {
      const own = cell?.[name];
      if (typeof own !== "function") continue;
      cell[name] = function (...args) {
        const out = own.apply(this, args);
        if (this.isOpened && this.undoList?.length) changed(false);
        else unchanged();
        return out;
      };
    }
    return (watching = true);
  };
  const adding = new Map();
  const printing = new Map();
  let nextAdd = 0;
  window.addEventListener("message", (event) => {
    const data = event.data;
    if (!data || typeof data !== "object") return;
    if (data.leafenBridge === "watch" && watch()) real.postMessage({ leafenBridge: "watching" }, "*");
    if (data.leafenBridge === "imagesAdded") {
      adding.get(data.id)?.(data.names ?? []);
      adding.delete(data.id);
    }
    if (data.leafenBridge === "printed") {
      printing.get(data.id)?.();
      printing.delete(data.id);
    }
    // Pictures the app kept from data it was given (a pasted picture):
    // addresses of this frame the editor can show them from.
    if (data.leafenBridge === "pictureUrls") {
      real.postMessage({ leafenBridge: "pictureUrlsMade", id: data.id, urls: (data.pictures ?? []).map((picture) => URL.createObjectURL(picture)) }, "*");
    }
    if (data.leafenBridge !== "getFile") return;
    const api = window.editor || window.editorCell;
    try {
      // A cell still being edited ends as the editor's own Save ends it, its value put in first
      // (2026-11-19: Save left it out and said "Saved"). A value the editor turns away — a formula
      // it can't read, one its rules refuse — keeps the cell open with the editor's own message,
      // and the document isn't taken ({ kept }).
      if (typeof api.asc_closeCellEditor === "function" && api.asc_getCellEditMode?.() && !api.asc_closeCellEditor()) {
        real.postMessage({ leafenBridge: "file", id: data.id, kept: true }, "*");
        return;
      }
      const bin = api.asc_nativeGetFile();
      told = inHistory = false;
      real.postMessage({ leafenBridge: "file", id: data.id, bin }, "*");
    } catch (error) {
      real.postMessage({ leafenBridge: "file", id: data.id, error: String(error?.message ?? error) }, "*");
    }
  });
  // The app keeps a picture where the saved file is made from and names it;
  // the editor shows it from a blob: address of this frame that it knows by
  // that name, so the saved document refers to it. CryptPad's build asks its
  // parent's APP for these.
  const addImages = (files) => new Promise((resolve) => {
    const list = [...files], id = ++nextAdd;
    adding.set(id, (names) => resolve(list.flatMap((file, at) => {
      if (!names[at]) return [];
      const url = URL.createObjectURL(file);
      window.AscCommon?.g_oDocumentUrls?.addImageUrl(names[at], url);
      return [{ name: names[at], url }];
    })));
    real.postMessage({ leafenBridge: "addImages", id, files: list }, "*");
  });
  // The font files of each style, by the editor's index of them, named as x2t looks for them.
  const fontStyles = { indexR: "", indexB: "_Bold", indexI: "_Italic", indexBI: "_Bold_Italic" };
  const unknownError = () => window.Asc?.c_oAscError?.ID?.Unknown ?? -1;
  const pictures = {
    AddImage(success, failure) {
      const input = Object.assign(document.createElement("input"), { type: "file", accept: "image/*" });
      input.addEventListener("change", () => {
        void addImages(input.files).then((added) => (added.length ? success(added[0]) : failure?.()));
      }, { once: true });
      input.click();
    },
    UploadImageFiles(files, documentId, userId, jwt, callback) {
      void addImages(files).then((added) => callback(added.length ? undefined : unknownError(), added.map((picture) => picture.url)));
    },
    // Printing (Ctrl+P, the File menu): the editor hands its print rendering
    // to its parent's APP, as in CryptPad's build; the app makes a PDF of it
    // with the document and the fonts it uses, and Leafen prints that
    // outside the app's frame, which can't open a print dialog. The editor
    // then only ends its busy state.
    printPdf(container, callback) {
      const id = ++nextAdd;
      printing.set(id, () => callback(null));
      let bin = null, fonts = [];
      try {
        const api = window.editor || window.editorCell, loader = api.FontLoader;
        bin = api.asc_nativeGetFile();
        fonts = loader.fontInfos.filter((font) => font.NeedStyles).flatMap((font) => Object.entries(fontStyles).flatMap(([index, suffix]) =>
          (typeof font[index] === "number" && font[index] !== -1 ? [{ name: `${font.Name}${suffix}.ttf`, file: loader.fontFiles[font[index]].Id }] : [])));
      } catch { /* the app says it couldn't print */ }
      real.postMessage({ leafenBridge: "print", id, data: container?.data ?? null, bin, fonts }, "*");
    },
  };
  // A spreadsheet cell being typed in stays in sight when the editor's frame
  // gets shorter, as when a tablet's keyboard rises over its lower half
  // (2026-11-19, Chrome on an Android tablet: the grid kept its top rows and
  // the cell typed in at row 10 went out of sight, its letters landing
  // unseen). Once the editor has laid itself out for the new size, the sheet
  // scrolls to the cell as its arrow keys do and the cell's editor moves with
  // it; a cell only chosen is left where the reader put it.
  let inSight = 0;
  window.addEventListener("resize", () => {
    clearTimeout(inSight);
    inSight = setTimeout(() => {
      const book = window.editorCell?.wb, sheet = book?.getWorksheet?.();
      if (!book?.cellEditor?.isOpened || typeof sheet?._scrollToRange !== "function") return;
      sheet._scrollToRange();
      book.cellEditor.move?.();
    }, 150);
  });
  // The browser can lose what the editor drew on its canvases and hand them
  // back blank (contextrestored): Chrome on an Android tablet did at its first
  // editor after a fresh start, its graphics contexts lost and restored three
  // times while the sheet first drew (2026-11-19: the grid stayed empty, the
  // formula bar and sheet tabs drawn, until the frame was resized), and the
  // editor draws only when something in it changes. Once a burst of them is
  // back, the editor draws everything again, as its own full repaint does: a
  // sheet's grid with its pictures and selection, and the cell being typed in;
  // a document's or a slide's page, and a presentation's slide list and notes,
  // which draw apart from it.
  let redraw = 0;
  document.addEventListener("contextrestored", () => {
    clearTimeout(redraw);
    redraw = setTimeout(() => {
      try {
        const book = window.editorCell?.wb;
        if (book) {
          book.drawWS();
          if (book.cellEditor?.isOpened) book.cellEditor._draw?.();
          return;
        }
        const page = window.editor?.WordControl;
        page?.OnRePaintAttack?.();
        page?.Thumbnails?.ClearCacheAttack?.();
        page?.Thumbnails?.OnUpdateOverlay?.();
        page?.m_oNotesApi?.OnPaint?.();
      } catch { /* the next change draws it */ }
    }, 100);
  }, true);
  let apart = false;
  try { void real.APP; } catch { apart = true; }
  if (!apart) {
    Object.assign(real.APP ??= {}, pictures);
    return;
  }
  // A formatted paste: the editor writes the pasted HTML into a frame it
  // makes and reads it back, which can't reach a frame of its own here. It
  // reads it from a shadow root of this page instead — the paste takes
  // computed styles from the node's own window — with inherited styles reset
  // as in a new page, and the paste always ends.
  function pasteInPage(html, text, callback) {
    let host = document.getElementById("leafen-paste");
    if (!host) {
      host = Object.assign(document.createElement("div"), { id: "leafen-paste" });
      host.style.cssText = "all: initial; position: absolute; top: -100px; left: 0; width: 10000px; height: 100px; overflow: hidden; z-index: -1000;";
      document.body.append(host);
      host.attachShadow({ mode: "open" });
    }
    const root = document.createElement("div");
    root.innerHTML = html;
    host.shadowRoot.replaceChildren(root);
    try {
      if (callback) {
        callback(root, text, host);
      } else {
        // Where it came from, as the editor tells by the page's ProgId.
        const common = window.AscCommon, from = root.querySelector('meta[name="ProgId"]')?.getAttribute("content") ?? "";
        const kind = ["Word", "Excel", "PowerPoint"].find((name) => from.startsWith(name));
        this.pastedFrom = kind ? common.c_oClipboardPastedFrom[kind] : null;
        this.Api.asc_PasteData(common.c_oAscClipboardDataFormat.HtmlElement, root, host, text);
      }
    } finally {
      window.AscCommon.g_clipboardBase.Paste_End();
    }
  }
  const patchPaste = () => {
    const base = window.AscCommon?.g_clipboardBase;
    if (base && base.CommonIframe_PasteStart !== pasteInPage) base.CommonIframe_PasteStart = pasteInPage;
  };
  for (const type of ["keydown", "paste", "drop"]) window.addEventListener(type, patchPaste, true);
  try { delete Navigator.prototype.serviceWorker; } catch { /* already gone */ }
  const memoryStorage = () => {
    const map = new Map();
    return {
      get length() { return map.size; }, key: (index) => [...map.keys()][index] ?? null,
      getItem: (key) => (map.has(String(key)) ? map.get(String(key)) : null), setItem: (key, value) => { map.set(String(key), String(value)); },
      removeItem: (key) => { map.delete(String(key)); }, clear: () => map.clear(),
    };
  };
  for (const name of ["localStorage", "sessionStorage"]) {
    try { void window[name]; } catch { Object.defineProperty(window, name, { value: memoryStorage(), configurable: true }); }
  }
  const NativeWorker = window.Worker;
  const BlobWorker = function (url, options) {
    try {
      return new NativeWorker(url, options);
    } catch {
      const address = new URL(url, location.href).href;
      const base = JSON.stringify(address.slice(0, address.lastIndexOf("/") + 1));
      const source = `self.Module=Object.assign(self.Module||{},{locateFile:(p)=>${base}+p});importScripts(${JSON.stringify(address)})`;
      return new NativeWorker(URL.createObjectURL(new Blob([source], { type: "text/javascript" })), options);
    }
  };
  BlobWorker.prototype = NativeWorker.prototype;
  window.Worker = BlobWorker;
  const pending = new Map();
  let next = 0;
  const APP = {
    ...pictures,
    urlArgs: "",
    changeTheme() {},
    getImageURL(name, callback) { const id = ++next; pending.set(id, callback); real.postMessage({ leafenBridge: "image", id, name }, "*"); },
  };
  const stand = new Proxy({}, {
    get(_, key) {
      if (key === "APP") return APP;
      const value = real[key];
      return typeof value === "function" ? value.bind(real) : value;
    },
  });
  try { window.parent = stand; } catch { /* the editor reaches the real parent */ }
  Object.defineProperty(window, "parentOrigin", { get: () => "null", set() {}, configurable: true });
  window.addEventListener("message", (event) => {
    const data = event.data;
    if (data && typeof data === "object" && data.leafenBridge === "imageUrl") {
      const callback = pending.get(data.id);
      pending.delete(data.id);
      callback?.(data.url);
    }
  });
})();
