// The packaged app's sign-ins on other servers, which the app keeps in the
// platform's secret storage rather than in the page's storage:
// the shell's credential broker, reached through Wails' bindings, or on
// Android the app's own Keystore store, reached by messages only the app's
// page can send. A page a server serves never asks for it.

import { watchKeychainPrompt } from "./keychainPrompt";
import { gateLocalKeySteps } from "./localKeyDatabase";

export type CredentialStore = {
  /** A profile's kept sign-in, or "" when none is kept. */
  load(profile: string): Promise<string>;
  save(profile: string, secret: string): Promise<void>;
  forget(profile: string): Promise<void>;
};

type AndroidCredentials = {
  postMessage(message: string): void;
  onmessage: ((event: { data: string }) => void) | null;
};

type AndroidReply = { id?: string; ok?: boolean; secret?: string };

function androidStore(bridge: AndroidCredentials): CredentialStore {
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
  // The app answers within the wait, or the step fails (lib/appSignIn.ts).
  const ask = (request: Record<string, string>) => new Promise<AndroidReply>((resolve) => {
    next += 1;
    const id = String(next);
    const timer = setTimeout(() => { if (waiting.delete(id)) resolve({ id, ok: false }); }, 30_000);
    waiting.set(id, (reply) => { clearTimeout(timer); resolve(reply); });
    bridge.postMessage(JSON.stringify({ ...request, id }));
  });
  return {
    load: async (profile) => {
      const reply = await ask({ op: "load", profile });
      return reply.ok && typeof reply.secret === "string" ? reply.secret : "";
    },
    save: async (profile, secret) => {
      if (!(await ask({ op: "save", profile, secret })).ok) throw new Error("The login couldn't be kept.");
    },
    forget: async (profile) => {
      await ask({ op: "forget", profile });
    },
  };
}

async function brokerStore(): Promise<CredentialStore> {
  const { Call } = await import("@wailsio/runtime");
  // A Mac's keychain may wait on macOS's prompt: the window says so meanwhile.
  const asking = async () => await Call.ByName("main.CredentialBroker.Asking") === true;
  const call = (method: string, ...args: string[]) => watchKeychainPrompt(Call.ByName(`main.CredentialBroker.${method}`, ...args), asking);
  return {
    load: async (profile) => {
      const secret: unknown = await call("Load", profile);
      return typeof secret === "string" ? secret : "";
    },
    save: async (profile, secret) => {
      await call("Save", profile, secret);
    },
    forget: async (profile) => {
      await call("Forget", profile);
    },
  };
}

/**
 * The app's page passes each step that stores or reads one of its keys
 * through the app first (lib/localKeyDatabase.ts): the app reads WebKit's own
 * key for them, so that when macOS asks before giving it, the window says so
 * from that prompt until the step is done — WebKit's read again, if it asks
 * too, stops the page with that line on screen. Once a read needs no prompt,
 * none will, and steps go straight on.
 */
export function gateWebKitKeySteps(): void {
  let free = false;
  // Steps at the same time wait on one read, and one prompt.
  let reading: Promise<void> | null = null;
  gateLocalKeySteps(async (step) => {
    if (free) return step();
    const { Call } = await import("@wailsio/runtime");
    let asked = false;
    const asking = async () => {
      const showing = await Call.ByName("main.CredentialBroker.Asking") === true;
      asked ||= showing;
      return showing;
    };
    reading ??= Call.ByName("main.CredentialBroker.ReadyWebKitKey").then(() => undefined, () => undefined)
      .finally(() => { reading = null; });
    const result = await watchKeychainPrompt(reading.then(step), asking);
    if (!asked) free = true;
    return result;
  });
}

let store: Promise<CredentialStore | null> | null = null;

/**
 * The packaged app's secret storage for sign-ins on other servers, or null
 * where the app has none (an Android WebView too old for the app's store).
 */
export function packagedCredentials(): Promise<CredentialStore | null> {
  store ??= (async () => {
    const android = (window as unknown as { leafenCredentials?: AndroidCredentials }).leafenCredentials;
    if (android && typeof android.postMessage === "function") return androidStore(android);
    if (/Android/u.test(navigator.userAgent)) return null;
    return brokerStore();
  })().catch(() => null);
  return store;
}
