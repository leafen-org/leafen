import { type RefObject, useId, useRef } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { AppConsent } from "@/appPlatformWire.generated";
import { AppPermissionList } from "@/components/admin/AppPermissionList";
import { permissionKey } from "@/components/admin/appPermissionWords";
import { appMaker, appText } from "@/components/admin/administratorAppsApi";
import { useOverlayLayer } from "@/lib/overlayLayer";
import styles from "@/components/admin/AdministratorApps.module.css";

/**
 * A person's own consent to an app they add themselves (docs/app-platform.md
 * 3): who made it and each thing it may do, the risky ones first; agreeing
 * accepts exactly this list. Asked again when the list changed since.
 */
export function AppConsentDialog({ consent, again, busy, error, onAgree, onClose, returnFocusRef }: {
  consent: AppConsent;
  /** What opened it, when the window comes after an answer (the "추가" the
   * person pressed): the focus goes back there when it closes. */
  returnFocusRef?: RefObject<HTMLElement | null>;
  /** Agreeing again because what the app may do changed. */
  again: boolean;
  busy: boolean;
  error: string;
  onAgree: () => void;
  onClose: () => void;
}) {
  const { t, i18n } = useTranslation(["common", "serverConsole"]);
  const titleId = useId();
  const overlayRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  const agreeRef = useRef<HTMLButtonElement>(null);
  const { zIndex } = useOverlayLayer(true, {
    kind: "alertdialog", ref: dialogRef, modalOwned: () => [overlayRef.current], onEscape: () => { if (!busy) onClose(); }, initialFocus: agreeRef,
    returnFocusRef,
  });
  const order = { high: 0, medium: 1, low: 2 } as const;
  const permissions = [...consent.permissions].sort((left, right) => order[left.risk] - order[right.risk]);
  // Agreeing again marks what the version they agreed to didn't ask for, as
  // an update's review does (2026-11-05: it said only that something changed).
  const before = again && consent.previousPermissions ? new Set(consent.previousPermissions.map(permissionKey)) : null;
  const name = appText(consent.name, i18n.language);
  const serverConsole = (key: string, values?: Record<string, unknown>) => t(`serverConsole:${key}`, values);
  return createPortal(
    <div ref={overlayRef} className={styles.overlay} style={{ zIndex }} data-app-consent>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("common:actions.close")} disabled={busy} onClick={onClose} />
      <section ref={dialogRef} className={styles.dialog} role="dialog" aria-modal="true" aria-labelledby={titleId} aria-busy={busy || undefined}>
        <h2 id={titleId} className={styles.dialogTitle}>{t(again ? "appConsent.againTitle" : "appConsent.title", { name })}</h2>
        <div className={styles.reviewMeta}>
          {/* Who it is from, as every other screen says it (appMaker). */}
          <span>{((maker) => maker.builtin ? t("serverConsole:apps.trust.builtin") : t("serverConsole:apps.by", { developer: maker.developer }))(
            appMaker(consent.developer, consent.builtin, i18n.language))}</span>
        </div>
        {again ? <p className={styles.note} data-app-consent-changed>{t("appConsent.changed")}</p> : null}
        <div className={styles.permissionsTitle}>{t("serverConsole:apps.review.permissionsTitle")}</div>
        {permissions.length ? (
          <AppPermissionList permissions={permissions} t={serverConsole} language={i18n.language}
            added={before ? (permission) => !before.has(permissionKey(permission)) : undefined} />
        ) : <p className={styles.note}>{t("serverConsole:apps.review.noPermissions")}</p>}
        <p className={styles.note}>{t("appConsent.note")}</p>
        {error ? <div className={styles.error} role="alert">{error}</div> : null}
        <div className={styles.dialogActions}>
          <button type="button" className={styles.cancel} disabled={busy} onClick={onClose}>{t("common:actions.cancel")}</button>
          <button ref={agreeRef} type="button" className={styles.primary} disabled={busy} data-app-consent-agree onClick={onAgree}>
            {t(again ? "appConsent.agreeOpen" : "appConsent.agreeAdd")}
          </button>
        </div>
      </section>
    </div>,
    document.body,
  );
}
