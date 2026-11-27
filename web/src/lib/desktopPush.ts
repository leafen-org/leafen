import { api } from "@/api";
import type { Session } from "@/types";
import {
  desktopNoticesHere,
  phoneNoticesHere,
  readDesktopNoticeState,
  turnDesktopNoticesOff,
  turnDesktopNoticesOn,
} from "./desktopNotices";

/**
 * Desktop push is turned on per browser: the browser allows notifications,
 * subscribes with the server's key, and the server keeps the subscription
 * for whoever is signed in. Signing out or turning it off forgets it.
 */
export type DesktopPushState = "unsupported" | "denied" | "on" | "off";

function supported() {
  return typeof window !== "undefined" && "serviceWorker" in navigator
    && "PushManager" in window && "Notification" in window;
}

/**
 * Whether this is an iPhone's or iPad's browser, where web push comes only
 * to Leafen added to the home screen (iOS 16.4 or later); the settings say
 * so instead of "this browser can't".
 */
export function iosBrowserWithoutPush(): boolean {
  if (typeof window === "undefined" || supported()) return false;
  const ios = /iPhone|iPad|iPod/u.test(navigator.userAgent) || (/Macintosh/u.test(navigator.userAgent) && navigator.maxTouchPoints > 1);
  const standalone = (navigator as Navigator & { standalone?: boolean }).standalone === true
    || window.matchMedia?.("(display-mode: standalone)").matches === true;
  return ios && !standalone && !phoneNoticesHere();
}

/** The app shows notifications itself here: a desktop's, or a phone's by background refresh. */
function appNoticesHere() {
  return desktopNoticesHere() || phoneNoticesHere();
}

/** A phone or a tablet (an iPad's Safari names itself a Mac, one with touch). */
function mobileDevice() {
  const agent = navigator.userAgent;
  return /Android|iPhone|iPad|iPod/u.test(agent) || (/Macintosh/u.test(agent) && navigator.maxTouchPoints > 1);
}

/**
 * The push row's name and description where it shows: the phone app's own,
 * the desktop app's, and in a browser or a home-screen Leafen the device's,
 * a phone's or tablet's being "mobile" as Notion names its own (seen
 * 2026-10-26: "Mobile push notifications"), not "desktop". The settings'
 * search finds the row by the same.
 */
export function pushSettingKeys(): { title: string; description: string } {
  if (phoneNoticesHere()) return { title: "notificationsPhoneApp", description: "notificationsPhoneAppDesc" };
  if (desktopNoticesHere()) return { title: "notificationsDesktopPush", description: "notificationsDesktopAppDesc" };
  if (typeof window !== "undefined" && mobileDevice()) return { title: "notificationsMobilePush", description: "notificationsMobilePushDesc" };
  return { title: "notificationsDesktopPush", description: "notificationsDesktopPushDesc" };
}

// A step that never ends fails the switch rather than leave it busy for good
// (2026-10-17 a service worker that never got ready; 2026-10-26 an iPhone
// whose push service never started, so neither its subscription nor the
// permission ask answered).
function within<T>(work: Promise<T>, milliseconds: number, what: string): Promise<T> {
  return Promise.race([
    work,
    new Promise<never>((_, reject) => setTimeout(() => reject(new DOMException(what, "TimeoutError")), milliseconds)),
  ]);
}

function applicationServerKey(publicKey: string) {
  const padded = `${publicKey}${"=".repeat((4 - (publicKey.length % 4)) % 4)}`
    .replaceAll("-", "+").replaceAll("_", "/");
  return Uint8Array.from(atob(padded), (character) => character.charCodeAt(0));
}

async function currentSubscription() {
  const registration = await within(navigator.serviceWorker.getRegistration("/"), 10_000, "The service worker didn't answer.");
  return registration
    ? { registration, subscription: await within(registration.pushManager.getSubscription(), 10_000, "The push subscription didn't answer.") }
    : null;
}

function subscriptionInput(subscription: PushSubscription) {
  const json = subscription.toJSON();
  return { endpoint: subscription.endpoint, keys: { p256dh: json.keys?.p256dh ?? "", auth: json.keys?.auth ?? "" } };
}

/**
 * Reads this browser's state and keeps an existing subscription with the
 * signed-in person. The desktop app shows notifications itself instead.
 * "unsupported" is a browser without push alone: one whose offline worker
 * hasn't registered yet — a first visit before its start has settled
 * (serviceWorker.ts) — holds no subscription, so its push is off, and
 * turning it on waits for the worker (2026-11-18: a phone's settings opened
 * in that moment said this browser couldn't, and went on saying so once the
 * worker ran).
 */
export async function readDesktopPushState(session: Session): Promise<DesktopPushState> {
  if (appNoticesHere()) return readDesktopNoticeState(session);
  if (!supported()) return "unsupported";
  if (Notification.permission === "denied") return "denied";
  const current = await currentSubscription();
  if (!current?.subscription) return "off";
  // Kept with the person behind the switch, which shows what this browser
  // holds without waiting on the server (2026-11-21: an iPhone's settings
  // showed the switch grey a while as they opened).
  void api.saveWebPushSubscription(session, subscriptionInput(current.subscription)).catch(() => undefined);
  return "on";
}

/**
 * What turning push on or off waits on at each of its steps (`waitsOn`): the
 * browser — its permission ask, its offline worker, its push service — the
 * system an app asks, or the server; the switch says the server isn't
 * answering only while it waits on the server (lib/switchTurns.ts).
 */
export type PushWait = "browser" | "device" | "server";

export async function turnDesktopPushOn(session: Session, waitsOn: (on: PushWait) => void = () => {}): Promise<DesktopPushState> {
  if (appNoticesHere()) { waitsOn("device"); return turnDesktopNoticesOn(session); }
  if (!supported()) return "unsupported";
  waitsOn("browser");
  // The ask waits on the person too, so it has longer. One closed without an
  // answer ("default") can be asked again: push stays off and the switch
  // free; only a refusal is left to the browser's settings (2026-11-21: a
  // prompt that went away said this site was blocked there).
  const permission = await within(Notification.requestPermission(), 60_000, "The permission ask didn't end.");
  if (permission !== "granted") return permission === "denied" ? "denied" : "off";
  waitsOn("server");
  const { publicKey } = await api.readWebPushState(session);
  waitsOn("browser");
  const registration = await within(navigator.serviceWorker.ready, 10_000, "The service worker isn't ready.");
  const subscription = await within((async () => await registration.pushManager.getSubscription()
    ?? await registration.pushManager.subscribe({
      userVisibleOnly: true, applicationServerKey: applicationServerKey(publicKey),
    }))(), 10_000, "The push subscription didn't end.");
  waitsOn("server");
  await api.saveWebPushSubscription(session, subscriptionInput(subscription));
  return "on";
}

export async function turnDesktopPushOff(session: Session, waitsOn: (on: PushWait) => void = () => {}): Promise<DesktopPushState> {
  if (appNoticesHere()) { waitsOn("device"); return turnDesktopNoticesOff(session); }
  if (!supported()) return "unsupported";
  waitsOn("browser");
  const current = await currentSubscription();
  if (current?.subscription) {
    waitsOn("server");
    await api.deleteWebPushSubscription(session, current.subscription.endpoint);
    waitsOn("browser");
    await current.subscription.unsubscribe();
  }
  return "off";
}
