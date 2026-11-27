import { WorkspaceIconGlyph, workspaceIconLetter } from "@/components/page/PageIcon";
import { type FormEvent, useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { LeafenApiError } from "@/apiTransport";
import {
  authErrorCodes,
  type AccountDeletionPreview,
  type AccountDeletionWorkspace,
} from "@/authWire.generated";
import { WarningCircleIcon } from "@/icons/leafen";
import { removePersonValues } from "@/lib/browserStorage";
import { newId } from "@/lib/ids";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { fieldFocusOnOpen, touchScreen } from "@/lib/screenKind";
import type { Session } from "@/types";
import { styles } from "./accountSettingsStyles";
import deletionStyles from "./AccountDeletion.module.css";
import cardStyles from "./AccountCardDialog.module.css";
import emailStyles from "./AccountEmailManagement.module.css";
import devices from "./AccountDevices.module.css";

// Deleting one's own account, as Notion's "Delete my account" does: a row
// under "지원" (seen 2026-10-12) whose button opens a dialog that names the
// workspaces deleted with it and those the person leaves, and they type
// their account email to delete it. The server signs them out at once
// and deletes the rest behind them.
export function AccountDeletionSection({ session, onDeleted }: { session: Session; onDeleted: () => void }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const headingId = useId();
  const [open, setOpen] = useState(false);
  return (
    <div className={styles.securityRow} role="group" aria-labelledby={headingId}>
      <div className={styles.securityInstruction}>
        <strong id={headingId}>{t("accountDeletionTitle")}</strong>
        <span>{t("accountDeletionDesc")}</span>
      </div>
      <button type="button" className={devices.softDanger} onClick={() => setOpen(true)}>
        {t("accountDeletionOpen")}
      </button>
      {open ? <AccountDeletionDialog session={session} onClose={() => setOpen(false)} onDeleted={onDeleted} /> : null}
    </div>
  );
}

function AccountDeletionDialog({ session, onClose, onDeleted }: {
  session: Session;
  onClose: () => void;
  onDeleted: () => void;
}) {
  const { t } = useTranslation(["workspaceSettingsDialog", "common"]);
  const titleId = useId();
  const emailId = useId();
  const overlayRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  const requestId = useRef(newId());
  const [preview, setPreview] = useState<AccountDeletionPreview | null>(null);
  const [email, setEmail] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  // A modal layer: the page stays still and Escape closes it while nothing
  // opened over it takes the key.
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [overlayRef.current], onEscape: () => { if (!busy) onClose(); }, initialFocus: dialogRef,
  });

  useEffect(() => {
    const controller = new AbortController();
    api.readAccountDeletion(session, controller.signal).then(setPreview).catch(() => {
      if (!controller.signal.aborted) setError(t("accountDeletionFailed"));
    });
    return () => controller.abort();
  }, [session, t]);

  const blocked = preview !== null && preview.blocker !== "none";
  // An account with no email confirms with its name, as the server checks.
  const confirmWith = preview?.email || session.user.name.trim();
  const matches = preview !== null && confirmWith !== "" && (preview.email
    ? email.trim().toLowerCase() === preview.email.toLowerCase()
    : email.trim() === confirmWith);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!preview || blocked || !matches || busy) return;
    setBusy(true);
    setError("");
    try {
      await api.deleteAccount(session, { requestId: requestId.current, email: email.trim() });
      // What the person kept on this browser goes with the account.
      removePersonValues(session.user.id);
      onDeleted();
    } catch (cause) {
      const code = cause instanceof LeafenApiError ? cause.code : "";
      setError(t(
        code === authErrorCodes.accountDeletionConfirmationMismatch ? "accountDeletionMismatch"
          : code === authErrorCodes.accountDeletionBlocked ? "accountDeletionFailed"
            : code === "LEAFEN_WORKSPACE_DELETE_CONFLICT" ? "accountDeletionBusy"
              : code === "LEAFEN_LEGAL_HOLD_PREVENTS_PERMANENT_DELETE" ? "accountDeletionLegalHold"
                : "accountDeletionFailed",
      ));
      setBusy(false);
      // What blocks it now, read again: its line says which hold, whose.
      if (code === authErrorCodes.accountDeletionBlocked || code === "LEAFEN_LEGAL_HOLD_PREVENTS_PERMANENT_DELETE") {
        void api.readAccountDeletion(session).then((next) => {
          setPreview(next);
          if (next.blocker !== "none") setError("");
        }).catch(() => undefined);
      }
    }
  }

  // Notion's deletion card (seen 2026-10-30, 420px): a red mark over the
  // question and its words, the workspaces that go with the account, the
  // email that confirms it, and the two buttons stacked full width.
  const dialog = (
    <div ref={overlayRef} className={emailStyles.backdrop} style={{ zIndex }}
      onMouseDown={(event) => { if (event.target === event.currentTarget && !busy) onClose(); }}>
      <section
        ref={dialogRef}
        className={`${emailStyles.dialog} ${cardStyles.card}`}
        style={{ width: 420 }}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-busy={busy || preview === null || undefined}
        tabIndex={-1}
      >
        <span className={cardStyles.mark} data-tone="danger" aria-hidden="true"><WarningCircleIcon size="card" /></span>
        <h2 id={titleId} className={cardStyles.title}>{t("accountDeletionDialogTitle")}</h2>
        <p className={cardStyles.words}>{t(preview?.deletedWorkspaceCount ? "accountDeletionWarning" : "accountDeletionWarningNoWorkspaces")}</p>
        <form onSubmit={submit}>
          {preview === null ? (
            <p className={deletionStyles.note} role="status">{error ? null : t("accountDeletionLoading")}</p>
          ) : (
            <>
              {preview.deletedWorkspaceCount > 0 ? (
                <WorkspaceList workspaces={preview.deletedWorkspaces} count={preview.deletedWorkspaceCount} />
              ) : null}
              {blocked ? (
                <p className={deletionStyles.blocked} role="alert">
                  {preview.blocker === "last_administrator"
                    ? t("accountDeletionBlockedLastAdministrator")
                    : preview.blocker === "legal_hold"
                      ? t("accountDeletionLegalHold")
                      // A hold naming the person, not a workspace of theirs, says so (2026-11-13).
                      : preview.blocker === "legal_hold_custodian"
                        ? t("accountDeletionLegalHoldPerson")
                        : t("accountDeletionBlockedSharedOrganization")}
                </p>
              ) : (
                <div className={deletionStyles.confirm}>
                  <label htmlFor={emailId}>{t(preview.email ? "accountDeletionConfirmLabel" : "accountDeletionConfirmNameLabel")}</label>
                  <input
                    id={emailId}
                    className={cardStyles.field}
                    data-look="boxed"
                    type={preview.email ? "email" : "text"}
                    value={email}
                    placeholder={confirmWith}
                    autoComplete="off"
                    spellCheck={false}
                    // A touch screen's opens with the keyboard down, as Notion's iPad (seen 2026-11-13).
                    autoFocus={fieldFocusOnOpen()}
                    disabled={busy}
                    onChange={(event) => {
                      setEmail(event.target.value);
                      if (error) setError("");
                    }}
                    {...(error === t("accountDeletionMismatch") ? { "aria-invalid": true as const, "aria-describedby": `${emailId}-error` } : {})}
                  />
                </div>
              )}
            </>
          )}
          {error ? <p id={`${emailId}-error`} className={cardStyles.error} role="alert">{error}</p> : null}
          <div className={`${cardStyles.stack} ${cardStyles.confirmStack}`}>
            <button type="submit" className={cardStyles.wide} data-kind="danger" disabled={!preview || blocked || !matches || busy}>
              {busy
                ? t("accountDeletionDeleting")
                // With no workspace of one's own to go, the button names the account alone, not "워크스페이스 0개".
                // A touch screen's says "삭제" without "영구", as Notion's phone and iPad (seen 2026-11-13).
                : preview?.deletedWorkspaceCount ? t(touchScreen() ? "accountDeletionConfirmTouch" : "accountDeletionConfirm", { count: preview.deletedWorkspaceCount })
                  : t(touchScreen() ? "accountDeletionConfirmAccountOnlyTouch" : "accountDeletionConfirmAccountOnly")}
            </button>
            <button type="button" className={cardStyles.wide} data-kind="cancel" onClick={onClose} disabled={busy}>
              {t("common:actions.cancel")}
            </button>
          </div>
        </form>
      </section>
    </div>
  );

  return typeof document === "undefined" ? dialog : createPortal(dialog, document.body);
}

function WorkspaceList({ workspaces, count }: {
  workspaces: AccountDeletionWorkspace[];
  count: number;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  return (
    <div className={deletionStyles.workspaces}>
      <ul>
        {workspaces.map((workspace) => (
          <li key={workspace.id}>
            <span className={deletionStyles.workspaceIcon} aria-hidden="true"><WorkspaceIconGlyph icon={workspace.icon} size={20} fallback={workspaceIconLetter(workspace.name)} /></span>
            <span className={deletionStyles.workspaceText}>
              <span>{workspace.name}</span>
              <small>{t("accountDeletionWorkspaceMembers", { count: workspace.memberCount })}</small>
            </span>
          </li>
        ))}
      </ul>
      {count > workspaces.length ? (
        <span className={deletionStyles.note}>{t("accountDeletionMore", { count: count - workspaces.length })}</span>
      ) : null}
    </div>
  );
}
