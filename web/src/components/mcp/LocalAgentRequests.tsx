import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { request } from "@/apiTransport";
import { followAcrossTabs, tellTabs } from "@/lib/followAcrossTabs";
import { isPackagedClient } from "@/lib/packagedClient";
import { activeProfileId, LOCAL_PROFILE_ID } from "@/lib/profileRegistry";
import { useAfterTyping } from "@/lib/typingHold";
import { ConfirmCard } from "@/components/ConfirmActionDialog";
import type { Session } from "@/types";

type AgentRequest = { request: string; client: string; created: number; here?: boolean };

// How often the window asks after web agents waiting at the tunnel's address.
const ASK_EVERY_MS = 3000;

/**
 * A packaged app's own server takes web agents through the tunnel but has no
 * sign-in outside (decided 2026-10-13): their requests to connect wait for the
 * owner here, and opening one shows the usual consent screen. So do those of
 * agents on this computer, whose browser has no sign-in here (2026-11-09).
 * One of the window's tabs in sight asks for all of them, which hear its
 * answer; a tab behind asks nothing (followAcrossTabs).
 */
export function LocalAgentRequests({ session }: { session: Session }) {
  const { t } = useTranslation("authGate");
  const [requests, setRequests] = useState<AgentRequest[]>([]);
  const [later, setLater] = useState<string[]>([]);
  // The window on the app's own server.
  const local = isPackagedClient() && activeProfileId() === LOCAL_PROFILE_ID;

  useEffect(() => {
    if (!local) return undefined;
    const topic = `local-agent-requests:${session.user.id}`;
    return followAcrossTabs(topic, ASK_EVERY_MS,
      () => request<{ requests: AgentRequest[] }>("/local-owner/agent-requests", { method: "GET" }, session)
        .then((answer) => tellTabs(topic, answer.requests)),
      (value) => { if (Array.isArray(value)) setRequests(value as AgentRequest[]); },
      { now: true });
  }, [local, session]);
  const waiting = requests.find((item) => !later.includes(item.request)) ?? null;

  const clear = useAfterTyping(waiting !== null);
  if (!waiting || !clear || new URLSearchParams(window.location.search).get("mcpAuthorization") === waiting.request) return null;
  // Only an answer closes it, "나중에" among them.
  return createPortal((
    <ConfirmCard required message={t(waiting.here ? "localAgent.titleHere" : "localAgent.title", { client: waiting.client })}
      detail={t(waiting.here ? "localAgent.bodyHere" : "localAgent.body")}
      confirmLabel={t("localAgent.open")} cancelLabel={t("localAgent.later")}
      markers={{ dialog: { "data-local-agent-request": "" }, confirm: { "data-local-agent-request-open": "" } }}
      onAnswer={(choice) => {
        if (choice === null) setLater((current) => [...current, waiting.request]);
        else window.location.assign(`/?mcpAuthorization=${encodeURIComponent(waiting.request)}`);
      }} />
  ), document.body);
}
