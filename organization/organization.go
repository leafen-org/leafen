// Package organization holds Leafen's organization features: single sign-on,
// SCIM, audit retention and export, legal hold, domains and managed accounts,
// organization-wide security policies, custom admin roles and per-agent
// permissions. The core never imports it; the program's entry
// (desktop/organization.go) attaches it.
package organization

import (
	"context"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	leafen "leafen.org/leafen/internal/app"
)

// Attach registers the module with the core, before the server is made.
func Attach() {
	leafen.RegisterOrganizationModule(leafen.OrganizationModule{Name: "organization", Attach: attach})
}

func attach(pb *pocketbase.PocketBase) {
	// Audit retention sweeps on its schedule and with the server's runners.
	retention := newEnterpriseAuditRetentionRunner(pb)
	registerEnterpriseAuditRetentionCron(pb, retention)
	leafen.RunWithServer(retention.Start, retention.Shutdown)
	// SCIM provisioning applies large group changes in turns.
	scimOperations := newSCIMOperationRunner(pb)
	leafen.RunWithServer(scimOperations.Start, scimOperations.Shutdown)
	// The audit log's exports and discovery exports run as durable jobs.
	leafen.RegisterDurableJobHandler(organizationAuditExportJobKind, applyOrganizationAuditExportChunk)
	leafen.RegisterDurableJobHandler(organizationDiscoveryExportJobKind, applyOrganizationDiscoveryExport)
	// A legal hold's export runs as a durable job, and a hold keeps the pages
	// it covers from permanent deletion.
	leafen.RegisterDurableJobHandler(notionAdminLegalHoldExportJobKind, applyNotionAdminLegalHoldExportChunk)
	registerEnterpriseLegalHoldHooks(pb)
	// Deleting a managed account runs in turns.
	leafen.RegisterDurableJobHandler(managedUserDeletionJobKind, func(r *leafen.DurableJobRunner, ctx context.Context, job leafen.StoredJob) error {
		return advanceManagedUserDeletion(r, ctx, job.ID)
	})
	// The module answers the core's checks where an organization feature
	// decides.
	leafen.SetOrganizationPolicy(policies)
	// The Notion-compatible Admin API's MCP connection and agent endpoints.
	leafen.RegisterNotionAdminAPIHandler(func(event *core.RequestEvent) (bool, error) {
		if !strings.HasPrefix(event.Request.URL.Path, leafen.NotionAdminAPIMCPConnectionsPath) {
			return false, nil
		}
		return serveNotionAdminMCPConnections(event)
	})
	legalHoldJobs, _ := pb.Store().Get(leafen.DurableJobRunnerStoreKey).(*leafen.DurableJobRunner)
	leafen.RegisterNotionAdminAPIHandler(func(event *core.RequestEvent) (bool, error) {
		return serveNotionAdminLegalHoldAPI(event, legalHoldJobs)
	})
	leafen.RegisterNotionAdminAPIHandler(func(event *core.RequestEvent) (bool, error) {
		if event.Request.URL.Path != leafen.NotionAdminAPIManagedUserSessionRevokePath || event.Request.Method != http.MethodPost {
			return false, nil
		}
		return true, serveNotionAdminManagedUserSessionRevoke(event)
	})
	leafen.RegisterNotionAdminAPIHandler(func(event *core.RequestEvent) (bool, error) {
		workspaceID, agentID, route, ok := notionAdminAgentGovernanceRoute(event.Request.URL.Path)
		if !ok {
			return false, nil
		}
		return serveNotionAdminAgentGovernance(event, workspaceID, agentID, route)
	})
	// An organization's SAML sign-in keeps its identity provider's values
	// when the server's address changes (enterprise_saml_address.go).
	leafen.FollowAddressChange(followSAMLAddressChange)
	// The sign-in screen offers company sign-in where one is (enterprise_saml_find.go).
	leafen.CompanySignInAvailable = companySignInAvailable
	pb.OnServe().BindFunc(func(event *core.ServeEvent) error {
		jobs, _ := pb.Store().Get(leafen.DurableJobRunnerStoreKey).(*leafen.DurableJobRunner)
		registerEnterpriseAuditRetentionRoutes(event)
		registerOrganizationAuditExportRoutes(event, jobs)
		registerOrganizationDiscoveryRoutes(event, jobs)
		registerEnterpriseMCPGovernanceRoutes(event)
		registerEnterpriseSAMLRoutes(event)
		registerEnterpriseSCIMRoutes(event, scimOperations)
		rooms, _ := pb.Store().Get(leafen.WorkspaceRoomsStoreKey).(leafen.WorkspaceRoomInvalidator)
		registerOrganizationManagedUserRoutes(event, rooms)
		registerManagedUserPolicyRoutes(event)
		registerEnterpriseDLPRoutes(event)
		registerEnterpriseLegalHoldRoutes(event)
		registerOrganizationGuestMFARoutes(event)
		registerOrganizationSignInPolicyRoutes(event)
		registerWorkspaceRestrictionRoutes(event, rooms)
		registerDatabasePropertyAccessRoutes(event, rooms)
		return event.Next()
	})
}

func registerOrganizationAuditExportRoutes(event *core.ServeEvent, jobs *leafen.DurableJobRunner) {
	event.Router.GET(leafen.APIRoot+leafen.OrganizationAuditExportListPath, leafen.ListOrganizationAuditExportsRoute)
	event.Router.POST(leafen.APIRoot+leafen.OrganizationAuditExportCreatePath, withJobs(jobs, createOrganizationAuditExport)).
		Bind(apis.BodyLimit(leafen.OrganizationAuditMaxBodyBytes))
	event.Router.GET(leafen.APIRoot+leafen.OrganizationAuditExportGetPath, withJobs(jobs, getOrganizationAuditExport))
	event.Router.GET(leafen.APIRoot+leafen.OrganizationAuditExportDownloadPath, withJobs(jobs, downloadOrganizationAuditExport))
}

// policies are the checks the module answers for the core.
func policies(policy *leafen.OrganizationPolicy) {
	// MCP clients reach a workspace only where its organization approved
	// them and has not turned the member's MCP access off.
	policy.MCPClientWorkspaces = filterApprovedMCPClientWorkspaces
	policy.MCPClientApproved = assertApprovedMCPClient
	// Members hold the admin roles the owner gives them.
	policy.AdminRoles = adminRoleStore{}
	// An organization may require sign-in through its SAML provider, or
	// with a password and 2-step verification rather than by a registered
	// device.
	policy.SSOSignIn = authorizeSSOSignIn
	policy.DeviceSignIn = deviceSignInAllowed
	// An organization manages the accounts on the domains it verified: it
	// may suspend them, keep their emails and name, end their sign-ins
	// sooner, keep them to its workspaces and keep workspace creation to
	// its owners and admins.
	policy.SuspendedUserSQL = suspendedUserSQL
	policy.ManagedAccounts = managedAccounts{}
	policy.ManagedUserNotices = managedUserNotices{}
	policy.SettingsDomains = settingsDomains{}
	policy.WorkspaceCreation = domainWorkspaceCreation
	// An organization's DLP policy blocks sensitive content and outbound
	// actions.
	policy.DLP = organizationDLP{}
	// An active legal hold keeps what it covers from permanent deletion,
	// and from retention sweeps.
	policy.PermanentDeleteHold = activeLegalHoldPreventsDeletion
	policy.RetentionHoldSQL = retentionHoldSQL
	// An organization may require its workspaces' guests to use 2-step
	// verification.
	policy.GuestMFA = guestWorkspaceMFA
	// An organization may block sharing outside it.
	policy.SharingBlocks = sharingBlocks{}
	// An owner may bring someone in as a restricted member, the role the
	// module gives (workspace_restricted_members.go).
	policy.RestrictedInvitations = true
}
