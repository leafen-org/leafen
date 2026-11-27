// Saving files onto the device in a packaged app, whose window can't
// download: the shell's file saver (a save dialog on a desktop, the share
// sheet on iOS) through Wails' bindings, or on Android the app's own
// Downloads store, reached by messages only the app's page can send. The
// file goes in base64 pieces.

type Saver = {
  /** Starts a save, giving its ID, or "" when the person cancelled. */
  begin(name: string, mime: string): Promise<string>;
  append(save: string, data: string): Promise<void>;
  finish(save: string): Promise<void>;
  abort(save: string): Promise<void>;
};

type AndroidFiles = {
  postMessage(message: string): void;
  onmessage: ((event: { data: string }) => void) | null;
};

type AndroidReply = { id?: string; ok?: boolean; save?: string };

const PIECE_BYTES = 1 << 20;

function androidSaver(bridge: AndroidFiles): Saver {
  const waiting = new Map<string, (reply: AndroidReply) => void>();
  bridge.onmessage = (event) => {
    try {
      const reply = JSON.parse(event.data) as AndroidReply;
      const answer = reply.id ? waiting.get(reply.id) : undefined;
      if (!answer || !reply.id) return;
      waiting.delete(reply.id);
      answer(reply);
    } catch {
      // Not an answer to a request.
    }
  };
  let next = 0;
  const ask = async (request: Record<string, string>) => {
    next += 1;
    const id = String(next);
    // The app answers within the wait, or the save fails (lib/appSignIn.ts).
    const reply = await new Promise<AndroidReply>((resolve) => {
      const timer = setTimeout(() => { if (waiting.delete(id)) resolve({ id, ok: false }); }, 60_000);
      waiting.set(id, (answer) => { clearTimeout(timer); resolve(answer); });
      bridge.postMessage(JSON.stringify({ ...request, id }));
    });
    if (!reply.ok) throw new Error("The file couldn't be saved.");
    return reply;
  };
  return {
    begin: async (name, mime) => (await ask({ op: "begin", name, mime })).save ?? "",
    append: async (save, data) => { await ask({ op: "append", save, data }); },
    finish: async (save) => { await ask({ op: "finish", save }); },
    abort: async (save) => { await ask({ op: "abort", save }).catch(() => undefined); },
  };
}

async function shellSaver(): Promise<Saver> {
  const { Call } = await import("@wailsio/runtime");
  return {
    begin: async (name) => {
      const save: unknown = await Call.ByName("main.FileSaver.Begin", name);
      return typeof save === "string" ? save : "";
    },
    append: async (save, data) => { await Call.ByName("main.FileSaver.Append", save, data); },
    finish: async (save) => { await Call.ByName("main.FileSaver.Finish", save); },
    abort: async (save) => { await Call.ByName("main.FileSaver.Abort", save).catch(() => undefined); },
  };
}

let saver: Promise<Saver> | null = null;

function deviceSaver(): Promise<Saver> {
  saver ??= (async () => {
    const android = (window as unknown as { leafenFiles?: AndroidFiles }).leafenFiles;
    if (android && typeof android.postMessage === "function") return androidSaver(android);
    return shellSaver();
  })();
  return saver;
}

function base64(bytes: Uint8Array): string {
  let binary = "";
  for (let start = 0; start < bytes.length; start += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(start, start + 0x8000));
  }
  return btoa(binary);
}

/** Saves a file onto the device; false when the person cancelled. */
export async function saveFileToDevice(file: Blob, name: string): Promise<boolean> {
  const save = await deviceSaver();
  const id = await save.begin(name, file.type);
  if (!id) return false;
  try {
    for (let offset = 0; offset < file.size; offset += PIECE_BYTES) {
      const piece = new Uint8Array(await file.slice(offset, offset + PIECE_BYTES).arrayBuffer());
      await save.append(id, base64(piece));
    }
    await save.finish(id);
    return true;
  } catch (error) {
    await save.abort(id);
    throw error;
  }
}

/** Saves what a server's file address holds onto the device. */
export async function saveUrlToDevice(url: string, name: string): Promise<boolean> {
  const response = await fetch(url);
  if (!response.ok) throw new Error(`The file couldn't be read (${response.status}).`);
  return saveFileToDevice(await response.blob(), name);
}
