import { useCallback, useEffect, useRef, useState } from "react";
import { useAttemptNotice } from "@/lib/useNoticeFor";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { useTranslation } from "react-i18next";
import { api } from "../../api";
import {
  administratorErrorCodes,
  type AdministratorServerMoveCounts as Counts,
  type AdministratorServerMoveIn as MoveIn,
  type AdministratorServerMoveOut as MoveOut,
} from "../../administratorWire.generated";
import { LeafenApiError } from "../../apiTransport";
import { followAcrossTabs, tellTabs } from "../../lib/followAcrossTabs";
import { activeNumberLocale } from "../../lib/i18n";
import { displayDateTime } from "@/lib/displayDate";
import { productErrorMessage } from "../../lib/productErrorPresentation";
import { openMovedServer } from "../../lib/profileSwitch";
import type { Session } from "../../types";
import styles from "./AdministratorSettingsPage.module.css";
import dataStyles from "./AdministratorDataManagement.module.css";
import { useCopyNotice } from "@/lib/copyNotice";
import { ReadFailed } from "../ReadFailed";

// While a move runs, its state is read again this often.
const STATUS_MS = 2000;
const COUNTS: (keyof Counts)[] = ["people", "workspaces", "pages", "files", "edits"];

type Translate = (key: string, options?: Record<string, unknown>) => string;

/**
 * Moving the whole server to another (the agent decision of 2026-10-19
 * 09:31): on the new server, a code for an hour; on this one, the code and
 * a confirmation, then this server stops taking changes and its data —
 * people, pages, history, files, settings and the remote address — goes to
 * the new server, which starts again with it and shows what it counted
 * against what this one counted. Leafen copies; the person's outside agent
 * may guide it, the code put in here by the person.
 */
export function ServerMoveSetting({ session }: {
  session: Session;
}) {
  const { t } = useTranslation("serverConsole");
  const copy = useCopyNotice();
  const [moveIn, setMoveIn] = useState<MoveIn | null>(null);
  const [moveOut, setMoveOut] = useState<MoveOut | null>(null);
  const [pasted, setPasted] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useAttemptNotice<string>(pasted);
  // A server that replicates keeps itself apart under Continuous replication instead.
  // Whether a copy keeps in step, once read: "keep apart" waits for it.
  const [replicating, setReplicating] = useState<boolean | null>(null);
  const mounted = useRef(true);

  // Either move's state that could not be read: the part says so with a way to read again.
  const [readFailed, setReadFailed] = useState(false);
  const load = useCallback((signal?: AbortSignal) => {
    const failed = () => { if (mounted.current && !signal?.aborted) setReadFailed(true); };
    api.serverMoveIn(session, signal).then((value) => { if (mounted.current) setMoveIn(value); }, failed);
    api.serverMoveOut(session, signal).then((value) => { if (mounted.current) setMoveOut(value); }, failed);
    api.scheduledBackup(session, signal).then((value) => { if (mounted.current) setReplicating(value.schedule.replication.enabled); }, () => undefined);
  }, [session]);

  useEffect(() => {
    mounted.current = true;
    const controller = new AbortController();
    load(controller.signal);
    return () => { mounted.current = false; controller.abort(); };
  }, [load]);

  const moving = moveOut?.state === "copying" || moveOut?.state === "finishing";
  const receiving = moveIn?.state === "waiting" || moveIn?.state === "receiving" || moveIn?.state === "restoring";
  // A move under way is followed until it settles; the new server goes away
  // while it starts again. One of the person's tabs in sight asks for all of
  // them, and a tab behind asks nothing (followAcrossTabs).
  useEffect(() => {
    if (!moving && !receiving) return undefined;
    const controller = new AbortController();
    const topic = `server-move:${session.user.id}`;
    const stop = followAcrossTabs(topic, STATUS_MS, () => Promise.allSettled([
      api.serverMoveIn(session, controller.signal), api.serverMoveOut(session, controller.signal), api.scheduledBackup(session, controller.signal),
    ]).then(([moveIn, moveOut, backup]) => tellTabs(topic, {
      moveIn: moveIn.status === "fulfilled" ? moveIn.value : undefined,
      moveOut: moveOut.status === "fulfilled" ? moveOut.value : undefined,
      replicating: backup.status === "fulfilled" ? backup.value.schedule.replication.enabled : undefined,
    })), (value) => {
      const told = value as { moveIn?: MoveIn; moveOut?: MoveOut; replicating?: boolean } | undefined;
      if (!mounted.current || !told) return;
      if (told.moveIn) setMoveIn(told.moveIn);
      if (told.moveOut) setMoveOut(told.moveOut);
      if (told.replicating !== undefined) setReplicating(told.replicating);
    });
    return () => { controller.abort(); stop(); };
  }, [moving, receiving, session]);

  async function act<T>(action: () => Promise<T>, apply: (value: T) => void) {
    setBusy(true);
    setError("");
    try {
      const value = await action();
      if (mounted.current) apply(value);
    } catch (cause) {
      if (mounted.current) setError(moveMessage(cause, t));
    } finally {
      if (mounted.current) setBusy(false);
    }
  }

  // Moving the whole server asks first in the common confirmation, which
  // says what follows, as every settings action that cannot be undone in
  // one step does; focus goes back to "이 서버 옮기기".
  async function startMove() {
    const code = pasted.trim();
    if (!await confirmAction({ message: t("serverMove.confirmQuestion"), detail: t("serverMove.warning"),
      confirmLabel: t("serverMove.confirm"), danger: true })) return;
    await act(() => api.startServerMoveOut(session, code), (value) => { setMoveOut(value); setPasted(""); });
  }

  if (!moveIn || !moveOut) return readFailed ? (
    <section className={styles.section} data-server-move data-settings-part="server-move">
      <div className={styles.sectionTitle}>{t("serverMove.title")}</div>
      <ReadFailed message={t("serverMove.readFailed")} onRetry={() => { setReadFailed(false); load(); }} />
    </section>
  ) : null;
  const errorLine = error ? <div id="server-move-error" className={dataStyles.live} data-error="true" role="alert">{error}</div> : null;
  const movedAway = moveOut.state === "moved" || (moveOut.state === "none" && Boolean(moveOut.to));

  return (
    <section className={styles.section} aria-labelledby="administrator-server-move-title" data-server-move data-settings-part="server-move">
      <div id="administrator-server-move-title" className={styles.sectionTitle}>{t("serverMove.title")}</div>
      <p className={`${styles.settingNote} ${styles.lead}`}>{t("serverMove.meta")}</p>

      {moveIn.state === "moved" ? (
        <div className={styles.emailProviderBlock} data-server-move-in="moved">
          <div className={styles.organizationSubhead}><span>{t("serverMove.movedInTitle")}</span></div>
          <p className={styles.emailProviderMeta}>
            {t("serverMove.movedIn", { address: moveIn.from || t("serverMove.anotherServer"), time: formatTime(moveIn.movedAt) })}
          </p>
          <CountTable expected={moveIn.expected} actual={moveIn.actual} t={t} />
          <div className={dataStyles.actions}>
            <button type="button" className={styles.secondaryButton} disabled={busy}
              onClick={() => void act(() => api.closeServerMoveIn(session), setMoveIn)} data-server-move-in-dismiss>
              {t("serverMove.dismiss")}
            </button>
          </div>
          <p className={styles.emailProviderMeta}>{t("serverMove.movedInHelp")}</p>
        </div>
      ) : null}

      {movedAway ? (
        <div className={styles.emailProviderBlock} data-server-move-out="moved">
          <div className={styles.organizationSubhead}><span>{t("serverMove.outTitle")}</span></div>
          <p className={styles.emailProviderMeta}>{t("serverMove.movedOut", { address: moveOut.to })}</p>
          <div className={dataStyles.actions}>
            <button type="button" className={styles.primaryButton} onClick={() => openMovedServer(moveOut.to)} data-server-move-open>
              {t("serverMove.openNew")}
            </button>
            {replicating !== false ? null : (
              <button type="button" className={styles.secondaryButton} disabled={busy} data-server-move-apart
                onClick={() => void act(() => api.keepServerApart(session), () => load())}>
                {t("replication.keepApart")}
              </button>
            )}
          </div>
          {replicating !== false ? null : <p className={styles.emailProviderMeta}>{t("replication.keepApartHelp")}</p>}
        </div>
      ) : moving ? (
        <div className={styles.emailProviderBlock} data-server-move-out={moveOut.state}>
          <div className={styles.organizationSubhead}><span>{t("serverMove.outTitle")}</span></div>
          <p className={styles.emailProviderMeta} role="status">
            {t(moveOut.state === "copying" ? "serverMove.copying" : "serverMove.finishing", { address: moveOut.to })}
          </p>
          <CountTable expected={moveOut.counts} t={t} />
        </div>
      ) : !receiving && moveIn.state !== "moved" ? (
        <div className={styles.emailProviderBlock} data-server-move-out={moveOut.state}>
          <div className={styles.organizationSubhead}><span>{t("serverMove.outTitle")}</span></div>
          {moveOut.state === "failed" ? <p className={dataStyles.live} data-error="true">{t("serverMove.failed")}</p> : null}
          <div className={styles.tunnelFields}>
            <label>
              <span>{t("serverMove.codeLabel")}</span>
              <textarea rows={3} value={pasted} disabled={busy} placeholder="leafen-move-1:…" data-server-move-code
                onChange={(event) => setPasted(event.target.value)}
                {...(error === t("serverMove.codeInvalid") ? { "aria-invalid": true as const, "aria-describedby": "server-move-error" } : {})} />
            </label>
            <div className={dataStyles.actions}>
              <button type="button" className={styles.secondaryButton} disabled={busy || !pasted.trim()} data-server-move-start
                onClick={() => void startMove()}>
                {t("serverMove.start")}
              </button>
            </div>
          </div>
          <p className={styles.emailProviderMeta}>{t("serverMove.outHelp")}</p>
        </div>
      ) : null}

      {!movedAway && !moving && moveIn.state !== "moved" ? (
        <div className={styles.emailProviderBlock} data-server-move-in={moveIn.state}>
          <div className={styles.organizationSubhead}><span>{t("serverMove.inTitle")}</span></div>
          {moveIn.state === "waiting" ? (
            <div className={styles.tunnelFields} data-server-move-in-code>
              <label>
                <span>{t("serverMove.inCodeLabel")}</span>
                <textarea readOnly rows={3} value={moveIn.code} onFocus={(event) => event.currentTarget.select()} />
              </label>
              <div className={dataStyles.actions}>
                <button type="button" className={styles.secondaryButton} onClick={() => void copy(moveIn.code)}>
                  {t("serverMove.copy")}
                </button>
                <button type="button" className={styles.secondaryButton} disabled={busy}
                  onClick={() => void act(() => api.closeServerMoveIn(session), setMoveIn)} data-server-move-in-close>
                  {t("serverMove.cancel")}
                </button>
              </div>
              <p className={styles.emailProviderMeta}>{t("serverMove.inCodeHelp", { time: formatTime(moveIn.expiresAt) })}</p>
            </div>
          ) : moveIn.state === "receiving" ? (
            <>
              {/* A finish that failed here leaves the place open to the same code and says why; cancelling it, or a
                  place an old server stopped sending to, lets a new code be made without starting again. */}
              {moveIn.errorCode ? (
                <p className={dataStyles.live} data-error="true" role="alert" data-server-move-in-failed={moveIn.errorCode}>
                  {t(stoppedKey(moveIn.errorCode))} {t("serverMove.inStoppedNext")}
                </p>
              ) : (
                <p className={styles.emailProviderMeta} role="status">{t("serverMove.receiving")}</p>
              )}
              <div className={dataStyles.actions}>
                <button type="button" className={styles.secondaryButton} disabled={busy}
                  onClick={() => void act(() => api.closeServerMoveIn(session), setMoveIn)} data-server-move-in-close>
                  {t("serverMove.cancel")}
                </button>
              </div>
            </>
          ) : moveIn.state === "restoring" ? (
            <p className={styles.emailProviderMeta} role="status">
              {t(moveIn.restart === "manual" ? "serverMove.restoringManual" : "serverMove.restoring")}
            </p>
          ) : (
            <>
              {/* A move this server refused to take says why here: the old server's screen says only that it stopped. */}
              {moveIn.errorCode ? (
                <p className={dataStyles.live} data-error="true" role="alert" data-server-move-in-refused={moveIn.errorCode}>
                  {t(moveIn.errorCode === administratorErrorCodes.dataBeforeSchemaBaseline ? "serverMove.inEarlierLeafen" : "serverMove.inRefused")}
                </p>
              ) : null}
              <div className={dataStyles.actions}>
                <button type="button" className={styles.secondaryButton} disabled={busy || moveIn.people > 1} data-server-move-in-open
                  onClick={() => void act(() => api.openServerMoveIn(session), setMoveIn)}>
                  {t("serverMove.inOpen")}
                </button>
              </div>
              <p className={styles.emailProviderMeta}>{moveIn.people > 1 ? t("serverMove.inNotEmpty") : t("serverMove.inHelp")}</p>
            </>
          )}
        </div>
      ) : null}
      {errorLine}
    </section>
  );
}

function CountTable({ expected, actual, t }: { expected: Counts; actual?: Counts; t: Translate }) {
  return (
    <div className={dataStyles.summary} data-server-move-counts>
      {COUNTS.map((key) => (
        <div key={key} className={dataStyles.summaryRow} data-server-move-count={key}>
          <span>{t(`serverMove.count.${key}`)}</span>
          <strong>
            {actual ? (actual[key] === expected[key]
              ? t("serverMove.countSame", { count: actual[key] })
              : t("serverMove.countDiffers", { actual: actual[key], expected: expected[key] })) : expected[key].toLocaleString(activeNumberLocale())}
          </strong>
        </div>
      ))}
    </div>
  );
}

function formatTime(value: string) {
  // The screen's own way of writing a moment, as everywhere else (lib/displayDate).
  return displayDateTime(value) || value;
}

// Why a finish failed on this server, by its code.
function stoppedKey(code: string) {
  if (code === administratorErrorCodes.serverMoveIncomplete) return "serverMove.inStoppedIncomplete";
  if (code === administratorErrorCodes.serverMoveBusy) return "serverMove.inStoppedBusy";
  return "serverMove.inStoppedFailed";
}

function moveMessage(error: unknown, t: Translate) {
  if (error instanceof LeafenApiError) {
    if (error.code === administratorErrorCodes.serverMoveCodeInvalid) return t("serverMove.codeInvalid");
    if (error.code === administratorErrorCodes.serverMoveNotEmpty) return t("serverMove.inNotEmpty");
    if (error.code === administratorErrorCodes.serverMoveVersionOlder) return t("serverMove.versionOlder");
    if (error.code === administratorErrorCodes.serverMoveUnreachable) return t("serverMove.unreachable");
    if (error.code === administratorErrorCodes.serverMoveBusy) return t("serverMove.busy");
    if (error.code === administratorErrorCodes.serverMoveSameServer) return t("serverMove.sameServer");
  }
  return productErrorMessage(error, "administratorScheduledBackup.save");
}
