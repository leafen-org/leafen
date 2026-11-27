import { type MutableRefObject, useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { Session } from "@/types";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { isPackagedClient } from "@/lib/packagedClient";
import { signInThroughApp } from "@/lib/appSignIn";
import { browserPasskeysAvailable } from "@/lib/passkeys";
import { passkeyErrorMessage } from "@/lib/passkeyErrorMessage";
import { useAccountPasskeys } from "./useAccountPasskeys";
import { styles } from "./accountSettingsStyles";
import dialogStyles from "./AccountEmailManagement.module.css";
import { displayDateTime } from "@/lib/displayDate";
import { ListNotice } from "@/components/settings/SettingsRowNotice";

export function AccountPasskeysSection({ session, onKeys, addRef }: {
  session: Session;
  /** How many passkeys the account holds, as they are read. */
  onKeys?(count: number): void;
  /** Adding a passkey, for another row that offers it (the password card's link), or null where none can be added now. */
  addRef?: MutableRefObject<(() => void) | null>;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const keys = useAccountPasskeys(session);
  const count = keys.state?.entries.length;
  useEffect(() => { if (count !== undefined) onKeys?.(count); }, [count, onKeys]);
  const [open, setOpen] = useState(false);
  const [browserBusy, setBrowserBusy] = useState(false);
  const [browserError, setBrowserError] = useState("");
  const managerRef = useRef<HTMLButtonElement>(null);
  const externalAbort = useRef<AbortController | null>(null);
  const context = `${session.user.id}\u0000${session.sessionId ?? session.token}`;
  useEffect(() => {
    setOpen(false); setBrowserBusy(false); setBrowserError("");
    return () => externalAbort.current?.abort();
  }, [context]);
  const capability = keys.state?.capability;
  // The app's window can't ask the system for a passkey on its own origin: it goes through the browser.
  const external = isPackagedClient();
  const sameOrigin = capability?.origin === window.location.origin;
  const canPrompt = !external && sameOrigin && browserPasskeysAvailable();
  useEffect(() => {
    if (!addRef) return undefined;
    addRef.current = external ? (capability?.available ? () => void manageInBrowser() : null)
      : keys.state?.canAdd && canPrompt ? () => void keys.add() : null;
    return () => { addRef.current = null; };
  });
  const disabledReason = !capability?.available ? t("passkeyUnavailable") : keys.state?.ssoRequired ? t("passkeySSO")
    : (keys.state?.entries.length ?? 0) >= (keys.state?.limit ?? 5) ? t("passkeyLimit") : !canPrompt ? t("passkeyDeviceUnsupported") : "";

  function close() { setOpen(false); window.requestAnimationFrame(() => managerRef.current?.focus()); }
  async function manageInBrowser() {
    if (!capability?.available || browserBusy) return;
    const abort = new AbortController(); externalAbort.current = abort;
    setBrowserBusy(true); setBrowserError("");
    try {
      await signInThroughApp(`${capability.origin}/auth/passkey?manage=1`, "passkeyCompletion", abort.signal);
      if (!abort.signal.aborted) await keys.refresh();
    } catch (failure) {
      if (!abort.signal.aborted) setBrowserError(passkeyErrorMessage(failure, (key) => t(key), "accountPasskeys.update"));
    } finally { if (!abort.signal.aborted) setBrowserBusy(false); }
  }

  return <div className={styles.securityPanel} data-account-passkeys aria-busy={keys.busy || browserBusy}>
    <div className={styles.securityRow}>
      {/* With none yet, Notion marks the row "권장됨" (seen 2026-11-09). */}
      <div className={styles.securityInstruction}><strong>{t("passkeysTitle")}{keys.state?.entries.length === 0
        ? <span className={styles.recommendedBadge} data-passkey-recommended>{t("passkeyRecommended")}</span> : null}</strong>
        <span>{t("passkeysDescription")}</span></div>
      {external ? <button className={styles.secondaryButton} disabled={!capability?.available || keys.busy || browserBusy} onClick={() => void manageInBrowser()}>{t("passkeyBrowser")}</button>
        : <div className={styles.sectionActions}>
          {Boolean(keys.state?.entries.length) ? <button ref={managerRef} className={styles.secondaryButton} onClick={() => { keys.clearError(); setOpen(true); }}>{t("managePasskeys")}</button> : null}
          <button className={styles.secondaryButton} disabled={keys.busy || !keys.state?.canAdd || !canPrompt} onClick={() => void keys.add()}>{t("addPasskey")}</button>
        </div>}
    </div>
    {external ? <p className={styles.organizationMeta}>{t("passkeyBrowserDescription")}</p> : null}
    {keys.state && disabledReason && !external ? <p className={styles.organizationMeta}>{disabledReason}</p> : null}
    {keys.state && !disabledReason && !external && keys.state.limit - keys.state.entries.length === 1 ? (
      <p className={styles.organizationMeta} data-passkeys-capacity="near">{t("passkeyNear", { limit: keys.state.limit })}</p>
    ) : null}
    {keys.state && !capability?.available && external ? <p className={styles.organizationMeta}>{t("passkeyUnavailable")}</p> : null}
    {keys.error || browserError ? <ListNotice role="alert" onRetry={() => void keys.refresh()} data-passkeys-read-failed={!keys.state || undefined}>{keys.error || browserError}</ListNotice> : null}
    {browserBusy ? <button className={styles.secondaryButton} onClick={() => { externalAbort.current?.abort(); setBrowserBusy(false); }}>{t("managedUsersCancel")}</button> : null}
    {open ? <PasskeyManager keys={keys} canPrompt={canPrompt} onClose={close} /> : null}
  </div>;
}

function PasskeyManager({ keys, canPrompt, onClose }: {
  keys: ReturnType<typeof useAccountPasskeys>; canPrompt: boolean; onClose: () => void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const titleID = useId();
  const dialogRef = useRef<HTMLElement>(null);
  const closeRef = useRef<HTMLButtonElement>(null);
  const [removeID, setRemoveID] = useState<string | null>(null);
  const { zIndex } = useOverlayLayer(true, { kind: "dialog", ref: dialogRef, modalOwned: () => [dialogRef.current?.parentElement], onEscape: () => { if (!keys.busy) removeID ? setRemoveID(null) : onClose(); } });
  useEffect(() => { closeRef.current?.focus(); }, [removeID]);
  const date = (value: string) => displayDateTime(value);
  return createPortal(<div className={dialogStyles.backdrop} style={{ zIndex }} onMouseDown={(event) => { if (event.target === event.currentTarget && !keys.busy) onClose(); }}>
    <section ref={dialogRef} className={dialogStyles.dialog} role="dialog" aria-modal="true" aria-labelledby={titleID} aria-busy={keys.busy}>
      <h2 id={titleID}>{removeID ? t("enterpriseRemove") : t("managePasskeys")}</h2>
      {removeID ? <p>{t("passkeyRemoveDescription")}</p> : <>
        <p>{t("passkeysDescription")}</p>
        <table className={dialogStyles.addresses} aria-label={t("managePasskeys")}><tbody>
          {keys.state?.entries.map((entry, index) => <tr className={dialogStyles.address} key={entry.id}>
            <td><strong>{t("passkeysTitle")} {index + 1}</strong><p>{t("passkeyAdded", { date: date(entry.createdAt) })}</p>
              {entry.lastUsedAt ? <p>{t("passkeyLastUsed", { date: date(entry.lastUsedAt) })}</p> : null}</td>
            <td><button disabled={keys.busy} onClick={() => { keys.clearError(); setRemoveID(entry.id); }}>{t("enterpriseRemove")}</button></td>
          </tr>)}
        </tbody></table>
      </>}
      {keys.error ? <div className={styles.notice} role="alert">{keys.error}</div> : null}
      <div className={dialogStyles.footer}>
        <button disabled={keys.busy} onClick={() => void keys.refresh()}>{t("refresh")}</button>
        <button ref={closeRef} disabled={keys.busy} onClick={() => removeID ? setRemoveID(null) : onClose()}>{t("managedUsersCancel")}</button>
        {removeID ? <button className={dialogStyles.primary} disabled={keys.busy} onClick={() => void keys.remove(removeID).then((ok) => { if (ok) setRemoveID(null); })}>{t("enterpriseRemove")}</button>
          : <button className={dialogStyles.primary} disabled={keys.busy || !keys.state?.canAdd || !canPrompt} onClick={() => void keys.add()}>{t("addPasskey")}</button>}
      </div>
    </section>
  </div>, document.body);
}
