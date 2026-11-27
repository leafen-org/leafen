// x2t, CryptPad's WebAssembly build of ONLYOFFICE's converter (ui/vendor,
// AGPL-3.0), as the Office Documents app uses it (app.js): started once in
// the page to turn a file into the editor's own form and back, and fresh in
// a worker for each print PDF.
const X2T = "9.3.2";
const vendor = new URL("./vendor/", import.meta.url);
// A print PDF takes x2t well under a second; one that runs past this is stopped.
const PRINT_WAIT_MS = 60_000;

// x2t in the page, stopped as failed when it hasn't started within `wait` ms.
export function loadX2t(wait) {
  const base = new URL(`x2t-${X2T}/`, vendor).href;
  return new Promise((resolve, reject) => {
    const late = setTimeout(() => reject(new Error("x2t didn't start in time")), wait);
    window.Module = {
      locateFile: (path) => base + path,
      onRuntimeInitialized() { clearTimeout(late); resolve(window.Module); },
      onAbort(what) { clearTimeout(late); reject(new Error(`x2t stopped: ${what}`)); },
    };
    const script = document.createElement("script");
    script.src = base + "x2t.js";
    script.onerror = () => { clearTimeout(late); reject(new Error("x2t didn't load")); };
    document.head.append(script);
  }).then((module) => {
    for (const folder of ["/working", "/working/media", "/working/fonts", "/working/themes"]) module.FS.mkdir(folder);
    return module;
  });
}

export function convert(x2t, bytes, from, to) {
  x2t.FS.writeFile(`/working/${from}`, bytes);
  x2t.FS.writeFile("/working/params.xml", '<?xml version="1.0" encoding="utf-8"?>'
    + '<TaskQueueDataConvert xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xmlns:xsd="http://www.w3.org/2001/XMLSchema">'
    + `<m_sFileFrom>/working/${from}</m_sFileFrom><m_sThemeDir>/working/themes</m_sThemeDir>`
    + `<m_sFileTo>/working/${from}.${to}</m_sFileTo><m_bIsNoBase64>false</m_bIsNoBase64></TaskQueueDataConvert>`);
  const code = x2t.ccall("main1", "number", ["string"], ["/working/params.xml"]);
  return { code, data: code === 0 ? x2t.FS.readFile(`/working/${from}.${to}`) : null };
}

// A PDF of the editor's print rendering, made by a fresh x2t in a worker —
// the document, the font files it uses and its pictures written where x2t
// looks for them, as CryptPad's build makes it — so a conversion that never
// ends stops after PRINT_WAIT_MS without stopping the page. The worker is
// given x2t's WebAssembly, which it can't find from a blob: address.
let x2tWasm = null;
export async function printPdf(files) {
  const base = new URL(`x2t-${X2T}/`, vendor).href;
  x2tWasm ??= await fetch(`${base}x2t.wasm`).then((response) => {
    if (!response.ok) throw new Error(`x2t.wasm ${response.status}`);
    return response.arrayBuffer();
  });
  const wasm = x2tWasm.slice(0);
  const source = "onmessage=(event)=>{if(event.data.wasm){"
    + "self.Module={wasmBinary:event.data.wasm,onRuntimeInitialized(){postMessage({ready:true})}};"
    + `importScripts(${JSON.stringify(`${base}x2t.js`)});return}const FS=Module.FS;`
    + 'for(const folder of ["/working","/working/media","/working/fonts","/working/themes"])try{FS.mkdir(folder)}catch{}'
    + "for(const [path,bytes] of event.data.files)FS.writeFile(path,bytes);"
    + 'const code=Module.ccall("main1","number",["string"],["/working/params.xml"]);let pdf=null;'
    + 'try{pdf=FS.readFile("/working/print.bin.pdf")}catch{}postMessage({code,pdf},pdf?[pdf.buffer]:[])}';
  const worker = new Worker(URL.createObjectURL(new Blob([source], { type: "text/javascript" })));
  return new Promise((resolve, reject) => {
    const late = setTimeout(() => { worker.terminate(); reject(new Error("x2t didn't make the PDF in time")); }, PRINT_WAIT_MS);
    worker.onerror = () => { clearTimeout(late); worker.terminate(); reject(new Error("x2t didn't start")); };
    worker.onmessage = (event) => {
      if (event.data?.ready) {
        worker.postMessage({ files });
        return;
      }
      clearTimeout(late);
      worker.terminate();
      if (event.data?.code !== 0 || !event.data?.pdf) reject(new Error(`x2t ${event.data?.code}`));
      else resolve(event.data.pdf);
    };
    worker.postMessage({ wasm }, [wasm]);
  });
}
