import { type FormEvent, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import {
  createPersonalAccessToken,
  type PersonalAccessToken,
  type PersonalAccessTokenLifetime,
} from "@/lib/personalAccessTokens";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import { useOverlayLayer } from "@/lib/overlayLayer";
import type { Session } from "@/types";
import styles from "./DeveloperSettings.module.css";
import own from "./PersonalAccessTokenDialog.module.css";
import { KeyLifetimeRow } from "./KeyLifetimeRow";
import { useCopyNotice } from "@/lib/copyNotice";

/**
 * Notion's "새 개인 액세스 토큰" (seen 2026-10-02 and 2026-11-13): a name, a
 * lifetime of a year unless chosen otherwise from the small "1년 ⌄" at the end
 * of its row, what the token can do, and "토큰 생성하기", which waits for a name. Workers, the other capability there, is not in
 * Leafen, nor Notion's developer terms. Once made, the same window
 * shows the secret for the one time it can be copied.
 */
export function PersonalAccessTokenDialog({
  session,
  workspaceId,
  onCreated,
  onClose,
}: {
  session: Session;
  workspaceId: string;
  onCreated: (token: PersonalAccessToken) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation(["workspaceSettingsDialog", "common"]);
  const titleId = useId();
  const overlayRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  const nameRef = useRef<HTMLInputElement>(null);
  const [name, setName] = useState("");
  const [lifetime, setLifetime] = useState<PersonalAccessTokenLifetime>("1y");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [secret, setSecret] = useState("");
  const copy = useCopyNotice();

  // A modal layer over the settings: Escape closes it, and only it, unless
  // it is busy.
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [overlayRef.current], onEscape: () => { if (!busy) onClose(); }, initialFocus: { current: null },
  });

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || !name.trim()) return;
    setBusy(true);
    setError("");
    try {
      const created = await createPersonalAccessToken(session, workspaceId, { name: name.trim(), expiresIn: lifetime });
      setSecret(created.secret);
      onCreated(created.token);
    } catch (failure) {
      const code = (failure as { code?: unknown } | null)?.code;
      setError(code === "LEAFEN_PERSONAL_ACCESS_TOKEN_LIMIT"
        ? t("personalAccessTokenLimit")
        : settingsErrorMessage(failure, t("personalAccessTokenCreateFailed")));
    } finally {
      setBusy(false);
    }
  }

  async function copySecret() {
    await copy(secret, undefined, (message) => setError(message));
  }

  return createPortal(
    <div ref={overlayRef} className={styles.overlay} style={{ zIndex }} data-personal-access-token-dialog>
      <button
        type="button"
        className={styles.backdrop}
        tabIndex={-1}
        aria-label={t("common:actions.close")}
        disabled={busy}
        onClick={onClose}
      />
      <section
        ref={dialogRef}
        className={`${styles.dialog} ${styles.tokenDialog}`}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-busy={busy || undefined}
      >
        <h2 id={titleId} className={styles.dialogTitle}>{t("personalAccessTokenDialogTitle")}</h2>
        {secret ? (
          <div className={styles.dialogBody}>
            <p className={styles.dialogNote}>{t("personalAccessTokenCreatedNote")}</p>
            <code className={styles.secret} data-personal-access-token-secret>{secret}</code>
            {error ? <div className={styles.error} role="alert">{error}</div> : null}
            <div className={styles.dialogActions}>
              <button type="button" className={styles.secondary} onClick={() => void copySecret()}>
                {t("personalAccessTokenCopy")}
              </button>
              <button type="button" className={styles.primary} onClick={onClose}>{t("common:actions.done")}</button>
            </div>
          </div>
        ) : (
          <form className={styles.dialogBody} onSubmit={(event) => void submit(event)}>
            <p className={styles.dialogNote}>{t("personalAccessTokenDialogDescription")}</p>
            <label className={styles.field}>
              <span className={styles.fieldLabel}>{t("personalAccessTokenNameLabel")}</span>
              <input
                ref={nameRef}
                type="text"
                value={name}
                maxLength={200}
                placeholder={t("personalAccessTokenNamePlaceholder")}
                onChange={(event) => setName(event.target.value)}
              />
            </label>
            <KeyLifetimeRow value={lifetime} onChange={setLifetime} />
            <fieldset className={styles.field}>
              <legend className={styles.fieldLabel}>{t("personalAccessTokenCapabilitiesLabel")}</legend>
              <span className={styles.fieldNote}>{t("personalAccessTokenCapabilitiesNote")}</span>
              <label className={styles.capability}>
                <input type="checkbox" checked disabled readOnly />
                <span>
                  <strong>{t("personalAccessTokenCapabilityApi")}</strong>
                  <span>{t("personalAccessTokenCapabilityApiDescription")}</span>
                </span>
              </label>
            </fieldset>
            {error ? <div className={styles.error} role="alert">{error}</div> : null}
            <div className={`${styles.dialogActions} ${own.actions}`}>
              <button type="submit" className={`${styles.primary} ${own.create}`} disabled={busy || !name.trim()}>
                {t("personalAccessTokenCreate")}
              </button>
            </div>
          </form>
        )}
      </section>
    </div>,
    document.body,
  );
}
