import { FormEvent, Suspense, useId, useRef, useState } from "react";
import { writeUncertainMark } from "@/lib/settingsErrors";
import { lazyScreen } from "@/lib/lazyScreen";
import { useTranslation } from "react-i18next";
import { ErrorBoundary } from "@/components/ErrorBoundary";
import { WorkspaceIconGlyph, workspaceIconKind, workspaceIconLetter } from "@/components/page/PageIcon";
import { PageIconPicker } from "@/components/page/PageIconPicker";
import { keptIconPicture, keptIconUploadTarget } from "@/components/teamspace/KeptIconGlyph";
import { keyCameLast } from "@/lib/lastPress";
import type { Session, Workspace } from "@/types";
import { api } from "@/api";
import {
  workspaceDeletionDeletedRecords,
  workspaceDeletionPhaseKey,
  workspaceUrlPrefix,
} from "./workspaceSettingsOverviewModel";
import { WorkspaceAllowedEmailDomains } from "./WorkspaceAllowedEmailDomains";
import { WorkspaceLandingPage } from "./WorkspaceLandingPage";
import { WorkspaceMoveSetting } from "./WorkspaceMoveSetting";
import { WorkspacePageViewAnalytics } from "./WorkspacePageViewAnalytics";
import { WorkspacePeopleProfileSettings } from "./WorkspacePeopleProfileSettings";
import { WorkspacePeopleCsvExport } from "./WorkspacePeopleCsvExport";
import { styles } from "./workspaceSettingsOverviewStyles";
import general from "./WorkspaceGeneral.module.css";
import cardStyles from "@/components/account/AccountCardDialog.module.css";
import deletionStyles from "@/components/account/AccountDeletion.module.css";
import { AccountSecurityDialog } from "@/components/account/AccountSecurityDialog";
import { Copy, WarningCircleIcon } from "@/icons/leafen";
import { IconTipButton } from "@/components/IconTipButton";
import { copyText } from "@/lib/copyText";
import { useOptionalProductToast } from "@/lib/productToast";
import idStyles from "./WorkspaceIdRow.module.css";
import { workspaceMembershipBounds } from "@/workspaceMembershipWire.generated";
import { useWorkspaceSettingsOverview } from "./useWorkspaceSettingsOverview";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

const WorkspaceExportDialog = lazyScreen(() =>
  import("@/components/WorkspaceExportDialog").then(({ WorkspaceExportDialog }) => ({
    default: WorkspaceExportDialog,
  })),
);

export function WorkspaceSettingsOverview({
  session,
  workspace,
  workspaces,
  canManageWorkspace,
  onWorkspaceChange,
  onWorkspaceDeleted,
  onOpenPage,
}: {
  session: Session;
  workspace: Workspace;
  workspaces: Workspace[];
  canManageWorkspace: boolean;
  onWorkspaceChange: (workspace: Workspace) => void;
  onWorkspaceDeleted: (workspaceId: string) => void | Promise<void>;
  /** Leaves the settings for a page, as a landing page just made opens. */
  onOpenPage?: (pageId: string) => void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [iconPickerOpen, setIconPickerOpen] = useState(false);
  const [exportOpen, setExportOpen] = useState(false);
  const iconButtonRef = useRef<HTMLButtonElement>(null);
  const exportButtonRef = useRef<HTMLButtonElement>(null);
  const workspaceSectionId = useId();
  const domainStatusId = useId();
  const iconLabelId = useId();
  const nameId = useId();
  const deleteConfirmId = useId();
  // The deletion window holds the workspace it was opened for from the name
  // typed to the end, though the settings move to another as it starts
  // (2026-11-11: it named the next workspace, and a failure left it open
  // asking to delete that one).
  const [deleteTarget, setDeleteTarget] = useState<{ id: string; name: string } | null>(null);
  const urlId = useId();
  const deleteWorkspaceSectionId = useId();
  const overview = useWorkspaceSettingsOverview({
    session,
    workspace,
    workspaces,
    canManageWorkspace,
    onWorkspaceChange,
    onWorkspaceDeleted,
    message: t,
  });
  // A legal hold that would refuse the deletion, said as the window opens
  // rather than after the name is typed (2026-11-06).
  const [deleteHeld, setDeleteHeld] = useState(false);
  const openDelete = () => {
    overview.setDeleteWorkspaceConfirm("");
    overview.setDeleteWorkspaceError("");
    setDeleteHeld(false);
    setDeleteTarget({ id: workspace.id, name: workspace.name });
    api.readWorkspaceDeletionPreview(session, workspace.id).then((preview) => setDeleteHeld(preview.legalHold), () => undefined);
  };
  const activeDeletion = overview.workspaceDeletion?.workspaceId === workspace.id
    ? overview.workspaceDeletion
    : undefined;
  const targetDeletion = deleteTarget && overview.workspaceDeletion?.workspaceId === deleteTarget.id
    ? overview.workspaceDeletion
    : undefined;
  const closeDelete = () => setDeleteTarget(null);
  // The window stays, busy, until the server takes the deletion, and says why
  // if that fails (its button then goes on with that workspace); once taken
  // the person leaves the workspace and the window with the settings, as
  // Notion's, while the rest of the deletion goes on quietly
  // (useWorkspaceSettingsOverview).
  const deleteWindowShown = Boolean(deleteTarget) && targetDeletion?.status !== "completed";
  const deletionPaused = targetDeletion?.status === "retryable";
  const workspaceDeletionRunning = activeDeletion?.status === "deleting";
  const workspaceDeletionCanResume = activeDeletion?.status === "retryable";
  const workspaceUrlPrefixValue = workspaceUrlPrefix();

  // Closed, the icon picker gives its button the keys back. Escape on one
  // pressed open leaves the page's body focused with no ring, a typed letter
  // landing nowhere, as Notion's (설정 › 일반 › 아이콘, measured twice on
  // 2026-11-15 in Chrome 1440 in different tabs, the screen and one typed
  // letter after the key, no script before it); opened from the keys, Escape
  // still gives its button the keys back (Notion's not measured there).
  const iconOpenedByKey = useRef(false);
  function closeIconPicker(restore = true) {
    setIconPickerOpen(false);
    if (restore) window.requestAnimationFrame(() => iconButtonRef.current?.focus());
  }
  const escapeIconPicker = () => closeIconPicker(iconOpenedByKey.current);

  function submitDeleteWorkspace(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    void (targetDeletion?.status === "retryable" ? overview.resumeWorkspaceDeletion() : overview.submitDeleteWorkspace());
  }

  // In Notion's groups (seen 2026-10-11, 사람 2026-10-12): 워크스페이스 설정 ·
  // 허용된 이메일 도메인 · 내보내기 · 애널리틱스 · 사람 · 위험 구역 · 워크스페이스 ID.
  // A save that failed is told under its own control, not above the section:
  // a box there moved what was pressed (2026-11-06).
  const failure = (at: "name" | "icon" | "landing") => overview.workspaceError && overview.workspaceErrorAt === at
    ? <div className={general.message} data-tone="error" role="alert" {...writeUncertainMark(overview.workspaceError)}>{overview.workspaceError}</div> : null;
  return (
    <>
      <section id="workspace" className={styles.section} aria-labelledby={workspaceSectionId}>
        <div id={workspaceSectionId} className={styles.sectionTitle}>{t("workspaceSectionTitle")}</div>
        {/* Notion's name, then its icon, each over its words and above its control (seen 2026-10-30). */}
        <div className={general.stacked}>
          <label htmlFor={nameId}>{t("workspaceNameField")}</label>
          <p>{t("workspaceNameHint", { count: workspaceMembershipBounds.creationNameRunes })}</p>
          <div className={general.box}>
            <input
              id={nameId}
              value={overview.name}
              maxLength={workspaceMembershipBounds.creationNameRunes}
              disabled={!canManageWorkspace}
              onChange={(event) => overview.setName(event.target.value)}
            />
            {/* Saved only from here, not by Enter or leaving the field, as Notion's. */}
            {canManageWorkspace && overview.name.trim() && overview.name !== workspace.name
              ? <button type="button" className={general.boxSave} onClick={() => void overview.saveName()}>{t("saveName")}</button> : null}
          </div>
          {failure("name")}
        </div>

        <div className={general.stacked}>
          <label htmlFor={urlId}>{t("workspaceUrlField")}</label>
          <div className={general.box} data-invalid={overview.domainError ? "true" : undefined} data-busy={overview.domainBusy ? "true" : undefined}>
            <span title={workspaceUrlPrefixValue}>{workspaceUrlPrefixValue}</span>
            <input
              id={urlId}
              value={overview.domain}
              aria-describedby={overview.domainError || overview.domainBusy ? domainStatusId : undefined}
              aria-invalid={overview.domainError ? "true" : undefined}
              disabled={!canManageWorkspace || overview.domainBusy}
              spellCheck={false}
              placeholder={t("placeholderWorkspaceName")}
              onChange={(event) => {
                overview.setDomain(event.target.value);
                if (overview.domainError) overview.setDomainError("");
              }}
              onBlur={() => void overview.saveDomain()}
              onKeyDown={(event) => {
                if (imeComposing(event)) return;
                if (fieldEnter(event)) event.currentTarget.blur();
              }}
            />
          </div>
          {overview.domainError ? (
            <div id={domainStatusId} className={general.message} data-tone="error" role="alert">{overview.domainError}</div>
          ) : overview.domainBusy ? (
            <div id={domainStatusId} className={general.message}>{t("workspaceUrlSaving")}</div>
          ) : null}
        </div>

        <div className={general.stacked} role="group" aria-labelledby={iconLabelId}>
          <strong id={iconLabelId}>{t("iconField")}</strong>
          <p>{t("workspaceIconHint")}</p>
          <div
            className={styles.iconPickerWrap}
            onKeyDown={(event) => {
              if (event.key !== "Escape" || !iconPickerOpen) return;
              event.preventDefault();
              event.stopPropagation();
              escapeIconPicker();
            }}
          >
            <button
              ref={iconButtonRef}
              type="button"
              className={general.iconBox}
              data-workspace-icon={workspaceIconKind(overview.icon)}
              data-waiting-note={overview.iconNote}
              aria-haspopup="dialog"
              aria-expanded={iconPickerOpen}
              aria-label={t("ariaChangeWorkspaceIcon")}
              disabled={!canManageWorkspace}
              onClick={() => { iconOpenedByKey.current = keyCameLast(); setIconPickerOpen((current) => !current); }}
            >
              {overview.icon.trim() ? <WorkspaceIconGlyph icon={overview.icon} size={64} fallback="" />
                : <span className={general.iconLetter} data-workspace-icon-letter>{workspaceIconLetter(workspace.name)}</span>}
            </button>
            {iconPickerOpen ? (
              <PageIconPicker
                placement="inline" current={overview.icon}
                returnFocusFromKeysOnly
                uploadTarget={keptIconUploadTarget(workspace.id, "workspace")}
                onPick={(icon) => { void overview.updateIcon(icon); closeIconPicker(); }}
                onPickBuiltInIcon={(icon) => { void overview.updateIcon(icon); closeIconPicker(); }}
                onPickImage={(value) => {
                  const picture = keptIconPicture(value);
                  if (!picture) return;
                  void overview.updateIcon(picture);
                  closeIconPicker();
                }}
                onRemove={() => { void overview.updateIcon(""); closeIconPicker(); }}
                onClose={() => (keyCameLast() ? escapeIconPicker() : closeIconPicker())}
              />
            ) : null}
          </div>
          {failure("icon")}
        </div>

        <WorkspaceLandingPage session={session} workspace={workspace} canManage={canManageWorkspace}
          onChoose={overview.updateLandingPage} onOpenPage={onOpenPage} />
        {failure("landing")}
      </section>

      {canManageWorkspace ? (
        <section className={styles.section} aria-label={t("allowedEmailDomainsTitle")}>
          <div className={styles.sectionTitle}>{t("allowedEmailDomainsTitle")}</div>
          <WorkspaceAllowedEmailDomains session={session} workspaceId={workspace.id} />
        </section>
      ) : null}

      {/* The workspace's content exports from here, as in Notion's
          "일반" (seen 2026-10-02), then its members and guests as CSV
          (2026-10-11); only its owner exports them. Its button reads
          "내보내기", the name of the window it opens, as Notion's
          (2026-11-06). */}
      {canManageWorkspace ? (
        <section className={styles.section} aria-label={t("exportTitle")} data-workspace-export>
          <div className={styles.sectionTitle}>{t("exportTitle")}</div>
          <div className={styles.securityRow}>
            <div className={styles.securityInstruction}>
              <strong>{t("exportWorkspaceContent")}</strong>
              <span>{t("exportWorkspaceContentDesc", { workspace: workspace.name || t("untitledWorkspace") })}</span>
            </div>
            <button
              ref={exportButtonRef}
              type="button"
              className={styles.secondaryButton}
              onClick={() => setExportOpen(true)}
            >
              {t("exportTitle")}
            </button>
          </div>
          <WorkspacePeopleCsvExport session={session} workspace={workspace} />
        </section>
      ) : null}
      {/* Moving the workspace to another Leafen server, or one here
          (workspace_move_in.go, workspace_move_out.go); its owner moves it. */}
      {canManageWorkspace ? <WorkspaceMoveSetting session={session} workspace={workspace} /> : null}
      {exportOpen ? (
        <Suspense fallback={null}>
          <ErrorBoundary scope="workspace-export-dialog">
            <WorkspaceExportDialog
              session={session}
              workspaceId={workspace.id}
              workspaceName={workspace.name}
              onClose={() => setExportOpen(false)}
              onRestoreFocus={() => exportButtonRef.current?.focus()}
            />
          </ErrorBoundary>
        </Suspense>
      ) : null}

      {canManageWorkspace ? (
        <section className={styles.section} aria-label={t("analyticsSectionTitle")}>
          <div className={styles.sectionTitle}>{t("analyticsSectionTitle")}</div>
          <WorkspacePageViewAnalytics session={session} workspace={workspace} onWorkspaceChange={onWorkspaceChange} />
        </section>
      ) : null}

      {canManageWorkspace ? (
        <section className={styles.section} aria-label={t("peopleSectionTitle")}>
          <div className={styles.sectionTitle}>{t("peopleSectionTitle")}</div>
          <WorkspacePeopleProfileSettings session={session} workspace={workspace} />
        </section>
      ) : null}

      <section className={styles.section} aria-label={t("dangerZoneTitle")}>
        <div className={styles.sectionTitle}>{t("dangerZoneTitle")}</div>
        {/* Notion's row (seen 2026-10-30): its name and words, and a red-tinted
            button that opens the card where the workspace's name confirms it.
            Notion asks first why, for its own team; Leafen runs no service to
            hear it (2026-10-19). A deletion under way shows here instead. */}
        <div className={styles.securityRow} aria-labelledby={deleteWorkspaceSectionId} data-workspace-danger>
          <div className={styles.securityInstruction}>
            <strong id={deleteWorkspaceSectionId}>{t("deleteWorkspace")}</strong>
            <span>{t("deleteWorkspaceDesc")}</span>
          </div>
          {activeDeletion ? (
            <div className={styles.deletionProgress} role="status" aria-label={t("deleteWorkspaceProgressAria")} aria-live="polite">
              <div className={styles.deletionProgressCopy}>
                <strong>{workspaceDeletionCanResume ? t("deletionPaused") : t("deletingWorkspaceTitle")}</strong>
                <span>{activeDeletion.error ?? t(workspaceDeletionPhaseKey(activeDeletion.result?.phase))}</span>
              </div>
              <span className={styles.deletionProgressCount}>
                {t("deletedRecordsCount", { count: workspaceDeletionDeletedRecords(activeDeletion.result) })}
              </span>
              {workspaceDeletionRunning ? <progress aria-label={t("deletingWorkspaceTitle")} /> : null}
              {workspaceDeletionCanResume ? (
                <button type="button" className={general.danger} disabled={overview.deleteWorkspaceBusy}
                  onClick={() => void overview.resumeWorkspaceDeletion()}>{t("resumeWorkspaceDeletion")}</button>
              ) : null}
            </div>
          ) : (
            <button type="button" className={general.danger} disabled={!overview.canDeleteWorkspace}
              onClick={openDelete}>
              {t("deleteWorkspace")}
            </button>
          )}
        </div>
        {!overview.canDeleteWorkspace && !activeDeletion ? (
          <div className={general.message}>{workspaces.length <= 1 ? t("needAnotherWorkspace") : t("onlyAdminsDelete")}</div>
        ) : null}
        {deleteWindowShown && deleteTarget ? (
          <AccountSecurityDialog title={t("deleteWorkspaceCardTitle")} busy={overview.deleteWorkspaceBusy} onClose={closeDelete}
            card={{ mark: <WarningCircleIcon size="card" />, tone: "danger", width: 420, words: t("deleteWorkspaceCardWords", { workspace: deleteTarget.name || t("untitledWorkspace") }) }}>
            <form onSubmit={submitDeleteWorkspace}>
              {/* Said where the name would go, as the account deletion window says its blockers. */}
              {deleteHeld ? <p className={deletionStyles.blocked} role="alert" data-workspace-delete-held>{t("settingsErrors:backend.legalHoldKeeps")}</p> : null}
              {deleteHeld || targetDeletion ? null : <div className={general.confirm}>
                <label htmlFor={deleteConfirmId}>{t("deleteWorkspaceConfirmLabel")}</label>
                <input
                  id={deleteConfirmId}
                  className={cardStyles.field}
                  data-look="boxed"
                  value={overview.deleteWorkspaceConfirm}
                  autoComplete="off"
                  spellCheck={false}
                  placeholder={deleteTarget.name}
                  disabled={overview.deleteWorkspaceBusy}
                  onChange={(event) => {
                    overview.setDeleteWorkspaceConfirm(event.target.value);
                    if (overview.deleteWorkspaceError) overview.setDeleteWorkspaceError("");
                  }}
                />
              </div>}
              {(targetDeletion?.error ?? overview.deleteWorkspaceError) ? <p className={cardStyles.error} role="alert">{targetDeletion?.error ?? overview.deleteWorkspaceError}</p> : null}
              <div className={`${cardStyles.stack} ${cardStyles.confirmStack}`}>
                <button type="submit" className={cardStyles.wide} data-kind="danger"
                  disabled={overview.deleteWorkspaceBusy || deleteHeld || (!deletionPaused && !overview.deleteWorkspaceConfirmMatches)}>
                  {overview.deleteWorkspaceBusy ? t("deleting") : deletionPaused ? t("resumeWorkspaceDeletion") : t("deleteWorkspaceCardConfirm")}
                </button>
                <button type="button" className={cardStyles.wide} data-kind="cancel" disabled={overview.deleteWorkspaceBusy}
                  onClick={closeDelete}>{t("common:actions.cancel")}</button>
              </div>
            </form>
          </AccountSecurityDialog>
        ) : null}
      </section>

      <WorkspaceIdSection workspaceId={workspace.id} />
    </>
  );
}

/**
 * The workspace's ID, the end of 일반 for everyone, as Notion's (seen 2026-11-21 in the business trial):
 * one line — "워크스페이스 ID" for its owner, "ID" for a member — with the ID and a button that copies it,
 * whose tip asks to click; a copy is told in a toast, as Notion's "워크스페이스 ID 클립보드에 복사됨".
 */
export function WorkspaceIdSection({ workspaceId, member = false }: { workspaceId: string; member?: boolean }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const toast = useOptionalProductToast();
  return (
    <section className={styles.section} aria-label={t("workspaceIdTitle")} data-workspace-id>
      <div className={styles.sectionTitle}>{t("workspaceIdTitle")}</div>
      <div className={idStyles.row}>
        <span className={idStyles.label}>{member ? t("workspaceIdShort") : t("workspaceIdTitle")}</span>
        <span className={idStyles.value}>
          <span className={idStyles.id} data-workspace-id-value>{workspaceId}</span>
          <IconTipButton className={idStyles.copy} label={t("workspaceIdCopy")} tip={t("workspaceIdCopyTip")} data-workspace-id-copy
            onClick={() => void copyText(workspaceId).then((copied) => toast?.notify(t(copied ? "workspaceIdCopied" : "workspaceIdCopyFailed"), copied ? "default" : "error"))}>
            <Copy aria-hidden="true" />
          </IconTipButton>
        </span>
      </div>
    </section>
  );
}
