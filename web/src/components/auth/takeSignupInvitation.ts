import { api } from "@/api";
import { isPageBusyRefusal } from "@/lib/pageBusy";
import { dropSignupInvitationFromAddress } from "@/lib/signupInvitation";
import type { Session } from "@/types";

/**
 * A guest invite link (pgl_, page_guest_links.go) taken by any account signed
 * in, which it lets into its pages; an email invitation's link taken by the
 * signed-in account that holds its address (2026-11-11: an account made before a page was shared with its
 * address met "사용 권한 없음"), as Notion's invitation lets an existing
 * account in: the server proves the address by the link's token and lets the
 * account into what the address was invited to. Says where to go — the page a
 * page's invitation shares — or null where the invitation isn't the
 * account's, which is then said as before (SignedInLinkNotice.tsx); refused
 * hears why the take was refused, for a link whose page is held a while.
 */
export async function takeSignupInvitation(
  session: Session, token: string, refused?: (caught: unknown) => void,
): Promise<{ pageId?: string } | null> {
  if (!token.startsWith("inv_") && !token.startsWith("pgl_")) return null;
  try {
    const taken = await api.takeSignupInvitation(session, token);
    dropSignupInvitationFromAddress();
    return taken;
  } catch (caught) {
    refused?.(caught);
    return null;
  }
}

/**
 * takeSignupInvitation as an account signs in with the invitation's link in
 * the address (AuthGate.tsx): a guest invite link one of whose pages an
 * operation holds a while isn't taken, and the app opens again signed in at
 * returnPath with the link, where its card says so and to open it again once
 * that's done (SignedInLinkNotice.tsx) — true then, the page already going
 * there (2026-11-23: signing in through such a link went in without the page
 * and without a word).
 */
export async function takeSignupInvitationSigningIn(session: Session, token: string, returnPath: string): Promise<boolean> {
  let held = false;
  await takeSignupInvitation(session, token, (caught) => { held = isPageBusyRefusal(caught); });
  if (!held) return false;
  // Loaded again, as the address may differ from the one on screen by its hash alone.
  window.history.replaceState(null, "", `${returnPath.split("#")[0]}#invitation=${encodeURIComponent(token)}`);
  window.location.reload();
  return true;
}
