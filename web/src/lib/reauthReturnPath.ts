import { isPublicAnonymousPath, isPublicFormPath } from "./router";

const REAUTH_RETURN_PATH_KEY = "leafen:auth:reauth-return-to";

interface ReauthReturnPathRecord {
  userId: string;
  path: string;
}

function privateRelativePath(value: unknown): string | null {
  if (typeof value !== "string" || !value.startsWith("/")) return null;
  if (value.startsWith("//") || value.startsWith("/\\")) return null;
  try {
    const parsed = new URL(value, "https://leafen.invalid");
    if (parsed.origin !== "https://leafen.invalid") return null;
    if (parsed.pathname === "/" || parsed.pathname.startsWith("/auth/")) return null;
    // A form's link is a place to come back to: signed in, it opens as a page of the app.
    if (isPublicAnonymousPath(parsed.pathname) && !isPublicFormPath(parsed.pathname)) return null;
    return `${parsed.pathname}${parsed.search}${parsed.hash}`;
  } catch {
    return null;
  }
}

/**
 * The person signed out on purpose: no place is kept for the next sign-in,
 * which opens the start page, as a first one does (useStartPage); their other
 * tabs learn it was asked for from lib/signOutMarks.ts.
 */
export function forgetReauthReturnPathForSignOut() {
  if (typeof window === "undefined") return;
  try {
    window.sessionStorage.removeItem(REAUTH_RETURN_PATH_KEY);
  } catch {
    // Without storage the sign-in screen's own address, the root, is the place.
  }
}

export function rememberReauthReturnPath(userId: string) {
  if (typeof window === "undefined") return;
  const normalizedUserId = userId.trim();
  const path = privateRelativePath(
    `${window.location.pathname}${window.location.search}${window.location.hash}`,
  );
  try {
    if (!normalizedUserId || !path) {
      window.sessionStorage.removeItem(REAUTH_RETURN_PATH_KEY);
      return;
    }
    const record: ReauthReturnPathRecord = { userId: normalizedUserId, path };
    window.sessionStorage.setItem(REAUTH_RETURN_PATH_KEY, JSON.stringify(record));
  } catch {
    // Restricted storage falls back to the ordinary workspace-home route.
  }
}

/**
 * Where the sign-in under way goes on: a link opened on the sign-in screen
 * names its place, over the place a renewed sign-in left.
 */
export function returnAfterSignInTo(path: string) {
  if (typeof window === "undefined") return;
  const place = privateRelativePath(path);
  if (!place) return;
  window.history.replaceState(null, "", place);
  try {
    const record = JSON.parse(window.sessionStorage.getItem(REAUTH_RETURN_PATH_KEY) ?? "null") as Partial<ReauthReturnPathRecord> | null;
    if (record?.userId) window.sessionStorage.setItem(REAUTH_RETURN_PATH_KEY, JSON.stringify({ userId: record.userId, path: place }));
  } catch {
    // The address the sign-in screen shows keeps the place.
  }
}

function consumeReauthReturnPath(userId: string) {
  if (typeof window === "undefined") return "/";
  let raw: string | null = null;
  try {
    raw = window.sessionStorage.getItem(REAUTH_RETURN_PATH_KEY);
    window.sessionStorage.removeItem(REAUTH_RETURN_PATH_KEY);
  } catch {
    return "/";
  }
  if (!raw) return "/";
  try {
    const record = JSON.parse(raw) as Partial<ReauthReturnPathRecord>;
    if (record.userId !== userId.trim()) return "/";
    return privateRelativePath(record.path) ?? "/";
  } catch {
    return "/";
  }
}

/**
 * Where a sign-in goes on: the address a renewed sign-in left, else the
 * private address the person opened while signed out (a page, an app's
 * place), else home — as a link opened before signing in keeps its place.
 */
export function signInReturnPath(userId: string) {
  const returned = consumeReauthReturnPath(userId);
  if (returned !== "/" || typeof window === "undefined") return returned;
  return privateRelativePath(`${window.location.pathname}${window.location.search}${window.location.hash}`) ?? "/";
}
