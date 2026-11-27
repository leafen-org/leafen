import type { MutableRefObject, RefObject } from "react";
import { LeafenApiError } from "@/apiTransport";
import { appPlatformErrorCodes } from "@/appPlatformWire.generated";
import { screenKind } from "@/lib/screenKind";

// The channel between Leafen and an app's frame (docs/app-platform.md 4):
// the frame says hello with the one-time value in its address; only then
// does it get a private message channel, through which Leafen answers its
// calls, within limits, and tells it when the theme changes.

/** Answers a frame's call; `ended` aborts when the frame's channel ends (its frame closed or stopped). */
export type AppFrameAnswer = (method: string, params: Record<string, unknown>, ended?: AbortSignal) => Promise<unknown>;

// What a frame may ask (stage supplement ④): a burst, then a steady rate,
// and so many at once. A call past them waits its turn, in the order calls
// came, as the server's app budget has a call wait (app_platform_budget.go);
// one with so many already waiting is refused, and a frame that keeps
// flooding is stopped, so one app can't make Leafen's own screen stall
// (2026-11-24: the Drive's Obsidian card read a 1,200-folder vault one call
// after another and stopped at the hundredth with "Too many calls").
const APP_FRAME_LIMITS = { burst: 100, perSecond: 20, concurrent: 16, waiting: 100, refusalsToStop: 200, refusalWindowMs: 10_000 };
const APP_FRAME_LIMITED = "LEAFEN_APP_BRIDGE_LIMITED";

// What Leafen refuses for a frame before a call reaches the server, said as
// the server's refusals are, "code: sentence", so an app can tell them apart
// and show words (2026-11-06: a guest's call came back as a bare
// "not_for_guests", and none of these was in the docs).
const APP_BRIDGE_REFUSALS = {
  unknownCall: ["LEAFEN_APP_BRIDGE_UNKNOWN_CALL", "This screen can't make that call here."],
  invalidCall: ["LEAFEN_APP_BRIDGE_INVALID_CALL", "The call's input isn't what it takes."],
  credentialsOff: ["LEAFEN_APP_CREDENTIALS_OFF", "The administrator has people's own logins off for this app."],
  popupBlocked: ["LEAFEN_APP_BRIDGE_POPUP_BLOCKED", "The browser blocked the window; make the call from a click."],
  needsClick: ["LEAFEN_APP_BRIDGE_NEEDS_CLICK", "Make this call once the person has used the page, from a click."],
  noDataAccess: ["LEAFEN_APP_BRIDGE_NO_DATA_ACCESS", "This screen can't reach Leafen's data here."],
  notForGuests: ["LEAFEN_APP_BRIDGE_NOT_FOR_GUESTS", "A guest has only the link's room, the store and notices."],
  readOnly: ["LEAFEN_APP_BRIDGE_READ_ONLY", "The person can only view this page, so the block can't change it."],
  tooLarge: ["LEAFEN_APP_BRIDGE_TOO_LARGE", "That is more than a block may keep."],
  blockGone: ["LEAFEN_APP_BRIDGE_BLOCK_GONE", "The block is no longer in the page."],
} as const;

export type AppBridgeRefusal = keyof typeof APP_BRIDGE_REFUSALS;

/** A refusal of Leafen's own for a frame's call, as the frame reads it. */
export function appBridgeRefusal(kind: AppBridgeRefusal) {
  const [code, sentence] = APP_BRIDGE_REFUSALS[kind];
  return new Error(`${code}: ${sentence}`);
}

export function appFrameTheme() {
  return document.documentElement.dataset.theme === "dark" ? "dark" : "light";
}

// The token of an install that was removed: the server no longer takes it.
const APP_TOKEN_REFUSED = "LEAFEN_USER_KEY_INVALID";

// The answers that mean the app is no longer the person's to use here: an
// administrator stopped it, or its token stopped holding (2026-11-03: an open
// app stopped by its administrator read as a key problem).
const APP_GONE_ANSWERS = new Set<string>([appPlatformErrorCodes.appStopped, APP_TOKEN_REFUSED]);

// The code a call's answer carries: an error's "code: sentence", the server's
// refusal an API call came back with, or a file's save that didn't go through
// with its token refused (a stopped app's refused save leaves its window as it is).
function answerCode(answer: unknown) {
  if (answer instanceof Error) return answer.message.split(":")[0];
  const value = answer as { body?: { error?: { code?: unknown } }; ok?: unknown; code?: unknown } | null | undefined;
  const code = value?.body?.error?.code ?? (value?.ok === false && value.code === APP_TOKEN_REFUSED ? value.code : undefined);
  return typeof code === "string" ? code : "";
}

function appCallGoneCode(answer: unknown) {
  // A token that can't be renewed names no answer of the app's own.
  if (answer instanceof LeafenApiError) {
    return answer.code === appPlatformErrorCodes.installNotFound || answer.code === appPlatformErrorCodes.appStopped ? answer.code : "";
  }
  const code = answerCode(answer);
  return APP_GONE_ANSWERS.has(code) ? code : "";
}

/**
 * Whether a call's answer says the install the frame opened with is gone —
 * its token refused, or the install not found as the token was renewed — as
 * when an administrator removed the app and installed it again.
 */
export function appInstallGone(answer: unknown) {
  if (answer instanceof LeafenApiError) return answer.code === appPlatformErrorCodes.installNotFound;
  return answerCode(answer) === APP_TOKEN_REFUSED;
}

/** How long a frame that said it would tell when it has drawn stays unseen, at most. */
const APP_FRAME_DRAW_WAIT_MS = 10_000;

/** Waits for the frame's hello and answers its calls; the returned stop ends the channel. */
export function bridgeAppFrame({ frame, nonce, port, answer, context, onReady, onHello, onFlood, onGone }: {
  frame: RefObject<HTMLIFrameElement | null>;
  nonce: string;
  port: MutableRefObject<MessagePort | null>;
  answer: AppFrameAnswer;
  context: () => Record<string, unknown>;
  /** The frame has its channel and its theme, and has drawn its first screen
   * when its page said it would tell (the SDK's data-leafen-opening). */
  onReady?: () => void;
  /** The frame said hello: it is opening its channel. */
  onHello?: () => void;
  onFlood?: () => void;
  /** A call found the app no longer the person's to use; the code says why. "kept" when the frame
   * stays as it was, to be told again by a later call (a file's window holding changes). */
  onGone?: (code: string) => void | "kept";
}) {
  let helloSeen = false;
  // What a call leaves behind for the frame (a PDF it prints) goes with its channel.
  const channelLife = new AbortController();
  let tokens = APP_FRAME_LIMITS.burst, refilled = performance.now(), pending = 0, stopped = false;
  const refusals: number[] = [];
  // The calls waiting their turn, in the order they came: the first goes
  // once the rate has room for it and fewer than so many are out.
  const waiting: Array<() => void> = [];
  let turnTimer = 0;
  const turns = {
    waiting: () => waiting.length,
    add(run: () => void) {
      waiting.push(run);
      turns.go();
    },
    go() {
      if (turnTimer) return;
      while (waiting.length && !stopped && pending < APP_FRAME_LIMITS.concurrent) {
        const now = performance.now();
        tokens = Math.min(APP_FRAME_LIMITS.burst, tokens + ((now - refilled) / 1000) * APP_FRAME_LIMITS.perSecond);
        refilled = now;
        if (tokens < 1) {
          turnTimer = window.setTimeout(() => {
            turnTimer = 0;
            turns.go();
          }, Math.ceil(((1 - tokens) / APP_FRAME_LIMITS.perSecond) * 1000));
          return;
        }
        tokens -= 1;
        waiting.shift()?.();
      }
    },
    clear() {
      waiting.length = 0;
      window.clearTimeout(turnTimer);
      turnTimer = 0;
    },
  };
  let goneSaid = false;
  const noticeGone = (code: string) => {
    if (!code || goneSaid) return;
    goneSaid = true;
    if (onGone?.(code) === "kept") goneSaid = false;
  };
  const onMessage = (event: MessageEvent) => {
    const target = frame.current;
    if (!target || event.source !== target.contentWindow || helloSeen) return;
    const data = event.data as { type?: unknown; nonce?: unknown } | null;
    if (data?.type !== "leafen:hello" || data.nonce !== nonce) return;
    helloSeen = true;
    onHello?.();
    // A page whose first screen is a placeholder says when it has drawn over it;
    // the frame stays unseen until then, or for at most APP_FRAME_DRAW_WAIT_MS.
    const draws = (data as { opening?: unknown }).opening === true;
    let readied = false;
    const ready = () => {
      if (readied) return;
      readied = true;
      onReady?.();
    };
    const channel = new MessageChannel();
    port.current?.close();
    port.current = channel.port1;
    channel.port1.onmessage = (message) => {
      if ((message.data as { type?: unknown } | null)?.type === "drawn") return ready();
      const call = message.data as { id?: unknown; method?: unknown; params?: unknown } | null;
      if (stopped || !call || typeof call.id !== "number" || typeof call.method !== "string") return;
      if (turns.waiting() >= APP_FRAME_LIMITS.waiting) {
        const now = performance.now();
        channel.port1.postMessage({ id: call.id, error: `${APP_FRAME_LIMITED}: Too many calls; slow down.` });
        refusals.push(now);
        while (refusals.length && now - refusals[0] > APP_FRAME_LIMITS.refusalWindowMs) refusals.shift();
        if (refusals.length >= APP_FRAME_LIMITS.refusalsToStop) {
          stopped = true;
          turns.clear();
          channelLife.abort();
          channel.port1.close();
          if (port.current === channel.port1) port.current = null;
          onFlood?.();
        }
        return;
      }
      const id = call.id, method = call.method;
      const params = call.params && typeof call.params === "object" ? call.params as Record<string, unknown> : {};
      turns.add(() => {
        pending += 1;
        void answer(method, params, channelLife.signal).then(
          (result) => {
            if (!stopped) channel.port1.postMessage({ id, result });
            noticeGone(appCallGoneCode(result));
          },
          (failure) => {
            if (!stopped) channel.port1.postMessage({ id, error: failure instanceof Error ? failure.message : "failed" });
            noticeGone(appCallGoneCode(failure));
          },
        ).finally(() => {
          pending -= 1;
          turns.go();
        });
      });
    };
    // Every place tells the app what screen it is on, by Leafen's own rule.
    target.contentWindow?.postMessage({ type: "leafen:ready", context: { screen: screenKind(), ...context() } }, "*", [channel.port2]);
    if (draws) window.setTimeout(ready, APP_FRAME_DRAW_WAIT_MS);
    else ready();
  };
  window.addEventListener("message", onMessage);
  const watcher = new MutationObserver(() => port.current?.postMessage({ type: "theme", theme: appFrameTheme() }));
  watcher.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
  return () => {
    turns.clear();
    channelLife.abort();
    window.removeEventListener("message", onMessage);
    watcher.disconnect();
    port.current?.close();
    port.current = null;
  };
}

/** Reads an answer from the server, or throws its error code and message. */
export async function appCallResult<T>(response: Response): Promise<T> {
  const body = await response.json().catch(() => null) as (T & { error?: { code?: string; message?: string } }) | null;
  if (!response.ok) throw new Error(`${body?.error?.code ?? "failed"}: ${body?.error?.message ?? ""}`);
  return body as T;
}
