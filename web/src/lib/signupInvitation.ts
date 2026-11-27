import type { TFunction } from "i18next";
import { authErrorCodes } from "@/authWire.generated";
import { authLine, type AuthLine, type AuthText } from "@/lib/authLine";
import { isPageBusyRefusal } from "@/lib/pageBusy";

const INVITATION_IN_HASH = /(?:^#|&)invitation=([^&]+)/u;
const JOIN_PATH = /^\/join\/?$/u;

/** Whether the address is where a workspace's or teamspace's invite link opens. */
export function isJoinPath(pathname: string): boolean {
  return JOIN_PATH.test(pathname);
}

/**
 * The invitation the address carries for sign-up, where sign-up is closed
 * (2026-10-18): an email invitation's link ends in "#invitation=<token>",
 * and a workspace's or teamspace's invite link opens /join#<key> — or
 * /join#invitation=<key>, as "앱에서 열기" hands the sign-in's invitation on
 * (AuthGate.tsx; 2026-11-12: the app read "invitation=<key>" as the key).
 */
export function readSignupInvitation(): string {
  if (typeof window === "undefined") return "";
  try {
    const { pathname, hash } = window.location;
    const match = INVITATION_IN_HASH.exec(hash);
    if (match) return decodeURIComponent(match[1]).trim();
    return isJoinPath(pathname) ? decodeURIComponent(hash.slice(1)).trim() : "";
  } catch {
    return "";
  }
}

/**
 * Whether an invitation opens as a page's or a person's — an email's, a
 * name's, or a guest invite link to pages — rather than as a workspace's or
 * teamspace's invite link, whatever address carries it.
 */
export function personalInvitation(token: string): boolean {
  return token.startsWith("inv_") || token.startsWith("nin_") || token.startsWith("pgl_");
}

/**
 * An email invitation's token leaves the address once its account is made;
 * an invite link's key stays, in the join page's own form, as its join page
 * reads it.
 */
export function dropSignupInvitationFromAddress() {
  if (typeof window === "undefined") return;
  const { pathname, search, hash } = window.location;
  const match = INVITATION_IN_HASH.exec(hash);
  if (!match) return;
  window.history.replaceState(window.history.state, "", isJoinPath(pathname) ? `${pathname}${search}#${match[1]}` : `${pathname}${search}`);
}

// Why an invitation lets no one in, as the server says it beside the
// invitation's code (invitation_ended.go), each kind in its own words: a
// guest invite link used by its one person, past its end, turned off, not
// found, or with no page left to share; an invitation by name used,
// withdrawn, past its seven days or not found; a workspace's or teamspace's
// invite link turned off or not found (made anew, say); its workspace
// deleted, or the server no longer letting workspaces add members. An email
// invitation says every way it may have ended, as before.
const INVITATION_ENDED: Record<string, string> = {
  guestLinkUsed: "authGate:guestLinkUsed", guestLinkExpired: "authGate:guestLinkExpired", guestLinkOff: "authGate:guestLinkOff",
  guestLinkUnknown: "authGate:guestLinkUnknown", guestLinkNoPages: "authGate:guestLinkNoPages",
  nameInvitationUsed: "authGate:nameInvitationUsed", nameInvitationWithdrawn: "authGate:nameInvitationWithdrawn",
  nameInvitationExpired: "authGate:nameInvitationExpired", nameInvitationUnknown: "authGate:nameInvitationUnknown",
  inviteLinkOff: "authGate:inviteLinkOff", inviteLinkUnknown: "authGate:inviteLinkUnknown",
  invitationWorkspaceDeleted: "authGate:invitationWorkspaceDeleted", invitationMemberAddsStopped: "authGate:invitationMemberAddsStopped",
};

/** Why an invitation can't be used: the cause the server named, else every way an email invitation ends. */
export function invitationInvalidText(caught: unknown, t: TFunction): string {
  return invitationEndedText(caught, t) || t("authGate:signupInvitationInvalid");
}

/** The cause the server named for an invitation that ended, "" where it named none. */
export function invitationEndedText(caught: unknown, t: TFunction): string {
  // The API error's typed cause (apiTransport.ts), never its prose.
  const cause = (caught as { causeKey?: unknown } | null)?.causeKey;
  const key = typeof cause === "string" ? INVITATION_ENDED[cause] : undefined;
  return key ? t(key) : "";
}

// What holds a page of a guest invite link taken meanwhile, as the server
// names it (page_guest_link_held.go): its blocks moved, its copy, a wiki's change.
const GUEST_LINK_PAGE_HELD = new Set(["moving", "copying", "wiki"]);

/**
 * A guest invite link refused because an operation holds one of its pages a
 * while (the server's 423): what holds it, that the link wasn't used, and to
 * open it again once that's done (2026-11-23: a one-person link taken then
 * was spent without the page); "" for any other failure.
 */
export function guestLinkHeldText(caught: unknown, t: TFunction): string {
  if (!isPageBusyRefusal(caught)) return "";
  const cause = (caught as { causeKey?: unknown }).causeKey;
  return t(`authGate:guestLinkPageHeld.${typeof cause === "string" && GUEST_LINK_PAGE_HELD.has(cause) ? cause : "other"}`);
}

/** Where sign-up is closed: why, and whom to ask if the administrator named someone. */
export function signupClosedNotice(t: TFunction, contact: string, mail: boolean): string {
  // Without mail an invited address can't be proven; only the invitation's link lets its person in.
  const notice = t(mail ? "authGate:signupRestricted" : "authGate:signupRestrictedNoMail");
  return contact ? `${notice} ${t("authGate:signupContact", { contact })}` : notice;
}

/**
 * The sign-up step where sign-up is closed: who may still make an account
 * (an invited address, or a company address the administrator allows) and
 * whom to ask; one refused hears signupClosedNotice.
 */
export function signupClosedStepNotice(t: TFunction, contact: string, mail: boolean): string {
  const notice = t(mail ? "authGate:signupClosedWhoMay" : "authGate:signupClosedWhoMayNoMail");
  return contact ? `${notice} ${t("authGate:signupContact", { contact })}` : notice;
}

/**
 * A refusal of sign-up said in the person's words, or "" for any other
 * failure, held as what to say (lib/authLine.ts). Mail the server couldn't
 * send stops sign-up too and sends the person to the administrator, so it
 * names whom to ask, as a closed server's refusal does (2026-11-07).
 */
export function signupRefusal(
  caught: unknown,
  contact: string,
  mail: boolean,
  failure: () => AuthLine,
): AuthText {
  const code = (caught as { code?: unknown } | null)?.code;
  if (code === authErrorCodes.signupRestricted) return authLine((t) => signupClosedNotice(t, contact, mail));
  if (code === authErrorCodes.signupInvitationInvalid) return authLine((t) => invitationInvalidText(caught, t));
  if (isPageBusyRefusal(caught)) return authLine((t) => guestLinkHeldText(caught, t));
  if (code === authErrorCodes.emailDeliveryFailed && contact) {
    // Whom to ask goes with what to do, before the reference's own line.
    const failed = failure();
    return authLine((t) => {
      const [said, ...reference] = failed.say(t).split("\n");
      return [`${said} ${t("authGate:signupContact", { contact })}`, ...reference].join("\n");
    });
  }
  return "";
}
