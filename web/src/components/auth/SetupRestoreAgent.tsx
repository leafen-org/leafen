import { type FormEvent, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { LeafenApiError } from "@/apiTransport";
import { administratorErrorCodes } from "@/administratorWire.generated";
import { outsideAddress } from "@/lib/addressReach";
import { copyText } from "@/lib/copyText";
import { backupPlaceFieldLabel } from "@/lib/backupPlaceFields";
import { productErrorMessage } from "@/lib/productErrorPresentation";
import { serverOrigin } from "@/lib/serverOrigin";
import { keepSetupRestoreSignIn, takeSetupRestoreSignIn, type SetupRestoreAgentOutside, type SetupRestoreAgentSecrets,
  type SetupRestoreAgentState } from "@/lib/setupRestore";
import { setupRestoreAgentSentence } from "@/lib/setupRestoreAgentText";
import styles from "../AuthGate.module.css";
import own from "./SetupRestore.module.css";
import agent from "./SetupRestoreAgent.module.css";
import { BackupProgressLine } from "../admin/BackupProgress";
import { TunnelFailureDetails } from "../remoteAccess/TunnelFailureDetails";

type Step = "where" | "outside" | "opening" | "sentence" | "ended";
const POLL_MS = 1_500;

/**
 * "에이전트에게 맡기기" in a new server's restore (user decision 2026-11-19):
 * first where the agent is — on this computer, it reaches the server as this
 * page does; elsewhere (a chat app), a way in from outside opens for the
 * restore alone, a one-time link or Tailscale, unless the server is reached
 * from anywhere already. Then the sentence to paste, with a restore code
 * that does this one restore only. The agent finds the backups and restores
 * them; what opens them — the backups' password or recovery code, the
 * storage's keys — the person types here, in the fields the agent's place
 * asks for, and the agent never sees. This screen holds the restore's
 * sign-in pass from the start, so once the agent restores, it goes on as
 * after a restore of its own (SetupRestore.tsx). Leafen's own screen.
 */
export function SetupRestoreAgent({ setupCode, onBack, onRestored }: {
  setupCode: string;
  onBack(): void;
  onRestored(restart: "automatic" | "manual"): void;
}) {
  const { t } = useTranslation("authGate");
  const [step, setStep] = useState<Step>("where");
  const [elsewhere, setElsewhere] = useState(false);
  const [code, setCode] = useState("");
  const [state, setState] = useState<SetupRestoreAgentState | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [copied, setCopied] = useState(false);
  const [secrets, setSecrets] = useState<SetupRestoreAgentSecrets>({});
  const restored = useRef(false);
  const restoring = useRef(false);
  const whenRestored = useRef(onRestored);
  whenRestored.current = onRestored;

  // While the code lives, the screen follows the restore: the outside way
  // opening, where the agent looks, what it found, what was typed. A server
  // that says it is set up, or that stops answering while the agent
  // restores, is starting again on the restore.
  useEffect(() => {
    if (step !== "opening" && step !== "sentence") return undefined;
    const finish = (restart: "automatic" | "manual") => {
      if (restored.current) return;
      restored.current = true;
      whenRestored.current(restart);
    };
    let live = true;
    let timer = 0;
    let missed = 0;
    const look = () => {
      api.setupRestoreAgentState(setupCode).then((next) => {
        if (!live) return;
        missed = 0;
        setState(next);
        restoring.current = Boolean(next.running);
        if (next.restart) finish(next.restart === "manual" ? "manual" : "automatic");
        else if (!next.active && next.ended === "restored") finish("automatic");
        else if (!next.active) setStep("ended");
        else if (step === "opening" && next.outside?.state === "on" && next.outside.address) setStep("sentence");
      }, (cause: unknown) => {
        if (!live) return;
        if (cause instanceof LeafenApiError && cause.code === administratorErrorCodes.setupClosed) {
          finish("automatic");
          return;
        }
        missed += 1;
        if (restoring.current && missed >= 2) finish("automatic");
      }).finally(() => {
        if (live && !restored.current) timer = window.setTimeout(look, POLL_MS);
      });
    };
    look();
    return () => {
      live = false;
      window.clearTimeout(timer);
    };
  }, [step, setupCode]);

  async function begin(outside: SetupRestoreAgentOutside, away: boolean) {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      const begun = await api.beginSetupRestoreAgent(setupCode, outside);
      // The pass this screen signs in with once the agent has restored.
      keepSetupRestoreSignIn(begun.signIn);
      setCode(begun.code);
      setElsewhere(away);
      setState(null);
      setCopied(false);
      setSecrets({});
      setStep(outside ? "opening" : "sentence");
    } catch (cause) {
      setError(productErrorMessage(cause, "administratorScheduledBackup.restore"));
    } finally {
      setBusy(false);
    }
  }

  async function chooseElsewhere() {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      const seen = await api.setupRestoreAgentState(setupCode);
      setBusy(false);
      // The server names only an outside one (setupRestorePublicAddress); judged as everywhere.
      if (outsideAddress(seen.publicAddress ?? "")) {
        await begin("", true);
        return;
      }
      setStep("outside");
    } catch (cause) {
      setError(productErrorMessage(cause, "administratorScheduledBackup.restore"));
      setBusy(false);
    }
  }

  async function stop() {
    if (busy) return;
    setBusy(true);
    try {
      await api.endSetupRestoreAgent(setupCode);
    } catch {
      // A code that can't be ended here ends by itself within half an hour.
    }
    takeSetupRestoreSignIn();
    setBusy(false);
    onBack();
  }

  async function typeSecrets(event: FormEvent) {
    event.preventDefault();
    const filled = Object.fromEntries(Object.entries(secrets).filter(([, value]) => value)) as SetupRestoreAgentSecrets;
    if (busy || !Object.keys(filled).length) return;
    setBusy(true);
    setError("");
    try {
      setState(await api.typeSetupRestoreAgentSecrets(setupCode, filled));
      setSecrets({});
    } catch (cause) {
      setError(productErrorMessage(cause, "administratorScheduledBackup.restore"));
    } finally {
      setBusy(false);
    }
  }

  const errorLine = error ? <p className={styles.error} role="alert" aria-live="assertive">{error}</p> : null;
  const stopButton = (
    <button className={styles.linkButton} type="button" onClick={() => void stop()} disabled={busy} data-setup-restore-agent-stop>
      {step === "where" || step === "outside" ? t("back") : t("setupRestoreAgentStop")}
    </button>
  );
  const choice = (label: string, note: string, onClick: () => void, name: string, primary = false) => (
    <div className={agent.choice}>
      <button className={primary ? styles.primary : styles.outlined} type="button" disabled={busy} onClick={onClick}
        aria-describedby={`setup-restore-agent-${name}-note`} data-setup-restore-agent-choice={name}>{label}</button>
      <p id={`setup-restore-agent-${name}-note`} className={styles.otherWayNote}>{note}</p>
    </div>
  );

  if (step === "where") {
    return (
      <div className={styles.form} data-setup-restore-agent="where">
        <div className={styles.notice}>
          <strong>{t("setupRestoreAgentWhereTitle")}</strong>
          <p>{t("setupRestoreAgentWhereBody")}</p>
        </div>
        {choice(t("setupRestoreAgentHere"), t("setupRestoreAgentHereNote"), () => void begin("", false), "here", true)}
        {choice(t("setupRestoreAgentElsewhere"), t("setupRestoreAgentElsewhereNote"), () => void chooseElsewhere(), "elsewhere")}
        {errorLine}
        {stopButton}
      </div>
    );
  }
  if (step === "outside") {
    return (
      <div className={styles.form} data-setup-restore-agent="outside">
        <div className={styles.notice}>
          <strong>{t("setupRestoreAgentOutsideTitle")}</strong>
          <p>{t("setupRestoreAgentOutsideBody")}</p>
        </div>
        {choice(t("setupRestoreAgentQuick"), t("setupRestoreAgentQuickNote"), () => void begin("quick", true), "quick", true)}
        {choice(t("setupRestoreAgentTailscale"), t("setupRestoreAgentTailscaleNote"), () => void begin("tailscale", true), "tailscale")}
        {errorLine}
        {stopButton}
      </div>
    );
  }
  if (step === "ended") {
    return (
      <div className={styles.form} data-setup-restore-agent="ended">
        <div className={styles.notice}>
          <strong>{t("setupRestoreAgentOpen")}</strong>
          <p role="status">{state?.ended === "set-up" ? t("setupRestoreClosed") : t("setupRestoreAgentEnded")}</p>
        </div>
        {state?.ended === "set-up" ? (
          <button className={styles.primary} type="button" onClick={() => window.location.replace("/")}>{t("setupRestoreAgentSignIn")}</button>
        ) : (
          <button className={styles.primary} type="button" disabled={busy} onClick={() => setStep("where")} data-setup-restore-agent-new>
            {t("setupRestoreAgentNewCode")}
          </button>
        )}
        {stopButton}
      </div>
    );
  }
  const outside = state?.outside;
  if (step === "opening") {
    return (
      <div className={styles.form} data-setup-restore-agent="opening">
        <div className={styles.notice}>
          <strong>{t("setupRestoreAgentOutsideTitle")}</strong>
          <p role="status">{outside?.tailscalePublishing ? t("setupRestoreAgentPublishing")
            : outside?.state === "retrying" ? t("setupRestoreAgentOutsideFailed") : t("setupRestoreAgentOpening")}</p>
        </div>
        {outside?.tailscaleLogin || outside?.tailscaleAllow ? (
          <div className={agent.choice}>
            <p className={agent.line}>{outside.tailscaleLogin ? t("setupRestoreAgentTailscaleSignIn") : t("setupRestoreAgentTailscaleAllow")}</p>
            <a className={styles.primary} href={outside.tailscaleLogin || outside.tailscaleAllow} target="_blank" rel="noreferrer"
              data-setup-restore-agent-tailscale>{t("setupRestoreAgentTailscaleOpen")}</a>
          </div>
        ) : null}
        {outside?.state === "retrying" && outside.failure ? <><p className={agent.note} data-tunnel-failure-line>
          {t(outside.mode === "tailscale" ? "setupRestoreAgentOutsideFailedTailscale" : "setupRestoreAgentOutsideFailedQuick")}</p>
          <TunnelFailureDetails failure={outside.failure} /></> : null}
        {stopButton}
      </div>
    );
  }

  const origin = elsewhere ? outside?.address || state?.publicAddress || "" : serverOrigin();
  const minutes = Math.max(1, Math.round(((state?.expiresAt ?? Date.now() + 30 * 60_000) - Date.now()) / 60_000));
  const sentence = origin && code ? setupRestoreAgentSentence({ origin, code, minutes, elsewhere }) : "";
  const place = state?.place;
  const placeName = !place ? "" : place.target === "webdav" ? place.webdavUrl ?? ""
    : place.target === "s3" ? [place.s3Endpoint, place.s3Bucket].filter(Boolean).join(" · ") : place.folder ?? "";
  // What the person types here: the keys of the storage the agent named, and
  // the backups' password once it found sealed ones.
  const asks: Array<{ name: keyof SetupRestoreAgentSecrets; label: string }> = [
    // Named as Settings and the restore screen name them (lib/backupPlaceFields.ts).
    ...(place?.target === "webdav" ? [{ name: "webdavPassword" as const, label: t(backupPlaceFieldLabel("webdavPassword")) }] : []),
    ...(place?.target === "s3" ? [{ name: "s3AccessKey" as const, label: t(backupPlaceFieldLabel("s3AccessKey")) },
      { name: "s3SecretKey" as const, label: t(backupPlaceFieldLabel("s3SecretKey")) }] : []),
    ...(state?.found?.sealed || state?.typed.secret ? [{ name: "secret" as const, label: t("setupRestoreSecret") }] : []),
  ];
  const status = state?.running ? t("setupRestoreAgentRestoring")
    : placeName ? t("setupRestoreAgentLooking", { place: placeName })
      : state?.agentSeen ? t("setupRestoreAgentConnected") : t("setupRestoreAgentWaiting");
  return (
    <div className={styles.form} data-setup-restore-agent="sentence">
      <div className={styles.notice}>
        <strong>{t("setupRestoreAgentSentenceTitle")}</strong>
        <p>{t("setupRestoreAgentSentenceBody")}</p>
      </div>
      <pre className={agent.sentence} data-setup-restore-agent-sentence>{sentence}</pre>
      <button className={styles.primary} type="button" disabled={!sentence}
        onClick={() => void copyText(sentence).then((done) => setCopied(done))} data-setup-restore-agent-copy>
        {copied ? t("setupRestoreAgentCopied") : t("setupRestoreAgentCopy")}
      </button>
      <p className={agent.note}>{t("setupRestoreAgentCodeNote", { minutes })}</p>
      <div className={agent.progress} data-setup-restore-agent-progress>
        <p className={agent.line} role="status" aria-live="polite">{status}</p>
        {/* How far the agent's restore has got, as the restore here shows it (setup_restore_agent.go). */}
        {state?.running && state.progress ? <BackupProgressLine progress={state.progress} /> : null}
        {state?.found ? (
          <p className={agent.note}>{[state.found.replica ? t("setupRestoreAgentFoundReplica") : "",
            t("setupRestoreAgentFoundSnapshots", { count: state.found.snapshots })].filter(Boolean).join(" ")}</p>
        ) : null}
      </div>
      {asks.length ? (
        <form className={agent.secrets} noValidate onSubmit={(event) => void typeSecrets(event)} data-setup-restore-agent-secrets>
          <strong className={agent.line}>{t("setupRestoreAgentSecretsTitle")}</strong>
          <p className={agent.note}>{t("setupRestoreAgentSecretsNote")}</p>
          {asks.map((ask) => (
            <label className={own.field} key={ask.name}>
              <span className={styles.label}>{ask.label}{state?.typed[ask.name] ? ` · ${t("setupRestoreAgentTyped")}` : ""}</span>
              <input className={styles.input} type="password" autoComplete="off" spellCheck={false} disabled={busy}
                value={secrets[ask.name] ?? ""} onChange={(event) => setSecrets((current) => ({ ...current, [ask.name]: event.target.value }))}
                data-setup-restore-agent-secret={ask.name} />
            </label>
          ))}
          <button className={styles.secondary} type="submit" disabled={busy || !Object.values(secrets).some(Boolean)}
            data-setup-restore-agent-secrets-save>{t("setupRestoreAgentSecretsSave")}</button>
        </form>
      ) : null}
      {errorLine}
      {stopButton}
    </div>
  );
}
