import { useEffect, useId, useRef, useState } from "react";
import { AppSelect } from "@/components/AppSelect";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { LeafenApiError } from "@/api";
import { readLogErrorCodes, type ReadLog, type ReadLogEvent } from "@/readLogWire.generated";
import { activeDateLocale } from "@/lib/i18n";
import { titleWithClosedMentions } from "@/lib/closedMentions";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { listedPageNameKind, untitledPageDisplayTitle } from "@/lib/pageTitle";
import { OPEN_READ_LOG_EVENT, readReadLog, setReadLogReaders, type OpenReadLogDetail } from "@/lib/readLog";
import styles from "./ReadLogDialog.module.css";

// The read log of a page (Leafen's own): who opened what in it, when and how, kept always unless the
// server's administrators turned it off; and, for compliance
// administrators, who reads it.

function moment(at: number) {
  return new Date(at).toLocaleString(activeDateLocale(), { year: "numeric", month: "short", day: "numeric", hour: "numeric", minute: "2-digit" });
}

/** What a refusal says: someone without full access, a log left to compliance administrators, or a failure. */
function failureKey(error: unknown) {
  if (!(error instanceof LeafenApiError)) return "loadFailed";
  return error.code === readLogErrorCodes.notFound ? "notAllowed" : error.code === readLogErrorCodes.complianceOnly ? "complianceOnly" : "loadFailed";
}

function EventLine({ event }: { event: ReadLogEvent }) {
  const { t } = useTranslation("readLog");
  const who = event.readerName || (event.via === "share" ? t("shareLink") : t("someone"));
  const what = event.kind !== "read" ? t(`kinds.${event.kind}`)
    : event.targetKind === "scope" ? t("wholeWorkspace") : titleWithClosedMentions(event.title, event.titleSpans)
      // An untitled page or row by its kind, as everywhere (untitledPageDisplayTitle); a file "제목 없음".
      || (event.targetKind === "file" ? t("untitled") : untitledPageDisplayTitle(listedPageNameKind(event)));
  const how = event.kind !== "read" ? ""
    : event.via === "app" ? t(event.how === "list" ? "via.appList" : "via.appOpen")
      : event.via === "mcp" ? (event.client ? t("via.mcpClient", { client: event.client }) : t("via.mcp"))
        : t(`via.${event.via || "app"}`);
  return (
    <li className={styles.event} data-read-log-event={event.kind} data-via={event.via}>
      <span className={styles.time}>{moment(event.at)}</span>
      <span className={styles.line}><span className={styles.who}>{who}</span> <span>{what}</span></span>
      {how || event.count > 1 ? (
        <span className={styles.how}>{how}{event.count > 1 ? `${how ? " · " : ""}×${event.count}` : ""}</span>
      ) : null}
    </li>
  );
}

function ReadLogDialog({ detail, onClose }: { detail: OpenReadLogDetail; onClose(): void }) {
  const { t } = useTranslation(["readLog", "common"]);
  const [log, setLog] = useState<ReadLog | null>(null);
  const [events, setEvents] = useState<ReadLogEvent[]>([]);
  const [failure, setFailure] = useState("");
  const [busy, setBusy] = useState(false);
  const dialogRef = useRef<HTMLDivElement>(null);
  const titleId = useId();
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [dialogRef.current?.parentElement], onEscape: onClose,
  });

  const failed = (error: unknown) => setFailure(failureKey(error));

  useEffect(() => {
    const controller = new AbortController();
    readReadLog(detail.workspaceId, detail.scope, "", controller.signal).then((next) => {
      setLog(next);
      setEvents(next.events);
    }).catch((error) => { if (!controller.signal.aborted) setFailure(failureKey(error)); });
    return () => controller.abort();
  }, [detail.scope, detail.workspaceId]);

  async function more() {
    if (!log?.nextCursor || busy) return;
    setBusy(true);
    try {
      const next = await readReadLog(detail.workspaceId, detail.scope, log.nextCursor);
      setLog(next);
      setEvents((current) => [...current, ...next.events]);
    } catch (error) {
      failed(error);
    } finally {
      setBusy(false);
    }
  }

  async function choose(readers: ReadLog["readers"]) {
    setBusy(true);
    setFailure("");
    try {
      const next = await setReadLogReaders(detail.workspaceId, detail.scope, readers);
      setLog(next);
      setEvents(next.events);
    } catch (error) {
      failed(error);
    } finally {
      setBusy(false);
    }
  }

  return createPortal(
    <div className={styles.overlay} style={{ zIndex }}>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("common:actions.close")} onClick={onClose} />
      <div ref={dialogRef} className={styles.dialog} role="dialog" aria-modal="true" aria-labelledby={titleId} data-read-log-dialog>
        <h2 id={titleId}>{t("title")}</h2>
        {log ? <p className={styles.name}>{titleWithClosedMentions(log.name, log.nameSpans)
          || untitledPageDisplayTitle(listedPageNameKind(log))}</p> : null}
        {failure ? <p className={styles.failure} role="alert">{t(failure)}</p> : null}
        {log ? (
          <>
            <p className={styles.since} data-read-log-recording={log.recording}>
              {log.recording ? t(log.retentionDays > 0 ? "keeping" : "keepingAlways", { days: log.retentionDays }) : t("serverOff")}
            </p>
            {log.canChooseReaders ? (
              <div className={styles.readersRow} data-read-log-readers={log.readers}>
                <span>{t("readers")}</span>
                <AppSelect aria-label={t("readers")} look="value" value={log.readers} disabled={busy || Boolean(log.readersFrom)}
                  onChange={(event) => void choose(event.target.value as ReadLog["readers"])}>
                  <option value="fullAccess">{t("readersFullAccess")}</option>
                  <option value="compliance">{t("readersCompliance")}</option>
                </AppSelect>
              </div>
            ) : null}
            {log.canChooseReaders && log.readersFrom ? <p className={styles.since}>{t("readersFrom", { name: log.readersFrom })}</p> : null}
            <ul className={styles.events} aria-label={t("title")}>
              {events.map((event) => <EventLine key={event.id} event={event} />)}
              {events.length === 0 ? <li className={styles.empty}>{t("empty")}</li> : null}
            </ul>
            {log.nextCursor ? (
              <button type="button" className={styles.more} disabled={busy} onClick={() => void more()}>{t("more")}</button>
            ) : null}
          </>
        ) : null}
        <div className={styles.actions}>
          <button type="button" className={styles.close} onClick={onClose}>{t("common:actions.close")}</button>
        </div>
      </div>
    </div>,
    document.body,
  );
}

/** Opens the read log a page's menu asks for (requestReadLogDialog). */
export function ReadLogDialogHost() {
  const [detail, setDetail] = useState<OpenReadLogDetail | null>(null);
  useEffect(() => {
    const listener = (event: Event) => setDetail((event as CustomEvent<OpenReadLogDetail>).detail);
    window.addEventListener(OPEN_READ_LOG_EVENT, listener);
    return () => window.removeEventListener(OPEN_READ_LOG_EVENT, listener);
  }, []);
  return detail ? <ReadLogDialog key={detail.scope} detail={detail} onClose={() => setDetail(null)} /> : null;
}
