import { authErrorCodes, type AuthSession } from "@/authWire.generated";
import { onServerAnswersAgain } from "@/lib/serverAnswers";
import type { Session } from "@/types";

/** Unsynced changes found at sign-out; a null count means they could not be read. */
export type PendingSignOut = { count: number } | { count: null };

/** The open discard question: keep waiting, or discard and sign out. */
export type PendingSignOutPrompt = PendingSignOut & {
  onCancel(): void;
  onDiscard(): void;
};

export interface ServerSessionTransport {
  listAuthSessions(session: Session): Promise<AuthSession[]>;
  revokeAuthSession(session: Session, sessionId: string): Promise<void>;
}

function errorField(error: unknown, key: "code" | "status"): unknown {
  if (!error || typeof error !== "object") return undefined;
  try {
    return Reflect.get(error, key);
  } catch {
    return undefined;
  }
}

// The server already refuses this token: the session is revoked, expired or
// its account is closed, so nothing is left open on the server.
function sessionAlreadyClosed(error: unknown): boolean {
  return errorField(error, "status") === 401
    && errorField(error, "code") === authErrorCodes.sessionInvalid;
}

function sessionNotFound(error: unknown): boolean {
  return errorField(error, "status") === 404
    && errorField(error, "code") === authErrorCodes.sessionNotFound;
}

// The notice that an account's server session stayed open (useProductSignOut)
// waits to be retried; another account's sign-in here closes it, as its
// retry is for a session this device no longer shows (2026-10-25: the notice
// stayed under the next account's screens for hours).
let openSessionNotice: { message: string; userId: string } | null = null;

/** Keeps that notice against the account it is about. */
export function keepOpenSessionNotice(message: string, userId: string) {
  openSessionNotice = { message, userId };
}

/** The open session was closed after all: nothing is left to tell. */
export function forgetOpenSessionNotice(userId: string) {
  if (openSessionNotice?.userId === userId) openSessionNotice = null;
}

/** Takes the notice's message when it is about an account other than the one now signed in. */
export function takeOtherAccountSessionNotice(userId: string): string | null {
  if (!openSessionNotice || openSessionNotice.userId === userId) return null;
  const { message } = openSessionNotice;
  openSessionNotice = null;
  return message;
}

/**
 * Revokes the server session this browser token belongs to, through the same
 * point revoke the account console uses. A session the server already refuses
 * counts as closed. Any other failure is thrown: the session may still be open.
 */
export async function revokeCurrentServerSession(
  transport: ServerSessionTransport,
  session: Session,
): Promise<void> {
  const revoke = async (sessionId: string): Promise<boolean> => {
    try {
      await transport.revokeAuthSession(session, sessionId);
      return true;
    } catch (error) {
      if (sessionAlreadyClosed(error)) return true;
      if (sessionNotFound(error)) return false;
      throw error;
    }
  };
  if (session.sessionId && await revoke(session.sessionId)) return;
  // A stored session from before the identifier was returned finds its own
  // row through the list's current marker.
  let sessions: AuthSession[];
  try {
    sessions = await transport.listAuthSessions(session);
  } catch (error) {
    if (sessionAlreadyClosed(error)) return;
    throw error;
  }
  const current = sessions.find((item) => item.current);
  if (!current || !await revoke(current.id)) {
    throw new Error("The current server session could not be revoked.");
  }
}

export interface ProductSignOutSteps {
  /** Durable unsynced changes this device holds for the account. */
  readPendingCount(): Promise<number>;
  /** Drops those changes; only called after the person chose to discard. */
  discardPending(): Promise<void>;
  /** Server-side work that needs the token before it is revoked. */
  beforeRevoke(): Promise<unknown>;
  revokeServerSession(): Promise<void>;
  /** Forgets the session on this device and leaves the private route. */
  endLocalSession(): Promise<void> | void;
  onPending(pending: PendingSignOut): void;
  onDiscardFailed(error: unknown): void;
  /** The server session stayed open; retry revokes it again. */
  onServerSessionOpen(retry: () => Promise<void>): void;
  /** A session that stayed open was closed by a later try. */
  onServerSessionClosed?(): void;
}

/**
 * Sign-out asks before discarding durable unsynced changes, starts revoking
 * the server session while this device still holds the token, and then
 * forgets the session locally even when the server cannot be reached, so a
 * shared device is never left signed in. A server session that stays open is
 * reported with a retry.
 */
export function createProductSignOut(steps: ProductSignOutSteps) {
  let settling = false;
  // A session left open is tried again by itself, when the server answers
  // and on a widening wait (2026-11-05: signed out offline, it stayed open
  // after the server was back until the person pressed retry).
  let retryTimer: ReturnType<typeof setTimeout> | undefined;
  let retryIn = 5_000;
  let unlisten: (() => void) | null = null;
  const stopRetrying = () => {
    if (retryTimer) clearTimeout(retryTimer);
    retryTimer = undefined;
    unlisten?.();
    unlisten = null;
    retryIn = 5_000;
  };
  const keepRetrying = () => {
    if (retryTimer) clearTimeout(retryTimer);
    retryTimer = setTimeout(() => void retryRevoke(), retryIn);
    retryIn = Math.min(60_000, retryIn * 2);
    unlisten ??= onServerAnswersAgain(() => void retryRevoke());
  };
  const retryRevoke = async (): Promise<void> => {
    if (retryTimer) clearTimeout(retryTimer);
    retryTimer = undefined;
    try {
      await steps.revokeServerSession();
      stopRetrying();
      steps.onServerSessionClosed?.();
    } catch {
      steps.onServerSessionOpen(retryRevoke);
      keepRetrying();
    }
  };

  const signOut = async (discard: boolean) => {
    const server = (async () => {
      await steps.beforeRevoke().catch(() => undefined);
      await steps.revokeServerSession();
    })();
    void server.catch(() => {
      steps.onServerSessionOpen(retryRevoke);
      keepRetrying();
    });
    if (discard) {
      try {
        await steps.discardPending();
      } catch (error) {
        steps.onDiscardFailed(error);
      }
    }
    await steps.endLocalSession();
  };

  return {
    /**
     * Signs out at once when nothing is waiting; otherwise asks first. With
     * ask, it asks even then (an account with no way back), once, with
     * whatever is waiting.
     */
    async request({ ask = false }: { ask?: boolean } = {}): Promise<void> {
      if (settling) return;
      settling = true;
      try {
        let count: number;
        try {
          count = await steps.readPendingCount();
        } catch {
          // Unknown is never treated as empty.
          steps.onPending({ count: null });
          return;
        }
        if (count > 0 || ask) {
          steps.onPending({ count });
          return;
        }
        await signOut(false);
      } finally {
        settling = false;
      }
    },
    /** The person chose to discard the listed changes and sign out. */
    async discardAndSignOut(): Promise<void> {
      if (settling) return;
      settling = true;
      try {
        await signOut(true);
      } finally {
        settling = false;
      }
    },
  };
}
