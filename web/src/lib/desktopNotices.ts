import { useEffect } from "react";
import { request } from "@/apiTransport";
import { isPackagedClient } from "./packagedClient";
import { pushRoute } from "./router";
import { onDocumentUnread } from "./documentChrome";
import { followAcrossTabs, tellTabs } from "./followAcrossTabs";
import { serverOrigin } from "./serverOrigin";
import type { Session } from "@/types";
import { personValue, storePersonValue } from "./browserStorage";
import { activeProfileId, LOCAL_PROFILE_ID } from "./profileRegistry";
import { rememberedServerIdentity } from "./serverKeyPin";

/**
 * The packaged desktop app's notifications. Its window has no push service,
 * so while it is open it asks its server what came since it last asked
 * (comments and mentions still unread ten seconds later, as a browser's push
 * brings them), shows each through the app, and keeps the person's unread
 * count on the app's icon, as Notion's desktop app does while open. On by
 * default on a desktop, turned off per device in Settings → Notifications.
 *
 * A phone's app gets no push (Leafen runs no push relay, decided
 * 2026-10-19): while it is open the window keeps the unread count on the
 * icon and hands what it watched to the app, which asks the server what came
 * since when the system wakes it in the background and shows that (the iOS
 * shell's notices_ios.go; Android's LeafenNotices.java, as
 * window.leafenNotices). Real-time notifications on a phone come to Leafen
 * added to the home screen, by web push.
 */

export type DesktopNoticeState = "on" | "off" | "denied";

type DesktopNoticesAnswer = {
  notices: Array<{
    id: string; title: string; body: string; url: string; occurredAt: number;
  }>;
  unreadCount: number;
  through: number;
};

const ASK_EVERY_MS = 20_000;
// How soon the icon follows a change of the window's own count.
const FOLLOW_COUNT_MS = 500;
const OPEN_NOTICE_EVENT = "leafen:open-notice";
const SHOWN_LIMIT = 200;
// Where the person's tabs got to, for a tab that starts while another asks:
// it goes on from there, not from its own start, so nothing between is
// missed if that tab closes. Older than this, the app was closed meanwhile.
const PLACE_KEY = "leafen:desktop-notices-place";
const PLACE_FRESH_MS = ASK_EVERY_MS * 2;

function keptPlace(userId: string, workspaces: string): number | null {
  try {
    const kept = JSON.parse(personValue(PLACE_KEY, { userId }) ?? "null") as { workspaces?: string; through?: number; at?: number } | null;
    if (kept?.workspaces !== workspaces || typeof kept.through !== "number" || typeof kept.at !== "number") return null;
    return Date.now() - kept.at < PLACE_FRESH_MS ? kept.through : null;
  } catch {
    return null;
  }
}

function keepPlace(userId: string, workspaces: string, through: number) {
  storePersonValue(PLACE_KEY, JSON.stringify({ workspaces, through, at: Date.now() }), { userId });
}

/** Whether this window is the packaged app's on a desktop, which shows notifications itself. */
export function desktopNoticesHere(): boolean {
  return isPackagedClient() && !/Android|iPhone|iPad/u.test(navigator.userAgent);
}

/** Whether this window is the packaged app's on a phone, which refreshes notifications in the background. */
export function phoneNoticesHere(): boolean {
  return isPackagedClient() && /Android|iPhone|iPad/u.test(navigator.userAgent);
}

/** What the window last saw, for a phone's background refresh; null stops it. */
type NoticeWatch = { profile: string; workspaces: readonly string[]; through: number; identity: string } | null;

type NoticeShell = {
  allowed(): Promise<boolean>;
  allow(): Promise<boolean>;
  show(id: string, title: string, body: string, url: string): Promise<void>;
  unread(count: number): Promise<void>;
  watch(watch: NoticeWatch): Promise<void>;
};

type AndroidNotices = {
  postMessage(message: string): void;
  onmessage: ((event: { data: string }) => void) | null;
};

// Android's app answers by messages, each to its request.
function androidShell(bridge: AndroidNotices): NoticeShell {
  const waiting = new Map<string, (reply: { allowed?: boolean }) => void>();
  bridge.onmessage = (event) => {
    try {
      const reply = JSON.parse(event.data) as { id?: string; allowed?: boolean };
      const answer = reply.id ? waiting.get(reply.id) : undefined;
      if (!answer || !reply.id) return;
      waiting.delete(reply.id);
      answer(reply);
    } catch {
      // Not an answer to a request.
    }
  };
  let next = 0;
  const ask = (request: Record<string, unknown>, wait = 30_000) => new Promise<{ allowed?: boolean }>((resolve) => {
    next += 1;
    const id = String(next);
    const timer = setTimeout(() => { if (waiting.delete(id)) resolve({}); }, wait);
    waiting.set(id, (reply) => { clearTimeout(timer); resolve(reply); });
    bridge.postMessage(JSON.stringify({ ...request, id }));
  });
  return {
    allowed: async () => (await ask({ op: "allowed" })).allowed === true,
    // The system's question waits for the person.
    allow: async () => (await ask({ op: "allow" }, 300_000)).allowed === true,
    show: async () => undefined,
    // Android's launcher shows the count on the app's notifications.
    unread: async () => undefined,
    watch: async (watch) => {
      await ask({ op: "watch", profile: watch?.profile ?? "", workspaces: watch?.workspaces ?? [], through: watch?.through ?? 0,
        identity: watch?.identity ?? "" });
    },
  };
}

function wailsShell(): NoticeShell {
  return {
    allowed: async () => (await shell("Allowed")) === true,
    allow: async () => (await shell("Allow")) === true,
    show: async (id, title, body, url) => {
      await shell("Show", id, title, body, url, [], null);
    },
    unread: async (count) => { await shell("SetUnread", count); },
    watch: async (watch) => {
      // A desktop's app shows notifications only while open.
      if (!phoneNoticesHere()) return;
      await shell("Watch", watch?.profile ?? "", watch?.workspaces ?? [], watch?.through ?? 0, watch?.identity ?? "");
    },
  };
}

let shellHere: NoticeShell | null = null;

function noticeShell(): NoticeShell {
  if (shellHere) return shellHere;
  const android = (window as unknown as { leafenNotices?: AndroidNotices }).leafenNotices;
  shellHere = android && typeof android.postMessage === "function" ? androidShell(android) : wailsShell();
  return shellHere;
}

/** What a phone's background refresh watches: another server's workspaces, never the app's own server. */
function phoneWatch(workspaces: readonly string[], through: number): NoticeWatch {
  const profile = activeProfileId();
  if (profile === LOCAL_PROFILE_ID || !profile.startsWith("http")) return null;
  return { profile, workspaces, through, identity: rememberedServerIdentity(profile) };
}

// Whether the person turned desktop notices off, on this browser.
const NOTICES_KEY = "leafen:desktop-notices";

function turnedOff(userId: string) {
  return personValue(NOTICES_KEY, { userId }) === "off";
}

function keepSetting(userId: string, value: "on" | "off") {
  // Without storage the default (on) stays.
  storePersonValue(NOTICES_KEY, value, { userId });
}

// Whether the app has asked the system, on this device, to show the person's
// notifications. macOS asks the person only once; after a refusal only the
// system's settings can let them through.
const ASKED_KEY = "leafen:desktop-notices-asked";

function asked(userId: string) {
  return personValue(ASKED_KEY, { userId }) !== null;
}

async function askSystem(userId: string) {
  storePersonValue(ASKED_KEY, "1", { userId });
  return noticeShell().allow().catch(() => false);
}

/**
 * Asks the system once to let the app show notifications, when the person
 * hasn't turned them off here, so macOS shows its permission prompt on its
 * own instead of only after the setting is turned on (seen 2026-10-04).
 */
export async function askDesktopNoticesOnce(userId: string) {
  if (turnedOff(userId) || asked(userId)) return;
  if (await noticeShell().allowed().catch(() => false)) return;
  await askSystem(userId);
}

async function shell(name: string, ...args: unknown[]): Promise<unknown> {
  const { Call } = await import("@wailsio/runtime");
  return Call.ByName(`main.DesktopNotices.${name}`, ...args);
}

export async function readDesktopNoticeState(session: Session): Promise<DesktopNoticeState> {
  if (turnedOff(session.user.id)) return "off";
  if (await noticeShell().allowed().catch(() => false)) return "on";
  return asked(session.user.id) ? "denied" : "off";
}

export async function turnDesktopNoticesOn(session: Session): Promise<DesktopNoticeState> {
  if (!(await askSystem(session.user.id))) return "denied";
  keepSetting(session.user.id, "on");
  return "on";
}

export async function turnDesktopNoticesOff(session: Session): Promise<DesktopNoticeState> {
  keepSetting(session.user.id, "off");
  await noticeShell().unread(0).catch(() => undefined);
  await noticeShell().watch(null).catch(() => undefined);
  return "off";
}

// The server takes 64 workspaces an ask; more are asked in turn and the
// answers joined, so every workspace's notices come.
const NOTICE_WORKSPACES_PER_ASK = 64;

async function askNotices(session: Session, workspaceIds: readonly string[], after: number | null, signal: AbortSignal) {
  const answers: DesktopNoticesAnswer[] = [];
  for (let start = 0; start < workspaceIds.length; start += NOTICE_WORKSPACES_PER_ASK) {
    const query = new URLSearchParams(workspaceIds.slice(start, start + NOTICE_WORKSPACES_PER_ASK).map((id) => ["workspaceId", id]));
    if (after !== null) query.set("after", String(after));
    answers.push(await request<DesktopNoticesAnswer>(`/account/desktop-notices?${query}`, { signal }, session));
  }
  return {
    notices: answers.flatMap((answer) => answer.notices),
    unreadCount: answers.reduce((sum, answer) => sum + answer.unreadCount, 0),
    through: Math.min(...answers.map((answer) => answer.through)),
  } satisfies DesktopNoticesAnswer;
}

/**
 * While the window is open on a desktop, shows the person's new comments and
 * mentions in the workspaces named and their unread count, and opens the page
 * of a notification clicked. On a phone the app's own inbox shows them while
 * it is open: the window keeps the count and what it saw, for the background
 * refresh.
 */
export function useDesktopNotices(session: Session, workspaceIds: readonly string[]) {
  const key = workspaceIds.join(",");
  useEffect(() => {
    const phone = phoneNoticesHere();
    if ((!desktopNoticesHere() && !phone) || !key) return;
    const workspaces = key.split(",");
    const controller = new AbortController();
    const topic = `desktop-notices:${serverOrigin()}:${session.user.id}:${key}`;
    const shown = new Set<string>();
    let after: number | null = keptPlace(session.user.id, key);
    let asking = false;
    let askAgain = false;
    // What one of the person's tabs read: each tab keeps where it got to and
    // what it showed, so a tab that asks next goes on from there; only the
    // tab that asked shows it and keeps the icon.
    const take = async (answer: DesktopNoticesAnswer, here: boolean) => {
      after = answer.through;
      if (here) keepPlace(session.user.id, key, answer.through);
      // Oldest first, each once.
      const fresh = [...answer.notices].sort((left, right) => left.occurredAt - right.occurredAt).filter((notice) => !shown.has(notice.id));
      for (const notice of fresh) {
        shown.add(notice.id);
        if (shown.size > SHOWN_LIMIT) shown.delete(shown.values().next().value!);
      }
      if (!here) return;
      if (!phone) for (const notice of fresh) await noticeShell().show(notice.id, notice.title, notice.body, notice.url).catch(() => undefined);
      await noticeShell().unread(answer.unreadCount).catch(() => undefined);
      // What the person saw moves on only while the window is in front: a
      // window left behind keeps the count, and what comes meanwhile waits
      // for the background refresh to show (2026-10-26: Android's hidden
      // window kept asking and marked a mention seen, so none came).
      if (phone && document.visibilityState === "visible") {
        await noticeShell().watch(phoneWatch(workspaces, answer.through)).catch(() => undefined);
      }
    };
    const ask = async () => {
      if (controller.signal.aborted) return;
      // A change while asking asks once more after, so the icon ends on it.
      if (asking) { askAgain = true; return; }
      asking = true;
      try {
        if (turnedOff(session.user.id)) return;
        const starting = after === null;
        const answer = await askNotices(session, workspaces, after, controller.signal);
        // A tab's first ask only learns where to start from: the others,
        // further on, keep their own place.
        if (starting) await take(answer, true);
        else tellTabs(topic, answer);
      } catch {
        // The server couldn't be reached; the next turn asks again.
      } finally {
        asking = false;
        if (askAgain) {
          askAgain = false;
          void ask();
        }
      }
    };
    // One of the person's tabs asks for all of them, in sight or not, as the
    // app's notifications come while it is behind; the others hear its
    // answer (followAcrossTabs; 2026-11-13: every tab asked every 20 seconds
    // after its own place, and each showed what came).
    const stopFollowing = followAcrossTabs(topic, ASK_EVERY_MS, ask, (value, here) => {
      if (value && typeof value === "object") void take(value as DesktopNoticesAnswer, here);
    }, { behind: true });
    // A tab that goes on from the others' place waits for its turn; one that
    // starts the app learns where to start.
    void askDesktopNoticesOnce(session.user.id).finally(() => { if (after === null) void ask(); });
    // The window's own count (its title, the ☰'s badge) follows the inbox at
    // once; the icon follows it within a second instead of at the next turn,
    // and once more as the window goes behind, where a phone's window stops
    // asking (2026-10-29: iOS kept 2 on its icon after the window showed 3
    // and was left before the next turn).
    let following: number | undefined;
    const stopUnread = onDocumentUnread(() => {
      window.clearTimeout(following);
      following = window.setTimeout(() => void ask(), FOLLOW_COUNT_MS);
    });
    const goingBehind = () => {
      if (phone && document.visibilityState === "hidden") void ask();
    };
    document.addEventListener("visibilitychange", goingBehind);
    let stopOpening: (() => void) | undefined;
    void import("@wailsio/runtime").then(({ Events, Window }) => {
      if (controller.signal.aborted) return;
      stopOpening = Events.On(OPEN_NOTICE_EVENT, async (event: { data: unknown }) => {
        const url = Array.isArray(event.data) ? event.data[0] : event.data;
        // The app names the page in front to open it (its shown tab on a Mac);
        // the app's other pages leave it.
        const target = Array.isArray(event.data) ? event.data[1] : "";
        if (typeof target === "string" && target && target !== await Window.Name().catch(() => "")) return;
        if (typeof url === "string" && url.startsWith("/") && !url.startsWith("//")) pushRoute(url);
      });
    }, () => undefined);
    return () => {
      controller.abort();
      stopFollowing();
      window.clearTimeout(following);
      stopUnread();
      document.removeEventListener("visibilitychange", goingBehind);
      stopOpening?.();
    };
  }, [key, session]);
}
