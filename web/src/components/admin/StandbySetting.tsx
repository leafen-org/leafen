import { useEffect, useRef, useState } from "react";
import { useAttemptNotice } from "@/lib/useNoticeFor";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { useTranslation } from "react-i18next";
import { api } from "../../api";
import { administratorErrorCodes, type AdministratorScheduledBackup as Schedule } from "../../administratorWire.generated";
import { LeafenApiError } from "../../apiTransport";
import { displayDateTime } from "@/lib/displayDate";
import { followAcrossTabs, tellTabs } from "@/lib/followAcrossTabs";
import { productErrorMessage } from "../../lib/productErrorPresentation";
import type { Session } from "../../types";
import { announceScheduledBackupSaved } from "./BackupFailureNotice";
import { replicationFailureText } from "./scheduledBackupFailure";
import styles from "./AdministratorSettingsPage.module.css";
import { SettingRow, SettingSwitch } from "./AdministratorSettingRows";
import dataStyles from "./AdministratorDataManagement.module.css";
import { useCopyNotice } from "@/lib/copyNotice";

// While a pair runs, its state is read again this often.
const STATUS_MS = 5000;
// A beat this late is said; those between beats are not (2026-11-04: the
// standby went between "following" and "silent for 1 second" as it was read).
const SILENT_SHOWN_SECONDS = 15;

type Translate = (key: string, options?: Record<string, unknown>) => string;

/**
 * A standby server (2026-10-17): on a replicating primary, adding a standby
 * gives a connection code and then shows the standby's beat; on another
 * server, pasting that code makes it follow the primary, take over by itself
 * when the primary stops answering, or now; and a server that took over says
 * when and from whom. A server with no outside address is named by its part,
 * "the primary" or "standby server", not by a stand-in for an address
 * (2026-11-11: "(no address)" stood in the sentences and as the row's name).
 */
export function StandbySetting({ session, schedule, onSchedule }: {
  session: Session;
  schedule: Schedule;
  onSchedule: (schedule: Schedule) => void;
}) {
  const { t } = useTranslation("serverConsole");
  const copy = useCopyNotice();
  const [busy, setBusy] = useState(false);
  const [code, setCode] = useState("");
  // A code made again starts like the one before; a line says it changed.
  const [remade, setRemade] = useState(false);
  const [pasted, setPasted] = useState("");
  const [error, setError] = useAttemptNotice<string>(pasted);
  const mounted = useRef(true);
  const standby = schedule.standby;
  const replicating = schedule.replication.enabled && schedule.replication.state !== "off" && schedule.replication.state !== "moved";

  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);

  // The pair's state follows while there is one: one of the person's tabs in
  // sight asks for all of them, the backup band's among them, and a tab
  // behind asks nothing (followAcrossTabs).
  useEffect(() => {
    if (standby.role === "none" && !code) return undefined;
    const controller = new AbortController();
    const topic = `scheduled-backup:${session.user.id}`;
    const stop = followAcrossTabs(topic, STATUS_MS,
      () => api.scheduledBackup(session, controller.signal).then((result) => tellTabs(topic, result.schedule)),
      (value) => { if (mounted.current && value && typeof value === "object") onSchedule(value as Schedule); });
    return () => {
      controller.abort();
      stop();
    };
  }, [standby.role, code, session, onSchedule]);

  async function run(action: () => Promise<{ schedule: Schedule }>) {
    setBusy(true);
    setError("");
    try {
      const result = await action();
      if (!mounted.current) return;
      onSchedule(result.schedule);
      announceScheduledBackupSaved();
    } catch (cause) {
      if (mounted.current) setError(standbyMessage(cause, t));
    } finally {
      if (mounted.current) setBusy(false);
    }
  }

  // Taking over asks first in the common confirmation, which says what
  // follows, as every settings action that cannot be undone in one step
  // does; focus goes back to "지금 전환".
  async function takeOver() {
    if (!await confirmAction({ message: t("standby.takeOverQuestion"), detail: t("standby.takeOverWarning"),
      confirmLabel: t("standby.takeOverConfirm"), danger: true })) return;
    await run(() => api.takeOverNow(session));
  }

  async function addStandby() {
    setBusy(true);
    setError("");
    try {
      const before = code;
      const result = await api.createStandbyCode(session);
      if (mounted.current) {
        setCode(result.code);
        setRemade(Boolean(before));
      }
    } catch (cause) {
      if (mounted.current) setError(standbyMessage(cause, t));
    } finally {
      if (mounted.current) setBusy(false);
    }
  }

  const tookOver = standby.tookOverAt ? (
    <p className={styles.emailProviderMeta} role="status" data-standby-took-over>
      {standby.tookOverFrom ? t("standby.tookOver", { time: formatTime(standby.tookOverAt), address: standby.tookOverFrom })
        : t("standby.tookOverPrimary", { time: formatTime(standby.tookOverAt) })}
    </p>
  ) : null;
  const errorLine = error ? <div id="standby-error" className={dataStyles.live} data-error="true" role="alert">{error}</div> : null;

  if (standby.role === "standby") {
    return (
      <div className={styles.emailProviderBlock} data-standby="standby" tabIndex={-1}>
        <p className={styles.emailProviderMeta}>{standby.peerOrigin ? t("standby.following", { address: standby.peerOrigin }) : t("standby.followingPlace")}</p>
        <div className={dataStyles.summary}>
          <div className={dataStyles.summaryRow}>
            <span>{t("standby.primaryBeat")}</span>
            <strong>{standby.takingOver ? t("standby.takingOver")
              // A place with no primary's replica it can open says so, as joining does.
              : standby.errorCode === administratorErrorCodes.replicaUnavailable ? t("standby.replicaUnavailable")
              // A primary's data this version doesn't open isn't taken over (the server says so as it refuses).
              : standby.errorCode === administratorErrorCodes.dataBeforeSchemaBaseline ? t("standby.earlierLeafen")
              : standby.errorCode ? replicationFailureText(standby.errorCode, schedule.target, t)
                : standby.peerSilentSeconds > SILENT_SHOWN_SECONDS ? t("standby.silentSeconds", { count: standby.peerSilentSeconds }) : t("standby.beating")}</strong>
          </div>
          <div className={dataStyles.summaryRow}>
            <span>{t("standby.followedLabel")}</span>
            <strong>{standby.followedAt ? t("standby.followedAt", { time: formatTime(standby.followedAt) }) : t("standby.starting")}</strong>
          </div>
          {directRow(standby.directCheck, t)}
          {standby.peerVersion ? (
            <div className={dataStyles.summaryRow}>
              <span>{t("standby.versions")}</span>
              <strong>{t("standby.versionPair", { primary: standby.peerVersion, standby: standby.version })}</strong>
            </div>
          ) : null}
        </div>
        <SettingSwitch title={t("standby.auto")} on={standby.autoTakeover} disabled={busy || standby.takingOver}
          onChange={(on) => void run(() => api.setStandbyAutoTakeover(session, on))} control={{ "data-standby-auto": "" }} />
        <div className={dataStyles.actions}>
          <button type="button" className={styles.secondaryButton} disabled={busy || standby.takingOver}
            onClick={() => void takeOver()} data-standby-takeover>
            {t("standby.takeOver")}
          </button>
          <button type="button" className={styles.secondaryButton} disabled={busy || standby.takingOver}
            onClick={() => void run(() => api.leaveStandby(session))} data-standby-leave>
            {t("standby.leave")}
          </button>
        </div>
        <p className={styles.emailProviderMeta}>{t("standby.help")}</p>
        {errorLine}
      </div>
    );
  }

  if (replicating) {
    return (
      <div className={styles.emailProviderBlock} data-standby={standby.role} tabIndex={-1}>
        {tookOver}
        {standby.role === "primary" ? (
          <>
            <div className={dataStyles.summary}>
              <div className={dataStyles.summaryRow}>
                <span>{standby.peerOrigin || t("standby.standbyRow")}</span>
                <strong>{standby.peerSilentSeconds >= 300 ? t("standby.gone")
                  : standby.peerSilentSeconds > SILENT_SHOWN_SECONDS ? t("standby.silentSeconds", { count: standby.peerSilentSeconds }) : t("standby.beating")}</strong>
              </div>
              {directRow(standby.directCheck, t)}
              {standby.peerVersion && standby.peerVersion !== standby.version ? (
                <div className={dataStyles.summaryRow}>
                  <span>{t("standby.versions")}</span>
                  <strong>{t("standby.versionPair", { primary: standby.version, standby: standby.peerVersion })}</strong>
                </div>
              ) : null}
            </div>
            <div className={dataStyles.actions}>
              <button type="button" className={styles.secondaryButton} disabled={busy}
                onClick={() => void run(() => api.dismissStandby(session))} data-standby-dismiss>
                {t("standby.dismiss")}
              </button>
            </div>
            <p className={styles.emailProviderMeta}>{t(standby.paused ? "standby.paused" : "standby.primaryHelp")}</p>
          </>
        ) : code ? (
          <div className={styles.tunnelFields} data-standby-code>
            <label>
              <span>{t("standby.codeLabel")}</span>
              <textarea readOnly rows={3} value={code} onFocus={(event) => event.currentTarget.select()} />
            </label>
            <div className={styles.sectionActions}>
              <button type="button" className={styles.secondaryButton} onClick={() => void copy(code)}>
                {t("standby.copy")}
              </button>
              {/* A code that went astray is closed by making another (replica_pair_check.go). */}
              <button type="button" className={styles.secondaryButton} disabled={busy} onClick={() => void addStandby()} data-standby-new-code>
                {t("standby.newCode")}
              </button>
            </div>
            {remade ? <p className={styles.settingNote} role="status" data-standby-new-code-made>{t("standby.newCodeMade")}</p> : null}
            <p className={styles.emailProviderMeta}>{t("standby.codeHelp")}</p>
          </div>
        ) : (
          <SettingRow title={t("standby.add")} description={t("standby.addHelp")}>
            <button type="button" className={styles.secondaryButton} disabled={busy} onClick={() => void addStandby()} data-standby-add>
              {t("standby.addAction")}
            </button>
          </SettingRow>
        )}
        {errorLine}
      </div>
    );
  }

  return (
    <div className={styles.emailProviderBlock} data-standby="none" tabIndex={-1}>
      {/* Its own standby comes with the replica; what follows is for being another's. */}
      {schedule.enabled && !schedule.replication.enabled ? <p className={styles.settingNote} data-standby-needs-replication>{t("standby.addNeedsReplication")}</p> : null}
      <div className={styles.organizationSubhead}><span>{t("standby.joinTitle")}</span></div>
      {tookOver}
      <div className={styles.tunnelFields}>
        <label>
          <span>{t("standby.joinLabel")}</span>
          <textarea rows={3} value={pasted} disabled={busy} placeholder="leafen-standby-1:…"
            onChange={(event) => setPasted(event.target.value)} data-standby-join-code
            {...(error === t("standby.codeInvalid") ? { "aria-invalid": true as const, "aria-describedby": "standby-error" } : {})} />
        </label>
        <div>
          <button type="button" className={styles.secondaryButton} disabled={busy || !pasted.trim()}
            onClick={() => void run(() => api.joinAsStandby(session, pasted.trim()))} data-standby-join>
            {t("standby.join")}
          </button>
        </div>
      </div>
      <p className={styles.emailProviderMeta}>{t("standby.joinHelp")}</p>
      {errorLine}
    </div>
  );
}

// directRow says whether the pair reached each other directly the last time
// one's beat was late; without that, the backup place alone judges.
function directRow(direct: string, t: Translate) {
  if (direct !== "reached" && direct !== "unreachable" && direct !== "none") return null;
  return (
    <div className={dataStyles.summaryRow} data-standby-direct={direct}>
      <span>{t("standby.directLabel")}</span>
      <strong>{t(`standby.direct.${direct}`)}</strong>
    </div>
  );
}

function formatTime(value: string) {
  // The screen's own way of writing a moment, as everywhere else (lib/displayDate).
  return displayDateTime(value) || value;
}

function standbyMessage(error: unknown, t: Translate) {
  if (error instanceof LeafenApiError) {
    if (error.code === administratorErrorCodes.standbyCodeInvalid) return t("standby.codeInvalid");
    if (error.code === administratorErrorCodes.standbyNotFresh) return t("standby.notFresh");
    if (error.code === administratorErrorCodes.standbyVersionOlder) return t("standby.versionOlder");
    if (error.code === administratorErrorCodes.standbyPlaceSlow) return t("standby.placeSlow");
    if (error.code === administratorErrorCodes.replicaUnavailable) return t("standby.replicaUnavailable");
    if (error.code === administratorErrorCodes.scheduledBackupFolderUnavailable) return t("replication.unreachable");
  }
  return productErrorMessage(error, "administratorScheduledBackup.save");
}
