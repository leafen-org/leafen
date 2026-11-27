import { type FormEvent, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { LeafenApiError } from "@/apiTransport";
import { administratorErrorCodes, type AdministratorBackupProgress } from "@/administratorWire.generated";
import { AppSelect } from "@/components/AppSelect";
import { SettingSwitch } from "@/components/admin/AdministratorSettingRows";
import { canChooseServerFolder, chooseServerFolder, deviceFolderName, listSyncFolders, pickedFoldersOnly, type SyncFolder } from "@/lib/deviceFolder";
import { displayDateTime } from "@/lib/displayDate";
import { canRestartPackagedServer, restartPackagedServer } from "@/lib/packagedServerRestart";
import { productErrorMessage } from "@/lib/productErrorPresentation";
import { saveSession } from "@/lib/authSessionStore";
import { thisWindowKey } from "@/lib/thisWindowWords";
import { useSeenSetupStatus } from "@/lib/useSeenSetupStatus";
import { BACKUP_PLACE_FIELDS, BACKUP_PLACE_PATH_STYLE, BACKUP_PLACE_TARGET_LABELS, backupPlacePlaceholder } from "@/lib/backupPlaceFields";
import { keepSetupRestoreSignIn, takeSetupRestoreSignIn, type SetupRestoreChoice, type SetupRestorePlace, type SetupRestorePlaceInput } from "@/lib/setupRestore";
import styles from "../AuthGate.module.css";
import own from "./SetupRestore.module.css";
import { SETUP_CODE_KEYBOARD } from "./codeKeyboard";
import { SetupRestoreAgent } from "./SetupRestoreAgent";
import { backupFailureDetails, backupPlaceUnavailable } from "../admin/scheduledBackupFailure";
import { BackupProgressLine } from "../admin/BackupProgress";
import { followSetupRestore } from "@/lib/setupRestoreFollow";

export { SetupBackupsOff } from "./SetupBackupsOff";

type Target = SetupRestorePlaceInput["target"];
type Phase = "place" | "restarting" | "manual";

const SYNC_SERVICE_NAMES: Record<SyncFolder["service"], string> = {
  "google-drive": "Google Drive", "icloud-drive": "iCloud Drive", dropbox: "Dropbox", onedrive: "OneDrive",
};
const RESTART_POLL_MS = 1_500;
const RESTART_LIMIT_MS = 5 * 60_000;

/**
 * "백업에서 복원" on a new server's setup (2026-11-06; from a dim link
 * under the name since 2026-11-19): a server whose
 * machine is gone comes back here without making a workspace first. The
 * person names where its backups are (a folder on this machine — on a
 * phone's own server one picked with the phone's picker — WebDAV or
 * S3-compatible storage); the place's continuous replica and snapshots show,
 * newest first, and the one chosen fills this server, which starts again on
 * it and then asks for the old sign-in. A server in a container names its
 * folder there, Leafen's image's backups folder (scheduled_backup_targets.go),
 * not "this computer". Beside it the restore can be left to an agent
 * (SetupRestoreAgent.tsx).
 */
export function SetupRestore({ setupCode, setupCodeNeeded, setSetupCode, onBack }: {
  setupCode: string;
  setupCodeNeeded: boolean;
  setSetupCode: (code: string) => void;
  onBack: () => void;
}) {
  const { t } = useTranslation("authGate");
  const [target, setTarget] = useState<Target>("folder");
  const [fields, setFields] = useState<Record<string, string>>({});
  const [pathStyle, setPathStyle] = useState(false);
  const [secret, setSecret] = useState("");
  const [place, setPlace] = useState<SetupRestorePlace | null>(null);
  const [choice, setChoice] = useState<SetupRestoreChoice | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [codeAsked, setCodeAsked] = useState(setupCodeNeeded);
  const [phase, setPhase] = useState<Phase>("place");
  const [byAgent, setByAgent] = useState(false);
  const [syncFolders, setSyncFolders] = useState<SyncFolder[]>([]);
  // A restore being put together: how far it has got, and why one stopped in the place's own words.
  const [restoreProgress, setRestoreProgress] = useState<AdministratorBackupProgress | null>(null);
  const [failureReason, setFailureReason] = useState("");
  const containerFolder = useSeenSetupStatus()?.containerFolder ?? "";
  const field = (name: string) => fields[name] ?? "";
  const change = (name: string, value: string) => {
    setFields((current) => ({ ...current, [name]: value }));
    setPlace(null);
    setChoice(null);
    setError("");
  };

  useEffect(() => {
    let live = true;
    void listSyncFolders().then((found) => { if (live) setSyncFolders(found); }, () => undefined);
    return () => { live = false; };
  }, []);

  // The server starts again on what was restored: once it says it is set up,
  // this browser signs in once with the restore's pass as the restored
  // server's owner (user decision 2026-11-07), and the page opens afresh — at
  // the sign-in when the pass doesn't take. A server started again by hand is
  // waited for the same way.
  useEffect(() => {
    if (phase === "place") return undefined;
    const deadline = phase === "restarting" ? Date.now() + RESTART_LIMIT_MS : Number.POSITIVE_INFINITY;
    let timer = 0;
    const open = () => {
      // The setup address would stay in the bar over the sign-in: the page opens at its start.
      const pass = takeSetupRestoreSignIn();
      if (!pass) {
        window.location.replace("/");
        return;
      }
      void api.setupRestoreSignIn(pass).then((session) => saveSession(session), () => false)
        .finally(() => window.location.replace("/"));
    };
    const look = () => {
      api.setupStatus().then((status) => {
        if (status.configured) open();
        else if (Date.now() >= deadline) window.location.replace("/");
        else timer = window.setTimeout(look, RESTART_POLL_MS);
      }, () => {
        if (Date.now() < deadline) timer = window.setTimeout(look, RESTART_POLL_MS);
      });
    };
    timer = window.setTimeout(look, RESTART_POLL_MS);
    return () => window.clearTimeout(timer);
  }, [phase]);

  const input = (): SetupRestorePlaceInput => ({
    ...(setupCode.trim() ? { setupCode: setupCode.trim() } : {}),
    target,
    ...(target === "folder" ? { folder: field("folder") } : {}),
    ...(target === "webdav" ? { webdavUrl: field("webdavUrl"), webdavUsername: field("webdavUsername"), webdavPassword: field("webdavPassword") } : {}),
    ...(target === "s3" ? {
      s3Endpoint: field("s3Endpoint"), s3Region: field("s3Region"), s3Bucket: field("s3Bucket"), s3Prefix: field("s3Prefix"),
      s3AccessKey: field("s3AccessKey"), s3SecretKey: field("s3SecretKey"), s3PathStyle: pathStyle,
    } : {}),
    // One field opens sealed backups, whichever of the two it is.
    ...(secret.trim() ? { code: secret.trim(), password: secret } : {}),
  });

  function failed(cause: unknown, operation: Parameters<typeof productErrorMessage>[1]) {
    if (cause instanceof LeafenApiError) {
      // A code missing is asked for; a wrong one says so (productErrorMessage). Either opens its field.
      if (cause.code === administratorErrorCodes.setupCodeRequired || cause.code === administratorErrorCodes.setupCodeInvalid) {
        setCodeAsked(true);
        if (cause.code === administratorErrorCodes.setupCodeRequired) return t("setupCodeRequired");
      }
      if (cause.code === administratorErrorCodes.setupClosed) return t("setupRestoreClosed");
      // A place missing, moved or not answering reads as one that can't be read, as before they had codes of their own.
      if (backupPlaceUnavailable(cause.code)) {
        if (target !== "folder") return t("setupRestorePlaceUnreadable");
        if (pickedFoldersOnly()) return t("setupRestoreFolderUnreadableDevice");
        return containerFolder ? t("setupRestoreFolderUnreadableContainer", { folder: containerFolder }) : t("setupRestoreFolderUnreadable");
      }
      if (cause.code === administratorErrorCodes.scheduledBackupInputInvalid) return t("setupRestorePlaceInvalid");
      if (cause.code === administratorErrorCodes.scheduledBackupSnapshotUnavailable
        || cause.code === administratorErrorCodes.replicaUnavailable) return t("setupRestoreUnavailable");
      // Data a restore can't take says why, and what to do instead.
      if (cause.code === administratorErrorCodes.dataBeforeSchemaBaseline) return t("setupRestoreEarlierLeafen");
      if (cause.code === administratorErrorCodes.dataFromNewerLeafen) return t("setupRestoreNewerLeafen");
    }
    return productErrorMessage(cause, operation);
  }

  async function find(event?: FormEvent) {
    event?.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      const found = await api.setupRestorePlace(input());
      setPlace(found);
      const first = found.replica.present && found.replica.readable ? "replica" : found.snapshots.find((item) => item.readable)?.name;
      setChoice(first ?? null);
    } catch (cause) {
      setError(failed(cause, "administratorScheduledBackup.read"));
    } finally {
      setBusy(false);
    }
  }

  async function restore() {
    if (busy || !choice) return;
    setBusy(true);
    setError("");
    setFailureReason("");
    try {
      const result = await api.setupRestore({ ...input(), choice });
      keepSetupRestoreSignIn(result.signIn);
      // Still being put together, it is followed here with its progress
      // (2026-11-22: a large backup was cut off at the 30-second answer limit).
      if (result.watch) {
        setRestoreProgress(result.progress ?? null);
        const followed = await followSetupRestore(result.watch, setRestoreProgress);
        setRestoreProgress(null);
        if (followed.failure) {
          setError(failed(new LeafenApiError(followed.failure.code, 422, "", followed.failure.message), "administratorScheduledBackup.restore"));
          setFailureReason(backupFailureDetails(followed.failure.reason, followed.failure.cause, followed.failure.code, t));
          return;
        }
      }
      await restored(result.restart);
    } catch (cause) {
      setError(failed(cause, "administratorScheduledBackup.restore"));
    } finally {
      setBusy(false);
    }
  }

  // The server starts again on what was restored: by itself, from the app's
  // window, or when the person starts it again.
  async function restored(restart: "automatic" | "manual") {
    if (restart === "automatic") {
      setPhase("restarting");
    } else if (canRestartPackagedServer()) {
      setPhase("restarting");
      await restartPackagedServer();
    } else {
      setPhase("manual");
    }
  }

  async function pickFolder() {
    const chosen = await chooseServerFolder(t("setupRestoreChooseFolder")).catch(() => null);
    if (chosen) change("folder", chosen);
  }

  const errorLine = error ? <p className={styles.error} role="alert" aria-live="assertive">{error}</p> : null;
  if (phase !== "place") {
    return (
      <div className={styles.form} data-setup-restore>
        <div className={styles.notice}>
          <strong>{t("setupRestoreTitle")}</strong>
          {/* Who comes in once it's back: this browser, or Leafen's own app (thisWindowWords.ts). */}
          <p role="status">{t(phase === "restarting"
            ? thisWindowKey("authGate:setupRestoreRestarting", "authGate:setupRestoreRestartingApp")
            : thisWindowKey("authGate:setupRestoreManual", "authGate:setupRestoreManualApp"))}</p>
        </div>
      </div>
    );
  }
  if (byAgent) {
    return <SetupRestoreAgent setupCode={setupCode} onBack={() => setByAgent(false)}
      onRestored={(restart) => void restored(restart).catch(() => setPhase("manual"))} />;
  }
  const picked = pickedFoldersOnly();
  const sealedLeft = Boolean(place && ((place.replica.present && !place.replica.readable) || place.snapshots.some((item) => !item.readable)));
  const opening = Boolean(place && sealedLeft && secret.trim());
  const time = (value: string) => (value ? displayDateTime(value) : "");
  const textField = (name: string, label: string, type = "text", placeholder?: string) => (
    <label className={own.field} key={name}>
      <span className={styles.label}>{label}</span>
      <input className={styles.input} type={type} value={field(name)} placeholder={placeholder} disabled={busy} autoComplete="off" spellCheck={false}
        onChange={(event) => change(name, event.target.value)} data-setup-restore-field={name} />
    </label>
  );
  return (
    <form className={styles.form} noValidate onSubmit={(event) => void find(event)} data-setup-restore>
      <div className={styles.notice}>
        <strong>{t("setupRestoreTitle")}</strong>
        <p>{t("setupRestoreBody")}</p>
      </div>
      <span className={styles.label}>{t("setupRestorePlace")}</span>
      <AppSelect aria-label={t("setupRestorePlace")} className={styles.choice} value={target} disabled={busy}
        onChange={(event) => { setTarget(event.target.value === "webdav" || event.target.value === "s3" ? event.target.value : "folder"); setPlace(null); setChoice(null); setError(""); }}>
        <option value="folder">{t(containerFolder ? "setupRestoreFolderContainer" : picked ? "setupRestoreFolderDevice" : "setupRestoreFolder")}</option>
        <option value="webdav">{t(BACKUP_PLACE_TARGET_LABELS.webdav)}</option>
        <option value="s3">{t(BACKUP_PLACE_TARGET_LABELS.s3)}</option>
      </AppSelect>
      {target === "folder" && picked ? (
        // A phone's folder is picked with its picker and shown by its name: there is no path to type.
        <>
          <button type="button" className={styles.secondary} disabled={busy} onClick={() => void pickFolder()}
            data-setup-restore-choose>{t("setupRestoreChooseFolder")}</button>
          <p className={styles.fieldHelp} data-setup-restore-device-folder>
            {deviceFolderName(field("folder")) ? t("setupRestoreFolderPicked", { folder: deviceFolderName(field("folder")) })
              : t("setupRestoreNoFolderPicked")}
          </p>
          <p className={styles.fieldHelp}>{t("setupRestoreFolderHelpDevice")}</p>
        </>
      ) : target === "folder" ? (
        <>
          {syncFolders.length ? (
            <div className={own.syncFolders}>
              {syncFolders.map((folder) => (
                <button key={folder.path} type="button" className={own.row} disabled={busy} data-chosen={field("folder") === folder.path || undefined}
                  onClick={() => change("folder", folder.path)} data-setup-restore-sync-folder={folder.service}>
                  <span>{SYNC_SERVICE_NAMES[folder.service]}{folder.account ? ` · ${folder.account}` : ""}</span>
                  <small>{folder.path}</small>
                </button>
              ))}
            </div>
          ) : null}
          {textField("folder", t("setupRestoreFolderPath"), "text", containerFolder || undefined)}
          <p className={styles.fieldHelp}>{containerFolder ? t("setupRestoreFolderHelpContainer", { folder: containerFolder }) : t("setupRestoreFolderHelp")}</p>
          {canChooseServerFolder() ? (
            <button type="button" className={styles.secondary} disabled={busy} onClick={() => void pickFolder()}>{t("setupRestoreChooseFolder")}</button>
          ) : null}
        </>
      ) : (
        // A WebDAV server's or storage's fields, named and in order as Settings asks for them (lib/backupPlaceFields.ts).
        <>
          {BACKUP_PLACE_FIELDS[target].map((place) => textField(place.name, t(place.label), place.type, backupPlacePlaceholder(place, t)))}
          {target === "s3" ? (
            <SettingSwitch title={t(BACKUP_PLACE_PATH_STYLE)} on={pathStyle} disabled={busy}
              onChange={(on) => { setPathStyle(on); setPlace(null); setChoice(null); setError(""); }} control={{ "data-setup-restore-path-style": "" }} />
          ) : null}
        </>
      )}
      {codeAsked ? (
        <label className={own.field}>
          <span className={styles.label}>{t("setupCode")}</span>
          <input className={styles.input} type="text" value={setupCode} disabled={busy} autoComplete="one-time-code" {...SETUP_CODE_KEYBOARD}
            onChange={(event) => { setSetupCode(event.target.value); setError(""); }} data-setup-restore-code />
        </label>
      ) : null}
      {place ? (
        <div className={own.found} data-setup-restore-found>
          {!place.replica.present && !place.snapshots.length ? <p className={styles.fieldHelp}>{t("setupRestoreNone")}</p> : null}
          {place.replica.present ? (
            <button type="button" className={own.row} disabled={busy || !place.replica.readable} aria-pressed={choice === "replica"}
              data-chosen={choice === "replica" || undefined} onClick={() => setChoice("replica")} data-setup-restore-choice="replica">
              <span>{place.replica.ownerOrigin
                ? t("setupRestoreReplicaOf", { address: place.replica.ownerOrigin }) : t("setupRestoreReplica")}</span>
              <small>{place.replica.readable ? t("setupRestoreLastChange", { time: time(place.replica.lastChangeAt) }) : t("setupRestoreUnreadable")}</small>
            </button>
          ) : null}
          {place.snapshots.map((snapshot) => (
            <button key={snapshot.name} type="button" className={own.row} disabled={busy || !snapshot.readable} aria-pressed={choice === snapshot.name}
              data-chosen={choice === snapshot.name || undefined} onClick={() => setChoice(snapshot.name)} data-setup-restore-choice={snapshot.name}>
              <span>{snapshot.readable ? time(snapshot.createdAt) || snapshot.name : snapshot.name}</span>
              <small>{snapshot.readable ? t("setupRestoreSnapshot") : t("setupRestoreUnreadable")}</small>
            </button>
          ))}
          {sealedLeft ? (
            <>
              <p className={styles.fieldHelp}>{t("setupRestoreSealed")}</p>
              <label className={own.field}>
                <span className={styles.label}>{t("setupRestoreSecret")}</span>
                <input className={styles.input} type="password" value={secret} disabled={busy} autoComplete="off"
                  onChange={(event) => { setSecret(event.target.value); setError(""); }} data-setup-restore-secret />
              </label>
            </>
          ) : null}
        </div>
      ) : null}
      {errorLine}
      {error && failureReason ? <p className={styles.fieldHelp} data-setup-restore-reason>{failureReason}</p> : null}
      {restoreProgress ? <BackupProgressLine progress={restoreProgress} /> : null}
      {place && choice ? (
        <button className={styles.primary} type="button" disabled={busy} onClick={() => void restore()} data-setup-restore-confirm>
          {busy ? t("setupRestoreWorking") : t("setupRestoreConfirm")}
        </button>
      ) : null}
      {/* One main button at a time: once a backup is chosen it is "복원", and looking
          again shows only to open sealed ones or when nothing here can be chosen —
          changing a field already clears what was found. */}
      {opening || !place || !choice ? (
        <button className={place && choice ? styles.secondary : styles.primary} type="submit" disabled={busy} data-setup-restore-find>
          {busy && !place ? t("setupRestoreFinding") : opening ? t("setupRestoreOpenSealed")
            : place ? t("setupRestoreFindAgain") : t("setupRestoreFind")}
        </button>
      ) : null}
      <div className={styles.otherWay}>
        <button className={styles.outlined} type="button" disabled={busy} onClick={() => { setError(""); setByAgent(true); }}
          aria-describedby="setup-restore-agent-note" data-setup-restore-agent-open>{t("setupRestoreAgentOpen")}</button>
        <p id="setup-restore-agent-note" className={styles.otherWayNote}>{t("setupRestoreAgentOpenNote")}</p>
      </div>
      <button className={styles.linkButton} type="button" onClick={onBack} disabled={busy} data-setup-restore-back>{t("setupRestoreBack")}</button>
    </form>
  );
}
