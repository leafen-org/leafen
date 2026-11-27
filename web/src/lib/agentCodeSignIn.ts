import { API_ROOT, throwResponseError } from "@/apiTransport";
import type { Session } from "@/types";
import { keepSignInFor, sessionFromStored } from "./authSessionStore";
import { desktopBackgroundAvailable } from "./desktopBackground";
import { type AgentFirstRunRequest, desktopFirstRun } from "./desktopFirstRun";
import { packagedCredentials } from "./packagedCredentials";
import { activeProfileId, addServerProfile, profileAt, serverProfileId } from "./profileRegistry";
import { openProfile } from "./profileSwitch";
import { endSignIn } from "./serverAccounts";
import { answeringServer, typedServerAddress } from "./typedServerAddress";

// An agent signs the app in with the one-time code the person got from a
// device signed in there (`leafen setup sign-in --code`, desktop/first_run.go;
// user decision 2026-11-13: signing in and adding an account go through the
// agent's conversation): on the first screen (FirstInstall), or past it as
// another Leafen beside the ones the app has, as the account menu's "Add an
// account" does (followAgentSignIns). Either way the window answers the
// agent's command how it went.

/** The refusal of a sign-in the app couldn't keep in its secret storage. */
export const AGENT_SIGN_IN_NOT_KEPT = "keep";

// How often a window past the first screen looks for an agent's sign-in.
const LOOK_EVERY = 2000;

async function joinWithAgentCode(origin: string, code: string, signOutLeastUsed: boolean): Promise<Session> {
  const response = await fetch(new URL(`${API_ROOT}/auth/device`, origin), {
    method: "POST",
    cache: "no-store",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(signOutLeastUsed ? { code, replaceOldestSession: true } : { code }),
  });
  const body: unknown = await response.json().catch(() => null);
  if (!response.ok) throwResponseError(body as Parameters<typeof throwResponseError>[0], response.status);
  const session = sessionFromStored(JSON.stringify(body));
  if (!session) throw notKept();
  return session;
}

function notKept(): Error {
  return Object.assign(new Error("The login couldn't be kept."), { code: AGENT_SIGN_IN_NOT_KEPT });
}

/**
 * Keeps a sign-in as its server profile's active one, where the app keeps
 * that profile's (packagedClient.ts signs the profile in with it as it
 * opens), with the accounts already signed in there beside it — through the
 * one way every change of them goes (authSessionStore.ts keepSignInFor); an
 * earlier sign-in of the same account ends, as adding an account does,
 * before the page starts over (which would cut its request off).
 */
async function keepAgentSignIn(origin: string, session: Session): Promise<void> {
  if (!await packagedCredentials()) throw notKept();
  // Under the profile the server is known as, also at the address it moved to.
  const replaced = await keepSignInFor(profileAt(origin)?.id ?? serverProfileId(origin), session).catch(() => { throw notKept(); });
  await Promise.all(replaced.map((earlier) => endSignIn(origin, earlier)));
}

/**
 * Signs the app in to the server at origin with the agent's code and keeps
 * the sign-in for that server's profile. It answers the session, or throws
 * the server's refusal with its code, or one with AGENT_SIGN_IN_NOT_KEPT.
 */
export async function signInWithAgentCode(origin: string, code: string, signOutLeastUsed: boolean): Promise<Session> {
  const session = await joinWithAgentCode(origin, code, signOutLeastUsed);
  await keepAgentSignIn(origin, session);
  return session;
}

/** Why a sign-in failed, as the agent's command reads it. */
export function agentSignInRefusal(caught: unknown): string {
  const code = (caught as { code?: unknown } | null)?.code;
  return typeof code === "string" && code ? code : "LEAFEN_TRANSPORT_UNAVAILABLE";
}

let following = false;

/**
 * Past the first screen, the app's windows look for an agent's sign-in; the
 * one the app hands it to adds that Leafen — another account on the server
 * open here, or a server of its own in the account menu — and opens it.
 */
export function followAgentSignIns(): void {
  if (following || !desktopBackgroundAvailable()) return;
  following = true;
  const look = async () => {
    let request: AgentFirstRunRequest | null;
    try {
      request = await desktopFirstRun.takeAgentSignIn();
    } catch {
      return; // A shell without it: nothing to follow.
    }
    if (request?.signIn && request.code && await addByAgent({ ...request, signIn: request.signIn, code: request.code })) return;
    window.setTimeout(() => void look(), LOOK_EVERY);
  };
  window.setTimeout(() => void look(), LOOK_EVERY);
}

// Adds the Leafen and answers; true once the page starts over on it.
async function addByAgent(request: AgentFirstRunRequest & { signIn: string; code: string }): Promise<boolean> {
  const answer = (account: string, refused: string) =>
    desktopFirstRun.answerAgentSignIn(request.at, account, refused).catch(() => undefined);
  const { origins, path } = typedServerAddress(request.signIn);
  const found = origins.length ? await answeringServer(origins) : { failure: "none" as const };
  if (!("origin" in found)) {
    await answer("", found.failure === "plain" ? "plain" : "address");
    return false;
  }
  const { origin } = found;
  try {
    const session = await signInWithAgentCode(origin, request.code, Boolean(request.signOutLeastUsed));
    // On the server open here it is another account beside the one signed
    // in: the page starts over and reads it from what the app keeps.
    const known = profileAt(origin)?.id ?? serverProfileId(origin);
    const here = activeProfileId() === known;
    if (!here) addServerProfile(origin);
    await answer(session.user.name || session.user.email || session.user.id, "");
    openProfile(known, "", path || (here ? "/" : ""));
    return true;
  } catch (caught) {
    await answer("", agentSignInRefusal(caught));
    return false;
  }
}
