// The app's first start (desktop/first_run.go): whether to sign in to a
// Leafen used already or start new on this computer, and what an agent going
// through the first run from a terminal chose for the person (`leafen-desktop
// setup start` or `sign-in`, with the one-time code it signs in with). The
// steps after it are the server's (lib/firstRunSteps.ts).

/** What an agent chose from a terminal while the first screen shows. */
export type AgentFirstRunRequest = { at: number; startHere?: boolean; signIn?: string; code?: string; signOutLeastUsed?: boolean };

export type FirstRunState = {
  pending: boolean;
  phone: boolean;
  done: boolean;
  choice: "" | "local" | "server";
  // What the computer's organization set through device management.
  managed?: { managed: boolean; serverUrl?: string };
  // Whether this computer serves a Leafen of its own (an older shell says nothing).
  ownServer?: boolean;
  request?: AgentFirstRunRequest;
};

async function call<T>(method: string, ...args: unknown[]): Promise<T> {
  const { Call } = await import("@wailsio/runtime");
  return await Call.ByName(`main.FirstRun.${method}`, ...args) as T;
}

export const desktopFirstRun = {
  state: () => call<FirstRunState>("State"),
  startHere: () => call<void>("StartHere"),
  connectedToServer: () => call<void>("ConnectedToServer"),
  // How signing in with an agent's code went, for its command to say:
  // the account it signed in as, or why it was refused.
  answerAgentSignIn: (at: number, account: string, refused: string) => call<void>("AnswerAgentSignIn", at, account, refused),
  // Past the first screen, an agent's sign-in with a code, to this window alone.
  takeAgentSignIn: () => call<AgentFirstRunRequest | null>("TakeAgentSignIn"),
};

/**
 * Whether another server's sign-in offers the way back to this device: not
 * where the computer's organization named the server and nothing here
 * serves — its first screen holds that server alone, so the way back would
 * lead to the same sign-in. Unknown, as an older shell or a failed read, it
 * is offered as before.
 */
export async function deviceToReturnTo(): Promise<boolean> {
  const state = await desktopFirstRun.state().catch(() => null);
  return !state?.managed?.serverUrl || state.ownServer !== false;
}
