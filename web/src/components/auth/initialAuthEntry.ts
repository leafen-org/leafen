import { consumeAuthCallbackCredential } from "@/lib/authCallbackCredential";
import { dropSignupInvitationFromAddress, isJoinPath, readSignupInvitation } from "@/lib/signupInvitation";

// The link the address carries as the page loads — a sign-in's return, a
// link from mail, a setup or device code — taken from the address at once
// and held for the sign-in screen (AuthGate.tsx), or for the signed-in app
// to say what became of it.

export type InitialAuthEntry = { pathname: string; credential: string; organization: string; failure?: { code: string; reference: string } };

/** A code in the address as it was typed, or none where its escapes don't read. */
function typedPathCode(segment: string): string {
  try {
    return decodeURIComponent(segment);
  } catch {
    return "";
  }
}

const SETUP_LINK_CODE_KEY = "leafen:v2:setup-link-code";

function keepSetupLinkCode(code: string) {
  try {
    window.sessionStorage.setItem(SETUP_LINK_CODE_KEY, code);
  } catch {
    // Without storage a reload asks for the code, which the link gives again.
  }
}

function keptSetupLinkCode(): string {
  try {
    return window.sessionStorage.getItem(SETUP_LINK_CODE_KEY) ?? "";
  } catch {
    return "";
  }
}

/** The setup is done: its code has nothing left to open. */
export function forgetSetupLinkCode() {
  try {
    window.sessionStorage.removeItem(SETUP_LINK_CODE_KEY);
  } catch {
    // Nothing was kept.
  }
}

function consumeInitialAuthEntry(): InitialAuthEntry {
  if (typeof window === "undefined") return { pathname: "", credential: "", organization: "" };
  const pathname = window.location.pathname;
  if (pathname === "/auth/callback") {
    // A provider step the server refused comes back with its code and reference.
    const url = new URL(window.location.href);
    const code = url.searchParams.get("error") ?? "";
    if (/^LEAFEN_[A-Z0-9_]{1,58}$/u.test(code)) {
      const reference = url.searchParams.get("reference") ?? "";
      window.history.replaceState(window.history.state, "", "/");
      return { pathname, credential: "", organization: "", failure: { code, reference } };
    }
    return { pathname, credential: consumeAuthCallbackCredential("code"), organization: "" };
  }
  if (pathname === "/verify-email" || pathname === "/reset-password") {
    return { pathname, credential: consumeAuthCallbackCredential("token"), organization: "" };
  }
  if (pathname === "/auth/saml/callback") {
    return { pathname, credential: consumeAuthCallbackCredential("samlCompletion"), organization: "" };
  }
  if (pathname === "/auth/saml") {
    const organization = new URL(window.location.href).searchParams.get("organization")?.trim() ?? "";
    return { pathname, credential: "", organization };
  }
  // The server's printed setup link carries its one-time code after "#code=".
  // It leaves the address at once but stays with this tab until the setup is
  // done, so a reload on the way asks for nothing again.
  if (pathname === "/setup") {
    const fragment = new URLSearchParams(window.location.hash.slice(1));
    const linked = fragment.get("code")?.trim() ?? "";
    if (linked) {
      // What else came after "#" (another server's page handing its list
      // over, switcherLists.ts) stays for what reads it.
      fragment.delete("code");
      const rest = fragment.toString();
      window.history.replaceState(window.history.state, "", rest ? `/setup#${rest}` : "/setup");
      keepSetupLinkCode(linked);
    }
    return { pathname, credential: linked || keptSetupLinkCode(), organization: "" };
  }
  // A signed-in device's QR to join this one; the code leaves the address at once.
  // It goes to the server as typed, which reads it (a code typed with a Korean
  // keyboard on comes as Hangul, account_device_codes.go).
  const device = /^\/device\/([^/]{8,160})\/?$/u.exec(pathname);
  if (device) {
    // A sign-in carried from another address of this server lands on the
    // page it was on (ProperAddressOffer.tsx); only a path here.
    const next = new URLSearchParams(window.location.search).get("next") ?? "";
    window.history.replaceState(window.history.state, "", /^\/(?![/\\])/u.test(next) ? next : "/");
    return { pathname: "/device", credential: typedPathCode(device[1] ?? ""), organization: "" };
  }
  return { pathname, credential: "", organization: "" };
}

const initialAuthEntry = consumeInitialAuthEntry();
// The address's link (a sign-in's return, an email link, a join code) is
// spent once it has had its go — signed in, refused, or left by a signed-in
// browser: signed out later in the same load, the sign-in screen opens plain
// rather than trying it again.
let initialAuthEntryTaken = false;
const NO_AUTH_ENTRY: InitialAuthEntry = { pathname: "", credential: "", organization: "" };

/** The address's link, or none once it was used. */
export function unusedAuthEntry(): InitialAuthEntry {
  return initialAuthEntryTaken ? NO_AUTH_ENTRY : initialAuthEntry;
}

/**
 * The address's link had its one go — signed in with, refused, or said to be
 * left: signed out later in the same load, it isn't tried again, and nothing
 * of it goes on with the tab to a later load. One read as left by a stored
 * sign-in the server no longer took is the sign-in screen's to use (App.tsx),
 * and used there it isn't said after the next reload (2026-11-25: a tab that
 * signed in through a device link said "기기 등록 링크입니다 … 이미 로그인돼 있어"
 * on the next page it reloaded).
 */
export function spendAuthEntry() {
  initialAuthEntryTaken = true;
  unsaidLink = null;
}

/** The address's credential, as a reset's step sends it with the new password. */
export function authEntryCredential(): string {
  return initialAuthEntry.credential;
}

// An auth link an app or browser opens while signed in: a device code or
// setup link goes home; a reset, an email check or a provider's return does its one thing on
// this screen's own step, with the way back to the workspace (App.tsx).
const SIGNED_IN_GATE_LINKS = new Set(["/reset-password", "/verify-email", "/auth/callback", "/auth/saml/callback", "/auth/saml"]);

/** Whether the address's auth link, not yet used, waits for this screen in a signed-in app. */
export function authLinkWaitsWhileSignedIn(): boolean {
  return !initialAuthEntryTaken && SIGNED_IN_GATE_LINKS.has(initialAuthEntry.pathname);
}

/**
 * Whether a signed-in app on this auth address has nothing left to read
 * there: the page's own link had its go in this load, and the address
 * carries no other. Read again, the spent link showed "완료했습니다" over "이 계정
 * 작업 링크에 토큰이 없습니다." after an email check had signed in (2026-11-06).
 */
export function authAddressSpent(pathname: string): boolean {
  if (!initialAuthEntryTaken || initialAuthEntry.pathname !== pathname) return false;
  return !/[?#&](?:token|code|samlCompletion|organization)=/u.test(`${window.location.search}${window.location.hash}`);
}

/** What a signed-in app does on an auth address it reaches. */
export function signedInAuthAddress(pathname: string): "home" | "gate" | null {
  if (pathname === "/setup" || pathname === "/setup/" || /^\/device(?:\/|$)/u.test(pathname)) return "home";
  return SIGNED_IN_GATE_LINKS.has(pathname) ? "gate" : null;
}

// The links read here as the page loads — an invitation, a workspace's
// /join, a setup code, a reset token — come after "#", and a link that
// differs from the tab's address only there (pasted into the address bar,
// say) changes the hash without loading the page: the page loads again for
// it, as a view link's does (2026-10-29: an invitation pasted into a server's
// sign-in tab did nothing until a reload).
if (typeof window !== "undefined") {
  window.addEventListener("hashchange", () => {
    const { pathname, hash } = window.location;
    if (isJoinPath(pathname) ? hash.length > 1 : /(?:^#|&)(?:invitation|code|token)=/u.test(hash)) window.location.reload();
  });
}

// An invitation's link (an email's, or one by name — not a workspace's
// /join, which a signed-in person answers) as the page loads.
const initialInvitation = typeof window === "undefined" || isJoinPath(window.location.pathname) ? "" : readSignupInvitation();

export type LinkLeftWhileSignedIn = { kind: "invitation"; token: string } | { kind: "device"; code: string };

// A link left while signed in, read but not yet said, goes with this tab
// through a reload that comes first — the one that brings the account's
// language (lib/accountLanguageSettled.ts), or the person's own — for the
// next load to say, as the address may no longer carry it: a device's code
// left it as the page loaded, and the start page puts its own address in
// place of "/" (2026-11-23: the first link opened after the account's
// language changed elsewhere lost its card).
const LEFT_LINK_KEY = "leafen:v2:link-left-while-signed-in";
let unsaidLink: LinkLeftWhileSignedIn | null = null;

function takeKeptLeftLink(): LinkLeftWhileSignedIn | null {
  try {
    const kept = JSON.parse(window.sessionStorage.getItem(LEFT_LINK_KEY) ?? "null") as Partial<{ kind: string; token: string; code: string }> | null;
    window.sessionStorage.removeItem(LEFT_LINK_KEY);
    if (kept?.kind === "invitation" && typeof kept.token === "string" && kept.token) return { kind: "invitation", token: kept.token };
    if (kept?.kind === "device" && typeof kept.code === "string" && kept.code) return { kind: "device", code: kept.code };
  } catch {
    // Nothing kept where nothing can be.
  }
  return null;
}

const keptLeftLink = typeof window === "undefined" ? null : takeKeptLeftLink();
if (typeof window !== "undefined") {
  window.addEventListener("pagehide", (event) => {
    // A page the browser keeps whole comes back with its link.
    if (!unsaidLink || event.persisted) return;
    try {
      window.sessionStorage.setItem(LEFT_LINK_KEY, JSON.stringify(unsaidLink));
    } catch {
      // Without storage the next load has only its address.
    }
  });
}

/**
 * An invitation's or a device's link opened in a browser already signed in:
 * it would make or sign in another account, which this browser, holding one
 * account a server, doesn't do there. The app says so rather than drop it
 * (2026-10-26: both went without a word).
 */
export function linkLeftWhileSignedIn(): LinkLeftWhileSignedIn | null {
  if (initialAuthEntryTaken) return null;
  if (initialAuthEntry.pathname === "/device" && initialAuthEntry.credential) unsaidLink = { kind: "device", code: initialAuthEntry.credential };
  else unsaidLink = initialInvitation ? { kind: "invitation", token: initialInvitation } : keptLeftLink;
  return unsaidLink;
}

/**
 * A link said to be left counts as used: signed out later in the same load,
 * the sign-in screen opens plain rather than trying it, and signing in again
 * doesn't say so again (2026-10-27: a device link left on a signed-in tab
 * came back as a wrong code after a password reset signed it out).
 */
export function settleLinkLeftWhileSignedIn() {
  spendAuthEntry();
  dropSignupInvitationFromAddress();
}
