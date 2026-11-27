import { api } from "@/api";
import type { AuthMFAState, AuthSession } from "@/authWire.generated";
import type { Session } from "@/types";

export type AccountSecuritySnapshot = {
  mfa: AuthMFAState;
  sessions: AuthSession[];
  /** Whether the person set the account's password (a backup sign-in). */
  passwordSet: boolean;
};

export type AccountSecurityTransport = Pick<
  typeof api,
  "listAuthSessions" | "readAccountMFA" | "readAccountState"
>;

function snapshotKey(session: Session): string {
  return `${session.user.id}\u0000${session.sessionId ?? session.token}`;
}

function createAccountSecurityService(
  transport: AccountSecurityTransport = api,
) {
  const activeSnapshots = new Map<string, Promise<AccountSecuritySnapshot>>();
  return {
    read(session: Session): Promise<AccountSecuritySnapshot> {
      const key = snapshotKey(session);
      const active = activeSnapshots.get(key);
      if (active) return active;

      const pending = Promise.all([
        transport.readAccountMFA(session),
        transport.listAuthSessions(session),
        transport.readAccountState(session),
      ]).then(([mfa, sessions, state]) => ({ mfa, sessions, passwordSet: state.passwordSet }));
      activeSnapshots.set(key, pending);
      void pending.finally(() => {
        if (activeSnapshots.get(key) === pending) activeSnapshots.delete(key);
      }).catch(() => undefined);
      return pending;
    },
  };
}

export const accountSecurityService = createAccountSecurityService();
