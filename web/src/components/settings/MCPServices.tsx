import { Fragment, useCallback, useEffect, useRef, useState } from "react";
import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import { LeafenApiError } from "@/apiTransport";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import { useRowNotice } from "@/lib/useNoticeFor";
import { checkMCPService, listMCPServices, removeMCPService, setMCPServiceReturn, type MCPService } from "@/mcpServicesApi";
import type { Session } from "@/types";
import table from "./DeveloperSettings.module.css";
import styles from "./SettingsItemRows.module.css";
import { openServiceSignInWindow, signInServiceIn } from "./mcpServiceSignIn";
import { RowNotice } from "./SettingsRowNotice";

// The person's own outside services in the settings' "연결": each connected
// once to Leafen by its remote MCP address — as Notion's "사용자 지정 MCP
// 연결" (seen 2026-10-12) — for every agent connected to Leafen to use.

const ERROR_KEYS: Record<string, string> = {
  LEAFEN_MCP_SERVICE_INVALID: "mcpService.errorInvalid",
  LEAFEN_MCP_SERVICE_UNSAFE_ADDRESS: "mcpService.errorUnsafeAddress",
  LEAFEN_MCP_SERVICE_TOKEN_NEEDED: "mcpService.errorTokenNeeded",
  LEAFEN_MCP_SERVICE_CLIENT_NEEDED: "mcpService.errorClientNeeded",
  LEAFEN_MCP_SERVICE_ADDRESS_UNREACHABLE: "mcpService.unreachable",
  LEAFEN_MCP_SERVICE_REGISTRATION_REFUSED: "mcpService.registrationRefused",
  LEAFEN_MCP_SERVICE_SIGN_IN_UNAVAILABLE: "mcpService.signInUnavailable",
  LEAFEN_MCP_SERVICE_TOKEN_REFUSED: "mcpService.errorTokenRefused",
  LEAFEN_MCP_SERVICE_UNSAFE_SIGN_IN: "mcpService.errorUnsafeSignIn",
  LEAFEN_MCP_SERVICE_FAILED: "mcpService.errorFailed",
  LEAFEN_MCP_SERVICE_TOO_MANY_TOOLS: "mcpService.errorTooManyTools",
  LEAFEN_MCP_SERVICE_POLICY_BLOCKED: "mcpService.errorPolicyBlocked",
};

// Refusals after which the person makes a client on the service for Leafen
// (the add dialog's "고급 설정"), told why before signing in — or, for a
// registration the service refused, once it was tried.
export const CLIENT_CODES = new Set([
  "LEAFEN_MCP_SERVICE_CLIENT_NEEDED", "LEAFEN_MCP_SERVICE_ADDRESS_UNREACHABLE", "LEAFEN_MCP_SERVICE_REGISTRATION_REFUSED",
]);

export function serviceError(t: TFunction, failure: unknown, fallback: string) {
  const key = failure instanceof LeafenApiError ? ERROR_KEYS[failure.code] : undefined;
  return key ? t(key) : settingsErrorMessage(failure, fallback);
}

function host(address: string) {
  try {
    return new URL(address).host;
  } catch {
    return address;
  }
}

// The address as the row shows it: two services on one host differ by path.
function shortAddress(address: string) {
  try {
    const parsed = new URL(address);
    return parsed.host + parsed.pathname.replace(/\/$/u, "");
  } catch {
    return address;
  }
}

export const serviceName = (service: MCPService) => service.name || host(service.url);

/**
 * The person's services, a window at a time (the latest connected first),
 * read again from the start after each change.
 */
export function useMCPServices(session: Session) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [services, setServices] = useState<MCPService[] | null>(null);
  const [nextCursor, setNextCursor] = useState("");
  const [error, setError] = useState("");
  const reload = useCallback(async (signal?: AbortSignal) => {
    try {
      const read = await listMCPServices(session, undefined, signal);
      setServices(read.services);
      setNextCursor(read.nextCursor ?? "");
      setError("");
      return read.services;
    } catch (failure) {
      if (!signal?.aborted) setError(settingsErrorMessage(failure, t("mcpService.loadFailed")));
      return null;
    }
  }, [session, t]);
  const loadMore = useCallback(async () => {
    if (!nextCursor) return;
    try {
      const read = await listMCPServices(session, nextCursor);
      setServices((shown) => [...(shown ?? []), ...read.services]);
      setNextCursor(read.nextCursor ?? "");
      setError("");
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("mcpService.loadFailed")));
    }
  }, [nextCursor, session, t]);
  useEffect(() => {
    const controller = new AbortController();
    void reload(controller.signal);
    return () => controller.abort();
  }, [reload]);
  return { services, nextCursor, error, reload, loadMore };
}

/** A row for each service: what it is, how it stands, and what can be done about it. */
export function MCPServiceRows({ session, services, onChanged, onReconnect, onManage }: {
  session: Session;
  services: MCPService[];
  onChanged: () => Promise<unknown>;
  onReconnect: (service: MCPService) => void;
  /** Notion's "Manage": the service's tools and how each runs. */
  onManage: (service: MCPService) => void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [busy, setBusy] = useState("");
  // A failure sits under its service's row and goes once that row reads
  // otherwise — signed in, connected anew, removed — or the next action starts.
  const [noticeFor, setNotice] = useRowNotice<string>((id) => {
    const service = services.find((item) => item.id === id);
    return service && [service.status, service.auth, service.url, service.toolCount, service.connectedAt,
      service.earlierCallback, service.callbackAdded];
  });

  // The failure shows as the service answers, and is held again to the row
  // as the list reads it after the attempt — unless another action started.
  const attempt = useRef(0);
  async function act(service: MCPService, run: () => Promise<unknown>, fallback: string) {
    const mine = ++attempt.current;
    setBusy(service.id);
    setNotice(service.id, null);
    let failed = "";
    try {
      await run();
    } catch (failure) {
      failed = serviceError(t, failure, fallback);
      setNotice(service.id, failed);
    } finally {
      setBusy("");
    }
    try {
      await onChanged();
    } finally {
      if (failed && attempt.current === mine) setNotice(service.id, failed);
    }
  }

  function signIn(service: MCPService) {
    if (service.auth !== "oauth") return onReconnect(service);
    const popup = openServiceSignInWindow();
    // A blocked window opens from the dialog's own button instead.
    if (!popup) return onReconnect(service);
    // A sign-in that finishes after the person left its window still shows;
    // one that needs a client of the person's own opens the dialog, which
    // says why and takes it.
    void act(service, () => signInServiceIn(popup, session, service.id, () => void onChanged()).catch((failure: unknown) => {
      if (!(failure instanceof LeafenApiError && CLIENT_CODES.has(failure.code))) throw failure;
      onReconnect(service);
    }), t("mcpService.signInFailed"));
  }

  // Asked in the common confirmation, as every settings row's disconnect is;
  // focus goes back to the row's button, or is let go when the row is gone.
  async function remove(service: MCPService) {
    if (!await confirmAction({ message: t("connectionDisconnectConfirm", { name: serviceName(service) }),
      detail: t("mcpService.removeConfirmDetail"), confirmLabel: t("connectionDisconnect"), danger: true })) return;
    await act(service, () => removeMCPService(session, service.id), t("mcpService.removeFailed"));
  }

  const status = (service: MCPService) => service.status === "connected"
    ? t("connectionApps.tools", { count: service.toolCount })
    : t(STATUS_KEYS[service.status]);
  return (
    <>
      {services.map((service) => (
        <Fragment key={service.id}>
          <div className={styles.row} data-mcp-service={service.id} data-status={service.status}>
            <span className={styles.icon} aria-hidden="true">{serviceName(service).slice(0, 1).toUpperCase()}</span>
            <span className={styles.text}>
              <strong>{serviceName(service)}</strong>
              <span>{[t("mcpService.kind"), shortAddress(service.url), status(service)].join(" · ")}</span>
              {service.earlierCallback ? (
                <span className={styles.returnMoved} data-mcp-service-return={service.callbackAdded ? "added" : "earlier"}>
                  <span>{t(service.callbackAdded ? "mcpService.returnAdded" : "mcpService.returnMoved",
                    { earlier: service.earlierCallback, callback: service.callback ?? "" })}</span>
                  <button type="button" className={table.secondary} disabled={busy === service.id}
                    onClick={() => void act(service, () => setMCPServiceReturn(session, service.id, !service.callbackAdded), t("mcpService.signInFailed"))}>
                    {t(service.callbackAdded ? "mcpService.returnUndo" : "mcpService.returnAdd")}
                  </button>
                </span>
              ) : null}
            </span>
            <span className={styles.actions}>
              {service.status === "unreachable" ? (
                <button type="button" className={table.secondary} disabled={busy === service.id}
                  onClick={() => void act(service, () => checkMCPService(session, service.id), t("mcpService.checkFailed"))}>
                  {t("mcpService.check")}
                </button>
              ) : service.status !== "connected" ? (
                <button type="button" className={table.secondary} disabled={busy === service.id} onClick={() => signIn(service)}>
                  {service.auth !== "oauth" ? t("mcpService.reconnect")
                    : service.status === "signing_in" ? t("mcpService.signIn") : t("mcpService.signInAgain")}
                </button>
              ) : null}
              {service.status === "connected" ? (
                <button type="button" className={table.secondary} aria-label={t("mcpService.manageNamed", { name: serviceName(service) })}
                  data-mcp-service-manage onClick={() => onManage(service)}>
                  {t("mcpService.manage")}
                </button>
              ) : null}
              <button type="button" className={table.secondary} aria-label={t("connectionDisconnectNamed", { name: serviceName(service) })}
                disabled={busy === service.id} data-mcp-service-remove onClick={() => void remove(service)}>
                {t("connectionDisconnect")}
              </button>
            </span>
          </div>
          <RowNotice text={noticeFor(service.id)} data-mcp-service-error />
        </Fragment>
      ))}
    </>
  );
}

const STATUS_KEYS: Record<Exclude<MCPService["status"], "connected">, string> = {
  signing_in: "mcpService.statusSigningIn",
  sign_in_again: "mcpService.statusSignInAgain",
  unreachable: "mcpService.statusUnreachable",
};
