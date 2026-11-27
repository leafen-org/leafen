import { cacheLanguagePreference, resolvedLanguageForPreference } from "@/i18n";
import { loadAuthenticatedSession } from "@/components/auth/authenticatedModules";
import { saveSession } from "@/lib/authSessionStore";
import { wakeTabsBehind } from "@/lib/desktopTabs";
import { markFreshAccount } from "@/lib/freshAccount";
import { firstRunLanguage } from "@/lib/firstRunLanguage";
import { newId } from "@/lib/ids";
import { prepareInitialWorkspace } from "@/lib/initialWorkspaceSetup";
import { pageHref } from "@/lib/navigation";
import { replaceRoute } from "@/lib/router";
import type { Session } from "@/types";

/** Where an account's first language went: its welcome page, or the empty workspace list when none was made. */
export type SettledLanguage = { page: string | null; restricted: boolean; joinable: boolean };

/** A first administrator as the claim answers: with the page their first workspace opens on, when the claim made it. */
export type ClaimedSession = Session & { firstPage?: string };

// This tab's latest claim, by which the server tells its own setup from one
// done elsewhere (internal/app/setup_claims.go, lib/agentStartRequest.ts).
let latestClaim = "";

/** The claim this tab sent last, or none. */
export function ownClaim(): string {
  return latestClaim;
}

/**
 * The first setup's claim: the language chosen on its first screen
 * (lib/firstRunLanguage.ts), and a new id the setup screen's asks name it by.
 */
export function claimInput(): { language?: string; requestId: string } {
  const language = firstRunLanguage();
  latestClaim = newId();
  return { ...(language ? { language } : {}), requestId: latestClaim };
}

/**
 * A first administrator made here goes on as the screen hands them on
 * (`accept`). Made with the language chosen on the first screen, which the
 * server gave the account as it made it, their first workspace is made in
 * it while the setup screen stays busy, and the account goes on as its
 * language step's "계속" would: neither that step nor a loading screen
 * comes between the name and the next step (the first run goes from one
 * step's busy button to the next). Otherwise — made by an agent's
 * conversation, or the workspace not made — the account's steps take the
 * language (AuthenticatedAccountGates, lib/freshAccount.ts). One set up in
 * another tab (not `here`) is read as that tab left it. The page opens only
 * once `accept` kept the sign-in: a page with no sign-in behind it is the
 * sign-in screen.
 */
export async function startFirstAdministrator(claimedSession: ClaimedSession, here: boolean, claimed: boolean,
  accept: (session: Session) => Promise<boolean>): Promise<void> {
  const { firstPage, ...session } = claimedSession;
  // The claim's sign-in is kept the moment it answers, before the screen goes
  // on: whatever comes between — a reload, the window closed — leaves the first
  // administrator signed in (their account has only a name, no other way in).
  if (claimed) await saveSession(session).catch(() => false);
  const language = here ? firstRunLanguage() : "";
  const settled = claimed && language ? await settleLanguage(session, language, firstPage ?? "") : null;
  if (here && !settled) markFreshAccount(session.user.id, language);
  if (await accept(session) && settled) openSettled(settled);
}

async function settleLanguage(session: Session, preference: string, firstPage: string): Promise<SettledLanguage | null> {
  try {
    // The product's code comes meanwhile: the account's steps open on it at
    // once, with no loading screen between the name and the next step. The
    // claim made the first workspace where it could (its first page); the
    // screen makes it otherwise ("follow the device" is resolved here).
    const [initial] = await Promise.all([firstPage ? { page: firstPage, restricted: false, joinable: false } : prepareInitialWorkspace({
      session, language: resolvedLanguageForPreference(preference), requestId: newId(),
      signal: new AbortController().signal, isCurrent: () => true,
    }).then((made) => ({ page: made.page?.id ?? null, restricted: made.restricted, joinable: made.joinable })),
    loadAuthenticatedSession().then((module) => module.preloadAuthenticatedProduct()).catch(() => undefined)]);
    if (!cacheLanguagePreference(session.user.id, preference)) return null;
    // The app's tabs behind go where this one went, awake.
    wakeTabsBehind();
    return initial;
  } catch {
    return null;
  }
}

/** Opens where the account's first language went, but on an invite link's page, which stays to be answered (components/join). */
export function openSettled(settled: SettledLanguage): void {
  if (/^\/join\/?$/u.test(window.location.pathname)) return;
  if (settled.page) replaceRoute(pageHref(settled.page));
  else if (settled.restricted || settled.joinable) replaceRoute("/");
}
