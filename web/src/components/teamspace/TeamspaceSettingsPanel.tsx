import { useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { RenameInPlaceIcon } from "@/icons/leafen";
import { ErrorBoundary } from "@/components/ErrorBoundary";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { AnchoredIconPicker, usePageIconPickerAnchor } from "@/components/page/AnchoredEmojiPicker";
import { ICON_FIRST_TABS } from "@/components/page/pageIconPickerModel";
import { useProductToast } from "@/lib/productToast";
import { requestTeamspacesRead } from "@/lib/workspaceTeamspaces";
import { keptIconPicture, keptIconUploadTarget, teamspaceGlyph } from "./KeptIconGlyph";
import { TeamspaceArchiveConfirm } from "./TeamspaceArchiveConfirm";
import { TeamspaceHeadCount } from "./TeamspaceHeadCount";
import { TeamspaceJoinButton } from "./TeamspaceJoinButton";
import { TeamspaceMembersView } from "./TeamspaceMembersView";
import { TeamspaceSecurityTab, TeamspaceSettingsGeneral } from "./TeamspaceSettingsTabs";
import { teamspaceLeaveLock } from "./teamspaceDialogModel";
import { teamspaceLeaveQuestion } from "./teamspaceLeaveQuestion";
import type { TeamspaceDialogController } from "./useTeamspaceDialog";
import styles from "./TeamspaceSettings.module.css";
import { ReadFailed } from "@/components/ReadFailed";
import nameFieldStyles from "../settings/NameInPlaceField.module.css";
import { caretAtEnd, nameFieldKeyDown } from "../settings/nameInPlaceField";

export type TeamspaceSettingsTab = "settings" | "members" | "security";


/**
 * A teamspace's settings, as Notion's (seen 2026-10-06): its icon, name and
 * member count over "설정", "멤버" and "보안". Every change is saved at once;
 * leaving and archiving close it. Beside the count the reader's place in it
 * (TeamspaceJoinButton): "✓ 참여 중" leaves it, and someone outside — come by
 * the library's "⋯" › "멤버 보기" — joins an open one or asks a closed one.
 */
export function TeamspaceSettingsPanel({ controller, titleId, initialInvite = false, initialTab, closeButton, workspaceName, onDone }: {
  controller: TeamspaceDialogController; titleId: string; initialInvite?: boolean;
  /** The tab it opens at, as a notice about the teamspace asks (its members, for a join request). */
  initialTab?: TeamspaceSettingsTab;
  closeButton: ReactNode;
  workspaceName: string; onDone(): void;
}) {
  const { t } = useTranslation(["teamspaceDialog", "common"]);
  const { notify } = useProductToast();
  // Its members first, as Notion's "팀스페이스 설정" opens every time (seen 2026-11-06, after looking at another tab too).
  const [tab, setTab] = useState<TeamspaceSettingsTab>(initialInvite ? "members" : initialTab ?? "members");
  const [renaming, setRenaming] = useState<string | null>(null);
  const [iconPicking, setIconPicking] = useState(false);
  // Its picker opens on "아이콘", its middle under the icon's, 4px down, over the window rather than in the
  // dialog, as Notion's teamspace settings (seen 2026-11-16 at 1440: picker 265,150 under the 64px icon at 437,82).
  const iconRef = useRef<HTMLButtonElement>(null);
  const iconAnchor = usePageIconPickerAnchor(iconPicking, iconRef);
  const [archiving, setArchiving] = useState(false);
  const { busy, canManageSelected, selected } = controller;
  if (!selected) {
    // Not read: what failed with a way to read again, not "none joined"; nothing while it is read.
    return <div className={styles.head}>{closeButton ? <span className={styles.closeSlot}>{closeButton}</span> : null}
      {controller.readFailed ? <ReadFailed message={controller.readFailed} onRetry={() => void controller.refreshSnapshot()} />
        : controller.loading ? null : <p className={styles.muted}>{t("teamspaceDialog:noneJoined")}</p>}</div>;
  }
  const teamspace = selected;
  // Every tab for everyone who opens it, its controls its owners' alone (Notion's code and seen 2026-11-18 for a
  // workspace owner and a restricted member who are only its members).
  const tabs: TeamspaceSettingsTab[] = ["settings", "members", "security"];
  // Its people, those of its groups counted in once each, as Notion's "N 멤버" (seen 2026-11-16: a teamspace made
  // from a group of two with its maker in it, "2 멤버"; one of a group of its maker alone, "1 멤버") — the server's
  // count (teamspace_summary.go); the list, at once after a change, while it holds people alone.
  const listCountsPeople = controller.members.length > 0 && !controller.memberCursor
    && controller.members.every((member) => member.principalType === "user");
  const memberCount = listCountsPeople ? controller.members.length : teamspace.summary.memberCount;
  const ownerCount = controller.members.filter((member) => member.role === "owner").length || teamspace.summary.ownerCount;

  function rename(value: string) {
    setRenaming(null);
    const name = value.trim();
    if (name && name !== teamspace.name) void controller.saveSelectedWith({ name }, t("teamspaceDialog:notice.saved"));
  }

  // Dim with its tip where one may not leave (TeamspaceLeaveLock).
  const leaveLock = teamspaceLeaveLock(teamspace, ownerCount);

  async function leave() {
    // The sidebar's question and notice, as Notion asks the same (useSidebarTeamspaceActions).
    if (!await confirmAction(teamspaceLeaveQuestion(teamspace.name))) return;
    if (await controller.leaveSelected(t("teamspaceDialog:notice.leftName", { name: teamspace.name }))) {
      notify(t("teamspaceDialog:notice.leftName", { name: teamspace.name }), "success");
      onDone();
    }
  }

  async function join() {
    const name = t("teamspaceDialog:notice.joinedName", { name: teamspace.name });
    if (!await controller.join(teamspace, name)) return;
    notify(name, "success");
    requestTeamspacesRead(controller.workspaceId);
  }

  // A request already waiting stands as sent, as the page path's (useTeamspaceCrumbJoin).
  async function request(message: string) {
    const sent = teamspace.requestPending || Boolean(await controller.requestAccess(teamspace, "", message));
    notify(t(sent ? "teamspaceDialog:notice.membershipRequested" : "teamspaceDialog:error.membershipRequest"), sent ? "success" : "error");
    if (sent) requestTeamspacesRead(controller.workspaceId);
    return sent;
  }

  async function archive() {
    const archived = await controller.archiveSelected();
    if (!archived) return false;
    notify(t("teamspaceDialog:notice.archived", { name: teamspace.name }), "success");
    onDone();
    return true;
  }

  return (
    <>
      <div className={styles.head} data-teamspace-settings-head>
        {closeButton ? <span className={styles.closeSlot}>{closeButton}</span> : null}
        <span className={styles.iconSlot}>
          <button ref={iconRef} type="button" className={styles.icon} disabled={!canManageSelected || busy} aria-label={t("teamspaceDialog:head.changeIcon")}
            onClick={() => setIconPicking(true)} data-teamspace-settings-icon>
            {teamspaceGlyph(teamspace)}
          </button>
          {iconPicking && iconAnchor ? (
            <ErrorBoundary scope="teamspace-icon-picker">
              <AnchoredIconPicker anchor={iconAnchor} marker="teamspace-icon-picker" onClose={() => setIconPicking(false)} picker={{
                tabOrder: ICON_FIRST_TABS, offerRandom: false, uploadTarget: keptIconUploadTarget(controller.workspaceId, "teamspace"),
                onPick: (icon) => { setIconPicking(false); void controller.saveSelectedWith({ icon }, t("teamspaceDialog:notice.saved")); },
                onPickBuiltInIcon: (icon) => { setIconPicking(false); void controller.saveSelectedWith({ icon }, t("teamspaceDialog:notice.saved")); },
                onPickImage: (value) => {
                  const icon = keptIconPicture(value);
                  if (!icon) return;
                  setIconPicking(false);
                  void controller.saveSelectedWith({ icon }, t("teamspaceDialog:notice.saved"));
                },
                onRemove: () => { setIconPicking(false); void controller.saveSelectedWith({ icon: "" }, t("teamspaceDialog:notice.saved")); },
              }} />
            </ErrorBoundary>
          ) : null}
        </span>
        {renaming !== null ? (
          <input className={`${nameFieldStyles.field} ${styles.nameInput}`} value={renaming} maxLength={80} autoFocus
            aria-label={t("teamspaceDialog:fields.name")} onFocus={caretAtEnd} data-focus-leaves
            onChange={(event) => setRenaming(event.target.value)} onBlur={(event) => rename(event.target.value)}
            onKeyDown={(event) => nameFieldKeyDown(event, { onEnter: (field) => rename(field.value), onEscape: () => setRenaming(null) })} />
        ) : (
          <h2 id={titleId} className={styles.name}>
            <span>{teamspace.name}</span>
            {canManageSelected ? <button type="button" className={styles.rename} disabled={busy} aria-label={t("teamspaceDialog:head.rename")}
              onClick={() => setRenaming(teamspace.name)}><RenameInPlaceIcon size="regular" aria-hidden="true" /></button> : null}
          </h2>
        )}
        <div className={styles.headLine}>
          <TeamspaceHeadCount teamspace={teamspace} count={memberCount} />
          <TeamspaceJoinButton teamspace={teamspace} leaveLock={leaveLock} busy={controller.actionBusy} onLeave={() => void leave()}
            onJoin={() => void join()} onRequest={request} />
        </div>
      </div>
      <nav className={styles.tabs} aria-label={t("teamspaceDialog:governance")}>
        {tabs.map((item) => <button key={item} type="button" aria-current={tab === item ? "page" : undefined}
          onClick={() => setTab(item)} data-teamspace-settings-tab={item}>{t(`teamspaceDialog:settingsTabs.${item}`)}</button>)}
      </nav>
      <div className={styles.body}>
        {tab === "settings" ? <TeamspaceSettingsGeneral controller={controller} workspaceName={workspaceName}
          leaveLock={leaveLock} onLeave={() => void leave()} onArchive={() => setArchiving(true)} /> : null}
        {tab === "members" ? <>
          {/* The server lists the requests to those who answer them (managesTeamspace). */}
          {controller.requests.length ? <section className={styles.requests}>
            <h3>{t("teamspaceDialog:requests")}</h3>
            {controller.requests.map((request) => <div key={request.id} className={styles.requestRow}>
              {/* Why they ask, when they said, as a workspace membership request's (WorkspaceRequestsSection). */}
              <span><span>{controller.requestLabel(request)}</span>
                {request.message ? <q className={styles.requestMessage} data-teamspace-request-message>{request.message}</q> : null}</span>
              <button type="button" className={styles.primary} disabled={busy}
                onClick={() => void controller.respond(request, "approved", t("teamspaceDialog:notice.approved"))}>{t("teamspaceDialog:approve")}</button>
              <button type="button" className={styles.secondary} disabled={busy}
                onClick={() => void controller.respond(request, "denied", t("teamspaceDialog:notice.denied"))}>{t("teamspaceDialog:deny")}</button>
            </div>)}
            {controller.requestCursor ? <button type="button" className={styles.secondary} disabled={busy}
              onClick={() => void controller.loadMore("requests")}>{t("teamspaceDialog:loadMore")}</button> : null}
          </section> : null}
          <TeamspaceMembersView controller={controller} initialInvite={initialInvite} onLeave={() => void leave()} />
        </> : null}
        {tab === "security" ? <TeamspaceSecurityTab controller={controller} /> : null}
        {/* A read that failed is said where its content shows (the members' read under 멤버). */}
        {controller.error && controller.error !== controller.readFailed ? <p className={styles.error} role="alert">{controller.error}</p> : null}
      </div>
      {archiving ? <TeamspaceArchiveConfirm name={teamspace.name} onArchive={archive} onClose={() => setArchiving(false)} /> : null}
    </>
  );
}
