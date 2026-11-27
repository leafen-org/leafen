import { useId } from "react";
import { useTranslation } from "react-i18next";
import { organizationModule } from "@/lib/organizationModule";
import { NotionAdminApiPanel } from "@/components/NotionAdminApiPanel";
import type { Session, Workspace } from "@/types";
import { DeveloperSettingsSection } from "./DeveloperSettingsSection";
import { EmojiSettingsSection } from "./EmojiSettingsSection";
import { SidebarPresetsSection } from "./SidebarPresetsSection";
import { PublicPagesSection } from "./PublicPagesSection";
import { VerifiedPagesSection } from "./VerifiedPagesSection";
import { WorkspaceConnectionsSection } from "./WorkspaceConnectionsSection";
import { WorkspaceAIConnections } from "./WorkspaceAIConnections";
import { WorkspaceAnalyticsSection } from "./WorkspaceAnalyticsSection";
import { WorkspaceSettingsOverview } from "./WorkspaceSettingsOverview";
import { WorkspaceMemberGeneral } from "./WorkspaceMemberGeneral";
import { WorkspacePeopleSection } from "./WorkspacePeopleSection";
import { WorkspaceOrganizationSettings } from "./WorkspaceOrganizationSettings";
import { WorkspaceUsageReport } from "./WorkspaceUsageReport";
import { WorkspaceTeamspacesSection } from "./WorkspaceTeamspacesSection";
import { WorkspaceSharingSecurity } from "./WorkspaceSharingSecurity";
import { requestAuditLogFocus } from "./WorkspaceOrganizationAudit";
import { WorkspaceRequestsSection } from "./WorkspaceRequestsSection";
import { WorkspaceServerAdministratorLog } from "./WorkspaceServerAdministratorLog";
import { useWorkspaceEnterpriseSettings } from "./useWorkspaceEnterpriseSettings";
import { useWorkspaceOrganizationSettings } from "./useWorkspaceOrganizationSettings";
import { styles } from "./workspaceSettingsOverviewStyles";
import { ListNotice } from "./SettingsRowNotice";
import { WorkspaceDeletedPages } from "./WorkspaceDeletedPages";
import { canManageWorkspace } from "./workspaceSettingsPageModel";
import type { SettingsSection } from "./settingsWindowModel";

// The workspace's sections of the settings window: what the person may see
// of them, read once the window is open, and each section's content.

export type WorkspaceSettingsAccess = ReturnType<typeof useWorkspaceSettingsAccess>;

export function useWorkspaceSettingsAccess({
  section,
  session,
  workspace,
  workspaces,
}: {
  section: SettingsSection;
  session: Session;
  workspace: Workspace;
  workspaces: Workspace[];
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const enterprise = useWorkspaceEnterpriseSettings({
    view: section,
    // 요청's "제어" shows the security settings' rows of who invites whom.
    active: section === "enterprise" || section === "sharing-security" || section === "identity" || section === "requests",
    errorFallback: t("errLoadOrganization"),
    session,
    workspace,
    workspaces,
  });
  const organizationSettings = useWorkspaceOrganizationSettings({
    // Security reads it for whether the person owns the organization.
    active: section === "organization" || section === "identity" || section === "sharing-security",
    view: section,
    errorFallback: t("errLoadOrganization"),
    session,
    workspace,
  });
  return {
    canManage: canManageWorkspace(workspace),
    canViewEnterprise: enterprise.organization ? enterprise.canViewEnterprise : null,
    enterpriseItems: { security: enterprise.areas.security, identity: enterprise.areas.identity },
    // Only with something of it to show: an IT admin's role covers none of its
    // parts (2026-11-05: the item opened on a bare title).
    canReadOrganization: organizationSettings.organization
      ? organizationSettings.canReadSettings && Boolean(
        organizationSettings.organization.capabilities?.canManagePeople
        || organizationSettings.organization.capabilities?.canManageRoles
        || organizationSettings.organization.capabilities?.canViewAuditLog
        || organizationSettings.organization.capabilities?.canViewAnalytics)
      : null,
    enterprise,
    organizationSettings,
  };
}

export function WorkspaceSettingsContent({
  section,
  access,
  invite,
  requestsTab = false,
  session,
  workspace,
  workspaces,
  onWorkspaceChange,
  onWorkspaceDeleted,
  onWorkspaceRefresh,
  onSelectSection,
  onOpenPage,
  onNavigate,
  serverAdministrator = false,
}: {
  section: SettingsSection;
  access: WorkspaceSettingsAccess;
  /** The people section opens with its invitation form. */
  invite: boolean;
  /** The people section opens at its "요청" tab, a wider touch screen's (WorkspacePeopleSection). */
  requestsTab?: boolean;
  session: Session;
  workspace: Workspace;
  workspaces: Workspace[];
  onWorkspaceChange: (workspace: Workspace) => void;
  onWorkspaceDeleted: (workspaceId: string) => void | Promise<void>;
  onWorkspaceRefresh: () => Promise<void>;
  /** Opens another section of the settings window. */
  onSelectSection?: (section: SettingsSection) => void;
  /** Closes the settings window on a page. */
  onOpenPage?: (pageId: string) => void;
  /** Closes the settings window on another of the app's addresses. */
  onNavigate?: (path: string) => void;
  /** The person administers the server, whose apps they install. */
  serverAdministrator?: boolean;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const adminRequiredId = useId();
  const { canManage, enterprise, organizationSettings } = access;
  const enterpriseSection = section === "enterprise" || section === "sharing-security";
  // The enterprise controls are the organization module's; the Admin API's
  // tokens, which are the core's, show within them or on their own.
  const EnterprisePanel = organizationModule()?.enterprise?.Panel;
  const adminPanel = (
    <NotionAdminApiPanel
      organizationId={workspace.organizationId}
      canManageSecurity={enterprise.areas.adminTokens}
      bounds={enterprise.notionAdmin.bounds}
      service={enterprise.notionAdmin.service}
    />
  );
  const enterprisePanel = (view: "identity" | "governance") => !enterprise.snapshot ? null : EnterprisePanel ? (
    <EnterprisePanel
      areas={enterprise.areas}
      isCurrent={enterprise.isCurrent}
      adminPanel={adminPanel}
      organizationId={workspace.organizationId}
      requestGeneration={enterprise.requestGeneration}
      service={enterprise.service}
      sessionIdentity={enterprise.sessionIdentity}
      signal={enterprise.signal}
      snapshot={enterprise.snapshot}
      view={view}
      onOpenAuditLog={onSelectSection ? () => { requestAuditLogFocus(); onSelectSection("organization"); } : undefined}
    />
  ) : view === "governance" ? <section className={styles.section}>{adminPanel}</section> : null;
  const notice = (text: string, busy = false) => (
    <section className={styles.section} aria-busy={busy || undefined}>
      <div className={styles.notice} data-tone={busy ? "neutral" : undefined}>{text}</div>
    </section>
  );
  // The organization could not be read: what failed, with a way to read it again.
  const failed = (text: string, onRetry: () => void) => (
    <section className={styles.section}>
      <ListNotice role="alert" onRetry={onRetry} data-organization-read-failed>{text}</ListNotice>
    </section>
  );

  let content;
  if (section === "identity") {
    // Notion's "신원" (seen 2026-10-02): domains and who may create workspaces
    // with them, managed users, SAML SSO, the enterprise-managed connections,
    // SCIM, and the IDs they're set up with.
    const organization = organizationSettings.snapshot;
    // Domains and managed users are the organization module's.
    const { IdentityDomains, ManagedUsers } = organizationModule() ?? {};
    content = (
      <div data-workspace-identity>
        {IdentityDomains ? (
          <section className={styles.section} role="region" aria-label={t("identityDomainsTitle")}>
            <div className={styles.sectionTitle}>{t("identityDomainsTitle")}</div>
            {organization ? (
              <IdentityDomains organization={organization} disabled={organizationSettings.mutating}
                mutate={organizationSettings.mutate} />
            ) : (
              organizationSettings.error ? (
                <ListNotice role="alert" onRetry={organizationSettings.refresh} data-organization-read-failed>{organizationSettings.error}</ListNotice>
              ) : <div className={styles.notice} data-tone="neutral">{t("loadingOrganization")}</div>
            )}
          </section>
        ) : null}
        {ManagedUsers && organization?.organization.capabilities.canManageIdentity ? (
          <ManagedUsers session={session} organizationId={workspace.organizationId} />
        ) : null}
        {enterprise.loading ? notice(t("loadingOrganization"), true)
          : enterprise.error ? failed(enterprise.error, enterprise.refresh)
          : enterprisePanel("identity")}
        <section className={styles.section} role="region" aria-label={t("identitySetupTitle")}>
          <div className={styles.sectionTitle}>{t("identitySetupTitle")}</div>
          <div className={styles.securityRow}>
            <div className={styles.securityInstruction}><strong>{t("identityWorkspaceId")}</strong></div>
            <code className={styles.securityCode}>{workspace.id}</code>
          </div>
          <div className={styles.securityRow}>
            <div className={styles.securityInstruction}><strong>{t("identityOrganizationId")}</strong></div>
            <code className={styles.securityCode}>{workspace.organizationId}</code>
          </div>
        </section>
      </div>
    );
  } else if (section === "organization") {
    content = organizationSettings.loading ? notice(t("loadingOrganization"), true)
      : organizationSettings.error ? failed(organizationSettings.error, organizationSettings.refresh)
      : organizationSettings.snapshot ? (
        <WorkspaceOrganizationSettings
          snapshot={organizationSettings.snapshot}
          userId={session.user.id}
          mutating={organizationSettings.mutating}
          mutate={organizationSettings.mutate}
        />
      ) : notice(t("loadingOrganization"), true);
  } else if (enterpriseSection) {
    // Which workspaces require their guests to use 2-step verification is the
    // organization module's, for the organization's owner.
    const { GuestMFA, SignInPolicy } = organizationModule() ?? {};
    const owner = Boolean(organizationSettings.snapshot?.organization.capabilities.canManageRoles);
    content = enterprise.loading ? notice(t("loadingOrganization"), true)
      : enterprise.error ? failed(enterprise.error, enterprise.refresh)
      : enterprise.snapshot && section === "enterprise" ? enterprisePanel("governance")
      : enterprise.snapshot ? (<>
        <WorkspaceSharingSecurity
          canManageSecurity={enterprise.canManageSecurity}
          isCurrent={enterprise.isCurrent}
          onRefresh={enterprise.refresh}
          organizationId={workspace.organizationId}
          policy={enterprise.snapshot.sharingSecurity}
          requestGeneration={enterprise.requestGeneration}
          service={enterprise.service}
          sessionIdentity={enterprise.sessionIdentity}
          signal={enterprise.signal}
          snapshotIdentity={enterprise.snapshot.snapshotIdentity}
          ownerControls={owner && (SignInPolicy || GuestMFA) ? <>
            {SignInPolicy ? <SignInPolicy session={session} organizationId={workspace.organizationId} /> : null}
            {GuestMFA ? <GuestMFA session={session} organizationId={workspace.organizationId} /> : null}
          </> : null}
          deletedPages={canManage ? <WorkspaceDeletedPages session={session} workspaceId={workspace.id} /> : null}
        />
        <WorkspaceServerAdministratorLog session={session} workspaceId={workspace.id} />
      </>) : notice(t("loadingOrganization"), true);
  } else if (section === "connections") {
    content = <WorkspaceAIConnections session={session} workspace={workspace} onNavigate={onNavigate} />;
  } else if (section === "integrations") {
    content = (
      <WorkspaceConnectionsSection session={session} workspace={workspace} onOpenMcp={() => onSelectSection?.("connections")}
        onOpenDeveloper={() => onSelectSection?.("developer")} />
    );
  } else if (section === "verified-pages") {
    content = <VerifiedPagesSection session={session} workspace={workspace} onOpenPage={(pageId) => onOpenPage?.(pageId)} />;
  } else if (section === "public-pages") {
    content = <PublicPagesSection session={session} workspace={workspace} onOpenPage={(pageId) => onOpenPage?.(pageId)} />;
  } else if (section === "emoji") {
    content = <EmojiSettingsSection session={session} workspace={workspace} />;
  } else if (section === "sidebar") {
    content = <SidebarPresetsSection session={session} workspaceId={workspace.id} />;
  } else if (section === "developer") {
    content = <DeveloperSettingsSection session={session} workspace={workspace} onNavigate={onNavigate} />;
  } else if (section === "analytics") {
    content = <WorkspaceAnalyticsSection session={session} workspace={workspace} />;
  } else if (section === "requests") {
    const controls = access.canViewEnterprise && access.enterpriseItems.security && enterprise.snapshot ? (
      <WorkspaceSharingSecurity
        membersOnly
        canManageSecurity={enterprise.canManageSecurity}
        isCurrent={enterprise.isCurrent}
        onRefresh={enterprise.refresh}
        organizationId={workspace.organizationId}
        policy={enterprise.snapshot.sharingSecurity}
        requestGeneration={enterprise.requestGeneration}
        service={enterprise.service}
        sessionIdentity={enterprise.sessionIdentity}
        signal={enterprise.signal}
        snapshotIdentity={enterprise.snapshot.snapshotIdentity}
      />
    ) : undefined;
    content = (
      <WorkspaceRequestsSection
        session={session}
        workspace={workspace}
        controls={controls}
        onOpenApps={serverAdministrator && onSelectSection ? () => onSelectSection("server-apps") : undefined}
      />
    );
  } else if (section === "people") {
    content = (
      <WorkspacePeopleSection key={`${session.user.id}:${session.sessionId}:${workspace.id}`}
        initialAddOpen={invite}
        initialTab={requestsTab ? "requests" : undefined}
        onOpenSecurity={access.canViewEnterprise && access.enterpriseItems.security && onSelectSection ? () => onSelectSection("sharing-security") : undefined}
        onNavigate={onNavigate}
        onOpenApps={serverAdministrator && onSelectSection ? () => onSelectSection("server-apps") : undefined}
        onWorkspaceRefresh={onWorkspaceRefresh}
        session={session}
        workspace={workspace}
      />
    );
  } else if (canManage && section === "teamspaces") {
    content = <WorkspaceTeamspacesSection session={session} workspace={workspace} />;
  } else if (canManage && section === "usage") {
    content = <WorkspaceUsageReport session={session} workspace={workspace} />;
  } else if (canManage) {
    content = (
      <WorkspaceSettingsOverview
        session={session}
        workspace={workspace}
        workspaces={workspaces}
        canManageWorkspace={canManage}
        onWorkspaceChange={onWorkspaceChange}
        onWorkspaceDeleted={onWorkspaceDeleted}
        onOpenPage={onOpenPage}
      />
    );
  } else if (section === "general") {
    content = <WorkspaceMemberGeneral session={session} workspace={workspace} onNavigate={onNavigate} onWorkspaceRefresh={onWorkspaceRefresh} />;
  } else {
    content = (
      <section className={styles.section} aria-labelledby={adminRequiredId}>
        <div id={adminRequiredId} className={styles.sectionTitle}>{t("adminRequired")}</div>
        <div className={styles.notice} data-tone="neutral">{t("adminOnlyNotice")}</div>
      </section>
    );
  }
  return <div className={styles.adminConsole} data-surface="workspace-admin">{content}</div>;
}
