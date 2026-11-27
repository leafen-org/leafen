"use client";

import { useCallback, useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { FirstRunScreen } from "@/components/firstInstall/FirstRunScreen";
import { Trans, useTranslation } from "react-i18next";
import { request } from "@/apiTransport";
import { settlePhoneOffer } from "@/lib/phoneOffer";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { useViewEntryFocus } from "@/lib/viewEntryFocus";
import { refreshRuntimeConfig } from "@/lib/runtimeConfigService";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import { useDeviceCode } from "@/lib/useDeviceCode";
import { useQRCode } from "@/lib/useQRCode";
import type { Session } from "@/types";
import { X } from "../icons";
import { UploadProgressBar } from "../import/UploadProgressBar";
import { RemoteAddressOwnWay } from "./RemoteAddressOwnWay";
import { useAddressStepPlace } from "./addressStepPlace";
import { OutsideAddressGuide } from "./OutsideAddressGuide";
import { TailscaleAddressGuide } from "./TailscaleAddressGuide";
import { tailscaleReason, tunnelChangeMessage, type ServerReach } from "./remoteAddressPrompt";
import styles from "./RemoteAddressChoice.module.css";
import { useOutsideOpener } from "@/lib/outsideOpener";
import { inDesktopTab } from "@/lib/desktopTabs";
import { followTunnel, tellTunnel } from "./tunnelAcrossTabs";
import { TunnelFailureDetails } from "./TunnelFailureDetails";
import { displayDateTime } from "@/lib/displayDate";

type TunnelStatus = {
  mode: string;
  state: "off" | "starting" | "on" | "retrying";
  url?: string;
  failure?: string;
  tailscaleLogin?: string;
  tailscaleAllow?: string;
  tailscaleAddress?: string;
  tailscaleAvailable?: boolean;
  /** The address is open, its name not in the public DNS yet. */
  tailscalePublishing?: boolean;
  address?: string;
  addressCheck?: { resolves?: string[]; names?: string[]; notAfter?: number; trusted: boolean; problem?: string };
  /** The address the start settings give (LEAFEN_PUBLIC_URL), which links use. */
  configured?: string; configuredInUse?: boolean;
};

type Step = "checking" | "choose" | "own" | "domain" | "domainCheck" | "tailscale" | "quick" | "done" | "configured";

/**
 * Choosing the server's outside address once, when something first needs
 * one (the built-in tunnel decision's changes of 2026-10-19): agents on the
 * internet, phones and people outside reach the server only at an address of
 * its own. One thing is decided here (the one-thing-per-screen decision of
 * 2026-11-09): whether it opens from anywhere. The recommended way is the one
 * large button — a free public address from the administrator's own
 * Tailscale, signed in right here — with an example under it of what an
 * address does (OutsideAddressGuide); once chosen, a QR opens the sign-in on
 * a phone and the moving guide under it shows the phone's and Tailscale's
 * screens (TailscaleAddressGuide). The other ways are folded under "다른 방법":
 * their own domain, given and checked right here as Settings' "내 주소" is
 * (2026-10-27: a first setup's screen never leaves for Settings), and a quick
 * try with no account whose address changes. "나중에" is a small one that says
 * what waiting costs; it is not kept: the next thing that needs an address
 * asks again. A phone's own server opens no public address. Opening, the
 * screen measures the network: where Tailscale can't be used (a closed
 * network, Tailscale blocked) it hands the address to an agent instead
 * (RemoteAddressOwnWay), and notices when the agent has set one. A company's
 * server (server_tunnel_company.go) opens on its own address the same way,
 * with Tailscale and its own settings under "다른 방법".
 */
export function RemoteAddressChoice({ session, workspaceId, screen = false, onClose }: {
  session: Session;
  /** A first run's step: the sign-in screen's panel over the whole window, without a close (FirstRunScreen). */
  screen?: boolean;
  /** The workspace an agent's connection code is for, when the screen hands the address to an agent. */
  workspaceId?: string;
  /** Closes the screen, with the outside address when one came up. */
  onClose: (address?: string) => void;
}) {
  const { t } = useTranslation(["serverConsole", "common"]);
  const titleId = useId();
  const overlayRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  const bodyRef = useRef<HTMLDivElement>(null);
  const [step, setStep] = useState<Step>("checking");
  const [reach, setReach] = useState<ServerReach | null>(null);
  const [status, setStatus] = useState<TunnelStatus | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // A domain of the administrator's own, and the step "뒤로" goes back to
  // from it, from Tailscale or from a quick try.
  const [domain, setDomain] = useState("");
  const [from, setFrom] = useState<Step>("choose");
  // "다른 방법" stays open coming back from a way opened in it.
  const [otherWays, setOtherWays] = useState(false);
  const alive = useRef(true);
  useEffect(() => () => { alive.current = false; }, []);

  const close = useCallback(() => onClose(step === "configured" ? status?.configured
    : step === "done" ? (status?.mode === "address" ? status.address : status?.url) : undefined),
    [onClose, status, step]);
  const { zIndex } = useOverlayLayer(!screen, { kind: "dialog", ref: dialogRef, modalOwned: () => [overlayRef.current], onEscape: close });
  // The dialog's step takes the focus as it opens, once it is drawn (it opens
  // still checking, with no choice yet), and as each step replaces the last;
  // the full-screen first-run step does the same in FirstRunLayer.
  useViewEntryFocus(bodyRef, step, { enabled: !screen });
  // Where the step shows from as one view replaces another, and "나중에" kept where
  // it stood when the check ends on the own way's screen (addressStepPlace.ts).
  const firstChoice = useRef<HTMLButtonElement>(null);
  const holdLater = useAddressStepPlace(step, screen, dialogRef, firstChoice);

  // A Tailscale sign-in already under way picks up where it is; otherwise
  // the network decides the layout (a phone's server needs no measure). A
  // screen not in front measures once it is — a browser's tab behind, or the
  // app's tab behind the shown one, which is in view as it starts but not
  // focused (desktop/window_tabs_start.go): the server measures for up to six
  // seconds and holds each asker's request meanwhile, and thirteen tabs
  // opened behind at once took the six requests a web view keeps to the
  // server with it, so every tab's other reads waited (2026-11-13).
  useEffect(() => {
    let stopWaiting = () => {};
    void (async () => {
      const next = await request<TunnelStatus>("/admin/tunnel", { method: "GET" }, session).catch(() => null);
      if (next) tellTunnel(session.user.id, next);
      if (!alive.current) return;
      if (next) setStatus(next);
      // The start settings' address while it is the server's: said, another way choosable over it (2026-11-22).
      if (next?.configured && next.configuredInUse) {
        setStep("configured");
        return;
      }
      if (next?.mode === "tailscale") {
        setStep(next.state === "on" && next.url ? "done" : "tailscale");
        return;
      }
      if (next?.tailscaleAvailable === false) {
        setStep("choose");
        return;
      }
      const inFront = () => document.visibilityState === "visible" && (!inDesktopTab() || document.hasFocus());
      if (!inFront()) {
        await new Promise<void>((shown) => {
          const look = () => {
            if (!inFront()) return;
            stopWaiting();
            shown();
          };
          document.addEventListener("visibilitychange", look);
          window.addEventListener("focus", look);
          stopWaiting = () => {
            document.removeEventListener("visibilitychange", look);
            window.removeEventListener("focus", look);
          };
        });
        if (!alive.current) return;
      }
      const measured = await request<ServerReach>("/admin/tunnel/reach", { method: "GET" }, session).catch(() => null);
      if (!alive.current) return;
      setReach(measured);
      // A company's server is offered its own address first.
      const own = Boolean(measured && (measured.situation !== "ok" || measured.company));
      if (own) holdLater();
      setStep(own ? "own" : "choose");
    })();
    return () => stopWaiting();
  }, [holdLater, session]);

  // Handed to an agent, or given here, the screen notices the address set and
  // its check; an agent's `leafen tailscale` or `leafen address` from the
  // terminal is followed from the choice as well (user decision 2026-11-09).
  // While Tailscale or a quick tunnel opens, the screen follows it, a little
  // more often. One of the person's tabs in sight asks for all of them; a tab
  // behind asks nothing (2026-11-13: every tab asked every three seconds).
  const following = step === "own" || step === "domainCheck" || step === "choose" ? 3000
    : step === "tailscale" || step === "quick" ? 2000 : 0;
  const stepNow = useRef(step);
  stepNow.current = step;
  useEffect(() => {
    if (!following) return undefined;
    return followTunnel(session.user.id, following, () => request<TunnelStatus>("/admin/tunnel", { method: "GET" }, session),
      (value) => {
        const next = value as TunnelStatus;
        const at = stepNow.current;
        if (!alive.current || (at !== "own" && at !== "domainCheck" && at !== "choose" && at !== "tailscale" && at !== "quick")) return;
        setStatus(next);
        if (at === "tailscale" || at === "quick") {
          if (next.state === "on" && next.url) {
            setStep("done");
            void refreshRuntimeConfig();
          }
        } else if (next.mode === "address" && next.addressCheck) {
          setStep("done");
          void refreshRuntimeConfig();
        } else if (next.mode === "tailscale") {
          setStep(next.state === "on" && next.url ? "done" : "tailscale");
          if (next.state === "on") void refreshRuntimeConfig();
        }
      });
  }, [following, session]);

  const openDomain = () => {
    setFrom(step);
    setError("");
    setStep("domain");
  };

  async function applyDomain() {
    setBusy(true);
    setError("");
    try {
      const next = await request<TunnelStatus>("/admin/tunnel", { method: "PUT", body: JSON.stringify({ mode: "address", address: domain.trim() }) }, session);
      tellTunnel(session.user.id, next);
      setStatus(next);
      if (next.addressCheck) {
        setStep("done");
        void refreshRuntimeConfig();
      } else {
        setStep("domainCheck");
      }
    } catch (failure) {
      setError(tunnelChangeMessage(failure, t, "address"));
    } finally {
      setBusy(false);
    }
  }

  async function start(mode: "tailscale" | "quick") {
    setBusy(true);
    setError("");
    if (mode === "tailscale") outside.prepare();
    setFrom(step);
    try {
      const next = await request<TunnelStatus>("/admin/tunnel", { method: "PUT", body: JSON.stringify({ mode }) }, session);
      tellTunnel(session.user.id, next);
      setStatus(next);
      setStep(mode);
    } catch (failure) {
      outside.settle();
      setError(settingsErrorMessage(failure, t("serverConsole:remoteChoice.failed")));
    } finally {
      setBusy(false);
    }
  }

  // While Tailscale, a quick tunnel or a domain's check opens, "뒤로" and
  // "나중에" turn it off and go back a step or on without one, as the domain
  // step's "뒤로" goes back (2026-11-03 audit: once Tailscale was chosen,
  // nothing left the screen, Escape and reloading included).
  async function leave(to: Step | "later") {
    setBusy(true);
    setError("");
    try {
      const next = await request<TunnelStatus>("/admin/tunnel", { method: "PUT", body: JSON.stringify({ mode: "off" }) }, session);
      tellTunnel(session.user.id, next);
      setStatus(next);
      outside.settle();
      if (to === "later") onClose();
      else setStep(to);
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("serverConsole:tunnel.applyFailed")));
    } finally {
      setBusy(false);
    }
  }
  const leaveButtons = (back: Step) => (
    <div className={styles.handoff} data-remote-leave>
      <button type="button" className={styles.laterLink} disabled={busy} onClick={() => void leave(back)}>{t("common:actions.back")}</button>
      <button type="button" className={styles.laterLink} disabled={busy} onClick={() => void leave("later")}>{t("serverConsole:remoteChoice.later")}</button>
    </div>
  );
  // Each wait shows it is going (the audit's few still seconds of measuring the network).
  const waiting = (label: string) => <UploadProgressBar className={styles.waiting} label={label} progress={{ phase: "preparing", percent: 0 }} />;
  const waitingAt = status?.tailscaleLogin || status?.tailscaleAllow || "";
  const outside = useOutsideOpener(step === "tailscale" ? waitingAt : "", step === "done");
  const signInPicture = useQRCode(step === "tailscale" ? waitingAt : "");
  const doneAddress = status?.mode === "address" ? status.address ?? "" : status?.url ?? "";
  // The phone that scans it joins this account there (account_device_codes.go).
  // A saved address that does not answer yet is not offered as open: no "has a public address" title, no QR.
  const addressBroken = status?.mode === "address" && Boolean(status.addressCheck?.problem);
  const { code: phoneCode } = useDeviceCode(session, step === "done" && Boolean(doneAddress) && !addressBroken);
  const addressPicture = useQRCode(step === "done" && doneAddress && phoneCode
    ? `${doneAddress.replace(/\/+$/u, "")}/device/${phoneCode.code}` : "");
  // Its phone QR is the phone a first setup on a local address held back (PhoneOfferLater).
  useEffect(() => {
    if (addressPicture) settlePhoneOffer(session.user.id);
  }, [addressPicture, session.user.id]);

  // A phone's own server opens no public address (server_tunnel_tailscale_phone.go): only "later".
  const phone = status?.tailscaleAvailable === false;
  const title = t(step === "configured" ? "serverConsole:remoteChoice.configuredTitle" : step === "done" ? (addressBroken ? "serverConsole:remoteChoice.doneSavedTitle" : "serverConsole:remoteChoice.doneTitle")
    // The address field opened from a company's or closed network's screen
    // keeps that screen's title, not "외부에서도 열리게 할까요?" (2026-11-06).
    : step === "own" || (step === "domain" && from === "own")
      ? (reach?.situation === "closed" || reach?.company ? "serverConsole:remoteChoice.closedTitle" : "serverConsole:remoteChoice.ownTitle")
      : "serverConsole:remoteChoice.title");
  const body = (
    <>
      <div className={styles.copy}>
        <h2 id={titleId}>{title}</h2>
        {step === "choose" || step === "checking" ? <p>{t(phone ? "serverConsole:remoteChoice.phone" : "serverConsole:remoteChoice.body")}</p> : null}
        {step === "own" && reach ? (
          <p>{reach.situation === "closed" ? t("serverConsole:remoteChoice.closedBody")
            : reach.situation === "ok" ? t("serverConsole:remoteChoice.companyBody")
              : t("serverConsole:remoteChoice.ownBody", { reason: tailscaleReason(t, reach) })}</p>
        ) : null}
      </div>
      {step === "own" && reach ? <RemoteAddressOwnWay session={session} reach={reach} workspaceId={workspaceId} screen={screen} onLater={() => onClose()}
        onTailscale={() => void start("tailscale")} onDomain={openDomain} /> : null}
      {step === "choose" && phone ? (
        <div className={styles.choices}>
          <button type="button" className={styles.smallChoice} disabled={busy} data-remote-choice="later" onClick={() => onClose()}>
            <strong>{t("serverConsole:remoteChoice.later")}</strong>
            <span>{t("serverConsole:remoteChoice.laterBody")}</span>
          </button>
        </div>
      ) : null}
      {/* While the network is measured the choices keep their room, so "나중에" is where it
          will stay: nothing comes up under a press (2026-11-05: a domain's box took its place). */}
      {(step === "choose" && !phone) || step === "checking" ? (
        <div className={styles.choices} data-checking={step === "checking" || undefined}>
          {step === "checking" ? (
            <div className={styles.checkingOver} role="status" data-remote-checking>
              <p>{t("serverConsole:remoteChoice.checking")}</p>
              {waiting(t("serverConsole:remoteChoice.checking"))}
            </div>
          ) : null}
          <button ref={firstChoice} type="button" className={styles.primaryChoice} disabled={busy || step === "checking"} data-remote-choice="tailscale"
            onClick={() => void start("tailscale")}>
            <strong>{t("serverConsole:remoteChoice.tailscaleChoice")}<span className={styles.badge}>{t("serverConsole:remoteChoice.recommended")}</span></strong>
            <span>{t("serverConsole:remoteChoice.tailscaleSteps")}</span>
            <span>{t("serverConsole:tunnel.tailscalePlan")}</span>
          </button>
          {/* A company's or home server with a way in already (port forwarding, a
              domain) is pointed at its own domain without opening "다른 방법" (user
              decision 2026-11-13). A server seen as a company's never comes here: it
              opens on its own screen ("own", companyBody), so the two never meet. */}
          <button type="button" className={styles.directHint} disabled={busy || step === "checking"} data-remote-direct-hint onClick={openDomain}>
            <Trans t={t} i18nKey="serverConsole:remoteChoice.directHint" components={{ way: <strong /> }} />
          </button>
          {/* What an address does, before anything is chosen; the button to press is above it.
              It plays while the network is measured, but takes no focus until the choices come. */}
          <div className={styles.choiceGuide} data-remote-outside-guide inert={step === "checking"}>
            <OutsideAddressGuide look={screen ? "screen" : "dialog"} />
          </div>
          <details className={styles.otherWays} open={otherWays} data-remote-other-ways
            onToggle={(event) => setOtherWays(event.currentTarget.open)}>
            <summary>{t("serverConsole:remoteChoice.otherWays")}</summary>
            <div className={styles.smallChoices}>
              <button type="button" className={styles.smallChoice} disabled={busy || step === "checking"} data-remote-choice="domain" onClick={openDomain}>
                <strong>{t("serverConsole:remoteChoice.domain")}<span className={styles.badge}>{t("serverConsole:remoteChoice.domainRecommended")}</span></strong>
                <span>{t("serverConsole:remoteChoice.domainBody")}</span>
              </button>
              <button type="button" className={styles.smallChoice} disabled={busy || step === "checking"} data-remote-choice="quick" onClick={() => void start("quick")}>
                <strong>{t("serverConsole:remoteChoice.quick")}</strong>
                <span>{t("serverConsole:remoteChoice.quickBody")}</span>
              </button>
            </div>
          </details>
          {/* Doing it is the way, "나중에" a small one that says what waiting costs (the
              one-program decision of 2026-10-27, 7) — in the first run's step and Settings' dialog alike. */}
          <div className={styles.screenLater}>
            <button type="button" className={styles.laterLink} disabled={busy} data-remote-choice="later" onClick={() => onClose()}>
              {t("serverConsole:remoteChoice.later")}
            </button>
            <p className={styles.fact} data-first-run-skip-cost={screen || undefined}>{t("serverConsole:remoteChoice.skipCost")}</p>
          </div>
        </div>
      ) : null}
      {step === "tailscale" ? (
        <div className={styles.progress} role="status" data-remote-tailscale={status?.tailscaleLogin ? "login" : status?.tailscaleAllow ? "allow" : status?.tailscalePublishing ? "publishing" : "starting"}>
          <p data-remote-tailscale-opened={outside.opened || undefined}>
            {t(status?.tailscaleLogin ? (outside.opened ? "serverConsole:tunnel.tailscaleLoginOpened" : "serverConsole:tunnel.tailscaleLogin")
              : status?.tailscaleAllow ? (outside.opened ? "serverConsole:tunnel.tailscaleAllowOpened" : "serverConsole:tunnel.tailscaleAllow")
              : status?.tailscalePublishing ? "serverConsole:tunnel.tailscalePublishing" : "serverConsole:remoteChoice.starting")}
          </p>
          {/* Done only once the name is in the public DNS: a phone that scans the address sooner
              is told "no such server" for minutes more (2026-11-11). */}
          {waitingAt ? null : waiting(t(status?.tailscalePublishing ? "serverConsole:tunnel.tailscalePublishing" : "serverConsole:remoteChoice.starting"))}
          {waitingAt ? (
            <div className={styles.handoff}>
              <a className={styles.openButton} href={waitingAt} target="_blank" rel="noopener noreferrer" onClick={outside.pressed}
                data-remote-tailscale-open>
                {t(outside.opened ? "serverConsole:tunnel.tailscaleOpenAgain" : "serverConsole:tunnel.tailscaleOpen")}
              </a>
              {/* The code opens what the step waits on: the sign-in, then the page that allows the address. */}
              {signInPicture ? (
                <figure className={styles.qr}>
                  <img src={signInPicture} alt={t(status?.tailscaleLogin ? "serverConsole:remoteChoice.signInQRAlt" : "serverConsole:remoteChoice.allowQRAlt")}
                    width={112} height={112} />
                  <figcaption>{t(status?.tailscaleLogin ? "serverConsole:remoteChoice.signInQR" : "serverConsole:remoteChoice.allowQR")}</figcaption>
                </figure>
              ) : null}
            </div>
          ) : null}
          {status?.failure && !waitingAt ? (
            <><p className={styles.note} data-tunnel-failure-line>{t("serverConsole:remoteChoice.tailscaleFailing")}</p><TunnelFailureDetails failure={status.failure} /></>
          ) : null}
          {leaveButtons(from)}
          <details className={styles.more} data-remote-more>
            <summary>{t("serverConsole:remoteChoice.details")}</summary>
            <p className={styles.fact}>{t("serverConsole:remoteChoice.tailscaleFact")}</p>
          </details>
        </div>
      ) : null}
      {/* What comes next on the phone and in Tailscale, shown while Leafen waits on them (2026-11-08);
          beside the step, not in it, so the step's text styles stay out of the guide's. */}
      {step === "tailscale" ? (
        <div className={styles.guide} data-remote-tailscale-guide><TailscaleAddressGuide look={screen ? "screen" : "dialog"} /></div>
      ) : null}
      {step === "domain" ? (
        <form className={styles.progress} data-remote-domain onSubmit={(event) => { event.preventDefault(); void applyDomain(); }}>
          <p>{t("serverConsole:tunnel.ownAddressDescription")}</p>
          <label className={styles.field}>
            <span>{t("serverConsole:tunnel.addressField")}</span>
            <input type="url" value={domain} disabled={busy} placeholder="https://wiki.example.com" required data-remote-domain-field
              onChange={(event) => { setDomain(event.target.value); setError(""); }}
              {...(error ? { "aria-invalid": true as const, "aria-describedby": "remote-address-error" } : {})} />
          </label>
          <p className={styles.fact}>{t("serverConsole:tunnel.ownAddressFact")}</p>
          <div className={styles.handoff}>
            {/* Empty, the field's own refusal says what is missing (the setup forms' rule), not a silent button. */}
            <button type="submit" className={styles.openButton} disabled={busy} data-remote-domain-apply>
              {t("serverConsole:tunnel.turnOn")}
            </button>
            <button type="button" className={styles.laterLink} disabled={busy} onClick={() => setStep(from)}>{t("common:actions.back")}</button>
          </div>
        </form>
      ) : null}
      {step === "domainCheck" ? (
        <div className={styles.progress} role="status" data-remote-domain-check>
          <p>{t("serverConsole:remoteChoice.domainChecking", { address: status?.address ?? domain.trim() })}</p>
          {waiting(t("serverConsole:remoteChoice.domainChecking", { address: status?.address ?? domain.trim() }))}
          {leaveButtons("domain")}
        </div>
      ) : null}
      {step === "quick" ? (
        <div className={styles.progress} role="status" data-remote-quick>
          <p>{t("serverConsole:remoteChoice.quickStarting")}</p>
          {waiting(t("serverConsole:remoteChoice.quickStarting"))}
          {status?.failure ? <p className={styles.note} data-remote-quick-failing>{t("serverConsole:remoteChoice.quickFailing")}</p> : null}
          {leaveButtons(from)}
        </div>
      ) : null}
      {step === "configured" && status?.configured ? (
        <div className={styles.progress} data-remote-configured>
          <code className={styles.address} data-remote-address>{status.configured}</code>
          <p className={styles.note}>{t("serverConsole:remoteChoice.configuredBody")}</p>
          <button type="button" className={styles.openButton} onClick={close}>{t("serverConsole:remoteChoice.finish")}</button>
          <button type="button" className={styles.laterLink} data-remote-configured-other onClick={() => setStep("choose")}>{t("serverConsole:remoteChoice.configuredOther")}</button>
        </div>
      ) : null}
      {step === "done" && doneAddress ? (
        <div className={styles.progress} data-remote-done>
          <code className={styles.address} data-remote-address>{doneAddress}</code>
          {status?.mode === "address" && status.addressCheck ? (
            <p className={styles.note} data-remote-address-check={status.addressCheck.problem || "ok"}>
              {status.addressCheck.problem ? t(`serverConsole:remoteChoice.check.${status.addressCheck.problem}`)
                : t("serverConsole:remoteChoice.check.ok", { names: (status.addressCheck.names ?? []).join(", "),
                  date: status.addressCheck.notAfter ? displayDateTime(status.addressCheck.notAfter) : "" })}
            </p>
          ) : null}
          {addressPicture ? (
            <figure className={styles.qr}>
              <img src={addressPicture} alt={t("serverConsole:remoteChoice.addressQRAlt")} width={112} height={112} />
              <figcaption>{t("serverConsole:remoteChoice.addressQR")}</figcaption>
            </figure>
          ) : null}
          {status?.mode !== "address" && status?.mode !== "tailscale" ? (
            <p className={styles.note}>{t("serverConsole:remoteChoice.quickChanges")}</p>
          ) : null}
          <button type="button" className={styles.openButton} onClick={close}>{t("serverConsole:remoteChoice.finish")}</button>
          {/* How a Tailscale address is named is for whoever wants it, folded. */}
          {status?.mode === "tailscale" ? (
            <details className={styles.more} data-remote-more>
              <summary>{t("serverConsole:remoteChoice.details")}</summary>
              <p className={styles.note}>{t("serverConsole:tunnel.tailscaleTailnetName")}</p>
            </details>
          ) : null}
        </div>
      ) : null}
      {error ? <p id="remote-address-error" className={styles.error} role="alert">{error}</p> : null}
    </>
  );
  if (screen) return <FirstRunScreen step="address" name={title} view={step}><div className={styles.screenBody} data-remote-address-choice={step}>{body}</div></FirstRunScreen>;
  const dialog = (
    <div ref={overlayRef} className={styles.overlay} style={{ zIndex }} data-remote-address-choice={step}>
      <button type="button" className={styles.backdrop} onClick={close} tabIndex={-1} aria-label={t("common:actions.close")} />
      <section ref={dialogRef} className={styles.dialog} role="dialog" aria-modal="true" aria-labelledby={titleId}>
        <button type="button" className={styles.close} onClick={close} aria-label={t("common:actions.close")}>
          <X size="regular" aria-hidden="true" />
        </button>
        <div ref={bodyRef} className={styles.dialogBody}>{body}</div>
      </section>
    </div>
  );

  return typeof document === "undefined" ? dialog : createPortal(dialog, document.body);
}
