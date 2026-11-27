// Opens a Word, Excel or PowerPoint file (docx, xlsx, pptx) a person opened
// from its file block, in ONLYOFFICE's editor as CryptPad builds it to run
// without a document server (ui/vendor, AGPL-3.0): x2t (WebAssembly, x2t.js)
// turns the file into the editor's own form and back, and a stand-in for the
// document server answers the editor as for one person editing alone.
// Someone who may change the page edits it, and saving puts it back in its
// own format through Leafen's save, which asks how to keep it when someone
// saved first (document apps decision of 2026-10-10, 11). A phone, and a file
// the editor couldn't open, get the light view (light.js) to read instead; a
// wider touch screen, a device short of memory and a narrow window get it
// first, with the editor a press away.
import { connect } from "./leafen-app-sdk.js";
import { lightWords, script, showLight } from "./light.js";
import { sharedInlineStrings } from "./xlsx-strings.js";
import { convert, loadX2t, printPdf } from "./x2t.js";

const ONLYOFFICE = "9.3.2";
const vendor = new URL("./vendor/", import.meta.url);
const state = document.getElementById("state");
const bar = document.getElementById("bar");
const status = document.getElementById("status");
const saveButton = document.getElementById("save");
const editInstead = document.getElementById("editInstead");
// After the bar's Save pressed with a mouse (the press takes the focus, ending what is being
// typed), or once the editor "Open in the editor" opened is ready, the editor's frame gives its
// caret the focus again, so the next key lands there as after its Ctrl+S (2026-11-18: it went
// nowhere). The document and presentation editors also take it through their own grabFocus: with
// the frame focused alone they showed the next letter but left it out of the document. The
// spreadsheet editor isn't asked: its grabFocus throws inside its frame on every call (ONLYOFFICE
// 9.3.2's spreadsheet DocumentHolder controller has no view to focus, 2026-11-19), and the cell
// selected takes the next key with the frame focused alone. Other focus stays.
let grabFocus = null;
const focusEditor = () => {
  const away = document.activeElement === document.body || bar.contains(document.activeElement);
  if (document.hasFocus() && away) { document.querySelector('iframe[name="frameEditor"]')?.focus(); grabFocus?.(); }
};
// A finger or a pen on the bar's Save leaves the editor its focus: Safari on an iPad puts the soft
// keyboard down as the focus leaves the editor's frame and raises it again only for a focus a tap
// makes, so the caret given back after the save brought it back only when the save was quick and
// the next letter otherwise waited for a tap on the document (2026-11-19). The editor keeps what is
// being typed in its document, so the save holds it as shown and typing goes on; a spreadsheet cell
// being typed in ends with its value put in, as the editor's own Save ends it (bridge.js). A mouse
// press still takes the focus, ending what is being typed, and gets the caret back after the save.
let touchPress = false;
saveButton.addEventListener("pointerdown", (event) => { touchPress = event.pointerType !== "mouse"; });
saveButton.addEventListener("mousedown", (event) => { if (touchPress) event.preventDefault(); });
// Leafen gives the app's frame the focus back once its question about a newer version is
// answered (2026-11-19: the next key went to the window's close button). The focus that comes to
// the frame as a whole — nothing of the app's own has it, Save is busy saving, or the editor's
// frame still is the focused element, as a finger's Save leaves it — goes on to the editor's
// caret at once, so the next key lands there even before the save ends.
addEventListener("focus", (event) => {
  if (event.target !== window) return;
  setTimeout(() => {
    const frame = document.querySelector('iframe[name="frameEditor"]'), now = document.activeElement;
    if (!frame || !(now === document.body || now === frame || now === saveButton && saveButton.disabled)) return;
    if (now === frame) frame.blur();
    focusEditor();
  });
});
// The editor takes about 600 MB; below this a device reads with the light view first.
const EDITOR_MEMORY_GB = 4;
const EDITOR_WAIT_MS = 90_000;
const kinds = { docx: "word", xlsx: "cell", pptx: "slide" };
// Pictures the editor takes, by type, and the extension they're kept under.
const pictureTypes = { "image/png": "png", "image/jpeg": "jpg", "image/gif": "gif", "image/bmp": "bmp", "image/tiff": "tif", "image/svg+xml": "svg" };
// The document server's answers to the editor's own Save and to a picture
// by address (sdkjs c_oAscServerCommandErrors, c_oAscForceSaveTypes,
// c_oAscServerError).
const SERVER_NO_ERROR = 0, SERVER_NOT_MODIFIED = 4, FORCE_SAVE_BUTTON = 1, FORCE_SAVE_TIME = -2, SERVER_UPLOAD_URL = -104;
const contentTypes = {
  docx: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
  xlsx: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
  pptx: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
};
const words = {
  en: {
    loading: "Opening the document…", failed: "This document couldn't be opened.", none: "Press a docx, xlsx or pptx file on a page to open it here.",
    save: "Save", saving: "Saving…", saved: "Saved", unsaved: "Not saved yet", notSaved: "Couldn't save", notSavedNotice: "Couldn't save the file. Try again.", printFailed: "Couldn't print", reading: "Read only",
    fallback: "The editor couldn't open this file here, so it's shown to read.", light: "Shown to read on this device.", narrow: "Shown to read in this narrow window.", touch: "Shown to read first on a touch screen.", edit: "Open in the editor",
    ...lightWords.en,
  },
  ko: {
    loading: "문서를 여는 중…", failed: "이 문서를 열지 못했습니다.", none: "페이지의 docx·xlsx·pptx 파일을 누르면 여기서 열립니다.",
    save: "저장", saving: "저장 중…", saved: "저장됨", unsaved: "아직 저장하지 않음", notSaved: "저장하지 못함", notSavedNotice: "파일을 저장하지 못했습니다. 다시 시도하세요.", printFailed: "인쇄하지 못함", reading: "읽기 전용",
    fallback: "편집기로 열지 못해 읽기만 할 수 있게 보여 줍니다.", light: "이 기기에서는 읽기만 할 수 있게 보여 줍니다.", narrow: "창이 좁아 읽기만 할 수 있게 보여 줍니다.", touch: "터치 화면이라 먼저 읽기만 할 수 있게 보여 줍니다.", edit: "편집기로 열기",
    ...lightWords.ko,
  },
};
let say = words.en;

try {
  const leafen = await connect();
  const korean = String(leafen.context.locale).startsWith("ko");
  say = korean ? words.ko : words.en;
  document.documentElement.dataset.theme = leafen.context.theme;
  const file = leafen.context.file;
  if (!file) {
    state.textContent = say.none;
  } else {
    state.textContent = say.loading;
    const ext = /\.(docx|xlsx|pptx)$/iu.exec(file.name)?.[1]?.toLowerCase()
      ?? Object.keys(contentTypes).find((key) => contentTypes[key] === file.contentType);
    if (!ext) throw new Error("not an Office file");
    const read = await leafen.api("GET", `/files/${encodeURIComponent(file.fileId)}`);
    if (read.status !== 200 || !(read.body instanceof ArrayBuffer)) throw new Error(`read ${read.status}`);
    const bytes = new Uint8Array(read.body);
    const light = (note, edit) => {
      bar.hidden = false;
      status.textContent = note;
      state.hidden = true;
      if (edit) {
        editInstead.hidden = false;
        editInstead.textContent = say.edit;
        editInstead.onclick = () => {
          editInstead.hidden = true;
          document.getElementById("light").replaceChildren();
          document.getElementById("light").hidden = true;
          document.getElementById("sheets").hidden = true;
          void edit().then(focusEditor);
        };
      }
      return showLight(ext, bytes, say, { root: document.getElementById("light"), tabs: document.getElementById("sheets"),
        place: (text) => { status.textContent = `${note} · ${text}`; } });
    };
    const editor = async () => {
      try {
        const x2t = await loadX2t(EDITOR_WAIT_MS);
        const given = ext === "xlsx" ? await sharedInlineStrings(bytes, script) : bytes;
        const opened = convert(x2t, given, `input.${ext}`, "bin");
        if (opened.code !== 0) throw new Error(`x2t ${opened.code}`);
        await open(leafen, file, ext, x2t, opened.data, file.canEdit, korean);
      } catch (error) {
        // The editor didn't open: the light view reads the file instead.
        delete document.getElementById("editor").dataset.opening;
        document.getElementById("editor").hidden = true;
        document.getElementById("editor").replaceChildren(Object.assign(document.createElement("div"), { id: "placeholder" }));
        saveButton.hidden = true;
        state.dataset.error = String(error?.message ?? error);
        await light(say.fallback);
      }
    };
    // A phone reads. A wider touch screen reads first and offers the editor
    // (document apps decision, 2026-11-14: only a phone reads alone), as a
    // computer short of memory and a narrow window on a computer do: a 768px
    // window with the sidebar left the frame 498px and the editor out of
    // reach behind "Read only" (2026-11-06).
    const kind = screenKind(leafen.context);
    const narrow = matchMedia("(max-width: 640px)").matches;
    const short = navigator.deviceMemory !== undefined && navigator.deviceMemory < EDITOR_MEMORY_GB;
    if (kind === "phone") await light(say.reading);
    else if (short) await light(say.light, editor);
    else if (kind === "touch") await light(say.touch, editor);
    else if (narrow) await light(say.narrow, editor);
    else await editor();
  }
} catch (error) {
  bar.hidden = true;
  document.getElementById("editor").hidden = true;
  state.hidden = false;
  state.textContent = say.failed;
  state.dataset.error = String(error?.message ?? error);
}

// The screen Leafen says it is on (context.screen): a phone, a wider touch
// screen or a desktop. The frame's own width doesn't say, as it may stand
// beside the sidebar (2026-11-14: 534px of an 834px iPad); a Leafen that
// doesn't say has the device's screen tell a phone (under 600px) instead.
function screenKind(context) {
  if (context.screen === "phone" || context.screen === "touch" || context.screen === "desktop") return context.screen;
  if (!matchMedia("(hover: none) and (pointer: coarse)").matches) return "desktop";
  return Math.min(screen.width, screen.height) < 600 ? "phone" : "touch";
}

// A picture given as a data: address, or null.
function dataPicture(address) {
  const parts = /^data:(image\/[\w.+-]+);base64,(.*)$/su.exec(String(address));
  if (!parts || !(parts[1] in pictureTypes)) return null;
  try {
    return new Blob([Uint8Array.from(atob(parts[2]), (c) => c.charCodeAt(0))], { type: parts[1] });
  } catch {
    return null;
  }
}

function dataUrl(bytes, type = "application/octet-stream") {
  return new Promise((resolve) => {
    const reader = new FileReader();
    reader.onload = () => resolve(reader.result);
    reader.readAsDataURL(new Blob([bytes], { type }));
  });
}

async function open(leafen, file, ext, x2t, bin, editing, korean) {
  // The pictures the document holds, as the editor asks for them, and those
  // put in while editing (kept in x2t's media folder, where the saved file
  // takes them from).
  const images = {};
  for (const name of x2t.FS.readdir("/working/media")) {
    if (name !== "." && name !== "..") images[`media/${name}`] = await dataUrl(x2t.FS.readFile(`/working/media/${name}`));
  }
  const image = (name) => images[name] ?? images[`media/${name}`] ?? "";
  let added = 0;
  // Each picture's name, or null for what isn't one.
  const addImages = async (files) => {
    const names = [];
    for (const picture of files ?? []) {
      const type = picture instanceof Blob ? picture.type : "";
      if (!(type in pictureTypes)) {
        names.push(null);
        continue;
      }
      const bytes = new Uint8Array(await picture.arrayBuffer());
      const name = `leafen-${Date.now().toString(36)}-${++added}.${pictureTypes[type]}`;
      x2t.FS.writeFile(`/working/media/${name}`, bytes);
      images[`media/${name}`] = await dataUrl(bytes, type);
      names.push(name);
    }
    return names;
  };
  const host = document.getElementById("editor");
  // In a dark Leafen the line saying the document is opening stays over the
  // editor until the document is drawn: the editor's own loading skeleton
  // and the page it holds the document's place with are light whatever theme
  // it is given — a white toolbar, a pale canvas, a white page (2026-11-15:
  // two to eight seconds of white between the dark line and the dark editor,
  // at every open), and its "ready" comes before the skeleton goes. A light
  // Leafen shows the skeleton as before.
  if (leafen.context.theme === "dark") host.dataset.opening = say.loading;
  host.hidden = false;
  state.hidden = true;
  bar.hidden = false;
  status.textContent = editing ? say.saved : say.reading;
  // One person editing alone, as the editor's document server would answer.
  // The editor calls itself by its user's id with its index after it (sdkjs
  // DocsCoApi: _userId = user id + indexUser) and takes a participant or a
  // lock under any other name for someone else's, so the participant and
  // every lock carry that one name (2026-11-18: under another, a paragraph
  // locked before a save showed a red "Me" after it and took no more typing).
  const person = { id: "u1", name: "Me" }, indexUser = 1;
  const me = { id: `${person.id}${indexUser}`, idOriginal: person.id, username: person.name, indexUser, connectionId: `conn-${person.id}`, isCloseCoAuthoring: false, view: !editing };
  // Changes counted, those the last saved file holds and those the document last taken held.
  let changes = 0, savedAt = 0, takenAt = 0, index = 0;
  // Leafen asks before closing while there are unsaved changes.
  // It says so from the first, "nothing", so Leafen keeps the version for a
  // save with nothing changed (2026-11-07: such a save met others with
  // "someone saved first").
  let toldUnsaved = null;
  const tellUnsaved = () => {
    const unsaved = editing && changes !== savedAt;
    if (unsaved === toldUnsaved) return;
    toldUnsaved = unsaved;
    leafen.setUnsaved(unsaved).catch(() => undefined);
  };
  if (editing) tellUnsaved();
  // A change, as the editor's frame tells each one (bridge.js); until it watches, as the editor's
  // own "modified" note tells them, which misses some. Unchanged: as the document was last taken.
  let watched = false;
  const changed = (back = false) => {
    changes = back ? takenAt : changes + 1;
    if (!saveButton.disabled) status.textContent = changes === savedAt ? say.saved : say.unsaved;
    tellUnsaved();
  };
  let ready, failed;
  const readiness = new Promise((resolve, reject) => { ready = resolve; failed = reject; });
  const waiting = setTimeout(() => failed(new Error("the editor didn't open in time")), EDITOR_WAIT_MS);
  window.APP = window.APP ?? {};
  const editor = new window.DocsAPI.DocEditor("placeholder", {
    document: {
      fileType: ext, key: `leafen-${file.fileId}-${Date.now()}`, title: file.name,
      // The editor's frame can't read a blob: address of a frame with no origin, so it reads data:.
      url: await dataUrl(bin),
      // Print: the editor's print rendering becomes a PDF Leafen prints (print below).
      permissions: { edit: editing, download: false, print: true, comment: false },
    },
    documentType: kinds[ext],
    editorConfig: {
      mode: editing ? "edit" : "view", lang: korean ? "ko" : "en", user: { id: person.id, name: person.name },
      // forcesave: the editor's own Save (Ctrl+S, its menu) asks the stand-in server, which saves to Leafen.
      customization: { compactHeader: true, chat: false, comments: false, hideRightMenu: true, help: false, plugins: false, forcesave: true,
        features: { featuresTips: false }, uiTheme: leafen.context.theme === "dark" ? "theme-dark" : "theme-classic-light" },
    },
    events: {
      onDocumentReady: () => {
        clearTimeout(waiting);
        delete host.dataset.opening;
        if (editing) frame()?.postMessage({ leafenBridge: "watch" }, "*");
        ready();
      },
      onError: (event) => { clearTimeout(waiting); failed(new Error(`editor ${JSON.stringify(event?.data ?? "")}`)); },
      onDocumentStateChange: (event) => { if (event?.data && !watched) changed(); },
    },
    width: "100%", height: "100%",
  });
  grabFocus = kinds[ext] === "cell" ? null : () => editor.grabFocus();
  const send = (message) => editor.sendMessageToOO(message);
  const frame = () => host.querySelector('iframe[name="frameEditor"]')?.contentWindow;
  // What the editor's frame asks of this one: pictures to show and to put in,
  // and the document it was asked for.
  const files = new Map();
  let asked = 0;
  window.addEventListener("message", (event) => {
    const data = event.data, source = event.source;
    if (!data || typeof data !== "object" || source !== frame()) return;
    if (data.leafenBridge === "watching") watched = true;
    if (data.leafenBridge === "changed" || data.leafenBridge === "unchanged") changed(data.leafenBridge === "unchanged");
    if (data.leafenBridge === "image") source.postMessage({ leafenBridge: "imageUrl", id: data.id, url: image(data.name) }, "*");
    if (data.leafenBridge === "addImages") {
      void addImages(data.files).catch(() => []).then((names) => source.postMessage({ leafenBridge: "imagesAdded", id: data.id, names }, "*"));
    }
    if (data.leafenBridge === "file") {
      files.get(data.id)?.(data);
      files.delete(data.id);
    }
    if (data.leafenBridge === "print") {
      void print(data).catch(() => { status.textContent = say.printFailed; }).finally(() => source.postMessage({ leafenBridge: "printed", id: data.id }, "*"));
    }
    if (data.leafenBridge === "pictureUrlsMade") {
      made.get(data.id)?.(data.urls ?? []);
      made.delete(data.id);
    }
  });
  // Pictures by address (a pasted picture comes as data; one from a URL or
  // pasted with a web page as an address): data is kept like a picture put
  // in, shown from an address of the editor's frame; this app reaches no
  // other address. Answered as the document server answers "imgurls".
  const made = new Map();
  const pictureUrls = (pictures) => new Promise((resolve) => {
    const id = ++asked;
    made.set(id, resolve);
    frame()?.postMessage({ leafenBridge: "pictureUrls", id, pictures }, "*");
  });
  const imageUrls = async (addresses) => {
    const refuse = { type: "documentOpen", data: { type: "imgurls", status: "err", data: String(SERVER_UPLOAD_URL) } };
    try {
      const pictures = (addresses ?? []).map(dataPicture);
      const kept = pictures.filter(Boolean);
      if (!kept.length) {
        send(refuse);
        return;
      }
      const names = await addImages(kept);
      const urls = await pictureUrls(kept);
      let next = 0;
      const answers = pictures.map((picture) => {
        const at = picture ? next++ : -1;
        return at >= 0 && names[at] && urls[at] ? { url: urls[at], path: `media/${names[at]}` } : { url: "error", path: "error" };
      });
      send({ type: "documentOpen", data: { type: "imgurls", status: "ok", data: { urls: answers, error: kept.length === answers.length ? 0 : SERVER_UPLOAD_URL } } });
    } catch {
      send(refuse);
    }
  };
  // The editor's print rendering, with the document, the font files it
  // uses — fetched once, by the name x2t looks for — and the document's
  // pictures, made a PDF away from the page and printed by Leafen.
  const fonts = new Map();
  const print = async ({ data, bin, fonts: used }) => {
    if (data == null || bin == null) throw new Error("nothing to print");
    const written = [];
    for (const font of Array.isArray(used) ? used : []) {
      const name = String(font?.name).replaceAll("/", "_"), path = String(font?.file);
      if (!/^fonts\/[\w.-]+$/u.test(path)) continue;
      if (!fonts.has(name)) {
        const response = await fetch(new URL(`onlyoffice-${ONLYOFFICE}/fonts/${path}`, vendor));
        if (!response.ok) continue;
        fonts.set(name, new Uint8Array(await response.arrayBuffer()));
      }
      written.push([`/working/fonts/${name}`, fonts.get(name)]);
    }
    for (const name of x2t.FS.readdir("/working/media")) {
      if (name !== "." && name !== "..") written.push([`/working/media/${name}`, x2t.FS.readFile(`/working/media/${name}`)]);
    }
    const encode = (value) => (typeof value === "string" ? new TextEncoder().encode(value) : value);
    written.push(["/working/pdf.bin", encode(data)], ["/working/print.bin", encode(bin)], ["/working/params.xml", '<?xml version="1.0" encoding="utf-8"?>'
      + '<TaskQueueDataConvert xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xmlns:xsd="http://www.w3.org/2001/XMLSchema">'
      + "<m_sFileFrom>/working/print.bin</m_sFileFrom><m_sThemeDir>/working/themes</m_sThemeDir><m_sFileTo>/working/print.bin.pdf</m_sFileTo>"
      + "<m_sFontDir>/working/fonts/</m_sFontDir><m_bIsNoBase64>false</m_bIsNoBase64></TaskQueueDataConvert>"]);
    await leafen.printDocument(await printPdf(written));
  };
  // The document as the editor has it now, with the changes told by then, which it holds; null
  // when the editor kept a cell being edited open, refusing its value with its own message.
  const current = () => new Promise((resolve, reject) => {
    const id = ++asked;
    files.set(id, (answer) => {
      if (answer.error) return reject(new Error(answer.error));
      if (answer.kept) return resolve(null);
      takenAt = changes;
      return resolve({ bin: answer.bin, at: changes });
    });
    frame()?.postMessage({ leafenBridge: "getFile", id }, "*");
  });
  // Saves the editor's document in its own format through Leafen; the top
  // bar's Save and the editor's own both come here, one at a time.
  let saving = null;
  const save = () => saving ??= (async () => {
    saveButton.disabled = true;
    status.textContent = say.saving;
    try {
      const taken = await current();
      if (!taken) {
        status.textContent = say.unsaved;
        return false;
      }
      const { bin: document, at } = taken;
      const out = convert(x2t, typeof document === "string" ? new TextEncoder().encode(document) : document, "output.bin", ext);
      if (out.code !== 0) throw new Error(`x2t ${out.code}`);
      const result = await leafen.saveFile(out.data, { contentType: contentTypes[ext] });
      if (!result.ok) {
        // A newer version settled in Leafen's own question isn't a failure.
        if (result.conflict) {
          status.textContent = say.unsaved;
        } else {
          status.textContent = say.notSaved;
          leafen.notice(say.notSavedNotice);
        }
        return false;
      }
      savedAt = at;
      tellUnsaved();
      status.textContent = changes === savedAt ? say.saved : say.unsaved;
      return true;
    } catch {
      status.textContent = say.notSaved;
      leafen.notice(say.notSavedNotice);
      return false;
    } finally {
      saveButton.disabled = false;
      saving = null;
      focusEditor();
    }
  })();
  // The editor's own Save: "not modified" when there is nothing new, else
  // Leafen's save. Its message for a failed save points to a download this
  // app doesn't offer, and the top bar says how the save went, so it ends
  // quietly either way — and at a time below any save's, so its next Save
  // asks again.
  const editorSave = async () => {
    if (!editing || changes === savedAt) {
      send({ type: "forceSaveStart", messages: { code: SERVER_NOT_MODIFIED, time: FORCE_SAVE_TIME } });
      return;
    }
    await save();
    send({ type: "forceSaveStart", messages: { code: SERVER_NO_ERROR, time: FORCE_SAVE_TIME } });
    send({ type: "forceSave", messages: { type: FORCE_SAVE_BUTTON, time: FORCE_SAVE_TIME, success: true } });
  };
  editor.connectMockServer({
    getParticipants: () => ({ index: me.indexUser, list: [me] }),
    getInitialChanges: () => [],
    getImageURL: async (name) => image(name),
    onAuth: () => {},
    onMessage: (message) => {
      switch (message.type) {
        case "isSaveLock": send({ type: "saveLock", saveLock: false }); break;
        case "getLock": {
          const block = message.block?.[0];
          const lock = { time: Date.now(), user: me.id, block };
          send({ type: "getLock", locks: kinds[ext] === "cell" ? [lock] : (typeof block === "string" ? { [block]: lock } : { [`l${Date.now()}`]: lock }) });
          break;
        }
        case "getMessages": send({ type: "message" }); break;
        case "saveChanges":
          if (message.endSaveChanges) send({ type: "unSaveLock", index: index++, time: Date.now() });
          else send({ type: "savePartChanges", changesIndex: -1, time: Date.now() });
          break;
        case "unLockDocument":
          if (message.releaseLocks && kinds[ext] === "cell") send({ type: "releaseLock", locks: [] });
          if (message.isSave) send({ type: "unSaveLock", time: -1, index: -1 });
          break;
        case "forceSaveStart": void editorSave(); break;
        case "openDocument":
          if (message.message?.c === "imgurls") void imageUrls(message.message.data);
          break;
        default: break;
      }
    },
  });
  await readiness;
  if (!editing) return;
  saveButton.hidden = false;
  saveButton.textContent = say.save;
  saveButton.addEventListener("click", () => { void save(); });
}
