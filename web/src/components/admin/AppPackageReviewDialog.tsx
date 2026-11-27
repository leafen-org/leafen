import { useId, useRef, useState, type RefObject } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { AppInstalledRef, AppPackageReview, AppTenancy } from "@/appPlatformWire.generated";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { formatFileSize } from "../database/files";
import { appDeveloperName, appText } from "./administratorAppsApi";
import { AppDataChanges } from "./AppDataChanges";
import { AppPackageReadingBody } from "./AppPackageReading";
import { AppPermissionList, useRevealedLineFocus } from "./AppPermissionList";
import { AppRowIcon } from "./AppRowIcon";
import { administratorUserStyles as rowStyles } from "./administratorUserManagementStyles";
import { permissionKey, permissionText } from "./appPermissionWords";
import type { AppPackageReading } from "./useAppPackageRead";
import styles from "./AdministratorApps.module.css";
import reviewStyles from "./AppPackageReviewDialog.module.css";

// A review's words by what installing it does to the installed app.
const changeWords: Record<NonNullable<AppInstalledRef["change"]>, { title: string; from: string; action: string }> = {
  update: { title: "apps.review.updateTitle", from: "apps.review.updateFrom", action: "apps.review.update" },
  reinstall: { title: "apps.review.reinstallTitle", from: "apps.review.reinstallFrom", action: "apps.review.reinstall" },
  downgrade: { title: "apps.review.downgradeTitle", from: "apps.review.downgradeFrom", action: "apps.review.downgrade" },
  unchanged: { title: "apps.review.unchangedTitle", from: "apps.review.unchangedFrom", action: "common:actions.close" },
};

type ReviewProps = {
  review: AppPackageReview;
  /** A new app asks here who may use it, so it works once installed. */
  tenancies?: readonly AppTenancy[];
  /** The organization the review was opened in ("org:<id>"). */
  currentTenancy?: string;
  busy: boolean;
  /** The app is in the trash: it comes back first, so nothing installs over it. */
  inTrash: boolean;
  error: string;
  /** personCredentials: whether people may give the app their own account details, for an app that asks for them. */
  onInstall: (acceptDataRemovals: boolean, everyone: readonly string[], personCredentials?: boolean) => void;
  onClose: () => void;
};

/**
 * What an uploaded app asks for, before it is installed: who made it, how
 * far it is trusted, its address, and what it can do in plain words, by what
 * that is about, the widest first. An update says which version it replaces and whether
 * the list changed and what it changes in the app's data; installing
 * approves exactly this list. Until its package is read the same window says
 * what is being read (AppPackageReading.tsx).
 */
export function AppPackageReviewDialog({ review, reading, onReadAgain, returnFocusRef, ...props }: Omit<ReviewProps, "review"> & {
  /** The package's review, once read. */
  review: AppPackageReview | null;
  /** The package while it is read, before its review. */
  reading?: AppPackageReading | null;
  /** Choose another file, or read the catalog's app again, after a read failed. */
  onReadAgain?: () => void;
  /** What opened it when the review came after reading a file ("앱 파일로 설치"): the focus goes back there. */
  returnFocusRef?: RefObject<HTMLElement | null>;
}) {
  const titleId = useId();
  const overlayRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  const focusRef = useRef<HTMLButtonElement>(null);
  const { busy, onClose } = props;
  const { t } = useTranslation(["common"]);
  // Reading, then its review, then a failure take the focus each as they show.
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [overlayRef.current], onEscape: () => { if (!busy) onClose(); }, initialFocus: focusRef,
    returnFocusRef, focusKey: review?.packageHash ?? (reading?.failure === undefined ? "reading" : "failed"),
  });
  return createPortal(
    <div ref={overlayRef} className={styles.overlay} style={{ zIndex }} data-app-review={review ? "" : undefined}
      data-app-package-reading={review ? undefined : reading?.failure === undefined ? "reading" : "failed"}>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("common:actions.close")} disabled={busy} onClick={onClose} />
      <section ref={dialogRef} className={styles.dialog} role="dialog" aria-modal="true" aria-labelledby={titleId}
        aria-busy={busy || (!review && reading?.failure === undefined) || undefined}>
        {review ? <AppPackageReviewBody key={review.packageHash} {...props} review={review} titleId={titleId} installRef={focusRef} />
          : reading ? <AppPackageReadingBody reading={reading} titleId={titleId} focusRef={focusRef} onClose={onClose} onAgain={onReadAgain} />
            : null}
      </section>
    </div>,
    document.body,
  );
}

function AppPackageReviewBody({ review, tenancies = [], currentTenancy, busy, inTrash, error, onInstall, onClose, titleId, installRef }: ReviewProps & {
  titleId: string;
  installRef: RefObject<HTMLButtonElement | null>;
}) {
  const { t, i18n } = useTranslation(["serverConsole", "common"]);
  const [acknowledged, setAcknowledged] = useState(review.trust === "builtin" || review.trust === "catalog");
  const changes = review.dataChanges ?? [];
  const removes = changes.some((change) => change.destructive);
  const [removalsAccepted, setRemovalsAccepted] = useState(false);
  // Everyone in the organization it's installed from may use it at first;
  // other organizations on the server stay off until chosen (permissions
  // narrow by default, 2026-11-06: every organization was on). A server
  // with one organization keeps that one on.
  const [notEveryone, setNotEveryone] = useState<ReadonlySet<string>>(() => new Set(tenancies.length > 1
    ? tenancies.map((tenancy) => tenancy.key).filter((key) => key !== currentTenancy) : []));
  // An app that asks people for their own account details has that on with
  // its approval, as the review says (the person credentials decision of
  // 2026-11-05); the administrator may turn it off here, or later.
  const asksCredentials = review.permissions.some((permission) => permission.kind === "personCredential");
  const [personCredentials, setPersonCredentials] = useState(true);
  const name = appText(review.name, i18n.language);
  const update = review.installed ?? null;
  // What installing it does to the installed app, as the server reckons the
  // versions: an update, the same version again, an earlier one, or nothing
  // at all for the very package installed (2026-11-12: the same version was
  // called "업데이트").
  const change = update ? update.change || "update" : null;
  const unchanged = change === "unchanged";
  // An update shows what it changes; what stays is there to open.
  const [allShown, setAllShown] = useState(update === null);
  const { within: allLines, reveal: revealAll } = useRevealedLineFocus<HTMLDivElement>();
  const changed = update !== null && update.permissionsHash !== review.permissionsHash;
  const order = { high: 0, medium: 1, low: 2 } as const;
  const permissions = [...review.permissions].sort((left, right) => order[left.risk] - order[right.risk]);
  // An update marks the lines it adds and lists those it drops, against the
  // installed version's (2026-10-16: it said only that the list changed).
  const before = update ? new Set(update.permissions.map(permissionKey)) : null;
  const after = new Set(review.permissions.map(permissionKey));
  // Access to a person's data changes in place: moving up to editing takes
  // nothing away, though its reading line goes (2026-11-05).
  const dropped = update ? update.permissions.filter((permission) => !after.has(permissionKey(permission))
    && !(permission.kind === "personAccess" && review.permissions.some((next) => next.kind === "personAccess"))) : [];
  const added = before ? permissions.filter((permission) => !before.has(permissionKey(permission))) : [];
  const linesChanged = dropped.length > 0 || added.length > 0;
  return (
    <>
      {/* The package's own icon, as its row shows once installed, or its first letter. */}
      <div className={reviewStyles.head}>
        <AppRowIcon className={rowStyles.memberAvatar} name={name} iconUrl={review.iconUrl} />
        <h2 id={titleId} className={styles.dialogTitle}>
          {t(change ? changeWords[change].title : "apps.review.title", { name })}
        </h2>
      </div>
      <div className={styles.reviewMeta}>
        {review.trust === "builtin" ? null : <span>{t("apps.by", { developer: appDeveloperName(review.developer.name, i18n.language) })}</span>}
        <span>{t("apps.version", { version: review.version })}</span>
        <span data-app-trust={review.trust}>{t(`apps.trust.${review.trust}`)}</span>
      </div>
      <div className={styles.reviewMeta}>
        <span>{t("apps.review.address", { slug: review.slug })}</span>
        <span>{t("apps.review.files", { count: review.fileCount, size: formatFileSize(review.size) })}</span>
      </div>
      {review.revoked ? (
        <p className={styles.note} role="alert" data-app-review-revoked>
          {t("apps.review.revoked", { reason: review.revokedReason ?? "" })}
        </p>
      ) : (
        <>
          {update && change ? (
            <p className={styles.note} data-app-review-update={change}>
              {t(changeWords[change].from, { current: update.version, next: review.version, version: review.version })}
            </p>
          ) : null}
          {changed ? (
            <p className={styles.note} data-app-review-changed>
              {t(linesChanged ? "apps.review.permissionsChanged" : "apps.review.permissionsDetailsChanged")}
            </p>
          ) : update && !unchanged ? <p className={styles.note} data-app-review-same>{t("apps.review.permissionsSame")}</p> : null}
          {review.nameLooksLike ? (
            <p className={styles.nameWarning} data-app-review-name-looks-like>{t("apps.review.nameLooksLike", {
              name: review.nameLooksLike === "Leafen" ? t("apps.review.leafen") : review.nameLooksLike,
            })}</p>
          ) : null}
          {review.needsLeafen ? (
            <p className={styles.note} data-app-review-needs-leafen>{t("apps.review.needsLeafen", { version: review.needsLeafen })}</p>
          ) : null}
          {allShown ? (
            <>
              <div className={styles.permissionsTitle}>{t("apps.review.permissionsTitle")}</div>
              {permissions.length ? (
                <AppPermissionList ref={allLines} permissions={permissions} t={t} language={i18n.language}
                  added={changed && before ? (permission) => !before.has(permissionKey(permission)) : undefined} />
              ) : <p className={styles.note}>{t("apps.review.noPermissions")}</p>}
            </>
          ) : (
            <>
              {added.length ? (
                <>
                  <div className={styles.permissionsTitle}>{t("apps.review.permissionsAdded")}</div>
                  <AppPermissionList permissions={added} t={t} language={i18n.language} />
                </>
              ) : null}
              {permissions.length ? (
                <button type="button" className={styles.permissionsMore} style={{ marginInlineStart: 0 }} data-app-permissions-all
                  onClick={(event) => { setAllShown(true); revealAll(event); }}>{t("apps.review.showAllPermissions")}</button>
              ) : null}
            </>
          )}
          {dropped.length ? (
            <>
              <div className={styles.permissionsTitle}>{t("apps.review.permissionsDropped")}</div>
              <ul className={styles.permissions} data-app-permissions-dropped>
                {dropped.map((permission, index) => (
                  <li key={`${permission.kind}-${index}`} className={reviewStyles.dropped} data-app-permission={permission.kind}>
                    {permissionText(permission, t, i18n.language)}
                  </li>
                ))}
              </ul>
            </>
          ) : null}
          <AppDataChanges changes={changes} accepted={removalsAccepted} busy={busy} onAccept={setRemovalsAccepted} />
          {!update && tenancies.length ? (
            <>
              <div className={styles.permissionsTitle}>{t("apps.review.whoTitle")}</div>
              {tenancies.map((tenancy) => (
                <label key={tenancy.key} className={styles.acknowledge}>
                  <input type="checkbox" checked={!notEveryone.has(tenancy.key)} disabled={busy} data-app-review-everyone={tenancy.key}
                    onChange={(event) => setNotEveryone((held) => {
                      const next = new Set(held);
                      if (event.target.checked) next.delete(tenancy.key);
                      else next.add(tenancy.key);
                      return next;
                    })} />
                  {t("apps.everyoneIn", { tenancy: tenancy.name || t("apps.serverTenancy") })}
                </label>
              ))}
              <p className={styles.note}>{t("apps.review.whoNote")}</p>
            </>
          ) : null}
          {!update && asksCredentials ? (
            <>
              <div className={styles.permissionsTitle}>{t("apps.credentials.title")}</div>
              <label className={styles.acknowledge}>
                <input type="checkbox" checked={personCredentials} disabled={busy} data-app-review-credentials
                  onChange={(event) => setPersonCredentials(event.target.checked)} />
                {t("apps.credentials.enabled")}
              </label>
              <p className={styles.note}>{t("apps.review.credentialsNote")}</p>
            </>
          ) : null}
        </>
      )}
      {inTrash ? <div className={styles.error} role="alert">{t("apps.review.inTrash")}</div> : null}
      {error ? <div className={styles.error} role="alert">{error}</div> : null}
      <div className={styles.dialogActions}>
        {/* What "설치" waits for stands beside it at the window's foot, in
            sight however long the review above (2026-11-08: under the fold,
            the button showed off with no reason). */}
        {!review.revoked && !unchanged && (review.trust === "signed" || review.trust === "unsigned") ? (
          <label className={styles.acknowledge}>
            <input type="checkbox" checked={acknowledged} disabled={busy} data-app-review-acknowledge
              onChange={(event) => setAcknowledged(event.target.checked)} />
            {t("apps.review.unreviewedWarning")}
          </label>
        ) : null}
        {/* The very package installed has nothing to install: the window only closes. */}
        {unchanged ? (
          <button ref={installRef} type="button" className={styles.primary} data-app-review-close onClick={onClose}>{t("common:actions.close")}</button>
        ) : (<>
          <button type="button" className={styles.cancel} disabled={busy} onClick={onClose}>{t("common:actions.cancel")}</button>
          <button ref={installRef} type="button" className={styles.primary}
            disabled={busy || inTrash || Boolean(review.revoked) || !acknowledged || Boolean(review.needsLeafen) || removes && !removalsAccepted} data-app-review-install
            onClick={() => onInstall(removes && removalsAccepted, tenancies.map((tenancy) => tenancy.key).filter((key) => !notEveryone.has(key)),
              !update && asksCredentials ? personCredentials : undefined)}>
            {t(change ? changeWords[change].action : "apps.review.install")}
          </button>
        </>)}
      </div>
    </>
  );
}

