package app

import (
	"context"
	"errors"
	"time"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
)

// The organization features are a module kept apart from the core, under the
// same AGPL-3.0-only license and free like the rest; the boundary keeps the core
// small and the module optional. The core never imports it: the program's
// entry (desktop/organization.go) attaches it before the server is made, and a build tagged leafen_core
// leaves it out. Without it the core sets no organization policy, as an
// organization that set none.

// OrganizationModule is what the module attaches to the core.
type OrganizationModule struct {
	Name string
	// Attach registers the module's routes, hooks and runners on the server
	// the core has made, after the core's own.
	Attach func(pb *pocketbase.PocketBase)
}

var organizationModules []OrganizationModule

// RegisterOrganizationModule attaches a module; the program's entry calls
// it before New makes the server.
func RegisterOrganizationModule(module OrganizationModule) {
	organizationModules = append(organizationModules, module)
}

func attachOrganizationModules(pb *pocketbase.PocketBase) {
	// What the modules attach belongs to this server: one that couldn't start
	// leaves nothing for the next to run (2026-11-05: the desktop app's "다시
	// 시도" after a damaged start ran the first try's SCIM runner on a server
	// with no database, and the app ended).
	organizationComponents = nil
	notionAdminAPIHandlers = nil
	for _, module := range organizationModules {
		if module.Attach != nil {
			module.Attach(pb)
		}
	}
}

type organizationComponent struct {
	start    func()
	shutdown func(context.Context) error
}

var organizationComponents []organizationComponent

// RunWithServer starts one of the module's runners when the server starts
// its own, and stops it first when the server shuts down.
func RunWithServer(start func(), shutdown func(context.Context) error) {
	organizationComponents = append(organizationComponents, organizationComponent{start, shutdown})
}

func startOrganizationComponents() {
	for _, component := range organizationComponents {
		component.start()
	}
}

func shutdownOrganizationComponents(ctx context.Context) error {
	var err error
	for _, component := range organizationComponents {
		err = errors.Join(err, component.shutdown(ctx))
	}
	return err
}

// organizationJobHandlers run the durable jobs of the kinds the module owns.
var organizationJobHandlers = map[string]func(*durableJobRunner, context.Context, storedJob) error{}

// RegisterDurableJobHandler has the core's job runner run a kind of job the
// module owns.
func RegisterDurableJobHandler(kind string, handler func(*durableJobRunner, context.Context, storedJob) error) {
	organizationJobHandlers[kind] = handler
}

// OrganizationPolicy holds the checks the core asks the organization module
// for where an organization feature decides. A check left nil is the core's
// own answer: no policy, allowed.
type OrganizationPolicy struct {
	// MCPClientWorkspaces keeps the workspaces an MCP client may reach for
	// the actor.
	MCPClientWorkspaces func(app core.App, workspaces []mcpGovernanceWorkspace, clientID string, actorID string) ([]mcpGovernanceWorkspace, error)
	// MCPClientApproved fails with errMCPClientNotApproved when the client
	// may not reach one of the workspaces.
	MCPClientApproved func(app core.App, workspaces []mcpGovernanceWorkspace, context mcpGovernanceContext) error
	// AdminRoles keeps the admin roles an organization's owner gives
	// members.
	AdminRoles OrganizationAdminRoleStore
	// SSOSignIn fails with errSAMLRequired when an organization requires the
	// person to sign in through its SAML provider and the sign-in, by method
	// (and provider), is not that.
	SSOSignIn func(app core.App, userID string, method string, providerID string) error
	// DeviceSignIn fails with errDeviceSignInRestricted when an organization
	// asks the person to sign in with a password and 2-step verification
	// rather than by a registered device (device_sign_in_policy.go).
	DeviceSignIn func(app core.App, userID string) error
	// SuspendedUserSQL is an SQL expression, true when an organization has
	// suspended the user the given expression names.
	SuspendedUserSQL func(userExpression string) string
	// ManagedAccounts answers what the organizations managing a person ask
	// of them.
	ManagedAccounts OrganizationManagedAccounts
	// ManagedUserNotices sends the emails queued for managed people.
	ManagedUserNotices OrganizationManagedUserNotices
	// SettingsDomains is the domains part of the organization's settings.
	SettingsDomains OrganizationSettingsDomains
	// WorkspaceCreation fails with errWorkspaceCreationRestricted when an
	// organization keeps the person from creating workspaces.
	WorkspaceCreation func(app core.App, user *core.Record) error
	// DLP checks content and outbound actions against the organization's DLP
	// policy.
	DLP OrganizationDLP
	// PermanentDeleteHold fails with errLegalHoldPreventsPermanentDelete when
	// an active legal hold covers what would be permanently deleted.
	PermanentDeleteHold func(app core.App, organizationID string, target legalHoldDeletionTarget) error
	// RetentionHoldSQL is an SQL expression, true of a record an active
	// legal hold covers: retention sweeps keep it (legal_hold_retention.go).
	RetentionHoldSQL func(record legalHoldRecordSQL) string
	// GuestMFA refuses a guest a workspace whose organization requires its
	// guests to turn on 2-step verification (guestMFARequired).
	GuestMFA func(app core.App, principalID string, workspace *core.Record) error
	// SharingBlocks blocks sharing outside the organization: publishing,
	// guests and outside emails, exports and copies to other workspaces.
	SharingBlocks OrganizationSharingBlocks
	// RestrictedInvitations lets an owner add or invite someone as a
	// restricted member, the role the module gives
	// (workspace_invitation_restricted.go).
	RestrictedInvitations bool
}

// OrganizationSharingBlocks decides the organization's blocks on sharing
// outside it; each check fails with errOrganizationSharingSecurityBlocked.
type OrganizationSharingBlocks interface {
	PublicSharing(app core.App, organizationID string) error
	Exports(app core.App, organizationID string) error
	CrossWorkspaceCopy(app core.App, source, target workspaceAuthorization) error
	Share(app core.App, organizationID, workspaceID, principalType, principalID string) error
	// Options are whether the organization lets its pages be published and
	// shared with guests and outside emails.
	Options(app core.App, organizationID string) (publishing, guests, externalEmail bool, err error)
}

// OrganizationDLP checks content and outbound actions against an
// organization's DLP policy.
type OrganizationDLP interface {
	// ContentScanner reads the policy once for a run of content checks.
	ContentScanner(app core.App, organizationID string) (OrganizationDLPContentScanner, error)
	// Outbound fails with errDLPPolicyBlocked when the policy blocks the
	// outbound action.
	Outbound(app core.App, organizationID, action string) error
}

// OrganizationDLPContentScanner checks content against a DLP policy as read
// once.
type OrganizationDLPContentScanner interface {
	// Revision is the revision of the policy the scanner read.
	Revision() int
	// Blocks tells whether the policy blocks content at all, so a check that
	// costs something can be skipped when it doesn't.
	Blocks() bool
	Enforce(values ...any) error
	RequireCurrentPolicy(app core.App, organizationID string) error
}

// OrganizationManagedAccounts answers what the organizations that verified a
// person's email domain ask of them.
type OrganizationManagedAccounts interface {
	Rules(app core.App, user *core.Record) (managedUserRules, error)
	ExternalWorkspaceJoin(app core.App, userID, workspaceOrganizationID string) error
	EmailChange(app core.App, user *core.Record, target string) error
	EmailAddition(app core.App, user *core.Record, target string) error
	EmailChanged(app core.App, user *core.Record, email, action string) error
}

// OrganizationManagedUserNotices sends the emails queued for managed people;
// the notification email runner asks it when they are due.
type OrganizationManagedUserNotices interface {
	NextDue(ctx context.Context, app core.App) (int64, error)
	SendDue(ctx context.Context, app core.App, now time.Time) error
}

// OrganizationSettingsDomains is the domains part of the organization's
// settings: its list, and the actions that add, verify and remove domains.
type OrganizationSettingsDomains interface {
	Load(app core.App, organizationID, afterDomain string) ([]organizationSettingsDomain, string, bool, error)
	// PrepareVerification looks the domain's TXT record up before the
	// change: what it proves, or why it doesn't.
	PrepareVerification(ctx context.Context, app core.App, organizationID, rawDomain string) (OrganizationDomainCheck, error)
	Mutate(app core.App, organizationID string, input organizationSettingsMutationInput,
		check OrganizationDomainCheck, now int64) (*organizationSettingsDomain, string, error)
}

// OrganizationDomainCheck is what looking a domain's TXT record up found:
// the challenge it proves, or why not — no record there, a different value
// (the first one found), or a lookup that failed. A check that proves
// nothing is kept on the domain and answered, not refused (2026-10-16: the
// button said nothing).
type OrganizationDomainCheck struct {
	Domain      string
	Challenge   string
	FailureCode string
	Found       string
}

// The reasons a domain's check proves nothing.
const (
	OrganizationDomainRecordMissing  = "record_missing"
	OrganizationDomainRecordMismatch = "record_mismatch"
	OrganizationDomainLookupFailed   = "lookup_failed"
)

// OrganizationAdminRoleStore keeps the admin roles an organization's owner
// gives members.
type OrganizationAdminRoleStore interface {
	// Assigned are the person's admin roles in the organizations, by
	// organization.
	Assigned(app core.App, userID string, organizationIDs []string) (map[string][]string, error)
	// Members are an organization's members' admin roles, by user.
	Members(app core.App, organizationID string) (map[string][]string, error)
	Add(app core.App, organizationID, userID, role, assignedBy string) error
	Remove(app core.App, organizationID, userID, role string) error
}

var organizationPolicy OrganizationPolicy

// SetOrganizationPolicy sets the module's checks; the module's Attach calls
// it.
func SetOrganizationPolicy(set func(*OrganizationPolicy)) {
	set(&organizationPolicy)
}

// notionAdminAPIHandlers serve the Notion-compatible Admin API endpoints the
// module owns, before the core's own.
var notionAdminAPIHandlers []func(*core.RequestEvent) (bool, error)

// RegisterNotionAdminAPIHandler adds a handler for Admin API endpoints; it
// answers whether it served the request.
func RegisterNotionAdminAPIHandler(handler func(*core.RequestEvent) (bool, error)) {
	notionAdminAPIHandlers = append(notionAdminAPIHandlers, handler)
}

// workspaceRoomsStoreKey keeps the server's workspace rooms, which the module
// tells when a person's access to a workspace changes.
const workspaceRoomsStoreKey = "leafen.workspaceRooms"
