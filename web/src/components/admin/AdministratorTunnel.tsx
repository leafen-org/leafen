import { useCallback, useEffect, useRef, useState } from "react";
import { useAttemptNotice } from "@/lib/useNoticeFor";
import { useTranslation } from "react-i18next";
import { request } from "@/apiTransport";
import { refreshRuntimeConfig } from "@/lib/runtimeConfigService";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import { tunnelChangeMessage } from "@/components/remoteAccess/remoteAddressPrompt";
import { tunnelFailureLine, tunnelHeld } from "@/components/remoteAccess/tunnelFailureLine";
import { TunnelFailureDetails } from "@/components/remoteAccess/TunnelFailureDetails";
import type { Session } from "@/types";
import styles from "./AdministratorSettingsPage.module.css";
import dataStyles from "./AdministratorDataManagement.module.css";
import { SettingField, SettingRow } from "./AdministratorSettingRows";
import { AppSelect } from "@/components/AppSelect";
import { SettingsSectionTitle } from "@/components/settings/SettingsSectionTitle";
import { displayDateTime } from "@/lib/displayDate";
import { useOutsideOpener } from "@/lib/outsideOpener";
import { useCopyNotice } from "@/lib/copyNotice";
import { ReadFailed } from "../ReadFailed";

type TunnelMode = "off" | "tailscale" | "address" | "quick" | "token";

/** The server's tunnel as the server tells it; the token itself never comes back. */
type TunnelStatus = {
  mode: TunnelMode;
  address: string;
  hasToken: boolean;
  state: "off" | "starting" | "on" | "retrying";
  url?: string;
  failure?: string;
  /** What kind the failure, or the hold, is (internal/app/server_tunnel_failure.go). */
  failureKind?: string;
  since?: number;
  configured?: string;
  configuredInUse?: boolean;
  stage?: string;
  /** The packaged app's own server, which answers only its window. */
  localOnly?: boolean;
  /** A certificate for the Tailscale address the server didn't get, found in the public logs. */
  certificateAlert?: { issuer: string; notBefore: string; serial: string };
  /** When the public certificate logs were last read for the address, and last couldn't be. */
  certificateWatch?: { checkedAt?: number; failedAt?: number };
  /** The tunnels' door a tunnel of the administrator's own names as its service, and whether one still reaches the server's own port. */
  door?: string;
  doorMissed?: boolean;
  /**
   * Tailscale: the public address it gave last, where to sign in or allow
   * the public address while the server waits, and the device name asked
   * for when Tailscale gave another.
   */
  tailscaleAddress?: string;
  tailscaleLogin?: string;
  tailscaleAllow?: string;
  tailscaleRenamed?: string;
  /** The address is open but its name isn't in the public DNS yet: it isn't said on until it is. */
  tailscalePublishing?: boolean;
  /** Your own address: the port the server takes HTTPS on itself, and what it found there. */
  addressTlsPort?: number;
  addressCheck?: { names?: string[]; notAfter?: number; trusted: boolean; problem?: string };
  /** Off: the outside addresses the server was opened at all the same, through a proxy or forward in front of it. */
  reachedFrom?: { origin: string; lastAt: string }[];
  /** Earlier addresses forwarding to the current one, until when. */
  forwards?: {
    from: string; to: string; until: number; kind: "tailscale" | "tls" | "proxy";
    // The last request at the old address and what made such requests (server_address_forwarding.go).
    lastSeen?: number; seen?: ("agents" | "apps" | "links" | "signIn" | "programs" | "keys")[];
    /** The earlier Tailscale device asks to be signed in again: the address doesn't forward now. */
    signedOut?: boolean;
  }[];
};

/**
 * "바깥에서 접속": the administrator lets people and agents outside reach the
 * server with no domain, port forwarding or certificate: through their own
 * Tailscale (a free public address by Funnel, built in — decided
 * 2026-10-19), their own address, or their own Cloudflare service — a quick
 * tunnel to try at once, or a tunnel of their own account. Leafen runs no
 * service of its own. The screen says what each passes through.
 */
// What still comes to an earlier address, as the screen names it.
const forwardingUses = {
  agents: "tunnel.forwardingUseAgents",
  apps: "tunnel.forwardingUseApps",
  links: "tunnel.forwardingUseLinks",
  signIn: "tunnel.forwardingUseSignIn",
  programs: "tunnel.forwardingUsePrograms",
  keys: "tunnel.forwardingUseKeys",
} as const;

export function AdministratorTunnel({ session }: { session: Session }) {
  const { t } = useTranslation("serverConsole");
  const [status, setStatus] = useState<TunnelStatus | null>(null);
  const [mode, setMode] = useState<TunnelMode>("off");
  const [token, setToken] = useState("");
  const [address, setAddress] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useAttemptNotice<string>(mode, token, address);
  // The own address refused when applied: its field is marked with the line (rejected-entry focus).
  const [addressRefused, setAddressRefused] = useState(false);
  const copy = useCopyNotice();
  // Tailscale's sign-in, then its page that allows a public address, opened as each comes after "적용".
  const tailscaleAt = status?.mode === "tailscale" ? status.tailscaleLogin || status.tailscaleAllow || "" : "";
  const outside = useOutsideOpener(tailscaleAt, status?.mode === "tailscale" && status.state === "on" && Boolean(status.url));

  const read = useCallback(async () => {
    try {
      const next = await request<TunnelStatus>("/admin/tunnel", { method: "GET" }, session);
      setStatus(next);
      return next;
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("tunnel.readFailed")));
      return null;
    }
  }, [session, t]);

  // The first read, and again from a failed one: the choices start from what it says.
  const readFirst = useCallback(() => {
    void read().then((next) => {
      if (!next) return;
      setMode(next.mode);
      setAddress(next.address);
    });
  }, [read]);
  useEffect(() => { readFirst(); }, [readFirst]);

  // The address the server is reached at changed — turned on, given, off:
  // the page takes it at once, for its links and what waits on one
  // (2026-10-26: a phone held back at the first setup waited for a reload).
  const reachedAt = status ? `${status.mode}|${status.state}|${status.url ?? ""}|${status.address ?? ""}` : "";
  const firstReach = useRef("");
  useEffect(() => {
    if (!reachedAt) return;
    if (!firstReach.current) {
      firstReach.current = reachedAt;
      return;
    }
    if (firstReach.current !== reachedAt) {
      firstReach.current = reachedAt;
      void refreshRuntimeConfig();
    }
  }, [reachedAt]);

  // While the tunnel opens or tries again, the screen follows it.
  useEffect(() => {
    if (status?.state !== "starting" && status?.state !== "retrying") return undefined;
    const timer = window.setTimeout(() => void read(), 2000);
    return () => window.clearTimeout(timer);
  }, [read, status]);

  // Your own address is checked a moment after it applies — its name looked
  // up, the certificate answering there read (server_address_tls.go). Till
  // the check is in, the page says it is checking and follows it, as the
  // address window does, so its line comes without opening the page again
  // (2026-11-19: "Checked: …" showed only after leaving and coming back).
  const addressChecking = status?.mode === "address" && Boolean(status.address) && !status.addressCheck;
  useEffect(() => {
    if (!addressChecking) return undefined;
    const timer = window.setTimeout(() => void read(), 1500);
    return () => window.clearTimeout(timer);
  }, [addressChecking, read, status]);

  async function clearCertificateAlert() {
    setBusy(true);
    setError("");
    try {
      setStatus(await request<TunnelStatus>("/admin/tunnel/certificate-alert/clear", { method: "POST" }, session));
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("tunnel.applyFailed")));
    } finally {
      setBusy(false);
    }
  }

  async function logoutTailscale() {
    setBusy(true);
    setError("");
    try {
      const next = await request<TunnelStatus>("/admin/tunnel/tailscale/logout", { method: "POST" }, session);
      setStatus(next);
      setMode(next.mode);
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("tunnel.tailscaleLogoutFailed")));
    } finally {
      setBusy(false);
    }
  }

  async function apply(next: TunnelMode) {
    setBusy(true);
    setError("");
    setAddressRefused(false);
    if (next === "tailscale") outside.prepare();
    try {
      const body = next === "token" ? { mode: next, token, address } : next === "address" ? { mode: next, address } : { mode: next };
      setStatus(await request<TunnelStatus>("/admin/tunnel", { method: "PUT", body: JSON.stringify(body) }, session));
      setMode(next);
      setToken("");
    } catch (failure) {
      outside.settle();
      setAddressRefused(next === "address");
      // Off refused, the choice goes back to what still runs, so the row never says off while it is on.
      if (next === "off" && status) setMode(status.mode);
      setError(tunnelChangeMessage(failure, t, next === "address" || next === "token" ? next : "other"));
    } finally {
      setBusy(false);
    }
  }

  // "끔" needs nothing more, so choosing it turns remote access off at once —
  // each way comes back with what it kept (the Tailscale device, the token,
  // the address); a quick try's address changes with every start anyway.
  // 2026-11-11: "끔" chosen said "같은 컴퓨터·같은 망에서만" while the public
  // address stayed open until a second press.
  function choose(next: TunnelMode) {
    setMode(next);
    if (next === "off" && status && status.mode !== "off") void apply("off");
  }

  const localOnly = Boolean(status?.localOnly);
  const choices: Array<[TunnelMode, string, string]> = [
    ["off", t("tunnel.off"), status?.reachedFrom?.length ? t("tunnel.offReached", { origin: status.reachedFrom[0].origin }) : t("tunnel.offDescription")],
    ["tailscale", t("tunnel.tailscale"), t("tunnel.tailscaleDescription")],
    ["address", t("tunnel.ownAddress"), t("tunnel.ownAddressDescription")],
    ["quick", t("tunnel.quick"), t("tunnel.quickDescription")],
    ["token", t("tunnel.token"), t("tunnel.tokenDescription")],
  ];
  // Every way is offered with the start settings' address too: one chosen
  // here takes over from it (server_tunnel_start_address.go, 2026-11-22).
  const offered = choices;
  const chosen = offered.find(([value]) => value === mode) ?? offered[0];
  const running = status?.state === "on" && status.url;
  // The chosen way's line says what it does in two sentences at most; what it
  // passes through and its limits are folded under it (2026-11-11: six
  // sentences at once).
  const facts: Record<TunnelMode, string[]> = {
    off: [],
    tailscale: [t("tunnel.tailscaleSetup"), t("tunnel.tailscaleFact")],
    address: [t("tunnel.ownAddressFact")],
    quick: [t("tunnel.quickLimits"), t("tunnel.cloudflare")],
    token: [t("tunnel.cloudflare")],
  };
  // Off chosen while the server still answers outside: it is being turned off.
  const stopping = busy && mode === "off" && Boolean(status && status.mode !== "off");
  // One choice in 설정's select, the chosen one's line under it (2026-11-05: five
  // bordered radio cards, each with its paragraph, all at once).
  return (
    <section className={styles.section} aria-labelledby="administrator-tunnel-title" data-server-tunnel>
      <SettingsSectionTitle id="administrator-tunnel-title" className={styles.sectionTitle} restates>{t("tunnel.title")}</SettingsSectionTitle>
      {/* The way in is the status read's: no choice until it is in (2026-11-20: a
          failed read offered "끄기" as if it were off). */}
      {status ? <div className={styles.settingRows}>
        <SettingRow title={t("tunnel.way")} data-tunnel-choice={mode}
          description={stopping ? undefined : chosen[2]}>
          <AppSelect look="setting" aria-label={t("tunnel.way")} value={mode} disabled={busy}
            onChange={(event) => choose(event.target.value as TunnelMode)} data-tunnel-mode>
            {offered.map(([value, label]) => <option key={value} value={value}>{label}</option>)}
          </AppSelect>
        </SettingRow>
        {!stopping && facts[chosen[0]].length ? (
          <details className={`${styles.settingNote} ${dataStyles.fold}`} data-tunnel-facts={chosen[0]}>
            <summary>{t("remoteChoice.details")}</summary>
            {facts[chosen[0]].map((fact) => <p key={fact} data-tunnel-fact>{fact}</p>)}
          </details>
        ) : null}
        {localOnly ? <p className={styles.settingNote} data-tunnel-local-only>{t("tunnel.localOnly")}</p> : null}
        {status.configured ? <p className={styles.settingNote} data-tunnel-configured={status.configuredInUse ? "in-use" : "kept"}>
          {t(status.configuredInUse ? "tunnel.configured" : "tunnel.configuredKept", { address: status.configured })}</p> : null}
      </div> : null}
      {mode === "address" ? (
        <div className={styles.settingRows}>
          <SettingField title={t("tunnel.addressField")}>
            <input type="url" value={address} disabled={busy} placeholder="https://wiki.example.com" data-tunnel-own-address-field
              onChange={(event) => { setAddress(event.target.value); setAddressRefused(false); }}
              {...(error && addressRefused ? { "aria-invalid": true as const, "aria-describedby": "tunnel-error" } : {})} />
          </SettingField>
        </div>
      ) : null}
      {status?.mode === "address" && status.address ? (
        <p className={styles.organizationMeta} data-tunnel-own-address>{t("tunnel.ownAddressSet", { address: status.address })}
          {status.addressTlsPort ? ` ${t("tunnel.ownAddressTLS", { port: status.addressTlsPort })}` : ""}</p>
      ) : null}
      {addressChecking ? (
        <p className={styles.organizationMeta} role="status" data-tunnel-address-check="checking">
          {t("remoteChoice.domainChecking", { address: status?.address ?? "" })}
        </p>
      ) : null}
      {status?.mode === "address" && status.addressCheck ? (
        <p className={status.addressCheck.problem ? styles.errorText : styles.organizationMeta} data-tunnel-address-check={status.addressCheck.problem || "ok"}>
          {status.addressCheck.problem ? t(`remoteChoice.check.${status.addressCheck.problem}`)
            : t("remoteChoice.check.ok", { names: (status.addressCheck.names ?? []).join(", "),
              date: status.addressCheck.notAfter ? displayDateTime(status.addressCheck.notAfter) : "" })}
        </p>
      ) : null}
      {mode === "token" ? (
        <div className={styles.settingRows}>
          <SettingField title={t("tunnel.tokenField")}>
            <input type="password" autoComplete="off" value={token} disabled={busy} data-tunnel-token
              placeholder={status?.hasToken ? t("tunnel.tokenKept") : t("tunnel.tokenPlaceholder")}
              onChange={(event) => setToken(event.target.value)} />
          </SettingField>
          <SettingField title={t("tunnel.addressField")}
            description={status?.door ? <span data-tunnel-door>{t("tunnel.door", { address: status.door })}</span> : undefined}>
            <input type="url" value={address} disabled={busy} placeholder="https://wiki.example.com" data-tunnel-address
              onChange={(event) => setAddress(event.target.value)} />
          </SettingField>
        </div>
      ) : null}
      {status?.mode === "token" && status.doorMissed ? (
        <p className={styles.organizationMeta} role="status" data-tunnel-door-missed>{t("tunnel.doorMissed", { address: status.door ?? "" })}</p>
      ) : null}
      {/* Off and left off, there is nothing to press and nothing to report. The state the
          server is in comes first, then what pressing would change (2026-11-11: "끄기 켜짐"). */}
      {mode === "off" && status?.mode === "off" ? null : <div className={styles.tunnelActions}>
        {/* Your own address runs no tunnel: its line says the address instead of a state. */}
        <span data-tunnel-state={stopping ? "stopping" : status?.state ?? "off"} data-tunnel-stage={status?.stage}>
          {stopping ? t("tunnel.state.stopping")
            : status && status.mode !== "address" && status.mode !== "off"
              // While it opens, what it waits for (server_tunnel_stage.go), as an agent is told.
              ? (status.state === "starting" && status.stage && status.stage !== "starting" ? t(`tunnel.stage.${status.stage}`) : t(`tunnel.state.${status.state}`))
              : ""}
        </span>
        {/* Only what would change; off is turned off as it is chosen. */}
        {mode !== "off" && (mode !== (status?.mode || "off") || mode === "address" || mode === "token" || status?.state === "retrying") ? (
          <button type="button" className={styles.primaryButton} disabled={busy} data-tunnel-apply onClick={() => void apply(mode)}>
            {t("tunnel.turnOn")}
          </button>
        ) : null}
      </div>}
      {running ? (
        <div className={styles.tunnelAddress}>
          <code data-tunnel-url>{status.url}</code>
          <button type="button" className={styles.secondaryButton} onClick={() => void copy(status.url ?? "")}>{t("tunnel.copy")}</button>
        </div>
      ) : null}
      {status?.mode === "tailscale" && (status.tailscaleLogin || status.tailscaleAllow) ? (
        <div className={styles.tunnelAddress} role="status" data-tunnel-tailscale-waiting={status.tailscaleLogin ? "login" : "allow"}>
          <p className={styles.organizationMeta} data-tunnel-tailscale-opened={outside.opened || undefined}>
            {t(status.tailscaleLogin ? (outside.opened ? "tunnel.tailscaleLoginOpened" : "tunnel.tailscaleLogin")
              : outside.opened ? "tunnel.tailscaleAllowOpened" : "tunnel.tailscaleAllow")}
          </p>
          <a className={styles.secondaryButton} href={status.tailscaleLogin || status.tailscaleAllow} target="_blank" rel="noopener noreferrer"
            onClick={outside.pressed} data-tunnel-tailscale-open>{t(outside.opened ? "tunnel.tailscaleOpenAgain" : "tunnel.tailscaleOpen")}</a>
        </div>
      ) : null}
      {status?.mode === "tailscale" && status.tailscalePublishing ? (
        <p className={styles.organizationMeta} role="status" data-tunnel-tailscale-publishing>{t("tunnel.tailscalePublishing")}</p>
      ) : null}
      {status?.mode === "tailscale" && status.tailscaleRenamed ? (
        <p className={styles.organizationMeta} role="status" data-tunnel-tailscale-renamed>
          {t("tunnel.tailscaleRenamed", { name: status.tailscaleRenamed })}
        </p>
      ) : null}
      {running && status.mode === "tailscale" ? (
        <div className={styles.tunnelAddress} data-tunnel-tailscale-on>
          <p className={styles.organizationMeta}>{t("tunnel.tailscaleTailnetName")}</p>
          <button type="button" className={styles.secondaryButton} disabled={busy} data-tunnel-tailscale-logout
            onClick={() => void logoutTailscale()}>{t("tunnel.tailscaleLogout")}</button>
        </div>
      ) : null}
      {/* Off, the server keeps no earlier address open — its kept certificate's
          port, Tailscale's old device — until it is turned on again (2026-11-11);
          one behind the administrator's own proxy goes on while that sends it. */}
      {/* An earlier Tailscale address whose device asks to be signed in again doesn't
          forward until it is, which is said with the way back (2026-11-11: it said it
          forwarded). One its device was signed out of here isn't listed at all. */}
      {(status?.forwards ?? []).map((forward) => {
        const paused = status?.mode === "off" && forward.kind !== "proxy";
        const signedOut = !paused && forward.kind === "tailscale" && forward.signedOut === true;
        return (
          <p key={forward.from} className={styles.organizationMeta} data-tunnel-forwarding={forward.kind}
            data-tunnel-forwarding-paused={paused || undefined} data-tunnel-forwarding-signed-out={signedOut || undefined}>
            {paused ? t("tunnel.forwardingPaused", { from: forward.from, date: displayDateTime(forward.until) })
              : signedOut ? t("tunnel.forwardingSignedOut", { from: forward.from })
                : t("tunnel.forwarding", { from: forward.from, to: forward.to, date: displayDateTime(forward.until) })}
            {forward.kind === "proxy" ? ` ${t("tunnel.forwardingProxy")}` : ""}
            {paused || signedOut ? "" : ` ${t("tunnel.forwardingExtends")}`}
            {forward.lastSeen && forward.seen?.length ? ` ${t("tunnel.forwardingInUse", {
              uses: forward.seen.map((use) => t(forwardingUses[use])).join(", "), date: displayDateTime(forward.lastSeen),
            })}` : ""}
          </p>
        );
      })}
      {status?.certificateAlert ? (
        <div className={styles.tunnelAddress} role="alert" data-tunnel-certificate-alert>
          <p className={styles.errorText}>{t("tunnel.certificateAlert", {
            issuer: status.certificateAlert.issuer,
            date: displayDateTime(/(?:[zZ]|[+-]\d\d:\d\d)$/u.test(status.certificateAlert.notBefore)
              ? status.certificateAlert.notBefore : `${status.certificateAlert.notBefore}Z`),
          })}</p>
          <button type="button" className={styles.secondaryButton} disabled={busy} onClick={() => void clearCertificateAlert()}>
            {t("tunnel.certificateAlertClear")}
          </button>
        </div>
      ) : null}
      {status?.mode === "tailscale" && status.certificateWatch && !status.certificateAlert ? (() => {
        // A look at the logs that failed is not an alert: the screen says when they were last read.
        const { checkedAt, failedAt } = status.certificateWatch;
        const unverified = Boolean(failedAt && (!checkedAt || failedAt > checkedAt));
        const date = checkedAt ? displayDateTime(checkedAt) : t("tunnel.certificateWatchNever");
        return (
          <p className={styles.organizationMeta} role={unverified ? "status" : undefined}
            data-tunnel-certificate-watch={unverified ? "unverified" : "checked"}>
            {t(unverified ? "tunnel.certificateWatchUnverified" : "tunnel.certificateWatchChecked", { date })}
          </p>
        );
      })() : null}
      {running && status.mode === "quick" ? <p className={styles.organizationMeta}>{t("tunnel.quickChanges")}</p> : null}
      {/* Why the address isn't open, in the screen's words; the tunnel's own words folded (tunnelFailureLine.ts). */}
      {status?.failure || status?.failureKind ? <>
        <p className={styles.errorText} role={tunnelHeld(status.failureKind) ? "status" : "alert"} data-tunnel-failure={status.failureKind || "unknown"}>
          {tunnelFailureLine(t, status.failureKind)}</p>
        {status.failure ? <TunnelFailureDetails failure={status.failure} /> : null}
      </> : null}
      {error && !status ? <ReadFailed message={error} onRetry={() => { setError(""); readFirst(); }} /> : null}
      {error && status ? <p id="tunnel-error" className={styles.errorText} role="alert">{error}</p> : null}
    </section>
  );
}
