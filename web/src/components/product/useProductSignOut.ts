import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { clearSessionIfCurrent, loadSession, sameSessionSignIn } from "@/lib/authSessionStore";
import { turnDesktopPushOff } from "@/lib/desktopPush";
import { forgetDevicePageCopies, readUnsyncedDeviceChangeCount } from "@/lib/deviceSignOutData";
import { localFirstProductCache as recordCache } from "@/lib/localFirstProductCache";
import type { NativePageMutationBrowser } from "@/lib/nativePageMutationBrowser";
import {
  createProductSignOut,
  forgetOpenSessionNotice,
  keepOpenSessionNotice,
  revokeCurrentServerSession,
  type PendingSignOut,
  type PendingSignOutPrompt,
} from "@/lib/productSignOut";
import { useProductToast } from "@/lib/productToast";
import { isPackagedClient } from "@/lib/packagedClient";
import { leaveServerForLocal } from "@/lib/profileSwitch";
import { leaveOpenPages } from "@/lib/pageLeft";
import { markSignOutHere, SIGN_OUT_HERE_EVENT, takeRemoteSignOutRequest, takeSignOutHere } from "@/lib/remoteSignOut";
import { forgetServerAccount, serverAccountPath } from "@/lib/serverAccounts";
import { forgetReauthReturnPathForSignOut } from "@/lib/reauthReturnPath";
import { markSignedOutByHand } from "@/lib/signOutMarks";
import { markOwnServerSignedOut } from "@/lib/ownServerSignOut";
import { replaceRoute } from "@/lib/router";
import { returnToDeviceLanguage } from "@/i18n";
import { logProductError } from "@/lib/safeErrorLog";
import type { Session } from "@/types";

// Sign-out from the workspace menu: it asks before dropping unsynced changes,
// revokes this server session, and reports a session the server kept open.
export function useProductSignOut<TResult>({
  flushPending,
  mutationOutbox,
  onSession,
  resetBootstrap,
  session,
}: {
  flushPending(): Promise<void>;
  mutationOutbox: NativePageMutationBrowser<TResult>;
  onSession(session: Session | null): void;
  resetBootstrap(): void;
  session: Session;
}) {
  const { t } = useTranslation("sidebar");
  const { notify, dismissMessage } = useProductToast();
  const [pending, setPending] = useState<PendingSignOut | null>(null);
  // What follows a sign-out that ends, such as going back to this device.
  const afterSignOut = useRef<(() => void) | null>(null);
  // Every account here is signing out, one after another.
  const signingOutAll = useRef(false);
  const local = useRef({ onSession, resetBootstrap });
  local.current = { onSession, resetBootstrap };
  const currentSession = useRef(session);
  currentSession.current = session;
  const flow = useMemo(() => createProductSignOut({
    // Queued changes, page text the server does not have yet, and page
    // changes from before the queue.
    readPendingCount: async () => (
      await mutationOutbox.readPendingCount() + await readUnsyncedDeviceChangeCount(session)
    ),
    discardPending: () => mutationOutbox.discardAll(),
    // Push delivery is forgotten with the token that registered it, and the
    // pages open are left while it still answers (pageLeft.ts).
    beforeRevoke: () => Promise.all([turnDesktopPushOff(session), leaveOpenPages(session)]),
    revokeServerSession: () => revokeCurrentServerSession(api, session),
    async endLocalSession() {
      // Asked for, it keeps no place: the next sign-in opens the start page,
      // and the person's other tabs that held this sign-in learn it was asked for.
      forgetReauthReturnPathForSignOut();
      markSignedOutByHand(session);
      // Nor does the app's own window sign its owner back in by itself (ownServerSignOut.ts).
      markOwnServerSignedOut();
      await clearSessionIfCurrent(session);
      const replacement = loadSession();
      const nextAccount = replacement && replacement.user.id !== session.user.id ? replacement : null;
      if (!replacement || nextAccount) {
        forgetServerAccount(session.user.id);
        void recordCache.clear(session.user.id).catch(() => undefined);
        // A shared device keeps no copy of the signed-out account's pages.
        void forgetDevicePageCopies(session.user.id).catch((error) => {
          logProductError("warn", "outbox.persistence", error, { causeStage: "outbox.signOutForget" });
        });
      }
      const after = afterSignOut.current;
      afterSignOut.current = null;
      if (!sameSessionSignIn(currentSession.current, session)) return;
      // Another account signed in here goes on where it was, or signs out in its turn.
      if (nextAccount) {
        if (signingOutAll.current) markSignOutHere("all");
        replaceRoute(signingOutAll.current ? "/" : serverAccountPath(nextAccount.user.id));
      }
      local.current.onSession(replacement);
      if (nextAccount && !signingOutAll.current) after?.();
      if (replacement) return;
      local.current.resetBootstrap();
      void returnToDeviceLanguage();
      replaceRoute("/");
      after?.();
    },
    onPending: setPending,
    onDiscardFailed(error) {
      logProductError("warn", "outbox.persistence", error, { causeStage: "outbox.signOutDiscard" });
    },
    onServerSessionOpen(retry) {
      const message = t("sidebar:remoteSignOutUnresolved");
      keepOpenSessionNotice(message, session.user.id);
      notify(message, "error", {
        label: t("sidebar:retryRemoteSignOut"),
        autoDismissMs: null,
        onClick: retry,
      });
    },
    onServerSessionClosed() {
      forgetOpenSessionNotice(session.user.id);
      dismissMessage(t("sidebar:remoteSignOutUnresolved"));
    },
  }), [dismissMessage, mutationOutbox, notify, session, t]);

  // Signing out asked from elsewhere (remoteSignOut.ts): another server's
  // page asking this one, which then goes back there, or this page's own
  // "모든 계정에서 로그아웃" once its other accounts are done. Each asks once.
  useEffect(() => {
    void takeRemoteSignOutRequest().then((request) => {
      if (!request) return;
      if (!request.real) {
        window.location.assign(request.back);
        return;
      }
      afterSignOut.current = () => window.location.assign(request.back);
      void flow.request();
    });
    const here = () => {
      const asked = takeSignOutHere();
      if (!asked) return;
      signingOutAll.current = asked === "all";
      // The app goes back to this device once none is left here.
      if (asked === "all" && isPackagedClient()) afterSignOut.current = leaveServerForLocal;
      void flow.request();
    };
    here();
    window.addEventListener(SIGN_OUT_HERE_EVENT, here);
    return () => window.removeEventListener(SIGN_OUT_HERE_EVENT, here);
  }, [flow]);

  const pendingSignOut: PendingSignOutPrompt | null = pending && {
    ...pending,
    onCancel() {
      // Keeping the changes means waiting for them: nudge the sync now.
      afterSignOut.current = null;
      setPending(null);
      void flushPending().catch(() => undefined);
    },
    onDiscard() {
      setPending(null);
      void flow.discardAndSignOut();
    },
  };
  return {
    pendingSignOut,
    requestSignOut(after?: () => void, ask?: boolean) {
      afterSignOut.current = after ?? null;
      void flow.request({ ask });
    },
  };
}
