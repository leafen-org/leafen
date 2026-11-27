"use client";
import { useAttemptNotice } from "@/lib/useNoticeFor";

import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../api";
import {
  type AdministratorScheduledBackup as Schedule,
  type AdministratorScheduledBackupInput as ScheduleInput,
} from "../../administratorWire.generated";
import { productErrorMessage } from "../../lib/productErrorPresentation";
import { AdministratorBackupRestore } from "./AdministratorBackupRestore";
import { ReplicationSetting } from "./ReplicationSetting";
import { StandbySetting } from "./StandbySetting";
import { announceScheduledBackupSaved } from "./BackupFailureNotice";
import { useSettingsUnsaved } from "../settings/settingsUnsaved";
import { ScheduledBackupSyncFolders } from "./ScheduledBackupSyncFolders";
import { ScheduledBackupFolderField } from "./ScheduledBackupFolderField";
import { ScheduledBackupPlaceFields } from "./ScheduledBackupPlaceFields";
import { useRefusedFieldFocus } from "./useRefusedFieldFocus";
import { BackupProtection } from "./BackupProtection";
import { ScheduledBackupStatus } from "./ScheduledBackupStatus";
import { protectionRequiredText, scheduleMessage, scheduledBackupFolderWords } from "./scheduledBackupFolderWords";
import { backupRefusalDetails } from "./scheduledBackupFailure";
import type { Session } from "../../types";
import styles from "./AdministratorSettingsPage.module.css";
import dataStyles from "./AdministratorDataManagement.module.css";
import { AppSelect } from "@/components/AppSelect";
import { SettingField, SettingRow, SettingSwitch } from "./AdministratorSettingRows";
import rowStyles from "./AdministratorSettingRows.module.css";
import { ReadFailed } from "../ReadFailed";
import { SettingsSaveBar } from "./SettingsSaveBar";

// How often a schedule can run, in hours; a server set otherwise (by its
// environment) shows its own as one more choice.
const INTERVALS = [6, 12, 24, 168];
// While a backup runs the screen looks again this often, for as long as it
// runs; one asked for is waited for this long to start (2026-11-22: a
// 46-minute first backup was left at "백업하는 중" after 10 minutes).
const POLL_MS = 2000, POLL_LIMIT_MS = 10 * 60 * 1000;

const draftOf = (schedule: Schedule): ScheduleInput => ({
  revision: schedule.revision, enabled: schedule.enabled, target: schedule.target, folder: schedule.folder,
  webdavUrl: schedule.webdavUrl, webdavUsername: schedule.webdavUsername, webdavPassword: "", clearWebdavPassword: false,
  s3Endpoint: schedule.s3Endpoint, s3Region: schedule.s3Region, s3Bucket: schedule.s3Bucket, s3Prefix: schedule.s3Prefix,
  s3AccessKey: schedule.s3AccessKey, s3SecretKey: "", clearS3Secret: false, s3PathStyle: schedule.s3PathStyle,
  intervalHours: schedule.intervalHours, keep: schedule.keep, password: "", clearPassword: false,
});

const sameDraft = (left: ScheduleInput, right: ScheduleInput) =>
  left.enabled === right.enabled && left.target === right.target && left.folder.trim() === right.folder.trim() &&
  left.webdavUrl.trim() === right.webdavUrl.trim() && left.webdavUsername.trim() === right.webdavUsername.trim() &&
  left.webdavPassword === right.webdavPassword && left.clearWebdavPassword === right.clearWebdavPassword &&
  left.s3Endpoint.trim() === right.s3Endpoint.trim() && left.s3Region.trim() === right.s3Region.trim() &&
  left.s3Bucket.trim() === right.s3Bucket.trim() && left.s3Prefix.trim() === right.s3Prefix.trim() &&
  left.s3AccessKey.trim() === right.s3AccessKey.trim() && left.s3SecretKey === right.s3SecretKey &&
  left.clearS3Secret === right.clearS3Secret && left.s3PathStyle === right.s3PathStyle &&
  left.intervalHours === right.intervalHours && left.keep === right.keep &&
  left.password === right.password && left.clearPassword === right.clearPassword;

// Where backups go is named: the folder, the WebDAV server's address, or
// the S3 storage's endpoint and bucket.
const backupPlaceSet = (schedule: Pick<Schedule, "target" | "folder" | "webdavUrl" | "s3Endpoint" | "s3Bucket">) => {
  if (schedule.target === "webdav") return Boolean(schedule.webdavUrl.trim());
  if (schedule.target === "s3") return Boolean(schedule.s3Endpoint.trim() && schedule.s3Bucket.trim());
  return Boolean(schedule.folder.trim());
};

/**
 * The server's own backups to a folder, a WebDAV server or S3-compatible
 * storage (user decision 2026-10-03): on or off, where (a folder — picked
 * with the system's dialog in the desktop app on its own server, and only
 * picked on a phone's own server — a WebDAV server's address, user name and
 * password, or the storage's endpoint, region, bucket, prefix and keys), how
 * often, how many to keep, how the last went, and a backup now. Set by the
 * server's environment, it only shows them.
 */
export function AdministratorScheduledBackup({ session, part = "backup", onSelectSection }: {
  session: Session;
  /** Which of the server's pages it draws: the schedule, the standby, or restoring. */
  part?: "backup" | "standby" | "restore";
  onSelectSection?: (section: "data" | "standby" | "restore") => void;
}) {
  const { t } = useTranslation("serverConsole");
  const [schedule, setSchedule] = useState<Schedule | null>(null);
  const [draft, setDraft] = useState<ScheduleInput | null>(null);
  const [busy, setBusy] = useState<"save" | "run" | "write-here" | null>(null);
  const [message, setMessage] = useState("");
  const [error, setError] = useAttemptNotice<string>(draft);
  // The field a refused place is about, marked with the status line (rejected-entry focus).
  const refused = (...lines: string[]) => error && lines.includes(error)
    ? { "aria-invalid": true as const, "aria-describedby": "scheduled-backup-status" } : {};
  // What the place answered and its common cause, beside the refusal it came with.
  const [why, setWhy] = useState({ error: "", text: "" });
  const [polling, setPolling] = useState(false);
  const [pendingFolder, setPendingFolder] = useState("");
  // A save refused for want of a way to open the backups waits for the code
  // made from beside the refusal, then goes on (2026-11-06: the refusal said
  // what was missing with the button to make it out of sight below).
  const [savePending, setSavePending] = useState(false);
  // What "지금 백업" brought says so on its row, in sight of the button; a save says so in its bar.
  const [ranHere, setRanHere] = useState(false);
  const mounted = useRef(true);
  const [readTurn, setReadTurn] = useState(0);
  // A refused save, from whichever press, hands the focus to the field it marks (useRefusedFieldFocus).
  const scheduleRef = useRef<HTMLElement>(null), [refusedTurn, setRefusedTurn] = useState(0);
  useRefusedFieldFocus(scheduleRef, refusedTurn);

  useEffect(() => {
    mounted.current = true;
    const controller = new AbortController();
    api.scheduledBackup(session, controller.signal)
      .then((result) => {
        setSchedule(result.schedule);
        setDraft(draftOf(result.schedule));
        if (result.schedule.running) setPolling(true);
      })
      .catch((cause) => {
        if (!controller.signal.aborted) setError(productErrorMessage(cause, "administratorScheduledBackup.read"));
      });
    return () => {
      mounted.current = false;
      controller.abort();
    };
  }, [readTurn, session]);

  // A server's part in a pair changing folds or unfolds what's above it (the
  // schedule goes while it follows another), and takes the button pressed:
  // where it says what's so comes into view, with the focus (2026-11-05: joining
  // left the window at the section below it, the focus in the search box).
  const pairRole = schedule?.standby.role ?? null;
  const pairRoleSeen = useRef(pairRole);
  useEffect(() => {
    const was = pairRoleSeen.current;
    pairRoleSeen.current = pairRole;
    if (!was || !pairRole || was === pairRole) return;
    const place = document.querySelector<HTMLElement>("[data-standby]");
    place?.scrollIntoView({ block: "start" });
    place?.focus({ preventScroll: true });
  }, [pairRole]);

  // A backup asked for, or one going, is followed until it finishes.
  useEffect(() => {
    if (!polling || !schedule) return undefined;
    const startedFrom = schedule.lastFinishedAt;
    let deadline = Date.now() + POLL_LIMIT_MS;
    const controller = new AbortController();
    let timer = 0;
    const look = () => {
      api.scheduledBackup(session, controller.signal).then((result) => {
        if (!mounted.current) return;
        setSchedule(result.schedule);
        if (result.schedule.running) deadline = Date.now() + POLL_LIMIT_MS;
        const finished = !result.schedule.running && result.schedule.lastFinishedAt !== startedFrom;
        if (finished || Date.now() > deadline) {
          setPolling(false);
          // A run that failed says so on its row; "시작했습니다" goes with it.
          if (finished) setMessage(result.schedule.lastErrorCode ? "" : t("schedule.done"));
          // The band at the top follows the run's outcome at once
          // (2026-10-16: a success kept the band about failures in a row).
          if (finished) announceScheduledBackupSaved();
          return;
        }
        timer = window.setTimeout(look, POLL_MS);
      }).catch(() => {
        if (!controller.signal.aborted && Date.now() <= deadline) timer = window.setTimeout(look, POLL_MS);
      });
    };
    timer = window.setTimeout(look, POLL_MS);
    return () => {
      controller.abort();
      window.clearTimeout(timer);
    };
    // The run is followed from the schedule seen when it was asked for.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [polling, session]);

  // A run its schedule starts while this is open is followed the same way
  // (2026-11-06: the 2:28:19 backup ran, and the screen still said it was next).
  const nextRun = schedule?.enabled && !schedule.running && !polling && schedule.nextRunAt ? Date.parse(schedule.nextRunAt) : NaN;
  useEffect(() => {
    if (!Number.isFinite(nextRun)) return undefined;
    const wait = nextRun - Date.now() + 1000;
    if (wait > 24 * 60 * 60 * 1000) return undefined;
    const timer = window.setTimeout(() => setPolling(true), Math.max(0, wait));
    return () => window.clearTimeout(timer);
  }, [nextRun]);
  // A place or a period changed and not saved is asked about before it goes,
  // as the server's other settings are (2026-11-06: a folder typed here went
  // without a word when the section changed or the window closed).
  useSettingsUnsaved(Boolean(schedule && draft && !sameDraft(draft, draftOf(schedule))));

  if (!schedule || !draft) {
    return (
      <section className={styles.section} aria-labelledby="administrator-schedule-title">
        <div id="administrator-schedule-title" className={styles.sectionTitle}>{t("schedule.title")}</div>
        {error ? <ReadFailed message={error} onRetry={() => { setError(""); setReadTurn((turn) => turn + 1); }} /> : <div className={dataStyles.live} role="status">{t("schedule.loading")}</div>}
      </section>
    );
  }

  const fixed = schedule.setByEnvironment;
  const changed = !sameDraft(draft, draftOf(schedule));

  const intervals = INTERVALS.includes(draft.intervalHours) ? INTERVALS : [...INTERVALS, draft.intervalHours];
  const change = (next: Partial<ScheduleInput>) => {
    setMessage("");
    setError("");
    // On what is there now, not this render's copy: a key pressed before the
    // last one was drawn kept its letter (2026-11-05: typing a long folder fast lost one).
    setDraft((current) => current ? { ...current, ...next } : current);
  };

  async function save(from: ScheduleInput | null = draft) {
    if (!from || busy) return;
    setRanHere(false);
    setBusy("save");
    setMessage("");
    setError("");
    try {
      const result = await api.saveScheduledBackup(session, {
        ...from, folder: from.folder.trim(), webdavUrl: from.webdavUrl.trim(), webdavUsername: from.webdavUsername.trim(),
        s3Endpoint: from.s3Endpoint.trim(), s3Region: from.s3Region.trim(), s3Bucket: from.s3Bucket.trim(),
        s3Prefix: from.s3Prefix.trim(), s3AccessKey: from.s3AccessKey.trim(),
      });
      if (!mounted.current) return;
      setSchedule(result.schedule);
      setDraft(draftOf(result.schedule));
      // Turned on or sent elsewhere, a backup starts right away, as the answer says.
      setMessage(result.backupStarted ? t("schedule.savedStarted") : t("schedule.saved"));
      announceScheduledBackupSaved();
      if (result.backupStarted) setPolling(true);
    } catch (cause) {
      if (!mounted.current) return;
      const said = scheduleMessage(cause, t, from.folder, schedule?.containerFolder);
      setError(said); setWhy({ error: said, text: backupRefusalDetails(cause, t) }); setRefusedTurn((turn) => turn + 1);
    } finally {
      if (mounted.current) setBusy(null);
    }
  }

  async function runNow() {
    if (busy) return;
    setRanHere(true);
    setBusy("run");
    setMessage("");
    setError("");
    try {
      const result = await api.runScheduledBackup(session);
      if (!mounted.current) return;
      setSchedule(result.schedule);
      // One already running starts nothing more; this one is followed.
      setMessage(result.alreadyRunning ? t("schedule.alreadyRunning") : t("schedule.requested"));
      setPolling(true);
    } catch (cause) {
      if (mounted.current) setError(scheduleMessage(cause, t));
    } finally {
      if (mounted.current) setBusy(null);
    }
  }

  // A place whose backups were opened with another server's code or password
  // takes this server's backups only once the person says so.
  async function writeHere() {
    if (busy) return;
    setRanHere(true);
    setBusy("write-here");
    setMessage("");
    setError("");
    try {
      const result = await api.writeBackupsHere(session);
      if (!mounted.current) return;
      keepProtection(result.schedule);
      if (result.schedule.enabled) setPolling(true);
    } catch (cause) {
      if (mounted.current) setError(scheduleMessage(cause, t));
    } finally {
      if (mounted.current) setBusy(null);
    }
  }

  // A cloud folder picked backs up there every day, keeping the last seven,
  // with the rest of the saved schedule as it was; with nothing yet to open
  // the backups, a recovery code is made and saved first (BackupProtection).
  function backUpToSyncFolder(folder: string) {
    if (!schedule) return;
    if (!schedule.protection.code && !schedule.protection.password) {
      setPendingFolder(folder);
      return;
    }
    void save({ ...draftOf(schedule), enabled: true, target: "folder", folder, intervalHours: 24, keep: 7 });
  }

  // A change of what opens the backups moves the schedule's revision only;
  // what is being typed above stays.
  function keepProtection(next: Schedule) {
    setSchedule(next);
    setDraft((current) => current ? { ...current, revision: next.revision } : draftOf(next));
  }

  function codeSavedForFolder() {
    const folder = pendingFolder;
    setPendingFolder("");
    if (folder && schedule) void save({ ...draftOf(schedule), enabled: true, target: "folder", folder, intervalHours: 24, keep: 7 });
    else if (savePending) {
      setSavePending(false);
      void save();
    }
  }

  const running = schedule.running || polling;
  const canRun = schedule.enabled && backupPlaceSet(schedule) && !changed && !running && !busy;
  const webdav = draft.target === "webdav";
  const s3 = draft.target === "s3";
  const insecure = (webdav && /^http:\/\//iu.test(draft.webdavUrl.trim())) || (s3 && /^http:\/\//iu.test(draft.s3Endpoint.trim()));
  // Under the address it is about (2026-11-06: under the last field it read
  // as the path-style switch's own line).
  const insecureNote = insecure ? (
    <span className={styles.settingNote} role="note" data-scheduled-backup-insecure>
      {s3 ? t("schedule.s3Insecure") : t("schedule.webdavInsecure")}
    </span>
  ) : null;
  const off = fixed || Boolean(busy);
  const standbyRole = schedule.standby.role === "standby";
  // Where a page needs the backups' place and there is none yet: one line, and the way to it.
  const needsPlace = (text: string) => (
    <section className={styles.section} aria-labelledby="administrator-schedule-needed-title" data-backup-place-needed>
      <div id="administrator-schedule-needed-title" className={styles.sectionTitle}>{t(part === "restore" ? "restore.title" : "replication.title")}</div>
      <div className={styles.settingRows}>
        <SettingRow title={text}>
          {onSelectSection ? (
            <button type="button" className={styles.secondaryButton} onClick={() => onSelectSection("data")}>{t("schedule.openBackups")}</button>
          ) : <span />}
        </SettingRow>
      </div>
    </section>
  );

  // 설정 › 서버 › 예비 서버: the replica, then the pair. The page's own title names the pair, so its section
  // has no title of its own; only the replica's "연속 복제" has one.
  if (part === "standby") {
    // A standby follows another server's backups; its own schedule waits.
    if (standbyRole) {
      return (
        <section className={styles.section} aria-label={t("standby.title")} data-scheduled-backup>
          <StandbySetting session={session} schedule={schedule} onSchedule={setSchedule} />
        </section>
      );
    }
    const replicating = (schedule.enabled && backupPlaceSet(schedule)) || (schedule.replication.state === "moved" && schedule.replication.enabled);
    return (
      <>
        {replicating ? (
          <section className={styles.section} aria-labelledby="administrator-replication-title" data-scheduled-backup>
            <div id="administrator-replication-title" className={styles.sectionTitle}>{t("replication.title")}</div>
            <ReplicationSetting session={session} schedule={schedule} onSchedule={setSchedule}
              onOpenRestore={onSelectSection ? () => onSelectSection("restore") : undefined} />
          </section>
        ) : needsPlace(t("replication.needsBackup"))}
        {/* A moved server may become the standby of the server that took its data (an old primary goes down to standby). */}
        <section className={styles.section} aria-label={t("standby.title")} data-scheduled-backup>
          <StandbySetting session={session} schedule={schedule} onSchedule={setSchedule} />
        </section>
      </>
    );
  }
  // 설정 › 서버 › 복원 및 옮기기: the backups kept, to go back to one.
  if (part === "restore") {
    if (standbyRole) return null;
    return backupPlaceSet(schedule)
      ? <AdministratorBackupRestore session={session} schedule={schedule} onSchedule={setSchedule} />
      : needsPlace(t("restore.needsBackup"));
  }
  if (standbyRole) {
    return (
      <section className={styles.section} aria-labelledby="administrator-schedule-title" data-scheduled-backup data-settings-part="scheduled-backup">
        <div id="administrator-schedule-title" className={styles.sectionTitle}>{t("schedule.title")}</div>
        <div className={styles.settingRows}>
          <SettingRow title={t("schedule.followsPrimary")}>
            {onSelectSection ? <button type="button" className={styles.secondaryButton} onClick={() => onSelectSection("standby")}>{t("schedule.openStandby")}</button> : <span />}
          </SettingRow>
        </div>
      </section>
    );
  }
  const folderWords = scheduledBackupFolderWords(t, schedule.containerFolder);
  const placeHelp = s3 ? t("schedule.s3Help") : webdav ? t("schedule.webdavHelp") : folderWords.help;
  return (
    <>
    {/* 설정's rows (2026-11-05: a checkbox and boxed fields, the place's fields shown with backups off). */}
    <section ref={scheduleRef} className={styles.section} aria-labelledby="administrator-schedule-title" data-scheduled-backup data-settings-part="scheduled-backup">
      <div id="administrator-schedule-title" className={styles.sectionTitle}>{t("schedule.title")}</div>
      <div className={styles.settingRows}>
        {fixed ? <p className={styles.settingNote} role="note">{t("schedule.setByEnvironment")}</p> : (
          <ScheduledBackupSyncFolders busy={Boolean(busy)} onPick={backUpToSyncFolder}
            current={schedule.enabled && schedule.target === "folder" ? schedule.folder : ""} />
        )}
        <SettingSwitch title={t("schedule.enabled")} description={t("schedule.enabledMeta")} on={draft.enabled} disabled={off}
          onChange={(enabled) => change({ enabled })} control={{ "data-scheduled-backup-enabled": "" }} />
        {draft.enabled ? (
          <>
            <SettingRow title={t("schedule.target")}>
              <AppSelect look="setting" aria-label={t("schedule.target")} value={draft.target} disabled={off}
                onChange={(event) => change({ target: event.target.value === "webdav" || event.target.value === "s3" ? event.target.value : "folder" })}
                data-scheduled-backup-target>
                <option value="folder">{t("schedule.targetFolder")}</option>
                <option value="webdav">{t("schedule.targetWebdav")}</option>
                <option value="s3">{t("schedule.targetS3")}</option>
              </AppSelect>
            </SettingRow>
            {s3 || webdav ? (
              <ScheduledBackupPlaceFields target={s3 ? "s3" : "webdav"} draft={draft} schedule={schedule} disabled={off}
                help={placeHelp} insecureNote={insecureNote} refused={refused} onChange={change} />
            ) : (
              <ScheduledBackupFolderField folder={draft.folder} placeholder={folderWords.placeholder} help={placeHelp}
                disabled={off} busy={Boolean(busy)} choosable={!fixed} invalid={refused(...folderWords.refusals)}
                onChange={(folder) => change({ folder })} />
            )}
            <SettingRow title={t("schedule.interval")}>
              <AppSelect look="setting" aria-label={t("schedule.interval")} value={draft.intervalHours} disabled={off}
                onChange={(event) => change({ intervalHours: Number(event.target.value) })} data-scheduled-backup-interval>
                {intervals.map((hours) => <option key={hours} value={hours}>{intervalLabel(hours, t)}</option>)}
              </AppSelect>
            </SettingRow>
            <SettingField title={t("schedule.keep")} description={t("schedule.keepMeta")}>
              <input className={rowStyles.short} type="number" min={1} max={365} value={draft.keep} disabled={off}
                onChange={(event) => change({ keep: Math.min(365, Math.max(1, Math.round(Number(event.target.value) || 1))) })}
                data-scheduled-backup-keep />
            </SettingField>
          </>
        ) : null}
      </div>
      {/* It comes up when there is something to say or save, under what brought it (2026-11-05: two buttons in the head, always). */}
      {changed || (error && !ranHere) || (message && !ranHere) ? (
        <SettingsSaveBar id="scheduled-backup-status" data-error={error ? "true" : undefined}>
          <span role={error ? "alert" : "status"} aria-live="polite">
            {error || (changed ? (startsOnSave(draft, schedule) ? `${t("shell.unsaved")} ${t("schedule.startsOnSave")}` : t("shell.unsaved")) : message)}
            {error && why.error === error && why.text ? <span data-scheduled-backup-refused-reason> {why.text}</span> : null}</span>
          {error && error === protectionRequiredText(t) && !savePending ? (
            <button type="button" className={styles.secondaryButton} onClick={() => setSavePending(true)} data-scheduled-backup-make-code>
              {t("protection.makeCode")}
            </button>
          ) : null}
          {changed && !fixed ? (
            <button type="button" className={styles.primaryButton} onClick={() => void save()}
              disabled={Boolean(busy) || (draft.enabled && !backupPlaceSet(draft))} data-scheduled-backup-save>
              {busy === "save" ? t("schedule.saving") : t("schedule.save")}
            </button>
          ) : null}
        </SettingsSaveBar>
      ) : null}
    </section>
    {/* Off, the schedule is its one switch; what opens the backups comes with it, or with a folder picked. */}
    {schedule.enabled || draft.enabled || pendingFolder ? (
      <BackupProtection session={session} schedule={schedule} onSchedule={keepProtection}
        askCode={Boolean(pendingFolder) || savePending} onCodeSaved={codeSavedForFolder}
        onCodeCancelled={() => { setPendingFolder(""); setSavePending(false); }} />
    ) : null}
    {schedule.enabled || schedule.lastFinishedAt || schedule.protection.opened ? (
      <ScheduledBackupStatus schedule={schedule} running={running} canRun={canRun} busy={Boolean(busy)} ranHere={ranHere}
        error={error} message={message} placeSet={backupPlaceSet(schedule)} onRunNow={() => void runNow()} onWriteHere={() => void writeHere()}
        onOpenRestore={onSelectSection ? () => onSelectSection("restore") : undefined} />
    ) : null}
    </>
  );
}

// A save that turns backups on, or sends them elsewhere, backs up at once
// (scheduled_backup_settings.go): said before it is pressed.
function startsOnSave(draft: ScheduleInput, schedule: Schedule) {
  if (!draft.enabled) return false;
  if (!schedule.enabled || draft.target !== schedule.target) return true;
  if (draft.target === "webdav") {
    return draft.webdavUrl.trim() !== schedule.webdavUrl || draft.webdavUsername.trim() !== schedule.webdavUsername ||
      draft.webdavPassword !== "" || draft.clearWebdavPassword;
  }
  if (draft.target === "s3") {
    return draft.s3Endpoint.trim() !== schedule.s3Endpoint || draft.s3Region.trim() !== schedule.s3Region ||
      draft.s3Bucket.trim() !== schedule.s3Bucket || draft.s3Prefix.trim() !== schedule.s3Prefix ||
      draft.s3AccessKey.trim() !== schedule.s3AccessKey || draft.s3SecretKey !== "" || draft.clearS3Secret ||
      draft.s3PathStyle !== schedule.s3PathStyle;
  }
  return draft.folder.trim() !== schedule.folder;
}

type Translate = (key: string, options?: Record<string, unknown>) => string;

function intervalLabel(hours: number, t: Translate) {
  if (hours === 24) return t("schedule.daily");
  if (hours === 168) return t("schedule.weekly");
  if (hours % 24 === 0) return t("schedule.everyDays", { count: hours / 24 });
  return t("schedule.everyHours", { count: hours });
}

