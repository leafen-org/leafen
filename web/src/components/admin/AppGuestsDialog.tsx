import { useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { AppGuests } from "@/appPlatformWire.generated";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import { appText, listAppGuestLinks, listAppGuestReports, readAppGuests, revokeAppGuestLink, setAppGuests } from "./administratorAppsApi";
import styles from "./AdministratorApps.module.css";
import { SettingSwitch } from "./AdministratorSettingRows";
import { displayDateTime } from "@/lib/displayDate";
import { ReadFailed } from "../ReadFailed";

/**
 * Public guests of an app, for the server's administrators
 * (docs/app-platform.md 9): whether people who use the app may make links
 * that let anyone in to its guest rooms, and the links that are still open,
 * each to be cut at once. Turning guests off ends every guest's session.
 * The open links come a window at a time and are read to the last, so every
 * one can be cut; the reports go on with "더 보기".
 */
export function AppGuestsDialog({ session, installId, name, onClose }: {
  session: Session;
  installId: string;
  name: string;
  onClose: () => void;
}) {
  const { t, i18n } = useTranslation(["serverConsole", "common"]);
  const titleId = useId();
  const overlayRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  const closeRef = useRef<HTMLButtonElement>(null);
  const [guests, setGuests] = useState<AppGuests | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // The first read that failed, said where what it reads shows, with a way to read again.
  const [readFailed, setReadFailed] = useState("");
  const [readTurn, setReadTurn] = useState(0);
  // Each read of the links counts up, and an older one still reading on stops.
  const linksRead = useRef(0);
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [overlayRef.current], onEscape: () => { if (!busy) onClose(); }, initialFocus: closeRef,
  });

  // The links after the first window, each window added as it comes, to the last.
  async function readLinksOn(cursor: string) {
    const read = ++linksRead.current;
    while (cursor) {
      const page = await listAppGuestLinks(session, installId, cursor);
      if (read !== linksRead.current) return;
      setGuests((current) => current && {
        ...current, links: [...current.links, ...page.links.filter((link) => !current.links.some((shown) => shown.id === link.id))],
      });
      cursor = page.nextCursor ?? "";
    }
  }

  useEffect(() => {
    let live = true;
    readAppGuests(session, installId).then(
      (read) => {
        if (!live) return;
        setGuests(read);
        return readLinksOn(read.linksNextCursor ?? "");
      },
    ).catch((failure) => { if (live) setReadFailed(settingsErrorMessage(failure, t("apps.readFailed"))); });
    return () => { live = false; linksRead.current += 1; };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- readLinksOn reads with the same session and app
  }, [readTurn, installId, session, t]);

  // A change answers with the first windows: the links are read on again,
  // and the reports already shown stay.
  async function change(run: () => Promise<AppGuests>) {
    setBusy(true);
    setError("");
    try {
      const next = await run();
      setGuests((current) => current ? { ...next, reports: current.reports, reportsNextCursor: current.reportsNextCursor } : next);
      await readLinksOn(next.linksNextCursor ?? "");
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("apps.changeFailed")));
    } finally {
      setBusy(false);
    }
  }

  async function moreReports(cursor: string) {
    setBusy(true);
    setError("");
    try {
      const page = await listAppGuestReports(session, installId, cursor);
      setGuests((current) => current && { ...current, reports: [...current.reports, ...page.reports], reportsNextCursor: page.nextCursor ?? "" });
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("apps.readFailed")));
    } finally {
      setBusy(false);
    }
  }

  const when = (at: number) => displayDateTime(at);
  // A room by the name the app's screen gives it, in the reader's language (app_platform_guest_rooms.go).
  const roomName = (room: string) => guests?.roomNames?.[room] ? appText(guests.roomNames[room], i18n.language) : room;
  const open = guests?.links.filter((link) => !link.revoked) ?? [];
  return createPortal(
    <div ref={overlayRef} className={styles.overlay} style={{ zIndex }} data-app-guests>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("common:actions.close")} disabled={busy} onClick={onClose} />
      <section ref={dialogRef} className={styles.dialog} role="dialog" aria-modal="true" aria-labelledby={titleId} aria-busy={busy || undefined}>
        <h2 id={titleId} className={styles.dialogTitle}>{t("apps.guests.title", { name })}</h2>
        {guests ? (
          <>
            <p className={styles.note}>{t("apps.guests.body", { count: guests.maxPerRoom })}</p>
            {/* A switch that saves as it turns, as 설정's rows (2026-11-06: a checkbox here, switches there). */}
            <SettingSwitch title={t("apps.guests.enabled")} description={t("apps.guests.enabledBody")} on={guests.enabled} disabled={busy}
              onChange={(enabled) => void change(() => setAppGuests(session, installId, enabled))} data-app-guests-enabled="" />
            <h3 className={styles.permissionsTitle}>{t("apps.guests.links")}</h3>
            {open.length ? open.map((link) => (
              <div key={link.id} className={`${styles.jobRow} ${styles.guestLink}`} data-app-guest-link={link.id}>
                <span className={styles.secretName}>
                  <strong>{roomName(link.room)}</strong>
                  <span>{[link.createdBy || t("apps.guests.someone"), t("apps.guests.until", { date: when(link.expiresAt) })].join(" · ")}</span>
                </span>
                <button type="button" className={styles.secondary} disabled={busy} data-app-guest-revoke
                  onClick={() => void confirmAction({ message: t("apps.guests.revokeConfirm", { room: roomName(link.room) }), detail: t("apps.guests.revokeConfirmDetail"),
                    confirmLabel: t("apps.guests.revoke"), danger: true })
                    .then((confirmed) => { if (confirmed) void change(() => revokeAppGuestLink(session, installId, link.id)); })}>{t("apps.guests.revoke")}</button>
              </div>
            )) : <p className={styles.note}>{t("apps.guests.noLinks")}</p>}
            {guests.reports?.length ? (
              <>
                <h3 className={styles.permissionsTitle}>{t("apps.guests.reports")}</h3>
                <ul className={styles.runList} data-app-guest-reports>
                  {guests.reports.map((report, index) => (
                    <li key={`${index}/${report.created}`} data-blocked={report.blocked || undefined}>
                      <span>{[when(report.created), roomName(report.room), report.reporter || t("apps.guests.someone"),
                        report.blocked ? t("apps.guests.blocked") : t("apps.guests.reported")].join(" · ")}</span>
                      {report.reason ? <span>{report.reason}</span> : null}
                    </li>
                  ))}
                </ul>
                {guests.reportsNextCursor ? (
                  <button type="button" className={`${styles.secondary} ${styles.listMore}`} disabled={busy} data-app-guest-reports-more
                    onClick={() => void moreReports(guests.reportsNextCursor ?? "")}>{t("apps.more")}</button>
                ) : null}
              </>
            ) : null}
          </>
        ) : null}
        {readFailed ? <ReadFailed message={readFailed} onRetry={() => { setReadFailed(""); setReadTurn((turn) => turn + 1); }} /> : null}
        {error ? <div className={styles.error} role="alert">{error}</div> : null}
        <div className={styles.dialogActions}>
          <button ref={closeRef} type="button" className={styles.secondary} disabled={busy} onClick={onClose}>{t("common:actions.close")}</button>
        </div>
      </section>
    </div>,
    document.body,
  );
}
