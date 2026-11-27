import { LeafenApiError } from "@/apiTransport";
import { readMCPService, signInMCPService } from "@/mcpServicesApi";
import { signInWindowEnded, type SignInWord } from "@/lib/signInWindow";
import type { Session } from "@/types";

// A service's sign-in page opens in a window of its own. The window is
// opened while the person's click still counts, before anything is awaited,
// and pointed at the page once the server answers with it.

export function openServiceSignInWindow() {
  return window.open("about:blank", "leafen-service-sign-in", "popup,width=520,height=720");
}

/** The word of the page a service's sign-in comes back to; one that names no service (its state expired) is the open window's too. */
export function serviceSignInWord(serviceId: string) {
  return (word: SignInWord) => word.type === "leafen:mcp-service" && (word.service === serviceId || !word.service);
}

/**
 * Why a sign-in that came back failed, as a refusal with the code its page
 * named (the service's sign-in server down for now), or null.
 */
export function serviceSignInFailure(word: SignInWord) {
  return word.ok === false && typeof word.code === "string" ? new LeafenApiError(word.code, 502, "", word.code) : null;
}

/**
 * Signs in to a connected service again in the window already opened, until
 * it comes back or the person leaves it (lib/signInWindow.ts); `late` hears a
 * sign-in that finished after they left. One that came back saying why it
 * failed throws that.
 */
export async function signInServiceIn(popup: Window, session: Session, serviceId: string, late?: () => void) {
  try {
    const answer = await signInMCPService(session, serviceId);
    if (!answer.authorizeUrl) throw new Error("The service has no login page.");
    popup.location.href = answer.authorizeUrl;
  } catch (failure) {
    popup.close();
    throw failure;
  }
  // A row asks to sign in again only while its service isn't connected.
  const heard: { failure: LeafenApiError | null } = { failure: null };
  const ours = serviceSignInWord(serviceId);
  await signInWindowEnded(popup, {
    matches: (word) => {
      if (!ours(word)) return false;
      heard.failure = serviceSignInFailure(word);
      return true;
    },
    completed: async () => (await readMCPService(session, serviceId)).service.status === "connected",
    late,
  });
  if (heard.failure) throw heard.failure;
}
