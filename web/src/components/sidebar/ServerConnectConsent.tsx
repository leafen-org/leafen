import { useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { ConfirmCard } from "@/components/ConfirmActionDialog";
import { answerConnectRequest, pendingConnectRequest } from "@/lib/switcherLists";
import type { Session } from "@/types";

function hostOf(origin: string): string {
  try {
    return new URL(origin).host;
  } catch {
    return origin;
  }
}

/**
 * Another server's page asks to list this account's workspaces (the
 * multi-server decision's 2026-10-07 addition): once signed in here, the
 * person allows it — this server's list key goes back to that page — or not.
 */
export function ServerConnectConsent({ session }: { session: Session }) {
  const { t } = useTranslation("sidebar");
  const [request] = useState(pendingConnectRequest);
  const [busy, setBusy] = useState(false);
  if (!request) return null;
  const host = hostOf(request.from);
  const answer = (allow: boolean) => {
    setBusy(true);
    void answerConnectRequest(session, request, allow);
  };
  // Only an answer closes it: the other server's page waits for one.
  return createPortal((
    <ConfirmCard required busy={busy} message={t("connect.title", { host })}
      detail={t("connect.body", { host, account: session.user.email || session.user.name })}
      confirmLabel={t("connect.allow")} cancelLabel={t("connect.deny")}
      markers={{ dialog: { "data-server-connect": request.from }, confirm: { "data-server-connect-allow": "" } }}
      onAnswer={(choice) => answer(choice !== null)} />
  ), document.body);
}
