import { authBounds } from "@/authWire.generated";
import { digest, randomBytes } from "./browserCrypto";
// A provider sign-in on another server from the packaged app. The server's
// sign-in opens in the system's browser, as RFC 8252 has native apps do, and
// comes back to the app: the shell's sign-in return (a loopback address on a
// desktop, the system's sign-in sheet on iOS) through Wails' bindings, or on
// Android the app's own scheme, reached by messages only the app's page can
// send. The completion code it brings back is exchanged with the PKCE
// verifier the sign-in started with; no one else can use it.

type Begun = { id: string; address: string };

type SignInBridge = {
  begin(): Promise<Begun>;
  open(id: string, address: string): Promise<void>;
  /** What the sign-in brought back, its return's query, or "" without one. */
  wait(id: string): Promise<string>;
  cancel(id: string): Promise<void>;
};

type AndroidSignIn = {
  postMessage(message: string): void;
  onmessage: ((event: { data: string }) => void) | null;
};

type AndroidReply = { id?: string; ok?: boolean; signIn?: string; address?: string; query?: string };

const BRIDGE_STEP_WAIT_MS = 30_000;

function androidBridge(bridge: AndroidSignIn): SignInBridge {
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
  // The app answers a step within its wait, or the step fails (2026-10-17: a
  // step the app never answered left sign-in waiting with no end).
  const ask = (request: Record<string, string>, waitMs = BRIDGE_STEP_WAIT_MS) => new Promise<AndroidReply>((resolve) => {
    next += 1;
    const id = String(next);
    const timer = setTimeout(() => { if (waiting.delete(id)) resolve({ id, ok: false }); }, waitMs);
    waiting.set(id, (reply) => { clearTimeout(timer); resolve(reply); });
    bridge.postMessage(JSON.stringify({ ...request, id }));
  });
  return {
    begin: async () => {
      const reply = await ask({ op: "begin" });
      if (!reply.ok || !reply.signIn || !reply.address) throw new Error("The login couldn't start.");
      return { id: reply.signIn, address: reply.address };
    },
    open: async (signIn, address) => {
      if (!(await ask({ op: "open", signIn, url: address })).ok) throw new Error("The login couldn't open.");
    },
    wait: async (signIn) => {
      // A sign-in in the browser lasts as long as the server keeps it.
      const reply = await ask({ op: "wait", signIn }, authBounds.providerStateTTLSeconds * 1000);
      return reply.ok && typeof reply.query === "string" ? reply.query : "";
    },
    cancel: async (signIn) => {
      await ask({ op: "cancel", signIn });
    },
  };
}

async function shellBridge(): Promise<SignInBridge> {
  const { Call } = await import("@wailsio/runtime");
  return {
    begin: async () => {
      const begun = await Call.ByName("main.SignInReturn.Begin") as Partial<Begun> | null;
      if (!begun?.id || !begun.address) throw new Error("The login couldn't start.");
      return { id: begun.id, address: begun.address };
    },
    open: async (id, address) => { await Call.ByName("main.SignInReturn.Open", id, address); },
    wait: async (id) => {
      const query: unknown = await Call.ByName("main.SignInReturn.Wait", id);
      return typeof query === "string" ? query : "";
    },
    cancel: async (id) => { await Call.ByName("main.SignInReturn.Cancel", id).catch(() => undefined); },
  };
}

let bridge: Promise<SignInBridge> | null = null;

function signInBridge(): Promise<SignInBridge> {
  bridge ??= (async () => {
    const android = (window as unknown as { leafenSignIn?: AndroidSignIn }).leafenSignIn;
    if (android && typeof android.postMessage === "function") return androidBridge(android);
    return shellBridge();
  })();
  return bridge;
}

function base64url(bytes: Uint8Array): string {
  return btoa(String.fromCharCode(...bytes)).replaceAll("+", "-").replaceAll("/", "_").replace(/=+$/u, "");
}

/** A PKCE verifier and its S256 challenge (RFC 7636). */
export async function pkcePair(): Promise<{ verifier: string; challenge: string }> {
  const verifier = base64url(randomBytes(32));
  return { verifier, challenge: base64url(await digest("SHA-256", new TextEncoder().encode(verifier))) };
}

let current: { bridge: SignInBridge; id: string } | null = null;

/**
 * Signs in through a server's sign-in address in the system's browser,
 * giving the completion code that came back (named code, or samlCompletion
 * for SAML) with the verifier that exchanges it, or null when the sign-in
 * ended without one. A sign-in started again ends the one before.
 */
export async function signInThroughApp(
  start: string,
  codeName: "code" | "samlCompletion" | "passkeyCompletion" | "appSignIn" = "code",
  signal?: AbortSignal,
): Promise<{ code: string; verifier: string } | null> {
  if (signal?.aborted) return null;
  const signIn = await signInBridge();
  if (current) await current.bridge.cancel(current.id);
  const { verifier, challenge } = await pkcePair();
  const begun = await signIn.begin();
  current = { bridge: signIn, id: begun.id };
  const cancel = () => { void signIn.cancel(begun.id).catch(() => undefined); };
  signal?.addEventListener("abort", cancel, { once: true });
  if (signal?.aborted) { cancel(); signal.removeEventListener("abort", cancel); if (current?.id === begun.id) current = null; return null; }
  try {
    const address = new URL(start);
    address.searchParams.set("app_return", begun.address);
    address.searchParams.set("app_challenge", challenge);
    await signIn.open(begun.id, address.toString());
    const code = new URLSearchParams(await signIn.wait(begun.id)).get(codeName) ?? "";
    return code ? { code, verifier } : null;
  } catch (error) {
    await signIn.cancel(begun.id).catch(() => undefined);
    throw error;
  } finally {
    signal?.removeEventListener("abort", cancel);
    if (current?.id === begun.id) current = null;
  }
}

/** The app as the server's "register this app" page names it. */
export function packagedAppName(): string {
  const agent = navigator.userAgent;
  if (/iPad/u.test(agent)) return "Leafen iPad";
  if (/iPhone|iPod/u.test(agent)) return "Leafen iPhone";
  if (/Android/u.test(agent)) return "Leafen Android";
  if (/Windows/u.test(agent)) return "Leafen Windows";
  if (/Mac OS X|Macintosh/u.test(agent)) return "Leafen macOS";
  return "Leafen";
}
