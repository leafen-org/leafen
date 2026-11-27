package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The organization policies a data folder keeps are the organization
// module's to enforce. A server without the module (a build tagged
// leafen_core) would leave them off without a word (2026-10-13): it says
// which it keeps but doesn't enforce, when it starts and to its
// administrators, and keeps what can't be undone — nothing is permanently
// deleted while a legal hold is active.

// unenforcedOrganizationPolicyQueries count, by policy, what is in force.
var unenforcedOrganizationPolicyQueries = []struct{ key, table, where string }{
	{"legalHolds", "leafen_organization_legal_holds", "[[status]] = 'active'"},
	{"dlp", "leafen_organization_dlp_policies", "[[enabled]] = 1"},
	{"sharingBlocks", "leafen_organization_sharing_security_policies",
		"[[public_web_sharing]] = 0 OR [[external_email_sharing]] = 0 OR [[guest_access]] = 0 OR [[exports]] = 0"},
	{"suspendedPeople", "leafen_organization_user_suspensions", "1 = 1"},
	{"guestTwoStep", "leafen_organization_guest_mfa_workspaces", "1 = 1"},
	{"passwordTwoStep", "leafen_organization_sign_in_policies", "1 = 1"},
	{"singleSignOn", "leafen_saml_providers", "[[enabled]] = 1 AND [[enforcement]] != 'optional'"},
	{"managedAccounts", "leafen_organization_managed_user_policies",
		"([[profile_changes]] = 0 OR [[external_workspaces]] = 0 OR [[session_duration_hours]] < 4320) AND " +
			verifiedDomainOf("leafen_organization_managed_user_policies")},
	{"mcpClients", "leafen_organization_mcp_workspace_policies", "[[enabled]] = 1"},
	{"workspaceCreation", "leafen_organization_policy_state",
		"[[workspace_creation_policy]] = 'owners_admins' AND " + verifiedDomainOf("leafen_organization_policy_state")},
}

// verifiedDomainOf is whether the organization of a policy row verified a
// domain: the rules for its company accounts reach no one before.
func verifiedDomainOf(table string) string {
	return "EXISTS (SELECT 1 FROM {{leafen_organization_domains}} [[domain]] WHERE [[domain]].[[organization_id]] = {{" +
		table + "}}.[[organization_id]] AND [[domain]].[[status]] = 'verified')"
}

// unenforcedOrganizationPolicies are the policies the data keeps in force
// while the server runs without the organization module; with it, none.
func unenforcedOrganizationPolicies(app core.App) ([]administratorUnenforcedPolicy, error) {
	policies := []administratorUnenforcedPolicy{}
	if len(organizationModules) > 0 {
		return policies, nil
	}
	for _, query := range unenforcedOrganizationPolicyQueries {
		var tables int
		if err := app.DB().NewQuery(`SELECT COUNT(*) FROM sqlite_master WHERE [[type]] = 'table' AND [[name]] = {:name}`).
			Bind(dbx.Params{"name": query.table}).Row(&tables); err != nil {
			return nil, err
		}
		if tables == 0 {
			continue
		}
		var count int
		if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{` + query.table + `}} WHERE ` + query.where).Row(&count); err != nil {
			return nil, err
		}
		if count > 0 {
			policies = append(policies, administratorUnenforcedPolicy{Key: query.key, Count: count})
		}
	}
	return policies, nil
}

// warnUnenforcedOrganizationPolicies says, as the server starts, which
// policies its data keeps that it won't enforce.
func warnUnenforcedOrganizationPolicies(app core.App) {
	policies, err := unenforcedOrganizationPolicies(app)
	if err != nil {
		app.Logger().Warn("The organization policies in this server's data couldn't be read", "error", err)
		return
	}
	if len(policies) == 0 {
		return
	}
	named := make([]string, 0, len(policies))
	for _, policy := range policies {
		named = append(named, fmt.Sprintf("%s %d", policy.Key, policy.Count))
	}
	message := "This server runs without the organization module, so the organization policies its data keeps are not enforced (" +
		strings.Join(named, ", ") + "); nothing is permanently deleted while a legal hold is active. Run the full Leafen build to enforce them."
	app.Logger().Warn(message)
	fmt.Println("Warning: " + message)
}

// errLegalHoldUnenforced keeps a permanent deletion while the data holds an
// active legal hold the server can't check without the organization module.
var errLegalHoldUnenforced = errors.Join(errLegalHoldPreventsPermanentDelete,
	errors.New("legal holds can't be checked without the organization module"))

// legalHoldsKeptWithoutModule reports whether the data holds an active legal
// hold the server runs without the module to check.
func legalHoldsKeptWithoutModule(app core.App) (bool, error) {
	if !tableKnown(app, organizationLegalHoldsTable) {
		return false, nil
	}
	var active int
	err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_organization_legal_holds}} WHERE [[status]] = 'active'`).Row(&active)
	return active > 0, err
}
