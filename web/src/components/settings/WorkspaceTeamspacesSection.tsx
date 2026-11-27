import { SwitchMark } from "@/components/Switch";
import { type MouseEvent, useEffect, useId, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { ArchiveIcon, ArrowDown, DotsHorizontal, ShieldCheckIcon, TeamspaceRestoreIcon, UprightKeyIcon } from "@/icons/leafen";
import { SettingsSearchToggle } from "./SettingsSearchToggle";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { TeamspaceDialog, type TeamspaceDialogView } from "@/components/TeamspaceDialog";
import { useProductToast } from "@/lib/productToast";
import { activeDateLocale } from "@/lib/i18n";
import type { Session, Workspace } from "@/types";
import type { TeamspaceAccess } from "@/teamspaceWire.generated";
import { defaultSecurityLock, teamspaceArchiveLock, teamspaceRestoreLock, type DialogTeamspace } from "@/components/teamspace/teamspaceDialogModel";
import { HoverTip } from "@/components/IconTipButton";
import { teamspaceGlyph } from "@/components/teamspace/KeptIconGlyph";
import { useTeamspaceDialog } from "@/components/teamspace/useTeamspaceDialog";
import { confirmOwnersOnlyCreate } from "@/components/teamspace/teamspaceSecurityConfirm";
import { SidebarActionMenu } from "@/components/sidebar/SidebarActionMenu";
import { selfMembershipRows, teamspaceMenuRows } from "@/components/sidebar/SidebarTeamspaceMenu";
import type { SidebarTeamspaceAction } from "@/components/sidebar/useSidebarTeamspaceActions";
import { useSidebarTeamspaceActions } from "@/components/sidebar/useSidebarTeamspaceActions";
import { WorkspaceDefaultTeamspaceField } from "./WorkspaceDefaultTeamspaceField";
import { WorkspaceTeamspaceAccessMenu } from "./WorkspaceTeamspaceAccessMenu";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { SettingsPhoneMenuScreen } from "./SettingsPhoneMenuScreen";
import { SettingsFilterChip, SettingsPersonFilterIcon } from "./SettingsFilterChip";
import chipStyles from "./SettingsFilterChip.module.css";
import { ReadFailed } from "@/components/ReadFailed";
import styles from "./WorkspaceTeamspacesSection.module.css";
import { styles as settingsStyles } from "./workspaceSettingsOverviewStyles";

type TeamspaceStatus = "active" | "archived";
type OpenDialog = { view: TeamspaceDialogView; teamspaceId?: string; invite?: boolean } | null;

// The day in the table's short form, as Notion's ("26. 11. 6.", seen 2026-11-07).
function updatedLabel(value: number) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleDateString(activeDateLocale(), { year: "2-digit", month: "numeric", day: "numeric" });
}

export function WorkspaceTeamspacesSection({
  session,
  workspace,
}: {
  session: Session;
  workspace: Workspace;
}) {
  const { t } = useTranslation([
    "teamspaceDialog",
    "workspaceSettingsDialog",
    "wikiView",
    "databaseView",
    "common",
  ]);
  const titleId = useId();
  // Every active teamspace for the workspace's owner, a private one they are not in too, as Notion's
  // Enterprise owner's table (readTeamspaceTableSnapshotState); the sidebar, search and moves keep to the visible ones.
  const controller = useTeamspaceDialog({ session, workspace, everyTeamspace: true });
  const [status, setStatus] = useState<TeamspaceStatus>("active");
  const [query, setQuery] = useState("");
  const [owner, setOwner] = useState("");
  const [access, setAccess] = useState<"" | TeamspaceAccess>("");
  const [security, setSecurity] = useState<"" | "custom">("");
  const [dialog, setDialog] = useState<OpenDialog>(null);
  const [menuFor, setMenuFor] = useState<DialogTeamspace | null>(null);
  const [restoreFor, setRestoreFor] = useState<DialogTeamspace | null>(null);
  const { phone } = useSidebarLayout(true);
  const menuButton = useRef<HTMLButtonElement>(null);
  const reopening = useRef(false);
  const { t: menuT } = useTranslation(["sidebar", "teamspaceDialog", "common"]);
  const { notify } = useProductToast();
  // A row's "…" does what the teamspace's own "…" in the sidebar does.
  const rowActions = useSidebarTeamspaceActions({ session, workspace, teamspaces: controller.teamspaces,
    onChanged: () => { void controller.refreshSnapshot(); if (status === "archived") reopenArchived(); },
    onOpenSettings: (teamspaceId) => setDialog({ view: "manage", teamspaceId }) });
  const source = status === "active" ? controller.teamspaces : controller.archived;
  // A failed read of the list is said in its place with a way to read it
  // again — in the empty line's place while none are shown — never as no teamspaces.
  const readFailed = controller.busy ? null : controller.readFailed;
  const readAgain = () => { if (status === "archived") reopenArchived(); else void controller.refreshSnapshot(); };
  const owners = useMemo(() => [...new Set(source.flatMap((teamspace) => teamspace.summary.ownerLabels))]
    .sort((left, right) => left.localeCompare(right)), [source]);
  const normalizedQuery = query.trim().toLocaleLowerCase();
  // The latest updated first, as Notion's "마지막 업데이트 ↓" (seen 2026-11-07).
  const filtered = useMemo(() => source.filter((teamspace) => {
    if (owner && !teamspace.summary.ownerLabels.includes(owner)) return false;
    if (access && teamspace.access !== access) return false;
    // "팀스페이스 자체 설정": one that sets its own page security (Notion's 보안 filter).
    if (security === "custom" && !teamspace.disablePublishing && !teamspace.disableGuests && !teamspace.disableExport) return false;
    if (!normalizedQuery) return true;
    return [teamspace.name, teamspace.description, ...teamspace.summary.ownerLabels]
      .join(" ").toLocaleLowerCase().includes(normalizedQuery);
  }).sort((left, right) => right.updatedAt - left.updatedAt), [access, normalizedQuery, owner, security, source]);

  function changeStatus(next: TeamspaceStatus) {
    setStatus(next);
    controller.openView(next === "archived" ? "archived" : "browse");
  }

  function closeDialog() {
    setDialog(null);
    void controller.refreshSnapshot();
    if (status === "archived") reopenArchived();
  }

  // The archived list read again: away, and back once the step away is drawn (both in one
  // turn are one change, and the list is not read again).
  function reopenArchived() {
    reopening.current = true;
    controller.openView("browse");
  }
  useEffect(() => {
    if (!reopening.current || controller.view !== "browse") return;
    reopening.current = false;
    controller.openView("archived");
  }, [controller, controller.view]);

  // A workspace owner's own way into an archived one they don't own, over its way back
  // (selfMembershipRows, Notion's Enterprise rows): only its owners bring it back.
  function archivedSelfRows(teamspace: DialogTeamspace) {
    if (!controller.workspaceAdmin || teamspace.membershipRole === "owner") return [];
    return selfMembershipRows(menuT, teamspace, (action: SidebarTeamspaceAction) => void rowActions.act(teamspace, action));
  }

  // An active row pressed outside its controls opens its settings, as its
  // "…" › 팀스페이스 설정 does (Notion's, seen 2026-11-16: the name, three
  // times in three tabs, and the row's empty part open that window on 멤버;
  // an archived row takes no press, seen once). Its controls, and the menus
  // they open over it, keep their own presses.
  function openFromRow(event: MouseEvent<HTMLElement>, teamspaceId: string) {
    const target = event.target, row = event.currentTarget;
    if (!(target instanceof Element) || !row.contains(target)) return;
    const control = target.closest("button, a, input, details");
    if (control && row.contains(control)) return;
    setDialog({ view: "manage", teamspaceId });
  }

  // A row's "…" closes once one of its lines is chosen.
  function closeRowMenu(event: MouseEvent<HTMLElement>) {
    event.currentTarget.closest("details")?.removeAttribute("open");
  }

  async function restore(teamspace: DialogTeamspace) {
    if (await controller.restore(teamspace)) notify(t("teamspaceDialog:notice.restored", { name: teamspace.name }), "success");
  }

  function restoreRow(teamspace: DialogTeamspace) {
    const lock = teamspaceRestoreLock(teamspace, controller.settings);
    const row = <button type="button" disabled={controller.busy || Boolean(lock)} data-teamspace-restore-lock={lock || undefined}
      onClick={(event) => { closeRowMenu(event); void restore(teamspace); }}>
      <TeamspaceRestoreIcon size="regular" aria-hidden="true" />{t("teamspaceDialog:restoreTeamspace")}
    </button>;
    return lock ? <HoverTip label={t(lock === "noDefault" ? "teamspaceDialog:restoreNoDefault" : "teamspaceDialog:restoreNotOwner")}
      placement="left" wide className={styles.restoreTip}>{row}</HoverTip> : row;
  }

  const statusChoices = [
    { value: "active", label: t("workspaceSettingsDialog:teamspacesStatusActive") },
    { value: "archived", label: t("teamspaceDialog:tabs.archived") },
  ];
  const ownersOnly = controller.settings?.ownersOnlyCreate === true;
  // Asked first both ways, as Notion's (teamspaceSecurityConfirm.tsx).
  async function changeOwnersOnly(next: boolean) {
    if (await confirmOwnersOnlyCreate(next)) await controller.updateSettings(next, t("teamspaceDialog:notice.policySaved"));
  }

  return (
    <section
      id="teamspaces"
      className={styles.section}
      aria-labelledby={titleId}
      aria-busy={controller.busy || undefined}
      data-teamspace-workspace-board
    >
      {/* As Notion's (seen 2026-10-12; its default teamspaces field 2026-11-11):
          the default teamspaces, the creation switch, then "팀스페이스" with its
          filters — status, owner, how open — a search and "새 팀스페이스". */}
      {/* With no teamspace yet there is none to choose, as Notion's hides it. */}
      {controller.workspaceAdmin && controller.teamspaces.length > 0 ? (
        <WorkspaceDefaultTeamspaceField teamspaces={controller.teamspaces} defaultIds={controller.settings?.defaultTeamspaceIds ?? []}
          busy={controller.busy} onUpdate={(ids) => controller.setDefaults(ids, t("teamspaceDialog:notice.defaultSet"))} />
      ) : null}
      <div className={settingsStyles.securityRow} data-teamspace-creation-policy>
        <div className={settingsStyles.securityInstruction}>
          <strong>{t("workspaceSettingsDialog:teamspacesOwnersOnly")}</strong>
          <span>{t("workspaceSettingsDialog:teamspacesOwnersOnlyDescription")}</span>
        </div>
        <button type="button" role="switch" aria-checked={ownersOnly} aria-label={t("workspaceSettingsDialog:teamspacesOwnersOnly")}
          disabled={!controller.settings || controller.busy}
          onClick={() => void changeOwnersOnly(!ownersOnly)}>
          <SwitchMark on={ownersOnly} />
        </button>
      </div>

      <div className={styles.listHeading}>
        <strong id={titleId}>{t("teamspaceDialog:title")}</strong>
        <span>{t("workspaceSettingsDialog:teamspacesListDescription")}</span>
      </div>

      <div className={chipStyles.bar}>
        <span data-teamspace-status-filter>
          <SettingsFilterChip marker="status" icon={<ArchiveIcon size="small" />}
            label={status === "active" ? t("workspaceSettingsDialog:teamspacesStatusFilter") : t("teamspaceDialog:tabs.archived")}
            active={status === "archived"} value={status} choices={statusChoices}
            onChoose={(next) => changeStatus(next as TeamspaceStatus)} />
        </span>
        <span data-teamspace-owner-filter>
          <SettingsFilterChip marker="owner" icon={<SettingsPersonFilterIcon />} label={t("wikiView:owners")}
            active={Boolean(owner)} value={owner} searchPlaceholder={t("workspaceSettingsDialog:verifiedPagesOwnerSearch")}
            emptyLabel={t("workspaceSettingsDialog:noSearchResults")}
            choices={[{ value: "", label: t("workspaceSettingsDialog:verifiedPagesAnyOwner") },
              ...owners.map((label) => ({ value: label, label }))]}
            onChoose={setOwner} />
        </span>
        <span data-teamspace-access-filter>
          <SettingsFilterChip marker="access" icon={<UprightKeyIcon size="small" />} label={t("workspaceSettingsDialog:teamspacesAccess")}
            active={Boolean(access)} value={access}
            choices={[{ value: "", label: t("workspaceSettingsDialog:teamspacesAnyAccess") },
              ...(["open", "closed", "private"] as TeamspaceAccess[]).map((value) => ({ value, label: t(`teamspaceDialog:access.${value}`) }))]}
            onChoose={(next) => setAccess(next as "" | TeamspaceAccess)} />
        </span>
        <span data-teamspace-security-filter>
          <SettingsFilterChip marker="security" icon={<ShieldCheckIcon size="small" />} label={t("workspaceSettingsDialog:teamspacesSecurity")}
            active={Boolean(security)} value={security}
            choices={[{ value: "", label: t("workspaceSettingsDialog:teamspacesAnySecurity") },
              { value: "custom", label: t("workspaceSettingsDialog:teamspacesCustomSecurity") }]}
            onChoose={(next) => setSecurity(next as "" | "custom")} />
        </span>
        <span className={chipStyles.spacer} />
        <SettingsSearchToggle query={query} onQueryChange={setQuery} placeholder={t("workspaceSettingsDialog:teamspacesSearch")}
          data={{ "data-teamspace-search": "" }} />
        {/* Who may make one is the read's (its owners-only rule): none until it is in. */}
        {controller.canCreate && controller.settings ? (
          <button type="button" className={styles.primary} onClick={() => setDialog({ view: "create" })}>
            {t("teamspaceDialog:tabs.create")}
          </button>
        ) : null}
      </div>

      <div className={styles.table} role="table" aria-label={t("teamspaceDialog:title")}>
        <div className={styles.tableHeader} role="row">
          <span role="columnheader">{t("teamspaceDialog:title")}</span>
          <span role="columnheader">{t("wikiView:owners")}</span>
          <span role="columnheader">{t("workspaceSettingsDialog:teamspacesAccess")}</span>
          <span role="columnheader" aria-sort="descending" className={styles.sorted}>
            {t("workspaceSettingsDialog:teamspacesLastUpdated")}<ArrowDown pixels={10} aria-hidden="true" />
          </span>
          <span role="columnheader" aria-hidden="true" />
        </div>
        {filtered.map((teamspace) => {
          const ownerText = teamspace.summary.ownerLabels.join(", ") || "—";
          const extraOwners = Math.max(0, teamspace.summary.ownerCount - teamspace.summary.ownerLabels.length);
          return (
            <div key={teamspace.id} className={styles.row} role="row" data-teamspace-row={teamspace.id}
              data-opens={status === "active" || undefined}
              onClick={status === "active" ? (event) => openFromRow(event, teamspace.id) : undefined}>
              <span className={styles.nameCell} role="cell">
                <span className={styles.icon} aria-hidden="true">{teamspaceGlyph(teamspace)}</span>
                <span><span className={styles.nameLine}><strong>{teamspace.name}</strong>{status === "archived" ? (
                  <span className={styles.archivedBadge} data-teamspace-archived-badge>{t("teamspaceDialog:tabs.archived")}</span>
                ) : null}</span>{teamspace.summary.memberCount ? (
                  // "멤버 1명 • 참여 중" for one the reader is in, as Notion's.
                  <small>{t("workspaceSettingsDialog:teamspacesMemberCount", { count: teamspace.summary.memberCount })}
                    {teamspace.joined ? ` • ${t("teamspaceDialog:head.joined")}` : ""}</small>
                ) : null}</span>
              </span>
              <span className={styles.cell} role="cell" title={ownerText}><small className={styles.mobileLabel}>{t("wikiView:owners")}</small>
                {/* The owner's face before the name, as Notion's (seen 2026-11-07). */}
                <span className={styles.ownerName}>{teamspace.summary.ownerUserIds?.[0] ? (
                  <PersonAvatar userId={teamspace.summary.ownerUserIds[0]} name={teamspace.summary.ownerLabels[0] ?? ""} size={20} />
                ) : null}<span>{ownerText}{extraOwners > 0 ? ` +${extraOwners}` : ""}</span></span></span>
              <span className={styles.cell} role="cell"><small className={styles.mobileLabel}>{t("workspaceSettingsDialog:teamspacesAccess")}</small>
                {status === "active" ? (
                  <WorkspaceTeamspaceAccessMenu teamspace={teamspace} busy={controller.busy} workspaceAdmin={controller.workspaceAdmin}
                    canChange={teamspace.membershipRole === "owner"}
                    locked={Boolean(defaultSecurityLock(teamspace, controller.settings, controller.workspaceAdmin))}
                    onChange={(kind) => void controller.changeSecurity(teamspace, kind, t("teamspaceDialog:notice.saved"),
                      t("teamspaceDialog:notice.defaultSet"))} />
                ) : <span>{t(`teamspaceDialog:access.${teamspace.access}`)}</span>}</span>
              <span className={styles.cell} role="cell"><small className={styles.mobileLabel}>{t("workspaceSettingsDialog:teamspacesLastUpdated")}</small><span>{updatedLabel(teamspace.updatedAt)}</span></span>
              <span className={styles.actions} role="cell">
                {status === "active" ? (
                  // Its "…" is the sidebar teamspace's (Notion's, seen 2026-11-11): 멤버 추가, 팀스페이스 설정, 복제, 나가기, 보관.
                  <button type="button" className={styles.rowMore} aria-label={menuT("sidebar:manageTeamspace", { name: teamspace.name })}
                    aria-haspopup="menu" aria-expanded={menuFor?.id === teamspace.id} data-teamspace-actions-menu
                    onClick={(event) => { menuButton.current = event.currentTarget; setMenuFor((open) => open?.id === teamspace.id ? null : teamspace); }}>
                    <DotsHorizontal size="small" aria-hidden="true" />
                  </button>
                ) : phone ? (
                  <button type="button" className={styles.rowMore} aria-label={t("teamspaceDialog:governance")}
                    aria-haspopup="dialog" aria-expanded={restoreFor?.id === teamspace.id} data-teamspace-actions-menu
                    onClick={() => setRestoreFor(teamspace)}>
                    <DotsHorizontal size="small" aria-hidden="true" />
                  </button>
                ) : (
                  // An archived one's "…" holds its way back, as Notion's: dim, its tip at its start, where
                  // it may not come back now (teamspaceRestoreLock, Notion's code and seen 2026-11-11).
                  <details className={`${styles.actionMenu} ${styles.restoreMenu}`} data-teamspace-actions-menu>
                    <summary aria-label={t("teamspaceDialog:governance")}><DotsHorizontal size="regular" aria-hidden="true" /></summary>
                    <div>
                      {archivedSelfRows(teamspace).map((row) => <button key={row.key} type="button" disabled={controller.busy} {...row.data}
                        onClick={(event) => { closeRowMenu(event); row.run?.(); }}>{row.icon}{row.label}</button>)}
                      {archivedSelfRows(teamspace).length ? <hr className={styles.menuLine} /> : null}
                      {restoreRow(teamspace)}
                    </div>
                  </details>
                )}
              </span>
            </div>
          );
        })}
        {readFailed && source.length === 0 ? <div className={styles.empty}><ReadFailed message={readFailed} onRetry={readAgain} /></div>
          : !controller.busy && !reopening.current && filtered.length === 0 ? <p className={styles.empty}>{t("workspaceSettingsDialog:teamspacesNone")}</p> : null}
      </div>

      {(status === "active" ? controller.snapshotCursor : controller.archivedCursor) ? (
        <button type="button" className={styles.loadMore} disabled={controller.busy} onClick={() => void controller.loadMore(status === "active" ? "browse" : "archived")}>{t("teamspaceDialog:loadMore")}</button>
      ) : null}
      {controller.notice ? <p className={styles.notice} role="status">{controller.notice}</p> : null}
      {readFailed ? (source.length ? <ReadFailed message={readFailed} onRetry={readAgain} /> : null)
        : controller.error ? <p className={styles.error} role="alert">{controller.error}</p> : null}

      {/* On a phone a card's "…" takes the whole screen the other settings menus take there
          (SettingsPhoneMenuScreen), under the teamspace's name: Notion's phone has no teamspaces
          screen to compare. */}
      {menuFor ? (() => {
        const rows = teamspaceMenuRows({ t: menuT, canCreate: controller.canCreate, privileged: controller.workspaceAdmin,
          teamspace: { ...menuFor, archiveLock: teamspaceArchiveLock(menuFor, controller.teamspaces, workspace.memberCount) },
          onManage: (teamspaceId, invite) => setDialog({ view: "manage", teamspaceId, invite }),
          onAct: (teamspace, action) => void rowActions.act(teamspace, action), close: () => setMenuFor(null) });
        const label = menuT("sidebar:manageTeamspace", { name: menuFor.name });
        return phone ? (
          <SettingsPhoneMenuScreen title={menuFor.name} label={label} rows={rows} close={() => setMenuFor(null)}
            data={{ "data-teamspace-actions-screen": "" }} />
        ) : createPortal(<SidebarActionMenu button={menuButton} label={label}
          backdropLabel={t("common:actions.close")} close={() => setMenuFor(null)} contentWidth pastWindow rows={rows} />, document.body);
      })() : null}
      {restoreFor && phone ? (() => {
        const lock = teamspaceRestoreLock(restoreFor, controller.settings);
        return <SettingsPhoneMenuScreen title={restoreFor.name} label={t("teamspaceDialog:governance")} close={() => setRestoreFor(null)}
          data={{ "data-teamspace-actions-screen": "" }}
          rows={[...archivedSelfRows(restoreFor).map((row) => ({ ...row, run: () => { setRestoreFor(null); row.run?.(); } })),
            { key: "restore", label: t("teamspaceDialog:restoreTeamspace"), icon: <TeamspaceRestoreIcon size="regular" aria-hidden="true" />,
            separatorBefore: archivedSelfRows(restoreFor).length > 0,
            disabled: controller.busy || Boolean(lock), data: lock ? { "data-teamspace-restore-lock": lock } : undefined,
            // Its reason under it, where the computer's shows a tip.
            description: lock ? t(lock === "noDefault" ? "teamspaceDialog:restoreNoDefault" : "teamspaceDialog:restoreNotOwner") : undefined,
            run: () => void restore(restoreFor) }]} />;
      })() : null}
      {rowActions.archiveConfirm}
      {dialog ? (
        <TeamspaceDialog
          key={`${dialog.view}:${dialog.teamspaceId ?? "new"}`}
          session={session}
          workspace={workspace}
          initialView={dialog.view}
          initialTeamspaceId={dialog.teamspaceId}
          initialInvite={dialog.invite}
          everyTeamspace
          onClose={closeDialog}
        />
      ) : null}
    </section>
  );
}
