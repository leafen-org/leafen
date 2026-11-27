import { request } from "@/apiTransport";
import type { Session } from "@/types";
import { wakeTabsBehind } from "./desktopTabs";
import { desktopFirstRun } from "./desktopFirstRun";
import { followAcrossTabs, hearTabs, tellTabs } from "./followAcrossTabs";
import { isPackagedClient } from "./packagedClient";

// The first run's steps after the name and the language, one flow in the app
// and in a browser (the one-program decision of 2026-10-27, 5): the outside
// address, then how outside agents connect (Leafen runs none of its own
// since 2026-10-29); the workspace then opens on its welcome page, as
// Notion's, which asks for no other device at sign-up either (2026-10-29:
// another device and a backup sign-in are set in Settings). They are the
// server's, for whoever set it up (internal/app/first_run_state.go, user
// decision 2026-11-09), so an agent's `leafen setup` and the screens go
// through the same ones: which are left, which were left for later and which
// are done are read and written there, through here alone. A phone's own
// Leafen, a trial, has no address to give and asks none. Settings brings back
// what was left for later.

export type FirstRunStep = "address" | "agents";

export const FIRST_RUN_STEPS: readonly FirstRunStep[] = ["address", "agents"];

/** Settings brings the steps left for later back: the window shows them. */
export const FIRST_RUN_REOPEN_EVENT = "leafen:first-run-reopen";

/** A screen an agent's `leafen setup` asked the setup person's window to open. */
export type SetupOpen = { what: string; at: number };

export type FirstRunState = {
  steps: FirstRunStep[];
  skipped: FirstRunStep[];
  done: boolean;
  // Whether this is the setup person's, whether an agent goes through the
  // first run by commands now, and the screen its last command asked to open.
  owner?: boolean;
  follow?: boolean;
  open?: SetupOpen;
};

const NONE: FirstRunState = { steps: [], skipped: [], done: true };

// The steps as last read, written or heard from another tab, and when, so
// the workspace's first screen knows them at once: read after it opened, the
// welcome page showed for a moment before the address step (2026-11-06).
// They are read ahead while the account's own steps are up
// (AuthenticatedAccountGates). The parts of the app that open with a start —
// the account's steps, the workspace, the steps' own window, the setup
// follow — each asked for them, five reads a start (2026-11-13, NAS-class
// server): an answer this new, or a read still out, serves every one of them,
// as new as following the steps keeps them (FirstRunSteps asks every two
// seconds); following them reads anew.
let known: { userId: string; state: FirstRunState; at: number } | null = null;
let reading: { userId: string; token: string; answer: Promise<FirstRunState> } | null = null;
let writes = 0;
const ANSWER_SERVES_MS = 2000;

// Every answer the server gives about them, read or written, is told to the
// person's other tabs, which follow it without asking (followAcrossTabs).
const topic = (userId: string) => `first-run:${userId}`;

const only = (values: unknown): FirstRunStep[] =>
  (Array.isArray(values) ? values : []).filter((step): step is FirstRunStep => FIRST_RUN_STEPS.includes(step));

async function phoneApp(): Promise<boolean> {
  if (!isPackagedClient()) return false;
  return (await desktopFirstRun.state().catch(() => null))?.phone ?? false;
}

function keep(session: Session, answer: FirstRunState): FirstRunState {
  const steps = { steps: only(answer.steps), skipped: only(answer.skipped), done: answer.done === true,
    owner: answer.owner === true, follow: answer.follow === true, open: answer.open };
  known = { userId: session.user.id, state: steps, at: Date.now() };
  tellTabs(topic(session.user.id), steps);
  return steps;
}

// A written answer is newer than any read still out: what asks next reads
// anew. The tabs behind follow it awake (wakeTabsBehind).
function wrote(session: Session, answer: FirstRunState) {
  writes += 1;
  wakeTabsBehind();
  reading = null;
  return keep(session, answer);
}

function readSteps(session: Session, anew = false): Promise<FirstRunState> {
  const userId = session.user.id;
  if (!anew && known?.userId === userId && Date.now() - known.at < ANSWER_SERVES_MS) return Promise.resolve(known.state);
  if (reading?.userId === userId && reading.token === session.token) return reading.answer;
  const since = writes;
  const answer = (async () => {
    if (await phoneApp()) return keep(session, NONE);
    const read = await request<FirstRunState>("/first-run", { method: "GET" }, session);
    // Written meanwhile, the steps are as the write answered.
    return writes === since || known?.userId !== userId ? keep(session, read) : known.state;
  })();
  reading = { userId, token: session.token, answer };
  void answer.catch(() => undefined).finally(() => { if (reading?.answer === answer) reading = null; });
  return answer;
}

/** The steps still to show where they're known already, else null (firstRunStepsLeft reads them). */
export function firstRunStepsLeftNow(userId: string): FirstRunStep[] | null {
  return known?.userId === userId ? known.state.steps : null;
}

/** The steps still to show, in order: none once over, or where none began. */
export async function firstRunStepsLeft(session: Session): Promise<FirstRunStep[]> {
  return (await readSteps(session)).steps;
}

/** The steps as the server has them, with whether the window follows an agent's commands now. */
export async function firstRunSetupState(session: Session): Promise<FirstRunState> {
  return readSteps(session);
}

/** Hears every answer about the steps, this tab's (here) and the person's other tabs', until the returned stop. */
export function hearFirstRun(userId: string, heard: (state: FirstRunState, here: boolean) => void): () => void {
  return hearTabs(topic(userId), (value, here) => {
    const state = value as FirstRunState;
    if (!here) known = { userId, state, at: Date.now() };
    heard(state, here);
  });
}

/**
 * Asks for the steps every `every` ms from whichever of the person's tabs is
 * in sight, one for all (followAcrossTabs), with the session as it is then
 * (`sessionNow`: its token may be renewed meanwhile); `heard` hears each
 * answer, as hearFirstRun. Until the returned stop.
 */
export function followFirstRun(session: Session, every: number,
  heard: (state: FirstRunState, here: boolean) => void = () => undefined,
  sessionNow: () => Session | null = () => session): () => void {
  const userId = session.user.id;
  const stopHearing = hearFirstRun(userId, heard);
  const stopAsking = followAcrossTabs(topic(userId), every, async () => {
    const asker = sessionNow();
    if (asker) await readSteps(asker, true);
  }, () => undefined);
  return () => {
    stopAsking();
    stopHearing();
  };
}

/** Reads the steps ahead of the workspace (AuthenticatedAccountGates). */
export function readFirstRunStepsAhead(session: Session): void {
  void readSteps(session).catch(() => undefined);
}

/** The steps left for later, which Settings can bring back. */
export async function firstRunStepsSkipped(session: Session): Promise<FirstRunStep[]> {
  return (await readSteps(session)).skipped;
}

/** A step done, or left for later. */
export async function passFirstRunStep(session: Session, step: FirstRunStep, done: boolean) {
  wrote(session, await request<FirstRunState>("/first-run/steps", { method: "POST", body: JSON.stringify({ step, done }) }, session));
}

/** The steps are over: they don't show again until Settings brings back what was left. */
export async function finishFirstRun(session: Session) {
  wrote(session, await request<FirstRunState>("/first-run/finish", { method: "POST" }, session));
}

/** Brings back the steps left for later, which the window then shows. */
export async function reopenFirstRun(session: Session) {
  wrote(session, await request<FirstRunState>("/first-run/reopen", { method: "POST" }, session));
  window.dispatchEvent(new Event(FIRST_RUN_REOPEN_EVENT));
}
